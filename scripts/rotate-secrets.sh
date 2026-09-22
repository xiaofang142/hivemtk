#!/usr/bin/env bash
# rotate-secrets.sh — 把已泄露/到期的凭证在本机环境里真正换掉（T-P0-05 的备用收口动作）
#
# 状态：2026-09-19 用户已就 F1「真实口令泄露进公开 git 历史」拍板【暂不处置】，
#       因此本脚本默认**拒绝执行**，只允许 --list / --dry-run。要真换，必须人工显式授权：
#           ROTATE_AUTHORIZED=1 bash scripts/rotate-secrets.sh --all-burned
#       （授权等价于重启 F1 决策；见 docs/audit-2026-09-19-sessionC.md §F1）
#
# 为什么需要脚本而不是照着文档手敲：一次轮换要同时落到 4 个 .env、1 次 ALTER USER、
# 2 个运行中的服务重启。少做其中任何一步，都会得到"配置文件与真实口令漂移"的状态——
# 表现为上千条测试假红，而不是显式报错（2026-09-18 就踩过一次）。所以要么全做，要么不做。
#
# 用法：
#   bash scripts/rotate-secrets.sh --list                # 只报告登记表，不读不写
#   bash scripts/rotate-secrets.sh --dry-run <secret>    # 在临时副本上演练，真实 .env 零改动
#   ROTATE_AUTHORIZED=1 bash scripts/rotate-secrets.sh <secret>
#   ROTATE_AUTHORIZED=1 bash scripts/rotate-secrets.sh --all-burned
#   bash scripts/rotate-secrets.sh --rollback <备份目录> # 用备份把文件与 role 口令改回旧值
#
# <secret> ∈ db_user | db_platform | merchant_hmac | license | jwt_user
# 退出码：0=成功，1=轮换后校验失败，2=被互斥/授权拦住或参数错误
set -uo pipefail

WORKROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)   # .../www/go/hivemtk
ENV_USER=$WORKROOT/hivemtk/.env
ENV_USERSRV=$WORKROOT/hivemtk/user-server/.env
ENV_ASSET=$WORKROOT/assetdpo/.env
ENV_PLAT=$WORKROOT/hivemtk-platform/platform-server/.env
CONTAINER_USER=mtk-postgres
CONTAINER_PLAT=mtk-platform-postgres
USER_PORT=${USER_SERVER_PORT:-8204}
PLAT_PORT=8205

GREEN='\033[0;32m'; YELLOW='\033[0;33m'; RED='\033[0;31m'; NC='\033[0m'
log()  { printf "${GREEN}[rotate]${NC} %s\n" "$*"; }
warn() { printf "${YELLOW}[rotate]${NC} %s\n" "$*"; }
err()  { printf "${RED}[rotate]${NC} %s\n" "$*" >&2; }

# ── 凭证登记表 ────────────────────────────────────────────────────────────
# 每条：名称|是否已被公开仓历史泄露|落地的 "文件常量:键" 列表(;分隔)|额外动作|需重启的端口
# 泄露判定来自 git log --all -S<真值> 在公开 hivemtk 仓的实测命中，非推断；明细见
# docs/operations/secret_rotation.md 二A.1（含"本机在用的 JWT 密钥其实没进过仓"这条反直觉结论）
SECRETS='
db_user|burned|ENV_USER:POSTGRES_PASSWORD;ENV_USERSRV:POSTGRES_PASSWORD;ENV_ASSET:HIVE_DB_PASSWORD|alter_user|8204
db_platform|burned|ENV_PLAT:POSTGRES_PASSWORD|alter_platform|8205
merchant_hmac|burned|ENV_USER:MERCHANT_API_SECRET;ENV_PLAT:MERCHANT_API_SECRET||8204;8205
license|burned|ENV_USER:PLATFORM_LICENSE_SECRET;ENV_PLAT:PLATFORM_LICENSE_SECRET||8204;8205
jwt_user|not-leaked|ENV_USER:USER_JWT_SECRET;ENV_USER:JWT_SECRET;ENV_USERSRV:USER_JWT_SECRET||8204
'
BURNED_LIST='db_user db_platform merchant_hmac license'

