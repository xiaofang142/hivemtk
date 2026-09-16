#!/usr/bin/env bash
# =============================================================================
# feature-doc-coverage-report.sh
# marketing-features/*.md 八节模板覆盖情况统计（OPT-DOC-14）
#
# 对应文档：[docs/standards/FEATURE_DOCUMENTATION_TEMPLATE.md]
# 目标：输出一份覆盖情况表，统计每篇文档的 §一 ~ §八 覆盖情况 + 完成度
#
# 用法：
#   bash scripts/feature-doc-coverage-report.sh
#   bash scripts/feature-doc-coverage-report.sh --json
#   bash scripts/feature-doc-coverage-report.sh --out report.md
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

DOCS_DIR="$PROJECT_ROOT/hivemtk/docs/marketing-features"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# 八节标题取自单一真源（与 FEATURE_DOCUMENTATION_TEMPLATE.md 对齐）
# shellcheck source=lib/feature-doc-sections.sh
source "$SCRIPT_DIR/lib/feature-doc-sections.sh"
SECTIONS=( "${FD_SECTIONS[@]}" )
SHORT_NAMES=( "${FD_SHORT[@]}" )

MODE="table"
OUT_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --json) MODE="json"; shift ;;
    --out) OUT_FILE="$2"; shift 2 ;;
    --help|-h)
      echo "Usage: $0 [--json] [--out FILE]"
      exit 0
      ;;
    *) echo "Unknown arg: $1" >&2; exit 1 ;;
  esac
done

if [ ! -d "$DOCS_DIR" ]; then
  echo -e "${RED}❌ 目录不存在: $DOCS_DIR${NC}" >&2
  exit 1
fi

# 用临时文件存储中间结果
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

TOTAL_DOCS=0
TOTAL_SLOTS=0
TOTAL_FILLED=0
REQUIRED_FILLED=0
OPTIONAL_FILLED=0
DETAIL_TSV="$TMP_DIR/detail.tsv"
> "$DETAIL_TSV"

