#!/usr/bin/env bash
# =============================================================================
# check-enum-consistency.sh
# ENUM 值域 ↔ Go 常量 一致性检查（OPT-DB-08 配套）
#
# 用途：防止 PG ENUM 迁移与 Go 代码常量漂移。
#
# 为什么必须有这个检查（2026-09-15 实证）：
#   migrations/047_pg_enums.sql 声明「所有 ENUM 值必须与 Go 代码常量保持一致」。
#   实测该约束曾被违反 3 处且长期无人发现：
#     1. platform_type_enum 缺 'qq'（model.ChannelTypeQQ 存在）
#     2. platform_type_enum 缺 'system'（047 自身会把未知值归一化为 'system'，
#        但枚举未包含 → VARCHAR→ENUM 强转必然报错）
#     3. source_type_enum 缺 'system_seed'（migrations/031、034b 种子数据实际写入）
#   由于 user-server 的 schema 由 GORM AutoMigrate 驱动且失败即 panic，
#   值域不一致的后果是**应用启动失败**，故必须在 CI 拦截。
#
# ⚠️ 本脚本 2026-09-15 重写：原实现存在两处致命缺陷，导致它**永远通过**：
#     a) 用 `\s` 匹配空白——POSIX ERE 不支持 `\s`，BSD grep 下命中数为 0，
#        Go 常量提取恒为空 → 全部落入 "数据不足" 分支 → 从不报错（假绿）；
#     b) awk 用 `gsub(/.*'/,"")` 贪婪剥离引号，多值同行时解析结果错误。
#     现已改用 `[[:space:]]` 与「按区间取行再抽引号」的解析方式。
#
# 用法：bash scripts/check-enum-consistency.sh
# 退出码：0 = 一致；1 = 存在不一致或解析失败
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 解析路径：
#   本仓库约定脚本位于 <workspace>/hivemtk/scripts/，故 ../.. = 工作区根，
#   其下应有 hivemtk/ 与 hivemtk-platform/。
#   但若在**独立 clone**（目录名不叫 hivemtk）中运行，../.. 会指向仓库之外，
#   此时回退为「以脚本所在仓库为根」的相对布局，保证脚本在两种场景都可用。
REPO_ROOT="$SCRIPT_DIR/.."
WORKSPACE_ROOT="$SCRIPT_DIR/../.."
if [[ -d "$WORKSPACE_ROOT/hivemtk/user-server" ]]; then
  PROJECT_ROOT="$(cd "$WORKSPACE_ROOT" && pwd)"
  USER_SERVER="$PROJECT_ROOT/hivemtk/user-server"
  MIGRATION="$PROJECT_ROOT/hivemtk/migrations/047_pg_enums.sql"
else
  PROJECT_ROOT="$(cd "$WORKSPACE_ROOT" && pwd)"
  REPO="$(cd "$REPO_ROOT" && pwd)"
  USER_SERVER="$REPO/user-server"
  MIGRATION="$REPO/migrations/047_pg_enums.sql"
fi

ERRORS=0
WARNS=0

log_pass() { printf '\033[0;32m✅ %s\033[0m\n' "$1"; }
log_fail() { printf '\033[0;31m❌ %s\033[0m\n' "$1"; ERRORS=$((ERRORS + 1)); }
log_warn() { printf '\033[1;33m⚠️  %s\033[0m\n' "$1"; WARNS=$((WARNS + 1)); }

if [[ ! -d "$USER_SERVER" ]]; then
  log_fail "Go 源码目录不存在: $USER_SERVER"
  exit 1
fi
if [[ ! -f "$MIGRATION" ]]; then
  log_fail "迁移文件不存在: $MIGRATION"
  exit 1
fi

# ---------------------------------------------------------------------------
# 提取 CREATE TYPE <name> AS ENUM ( ... ); 区间内的所有单引号值
# ---------------------------------------------------------------------------
extract_sql_enum() {
  local enum_name="$1"
  # grep 无命中会返回 1；配合 set -e/pipefail 会中断脚本，故整体兜底为 true
  {
    awk -v en="$enum_name" '
      $0 ~ ("CREATE TYPE " en " AS ENUM") { capture = 1; next }
      capture && /\);/ { capture = 0 }
      capture { print }
    ' "$MIGRATION" \
      | grep -oE "'[^']+'" \
      | tr -d "'" \
      | LC_ALL=C sort -u
  } || true
}

