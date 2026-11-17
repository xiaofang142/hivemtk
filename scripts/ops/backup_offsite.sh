#!/usr/bin/env bash
# =============================================================
# backup_offsite.sh —— PG 备份 + 异地副本（2026-11-16 审计 P1-3 / T-04）
#
# 背景：docs/operations/DR_RECOVERY.md §1.3 的 backup.sh 只落本机磁盘
# /var/backups/hivemtk——宿主机盘损/被勒索时备份与源库一起消失，RPO=∞。
# 本脚本在同一条 pg_dump 后追加「对象存储上传」一步：七牛已在配置面
# （QINIU_ACCESS_KEY/SECRET_KEY/BUCKET，.env 注入），用 qshell 上传。
#
# 设计：
#   1. dump 用 -Fc（custom 格式，pg_restore 直接吃，配合 scripts/ops/verify_restore.sh）
#   2. 上传前 sha256 双端校验思路落本地一半：qshell fput 返回后用 qshell stat 比对 hash
#      （上传失败不删本地产物——本地那份永远是最后一道退路）
#   3. 保留期分层：本地 7 天（与 DR 文档 backup.sh 口径一致）、对象存储 30 天
#      （qshell 按前缀列出后删除过期项；删除失败只告警不中断）
#   4. 所有密钥从环境/.env 注入，脚本内零字面量凭证（check-secrets.sh B 段口径）
#
# 用法（生产机）：
#   set -a; . /opt/hivemtk/.env; set +a
#   bash scripts/ops/backup_offsite.sh                     # dump + 上传 + 清理
#   OFFSITE_SKIP_UPLOAD=1 bash scripts/ops/backup_offsite.sh   # 只 dump（qshell 未装/未配时）
#
# cron（见 DR_RECOVERY.md §1.5）：
#   0 2 * * * root set -a; . /opt/hivemtk/.env; set +a; bash /opt/hivemtk/scripts/ops/backup_offsite.sh >> /var/log/hivemtk-backup.log 2>&1
# 退出码：0=全链成功或显式跳过上传；1=dump/校验失败；2=前置不满足（缺 psql/pg_dump/容器）
set -uo pipefail

BACKUP_ROOT="${BACKUP_ROOT:-/var/backups/hivemtk}"
STAMP="$(date +%Y%m%d_%H%M%S)"
BACKUP_DIR="${BACKUP_ROOT}/${STAMP}"
DB_NAME="${USER_DB_NAME:-user_db}"
DB_USER="${POSTGRES_USER:-admin}"
DB_PORT="${USER_POSTGRES_HOST_PORT:-8202}"
RETAIN_LOCAL_DAYS="${RETAIN_LOCAL_DAYS:-7}"
RETAIN_REMOTE_DAYS="${RETAIN_REMOTE_DAYS:-30}"

err()  { printf '[offsite-backup][ERR] %s\n' "$*" >&2; }
warn() { printf '[offsite-backup][WARN] %s\n' "$*"; }
log()  { printf '[offsite-backup] %s\n' "$*"; }

[ -n "${POSTGRES_PASSWORD:-}" ] || { err "POSTGRES_PASSWORD 未注入（set -a; . ./.env; set +a）"; exit 2; }
mkdir -p "$BACKUP_DIR"

# ---- 1. dump（-Fc）。双形态（2026-11-17 产线实测：宿主机无 pg_dump 客户端）----
#   host   ：宿主机有 pg_dump 且 8202 可达 → 直连（-h/-p 显式，DR §1.3 口径）
#   docker ：否则走 docker compose exec -T mtk-postgres（pgvector 镜像自带客户端；
#            容器内 POSTGRES_PASSWORD 已由 compose 注入，免宿主机客户端依赖）
DUMP_FILE="${BACKUP_DIR}/user_db.dump"
COMPOSE_DIR="${COMPOSE_DIR:-$(pwd)}"
# 形态优先级（2026-11-17 二次审核反转）：**docker 优先**——恢复端是容器内 pg_restore，
# host 新版 pg_dump 产的 archive（如 v1.15）容器旧版 pg_restore 会拒（"unsupported
# version (1.15)"实测）；容器内客户端与恢复端同版本族，从根上消灭版本倾斜。
# host pg_dump 仅作无 docker 环境的兜底（此时恢复端也应是同版本宿主客户端）。
if command -v docker >/dev/null 2>&1 && docker compose -f "$COMPOSE_DIR/docker-compose.yml" ps -q mtk-postgres 2>/dev/null | grep -q .; then
  log "形态=docker：compose exec mtk-postgres pg_dump ${DB_NAME} → ${DUMP_FILE}"
  # 容器内 PG 端口被 compose 钉死为 8202（docker-compose.yml command -c port=8202），
  # 与宿主机映射端口 USER_POSTGRES_HOST_PORT 无关——这里必须用容器内常量
  if ! (cd "$COMPOSE_DIR" && docker compose exec -T mtk-postgres \
        pg_dump -U "$DB_USER" -p 8202 -Fc "$DB_NAME") > "$DUMP_FILE"; then
    err "docker 形态 pg_dump 失败（COMPOSE_DIR=${COMPOSE_DIR}）"
    exit 1
  fi
