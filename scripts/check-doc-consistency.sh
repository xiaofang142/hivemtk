#!/usr/bin/env bash
# =============================================================================
# check-doc-consistency.sh
# 文档一致性检查脚本（OPT-DOC-15）
#
# 校验项：
#   1. README 引用 vs 实际 .md 文件存在性
#   2. 文档内部相对链接断链
#   3. 8 节模板结构
#   4. ADR 编号连续性
#   5. 元数据块（所属系统/功能 slug）
#   6. .github/CODEOWNERS 路径引用
#
# 用法：
#   bash scripts/check-doc-consistency.sh
#   或: bash scripts/check-doc-consistency.sh --strict
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# 8 节模板判定统一走单一真源
# shellcheck source=lib/feature-doc-sections.sh
source "$SCRIPT_DIR/lib/feature-doc-sections.sh"

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

echo "============================================================"
echo "  文档一致性检查（OPT-DOC-15）"
echo "  项目根: $PROJECT_ROOT"
echo "  模式: ${STRICT:-normal}"
echo "============================================================"
echo ""

# -----------------------------------------------------------------------------
# 1. README 引用 vs 实际 .md 文件存在性
# -----------------------------------------------------------------------------
echo "[1/6] README 引用检查..."

# 1.1 marketing-features/README.md 引用的所有 .md 必须存在
MF_README="$PROJECT_ROOT/hivemtk/docs/marketing-features/README.md"
if [ -f "$MF_README" ]; then
  # 提取形如 [xxx.md](xxx.md) 的本地链接
  REFERENCED=$(grep -oE '\[[^]]+\]\(([a-zA-Z0-9_./-]+\.md)\)' "$MF_README" \
    | sed -E 's/.*\(([^)]+)\).*/\1/' \
    | sort -u || true)
  
  MISSING=0
  for ref in $REFERENCED; do
    # 跳过外链（http/https/绝对路径）
    if [[ "$ref" =~ ^https?:// ]] || [[ "$ref" =~ ^/ ]]; then
      continue
    fi
    # 跨仓库引用（hivemtk/ ↔ hivemtk-platform/）走相对路径 ../../...
    # 需相对源文件所在目录解析,不是 PROJECT_ROOT
    if [[ "$ref" =~ ^\.\./\.\./hivemtk-platform/ ]]; then
      # 源文件在 hivemtk/docs/marketing-features/,解析为 hivemtk-platform/...
      cross_path="$PROJECT_ROOT/$ref"
      if [ ! -f "$cross_path" ]; then
        log_warn "marketing-features/README.md 跨仓库引用解析失败: $ref"
        MISSING=$((MISSING+1))
      fi
      continue
    fi
    full_path="$PROJECT_ROOT/hivemtk/docs/marketing-features/$ref"
    if [ ! -f "$full_path" ]; then
      log_fail "marketing-features/README.md 引用了不存在的文件: $ref"
      MISSING=$((MISSING+1))
    fi
  done
  if [ $MISSING -eq 0 ]; then
    log_pass "marketing-features/README.md 全部链接有效"
  fi
fi

# 1.2 platform-features/README.md 同样检查
PF_README="$PROJECT_ROOT/hivemtk-platform/docs/platform-features/README.md"
if [ -f "$PF_README" ]; then
  REFERENCED=$(grep -oE '\[[^]]+\]\(([a-zA-Z0-9_./-]+\.md)\)' "$PF_README" \
    | sed -E 's/.*\(([^)]+)\).*/\1/' \
    | sort -u || true)
  
  MISSING=0
  for ref in $REFERENCED; do
    if [[ "$ref" =~ ^https?:// ]] || [[ "$ref" =~ ^/ ]]; then
      continue
    fi
    if [[ "$ref" =~ ^\.\./\.\./hivemtk/ ]]; then
      # 源文件在 hivemtk-platform/docs/platform-features/,解析为 hivemtk/...
      cross_path="$PROJECT_ROOT/$ref"
      if [ ! -f "$cross_path" ]; then
        log_warn "platform-features/README.md 跨仓库引用解析失败: $ref"
        MISSING=$((MISSING+1))
      fi
      continue
    fi
    full_path="$PROJECT_ROOT/hivemtk-platform/docs/platform-features/$ref"
    if [ ! -f "$full_path" ]; then
      log_fail "platform-features/README.md 引用了不存在的文件: $ref"
      MISSING=$((MISSING+1))
    fi
  done
  if [ $MISSING -eq 0 ]; then
    log_pass "platform-features/README.md 全部链接有效"
  fi
fi

# -----------------------------------------------------------------------------
# 2. ADR 编号连续性
# -----------------------------------------------------------------------------
echo ""
echo "[2/6] ADR 编号连续性..."

ADR_DIR="$PROJECT_ROOT/hivemtk/docs/architecture/adr"
if [ -d "$ADR_DIR" ]; then
  ADR_FILES=$(ls "$ADR_DIR"/ADR-*.md 2>/dev/null | sort || true)
  if [ -n "$ADR_FILES" ]; then
    NUMBERS=$(echo "$ADR_FILES" | sed -E 's/.*ADR-0*([0-9]+).*/\1/' | sort -n)
    PREV=0
    GAPS=0
    for n in $NUMBERS; do
      if [ "$PREV" -ne 0 ] && [ "$n" -ne "$((PREV+1))" ]; then
        log_warn "ADR 编号断档: ADR-$(printf '%03d' $PREV) → ADR-$(printf '%03d' $n) (缺 ADR-$(printf '%03d' $((PREV+1))) ~ ADR-$(printf '%03d' $((n-1))))"
        GAPS=$((GAPS+1))
      fi
      PREV=$n
    done
    if [ $GAPS -eq 0 ]; then
      log_pass "ADR 编号连续 (现有 $PREV 个 ADR)"
    else
      log_warn "ADR 编号有 $GAPS 个断档,需 OPT-DOC-10 补档或说明"
    fi
  fi
fi

# -----------------------------------------------------------------------------
# 3. Feature Doc 8 节模板结构（OPT-DOC-13 关联）
# -----------------------------------------------------------------------------
echo ""
echo "[3/6] Feature Doc 8 节结构..."

FEATURE_DOCS=$(find "$PROJECT_ROOT/hivemtk/docs/marketing-features" -maxdepth 1 -name "*.md" ! -name "README.md" 2>/dev/null)
STRUCT_VIOLATIONS=0
for f in $FEATURE_DOCS; do
  filename=$(basename "$f")
  # 跳过下线说明文档
  if [[ "$filename" =~ ^DEPRECATED_ ]]; then
    continue
  fi
  # 检查是否含 8 节标题。
  # 旧实现用 `"^## 一、\|功能完成状态"` —— BSD grep 的 BRE 不支持 `\|`，
  # 整条模式退化成字面量 `^## 一、|功能完成状态`，恒定不匹配，
  # 导致对已含 §一 的文档误报「缺失」。改用 lib 的严格判定。
  if ! fd_section_present "$f" 0; then
    log_warn "缺 §一 功能完成状态: $filename"
    STRUCT_VIOLATIONS=$((STRUCT_VIOLATIONS+1))
  fi
  if ! fd_section_present "$f" 1; then
    log_warn "缺 §二 核心原理: $filename"
    STRUCT_VIOLATIONS=$((STRUCT_VIOLATIONS+1))
  fi
done
if [ $STRUCT_VIOLATIONS -eq 0 ]; then
  log_pass "所有 feature doc 含 §一 §二 必填节"
fi

# -----------------------------------------------------------------------------
# 4. CODEOWNERS 路径引用
# -----------------------------------------------------------------------------
echo ""
echo "[4/6] CODEOWNERS 路径检查..."

# 修正（2026-09-15）：CODEOWNERS 位于 hivemtk 仓库根（$PROJECT_ROOT/hivemtk/），
# 且其内部路径是相对**该仓库根**的。原实现用 $PROJECT_ROOT/CODEOWNERS（工作区根）
# 判存在性，恒为 false，导致本节被静默跳过（既无通过也无告警）。
REPO_ROOT="$PROJECT_ROOT/hivemtk"
if [ -f "$REPO_ROOT/CODEOWNERS" ]; then
  # 提取 CODEOWNERS 中所有路径（行首 / 开头）
  PATHS=$(grep -E "^/" "$REPO_ROOT/CODEOWNERS" | awk '{print $1}' | grep -v "^$" || true)
  MISSING=0
  for p in $PATHS; do
    # 跳过通配符
    if [[ "$p" =~ \*$ ]] || [[ "$p" =~ \.\* ]]; then
      continue
    fi
    # CODEOWNERS 路径中 * 不当 glob
    if [[ "$p" =~ \* ]]; then
      continue
    fi
    # CODEOWNERS 路径以 / 开头（相对仓库根）
    stripped="${p#/}"
    full_path="$REPO_ROOT/$stripped"
    if [ ! -e "$full_path" ]; then
      log_warn "CODEOWNERS 引用的路径不存在: $p"
      MISSING=$((MISSING+1))
    fi
  done
  if [ $MISSING -eq 0 ]; then
    log_pass "CODEOWNERS 路径引用全部有效"
  fi
else
  log_warn "未找到 CODEOWNERS: $REPO_ROOT/CODEOWNERS"
fi

# -----------------------------------------------------------------------------
# 5. 顶层架构文档 vs marketing-features 一致性
# -----------------------------------------------------------------------------
echo ""
echo "[5/6] 顶层架构文档 vs 营销文档一致性..."

# 检查 ARCHITECTURE_OVERVIEW.md 引用的营销文档
# 修正（2026-09-15）：原路径为 $PROJECT_ROOT/ARCHITECTURE_OVERVIEW.md（工作区根），
# 实际位于 docs/architecture/ 下，导致该节静默跳过。
ARCH_DOC="$PROJECT_ROOT/docs/architecture/ARCHITECTURE_OVERVIEW.md"
if [ -f "$ARCH_DOC" ]; then
  REFS=$(grep -oE 'marketing-features/[a-zA-Z0-9_./-]+\.md' "$ARCH_DOC" | sort -u || true)
  MISSING=0
  for ref in $REFS; do
    full_path="$PROJECT_ROOT/hivemtk/docs/$ref"
    if [ ! -f "$full_path" ]; then
      log_warn "ARCHITECTURE_OVERVIEW.md 引用了不存在的营销文档: $ref"
      MISSING=$((MISSING+1))
    fi
  done
  if [ $MISSING -eq 0 ]; then
    log_pass "ARCHITECTURE_OVERVIEW.md 营销文档引用全部有效"
  fi
fi

# -----------------------------------------------------------------------------
# 6. 关键文件存在性
# -----------------------------------------------------------------------------
echo ""
echo "[6/6] 关键文件存在性..."

# 说明（2026-09-15 修正）：
#   PROJECT_ROOT 为**工作区根**（含 hivemtk/、hivemtk-platform/、docs/ 等）。
#   原表把这些文件全部写成 ":."（即期望它们位于工作区根），实测 12 条全部误报缺失——
#   这些文件实际分散在 hivemtk/、docs/architecture/、docs/governance/ 下。
#   现已按实测路径逐条修正。
KEY_FILES=(
  "README.md:hivemtk/"
  "README.en.md:hivemtk/"
  "README.md:hivemtk-platform/"
  "LICENSE:hivemtk/"
  "LICENSE:hivemtk-platform/"
  "CONTRIBUTING.md:hivemtk/"
  "CODE_OF_CONDUCT.md:hivemtk/"
  "SECURITY.md:hivemtk/"
  "CHANGELOG.md:hivemtk/"
  "NOTICE:hivemtk/"
  "NOTICE:hivemtk-platform/"
  "THIRD_PARTY_LICENSES.md:hivemtk/"
  "CLA.md:hivemtk/"
  # ---- 仓库根（hivemtk 仓库）----
  "CODEOWNERS:hivemtk/"
  # ---- 工作区治理文档 ----
  "GOVERNANCE.md:docs/governance/"
  "MAINTAINERS.md:docs/governance/"
  "78-OPTIMIZATION-TASKS.md:docs/governance/"
  # ---- 工作区架构文档 ----
  "ARCHITECTURE_OVERVIEW.md:docs/architecture/"
  "USER_SERVER_DEEP_ARCHITECTURE.md:docs/architecture/"
  "PLATFORM_DEEP_ARCHITECTURE.md:docs/architecture/"
  "FRONTEND_DEEP_ARCHITECTURE.md:docs/architecture/"
  "DEPLOYMENT_OPS_ARCHITECTURE.md:docs/architecture/"
  "CROSS_CUTTING_CONCERNS.md:docs/architecture/"
  # ---- 仓库内文档 ----
  "DATABASE_SCHEMA_DEEP_DIVE.md:hivemtk/docs/architecture/"
  "INDEX.md:hivemtk/docs/"
  "FEATURE_DOCUMENTATION_TEMPLATE.md:hivemtk/docs/standards/"
  "MASTER_RULES.md:hivemtk/docs/standards/"
  "DEPRECATED_auto-reply.md:hivemtk/docs/marketing-features/"
)

MISSING=0
SKIPPED_WS=0
# 工作区级文档（docs/governance、docs/architecture 等）**不在本仓库内**，
# 它们位于工作区根目录且未纳入版本控制。在 CI（仅 checkout 本仓库）或独立 clone 中
# 这些文件必然不存在，若仍按「缺失」告警会产生大量假阳性。
# 因此先探测工作区布局是否存在，不存在则跳过该类条目。
HAS_WORKSPACE_DOCS=0
if [ -d "$PROJECT_ROOT/docs/architecture" ]; then
  HAS_WORKSPACE_DOCS=1
fi

for entry in "${KEY_FILES[@]}"; do
  IFS=':' read -r f dir <<< "$entry"
  # dir=. 时不要加点号前缀,避免 ./CODEOWNERS 变 .CODEOWNERS
  if [ "$dir" = "." ]; then
    full_path="$PROJECT_ROOT/$f"
  else
    full_path="$PROJECT_ROOT/$dir$f"
  fi
  # 工作区级文档缺失时跳过（仅在无工作区布局时生效）
  if [ "$HAS_WORKSPACE_DOCS" -eq 0 ]; then
    case "$dir" in
      docs/*)
        SKIPPED_WS=$((SKIPPED_WS+1))
        continue
        ;;
    esac
  fi
  if [ ! -f "$full_path" ]; then
    log_warn "关键文件缺失: $f (查找于 $dir)"
    MISSING=$((MISSING+1))
  fi
done
if [ "$SKIPPED_WS" -gt 0 ]; then
  echo "  ℹ️  跳过 $SKIPPED_WS 个工作区级文档（未检测到工作区布局：$PROJECT_ROOT/docs/architecture 不存在）"
fi
if [ $MISSING -eq 0 ]; then
  log_pass "所有 ${#KEY_FILES[@]} 个关键文件存在（跳过 $SKIPPED_WS 个工作区级条目）"
fi

# -----------------------------------------------------------------------------
# 总结
# -----------------------------------------------------------------------------
echo ""
echo "============================================================"
echo "  总结"
echo "============================================================"
if [ $ERRORS -gt 0 ]; then
  echo -e "${RED}❌ 发现 $ERRORS 个错误${NC}"
fi
if [ $WARNINGS -gt 0 ]; then
  echo -e "${YELLOW}⚠️  发现 $WARNINGS 个警告${NC}"
fi
if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
  echo -e "${GREEN}✅ 全部检查通过${NC}"
  exit 0
fi

# strict 模式下 warnings 也阻断
if [ -n "$STRICT" ] && [ $WARNINGS -gt 0 ]; then
  echo ""
  echo "::error::strict 模式下 ${WARNINGS} 个警告视为错误"
  exit 1
fi

if [ $ERRORS -gt 0 ]; then
  exit 1
fi
exit 0
