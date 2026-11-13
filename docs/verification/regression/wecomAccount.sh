#!/usr/bin/env bash
# wecomAccount 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #1085｜D-42 上游投递失败（errcode=40013）回 502 UPSTREAM_FAILURE_5005 而非 400
# 预期：临时假凭据账号 → send → 502 + UPSTREAM_FAILURE_5005 + 上游 errcode 原文 → 删除临时账号
# 注意：本用例真实出网到企微上游（qyapi.weixin.qq.com）
case_1085_send_upstream_502() {
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"name":"k4-d42-temp","corp_id":"k4fakecorpid","corp_secret":"k4fakesecret","agent_id":1000002,"agent_secret":"k4agentsecret"}' \
    "$K4_BASE/api/wecom/accounts")
  echo "$RESP"
  AID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  if [ -n "${AID:-}" ]; then
    curl -sS -m 40 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
      -d "{\"account_id\":$AID,\"external_user_id\":\"k4fakeuser\",\"msg_type\":\"text\",\"content\":\"k4 d42 regression send\"}" \
      "$K4_BASE/api/wecom/messages/send"
    echo
    echo "assert: HTTP 应 502 且 code=UPSTREAM_FAILURE_5005、message 含 errcode=40013"
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/wecom/accounts/$AID" >/dev/null
    echo "cleanup: 已删除临时账号 id=$AID"
  fi
}

"$@"
