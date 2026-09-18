#!/usr/bin/env bash
# =============================================================================
# check-unwired-assets.sh
# "已实现未接线"资产基线校验（新规划任务清单 T-P0-04）
#
# 背景：本项目的主缺口形态不是"没实现"，而是"实现了但没人调用"（第二步调研 判定 A，
# 共 7 组）。这类代码能编译、能过 lint、单测甚至很绿，却在生产路径上永不执行。
# 把 7 组固化成可重跑判定，用于防止：
#   ① 接线状态悄悄变化后无人回灌文档        → 退出码 1
#   ② 被登记的能力改名/删除致检查形同虚设  → 退出码 2（fail-closed）
#
# 判定口径：
#   定义存在 = defpat 在非测试 .go 文件命中 ≥1（命中不到即 exit 2）
#   已接线   = callpat 在 scope 内命中，且剔除注释行与 func 定义行后 ≥1
#   组状态   = 组内任一符号已接线即 WIRED，全为 0 才 UNWIRED
#
# 用法：
#   bash scripts/check-unwired-assets.sh            # 输出表格 + 退出码
#   bash scripts/check-unwired-assets.sh --verbose   # 附接线命中的 文件:行
#
# 新增条目：P1+ 每实现一个新能力若暂无生产调用点，就在这里加一行登记，
# 而不是留下一个"看起来能用、其实永不执行"的符号。
#
# 兼容 bash 3.2（macOS 自带），不使用关联数组。
# =============================================================================

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)/user-server"

GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; NC='\033[0m'

VERBOSE=0
if [[ "${1:-}" == "--verbose" ]]; then VERBOSE=1; fi

if [[ ! -d "$SERVER_DIR/internal" ]]; then
  echo -e "${RED}❌ 找不到 user-server 源码目录：$SERVER_DIR（本脚本须在 hivemtk 仓内运行）${NC}"
  exit 2
fi

# -----------------------------------------------------------------------------
# 基线表：item|描述|defpat|callpat|scope
#   - callpat 一律带调用括号或包限定，避免命中同名散文
#   - scope 为相对 SERVER_DIR 的目录（空格分隔）；留空 = 整个 user-server
# -----------------------------------------------------------------------------
BASELINE=(
  "1|冷触达审批门（checker 为 nil 即直接放行）|func SetGlobalApprovalChecker|SetGlobalApprovalChecker\(|"
  "1|冷触达审批门白名单构造|func NewWhiteList|NewWhiteList\(|"
  "2|Saga 补偿管理器注入|func \\(d \\*SOPExecutionDispatcher\\) SetCompensationManager|SetCompensationManager\(|"
  "3|工具熔断器注册中心|func NewCircuitBreakerRegistry|NewCircuitBreakerRegistry\(|"
  "4|工具审计 DB 持久化|func NewDBAuditLogger|NewDBAuditLogger\(|"
  "4|工具审计 内存+DB+告警 组合器|func NewCompositeAuditLogger|NewCompositeAuditLogger\(|"
  "5|Agent 双模式分派（passive/active）|func Resolver|lifecycle\\.Resolver\\(|"
  "6|挽回队列的定时消费者|type RecoveryQueue struct|RecoveryQueue|internal/cron internal/app"
  "7|Agent 断点续跑 存点|func SaveCheckpoint|SaveCheckpoint\(|"
  "7|Agent 断点续跑 取点|func LoadLatestCheckpoint|LoadLatestCheckpoint\(|"
  "7|Agent 断点续跑 续跑阶段|func ResumeStage|ResumeStage\(|"
)

hits() {  # hits <pattern> <dir...> — 只扫 .go，跳过 _test.go
  local pat="$1"; shift
  grep -rnE --include='*.go' "$pat" "$@" 2>/dev/null | grep -v '_test\.go:' || true
}

echo "══════ 未接线资产基线（判定 A · 7 组）══════"

DEAD=0
WIRED_ITEMS=" "
LAST_ITEM=""

for entry in "${BASELINE[@]}"; do
  IFS='|' read -r item desc defpat callpat scope <<< "$entry"

  def_lines=$(hits "$defpat" "$SERVER_DIR")
  def_n=0
  [[ -n "$def_lines" ]] && def_n=$(printf '%s\n' "$def_lines" | grep -c .)

  if [[ "$def_n" -eq 0 ]]; then
    printf '  [项%s] %b定义缺失%b  %s —— defpat 未命中，基线已不可信\n' \
      "$item" "$RED" "$NC" "$desc"
    DEAD=1
    continue
  fi

  scope_dirs="$SERVER_DIR"
  if [[ -n "$scope" ]]; then
    scope_dirs=""
    for d in $scope; do
      if [[ ! -d "$SERVER_DIR/$d" ]]; then
        # scope 目录不存在时命中必然为 0，会让该项"看似未接线"实为检查失效 —— 显式失败
        printf '  [项%s] %bscope 目录缺失%b %s（%s/%s 不存在）\n' \
          "$item" "$RED" "$NC" "$desc" "$SERVER_DIR" "$d"
        DEAD=1
        scope_dirs=""
        break
      fi
      scope_dirs="$scope_dirs $SERVER_DIR/$d"
    done
    [[ -z "$scope_dirs" && $DEAD -eq 1 ]] && continue
  fi

  wiring=$(hits "$callpat" $scope_dirs \
    | grep -vE ':[0-9]+:[[:space:]]*(//|\*|/\*)' \
    | grep -vE ':[0-9]+:[[:space:]]*func ' || true)
  n=0
  [[ -n "$wiring" ]] && n=$(printf '%s\n' "$wiring" | grep -c .)

  if [[ "$n" -gt 0 ]]; then
    printf '  [项%s] %bWIRED%b    接线数=%-3s %s（def 命中 %s 处）\n' \
      "$item" "$GREEN" "$NC" "$n" "$desc" "$def_n"
    if [[ "$LAST_ITEM" != "$item" ]]; then
      WIRED_ITEMS="$WIRED_ITEMS$item "
      LAST_ITEM="$item"
    fi
  else
    printf '  [项%s] %bUNWIRED%b  接线数=0   %s（定义在，无人调用）\n' \
      "$item" "$YELLOW" "$NC" "$desc"
  fi

  if [[ $VERBOSE -eq 1 && $n -gt 0 ]]; then
    printf '%s\n' "$wiring" | sed -e "s#$SERVER_DIR/##" -e 's/^\(.\{140\}\).*/      ↳ \1…/'
  fi
done

echo "----------------------------------------------------------------------"

if [[ $DEAD -eq 1 ]]; then
  echo -e "${RED}❌ 有登记符号的定义未命中：能力已改名或被删除，请更新本基线（exit 2）${NC}"
  exit 2
fi

DRIFT=""
for it in 1 2 3 4 5 6 7; do
  case "$WIRED_ITEMS" in
    *" $it "*) DRIFT="$DRIFT 项$it" ;;
  esac
done

if [[ -n "$DRIFT" ]]; then
  echo -e "${YELLOW}⚠️  与基线不符（exit 1）：$DRIFT 已接线，而登记为未接线${NC}"
  echo    "   → 回灌 docs/replan-2026-09/本项目调研.md 判定 A、AI_CORE_FEATURE_INVENTORY 短板表与对应任务卡"
  exit 1
fi

echo -e "${GREEN}✅ 与基线一致：判定 A 的 7 组仍全部未接线（exit 0）${NC}"
echo    "   这些能力在接线前不得对外宣称可用；接线属改变生产行为的变更，须逐条灰度。"
exit 0
