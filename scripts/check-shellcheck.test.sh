#!/usr/bin/env bash
# =============================================================================
# check-shellcheck.test.sh —— check-shellcheck.sh 的反向测试（证明它会红）
#
# 为什么需要这份文件：一道只会退 0 的门和一道没装的门，输出长得一样。本仓的规矩是
# 「新校验必须逐格真跑反向」，且每格要断言**红因**（只比 rc 会把"环境坏了的红"
# 当成"判据有牙的红"）。16 格各自钉住闸的一条分支：
#   T0 真树绿；T0b 闸自己印的 scanned/checked 两个数与**独立算的** git ls-files 计数对账
#      （证明它真扫了那么多文件，而不是扫了三个就宣布干净）
#   T1 缺 shell 声明的探针 ⇒ rc=1 且点名 SC2148（T1b 还要求红因点到探针本身）
#   T2 语法解析不了的探针 ⇒ rc=1 且点名 SC107x（证明不是只认 SC2148 一种码）
#   T3 含非法 UTF-8 字节的探针 ⇒ rc=2（闸的判据①：实测工具对这种文件退 0，
#      不拦就是把"没分析过"记成"没问题"；这一格把"它确实退 0"重新量一遍再往下走）
#   T4 PATH 里摘掉工具所在目录 ⇒ rc=2 且说"找不到"（缺工具不许退 0＝SKIP 不是 PASS）
#   T5 扫描根不在 git 仓里 ⇒ rc=2 且说 git ls-files 失败（枚举不出来不许判绿）
#   T6 只枚举到 3 个文件（< 下界 50）⇒ rc=2（面骤减＝枚举坏了，不是债清完了）
#   T7a/b/c 假工具喂 rc×finding 数的错配 ⇒ rc=2「自相矛盾」/「不是合法 JSON」
#   T7d 假工具退 2（它自己没检查成）⇒ 闸退 2，不许并进"绿"
#   T8/T8b/T8c 假工具回记 argv：断言每次调用都带 `-f json -S error`（旋钮不是摆设）、
#      全绿路径退 0、首行印得出工具版本号
#   T9 清完夹具后真树必须还是绿的（防"测试把仓库改红了自己不知道"）
#
# 用法：bash scripts/check-shellcheck.test.sh
#   rc=0 全部格子符合期望；rc=1 有格不符（每格印 PASS/FAIL + 实际 rc + 输出前 6 行）
#
# 夹具纪律（本仓踩过的死法，这里逐条避开）：
#   - 探针是**未跟踪但未被 ignore** 的临时文件，收尾只 `rm -f`；绝不对工作树跑
#     git checkout/restore/stash（那会连别人在改的行一起抹掉）。
#   - 每格先断言夹具真的落到了磁盘上、且真的进了扫描面（不然证的是别的分支）。
#   - 判"在不在清单里"用内存里的整份清单做子串匹配，不拿 `... | grep -q`：pipefail 下
#     grep 一命中就退，上游收到 SIGPIPE 的 141 就是整条管道的 rc（老坑换个地方复发）。
#   - 想验"参数没落进去"不许靠"摘掉真工具的参数"：零债树上每个文件都无输出，那种变异
#     不可观测（实测跑出来 rc=0）；要换假工具把形状直接喂进判据分支。
#   - 别用 env -i 或 PATH=/usr/bin:/bin 构造"缺工具"：/usr/bin 下是 Xcode 的 shim，
#     环境一窄闸自己的 python3 先退 69（"not agreed to the Xcode license"），红因不在分支上。
#     也不许"把装着 shellcheck 的 PATH 目录摘掉"——CI 的 runner 镜像在 /usr/bin 与 /bin 里
#     都自带 shellcheck，摘掉＝把 bash/python3/git 一起摘掉（本电池第一版就是在 CI 红在
#     `bash: command not found`、本地 mac 全绿）。T4 改成自建"只缺 shellcheck"的腿内目录。
# =============================================================================

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATE="$SCRIPT_DIR/check-shellcheck.sh"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
[ -f "$GATE" ] || { echo "FAIL 找不到被测闸 $GATE"; exit 1; }

