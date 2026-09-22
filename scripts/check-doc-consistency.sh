#!/usr/bin/env bash
# =============================================================================
# check-doc-consistency.sh
# 文档一致性检查脚本（OPT-DOC-15）
#
# 校验项（与脚本内 [n/6] 分节一一对应）：
#   1. README 引用（marketing-features / platform-features 两个索引）
#   2. ADR 编号连续性（adr/README.md 登记为「已删除/作废」的有意缺号不计入断档）
#   3. Feature Doc 8 节模板结构（判定源 lib/feature-doc-sections.sh）
#   4. CODEOWNERS 路径引用
#   5. 顶层架构文档 vs 营销文档一致性
#   6. 关键文件存在性
#
# 注意：本脚本**不**做任意文档间相对链接的断链检查（第 1 项只覆盖两个 README 索引）。
# 新增/改动的正文内相对链接需人工确认目标存在，否则会静默成为死引用。
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

# -----------------------------------------------------------------------------
# 前置：本脚本是「工作区级」而非「仓库级」——它同时看 hivemtk/ 与 hivemtk-platform/
# 两个仓，以及仓库外的顶层 docs/。故 PROJECT_ROOT 必须是仓库的上一级。
# 布局不成立时（改名克隆、把仓库放到别的层级）过去只在 [3/6] 的 `find` 上因
# set -e 直接退出：rc=1、除"项目根: /tmp"外无任何解释，看起来像仓库违规、
# 实际是脚本找不到根。现显式以 rc=2 早退并给可操作信息（2=输入无效，1=发现违规）。
# -----------------------------------------------------------------------------
if [ ! -d "$PROJECT_ROOT/hivemtk" ]; then
  echo -e "${RED}❌ 工作区布局不成立：$PROJECT_ROOT/hivemtk/ 不存在${NC}" >&2
  echo "   推导链：SCRIPT_DIR=$SCRIPT_DIR ⇒ PROJECT_ROOT=$PROJECT_ROOT" >&2
  echo "   本脚本须在 <workspace>/hivemtk/scripts/ 下运行（仓库的上一级须直接含 hivemtk/）。" >&2
  echo "   GitHub Actions 的 <work>/<repo>/<repo> 恰好满足该布局，故 CI 可用；" >&2
  echo "   影子克隆/改名目录要复刻同样层级，否则此门无法判定（不是仓库的错）。" >&2
  exit 2
fi

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
  # 有意缺号：adr/README.md 登记为「已删除/作废」的编号不允许回填（编号不复用是显式约定），
  # 因此这类断档视为已归档，不再告警；只有**未登记**的断档才是真问题。
  VOIDED=""
  if [ -f "$ADR_DIR/README.md" ]; then
    VOIDED=$(grep -oE '\|[[:space:]]*ADR-0*[0-9]+[[:space:]]*\|[[:space:]]*(❌[[:space:]]*)?(已删除|作废)' "$ADR_DIR/README.md" \
             | grep -oE 'ADR-0*[0-9]+' | sed -E 's/ADR-0*//' | tr '\n' ' ' || true)
  fi
  is_voided() { case " $VOIDED " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }

  ADR_FILES=$(ls "$ADR_DIR"/ADR-*.md 2>/dev/null | sort || true)
  if [ -n "$ADR_FILES" ]; then
    NUMBERS=$(echo "$ADR_FILES" | sed -E 's/.*ADR-0*([0-9]+).*/\1/' | sort -n)
    PREV=0
    GAPS=0
    for n in $NUMBERS; do
      if [ "$PREV" -ne 0 ] && [ "$n" -ne "$((PREV+1))" ]; then
        UNDOC=""
        for m in $(seq $((PREV+1)) $((n-1))); do
          if is_voided "$m"; then
            log_pass "ADR-$(printf '%03d' "$m") 已在 adr/README.md 登记为作废（有意缺号，不回填）"
          else
            UNDOC="$UNDOC ADR-$(printf '%03d' "$m")"
          fi
        done
        if [ -n "$UNDOC" ]; then
          log_warn "ADR 编号断档: ADR-$(printf '%03d' "$PREV") → ADR-$(printf '%03d' "$n")（未登记作废：${UNDOC}）"
          GAPS=$((GAPS+1))
        fi
      fi
      PREV=$n
    done
    if [ $GAPS -eq 0 ]; then
      log_pass "ADR 编号连续或断档均已登记（末号 ADR-$(printf '%03d' "$PREV")）"
    else
      log_warn "ADR 有 $GAPS 处未登记断档,需补档或在 adr/README.md 说明"
    fi
  fi
fi

# -----------------------------------------------------------------------------
# 3. Feature Doc 8 节模板结构（OPT-DOC-13 关联）
# -----------------------------------------------------------------------------
echo ""
echo "[3/6] Feature Doc 8 节结构..."

# set -e 下 `VAR=$(find …)` 在 find 非零退出时会让整脚本死掉（目录缺失时以前就是这样），
# 故显式吞掉退出码，并在下面对"零文件"单独判告警——不然本节会静默"通过"，
# 而"扫了 0 个文档"与"扫了 20 个且全合规"是两件完全不同的事。
FEATURE_DOCS=$(find "$PROJECT_ROOT/hivemtk/docs/marketing-features" -maxdepth 1 -name "*.md" ! -name "README.md" 2>/dev/null || true)
STRUCT_VIOLATIONS=0
FD_COUNT=$(printf '%s\n' "$FEATURE_DOCS" | grep -c . || true)
if [ "$FD_COUNT" -eq 0 ]; then
  log_warn "docs/marketing-features 下没有任何 feature doc，本节零覆盖（不是通过）"
fi
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
  log_pass "所有 feature doc 含 §一 §二 必填节（共检查 $FD_COUNT 个）"
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
  # DEPRECATED_auto-reply.md 已在 48e9d47c「docs: 清理冗余废弃文档（21 篇）」中删除，
  # 全仓对其零引用，不再作为关键文件期望（曾产生长期假阳性告警）。
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
