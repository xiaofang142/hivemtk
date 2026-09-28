#!/usr/bin/env bash
# =============================================================================
# check-bash32-parse.sh —— 全仓 shell 文件必须能被 macOS 出厂的那版解析器读通
#
# 立项原因（2026-09-24 实测，不是假想敌）：本轮改 scripts/rotate-secrets.sh 时写进了一句
# 「命令替换里嵌 case」：
#     leaked=$(... | while read -r ef; do ... case "$p" in "$STAGE"/*) : ;; *) ... ;; esac)
# 这一句在 CI（ubuntu，bash 5.x）里合法，在 shellcheck 0.11.0 里 **0 条 finding**，
# 而 mac 开发机上 `/bin/bash -n` 直接：
#     syntax error near unexpected token `)'
# 也就是说：一门已存在的静态分析门（error 级零容忍）与 CI 的解析器**都看不见这个缺陷**，
# 而部署／装机／口令轮换这类脚本恰恰是在 mac 上由人直接 `bash scripts/x.sh` 跑的。
# 发现它的唯一一步是把文件交给 3.2 读一遍——那就把它做成门。
#
# 判据（三条，都来自实测）：
#   ① 解析器按 **major 版本** 选：只有 bash 3.x 这一档有牙（它就是本机踩坑的那版）。
#      解析器 ≥ 4（CI runner 的形状）时本门**不许打印绿读数**，只能打 `SKIP(...)` 并退 0：
#      拿 bash 5 的"全过"冒充 3.2 的"全过"，正是本轮 shellcheck 已经犯过一次的那类假绿。
#      ⇒ 这条 SKIP 分支由 C5/C6 用假 bash 夹具双向验（既验它打 SKIP，也验它不打绿）。
#   ② 逐文件 `"$PARSER" -n`，任一文件退非 0 ⇒ 本门 rc=1，并把该文件 stderr 首行抄出来
#      （只报"有几处坏"＝把红因扔掉，下一位还得自己复跑一遍才知道坏在哪句）。
#   ③ 枚举与扫描根对账沿用姊妹门口径：空面／面骤减／找不到解析器 ⇒ rc=2（"没跑过"
#      不是绿，也不是红，是环境坏了）。
#
# 扫描面：`git ls-files --cached --others --exclude-standard` 里的 *.sh / *.bash，
#   与 check-shellcheck.sh、check-shell-cjk-expansion.sh 同一条口径（os.walk 会把仓根
#   那棵 gitignore 的取证快照树卷进来，实测同一棵树两次扫 136 → 538）。
#   代价与被忽略的 shell 文件：本门同样看不见，属姊妹门的共享盲区。
#
# 覆盖面与盲区：
#   - 只判**解析层**：`bash -n` 不做展开、不跑逻辑，运行期错误（未定义变量、
#     管道子 shell 的 set -u 语义）一律不在判据内 —— 那些属 shellcheck 与用例。
#   - 只对 3.2 这一档有效：4.x/5.x 独有的其它形状（如 `${VAR@Q}`）本门测不到，
#     除非开发机装了对应版本的 bash 并用 BASH32_BIN 指过去（那时判据仍按 major 分类）。
#   - 解析器 ≥4 的那一档（ubuntu runner）本门恒 SKIP ⇒ 它在那儿**不算执行点**。2026-09-28 起
#     它有两个真执行点：本地 `make audit`，以及 lint.yml 的 `bash32-parse` 作业（`macos-latest`
#     的 /bin/bash 就是 3.2.57，与本机同版；该作业第一格先实测版本，非 3.x 直接红，
#     不让"恒 SKIP"伪装成绿步骤）。在那之前只有 `make audit` 一侧，CI 侧属 §23.22 第 5 段②
#     记下的"注册了但从未开火"缺口。
#
# 用法：bash scripts/check-bash32-parse.sh
#   rc=0 解析器是 3.x 且全面读通（或解析器不是 3.x ⇒ SKIP，退 0 但不印绿）；
#   rc=1 至少一个文件读不通；rc=2 环境或判据本身有问题（没有 bash、扫描根不对、面骤减）
#
# 反向测试（改完本脚本必须逐格真跑）：bash scripts/check-bash32-parse.test.sh
#   前提：本机 /bin/bash 是 3.x。不满足时用例退 2 并自报 ENV-BROKEN（不是"九格全过"）。
#   两个执行点：本地 `make audit`，以及 lint.yml 的 `bash32-parse` 作业（macos-latest，
#   第一格先实测 `$BASH_VERSION` 非 3.x 即红 ⇒ 不会以 ENV-BROKEN/SKIP 的形状混成绿）。
# =============================================================================

set -uo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
PARSER="${BASH32_BIN:-/bin/bash}"
[ -x "$PARSER" ] || PARSER=$(command -v bash || true)
MIN_FILES=${MIN_FILES:-60}   # 本仓实测 140；骤减到 60 以下＝扫描根或 git 口径坏了

if [ -z "$PARSER" ]; then
  echo "::error::找不到可用的 bash（BASH32_BIN 未设且 /bin/bash 与 PATH 里都没有）⇒ 判据没跑过，不判绿" >&2
  exit 2
fi
if [ ! -d "$ROOT/scripts" ]; then
  echo "::error::扫描根不对：$ROOT 下没有 scripts/ ⇒ 判据没跑过，不判绿" >&2
  exit 2
fi

VER=$("$PARSER" -c 'printf %s "$BASH_VERSION"' 2>/dev/null)
case "$VER" in
  3.*) TEETH=yes ;;
  *)   TEETH=no ;;
esac

cd "$ROOT" || exit 2
FILES=$(git ls-files --cached --others --exclude-standard | grep -E '\.(sh|bash)$' | sort)
N=$(printf '%s\n' "$FILES" | grep -c . || true)
if [ "$N" -lt "$MIN_FILES" ]; then
  echo "::error::枚举到 $N 个 shell 文件（下限 ${MIN_FILES}）⇒ 面骤减，判据不可信" >&2
  exit 2
fi

echo "解析器：${PARSER}（bash ${VER:-未知}）· 有牙档位=bash 3.x · 面=${N} 个 shell 文件"

if [ "$TEETH" = no ]; then
  # 关键措辞：这一支里"全过/绿/通过"一个都不许出现，否则 CI 日志里它和真读数长一样。
  echo "SKIP：本机解析器不是 bash 3.x，覆盖不到那条只在 3.2 上断掉的语法面 ⇒ 本门这一趟没有判出任何东西"
  exit 0
fi

BAD=0
while IFS= read -r f; do
  [ -n "$f" ] || continue
  err=$("$PARSER" -n "$f" 2>&1) || {
    BAD=$((BAD + 1))
    printf '::error::%s 读不通：%s\n' "$f" "$(printf '%s' "$err" | sed -n '1p')"
  }
done <<EOF
$FILES
EOF

if [ "$BAD" -ne 0 ]; then
  echo "红：$BAD/$N 个文件在 bash ${VER} 下解析失败（上面逐条点名，红因已抄出）"
  exit 1
fi
echo "✅ $N 个 shell 文件在 bash ${VER} 下全部读通（解析层，不含运行期判据）"
