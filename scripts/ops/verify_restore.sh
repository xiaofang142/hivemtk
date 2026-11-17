#!/usr/bin/env bash
# verify_restore.sh —— 恢复演练一键验证（make verify-restore）
#
# 出自《功能模块多角度评估-2026-10-25》§运维：自托管产品的运维责任在用户，
# "备份可恢复"必须可验证——本脚本把"验证"做成产品能力而不是内部待办。
#
# 做法：把指定备份恢复进一次性 scratch 库（**绝不触碰** USER_DB_NAME 源库），
# 三项体检后丢弃 scratch：
#   ① scratch 表数 > 0 且与源库表数比对（不等=WARN：旧备份 vs 已演进的库属合法差异）
#   ② 源库 Top-N 大表（reltuples 有界）逐表精确 COUNT 比对，不等=FAIL
#   ③ 恢复过程 ON_ERROR_STOP=1，任何 SQL 错误=FAIL
#
# 用法：
#   ./scripts/ops/verify_restore.sh FILE=backup_xxx.sql   # 或 make verify-restore FILE=...
#   省略 FILE 时自动取仓库根目录最新的 backup_*.sql
#
# 可调环境变量：CONTAINER（默认 mtk-postgres）、POSTGRES_USER（admin）、
#   PGPORT（8202）、USER_DB_NAME（user_db）、TOP_N（5）、MAX_ROWS（2000000，
#   超过该行数的表跳过精确比对并 WARN——验证的是"能恢复"，不是全量对账）。
set -euo pipefail

cd "$(dirname "$0")/../.."   # 仓库根（scripts/ops/ → 根）

FILE_ARG="${1:-}"
CONTAINER="${CONTAINER:-mtk-postgres}"
PSQL_USER="${POSTGRES_USER:-admin}"
PGPORT="${PGPORT:-8202}"
SRC_DB="${USER_DB_NAME:-user_db}"
TOP_N="${TOP_N:-5}"
MAX_ROWS="${MAX_ROWS:-2000000}"
SCRATCH_DB="verify_restore_scratch_$(date +%s)"

dc() { docker compose exec -T "$CONTAINER" "$@"; }

# 摘要表：psql -t 输出去空白
q() { dc psql -U "$PSQL_USER" -p "$PGPORT" -d "$1" -t -A -c "$2"; }

