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

# ---------------------------------------------------------------------------
# D07：意图常量表 ↔ DefaultIntents 词条 Type 的双向覆盖
#
# 为什么要这一条（docs/tech-research/DECISIONS.md D07）：IntentGreeting 曾长期
# 「常量存在、词条永无」，规则层因此永远产不出 greeting，下游按 greeting 分支的逻辑
# 永不触发；2026-10 已补词条，但同类漂移（新增意图忘配词条）没有闸会重演。
#
# 三条判据：
#   ① 每个字符串值常量都要出现在某个词条的 Type 上，或在「非词条产出」豁免名单里；
#   ② 词条的 Type 必须是已声明的常量（含归并别名），拼错或改名即红；
#   ③ 豁免项与别名归并的落点都必须在源码里真被产出，否则豁免/归并只是橡皮章。
# ---------------------------------------------------------------------------
INTENT_SRC="$USER_SERVER/internal/service/intent_recognition.go"

# 值由兜底/歧义逻辑给出、词典里本就不该有词条的意图。
# 上界当棘轮用：只准减不准悄悄加，新增项要么配词条要么进这里并被 ③ 验过。
INTENT_NON_LEXICON="IntentClarify IntentUnknown"
INTENT_NON_LEXICON_MAX=2

# 取「IntentType 销售意图类型常量」那个 const 块的正文
intent_const_block() {
  {
    awk '/IntentType 销售意图类型常量/{ cap = 1; next }
         cap && /^\)/ { cap = 0 }
         cap { print }' "$INTENT_SRC"
  } || true
}

# 字符串值常量名（别名行右侧是标识符，不会被这条匹配）
intent_value_consts() {
  {
    intent_const_block \
      | grep -oE 'Intent[A-Za-z0-9_]+[[:space:]]*=[[:space:]]*"[^"]+"' \
      | sed -E 's/^([A-Za-z0-9_]+)[[:space:]]*=.*$/\1/' \
      | LC_ALL=C sort -u
  } || true
}

# 归并别名行：`IntentA = IntentB`，输出 "IntentA IntentB"
intent_alias_pairs() {
  {
    intent_const_block \
      | grep -E '^[[:space:]]*Intent[A-Za-z0-9_]+[[:space:]]*=[[:space:]]*Intent[A-Za-z0-9_]+[[:space:]]*$' \
      | sed -E 's/^[[:space:]]*//; s/[[:space:]]*=[[:space:]]*/ /; s/[[:space:]]*$//'
  } || true
}

# 词条 Type 集合（只认 DefaultIntents 里 `Type: IntentXxx, Name:` 那一行形状）
intent_lexicon_types() {
  {
    grep -E '^[[:space:]]*Type:[[:space:]]*Intent[A-Za-z0-9_]+,' "$INTENT_SRC" \
      | sed -E 's/.*Type:[[:space:]]*(Intent[A-Za-z0-9_]+),.*$/\1/' \
      | LC_ALL=C sort -u
  } || true
}

# 该常量是否在源码里被真产出过（赋给 IntentType 字段）
intent_produced_in_source() {
  { grep -Eq "IntentType:[[:space:]]*$1[,[:space:]]" "$INTENT_SRC"; } || return 1
}

