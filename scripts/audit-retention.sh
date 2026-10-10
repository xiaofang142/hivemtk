#!/usr/bin/env bash
# =============================================================================
# audit-retention.sh — 审计表保留策略（提交组6 A-5，2026-10-11）
#
# 三张审计表（operation_logs / mcp_call_audits / mcp_credential_audit_logs）按
# 保留天数清理。默认 90 天（与 operation_logs 手动清理端点同口径）。
# 在生产机上执行（依赖 mtk-postgres 容器名与 /opt/hivemtk/.env 的口令）。
# main.go 当前禁碰（无法注册常驻 cron），运维侧 crontab 挂本脚本即可：
#   17 3 * * * root bash /opt/hivemtk/scripts/audit-retention.sh --days 90 --execute
#
# 用法：
#   bash scripts/audit-retention.sh --days 90            # dry-run：只打印将删的行数
#   bash scripts/audit-retention.sh --days 90 --execute  # 真删
# =============================================================================
set -e
DAYS=90
EXECUTE=0
if [ "$1" = "--days" ]; then DAYS="${2:-90}"; [ "${3:-}" = "--execute" ] && EXECUTE=1; fi
if [ "$1" = "--execute" ]; then EXECUTE=1; [ "$2" = "--days" ] && DAYS="${3:-90}"; fi

PW=$(grep -m1 '^POSTGRES_PASSWORD=' /opt/hivemtk/.env | cut -d= -f2-)
PSQL="docker exec -e PGPASSWORD=$PW mtk-postgres psql -U admin -p 8202 -d user_db -tAc"

echo "== 审计保留清理（保留 ${DAYS} 天，execute=$EXECUTE）=="
for t in operation_logs mcp_call_audits mcp_credential_audit_logs; do
  cnt=$($PSQL "SELECT count(*) FROM $t WHERE created_at < now() - interval '$DAYS days';")
  echo "  $t: 将删 $cnt 行"
  if [ "$EXECUTE" = "1" ] && [ "$cnt" -gt 0 ]; then
    $PSQL "DELETE FROM $t WHERE created_at < now() - interval '$DAYS days';" >/dev/null
    echo "  $t: 已删除"
  fi
done
[ "$EXECUTE" = "1" ] || echo "（dry-run：未执行删除，加 --execute 生效）"
