#!/usr/bin/env bash
# xianyuCard 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #1140｜D-44 redirect_url 清空后生成短链 → 400 ErrCardRedirectURLRequired（不再 200 假成功）
case_1140_generate_short_link_requires_redirect() {
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"title":"k4 d44 regression card","image_url":"https://example.com/k4xy.jpg","redirect_url":"https://example.com/xytarget"}' \
    "$K4_BASE/api/xianyu/create")
  echo "$RESP"
  XID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  echo
  if [ -n "${XID:-}" ]; then
    curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
      -d "{\"id\":$XID,\"title\":\"k4 d44 regression card\",\"image_url\":\"https://example.com/k4xy.jpg\",\"redirect_url\":\"\"}" \
      "$K4_BASE/api/xianyu/update" >/dev/null
    curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/xianyu/$XID/generate-short-link"
    echo
    echo "assert: HTTP 应 400 且文案含「卡片未配置跳转地址」"
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/xianyu/delete/$XID" >/dev/null
    echo "cleanup: 已删除临时卡 id=$XID"
  fi
}

"$@"