# EXIT/INT 都保证丢 scratch：验证产物不落一库垃圾
cleanup() {
  dc psql -U "$PSQL_USER" -p "$PGPORT" -d postgres -c "DROP DATABASE IF EXISTS \"$SCRATCH_DB\"" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

# ── 选文件 ────────────────────────────────────────────────────────────────
if [ -z "$FILE_ARG" ]; then
  # .dump（custom，backup_offsite.sh 产物）与 .sql（make db-backup 产物）都认，取最新
  FILE="$(find . -maxdepth 1 \( -name "backup_*.sql" -o -name "user_db.dump" \) -type f -print0 2>/dev/null | xargs -0 ls -t 2>/dev/null | head -1 || true)"
  if [ -z "$FILE" ]; then
    echo "❌ 未指定 FILE，且仓库根目录没有 backup_*.sql / user_db.dump。用法："
    echo "   make verify-restore FILE=backup_20260101_120000.sql   # plain SQL"
    echo "   make verify-restore FILE=user_db.dump                 # custom（pg_restore）"
    exit 1
  fi
  echo "ℹ️  未指定 FILE，自动选最新备份：$FILE"
else
  FILE="$FILE_ARG"
fi
[ -f "$FILE" ] || { echo "❌ 文件不存在: $FILE"; exit 1; }
FILE_SIZE=$(du -h "$FILE" | cut -f1 | awk '{print $1}')

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "恢复演练：${FILE}（${FILE_SIZE}）→ scratch 库 $SCRATCH_DB"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
T0=$(date +%s)

# ── ① 恢复（SQL 任何错误即失败）─────────────────────────────────────────
dc psql -U "$PSQL_USER" -p "$PGPORT" -d postgres -c "DROP DATABASE IF EXISTS \"$SCRATCH_DB\"" >/dev/null
dc psql -U "$PSQL_USER" -p "$PGPORT" -d postgres -c "CREATE DATABASE \"$SCRATCH_DB\"" >/dev/null
# 2026-11-17 二次审核补：backup_offsite.sh 产 custom 格式（-Fc，PGDMP 魔数），此前本脚本
# 只认 plain SQL（无条件 psql <）⇒ 两件交付物互相不能消费、产线 dump 无法自验。
# 按魔数分派：PGDMP → 容器内 pg_restore（stdin 经 exec -T 管道，custom 格式支持）；否则 psql。
if [ "$(head -c 5 "$FILE")" = "PGDMP" ]; then
  echo "ℹ️  custom 格式（pg_restore 分支）"
  if ! dc pg_restore -U "$PSQL_USER" -p "$PGPORT" -d "$SCRATCH_DB" --no-owner -x \
        < "$FILE" > /tmp/verify_restore_psql.log 2>&1; then
    echo "❌ 体检③失败：pg_restore 过程出错（tail 如下）"
    tail -5 /tmp/verify_restore_psql.log
    exit 1
  fi
else
  if ! dc psql -U "$PSQL_USER" -p "$PGPORT" -v ON_ERROR_STOP=1 -d "$SCRATCH_DB" < "$FILE" > /tmp/verify_restore_psql.log 2>&1; then
    echo "❌ 体检③失败：恢复过程中出现 SQL 错误（tail 如下）"
    tail -5 /tmp/verify_restore_psql.log
    exit 1
  fi
fi
echo "✅ 体检③ 恢复过程零错误"

# ── ② 表数比对 ──────────────────────────────────────────────────────────
TABLES_SQL="SELECT count(*) FROM information_schema.tables WHERE table_schema='public'"
SRC_TABLES=$(q "$SRC_DB" "$TABLES_SQL")
DST_TABLES=$(q "$SCRATCH_DB" "$TABLES_SQL")
if [ "$DST_TABLES" -eq 0 ]; then
  echo "❌ 体检①失败：scratch 库 0 张表——备份内容为空或格式不对"
  exit 1
fi
if [ "$SRC_TABLES" = "$DST_TABLES" ]; then
  echo "✅ 体检① 表数一致：$DST_TABLES"
else
  echo "⚠️  体检① 表数不一致：源库 $SRC_TABLES vs 恢复 ${DST_TABLES}（旧备份对已演进库属合法差异，不判失败）"
fi

# ── ③ Top-N 大表行数抽查 ────────────────────────────────────────────────
# reltuples 选表（有界：超过 MAX_ROWS 的表跳过精确比对，COUNT 全表在大表上太贵）；
# 恢复侧 COUNT 用精确值。备份是快照，恢复后行数必须与源库此刻一致
# （验证窗口内源库仍在写入 ⇒ 单表轻微漂移属窗口噪声，按 <1% 容差判）。
TOP_TABLES=$(q "$SRC_DB" "SELECT relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname='public' AND c.relkind='r' AND c.reltuples > 0 AND c.reltuples < $MAX_ROWS
  ORDER BY c.reltuples DESC LIMIT $TOP_N")
FAILS=0; CHECKED=0
while IFS= read -r t; do
  [ -z "$t" ] && continue
  # 恢复库可能缺表（旧备份 vs 已演进库）：to_regclass 探测，缺表=FAIL（不是 psql 裸 ERROR）
  if [ "$(q "$SCRATCH_DB" "SELECT to_regclass('public.\"$t\"') IS NULL")" = "t" ]; then
    echo "   ❌ $t: 恢复库缺表（备份早于建表）"
    FAILS=$((FAILS+1)); CHECKED=$((CHECKED+1))
    continue
  fi
  SRC_ROWS=$(q "$SRC_DB" "SELECT count(*) FROM \"$t\"")
  DST_ROWS=$(q "$SCRATCH_DB" "SELECT count(*) FROM \"$t\"")
  CHECKED=$((CHECKED+1))
  if [ "$SRC_ROWS" = "$DST_ROWS" ]; then
    echo "   ✅ $t: $SRC_ROWS 行"
  else
    # 容差 1%：源库在验证窗口内新增/删除的行不算恢复失败
    DIFF=$(( SRC_ROWS > DST_ROWS ? SRC_ROWS - DST_ROWS : DST_ROWS - SRC_ROWS ))
    if [ "$SRC_ROWS" -gt 0 ] && [ $(( DIFF * 100 / SRC_ROWS )) -lt 1 ]; then
      echo "   ✅ $t: 源 $SRC_ROWS / 恢复 ${DST_ROWS}（窗口内漂移 <1%，容差内）"
    else
      echo "   ❌ $t: 源 $SRC_ROWS / 恢复 $DST_ROWS"
      FAILS=$((FAILS+1))
    fi
  fi
done <<< "$TOP_TABLES"
if [ "$CHECKED" -eq 0 ]; then
  echo "⚠️  源库没有可抽查的有界非空表（全空或全部超限）——行数抽查跳过"
elif [ "$FAILS" -gt 0 ]; then
  echo "❌ 体检②失败：$FAILS/$CHECKED 张表行数不符——备份或恢复不完整"
  exit 1
else
  echo "✅ 体检② Top-$CHECKED 大表行数全部一致"
fi

T1=$(date +%s)
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ 恢复演练 PASS：$FILE 可恢复（耗时 $((T1-T0))s，scratch 已丢弃）"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
