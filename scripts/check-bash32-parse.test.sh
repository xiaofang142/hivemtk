#!/usr/bin/env bash
# =============================================================================
# check-bash32-parse.test.sh —— 上一道门的九格反向测
#
# 为什么必须有：这道门的"绿"和"根本没判"长得非常像（CI 上它必定走 SKIP 分支）。
# 不拿假 bash 夹具双向跑一遍，就无法区分「3.2 读通了」与「解析器档位不对、什么都没读」。
#
# 格表（每格绑定"哪条判据分支该开火"，见门里的 ①②③）：
#   C1 干净夹具 + 3.2 解析器      ⇒ rc=0 且印出"全部读通"（正控制：门会绿）
#   C2 命令替换里嵌 case（本轮真实缺陷形状） ⇒ rc=1、点名该文件、抄出红因首行
#   C3 普通语法错（未闭合花括号）   ⇒ rc=1（证明判据不是只认那一种形状）
#   C4 假 bash 5 打在干净夹具上    ⇒ rc=0、必须印 SKIP、且整段输出里不许出现绿读数措辞
#   C5 假 bash 5 打在坏夹具上      ⇒ rc=0（同上一格的对照：坏文件在这一档**不会被抓到**）
#   C6 没有可用 bash              ⇒ rc=2 且明说"判据没跑过"
#   C7 面骤减（MIN_FILES 抬高）    ⇒ rc=2（枚举不可信不判绿）
#   C8 扫描根下没有 scripts/       ⇒ rc=2
#   C9 门脚本自己过 3.2 的 -n      ⇒ 常驻自检：这道门管别人，它自己也必须在被管的面里
#
# 执行入口：bash scripts/check-bash32-parse.test.sh（不联网、不碰真实仓的工作树）
# =============================================================================

set -uo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
GATE="$ROOT/scripts/check-bash32-parse.sh"
FAIL=0
WORK=$(mktemp -d /tmp/bash32parse-test.XXXXXX)
trap 'rm -rf "$WORK"' EXIT

ok()  { echo "  ✓ $1"; }
bad() { echo "  ✗ $1"; FAIL=$((FAIL + 1)); }

[ -f "$GATE" ] || { echo "FATAL: 找不到门脚本 $GATE"; exit 1; }

# 环境前提（不是可选装饰）：C1–C3 的判据**依赖本机真有一版 bash 3.x**。/bin/bash 是 5.x
# 的机器（ubuntu runner、装过 bash 的 mac）上跑这套格子，C2 会从"抓到缺陷"变成"抓不到"——
# 那是环境不满足，不是判据退化。所以这里先量一次，不满足就退 2 并写明，绝不让它退 0 冒充
# "九格全过"。也因此本用例**不接 CI**（CI 里它恒 rc=2），与门本身同属 mac 本地常驻件。
SYS_BASH_VER=$(/bin/bash -c 'printf %s "$BASH_VERSION"' 2>/dev/null)
case "$SYS_BASH_VER" in
  3.*) : ;;
  *)
    echo "ENV-BROKEN：本机 /bin/bash 不是 3.x（实测 ${SYS_BASH_VER:-读不到}）⇒ C1–C3 的判据前提不成立，不跑不判"
    exit 2
    ;;
esac

# 假仓库夹具：门用 `git ls-files` 取面，所以夹具必须是个 git 目录
new_fixture() {
  d="$WORK/$1"
  mkdir -p "$d/scripts"
  cp "$GATE" "$d/scripts/check-bash32-parse.sh"
  printf '#!/usr/bin/env bash\nset -eu\necho clean\n' > "$d/clean.sh"
  ( cd "$d" && git init -q . && git add -A >/dev/null 2>&1 )
  printf '%s' "$d"
}

