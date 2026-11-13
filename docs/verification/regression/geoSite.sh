#!/usr/bin/env bash
# geo site 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #440/#441/#442｜无启用站点时 export/deploy/full-pipeline
# 预期（2026-11-13 修复后）：400 INVALID_PARAM_1001 +「站点配置不完整： 尚未启用静态站…」
# （export 与 full-pipeline 的 export 步骤本轮从 500 对齐为 400，ErrSiteConfigInvalid 门）
case_440_export_no_site_400() {
  curl -sS -m 30 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{}' "$K4_BASE/api/geo/site/export"
}
case_441_deploy_no_site_400() {
  curl -sS -m 30 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{}' "$K4_BASE/api/geo/site/deploy"
}
case_442_full_pipeline_no_site_400() {
  curl -sS -m 30 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{}' "$K4_BASE/api/geo/site/full-pipeline"
}

# 台账 #444/#445｜robots-preview 内容 + sites 空列表
case_444_robots_preview() {
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/site/robots-preview"
}
case_445_sites_list() {
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/sites?page=1&page_size=5"
}

# 台账 #446/#447/#448｜sites CRUD 全链（含重复删 404）；用后清理
case_446_crud_sites() {
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"domain":"k4site.example.com","provider":"cloudflare","hugo_path":"","git_repo":"","active":false}' \
    "$K4_BASE/api/geo/sites")
  echo "$RESP"
  SID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  if [ -n "${SID:-}" ]; then
    curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
      -d '{"domain":"k4site.example.com","provider":"vercel","active":false}' \
      "$K4_BASE/api/geo/sites/$SID"
    echo
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/sites/$SID"
    echo
    echo "-- repeat DELETE（应 404 站点配置不存在）"
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/sites/$SID"
    echo
  fi
}

"$@"
