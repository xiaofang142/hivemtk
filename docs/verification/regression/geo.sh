#!/usr/bin/env bash
# geo 回归用例（台账 docs/verification/api-list.md；geoProbe 用例见 geoProbe.sh）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #431｜D-21 未配置推送平台 → 503 SERVICE_UNAVAILABLE_6003 + 原文案
case_431_push_urls_503() {
  curl -sS -m 30 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"urls":["https://example.com/k4-d21-regression"]}' \
    "$K4_BASE/api/geo/push/urls"
}

# 台账 #450｜D-22 pushers 平台白名单 + 重复平台友好文案
# 预期：bogus → 400 白名单文案；indexnow 建→重 → 400「平台 indexnow 已存在配置」；用后清理
case_450_pushers_whitelist() {
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"platform":"bogus","config_json":{"site":"https://k4d22.example.com","token":"abc"}}' \
    "$K4_BASE/api/geo/pushers"
  echo
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"platform":"indexnow","config_json":{"site":"https://k4d22.example.com","key":"k4key123"}}' \
    "$K4_BASE/api/geo/pushers")
  echo "$RESP"
  PID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"platform":"indexnow","config_json":{"site":"https://k4d22b.example.com","key":"k4key456"}}' \
    "$K4_BASE/api/geo/pushers"
  echo
  echo "assert: 第1行应 400 不支持的平台；第3行应 400 已存在配置"
  if [ -n "${PID:-}" ]; then
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/geo/pushers/$PID" >/dev/null
    echo "cleanup: 已删除临时 pusher $PID"
  fi
}

# 台账 #458｜D-23 空 keyword 不再静默生成（binding:required）
# 预期：400 INVALID_PARAM_1001 + Keyword required
case_458_content_generate_empty_keyword() {
  curl -sS -m 30 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"keyword":""}' "$K4_BASE/api/geo/content/generate"
}

"$@"
