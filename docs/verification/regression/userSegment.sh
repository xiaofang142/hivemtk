#!/usr/bin/env bash
# userSegment 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #1058｜D-40 RFM rule 空 name 可存（不再 400 required）+ 不存在 id 404；用后清理
case_1058_rfm_rule_empty_name_ok() {
  echo "-- 不存在 id"
  curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"name":"x"}' "$K4_BASE/api/user-segment/rfm/rule/999999"
  echo
  echo "-- setup 临时规则"
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"name":"k4-d40-temp-rule","is_active":true}' "$K4_BASE/api/user-segment/rfm/rule")
  echo "$RESP"
  RID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  echo "-- PUT 空 name（应 200 且落库空串）"
  curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"name":"","is_active":true}' "$K4_BASE/api/user-segment/rfm/rule/$RID"
  echo
  if [ -n "${RID:-}" ]; then
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/user-segment/rfm/rule/$RID" >/dev/null
    echo "cleanup: 已删除临时规则 id=$RID"
  fi
}

"$@"