check_intent_dictionary() {
  local err_before="$ERRORS"
  if [[ ! -f "$INTENT_SRC" ]]; then
    log_fail "[intent_dict] 源文件不存在: $INTENT_SRC"
    return
  fi

  local consts aliases pairs types declared
  consts="$(intent_value_consts)"
  pairs="$(intent_alias_pairs)"
  types="$(intent_lexicon_types)"

  if [[ -z "$consts" ]]; then
    log_fail "[intent_dict] 未解析到任何 IntentXxx 字符串值常量——检查可能失效（const 块形状或注释锚点变了）"
    return
  fi
  if [[ -z "$types" ]]; then
    log_fail "[intent_dict] 未解析到任何 DefaultIntents 词条 Type——检查可能失效（词条行形状变了）"
    return
  fi

  declared="$(printf '%s\n%s\n' "$consts" "$(printf '%s\n' "$pairs" | awk 'NF==2 {print $1}')" \
    | grep -E '^Intent[A-Za-z0-9_]+$' | LC_ALL=C sort -u)"

  # ① 常量 → 词条
  local exempt missing
  exempt="$(printf '%s\n' $INTENT_NON_LEXICON | LC_ALL=C sort -u)"
  local exempt_n
  exempt_n="$(printf '%s\n' "$exempt" | grep -c . )"
  if [[ "$exempt_n" -gt "$INTENT_NON_LEXICON_MAX" ]]; then
    log_fail "[intent_dict] 非词条产出豁免已达 ${exempt_n} 项，超过上界 ${INTENT_NON_LEXICON_MAX}——新增意图应配词条，别往豁免名单里塞"
  fi

  missing="$(comm -23 <(printf '%s\n' "$consts") <(printf '%s\n' "$types"))"
  missing="$(comm -23 <(printf '%s\n' "$missing") <(printf '%s\n' "$exempt"))"
  if [[ -n "$missing" ]]; then
    log_fail "[intent_dict] 常量存在但无词条（规则层永远产不出该意图）："
    printf '%s\n' "$missing" | sed 's/^/      - /'
  fi

  # ② 词条 → 常量
  local extra
  extra="$(comm -13 <(printf '%s\n' "$declared") <(printf '%s\n' "$types"))"
  if [[ -n "$extra" ]]; then
    log_fail "[intent_dict] 词条 Type 引用了未声明的常量（改名或拼错）："
    printf '%s\n' "$extra" | sed 's/^/      - /'
  fi

  # ③a 豁免项：必须仍在常量表里、必须仍未被词条覆盖、必须真被产出
  local name
  for name in $INTENT_NON_LEXICON; do
    if ! printf '%s\n' "$consts" | grep -Fxq "$name"; then
      log_fail "[intent_dict] 豁免项 $name 已不在常量表里——请从 INTENT_NON_LEXICON 删除，留着会掩盖同名常量的重新丢失"
      continue
    fi
    if printf '%s\n' "$types" | grep -Fxq "$name"; then
      log_fail "[intent_dict] 豁免项 $name 现在已有词条——豁免过期，请从 INTENT_NON_LEXICON 删除，否则日后删词条不会红"
      continue
    fi
    if ! intent_produced_in_source "$name"; then
      log_fail "[intent_dict] 豁免项 $name 在源码里没有任何 IntentType 产出点——它既无词条也不会被产出，属死常量"
    fi
  done

  # ③b 归并别名：落点必须是已声明常量，且该落点自己要有词条（否则归并到一个同样产不出的意图）
  if [[ -n "$pairs" ]]; then
    local src dst
    while read -r src dst; do
      [[ -z "${src:-}" || -z "${dst:-}" ]] && continue
      if ! printf '%s\n' "$consts" | grep -Fxq "$dst"; then
        log_fail "[intent_dict] 归并别名 $src 指向未声明的常量 $dst"
        continue
      fi
      if ! printf '%s\n' "$types" | grep -Fxq "$dst"; then
        log_fail "[intent_dict] 归并别名 $src → $dst，但 $dst 无词条：归并后仍然产不出该意图"
      fi
    done <<< "$pairs"
  fi

  # 值唯一性：两个常量映到同一个字符串值时，词条覆盖判断会互相顶包
  local vals val_dups
  vals="$(intent_const_block | grep -oE '"[^"]+"' | LC_ALL=C sort)"
  val_dups="$(printf '%s\n' "$vals" | LC_ALL=C uniq -d)"
  if [[ -n "$val_dups" ]]; then
    log_fail "[intent_dict] 字符串值常量出现重复值（词条覆盖会顶包）："
    printf '%s\n' "$val_dups" | sed 's/^/      - /'
  fi

  if [[ "$ERRORS" -eq "$err_before" ]]; then
    log_pass "[intent_dict] $(printf '%s\n' "$consts" | grep -c .) 个意图常量 / $(printf '%s\n' "$types" | grep -c .) 个词条 Type，覆盖完整（豁免 ${exempt_n} 项）"
  fi
}

echo "============================================================"
echo "  ENUM 一致性检查（OPT-DB-08 配套）"
echo "============================================================"
echo "迁移文件 : ${MIGRATION#"$(cd "$SCRIPT_DIR/.." && pwd)"/}"
echo "Go 源码  : ${USER_SERVER#"$(cd "$SCRIPT_DIR/.." && pwd)"/}"
echo ""

echo "[1/6] platform_type_enum ↔ ChannelTypeXxx"
check_pair "platform_type_enum" "$USER_SERVER/internal/model" "ChannelType"

echo ""
echo "[2/6] intent_major_enum ↔ IntentMajorXxx"
# 已知缺口：Go 侧不存在 IntentMajorXxx 常量（见 047 第 3 节说明）
check_pair "intent_major_enum" "$USER_SERVER/internal/service" "IntentMajor" "yes"

echo ""
echo "[3/6] message_status_enum ↔ MessageStatusXxx"
check_pair "message_status_enum" "$USER_SERVER/internal/model" "MessageStatus"

echo ""
echo "[4/6] embed_status_enum ↔ EmbedStatusXxx"
check_pair "embed_status_enum" "$USER_SERVER/internal/model" "EmbedStatus"

echo ""
echo "[5/6] source_type_enum ↔ SourceTypeXxx"
check_pair "source_type_enum" "$USER_SERVER/internal/model" "SourceType"

echo ""
echo "[6/6] IntentXxx 常量 ↔ DefaultIntents 词条 Type"
check_intent_dictionary

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
