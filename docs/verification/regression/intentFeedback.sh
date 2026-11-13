#!/usr/bin/env bash
# intentRecognition 回归用例（台账 docs/verification/api-list.md；TB-09 意图纠错反馈）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #604/#605｜correct 校验链（缺参/未知意图/合法热生效）+ override 读写一致；用后复原
# 注意：correct 会写意图覆盖词面（热生效），用例末尾把覆盖表清空复原
case_604_intent_feedback_cycle() {
  echo "-- 缺参（应 400 log_id 与 corrected_type 为必填）"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"message":"x"}' "$K4_BASE/api/intent/feedback/correct"; echo
  echo "-- 未知意图（应 400 不是已知意图）"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"log_id":21,"corrected_type":"kb_search","operator":"admin"}' \
    "$K4_BASE/api/intent/feedback/correct"; echo
  LOGID=$(curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/intent-records?page=1&page_size=1" \
    | python -X utf8 -I -c "import sys,json;items=json.load(sys.stdin).get('data',{}).get('list') or [];print(items[0].get('id','') if items else '')" 2>/dev/null || true)
  echo "-- 合法纠错 log_id=$LOGID → price_inquiry（应 200 热生效）"
  [ -n "$LOGID" ] && curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d "{\"log_id\":$LOGID,\"corrected_type\":\"price_inquiry\",\"operator\":\"admin\"}" \
    "$K4_BASE/api/intent/feedback/correct"; echo
  echo "-- override 读取面（应含 price_inquiry 覆盖）"
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/intent/feedback/override"; echo
  echo "-- 复原：清空覆盖词表"
  curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{}' "$K4_BASE/api/intent-records/keywords-override"; echo
  echo "-- override 终态（应 data:{}）"
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/intent/feedback/override"; echo
}

# 台账 #727/#62｜PLT-01 platform-usage 与 AM-02 credits（平台门）
case_727_platform_usage() {
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/llm/platform-usage"
}
case_62_asset_market_credits_gate() {
  # PLATFORM_ENABLED=off 时应 403 FORBIDDEN_2002 平台门
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/v1/asset-market/credits"
}

"$@"