# 本轮真缺陷的形状：命令替换里嵌 case（bash 5 合法、shellcheck 零 finding、3.2 断在 `)`）
seed_case_in_subshell() {
  printf '%s\n' \
    '#!/usr/bin/env bash' \
    'STAGE=/tmp' \
    'p=/tmp/x' \
    'leaked=$(printf "a\n" | while read -r l; do' \
    '  case "$p" in "$STAGE"/*) : ;; *) printf "%s\n" "$l" ;; esac' \
    'done)' > "$1/bad-case.sh"
}

seed_plain_syntax() {
  printf '%s\n' '#!/usr/bin/env bash' 'f() {' '  echo unclosed' > "$1/bad-brace.sh"
}

# 假 bash：版本问答答 5.x，其余动作转真 bash 执行（模拟 CI runner 的档位）
make_fake_bash5() {
  f="$WORK/fake-bash5"
  printf '%s\n' '#!/usr/bin/env bash' \
    'if [ "${1:-}" = "-c" ]; then' \
    '  printf "5.0.17(1)-release"' \
    '  exit 0' \
    'fi' \
    'exec /bin/bash "$@"' > "$f"
  chmod +x "$f"
  printf '%s' "$f"
}

run_gate() {   # run_gate <夹具目录> [BASH32_BIN=...]
  d=$1
  shift
  ( cd "$d" && env "$@" MIN_FILES=1 bash scripts/check-bash32-parse.sh 2>&1 )
}

echo "C1 干净夹具 + 本机 3.2 ⇒ 门会绿（正控制）"
d=$(new_fixture c1)
out=$(run_gate "$d"); rc=$?
[ "$rc" = 0 ] && ok "rc=0（真实读数：$(printf '%s' "$out" | sed -n '1p' | cut -c1-60)…）" \
              || bad "干净夹具被判红：rc=${rc} / $(printf '%s' "$out" | sed -n '1,3p' | tr '\n' ' ')"
printf '%s' "$out" | grep -q '全部读通' && ok '绿读数带上了面数与"读通"字样' || bad 'rc=0 却没打印判据结论'

echo "C2 命令替换里嵌 case（本轮真实缺陷）⇒ 必须红且点名"
d=$(new_fixture c2)
seed_case_in_subshell "$d"
out=$(run_gate "$d"); rc=$?
[ "$rc" = 1 ] && ok 'rc=1（缺陷没被当成绿）' || bad "期望 rc=1，实得 rc=${rc}"
printf '%s' "$out" | grep -q 'bad-case\.sh' && ok '点名了坏文件' || bad '红但没说是哪个文件'
printf '%s' "$out" | grep -qi 'syntax error' && ok '红因首行抄进了输出' || bad '只报"有坏"没抄红因：'"$(printf '%s' "$out" | tail -2 | tr '\n' ' ')"

echo "C3 未闭合花括号（另一种语法错）⇒ 也必须红"
d=$(new_fixture c3)
seed_plain_syntax "$d"
out=$(run_gate "$d"); rc=$?
[ "$rc" = 1 ] && printf '%s' "$out" | grep -q 'bad-brace\.sh' && ok '第二类形状同样被抓（rc=1 且点名）' \
  || bad "期望 rc=1＋点名，实得 rc=${rc} / $(printf '%s' "$out" | tail -2 | tr '\n' ' ')"

FAKE=$(make_fake_bash5)
echo "C4 假 bash 5 打在干净夹具上 ⇒ SKIP，且不许出现绿读数措辞"
d=$(new_fixture c4)
out=$(run_gate "$d" BASH32_BIN="$FAKE"); rc=$?
[ "$rc" = 0 ] && ok 'rc=0（无牙档不拦人）' || bad "SKIP 档不该非 0，实得 rc=${rc}"
printf '%s' "$out" | grep -q 'SKIP' && ok '打印里有 SKIP 字样' || bad '没打 SKIP：'"$(printf '%s' "$out" | tr '\n' ' ')"
if printf '%s' "$out" | grep -qE '✅|全部读通|绿'; then
  bad "SKIP 档里出现了绿读数措辞：$(printf '%s' "$out" | grep -E '✅|全部读通|绿' | head -1)"
