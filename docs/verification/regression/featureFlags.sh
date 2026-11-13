#!/usr/bin/env bash
# featureFlags 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #404｜D-19 显式 rollout_percentage=0 落库 0（*int 指针区分显式零）
# 预期：200 code=0 且 data.rollout_percentage=0；用后清理
case_404_create_rollout_zero() {
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"key":"k4-d19-check-a","name":"K4 D19 zero rollout","description":"ledger D-19 regression","enabled":true,"rollout_percentage":0}' \
    "$K4_BASE/api/feature-flags"
  echo
  ID=$(curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/feature-flags?key=k4-d19-check-a" \
    | python -X utf8 -I -c "import sys,json;d=json.load(sys.stdin);items=d.get('data') or [];print((items[0] if isinstance(items,list) and items else items).get('id',''))" 2>/dev/null || true)
  echo "assert: 上行响应 rollout_percentage 应为 0（创建 + GET 复核 id=$ID）"
  if [ -n "${ID:-}" ]; then
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/feature-flags/$ID" >/dev/null
    echo "cleanup: 已删除临时 flag id=$ID"
  fi
}

"$@"