secret_row() { printf '%s\n' "$SECRETS" | grep -E "^$1\|" | head -1; }
envfile_path() { case "$1" in ENV_USER) echo "$ENV_USER";; ENV_USERSRV) echo "$ENV_USERSRV";; ENV_ASSET) echo "$ENV_ASSET";; ENV_PLAT) echo "$ENV_PLAT";; *) return 1;; esac; }

# 取键值：.env 存在同文件重复键（历史遗留），取第一条；用 awk 而非 cut，
# 因为口令本身可能含 '='，cut -d= -f2 会截断成错值。
read_key() { awk -F= -v K="$2" '$1 ~ "^[[:space:]]*"K"[[:space:]]*$" && !seen++ {v=substr($0,index($0,"=")+1); gsub(/[[:space:]]*$/,"",v); gsub(/^["'\'']|["'\'']$/,"",v); print v}' "$1"; }

# 同文件内所有重复出现都要改，否则后读到的那一段会把新值盖掉
write_key() {
  local f=$1 k=$2 v=$3
  [ -f "$f" ] || { err "缺少文件 $f"; return 1; }
  awk -v K="$k" -v V="$v" 'BEGIN{p=K "="}
    { if (index($0,p)==1) { sub(p "[^\r]*", p V) }; print }' "$f" > "$f.rot.$$" && mv "$f.rot.$$" "$f"
}

gen() { openssl rand -hex "$1"; }

dbpw_verify() { PGPASSWORD=$2 psql -h 127.0.0.1 -p "$1" -U admin -d "$3" -Atc 'select 1' >/dev/null 2>&1; }

alter_role() { # $1=user|platform $2=新口令
  if [ "$1" = user ]; then
    docker exec "$CONTAINER_USER" sh -c "PGPORT=8202 psql -U admin -d postgres -tc \"ALTER USER admin WITH PASSWORD '$2'\"" >/dev/null
  else
    docker exec "$CONTAINER_PLAT" sh -c "PGPORT=8201 psql -U admin -d postgres -tc \"ALTER USER admin WITH PASSWORD '$2'\"" >/dev/null
  fi
}

# ── 互斥：有人在跑测试/灌数时禁止切库口令（切了不会报错，只会变成上千条假红）──
interlock() {
  local busy
  busy=$(pgrep -fl 'go test|golangci|vite build|api_verify|deep_trace|deep_lib|bulk_seed|geo_full_test|expand_knowledge|bootstrap\.sh' 2>/dev/null | grep -vw "$$" || true)
  [ -z "$busy" ] && return 0
  err "检测到并发作业，轮换会把它变成假红："; printf '%s\n' "$busy" | sed 's/^/    /' >&2
  err "请等它跑完再执行（或 ROTATE_FORCE=1 强行继续，不推荐）。"
  [ "${ROTATE_FORCE:-0}" = "1" ] && { warn "ROTATE_FORCE=1，强行继续"; return 0; }
  return 2
}

# ── 参数 ─────────────────────────────────────────────────────────────────
MODE=${1:-}; shift || true
DRY=0; LIST=0; ROLLDIR=""
case "$MODE" in
  --dry-run)  DRY=1;  MODE=${1:-}; shift || true ;;
  --list)     LIST=1 ;;
  --rollback) ROLLDIR=${1:-}; shift || true ;;
esac

if [ "$LIST" = 1 ] || [ -z "$MODE" ]; then
  printf '%-14s %-12s %s\n' SECRET 泄露状态 落地位置
  printf '%s\n' "$SECRETS" | while IFS='|' read -r name burn loc _a _p; do
    [ -n "$name" ] || continue
    printf '%-14s %-12s %s\n' "$name" "$burn" "${loc//;/  +  }"
  done
  echo; echo "已泄露集合（--all-burned 覆盖）：$BURNED_LIST"
  echo "依据：git log -S<真值> 在公开 hivemtk 仓的实测命中（以清理提交 81955cfc 为界）"
  echo "判据与逐项结果见 docs/operations/secret_rotation.md 二A.1"
  exit 0
fi

