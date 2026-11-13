#!/usr/bin/env bash
# tuning 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #1045｜D-38 候选状态白名单 + 不存在 id 404
# 预期：status=aproved（拼错）→ 400 白名单文案；valid status + 999999 → 404 NOT_FOUND_1002
case_1045_status_whitelist_and_404() {
  echo "-- bogus status"
  curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"status":"aproved"}' "$K4_BASE/api/admin/tuning/prompt/candidates/999999/status"
  echo
  echo "-- valid status, missing id"
  curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"status":"approved"}' "$K4_BASE/api/admin/tuning/prompt/candidates/999999/status"
  echo
}

"$@"