# 工具得在 PATH 里才能跑（T4 那格自己把它摘一次，用来证明"缺工具"是 rc=2 不是 rc=0）
SC_PATH="$HOME/.local/bin:$PATH"
WORK="/tmp/check-shellcheck.test.$$"
mkdir -p "$WORK"
OUT="$WORK/out.txt"

PASS_COUNT=0
FAIL_COUNT=0

# report <格名> <期望rc> <期望红因子串> <实际rc> <输出文件>
report() {
  local name="$1" want_rc="$2" want_msg="$3" got_rc="$4" file="$5"
  if [ "$got_rc" != "$want_rc" ]; then
    printf 'FAIL %-46s rc=%s（期望 %s）\n' "$name" "$got_rc" "$want_rc"
    sed -n '1,6p' "$file" | sed 's/^/       | /'
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  if [ -n "$want_msg" ] && ! grep -qF -- "$want_msg" "$file"; then
    printf 'FAIL %-46s rc 对上了但红因里没有「%s」\n' "$name" "$want_msg"
    sed -n '1,6p' "$file" | sed 's/^/       | /'
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  printf 'PASS %-46s rc=%s\n' "$name" "$got_rc"
  PASS_COUNT=$((PASS_COUNT + 1))
}

count_scan_surface() {
  git -C "$ROOT" ls-files --cached --others --exclude-standard \
    | grep -E '\.(sh|bash)$' | wc -l | tr -d ' '
}

# 探针放 scripts/ 下，名字带 tmp-shellcheck-reverse 前缀，ASCII 内容（不掺中文，免得和姊妹门耦合）
PROBE="$ROOT/scripts/tmp-shellcheck-reverse-probe.sh"
cleanup() {
  rm -f "$PROBE" "$OUT"
  rm -rf "$WORK"
}
trap cleanup EXIT

# ---- 开工前先确认工作树本来是干净的（绿门），否则后面每格的期望都不成立 ----
[ -f "$PROBE" ] && { echo "FAIL 探针位已被占用：$PROBE 已存在，先人工确认归属"; exit 1; }

# ===================== T0：真树绿 + 计数对账 =====================
SURFACE=$(count_scan_surface)
(cd "$ROOT" && PATH="$SC_PATH" bash "$GATE") > "$OUT" 2>&1
RC=$?
report "T0 真树：零 error 且计数对账" 0 "✅" "$RC" "$OUT"
G_SCANNED=$(sed -n 's/.*scanned=\([0-9]*\).*/\1/p' "$OUT" | head -1)
G_CHECKED=$(sed -n 's/.*checked=\([0-9]*\).*/\1/p' "$OUT" | head -1)
if [ "$G_SCANNED" = "$SURFACE" ] && [ "$G_CHECKED" = "$SURFACE" ]; then
  printf 'PASS %-46s scanned=checked=%s（独立口径 %s）\n' "T0b 两个计数 == 独立 ls-files 计数" "$G_CHECKED" "$SURFACE"
  PASS_COUNT=$((PASS_COUNT + 1))
else
  printf 'FAIL %-46s scanned=%s checked=%s 独立=%s\n' "T0b 两个计数 == 独立 ls-files 计数" \
    "${G_SCANNED:-无}" "${G_CHECKED:-无}" "$SURFACE"
  FAIL_COUNT=$((FAIL_COUNT + 1))
fi