# 一次性扫描所有文档，统计覆盖
for f in "$DOCS_DIR"/*.md; do
  filename=$(basename "$f")

  case "$filename" in
    README.md|DEPRECATED_*) continue ;;
  esac

  TOTAL_DOCS=$((TOTAL_DOCS+1))
  filled=0
  cov=""

  for i in "${!SECTIONS[@]}"; do
    section="${SECTIONS[$i]}"
    # 旧实现允许「任意一节的数字前缀」+ 本节名 命中（如 `## 六、数据模型` 被算作 §五），
    # 会虚高覆盖率。改用 lib 的严格判定：序号必须与节序一致。
    if fd_section_present "$f" "$i"; then
      cov+="1"
      filled=$((filled+1))
      TOTAL_FILLED=$((TOTAL_FILLED+1))
      if [ "$i" -le 5 ]; then
        REQUIRED_FILLED=$((REQUIRED_FILLED+1))
      else
        OPTIONAL_FILLED=$((OPTIONAL_FILLED+1))
      fi
    else
      cov+="0"
    fi
    TOTAL_SLOTS=$((TOTAL_SLOTS+1))
  done

  printf "%s\t%s\t%d\n" "$filename" "$cov" "$filled" >> "$DETAIL_TSV"
done

# JSON 输出模式
if [ "$MODE" = "json" ]; then
  cat <<EOF
{
  "docs_dir": "$DOCS_DIR",
  "total_docs": $TOTAL_DOCS,
  "total_slots": $TOTAL_SLOTS,
  "total_filled": $TOTAL_FILLED,
  "required_filled": $REQUIRED_FILLED,
  "optional_filled": $OPTIONAL_FILLED,
  "coverage_pct": $(awk "BEGIN{printf \"%.1f\", $TOTAL_FILLED*100/$TOTAL_SLOTS}"),
  "docs": {
EOF
  first=1
  while IFS=$'\t' read -r filename cov filled; do
    if [ $first -eq 0 ]; then echo ","; fi
    first=0
    pct=$(awk "BEGIN{printf \"%.0f\", $filled*100/8}")
    echo -n "    \"$filename\": {\"coverage\": \"$cov\", \"filled\": $filled, \"total\": 8, \"pct\": $pct}"
  done < "$DETAIL_TSV"
  echo ""
  echo "  }"
  echo "}"
  exit 0
fi

# Markdown 表格输出
TABLE=""
TABLE+="## 模板 §一~§八 覆盖率报告（OPT-DOC-14）\n\n"
TABLE+="> **目录**: \`$DOCS_DIR\`  \n"
TABLE+="> **生成时间**: $(date '+%Y-%m-%d %H:%M:%S')  \n"
TABLE+="> **总文档数**: $TOTAL_DOCS 篇  \n\n"

TABLE+="### 全局统计\n\n"
TABLE+="| 维度 | 数值 |\n"
TABLE+="|------|------|\n"
TABLE+="| 文档总数 | $TOTAL_DOCS |\n"
TABLE+="| 必填节（§一~§六）总数 | $((TOTAL_DOCS * 6)) |\n"
TABLE+="| 必填节已填 | $REQUIRED_FILLED |\n"
TABLE+="| 必填节覆盖率 | $(awk "BEGIN{printf \"%.1f\", $REQUIRED_FILLED*100/($TOTAL_DOCS*6)}")% |\n"
TABLE+="| 推荐节（§七 §八）总数 | $((TOTAL_DOCS * 2)) |\n"
TABLE+="| 推荐节已填 | $OPTIONAL_FILLED |\n"
TABLE+="| 推荐节覆盖率 | $(awk "BEGIN{printf \"%.1f\", $OPTIONAL_FILLED*100/($TOTAL_DOCS*2)}")% |\n"
TABLE+="| **全节总覆盖率** | **$(awk "BEGIN{printf \"%.1f\", $TOTAL_FILLED*100/$TOTAL_SLOTS}")%** |\n\n"

TABLE+="### 详细覆盖表（按文件名排序）\n\n"
TABLE+="| filename | §一 | §二 | §三 | §四 | §五 | §六 | §七 | §八 | filled | total | pct |\n"
TABLE+="|----------|-----|-----|-----|-----|-----|-----|-----|-----|--------|-------|-----|\n"

# 按文件名排序，输出每行
while IFS=$'\t' read -r filename cov filled; do
  pct=$(awk "BEGIN{printf \"%.0f\", $filled*100/8}")
  # 把 0/1 序列转成 ❌/✅
  c1=$( [ "${cov:0:1}" = "1" ] && echo "✅" || echo "❌" )
  c2=$( [ "${cov:1:1}" = "1" ] && echo "✅" || echo "❌" )
  c3=$( [ "${cov:2:1}" = "1" ] && echo "✅" || echo "❌" )
  c4=$( [ "${cov:3:1}" = "1" ] && echo "✅" || echo "❌" )
  c5=$( [ "${cov:4:1}" = "1" ] && echo "✅" || echo "❌" )
  c6=$( [ "${cov:5:1}" = "1" ] && echo "✅" || echo "❌" )
  c7=$( [ "${cov:6:1}" = "1" ] && echo "✅" || echo "❌" )
  c8=$( [ "${cov:7:1}" = "1" ] && echo "✅" || echo "❌" )
  safe_fn=$(echo "$filename" | sed 's/|/\\|/g')
  TABLE+="| $safe_fn | $c1 | $c2 | $c3 | $c4 | $c5 | $c6 | $c7 | $c8 | $filled | 8 | ${pct}% |\n"
done < <(sort -t$'\t' -k1 "$DETAIL_TSV")

# 覆盖最低 Top 5
TABLE+="\n### 覆盖最低 Top 5（需补齐）\n\n"
TABLE+="| 排名 | filename | 已填 | 完成度 |\n"
TABLE+="|------|----------|------|--------|\n"
rank=1
while IFS=$'\t' read -r filename cov filled; do
  pct=$(awk "BEGIN{printf \"%.0f\", $filled*100/8}")
  TABLE+="| $rank | $filename | $filled/8 | ${pct}% |\n"
  rank=$((rank+1))
done < <(sort -t$'\t' -k3 -n "$DETAIL_TSV" | head -5)

# 覆盖最高 Top 5
TABLE+="\n### 覆盖最高 Top 5（最佳实践）\n\n"
TABLE+="| 排名 | filename | 已填 | 完成度 |\n"
TABLE+="|------|----------|------|--------|\n"
rank=1
while IFS=$'\t' read -r filename cov filled; do
  pct=$(awk "BEGIN{printf \"%.0f\", $filled*100/8}")
  TABLE+="| $rank | $filename | $filled/8 | ${pct}% |\n"
  rank=$((rank+1))
done < <(sort -t$'\t' -k3 -nr "$DETAIL_TSV" | head -5)

# §四~§八 专项统计（OPT-DOC-14 重点）
TABLE+="\n### §四~§八 专项覆盖（OPT-DOC-14 重点）\n\n"
TABLE+="| 节 | 标题 | 覆盖文档数 | 总文档 | 覆盖率 |\n"
TABLE+="|----|------|-----------|--------|--------|\n"
for i in 3 4 5 6 7; do
  section="${SECTIONS[$i]}"
  count=0
  for f in "$DOCS_DIR"/*.md; do
    filename=$(basename "$f")
    case "$filename" in
      README.md|DEPRECATED_*) continue ;;
    esac
    # 旧实现允许「任意一节的数字前缀」+ 本节名 命中（如 `## 六、数据模型` 被算作 §五），
    # 会虚高覆盖率。改用 lib 的严格判定：序号必须与节序一致。
    if fd_section_present "$f" "$i"; then
      count=$((count+1))
    fi
  done
  pct=$(awk "BEGIN{printf \"%.1f\", $count*100/$TOTAL_DOCS}")
  TABLE+="| ${SHORT_NAMES[$i]} | $section | $count | $TOTAL_DOCS | ${pct}% |\n"
done

# 输出
if [ -n "$OUT_FILE" ]; then
  printf "$TABLE" > "$OUT_FILE"
  echo -e "${GREEN}✅ 报告已写入: $OUT_FILE${NC}"
else
  echo -e "$TABLE"
fi

# 退出状态
GLOBAL_PCT=$(awk "BEGIN{printf \"%.0f\", $TOTAL_FILLED*100/$TOTAL_SLOTS}")
if [ "$GLOBAL_PCT" -lt 30 ]; then
  echo -e "\n${RED}❌ 全节总覆盖率 ${GLOBAL_PCT}% < 30%，需立即补齐${NC}" >&2
  exit 1
elif [ "$GLOBAL_PCT" -lt 50 ]; then
  echo -e "\n${YELLOW}⚠️  全节总覆盖率 ${GLOBAL_PCT}% < 50%，建议补齐${NC}" >&2
  exit 0
fi
echo -e "\n${GREEN}✅ 全节总覆盖率 ${GLOBAL_PCT}%${NC}"
