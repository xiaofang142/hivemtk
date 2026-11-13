#!/usr/bin/env bash
# kuaishouCard 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #686｜D-33 只改 title 不重建短链（已分发链接不失效）
# 预期：临时卡 create（带 short_code）→ PUT 只改 title → short_code 保持 → 删除
case_686_update_keeps_short_code() {
  RESP=$(curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"title":"k4 d33 regression card","image_url":"https://example.com/k4.jpg","redirect_url":"https://example.com/target"}' \
    "$K4_BASE/api/kuaishou/create")
  echo "$RESP"
  ID=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('id',''))" 2>/dev/null || true)
  CODE=$(printf '%s' "$RESP" | python -X utf8 -I -c "import sys,json;print(json.load(sys.stdin).get('data',{}).get('short_code',''))" 2>/dev/null || true)
  echo
  if [ -n "${ID:-}" ]; then
    curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
      -d "{\"id\":$ID,\"title\":\"k4 d33 title only update\",\"redirect_url\":\"https://example.com/target\"}" \
      "$K4_BASE/api/kuaishou/update"
    echo
    echo "assert: short_code 应保持 $CODE（不重建短链）"
    curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/kuaishou/delete/$ID" >/dev/null
    echo "cleanup: 已删除临时卡 id=$ID"
  fi
}

"$@"