# ===================== T1：缺 shell 声明（SC2148）=====================
printf 'echo probe\n' > "$PROBE"
[ -f "$PROBE" ] || { echo "FAIL T1 夹具没落盘"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
# 判"探针在不在扫描面里"用**内存里的整份清单**做子串匹配，不用 `... | grep -qF`：
# pipefail 下 grep 一命中就退，git 收到 SIGPIPE 的 141 会成为整条管道的 rc，
# `if !` 会把它读成"没命中"——夹具明明在场，却报成"被 ignore 吃了"（本仓老坑，形态换了个地方）。
SURFACE_LIST=$(git -C "$ROOT" ls-files --cached --others --exclude-standard | grep -E '\.(sh|bash)$' || true)
case "$SURFACE_LIST" in
  *tmp-shellcheck-reverse-probe.sh*)
    (cd "$ROOT" && PATH="$SC_PATH" bash "$GATE") > "$OUT" 2>&1
    RC=$?
    report "T1 缺 shell 声明 ⇒ rc=1 SC2148" 1 "SC2148" "$RC" "$OUT"
    grep -qF -- 'tmp-shellcheck-reverse-probe.sh' "$OUT" || {
      printf 'FAIL %-46s 红因没点到探针\n' "T1b 红因点名探针文件"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
    ;;
  *)
    printf 'FAIL %-46s 探针不在扫描面里（.gitignore 把它吃了？）\n' "T1 缺 shell 声明 ⇒ rc=1 SC2148"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    ;;
esac
rm -f "$PROBE"

# ===================== T2：语法解析不了（SC107x，换一族码）=====================
printf '#!/usr/bin/env bash\nif true; then\n' > "$PROBE"
(cd "$ROOT" && PATH="$SC_PATH" bash "$GATE") > "$OUT" 2>&1
RC=$?
report "T2 解析失败 ⇒ rc=1 SC107x" 1 "SC107" "$RC" "$OUT"
rm -f "$PROBE"