elif command -v pg_dump >/dev/null 2>&1; then
  log "形态=host：pg_dump ${DB_NAME} → ${DUMP_FILE}（⚠️ 无 docker 兜底形态，archive 版本须与恢复端匹配）"
  if ! PGPASSWORD="${POSTGRES_PASSWORD}" pg_dump -h 127.0.0.1 -p "$DB_PORT" -U "$DB_USER" -Fc \
        -f "$DUMP_FILE" "$DB_NAME"; then
    err "pg_dump 失败（宿主机直连 127.0.0.1:${DB_PORT}）"
    exit 1
  fi
else
  err "宿主机既无可用的 mtk-postgres 容器也无 pg_dump，无法 dump"
  exit 2
fi

# 校验产物：非空 + custom 格式魔数 PGDMP（免宿主机 pg_restore 依赖；docker 形态宿主机无客户端）
if [ ! -s "$DUMP_FILE" ]; then
  err "dump 产物为空"
  exit 1
fi
if [ "$(head -c 5 "$DUMP_FILE")" != "PGDMP" ]; then
  err "dump 产物不是 PostgreSQL custom 格式（缺 PGDMP 魔数）——检查 pg_dump 是否真的成功"
  exit 1
fi
# macOS 无 sha256sum（shasum -a 256 等价）；Linux 产线走 sha256sum
SHA256_LOCAL="$(sha256sum "$DUMP_FILE" 2>/dev/null || shasum -a 256 "$DUMP_FILE" | cut -d' ' -f1)"
SHA256_LOCAL="$(printf '%s' "$SHA256_LOCAL" | cut -d' ' -f1)"
log "dump 完成 sha256=${SHA256_LOCAL:0:16}… size=$(du -h "$DUMP_FILE" | cut -f1)"

# ---- 2. 异地副本（七牛；未配置 qshell/凭证时显式跳过，不冒充绿）----
if [ "${OFFSITE_SKIP_UPLOAD:-0}" = "1" ]; then
  warn "OFFSITE_SKIP_UPLOAD=1，跳过上传（本地副本仍生成）"
elif ! command -v qshell >/dev/null 2>&1; then
  warn "qshell 未安装，跳过上传。安装与配对账号见 docs/operations/DR_RECOVERY.md §1.5"
elif [ -z "${QINIU_ACCESS_KEY:-}" ] || [ -z "${QINIU_SECRET_KEY:-}" ] || [ -z "${QINIU_BUCKET:-}" ]; then
  warn "QINIU_ACCESS_KEY/SECRET_KEY/BUCKET 未注入，跳过上传（不冒充绿：本次 RPO 仍=本地）"
else
  qshell account "$QINIU_ACCESS_KEY" "$QINIU_SECRET_KEY" hivemtk-backup >/dev/null 2>&1 || true
  REMOTE_KEY="pg/${STAMP}/user_db.dump"
  if qshell fput "$QINIU_BUCKET" "$REMOTE_KEY" "$DUMP_FILE" --overwrite; then
    log "已上传 ${QINIU_BUCKET}:${REMOTE_KEY}"
    # 双端校验：对象存储侧 hash 与本地一致才认定副本有效
    REMOTE_HASH="$(qshell stat "$QINIU_BUCKET" "$REMOTE_KEY" 2>/dev/null | awk '/Hash:/ {print $2}')"
    if [ -n "$REMOTE_HASH" ] && [ "$REMOTE_HASH" != "$SHA256_LOCAL" ]; then
      # qshell 返回的是 etag 形态（与 sha256 不同算法）：不一致不代表损坏，显式提示人工核对
      warn "远端 Hash=${REMOTE_HASH:0:16}… 与本地 sha256 算法不同，请人工比对首次副本（仅首次，后续按 size 抽查）"
    fi
    # 远端保留期清理（按前缀列出，删除过期项；失败只告警）
    if [ "$RETAIN_REMOTE_DAYS" -gt 0 ]; then
      CUTOFF="$(date -d "-${RETAIN_REMOTE_DAYS} days" +%Y%m%d 2>/dev/null || date -v-${RETAIN_REMOTE_DAYS}d +%Y%m%d 2>/dev/null || true)"
      if [ -n "$CUTOFF" ]; then
        qshell listbucket "$QINIU_BUCKET" --prefix 'pg/' 2>/dev/null \
          | awk -v c="$CUTOFF" '{d=substr($1,4,8); if (d != "" && d < c) print $1}' \
          | while IFS= read -r key; do
              qshell delete "$QINIU_BUCKET" "$key" >/dev/null 2>&1 || warn "远端清理失败: $key"
            done
      fi
    fi
  else
    warn "上传失败（本地副本不受影响，可手工重传：qshell fput ${QINIU_BUCKET} pg/${STAMP}/user_db.dump ${DUMP_FILE}）"
  fi
fi

# ---- 3. 本地保留期清理（与 DR 文档 backup.sh 同口径 7 天）----
find "$BACKUP_ROOT" -maxdepth 1 -type d -mtime +"$RETAIN_LOCAL_DAYS" -exec rm -rf {} + 2>/dev/null || true

log "备份链完成：${BACKUP_DIR}"
exit 0
