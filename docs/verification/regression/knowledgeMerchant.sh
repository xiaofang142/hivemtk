#!/usr/bin/env bash
# knowledgeMerchant 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #673/#674｜D-30 不存在 chunk：PUT/DELETE 一律 404 NOT_FOUND_1002「分段不存在」
case_673_put_missing_chunk_404() {
  curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"content":"x"}' "$K4_BASE/api/knowledge-merchant/chunks/999999"
}
case_674_delete_missing_chunk_404() {
  curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/knowledge-merchant/chunks/999999"
}

# 台账 #675｜D-31 split 空白 parts 拒绝且不丢原分段（数据丢失级修复）
# 预期：建产品→batch/import(items)→chunk split [" ",""] → 400「拆分内容不能为空」
#       且原 chunk 仍在；合法 2 段 split 200 1 拆 2；用后清理
case_675_split_no_data_loss() {
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"product_id":"k4-d31-reg-prod","name":"K4 D31 regression product","category":"test"}' \
    "$K4_BASE/api/rag-config/products")
  PROD=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo "setup product: $PROD"
  [ -n "${PROD:-}" ] || { echo "setup 失败"; return 1; }
  IMP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d "{\"product_id\":\"$PROD\",\"items\":[{\"title\":\"K4 D31 regression\",\"content\":\"First paragraph for chunking. Second paragraph for chunking. Third paragraph for chunking.\"}]}" \
    "$K4_BASE/api/knowledge-merchant/batch/import")
  echo "$IMP"
  DOC=$(printf '%s' "$IMP" | python -X utf8 -I -c "import sys,json;ids=json.load(sys.stdin).get('data',{}).get('document_ids') or [];print(ids[0] if ids else '')" 2>/dev/null || true)
  echo
  CHUNKS=$(curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/knowledge-merchant/documents/$DOC/chunks?page=1&page_size=10")
  echo "$CHUNKS"
  CID=$(printf '%s' "$CHUNKS" | python -X utf8 -I -c "import sys,json;items=json.load(sys.stdin).get('data',{}).get('items') or [];print(items[0].get('id','') if items else '')" 2>/dev/null || true)
  echo
  echo "split 空白 parts（应 400 且原 chunk 保留）："
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"parts":[" ",""]}' "$K4_BASE/api/knowledge-merchant/chunks/$CID/split"
  echo
  echo "split 合法 2 段（应 200 1 拆 2）："
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"parts":["regression part one","regression part two"]}' \
    "$K4_BASE/api/knowledge-merchant/chunks/$CID/split"
  echo
  AFTER=$(curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/knowledge-merchant/documents/$DOC/chunks?page=1&page_size=10")
  printf '%s' "$AFTER" | python -X utf8 -I -c "import sys,json;d=json.load(sys.stdin);print('chunks now:', d.get('data',{}).get('total'))"
  for id in $(printf '%s' "$AFTER" | python -X utf8 -I -c "import sys,json;items=json.load(sys.stdin).get('data',{}).get('items') or [];print(' '.join(str(i.get('id')) for i in items))"); do
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/knowledge-merchant/chunks/$id" >/dev/null
  done
  curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/rag-config/products/$PROD" >/dev/null
  echo "cleanup: 已删除临时 chunks/product"
}

# 台账 #681｜D-32 external/import 鉴权先于参数校验
# 预期：无 token 空 body → 401 UNAUTHORIZED_2001（本用例故意不带鉴权）
case_681_external_import_auth_first() {
  K4_TOKEN="" bash -c 'curl -sS -m 20 -X POST -H "Content-Type: application/json" -d "{}" "$1/api/knowledge-merchant/external/import"' _ "$K4_BASE"
}

"$@"