# ===================== T3：非法 UTF-8 字节 ⇒ 必须 rc=2，不许退 0 =====================
printf '#!/usr/bin/env bash\nLC_ALL=C printf "x"\n' > "$PROBE"
printf '\xff\xfe\n' >> "$PROBE"
DECODEABLE=$(python3 -c 'import sys
try:
    open(sys.argv[1], encoding="utf-8").read(); print("yes")
except UnicodeDecodeError:
    print("no")' "$PROBE")
if [ "$DECODEABLE" != "no" ]; then
  printf 'FAIL %-46s 夹具其实能按 UTF-8 读出来，这格没测到东西\n' "T3 非法 UTF-8 ⇒ rc=2"
  FAIL_COUNT=$((FAIL_COUNT + 1))
else
  # 先证明 shellcheck 自己对它是退 0 的（这条断言＝"必须拦"的理由，不是想当然）
  (cd "$ROOT" && PATH="$SC_PATH" shellcheck -f json -S error -- "$PROBE") >/dev/null 2>&1
  SC_RC=$?
  [ "$SC_RC" = "0" ] || printf '注：本机 shellcheck 对非法 UTF-8 退了 %s（不再是 0），T3 的拦法仍按 rc=2 判\n' "$SC_RC"
  (cd "$ROOT" && PATH="$SC_PATH" bash "$GATE") > "$OUT" 2>&1
  RC=$?
  report "T3 非法 UTF-8 ⇒ rc=2（不是「没问题」）" 2 "无法判它" "$RC" "$OUT"
fi
rm -f "$PROBE"

# ===================== T4：PATH 里没有 shellcheck ⇒ rc=2 =====================
# 形状：从"其它格喂给闸的那条 PATH（SC_PATH）"里摘掉所有装着 shellcheck 的目录，
#       再在最前面垫一个腿内目录（bash/python3/git/dirname 的绝对路径软链）。
# 为什么要垫：CI 的 ubuntu runner 上 /usr/bin 与 /bin 里**都躺着** shellcheck（镜像自带），
#   只摘不垫＝把闸自己需要的 bash/python3/git/dirname 一起摘掉。本电池第一版就是这么在
#   CI 红成 `line 179: bash: command not found`、rc=127（本地 mac 却全绿：那件工具在本机
#   只装在 ~/.local/bin，不存在"与核心工具同居"这个前提）——量到的是"环境坏了"，不是被测分支。
# 为什么一律用**绝对路径**调 bash（"$BASH_ABS" 而不是 PATH=... bash）：
#   `PATH=<新值> bash …` 里"新值是否参与 bash 这个命令本身的查找"，本机 bash 3.2 与 runner
#   的 bash 5 语义不同（实测同一构造本机 rc=2、CI rc=127）。平台语义不该进到判据里，
#   所以腿内一律给绝对路径，让两条平台走同一条查找路径。
# 为什么 python3 单独解析：本机它是 pyenv 的 shim，shim 自己要 PATH 上的帮手，
#   腿内 PATH 放个 shim 会把闸卡在"起解释器"（实测挂 40s+，既非红因也非绿）⇒ 取 sys.executable。
# 为什么不用 env -i / PATH=/usr/bin:/bin：mac 上那是 Xcode shim 的 rc=69（见头注纪律）。
LEGBIN="$WORK/legbin"
mkdir -p "$LEGBIN"
BASH_ABS="$(command -v bash || true)"
REPAIR_OK=1
for b in bash git dirname; do
  abs="$(command -v "$b" || true)"
  [ -n "$abs" ] || { REPAIR_OK=0; continue; }
  ln -sf "$abs" "$LEGBIN/$b"
done
# python3 单独解析：本机它是 pyenv 的 shim（~/.pyenv/shims/python3），shim 自己要 PATH 上的
# 帮手，腿内 PATH 里放个 shim 会让闸卡在"起解释器"这一步（实测挂 120s+，不是红因也不是绿）。
# 取 sys.executable 指向的**真解释器**（CI 上就是 /usr/bin/python3，两条路同形）。
PY_ABS="$(python3 -c 'import sys; print(getattr(sys, "_base_executable", "") or sys.executable)' 2>/dev/null || true)"
if [ -z "$PY_ABS" ] || [ ! -x "$PY_ABS" ]; then
  REPAIR_OK=0
else
  ln -sf "$PY_ABS" "$LEGBIN/python3"
fi
if [ "$REPAIR_OK" = "0" ] || [ -z "$BASH_ABS" ]; then
  printf 'FAIL %-46s 环境本身缺 bash/python3/git/dirname，本格无法构造\n' "T4 缺 shellcheck ⇒ rc=2"
  FAIL_COUNT=$((FAIL_COUNT + 1))
else
  NO_SC_PATH=""; DROPPED=0
  OLD_IFS="$IFS"
  IFS=:
  for p in $SC_PATH; do
    [ -n "$p" ] || continue
    if [ -x "$p/shellcheck" ]; then
      DROPPED=$((DROPPED + 1))
      continue
    fi
    NO_SC_PATH="${NO_SC_PATH:+$NO_SC_PATH:}$p"
  done
  IFS="$OLD_IFS"
  LEGPATH="$LEGBIN${NO_SC_PATH:+:$NO_SC_PATH}"
  if [ "$DROPPED" = "0" ]; then
    printf 'FAIL %-46s SC_PATH 里本来就没有 shellcheck（T0 那格应先红），无法构造"缺工具"\n' "T4 缺 shellcheck ⇒ rc=2"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  elif ! PATH="$LEGPATH" "$BASH_ABS" -c \
      'command -v dirname >/dev/null && command -v python3 >/dev/null && command -v git >/dev/null && ! command -v shellcheck >/dev/null'; then
    printf 'FAIL %-46s 腿内 PATH 没构造对（核心工具不齐，或仍找得到 shellcheck）\n' "T4 缺 shellcheck ⇒ rc=2"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  else
    (cd "$ROOT" && PATH="$LEGPATH" SHELLCHECK_PATH= "$BASH_ABS" "$GATE") > "$OUT" 2>&1
    RC=$?
    report "T4 缺 shellcheck ⇒ rc=2 不判绿" 2 "找不到 shellcheck" "$RC" "$OUT"
  fi
fi

# ===================== T5：扫描根不在 git 仓里 ⇒ rc=2 =====================
NODIR="$WORK/nogit"
mkdir -p "$NODIR/scripts"
cp "$GATE" "$NODIR/scripts/check-shellcheck.sh"
OUT5="$WORK/nogit.out"
(cd "$NODIR" && PATH="$SC_PATH" bash "$NODIR/scripts/check-shellcheck.sh") > "$OUT5" 2>&1
RC=$?
report "T5 无 git 仓 ⇒ rc=2（枚举不确定）" 2 "git ls-files 失败" "$RC" "$OUT5"

# ===================== T6：枚举面骤减（< 下界）⇒ rc=2 =====================
MINI="$WORK/mini"
mkdir -p "$MINI/scripts"
cp "$GATE" "$MINI/scripts/check-shellcheck.sh"
printf '#!/usr/bin/env bash\necho a\n' > "$MINI/scripts/a.sh"
printf '#!/usr/bin/env bash\necho b\n' > "$MINI/scripts/b.sh"
printf '#!/usr/bin/env bash\necho c\n' > "$MINI/scripts/c.sh"
git -c init.defaultBranch=main -c user.email=t@t -c user.name=t -C "$MINI" init -q
git -C "$MINI" add -A
OUT6="$WORK/mini.out"
(cd "$MINI" && PATH="$SC_PATH" bash "$MINI/scripts/check-shellcheck.sh") > "$OUT6" 2>&1
RC=$?
report "T6 只枚举到 3 个文件 ⇒ rc=2 面骤减" 2 "只枚举到" "$RC" "$OUT6"

# ============ T7/T8：假 shellcheck 直接喂判据分支（rc／输出／参数形状）============
# 为什么用假工具而不是"改闸副本"：真树上 error 面为零，把副本的 `-f json` 摘掉后
# 每个文件都是"无输出"，闸照样退 0 —— 那种变异**不可观测**，证不到任何分支（实测过）。
# 闸留了 SHELLCHECK_PATH 这个测试钩子（且它优先于 PATH，见闸内注释），所以能直接喂形状。
FAKE="$WORK/fake-shellcheck"
make_fake() {  # $1=body 文件内容（不含 --version 分支，统一由本函数垫）
  printf '#!/bin/sh\nif [ "$1" = "--version" ]; then echo "ShellCheck - fake"; echo "version: 0.0.0-fake"; exit 0; fi\n' > "$FAKE"
  printf '%s\n' "$1" >> "$FAKE"
  chmod +x "$FAKE"
  [ -x "$FAKE" ] || return 1
  FAKE_MD5=$(md5 -q "$FAKE" 2>/dev/null || md5sum < "$FAKE" | cut -d' ' -f1)
}

# T7a rc=0 却带着 finding ⇒ 走"自相矛盾"那条对账
make_fake 'echo "[{\"file\":\"x\",\"line\":1,\"column\":1,\"level\":\"error\",\"code\":9999,\"message\":\"fake finding\"}]"
exit 0'
OUT7A="$WORK/fake-rc0-with-finding.out"
(cd "$ROOT" && PATH="$SC_PATH" SHELLCHECK_PATH="$FAKE" bash "$GATE") > "$OUT7A" 2>&1
RC=$?
report "T7a rc=0 带 finding ⇒ rc=2 自相矛盾" 2 "自相矛盾" "$RC" "$OUT7A"

# T7b rc=1 却输出非 JSON ⇒ 走"输出不是合法 JSON"
make_fake 'echo "this is not json at all"
exit 1'
OUT7B="$WORK/fake-not-json.out"
(cd "$ROOT" && PATH="$SC_PATH" SHELLCHECK_PATH="$FAKE" bash "$GATE") > "$OUT7B" 2>&1
RC=$?
report "T7b 输出非 JSON ⇒ rc=2" 2 "不是合法 JSON" "$RC" "$OUT7B"

# T7c rc=1 且 finding 集为空 ⇒ 对账的另一半（不能只测一边）
make_fake 'echo "[]"
exit 1'
OUT7C="$WORK/fake-rc1-empty.out"
(cd "$ROOT" && PATH="$SC_PATH" SHELLCHECK_PATH="$FAKE" bash "$GATE") > "$OUT7C" 2>&1
RC=$?
report "T7c rc=1 空 finding ⇒ rc=2 自相矛盾" 2 "自相矛盾" "$RC" "$OUT7C"

# T7d rc=2（工具自己没检查成）⇒ 闸不许并进"绿"，也不许当成"有 bug"
make_fake 'echo "fake: cannot read" >&2
exit 2'
OUT7D="$WORK/fake-rc2.out"
(cd "$ROOT" && PATH="$SC_PATH" SHELLCHECK_PATH="$FAKE" bash "$GATE") > "$OUT7D" 2>&1
RC=$?
report "T7d 工具 rc=2 ⇒ 闸 rc=2（不是没问题）" 2 "无法判它" "$RC" "$OUT7D"

# T8 假工具把收到的 argv 记下来 ⇒ 证明 -f json / -S error 这两个旋钮不是摆设
ARGV="$WORK/argv.log"; : > "$ARGV"
make_fake 'printf "%s\n" "$*" >> "${FAKE_ARGV_LOG:?}"
echo "[]"
exit 0'
OUT8="$WORK/argv.out"
(cd "$ROOT" && PATH="$SC_PATH" SHELLCHECK_PATH="$FAKE" FAKE_ARGV_LOG="$ARGV" bash "$GATE") > "$OUT8" 2>&1
RC=$?
report "T8 全绿（假工具零 finding）⇒ rc=0" 0 "✅" "$RC" "$OUT8"
CALLS=$(wc -l < "$ARGV" | tr -d ' ')
NOPTS=$(grep -c -- '-f json -S error -- ' "$ARGV" || true)
if [ "$CALLS" = "0" ]; then
  printf 'FAIL %-46s 假工具一次都没被调用（SHELLCHECK_PATH 钩子没生效）\n' "T8b 参数形状断言"
  FAIL_COUNT=$((FAIL_COUNT + 1))
elif [ "$NOPTS" != "$CALLS" ]; then
  printf 'FAIL %-46s %s/%s 次调用缺 -f json 或 -S error\n' "T8b 参数形状断言" "$NOPTS" "$CALLS"
  FAIL_COUNT=$((FAIL_COUNT + 1))
else
  printf 'PASS %-46s %s 次调用全部带 -f json -S error\n' "T8b 参数形状断言" "$CALLS"
  PASS_COUNT=$((PASS_COUNT + 1))
fi
# 假工具必须报出它自己的版本（首行读数里的版本号是归属取证的一部分，不能是 "?"）
if grep -qF -- "0.0.0-fake" "$OUT8"; then
  printf 'PASS %-46s 首行 shellcheck=0.0.0-fake\n' "T8c 版本印在首行"
  PASS_COUNT=$((PASS_COUNT + 1))
else
  printf 'FAIL %-46s 首行没印出工具版本（拿哪一版跑的？无法归属）\n' "T8c 版本印在首行"
  FAIL_COUNT=$((FAIL_COUNT + 1))
fi

# ---- 收尾自检：夹具必须已经不在，且真树必须还是绿的 ----
rm -f "$PROBE"
[ -e "$PROBE" ] && { echo "FAIL 探针没清掉：$PROBE"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
(cd "$ROOT" && PATH="$SC_PATH" bash "$GATE") > "$OUT" 2>&1
RC=$?
report "T9 收尾：清完夹具后真树仍绿" 0 "✅" "$RC" "$OUT"

echo "──────── 合计 PASS=$PASS_COUNT FAIL=$FAIL_COUNT ────────"
[ "$FAIL_COUNT" = "0" ] || exit 1
exit 0