if [ -n "$ROLLDIR" ]; then
  [ -s "$ROLLDIR/old.tsv" ] || { err "$ROLLDIR/old.tsv 不存在或为空"; exit 2; }
  warn "回滚 $(wc -l < "$ROLLDIR/old.tsv" | tr -d ' ') 处写入"
  while IFS=$'\t' read -r f k o; do
    [ -n "$f" ] && [ -n "$o" ] || continue
    write_key "$f" "$k" "$o" && log "  还原 $f:$k"
    [ "$k" = POSTGRES_PASSWORD ] || continue
    case "$f" in
      *hivemtk-platform*) alter_role platform "$o" && log "  8201 role 口令已改回旧值" ;;
      *)                  alter_role user "$o"     && log "  8232 role 口令已改回旧值" ;;
    esac
  done < "$ROLLDIR/old.tsv"
  warn "回滚后需重启服务（env 只在启动时读一次），并跑 scripts/api_verify_full.py 复测"
  exit 0
fi

# 真实执行需要人工显式授权（F1 决策：暂不处置）
if [ "$DRY" = 0 ] && [ "${ROTATE_AUTHORIZED:-0}" != "1" ]; then
  err "拒绝执行：F1 泄露口令处置已由用户拍板为「暂不处置」（2026-09-19）。"
  err "本脚本仅开放 --list / --dry-run。确要轮换请显式加授权位："
  err "    ROTATE_AUTHORIZED=1 bash scripts/rotate-secrets.sh --all-burned"
  exit 2
fi
[ "$DRY" = 1 ] && [ "${ROTATE_AUTHORIZED:-0}" = 1 ] && { err "--dry-run 与 ROTATE_AUTHORIZED 互斥"; exit 2; }

TARGETS=""
case "$MODE" in
  --all-burned) TARGETS=$BURNED_LIST ;;
  *) secret_row "$MODE" >/dev/null || { err "未知 secret：$MODE（见 --list）"; exit 2; }; TARGETS=$MODE ;;
esac

# 演练：把 4 个 .env 复制到临时目录后在其上操作，真实文件全程只读
STAGE=""
if [ "$DRY" = 1 ]; then
  STAGE=$(mktemp -d); trap 'rm -rf "$STAGE"' EXIT
  for f in "$ENV_USER" "$ENV_USERSRV" "$ENV_ASSET" "$ENV_PLAT"; do
    [ -f "$f" ] || continue
    d="$STAGE/$(cd "$(dirname "$f")" && pwd | sed "s|$WORKROOT/||")"; mkdir -p "$d"
    cp "$f" "$d/$(basename "$f")"
  done
  ENV_USER=$STAGE/hivemtk/.env
  ENV_USERSRV=$STAGE/hivemtk/user-server/.env
  ENV_ASSET=$STAGE/assetdpo/.env
  ENV_PLAT=$STAGE/hivemtk-platform/platform-server/.env
  log "演练模式：只在 $STAGE 的副本上写，真实 .env 不受影响"
fi

