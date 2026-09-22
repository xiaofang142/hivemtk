#!/usr/bin/env bash
# =============================================================
# check-gitleaks-config.sh —— .gitleaks.toml 配置闸（只管"锁的形状"，不管"扫没扫"）
#
# 立项原因（第四十一轮）：能让更多密钥**不被报出**的文件只有 .gitleaks.toml 一个，
# 而它恰好落在那道密钥门的覆盖面之外 —— user-server-ci.yml 的 on.push / on.pull_request
# 都带窄 paths 过滤，名单里没有 .gitleaks.toml，所以「只改豁免表」的那一笔提交
# 在 CI 上压根不会触发 gitleaks（实测：改配置那笔跑到的 checks 里查无 gitleaks 任务）。
# 后果是豁免表可以任意变宽而无人拦：手写一条 (?i)secret.* 或删掉 [extend] 那段，
# 门从此静默失去半径，且没有任何门会因此变红。
#
# 本闸不重跑密钥扫描（那是 user-server-ci 里 gitleaks-action 的活；它按 push 窗口扫，
# 只改配置的提交本身不引入新密钥，重跑等于空跑）。它只保证那把锁的四个性质，
# 每条对应一种实测过的静默失效形态：
#   ① [extend] 段里必须有 useDefault = true。
#      丢了它 = 规则集变空 = 门恒绿。本仓自己的取证记录：只删这段、allowlist 原样保留，
#      同一提交立刻从 `leaks found: 1` 变 `no leaks found`（见 .gitleaks.toml 头注释）。
#      useDefault = false 与"这一行不存在"同罪，一并判红。
#   ② [allowlist].regexes 每条必须是**精确字面串**：字符集只允许 [A-Za-z0-9_.-]，长度 ≥12。
#      出现任何正则元字符（* + ? [ ] ( ) | { } \ ^ $ / 空格 中文）即红 ——
#      豁免半径必须恰好等于那一个串值。这是本仓既定的豁免规矩（"只豁这一串本身，
#      不按文件、不按规则"），本闸把它从口头规矩变成可跑判据。
#   ③ 文件头声明的「当前 N 条」必须等于 regexes 实际条数。
#      触发实例：09-22 一天内豁免从 3 条长到 5 条而头注释仍写 3 条。口径与账不符时，
#      下一个读它的人会按错的数去核，等于把人往假结论上带。
#   ④ [allowlist] 段内除 description / regexes 外不许出现别的豁免面（paths / targets /
#      commits 等）。它们按路径或提交豁免，半径天然比按值宽，
#      要放开必须先把本闸改了并写清理由（红因会提示这一点）。
#
# 覆盖面口径（本闸摸不到什么，勿当全量保证）：
#   · 只读工作树的 .gitleaks.toml 一个文件。绿只证明"锁的形状合规"，
#     **不**证明 gitleaks 真跑过（触发面由随本闸新建的 gitleaks-config.yml 给出），
#     更不证明被豁的那个值真不是凭据 —— 后者靠豁免条目自带的逐条取证注释 + 人工核对。
#   · 不做全文历史扫描。存量历史命中（第四十一轮实测全量 1885 处 / 1162 个提交）
#     属另一条待拍板线，本闸既不新增也不结掉那笔账。
#   · 解析锚定「[allowlist] 段内、regexes = [ 之后、] 之前的每一行」，缩进任意，
#     但每行必须是一条 '''字面串'''（可选尾逗号）。数组写成一行多元素、或数组未闭合，
#     都会被解析成少一条或多一条 BADSHAPE → 判红，不会假绿（首轮实测就是靠这条
#     把"只认顶格 '''"的错口径抓出来的：那样解析出 2 条 ≠ 实际 5 条）。
#   · 只看 hivemtk 本仓这一份配置；platform 仓不跑 gitleaks，无需配套。
#
# 反向测试（改完必跑，四格都要按预期变红，红因必须读到下面四条 FAIL 文案之一）：
#   见文件末尾「反向测试执行口径」。禁对未提交文件跑 git checkout/restore，
#   还原一律用 cp 备份 + md5 比对。

