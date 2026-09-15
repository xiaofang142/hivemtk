#!/usr/bin/env bash
# =============================================================================
# check-feature-doc.sh
# 功能文档 8 节模板结构校验（OPT-DOC-13）
#
# 对应文档：[docs/standards/FEATURE_DOCUMENTATION_TEMPLATE.md]
# 校验项：
#   1. 必填节（§一 ~ §六）：功能完成状态 / 核心原理 / 设计标准 / 架构与模块关系 / 数据模型 / 业务流程
#   2. 推荐节（§七 §八）：前端交互 / 测试策略（缺失给 WARN）
#   3. 元数据块：所属系统 / 功能 slug / 代码位置
#
# 用法：
#   bash scripts/check-feature-doc.sh
#   或: bash scripts/check-feature-doc.sh --strict
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

ERRORS=0
WARNINGS=0
STRICT="${1:-}"

log_pass() { echo -e "${GREEN}✅ $1${NC}"; }
log_fail() { echo -e "${RED}❌ $1${NC}"; ERRORS=$((ERRORS+1)); }
log_warn() { echo -e "${YELLOW}⚠️  $1${NC}"; WARNINGS=$((WARNINGS+1)); }

DOCS_DIR="$PROJECT_ROOT/hivemtk/docs/marketing-features"
if [ ! -d "$DOCS_DIR" ]; then
  log_fail "目录不存在: $DOCS_DIR"
  exit 1
fi

echo "============================================================"
echo "  Feature Doc 8 节模板校验（OPT-DOC-13）"
echo "  目录: $DOCS_DIR"
echo "  模式: ${STRICT:-normal}"
echo "============================================================"
echo ""

# 必填节标题正则（宽松匹配）
REQUIRED_SECTIONS=(
  "功能完成状态"
  "核心原理"
  "设计标准"
  "架构与模块关系"
  "数据模型"
  "业务流程"
)

RECOMMENDED_SECTIONS=(
  "前端交互"
  "测试策略"
)

PASSED=0
FAILED=0
SKIPPED=0

for f in "$DOCS_DIR"/*.md; do
  filename=$(basename "$f")
  # 跳过非功能文档（README / DEPRECATED / OPT 任务清单等）
  if [ "$filename" = "README.md" ] || [[ "$filename" =~ ^DEPRECATED_ ]]; then
    SKIPPED=$((SKIPPED+1))
    continue
  fi

  # 必填节检查
  MISSING_REQUIRED=()
  for section in "${REQUIRED_SECTIONS[@]}"; do
    if ! grep -q "$section" "$f"; then
      MISSING_REQUIRED+=("$section")
    fi
  done

  # 推荐节检查
  MISSING_RECOMMENDED=()
  for section in "${RECOMMENDED_SECTIONS[@]}"; do
    if ! grep -q "$section" "$f"; then
      MISSING_RECOMMENDED+=("$section")
    fi
  done

  # 元数据块检查
  HAS_METADATA=false
  if grep -qE "^\*\*所属系统\*\*:|^>[[:space:]]*\*\*所属系统\*\*:" "$f" \
     || grep -qE "^\*\*功能 slug\*\*:|^>[[:space:]]*\*\*功能 slug\*\*:" "$f"; then
    HAS_METADATA=true
  fi

  # 输出结果
  if [ ${#MISSING_REQUIRED[@]} -gt 0 ]; then
    log_fail "$filename 缺必填节: ${MISSING_REQUIRED[*]}"
    FAILED=$((FAILED+1))
  else
    if [ ${#MISSING_RECOMMENDED[@]} -gt 0 ]; then
      log_warn "$filename 缺推荐节: ${MISSING_RECOMMENDED[*]}"
    fi
    if [ "$HAS_METADATA" = false ]; then
      log_warn "$filename 缺元数据块（**所属系统** / **功能 slug**）"
    fi
    log_pass "$filename 通过必填节校验"
    PASSED=$((PASSED+1))
  fi
done

# 总结
echo ""
echo "============================================================"
echo "  总结"
echo "============================================================"
echo "  通过: $PASSED"
echo "  失败: $FAILED"
echo "  跳过: $SKIPPED（README / DEPRECATED）"

if [ $WARNINGS -gt 0 ]; then
  echo -e "  警告: ${YELLOW}$WARNINGS${NC}"
fi

if [ $ERRORS -gt 0 ]; then
  exit 1
fi

# strict 模式下推荐节缺失也算错误
if [ -n "$STRICT" ] && [ $WARNINGS -gt 0 ]; then
  echo ""
  echo "::error::strict 模式下 ${WARNINGS} 个警告视为错误"
  exit 1
fi

exit 0