else
  ok 'SKIP 档不打印任何"通过/读通/绿"字样（假绿形状已封）'
fi

echo "C5 假 bash 5 打在坏夹具上 ⇒ 抓不到（证明'CI 恒绿'是假绿，不是覆盖）"
d=$(new_fixture c5)
seed_case_in_subshell "$d"
out=$(run_gate "$d" BASH32_BIN="$FAKE"); rc=$?
[ "$rc" = 0 ] && printf '%s' "$out" | grep -q 'SKIP' \
  && ok '坏文件在 5.x 档下退 0＋SKIP：这道门在 CI 里没有判出任何东西，只能靠本地跑' \
  || bad "无牙档行为不符预期：rc=${rc} / $(printf '%s' "$out" | tr '\n' ' ')"

echo "C6 没有可用 bash ⇒ rc=2 且明说判据没跑过"
mkdir -p "$WORK/emptybin"
d=$(new_fixture c6)
out=$(cd "$d" && env -i PATH="$WORK/emptybin" MIN_FILES=1 HOME="$HOME" /bin/bash scripts/check-bash32-parse.sh 2>&1); rc=$?
[ "$rc" = 2 ] && ok 'rc=2（缺工具＝没跑过，不并入绿也不并入红）' || bad "期望 rc=2，实得 rc=${rc}"
printf '%s' "$out" | grep -q '判据没跑过' && ok '红因写明"判据没跑过"' || bad "rc=2 但没说明原因：$out"

echo "C7 面骤减 ⇒ rc=2（枚举不可信）"
d=$(new_fixture c7)
out=$(cd "$d" && env MIN_FILES=99999 bash scripts/check-bash32-parse.sh 2>&1); rc=$?
[ "$rc" = 2 ] && printf '%s' "$out" | grep -q '面骤减' && ok 'rc=2 且写明面骤减' \
  || bad "期望 rc=2＋骤减原因，实得 rc=${rc} / $(printf '%s' "$out" | tr '\n' ' ')"

echo "C8 扫描根下没有 scripts/ ⇒ rc=2"
# 门是按 `$0` 的上两级推根的，所以"跑真脚本"永远推得出真仓 —— 必须把门挪到一个
# 父目录下没有 scripts/ 的位置（tools/），才测得到那条"根不对 ⇒ rc=2"分支。
mkdir -p "$WORK/c8root/tools"
cp "$GATE" "$WORK/c8root/tools/x.sh"
out=$(cd "$WORK/c8root" && env MIN_FILES=1 bash tools/x.sh 2>&1); rc=$?
[ "$rc" = 2 ] && ok "rc=2（根不对时不判绿）：$(printf '%s' "$out" | sed -n '1p')" \
  || bad "期望 rc=2，实得 rc=${rc} / $(printf '%s' "$out" | tr '\n' ' ')"

echo "C9 门自己在被管的面里（自我覆盖，防'管别人不管自己'）"
if /bin/bash -n "$GATE" 2>/dev/null; then ok '门脚本自身过 /bin/bash 3.2 -n'; else bad '门脚本自己读不通'; fi
if /bin/bash -n "$ROOT/scripts/check-bash32-parse.test.sh" 2>/dev/null; then
  ok '本用例自身也过 3.2 -n'
else
  bad '本用例自己读不通（那它在 mac 上根本跑不起来）'
fi
# 面是 `git ls-files` 现取的 ⇒ 新增 *.sh 自动进面，不需要任何登记；这里断言的只是
# "门与它的用例这两份字节本身读得通"，别长成"它管别人、自己没人管"。

echo
if [ "$FAIL" = 0 ]; then
  echo "===== 用例：九格全过（断言失败 0 处）====="
  exit 0
fi
echo "===== 用例：$FAIL 处断言失败 ====="
exit 1
