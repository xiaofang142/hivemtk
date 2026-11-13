#!/usr/bin/env bash
# glossary / i18nStats 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #543-548｜glossary 全生命周期（中文保真 + 软删复活）；用后清理
# 注意：中文 body 经 heredoc 文件传递，避免 Git Bash 管道编码损耗
case_543_glossary_lifecycle() {
  BODY=$(mktemp)
  printf '%s' '{"term_id":"k4-reg-gloss","category":"product","preserve":true,"translations":{"en":"k4 regression term","zh":"客户成功"},"pattern":"","status":"active"}' > "$BODY"
  echo "-- create（zh 应保真「客户成功」）"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    --data-binary @"$BODY" "$K4_BASE/api/glossaries"; echo
  echo "-- list";  curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/glossaries"; echo
  echo "-- detail"; curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/glossaries/k4-reg-gloss"; echo
  echo "-- update"; curl -sS -m 20 -X PUT -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"category":"ops","preserve":false,"translations":{"en":"k4 regression term v2","zh":"客户成功"}}' \
    "$K4_BASE/api/glossaries/k4-reg-gloss"; echo
  echo "-- validate（文本预览语义：{text,lang,term_id}）"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"text":"客户成功是我们唯一的术语","lang":"zh","term_id":"k4-reg-gloss"}' \
    "$K4_BASE/api/glossaries/validate"; echo
  echo "-- delete"; curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/glossaries/k4-reg-gloss"; echo
  echo "-- recreate after delete（应 200 复活，不得 409 dup）"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    --data-binary @"$BODY" "$K4_BASE/api/glossaries"; echo
  curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/glossaries/k4-reg-gloss" >/dev/null
  rm -f "$BODY"
  echo "cleanup: 临时词条已删除"
}

# 台账 #556-561｜i18n stats 六张报表（空库零值结构）
case_556_i18n_stats_all() {
  for p in "i18n/stats" "i18n/stats/lang-dist?days=7" "i18n/stats/cache?days=7" "i18n/stats/glossary" "i18n/stats/quality?days=7" "i18n/stats/latency?days=7"; do
    echo "-- $p"
    curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/$p"; echo
  done
}

"$@"
