#!/usr/bin/env bash
# workflowOrchestrator 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #1124/#1125/#1126｜D-43 不存在工作流版本：DELETE/publish/archive 一律 404（不再假成功）
case_1124_delete_missing_version_404() {
  curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/workflows/versions/999999"
}
case_1125_publish_missing_version_404() {
  curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/workflows/versions/999999/publish"
}
case_1126_archive_missing_version_404() {
  curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/workflows/versions/999999/archive"
}

"$@"
