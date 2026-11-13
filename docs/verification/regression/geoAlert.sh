#!/usr/bin/env bash
# geoAlert / geo 报表与配置 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #477-480｜alerts 全生命周期：list→unread→ack→unread→delete→重复删 404
# 注意：ack/delete 会消费列表里最旧的一条告警（运行时衍生的 ops 数据，非结构化种子）
case_477_alerts_lifecycle() {
  echo "-- list";        curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/alerts?page=1&page_size=5"; echo
  echo "-- unread-count"; curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/alerts/unread-count"; echo
  ID=$(curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/alerts?page=1&page_size=1" \
    | python -X utf8 -I -c "import sys,json;items=json.load(sys.stdin).get('data',{}).get('list') or [];print(items[0].get('id','') if items else '')" 2>/dev/null || true)
  echo "-- ack id=$ID（应 200 已确认）"
  [ -n "$ID" ] && curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' -d '{}' "$K4_BASE/api/geo/alerts/$ID/ack"; echo
  echo "-- unread-count（应减 1）"
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/alerts/unread-count"; echo
  echo "-- delete id=$ID（应 200）"
  [ -n "$ID" ] && curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/alerts/$ID"; echo
  echo "-- repeat delete（应 404 告警不存在）"
  [ -n "$ID" ] && curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/alerts/$ID"; echo
}

# 台账 #470-473｜reports 四张报表
case_470_reports_all() {
  echo "-- summary";       curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/reports/summary?days=30"; echo
  echo "-- roi";           curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/reports/roi?days=30"; echo
  echo "-- api-costs";     curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/reports/api-costs?days=30"; echo
  echo "-- content-format"; curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/reports/content-format?days=30"; echo
}

# 台账 #474/#475｜config GET + 按前端契约形状 PUT（advantages 字符串、competitors 数组）
case_475_config_roundtrip() {
  echo "-- GET"; curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/config"; echo
  echo "-- PUT（HiveMTK 幂等回写）"
  curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"brand_name":"HiveMTK","brand_description":"私域 AI 客服与营销自动化平台","domain":"https://example.com","advantages":"私域部署、多渠道接入、知识库 RAG","competitors":["竞品A"],"negative_keywords":[],"default_model":"","verify_models":[]}' \
    "$K4_BASE/api/geo/config"
  echo
  echo "assert: PUT 应 200 更新配置成功；GET 复核 brand/advantages/competitors 一致"
}

# 台账 #460-464｜内容五件套（LLM 未启走启发式降级，均为 200 code=0）
case_460_content_ops_fallback() {
  echo "-- score"
  curl -sS -m 40 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"title":"K4 GEO scoring test","content":"Body about GEO optimization. GEO optimization matters. GEO optimization."}' \
    "$K4_BASE/api/geo/content/score"; echo
  echo "-- optimize"
  curl -sS -m 40 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"title":"K4 GEO opt","content":"Body text about GEO optimization for optimize test."}' \
    "$K4_BASE/api/geo/content/optimize"; echo
  echo "-- eeat"
  curl -sS -m 40 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"title":"K4 GEO eeat","content":"Body text for E-E-A-T enhancement test."}' \
    "$K4_BASE/api/geo/content/eeat"; echo
  echo "-- schema"
  curl -sS -m 40 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"title":"K4 GEO schema","content":"Body text for schema generation test."}' \
    "$K4_BASE/api/geo/content/schema"; echo
  echo "-- uniqueness"
  curl -sS -m 40 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"title":"K4 GEO uniqueness","content":"Body text for uniqueness check test."}' \
    "$K4_BASE/api/geo/content/uniqueness"; echo
}

"$@"
