#!/usr/bin/env bash
# feishu 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #417｜D-20 假凭证 test-send 走 502 UPSTREAM_FAILURE_5005（ErrorFromDB→IsUpstreamFailure）
# 预期：创建临时假凭证账号 → POST test-send 502 + UPSTREAM_FAILURE_5005 → 删除临时账号
case_417_test_send_upstream_502() {
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"account_name":"k4-d20-temp","app_id":"cli_k4fake123","app_secret":"k4fake_secret"}' \
    "$K4_BASE/api/feishu/accounts")
  echo "$RESP"
  AID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  if [ -n "${AID:-}" ]; then
    curl -sS -m 30 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
      -d '{"open_id":"ou_k4test","content":"k4 d20 regression"}' \
      "$K4_BASE/api/feishu/accounts/$AID/test-send"
    echo
    echo "assert: HTTP 应 502 且 code=UPSTREAM_FAILURE_5005"
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/feishu/accounts/$AID" >/dev/null
    echo "cleanup: 已删除临时账号 id=$AID"
  fi
}

"$@"
