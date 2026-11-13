#!/usr/bin/env bash
# system 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #856｜D-35 不存在 platform-account 删除 → 404（RowsAffected 守卫，不再假成功）
case_856_delete_missing_404() {
  curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/platform-accounts/999999"
}

# 台账 #980｜D-978 部分保存不清零布尔开关（FillUnsetFromExisting）
# 预期：POST 只传 site_name+theme_color → 200；GET 复核 enable_register/email_marketing/rag 保持 true
# 注意：本用例会改 site_name，跑完请回填原值（local dev 值）
case_980_partial_save_preserves_booleans() {
  echo "-- before"
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/system/config" \
    | python -X utf8 -I -c "import sys,json;d=json.load(sys.stdin)['data'];print({k:d.get(k) for k in ('site_name','enable_register','enable_email_marketing','enable_rag','maintenance_mode','max_upload_size_mb')})"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"site_name":"k4-d978-regression","theme_color":"#409EFF"}' \
    "$K4_BASE/api/system/config"
  echo
  echo "-- after"
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/system/config" \
    | python -X utf8 -I -c "import sys,json;d=json.load(sys.stdin)['data'];print({k:d.get(k) for k in ('site_name','enable_register','enable_email_marketing','enable_rag','maintenance_mode','max_upload_size_mb')})"
  echo "assert: after 中三个 enable_* 均为 True（缺键不清零）；site_name 被改为回归值属预期"
}

"$@"