set -eo pipefail

CONFIG=".gitleaks.toml"
MIN_LITERAL_LEN=12

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "$ROOT" ]]; then
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi
cd "$ROOT"

fail() { echo "FAIL gitleaks-config: $*" >&2; exit 1; }

if [[ ! -f "$CONFIG" ]]; then
  # 配置消失 = gitleaks 退回内置默认集，本仓那几条假凭据豁免全部失效，
  # CI 会立刻红在别处；但"文件不在了"本身必须先被本闸记下，不能当无事发生。
  fail "$CONFIG 不存在（豁免表消失等于密钥门换了一副口径）"
fi
if [[ ! -s "$CONFIG" ]]; then
  fail "$CONFIG 是 0 字节文件（空配置既无 useDefault 也无 allowlist）"
fi

# ---------- ① [extend] useDefault = true ----------
# 状态机只认 [extend] 段内的 useDefault，避免别处同名键混进来。
EXTEND_VERDICT="$(awk '
  /^[[:space:]]*#/ { next }
  /^[[:space:]]*\[/ {
    if (section == "extend") { print verdict }
    section = $0
    gsub(/[][[:space:]]/, "", section)
    verdict = "missing"
    next
  }
  section == "extend" && /^[[:space:]]*useDefault[[:space:]]*=/ {
    if ($0 ~ /^[[:space:]]*useDefault[[:space:]]*=[[:space:]]*true[[:space:]]*$/) verdict = "ok"
    else verdict = "not-true"
  }
  END { if (section == "extend") print verdict }
' "$CONFIG")"

case "$EXTEND_VERDICT" in
  ok) ;;
  not-true) fail "[extend] useDefault 存在但不是 true —— 规则集会变空，密钥门恒绿" ;;
  missing) fail "缺 [extend] useDefault = true —— gitleaks 将只按本文件里的空规则集扫描，密钥门静默零覆盖" ;;
  *) fail "[extend] 段解析异常（取值：'${EXTEND_VERDICT}'）—— 判据不认这种形状" ;;
esac

# ---------- ② + ③ [allowlist] 的 regexes 条目 ----------
# awk 里单引号无法直接写进 shell 单引号程序，故用 \047 表示三引号定界符。
# 输出协议：每条字面串原样一行；形状不合的行输出 "@@BAD@@<原因>\t<原始行>"。
RAW="$(awk '
  /^[[:space:]]*#/ { next }
  /^[[:space:]]*\[/ { in_allow = ($0 ~ /^[[:space:]]*\[allowlist\][[:space:]]*$/); next }
  !in_allow { next }
  !seen && /^[[:space:]]*regexes[[:space:]]*=/ { seen = 1; next }
  seen && /^[[:space:]]*\]/ { closed = 1; exit }
  seen {
    line = $0
    sub(/^[[:space:]]*/, "", line)
    sub(/[[:space:]]*$/, "", line)
    if (substr(line, 1, 3) != "\047\047\047") { print "@@BAD@@非三引号字面量条目\t" line; next }
    body = substr(line, 4)
    if (!match(body, /\047\047\047[[:space:]]*,?[[:space:]]*$/)) { print "@@BAD@@三引号未闭合\t" line; next }
    print substr(body, 1, RSTART - 1)
  }
  END { if (!seen) print "@@BAD@@[allowlist] 段内没有 regexes 数组" ; else if (!closed) print "@@BAD@@regexes 数组未闭合" }
' "$CONFIG")"

if printf '%s\n' "$RAW" | grep -v '^@@BAD@@' | awk 'NF == 0 { bad = 1 } END { exit bad ? 0 : 1 }'; then
  fail "regexes 条目里有空串（空字面量会匹配一切，等于关掉豁免面以外的整道门）"
fi

BADLINES="$(printf '%s\n' "$RAW" | grep '^@@BAD@@' || true)"
if [[ -n "$BADLINES" ]]; then
  fail "regexes 解析失败：$(printf '%s\n' "$BADLINES" | sed 's/^@@BAD@@//' | tr '\n' '; ')"
