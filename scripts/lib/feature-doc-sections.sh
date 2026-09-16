#!/usr/bin/env bash
# =============================================================================
# lib/feature-doc-sections.sh
# 功能文档「8 节模板」的**单一真源**（OPT-DOC-13 / OPT-DOC-14 / OPT-DOC-15 共用）
#
# 背景（2026-09-16 修复）：
#   三个脚本各自写了一份「某节是否存在」的判定，且三份互相矛盾 ——
#     · check-feature-doc.sh         用 `grep -q "$section"` 全文子串匹配
#       → 文档只要在目录/对照表里**提到**节名就算通过（实测 2/8 的文档被判为全绿）
#     · feature-doc-coverage-report.sh 锚定标题，但序号前缀允许「任意一节的数字」
#       → `## 六、数据模型` 会被算作 §五「数据模型」命中（覆盖率虚高）
#     · check-doc-consistency.sh     用 `"^## 一、\|功能完成状态"`
#       → BSD grep 的 BRE **不支持 `\|`**，整条模式退化成字面量，恒定不匹配
#         （对已含 §一 的文档误报缺失）
#   故抽成本文件：所有脚本一律 `source` 它，禁止再各自写正则。
#
# 判定规则（严格）：
#   节 i（0-based，对应 §(i+1)）命中，当且仅当存在一个 Markdown 标题行满足以下之一：
#     1) `## §三 设计标准` / `## §3 设计标准`   （§ + 本节序号）
#     2) `## 三、设计标准` / `## 3. 设计标准`   （本节序号 + 分隔符）
#     3) `## 设计标准`                          （无序号，纯节名）
#   即：**必须落在标题行上**；若带序号，序号必须与该节对应。
#   目录、对照表、正文里的提及一律不算。
#
# 用法：
#   source "$(dirname "$0")/lib/feature-doc-sections.sh"
#   if fd_section_present "$file" 2; then ... fi
# =============================================================================

FD_SECTIONS=( "功能完成状态" "核心原理" "设计标准" "架构与模块关系" "数据模型" "业务流程" "前端交互" "测试策略" )
FD_SHORT=(    "§一"          "§二"      "§三"      "§四"            "§五"      "§六"      "§七"      "§八" )
FD_NUMERALS=( "一"           "二"       "三"       "四"             "五"       "六"       "七"       "八" )
FD_REQUIRED_COUNT=6   # §一 ~ §六 为必填，§七 §八 为推荐

# fd_section_present <file> <index0>
# 返回 0 表示该节存在（命中标题），1 表示缺失
fd_section_present() {
  local f="$1" i="$2"
  local name="${FD_SECTIONS[$i]}"
  local num="${FD_NUMERALS[$i]}"

  # 1) §<本节序号> 名称   —— `## §三 设计标准`、`## §3 设计标准`
  if grep -qE "^#{1,6}[[:space:]]*§(${num}|$((i + 1)))[、.]?[[:space:]]*${name}" "$f" 2>/dev/null; then
    return 0
  fi
  # 2) <本节序号>、名称   —— `## 三、设计标准`、`## 3. 设计标准`
  if grep -qE "^#{1,6}[[:space:]]*(${num}|$((i + 1)))[、.][[:space:]]*${name}" "$f" 2>/dev/null; then
    return 0
  fi
  # 3) 纯名称（无序号）   —— `## 设计标准`
  if grep -qE "^#{1,6}[[:space:]]*${name}" "$f" 2>/dev/null; then
    return 0
  fi
  return 1
}

# fd_missing_sections <file> [<kind>]
# kind=required（默认）输出 §一~§六 中缺失的节名，每行一个；kind=all 输出全部 8 节
fd_missing_sections() {
  local f="$1" kind="${2:-required}"
  local limit=$FD_REQUIRED_COUNT
  [ "$kind" = "all" ] && limit=8
  local i
  for ((i = 0; i < limit; i++)); do
    if ! fd_section_present "$f" "$i"; then
      printf '%s\n' "${FD_SECTIONS[$i]}"
    fi
  done
}
