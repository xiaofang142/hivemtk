#!/usr/bin/env bash
# knowledge 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #645｜D-26 不存在产品 rebuild-index → 404 rag product not found（不再假成功）
case_645_rebuild_index_missing_404() {
  curl -sS -m 20 -X POST -H "$(auth)" \
    "$K4_BASE/api/knowledge/products/no-such-product-k4/rebuild-index"
}

# 台账 #647｜D-27 search 契约（跨产品泄漏复现需 embedding，本地未启时优雅空结果）
# 预期：200 code=0 结构完整 items/total/latency_ms
case_647_search_contract() {
  curl -sS -m 30 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"query":"test","product_id":"k4-d27-prod","top_k":5}' \
    "$K4_BASE/api/knowledge/search"
}

# 台账 #648/#660｜D-29 导入成功/失败都写 import-logs（导入 23502 由 v3.92.0 修复）
# 预期：建产品→导入 200 document_id→import-logs 有 success 记录→stats/imports 反映→清理
case_648_import_logs_written() {
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"product_id":"k4-d29-reg-prod","name":"K4 D29 regression product","category":"test"}' \
    "$K4_BASE/api/rag-config/products")
  echo "$RESP"
  PROD=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  [ -n "${PROD:-}" ] || { echo "setup 失败：产品未创建"; return 1; }
  IMP=$(curl -sS -m 30 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d "{\"product_id\":\"$PROD\",\"title\":\"K4 D29 regression doc\",\"content\":\"k4 d29 regression import body.\",\"category\":\"test\"}" \
    "$K4_BASE/api/knowledge/import/text")
  echo "$IMP"
  DOC=$(printf '%s' "$IMP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('document_id',''))" 2>/dev/null || true)
  echo
  echo "assert: 导入应 200 code=0（v3.92.0 filename 放宽后不再 23502）"
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/knowledge/import-logs?page=1&page_size=3"
  echo
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/knowledge/stats/imports"
  echo
  echo "assert: import-logs 应含 status=success 且 document_id=$DOC"
  [ -n "${DOC:-}" ] && curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/knowledge/documents/$DOC" >/dev/null
  curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/rag-config/products/$PROD" >/dev/null
  echo "cleanup: 已删除临时 doc/product"
}

# 台账 #653｜D-28 openapi source 越权删除 404 + 保留；正确删 200；重复删 404
case_653_openapi_delete_authz() {
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"product_id":"k4-d28-reg-prod","name":"k4-d28-src","endpoint":"https://k4d28.example.com/api","method":"GET"}' \
    "$K4_BASE/api/knowledge/openapi/sources")
  echo "$RESP"
  SID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  if [ -n "${SID:-}" ]; then
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/knowledge/openapi/sources/$SID?product_id=other-product-xxx"
    echo
    echo "assert: 越权删应 404 NOT_FOUND_1002 且 GET 可取回（数据保留）"
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/knowledge/openapi/sources/$SID?product_id=k4-d28-reg-prod"
    echo
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/knowledge/openapi/sources/$SID?product_id=k4-d28-reg-prod"
    echo
    echo "assert: 正确删 200、重复删 404"
  fi
}

"$@"