rc=0
for t in $TARGETS; do
  row=$(secret_row "$t") || { err "登记表里没有 $t"; rc=1; continue; }
  IFS='|' read -r name burn loc act ports <<<"$row"
  if [ "$burn" = not-leaked ] && [ "$MODE" = --all-burned ]; then
    warn "$name：本机在用的值从未进过版本库，换它只有打扰没有收益，跳过"
    continue
  fi
  if [ "$DRY" = 0 ]; then interlock || exit 2; fi

  new=$(gen 32)
  { [ "$act" = alter_user ] || [ "$act" = alter_platform ]; } && new=$(gen 24)
  BK=${TMPDIR:-/tmp}/hivemtk-rotate-$(date +%Y%m%d-%H%M%S)
  mkdir -p "$BK" && chmod 700 "$BK"
  log "轮换 $name（新值长度 ${#new}，不打印明文）"

  # 只处理"落点里确实有这个键"的文件：PLATFORM_LICENSE_SECRET 已随授权功能下线从两侧 .env 删除，
  # 若把"键不存在"计入写后校验，--all-burned 会永久红、下面的备份计数还会报文件不存在。
  # 判定结果经文件回传（warn 走 stdout，用命令替换捕获会把提示语当成数据）。
  : > "$BK/old.tsv"
  printf '%s\n' "$loc" | tr ';' '\n' | while IFS=: read -r ef key; do
    [ -n "$ef" ] || continue
    f=$(envfile_path "$ef") || continue
    old=$(read_key "$f" "$key")
    [ -n "$old" ] || { warn "  $f 里没有 $key，跳过"; continue; }
    printf '%s\t%s\t%s\n' "$f" "$key" "$old" >> "$BK/old.tsv"
    write_key "$f" "$key" "$new" && log "  写入 $(basename "$(dirname "$f")")/$(basename "$f"):$key"
  done
  if [ ! -s "$BK/old.tsv" ]; then
    warn "$name：所有落点都已无此键 —— 该凭证已随功能下线，无需轮换"
    rm -rf "$BK"; continue
  fi

  # 写后校验：只核"原本就有这个键"的落点，键的每一条重复出现都必须是新值，且不能误伤别的键
  bad=$(cut -f1,2 "$BK/old.tsv" | while IFS=$'\t' read -r f key; do
    grep -E "^[[:space:]]*${key}=" "$f" | grep -qxF "${key}=${new}" || echo "$f:$key 未全部更新"
  done)
  [ -n "$bad" ] && { err "写后校验失败：$bad"; rc=1; }

  if [ "$DRY" = 1 ]; then
    log "  演练完成：备份 $(wc -l < "$BK/old.tsv" | tr -d ' ') 条，未触碰数据库与服务"
    rm -rf "$BK"; continue
  fi

  case "$act" in
    alter_user)
      alter_role user "$new" || { err "ALTER USER 失败（$CONTAINER_USER）"; rc=1; continue; }
      dbpw_verify 8232 "$new" user_db || { err "新口令连不上 8232"; rc=1; continue; }
      dbpw_verify 8232 "$(cut -f3 "$BK/old.tsv" | head -1)" user_db && { err "旧口令仍可用＝没换掉"; rc=1; continue; }
      log "  8232：新口令可用、旧口令已失效"
      ;;
    alter_platform)
      alter_role platform "$new" || { err "ALTER USER 失败（$CONTAINER_PLAT）"; rc=1; continue; }
      dbpw_verify 8201 "$new" platform_db || { err "新口令连不上 8201"; rc=1; continue; }
      log "  8201：新口令可用"
      ;;
  esac

  # 服务侧：env 只在启动时读一次，不重启等于没换。
  # 按实测启动方式分别复现：
  #   :8204 进程 env 里同时含根 .env 与 user-server/.env 的键 → 两个都 source（后者覆盖）
  #   :8205 进程 env 里没有根 .env 独有键（LLM_MODEL/USER_JWT_SECRET 等实测 0 命中）→ 只 source 自己那份
  for p in ${ports//;/ }; do
    pid=$(lsof -nP -tiTCP:"$p" -sTCP:LISTEN 2>/dev/null | head -1)
    [ -n "$pid" ] || { warn "  :$p 无监听进程，跳过重启（下次启动自然用新值）"; continue; }
    cwd=$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | grep '^n' | cut -c2-)
    cmdstr=$(ps -o command= -p "$pid")
    kill "$pid" 2>/dev/null
    for _ in 1 2 3 4 5 6 7 8 9 10; do kill -0 "$pid" 2>/dev/null || break; sleep 0.5; done
    kill -9 "$pid" 2>/dev/null || true
    envsrc=". ./.env"
    [ "$p" = "$USER_PORT" ] && envsrc=". $ENV_USER; [ -f .env ] && . ./.env"
    ( cd "$cwd" && set -a && eval "$envsrc" && set +a; nohup $cmdstr >> "$BK/server-$p.log" 2>&1 & ) \
      && log "  :$p 已用新 env 重启（cwd=$cwd cmd=$cmdstr）"
  done
  sleep 3
  for p in ${ports//;/ }; do
    lsof -nP -tiTCP:"$p" -sTCP:LISTEN >/dev/null 2>&1 || continue
    curl -sf -m 5 "http://127.0.0.1:$p/health" >/dev/null \
      || { err "  :$p 健康检查未通过 → 立即回滚：bash $0 --rollback $BK"; rc=1; }
  done
  [ $rc = 0 ] && log "  备份在 $BK/old.tsv（700 权限，确认稳定后自行删除）"
done

[ $rc != 0 ] && { err "轮换未全部成功，请先回滚再排查"; exit 1; }
log "完成。收尾必做三件事："
echo "  1) bash scripts/check-secrets.sh            # 确认没有新明文进仓"
echo "  2) set -a && . hivemtk/.env && set +a && python3 scripts/api_verify_full.py   # 三端验收"
echo "  3) 部署机不会因本机轮换而变安全：同名凭证要在部署侧各自轮换一遍"
exit 0
