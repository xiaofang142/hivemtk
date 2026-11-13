#!/usr/bin/env bash
# geoProbe 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #537｜D-23d PUT competitors 不传 domain 保持原域名（部分更新语义，competitor.go:141）
# 预期：临时竞品 PUT {name,notes} → 200 且 domain 保持原值；用后清理
case_537_competitor_partial_update() {
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"domain":"k4d23d.example.com","name":"K4 temp competitor","category":"blog","status":"active"}' \
    "$K4_BASE/api/geo/competitors")
  echo "$RESP"
  CID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  if [ -n "${CID:-}" ]; then
    curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
      -d '{"name":"K4 temp competitor-upd","notes":"k4 regression partial update"}' \
      "$K4_BASE/api/geo/competitors/$CID"
    echo
    echo "assert: data.domain 应保持 k4d23d.example.com（category/priority/status 未传被清空=全量覆盖设计，前端回填）"
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/competitors/$CID" >/dev/null
    echo "cleanup: 已删除临时竞品 id=$CID"
  fi
}

# 台账 #521-533｜probe 读路径 + 动作路径（LLM/引擎未配置时的门与降级）
case_521_probe_readonly() {
  echo "-- engines";      curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/probe/engines"; echo
  echo "-- engines/meta"; curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/probe/engines/meta"; echo
  echo "-- runs";         curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/probe/runs?engine=&limit=5"; echo
  echo "-- sov";          curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/sov"; echo
  echo "-- crawler-stats"; curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/crawler-stats"; echo
  echo "-- source-catalog/levels"; curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/source-catalog/levels?url=https://example.com/doc"; echo
}

case_523_probe_import() {
  curl -sS -m 30 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"rows":[{"engine":"openai","query":"HiveMTK","response":"HiveMTK mentioned positively","brand":"HiveMTK"}]}' \
    "$K4_BASE/api/geo/probe/import"
}

case_524_probe_test_engine_gate() {
  # 引擎未配置时应 400「engine default not found (available: ...)」
  curl -sS -m 60 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"engine":"default","query":"HiveMTK"}' "$K4_BASE/api/geo/probe/test"
}

case_532_inaccurate_claims_llm_gate() {
  # LLM 未配置时应 503 SERVICE_UNAVAILABLE_6003 + 模型供应商引导
  curl -sS -m 40 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"brand_name":"HiveMTK"}' "$K4_BASE/api/geo/inaccurate-claims"
}

# 台账 #515-519｜platform platforms/accounts CRUD + publish 业务门；用后清理
case_515_platform_lifecycle() {
  echo "-- platforms"; curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/platform/platforms"; echo
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"platform":"github_readme","name":"k4-pa-reg","credentials":{"token":"k4faketoken"}}' \
    "$K4_BASE/api/geo/platform/accounts")
  echo "$RESP"
  PAID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  echo "-- publish 不存在文章（应 BUSINESS_5001 文章不存在）"
  curl -sS -m 30 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"article_id":"no-such-article","platform":"github_readme"}' "$K4_BASE/api/geo/platform/publish"
  echo
  if [ -n "${PAID:-}" ]; then
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/platform/accounts/$PAID" >/dev/null
    echo "cleanup: 临时平台账号已删除"
  fi
}

# 台账 #506-514｜geo workflow CRUD 全链 + 模板；用后清理
case_508_workflow_crud() {
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"name":"k4-wf-reg","description":"k4 regression","steps":[{"name":"s1","type":"noop"}]}' \
    "$K4_BASE/api/geo/workflow/workflows")
  echo "$RESP"
  WID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  if [ -n "${WID:-}" ]; then
    curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
      -d '{"name":"k4-wf-reg-upd","steps":[{"name":"s1","type":"noop"},{"name":"s2","type":"noop"}],"enabled":false}' \
      "$K4_BASE/api/geo/workflow/workflows/$WID"; echo
    curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/workflow/workflows/$WID/executions"; echo
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/workflow/workflows/$WID"; echo
    echo "-- repeat DELETE（应 404）"
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/workflow/workflows/$WID"; echo
  fi
  echo "-- templates list"
  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/geo/workflow/templates"; echo
}

"$@"
