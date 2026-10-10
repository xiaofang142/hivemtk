#!/usr/bin/env bash
# 给 scripts/check-cleared-fake-assets.sh 做反向对账：证明它真的有牙，而不是"绿着但一条都不判"。
#
# 三档：好夹具必须 0、注回一个谎报形状必须 1、检查对象缺失必须 2。
# 少了第三档，"文件被改名/删掉"会读成通过；少了第二档，一条写坏的正则能一直绿。
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
GATE="${HERE}/check-cleared-fake-assets.sh"
FIX="$(mktemp -d "${TMPDIR:-/tmp}/cleared-fake-assets-fixtures.XXXXXX")"
trap 'rm -rf "$FIX"' EXIT

SERVER="${FIX}/user-server/internal"
mkdir -p "${SERVER}/service" "${SERVER}/bridge" "${SERVER}/router" "${SERVER}/app" \
  "${SERVER}/repository" "${SERVER}/model" \
  "${SERVER}/aiagent/agent/tooluse" "${SERVER}/aiagent/knowledge/repository" \
  "${FIX}/user-server/internal/dto"

# 只在"必须存在"那几条里写内容，其余给空文件：空文件不含任何被禁符号。
printf '%s\n' '没有装配真实发送器' > "${SERVER}/service/reach_pipeline_dispatch.go"
printf '%s\n' 'SetReachSender(app.NewPipelineReachSender(db))' > "${SERVER}/router/service_routes.go"
printf '%s\n' 'NewSubflowNodeExecutor(orch)' > "${SERVER}/service/workflow_node_executor.go"
printf '%s\n' 'attachFAQAnswerCache(o, gormDB)' > "${SERVER}/app/sales_engine_factory.go"
printf '%s\n' 'faq_answer_enabled' > "${SERVER}/app/faq_cache_wiring.go"
printf '%s\n' '没有任何写入路径维护' > "${SERVER}/model/knowledge_base.go"
: > "${SERVER}/service/smart_cs_orchestrator.go"
: > "${SERVER}/service/reach_pipeline.go"
: > "${SERVER}/bridge/reach_adapter.go"
: > "${SERVER}/router/router.go"
: > "${SERVER}/repository/faq_entry.go"
: > "${SERVER}/repository/sop_template.go"
: > "${SERVER}/aiagent/agent/tooluse/private_message_tools.go"
: > "${SERVER}/aiagent/knowledge/repository/knowledge_document.go"

rc_of() {
  local expect_label="$1"; shift
  local out rc=0
  out="$("$@" 2>&1)" || rc=$?
  LAST_OUT="$out"
  LAST_RC=$rc
  printf '%s\n' "$out" | grep -E "^(FAIL|BROKEN)" | head -5
  printf '%s\n' "$out" | grep -E "^读数"
  printf '  -> rc=%s（%s）\n' "$rc" "$expect_label"
}

verdict=0
check() {
  local want="$1" label="$2"
  if [ "$LAST_RC" != "$want" ]; then
    printf 'MISMATCH %s：期望 rc=%s，实得 rc=%s\n' "$label" "$want" "$LAST_RC"
    verdict=1
  else
    printf 'MATCH    %s（rc=%s）\n' "$label" "$want"
  fi
}

echo "── 档1：全部判据成立 ──"
rc_of "期望 0" bash "$GATE" --repo "$FIX"
check 0 "好夹具"

echo "── 档2：把一个已删的谎报符号注回去 ──"
printf '%s\n' 'var globalFAQCache *FAQAnswerCacheService' >> "${SERVER}/service/smart_cs_orchestrator.go"
rc_of "期望 1" bash "$GATE" --repo "$FIX"
check 1 "注回 globalFAQCache"
printf '%s\n' "$LAST_OUT" | grep -q "globalFAQCache" || {
  printf 'MISMATCH 违规行没点名符号\n'; verdict=1;
}

echo "── 档3：检查对象本身缺失 ──"
perl -pi -e 's/^var globalFAQCache.*$//' "${SERVER}/service/smart_cs_orchestrator.go"
rm -f "${SERVER}/service/reach_pipeline_dispatch.go"
rc_of "期望 2" bash "$GATE" --repo "$FIX"
check 2 "缺被检查文件"
printf '%s\n' "$LAST_OUT" | grep -q "没有判定资格" || {
  printf 'MISMATCH 缺失档没有出声\n'; verdict=1;
}

echo "── 对真仓库跑一遍（档1 的现实版本）──"
rc_of "期望 0" bash "$GATE" --repo "$(dirname "$HERE")"
check 0 "真仓库"

if [ "$verdict" != 0 ]; then
  echo "❌ 反向对账未通过"
  exit 1
fi
echo "✅ 三档 + 真仓库全部符合预期"