fi

ENTRIES="$(printf '%s\n' "$RAW" | grep -v '^@@BAD@@' || true)"
ENTRY_COUNT=0
if [[ -n "$ENTRIES" ]]; then
  ENTRY_COUNT="$(printf '%s\n' "$ENTRIES" | wc -l | tr -d ' ')"
fi

if [[ "$ENTRY_COUNT" -gt 0 ]]; then
  while IFS= read -r entry; do
    [[ -n "$entry" ]] || continue
    if [[ ! "$entry" =~ ^[A-Za-z0-9_.-]+$ ]]; then
      # 刻意不回显条目正文：豁免表条目本身可能是"看着像真凭据"的串，
      # 抄进 CI 日志等于多开一条泄露面。只报长度与判据。
      fail "regexes 有条目（长度 ${#entry}）含正则元字符或非常规字符 —— 豁免必须精确到一个字面串值，不接受模式"
    fi
    if [[ "${#entry}" -lt "$MIN_LITERAL_LEN" ]]; then
      fail "regexes 有条目长度 ${#entry} < ${MIN_LITERAL_LEN} —— 短串会顺手吃掉同前缀的真凭据"
    fi
  done <<< "$ENTRIES"
fi

DECLARED_RAW="$(awk '/当前 [0-9]+ 条/{ if (match($0, /当前 [0-9]+ 条/)) { print substr($0, RSTART, RLENGTH); exit } }' "$CONFIG")"
if [[ -z "$DECLARED_RAW" ]]; then
  fail "文件头缺「当前 N 条」声明 —— 豁免表必须自带条数口径，供本闸对账"
fi
DECLARED="$(printf '%s' "$DECLARED_RAW" | tr -dc '0-9')"
if [[ "$DECLARED" != "$ENTRY_COUNT" ]]; then
  fail "头注释声明「${DECLARED} 条」≠ regexes 实际 ${ENTRY_COUNT} 条 —— 口径与账不符，按实际条数把头注释改对"
fi

# ---------- ④ [allowlist] 段内只允许 description / regexes ----------
EXTRA_KEYS="$(awk '
  /^[[:space:]]*#/ { next }
  /^[[:space:]]*\[/ { in_allow = ($0 ~ /^[[:space:]]*\[allowlist\][[:space:]]*$/); next }
  in_allow && /^[[:space:]]*[A-Za-z_]+[[:space:]]*=/ {
    key = $0
    sub(/[[:space:]]*=.*$/, "", key)
    sub(/^[[:space:]]+/, "", key)
    if (key != "description" && key != "regexes") print key
  }
' "$CONFIG")"
if [[ -n "$EXTRA_KEYS" ]]; then
  fail "[allowlist] 出现按路径/提交豁免的键（$(printf '%s' "$EXTRA_KEYS" | tr '\n' ' ')）—— 半径比按值豁免宽得多，要放开先改本闸并写清理由"
fi

echo "scanned=1 file, entries=${ENTRY_COUNT} declared=${DECLARED} (extend=ok, allowlist keys=description+regexes)"
echo "OK gitleaks-config: 豁免表形状合规"
exit 0

# =============================================================
# 反向测试执行口径（每次改本闸都重跑一遍，四格红因都要读到）：
#   cp .gitleaks.toml /tmp/glcfg.bak && md5 -q .gitleaks.toml
#   R1  sed -i '' '/^useDefault = true$/d' .gitleaks.toml
#       → 期望 FAIL「缺 [extend] useDefault = true」
#   R2  在数组末尾插一条 '''(?i)redis-password-.*'''
#       → 期望 FAIL「含正则元字符或非常规字符」
#   R3  把头注释「当前 N 条」的数字改成与实际不符
#       → 期望 FAIL「声明≠实际」
#   R4  在 [allowlist] 段加一行 paths = ['''.github''']
#       → 期望 FAIL「出现按路径/提交豁免的键」
#   还原 cp /tmp/glcfg.bak .gitleaks.toml && md5 -q .gitleaks.toml（必须等于开头记下的值）
