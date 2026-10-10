#!/usr/bin/env bash
# =============================================================================
# check-tenant-scope.sh — 多租户隔离守卫（P0-9，2026-10-11）
#
# 红线：核心业务表（customers/inbox_conversations/message_hub/clues/
# opportunities/rag_products/kb_documents）的查询必须经过 scope.TeamScoped。
# 新增裸查询（基线外的行）会让跨团队数据漏给普通用户——这是 P0 要堵死的口子。
#
# 判据：含 `.Model(&model.<租户表>{})` 的行，同行或随后 2 行内必须出现
# TeamScoped（链式调用可跨行）；基线（.baseline）里登记的既有豁免点不拦，
# 但基线只减不增要靠 review——豁免点必须写明"为什么可以裸查"。
#
# 用法:
#   bash scripts/check-tenant-scope.sh            # 校验
#   UPDATE_TENANT_BASELINE=1 bash scripts/...     # 重新生成基线（diff 后入库）
# =============================================================================
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET=""
for _cand in "$SCRIPT_DIR/../user-server" "$SCRIPT_DIR/../../hivemtk/user-server"; do
  if [ -d "$_cand/internal" ]; then
    TARGET="$(cd "$_cand" && pwd)"
    break
  fi
done
TARGET="${1:-$TARGET}"
[ -n "$TARGET" ] || TARGET="$PROJECT_ROOT/user-server"
BASELINE="$SCRIPT_DIR/tenant-scope.baseline"

RED='\033[0;31m'; GREEN='\033[0;32m'; NC='\033[0m'
ERRORS=0

# 租户模型（多租户 P0 清单）：新增租户表在这里登记
PATTERN='\.Model\(&(model|sysmodel)\.(Customer|InboxConversation|MessageHub|Clue|Opportunity|RagProduct|KBDocument)\{\}\)'

current=$(grep -rnE "$PATTERN" "$TARGET/internal" --include="*.go" \
  | grep -v "_test.go" \
  | awk -F: '{file=$1; line=$2; ctx=$0;
      ok=0;
      if (ctx ~ /TeamScoped/) ok=1;
      else {
        # 随后 2 行内出现 TeamScoped 也算链式合规
        if ((getline nxt1 < file) > 0) { if (nxt1 ~ /TeamScoped/) ok=1;
          else if ((getline nxt2 < file) > 0) { if (nxt2 ~ /TeamScoped/) ok=1 } }
      }
      if (!ok) print file ":" line }' || true)

# 说明：上面 awk 对同一文件连续读行有游标问题，改用更稳的两遍法重算
current=$(grep -rnE "$PATTERN" "$TARGET/internal" --include="*.go" | grep -v "_test.go" | cut -d: -f1-2 | sort -u)
bare=""
while IFS= read -r loc; do
  [ -z "$loc" ] && continue
  f="${loc%%:*}"; ln="${loc##*:}"
  window=$(sed -n "${ln},$((ln+2))p" "$f")
  if ! echo "$window" | grep -q "TeamScoped"; then
    bare="$bare $loc"
  fi
done <<EOF
$current
EOF

bare_count=$(echo $bare | wc -w)

if [ "${UPDATE_TENANT_BASELINE:-0}" = "1" ]; then
  {
    echo "# tenant-scope.baseline — 多租户裸查询豁免点（check-tenant-scope.sh 生成）"
    echo "# 每行 <TARGET 相对路径>:count；豁免必须在代码注释里写明理由（系统上下文直通/平台管线）"
    echo "$bare" | tr ' ' '\n' | grep -v '^$' | cut -d: -f1 | sed "s#^$TARGET/##;s#^user-server/##" | sort | uniq -c | awk '{print $2":"$1}'
  } > "$BASELINE"
  echo -e "${GREEN}✅ 基线已更新: $BASELINE（$(echo $bare | wc -w) 个豁免点）${NC}"
  exit 0
fi

# 与基线对账：每个文件的裸查询数不得超过基线登记数
fail=0
declared=$(grep -v '^#' "$BASELINE" 2>/dev/null | grep -v '^$' || true)
while IFS= read -r loc; do
  [ -z "$loc" ] && continue
  f="${loc%%:*}"
  rel="${f#"$TARGET/"}"
  rel="${rel#user-server/}"
  allow=$(echo "$declared" | grep "^$rel:" | head -1 | cut -d: -f2)
  allow=${allow:-0}
  got=$(echo "$bare" | tr ' ' '\n' | grep -c "^$f:" || true)
  if [ "$got" -gt "$allow" ]; then
    echo -e "${RED}❌ $rel 出现 $got 处裸查询（基线允许 $allow）——核心表查询必须走 scope.TeamScoped(ctx)${NC}" >&2
    echo "$bare" | tr ' ' '\n' | grep "^$f:" | sed "s/^/    /" >&2
    fail=1
  fi
done <<EOF
$(echo "$bare" | tr ' ' '\n' | grep -v '^$' | cut -d: -f1 | sort -u)
EOF

if [ "$fail" = "0" ]; then
  echo -e "${GREEN}✅ 租户隔离守卫通过（裸查询 $bare_count 处，均在基线内）${NC}"
fi
exit $fail
