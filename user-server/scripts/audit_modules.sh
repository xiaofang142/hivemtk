#!/usr/bin/env bash
# 逐模块功能审计自动循环（架构师审计轮）：
# 按功能模块顺序跑真库测试，结果落盘 logs/audit/<module>.log，供分诊与回归。
# 用法: POSTGRES_TEST_* 已导出后 bash scripts/audit_modules.sh [模块名...]
set -u
cd "$(dirname "$0")/.."
LOGDIR="logs/audit"
mkdir -p "$LOGDIR"

MODULES_DEFAULT="
aiagent-llm:./internal/aiagent/llm/...:.
aiagent-agent:./internal/aiagent/agent/...:.
aiagent-rag-knowledge:./internal/aiagent/rag/... ./internal/aiagent/knowledge/... ./internal/aiagent/mcp/... ./internal/aiagent/eval/...:.
geo:./internal/geo/...:.
browser-automation:./internal/browser_automation/...:.
bridge-channelgw:./internal/bridge/... ./internal/channelgw/...:.
egolite:./internal/egolite/...:.
sop-confidence:./internal/service/sop/... ./internal/service/confidence/... ./internal/service/feedback_loop/... ./internal/service/trace_learning/... ./internal/service/humanize/... ./internal/service/cards/...:.
ops-reporting:./internal/ops/... ./internal/monitor/... ./internal/etl/... ./internal/reporting/...:.
content:./internal/content/... ./internal/contentguard/...:.
email-sms:./internal/email/... ./internal/service/sms/... ./internal/service/email/... ./internal/pkg/mail/...:.
websocket-event:./internal/websocket/... ./internal/event/...:.
core-infra:./internal/pkg/... ./internal/storage/... ./internal/cache/... ./internal/config/... ./internal/platform/...:.
model-migration:./internal/model/... ./internal/migration/...:.
repository-core:./internal/repository/...:.
router-middleware:./internal/router/... ./internal/middleware/...:.
app:./internal/app/...:.
controller-core:./internal/controller/...:.
service-core:./internal/service/...:.
"

SELECTED="$*"
[ -z "$SELECTED" ] && SELECTED="$MODULES_DEFAULT"

echo "$MODULES_DEFAULT" | while IFS=: read -r name pkgs _; do
  [ -z "$name" ] && continue
  # 仅跑被选中的模块（默认全量）
  case " $SELECTED " in
    *" $name "*|*" $MODULES_DEFAULT "*) ;;
    *) continue ;;
  esac
  echo "===[$name] $(date +%H:%M:%S) START==="
  go test $pkgs -count=1 -timeout 30m > "$LOGDIR/$name.log" 2>&1
  rc=$?
  okn=$(grep -cE "^ok " "$LOGDIR/$name.log" || true)
  failn=$(grep -cP "^FAIL	" "$LOGDIR/$name.log" || true)
  echo "===[$name] rc=$rc ok=$okn failpkgs=$failn END==="
done
echo "===ALL DONE==="
