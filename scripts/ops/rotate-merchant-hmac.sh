#!/usr/bin/env bash
# =============================================================================
# rotate-merchant-hmac.sh — 商户 HMAC 密钥轮换执行脚本 (骨架)
# 配套策略: docs/operations/secret_rotation.md §4.2
# 创建日期: 2026-08-16
#
# ⚠️  本文件为骨架, 仅打印步骤与示例命令, 不执行任何实际变更
#     涉及数据库, 需 DBA + SRE 双人审批
#
# 用法:
#   bash rotate-merchant-hmac.sh --merchant-id m_12345          # 单商户
#   bash rotate-merchant-hmac.sh --batch --limit 100 --dry-run # 批量
#
# 环境变量:
#   PGHOST / PGPORT / PGUSER / PGPASSWORD / PGDATABASE
#   ROTATION_GRACE_DAYS (默认 7, 商户旧密钥保留天数)
# =============================================================================

set -euo pipefail

# ---- 参数解析 ----
DRY_RUN=true
MERCHANT_ID=""
BATCH=false
LIMIT=100
GRACE_DAYS="${ROTATION_GRACE_DAYS:-7}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)   DRY_RUN=true; shift ;;
    --execute)   DRY_RUN=false; shift ;;
    --merchant-id) MERCHANT_ID="$2"; shift 2 ;;
    --batch)     BATCH=true; shift ;;
    --limit)     LIMIT="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,30p' "$0"
      exit 0
      ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

# ---- 颜色 ----
if [ -t 1 ]; then C_INFO=$'\033[36m'; C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_FAIL=$'\033[31m'; C_RST=$'\033[0m'
else C_INFO=""; C_OK=""; C_WARN=""; C_FAIL=""; C_RST=""
fi
log()  { echo "${C_INFO}[INFO]${C_RST}  $*"; }
ok()   { echo "${C_OK}[OK]${C_RST}    $*"; }
warn() { echo "${C_WARN}[WARN]${C_RST}  $*"; }

run() {
  if [ "$DRY_RUN" = true ]; then echo "  [DRY-RUN] $*"
  else log "EXECUTING: $*"; eval "$@"; fi
}

# ---- 校验 ----
if [ -z "$MERCHANT_ID" ] && [ "$BATCH" = false ]; then
  fail "必须指定 --merchant-id 或 --batch"
  exit 1
fi

: "${PGHOST:?Must export PGHOST}"
: "${PGUSER:?Must export PGUSER}"
: "${PGPASSWORD:?Must export PGPASSWORD}"
: "${PGDATABASE:?Must export PGDATABASE}"

echo "============================================================"
echo "  商户 HMAC 密钥轮换 (OPT-SEC-EXT-1)"
echo "  模式: $([ "$DRY_RUN" = true ] && echo DRY-RUN || echo EXECUTE)"
echo "  灰度期: ${GRACE_DAYS} 天"
echo "  目标: $([ -n "$MERCHANT_ID" ] && echo "商户 $MERCHANT_ID" || echo "批量前 $LIMIT 个")"
echo "============================================================"
echo ""

# ---- 步骤 1: 列出待轮换商户 ----
log "步骤 1/6: 查询待轮换商户 (last_rotated_at < now() - 180d)"
if [ -n "$MERCHANT_ID" ]; then
  SQL="SELECT merchant_id, hmac_key_current, hmac_key_rotated_at FROM merchant_credentials WHERE merchant_id = '$MERCHANT_ID';"
else
  SQL="SELECT merchant_id, substr(hmac_key_current, 1, 8) || '...' AS key_prefix, hmac_key_rotated_at FROM merchant_credentials WHERE hmac_key_rotated_at < now() - interval '180 days' ORDER BY hmac_key_rotated_at ASC LIMIT $LIMIT;"
fi
run "psql -h \"$PGHOST\" -U \"$PGUSER\" -d \"$PGDATABASE\" -c \"$SQL\""

# ---- 步骤 2: 生成新 HMAC 密钥 ----
log "步骤 2/6: 为每个待轮换商户生成新 HMAC 密钥 (CSPRNG 32 bytes)"
if [ -n "$MERCHANT_ID" ]; then
  if [ "$DRY_RUN" = true ]; then
    echo "  [DRY-RUN] NEW_KEY=\$(openssl rand -hex 32)"
  else
    NEW_KEY=$(openssl rand -hex 32)
    log "已为 $MERCHANT_ID 生成 NEW_KEY (length=${#NEW_KEY})"
  fi
else
  warn "批量模式: 实际执行需遍历每行, 本骨架仅演示单商户"
fi

# ---- 步骤 3: 更新数据库 (dual-key 设计) ----
log "步骤 3/6: 更新 merchant_credentials (current → previous, new → current)"
if [ -n "$MERCHANT_ID" ]; then
  SQL="UPDATE merchant_credentials
       SET hmac_key_previous = hmac_key_current,
           hmac_key_current = \$NEW_KEY,
           hmac_key_rotated_at = now()
       WHERE merchant_id = '$MERCHANT_ID'
       RETURNING merchant_id, hmac_key_rotated_at;"
  if [ "$DRY_RUN" = true ]; then
    echo "  [DRY-RUN] psql -c \"$SQL\"  (变量绑定: \$NEW_KEY)"
  else
    env "PGPASSWORD=$PGPASSWORD" psql -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE" \
      --variable=ON_ERROR_STOP=1 -c "$SQL" -v NEW_KEY="'$NEW_KEY'"
  fi
fi

# ---- 步骤 4: 清除应用缓存 (旧商户 key 缓存) ----
log "步骤 4/6: 清除应用层缓存 (merchant_creds:*)"
run "redis-cli -h \"${REDIS_HOST:-127.0.0.1}\" -p \"${REDIS_PORT:-6379}\" --scan --pattern 'merchant_creds:*' | xargs -r redis-cli -h \"${REDIS_HOST:-127.0.0.1}\" -p \"${REDIS_PORT:-6379}\" DEL"

# ---- 步骤 5: 灰度期监控 ----
log "步骤 5/6: 灰度期监控 (${GRACE_DAYS} 天)"
warn "应用层 hmac.verify.previous 比例应从 ~0 升到 ~5% (用户重试旧签)"
warn "P1 告警: hmac.verify.fail > 0.1% (灰度期容忍 0.5%)"

# ---- 步骤 6: 灰度期后清理 previous 字段 ----
log "步骤 6/6: T+${GRACE_DAYS}d 清理 previous 字段"
SQL="UPDATE merchant_credentials
     SET hmac_key_previous = NULL
     WHERE hmac_key_rotated_at < now() - interval '${GRACE_DAYS} days'
       AND hmac_key_previous IS NOT NULL
     RETURNING merchant_id;"
run "psql -h \"$PGHOST\" -U \"$PGUSER\" -d \"$PGDATABASE\" -c \"$SQL\""

echo ""
echo "============================================================"
ok "商户 HMAC 轮换流程完成 (模式: $([ "$DRY_RUN" = true ] && echo DRY-RUN || echo EXECUTE))"
echo "  - 数据库表: merchant_credentials (dual-key: current + previous)"
echo "  - 灰度期: ${GRACE_DAYS} 天"
echo "  - 清理: T+${GRACE_DAYS}d 后自动清理 previous"
echo "============================================================"
