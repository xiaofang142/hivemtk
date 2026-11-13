#!/usr/bin/env bash
# assetBundle 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #48｜D-48 平台集成门（rejectPlatformDisabled）
# 预期：403 {"code":"FORBIDDEN_2002"} + 开通指引文案（PLATFORM_ENABLED=off 时设计内拒绝）
case_48_submit_platform_disabled() {
  curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/asset-bundle/999999/submit-platform"
}

"$@"
