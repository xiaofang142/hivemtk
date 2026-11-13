#!/usr/bin/env bash
# kbCrawler 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #623/#624/#625｜D-24 不存在爬取源：PUT/pause/resume 一律 404 NOT_FOUND_1002
case_623_put_missing_source_404() {
  curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"name":"x"}' "$K4_BASE/api/kb/crawler/sources/999999"
}
case_624_pause_missing_source_404() {
  curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/kb/crawler/sources/999999/pause"
}
case_625_resume_missing_source_404() {
  curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/kb/crawler/sources/999999/resume"
}

# 台账 #632/#633/#634｜D-25 不存在 QA 候选：approve/reject/publish 一律 404 NOT_FOUND_1002
case_632_approve_missing_404() {
  curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/kb/qa/candidates/999999/approve"
}
case_633_reject_missing_404() {
  curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/kb/qa/candidates/999999/reject"
}
case_634_publish_missing_404() {
  curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/kb/qa/candidates/999999/publish"
}

"$@"
