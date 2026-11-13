#!/usr/bin/env bash
# unknownQuestion 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #1051/#1054｜D-39 不存在目标 review 一律 404 NOT_FOUND_1002
case_1051_review_missing_404() {
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"action":"approve"}' "$K4_BASE/api/unknown-questions/999999/review"
}
case_1054_cluster_review_missing_404() {
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"action":"approve"}' "$K4_BASE/api/unknown-questions/clusters/999999/review"
}

"$@"
