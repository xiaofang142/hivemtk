#!/usr/bin/env bash
# geoEntity 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #501｜source_level 白名单：非法值 400 + 文案透传（不再 500/脱敏）；合法值 200；用后清理
case_501_kb_doc_source_level() {
  echo "-- 非法 source_level=official（应 400 +「source_level 只支持 A/B/C/D」）"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"title":"k4 sl test","content":"body","source_level":"official"}' \
    "$K4_BASE/api/geo/kb/documents"
  echo
  echo "-- 合法 source_level=B（应 200）"
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"title":"K4 kb regression doc","content":"HiveMTK regression body for kb doc.","source_level":"B"}' \
    "$K4_BASE/api/geo/kb/documents")
  echo "$RESP"
  DID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  if [ -n "${DID:-}" ]; then
    echo "-- GET detail（应 200）"
    curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/kb/documents/$DID"; echo
    echo "-- DELETE（应 200）+ 重复删（应 404）"
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/kb/documents/$DID"; echo
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/kb/documents/$DID"; echo
    echo "cleanup: 临时文档已删除"
  fi
}

# 台账 #498/#499｜entity 空图降级 + extract（LLM 依赖，未启时诚实降级报错）
case_498_entity_graph_empty() {
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/entity/999999/graph"
}

"$@"