# ---------------------------------------------------------------------------
# 提取形如 `PrefixXxx  Prefix = "value"` 的 Go 常量值
# 注意：必须用 [[:space:]]，POSIX ERE 不支持 \s
# ---------------------------------------------------------------------------
extract_go_constants() {
  local dir="$1" prefix="$2"
  {
    grep -rhoE "${prefix}[A-Za-z0-9_]+[[:space:]]+${prefix}[[:space:]]*=[[:space:]]*\"[^\"]+\"" "$dir" 2>/dev/null \
      | sed -E 's/.*"([^"]+)".*/\1/' \
      | LC_ALL=C sort -u
  } || true
}

# ---------------------------------------------------------------------------
# 比对一对「枚举 ↔ Go 常量」
#   $1 枚举名  $2 Go 源目录  $3 Go 常量前缀  $4 是否允许 Go 侧无绑定（known gap）
# ---------------------------------------------------------------------------
check_pair() {
  local enum_name="$1" go_dir="$2" go_prefix="$3" known_gap="${4:-no}"

  local sql_vals
  sql_vals="$(extract_sql_enum "$enum_name")"
  if [[ -z "$sql_vals" ]]; then
    log_fail "[$enum_name] 无法从迁移文件解析出枚举值域（解析逻辑或文件格式已变更）"
    return
  fi

  local go_vals
  go_vals="$(extract_go_constants "$go_dir" "$go_prefix")"
  if [[ -z "$go_vals" ]]; then
    if [[ "$known_gap" == "yes" ]]; then
      log_warn "[$enum_name] Go 侧无 ${go_prefix}Xxx 常量绑定（已知缺口，见迁移文件第 3 节说明）"
    else
      log_fail "[$enum_name] 在 $go_dir 中未解析到 ${go_prefix}Xxx 常量——检查可能失效，或常量已被重命名"
    fi
    return
  fi

  local only_go only_sql
  only_go="$(comm -23 <(printf '%s\n' "$go_vals") <(printf '%s\n' "$sql_vals"))"
  only_sql="$(comm -13 <(printf '%s\n' "$go_vals") <(printf '%s\n' "$sql_vals"))"

  if [[ -n "$only_go" ]]; then
    log_fail "[$enum_name] Go 常量存在但 ENUM 值域缺失（VARCHAR→ENUM 强转会失败）："
    printf '%s\n' "$only_go" | sed 's/^/      - /'
  fi

  if [[ -n "$only_sql" ]]; then
    log_warn "[$enum_name] ENUM 值域比 Go 常量多出以下值（应为历史/种子数据，需在迁移中注明）："
    printf '%s\n' "$only_sql" | sed 's/^/      - /'
  fi

  if [[ -z "$only_go" && -z "$only_sql" ]]; then
    log_pass "[$enum_name] 与 ${go_prefix}Xxx 完全一致（$(printf '%s\n' "$sql_vals" | wc -l | tr -d ' ') 值）"
  fi
}

echo "============================================================"
echo "  ENUM 一致性检查（OPT-DB-08 配套）"
echo "============================================================"
echo "迁移文件 : ${MIGRATION#"$(cd "$SCRIPT_DIR/.." && pwd)"/}"
echo "Go 源码  : ${USER_SERVER#"$(cd "$SCRIPT_DIR/.." && pwd)"/}"
echo ""

echo "[1/5] platform_type_enum ↔ ChannelTypeXxx"
check_pair "platform_type_enum" "$USER_SERVER/internal/model" "ChannelType"

echo ""
echo "[2/5] intent_major_enum ↔ IntentMajorXxx"
# 已知缺口：Go 侧不存在 IntentMajorXxx 常量（见 047 第 3 节说明）
check_pair "intent_major_enum" "$USER_SERVER/internal/service" "IntentMajor" "yes"

echo ""
echo "[3/5] message_status_enum ↔ MessageStatusXxx"
check_pair "message_status_enum" "$USER_SERVER/internal/model" "MessageStatus"

echo ""
echo "[4/5] embed_status_enum ↔ EmbedStatusXxx"
check_pair "embed_status_enum" "$USER_SERVER/internal/model" "EmbedStatus"

echo ""
echo "[5/5] source_type_enum ↔ SourceTypeXxx"
check_pair "source_type_enum" "$USER_SERVER/internal/model" "SourceType"

echo ""
echo "============================================================"
if [[ "$ERRORS" -gt 0 ]]; then
  printf '\033[0;31m❌ ENUM 一致性检查失败：%d 个错误\033[0m\n' "$ERRORS"
  echo "   值域不一致会导致 AutoMigrate 的 VARCHAR→ENUM 强转失败并使应用启动 panic。"
  exit 1
fi
if [[ "$WARNS" -gt 0 ]]; then
  printf '\033[1;33m⚠️  ENUM 一致性检查通过（%d 个警告）\033[0m\n' "$WARNS"
else
  printf '\033[0;32m✅ ENUM 一致性检查完全通过\033[0m\n'
fi
exit 0
