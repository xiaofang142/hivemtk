#!/usr/bin/env bash
# =============================================================================
# check-shell-cjk-expansion.test.sh —— 姊妹门（CJK 展开闸）的反向测试
#
# 为什么现在补这份文件：那道门的**扫描面口径**在 2026-09-23 被改过（从
# 「os.walk + 目录黑名单」换成「git ls-files --cached --others --exclude-standard」，
# 因为 gitignore 的仓根 logs/ 取证快照树会让同一棵树两次扫出 136 与 538，
# 门本地恒红、CI 恒绿）。改了判据的来源却不证明它还有牙，等于把一次修复说成一句话。
# 改了判据的来源却不证明它还有牙，等于把一次修复说成一句话。9 格分别钉住（另有
# C1b/C2b 两格只在失败时打印，它们要求"红因点到探针/不许点到 logs/ 副本"）：
#   C0 真树绿，且闸自己印的 scanned == 独立算的 git ls-files 计数
#   C1 往仓里放一个含 `$VAR`+中文 的**未跟踪未忽略**探针 ⇒ rc=1 且点名它；撤掉回 rc=0
#   C2 合成树里"同一份坏内容的两份"：未跟踪未忽略的那份必须被抓、
#      落在 .gitignore 的 logs/ 里的那份必须**不进扫描面**（本轮改造的靶心）
#   C3 缺基线文件 ⇒ rc=2（"没有基线＝零覆盖"不许当绿）
#   C4 命中 0 而基线非 0 ⇒ rc=2（枚举坏了不许当收敛）
#   C5 扫描根不对（scripts/ 不在） ⇒ rc=2
#
# 用法：bash scripts/check-shell-cjk-expansion.test.sh   （rc=0 全过 / rc=1 有格不符）
#
# 夹具纪律同 check-shellcheck.test.sh：只 `rm -f` 自己造的文件，绝不对工作树跑
# git checkout/restore/stash；清单命中判定用内存子串，不拿 `... | grep -q` 进管道。
# =============================================================================

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATE="$SCRIPT_DIR/check-shell-cjk-expansion.sh"
BASELINE="$SCRIPT_DIR/shell-cjk-expansion.baseline"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
[ -f "$GATE" ] || { echo "FAIL 找不到被测闸 $GATE"; exit 1; }
[ -f "$BASELINE" ] || { echo "FAIL 被测闸自己的基线不在（${BASELINE}）—— 先查是不是别人在改"; exit 1; }

WORK="/tmp/check-shell-cjk.test.$$"
OUT="$WORK/out.txt"
mkdir -p "$WORK"
PROBE="$ROOT/scripts/tmp-cjk-reverse-probe.sh"
PASS_COUNT=0
FAIL_COUNT=0

cleanup() { rm -f "$PROBE"; rm -rf "$WORK"; }
trap cleanup EXIT

report() {  # report <格名> <期望rc> <期望红因子串> <实际rc> <输出文件>
  local name="$1" want_rc="$2" want_msg="$3" got_rc="$4" file="$5"
  if [ "$got_rc" != "$want_rc" ]; then
    printf 'FAIL %-44s rc=%s（期望 %s）\n' "$name" "$got_rc" "$want_rc"
    sed -n '1,6p' "$file" | sed 's/^/       | /'
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  if [ -n "$want_msg" ] && ! grep -qF -- "$want_msg" "$file"; then
    printf 'FAIL %-44s rc 对上了但红因里没有「%s」\n' "$name" "$want_msg"
    sed -n '1,6p' "$file" | sed 's/^/       | /'
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  printf 'PASS %-44s rc=%s\n' "$name" "$got_rc"
  PASS_COUNT=$((PASS_COUNT + 1))
}

# 坏形状的字面量：`$V` 紧跟全角逗号。整条 printf 用单引号包住 —— 单引号内 bash 不展开，
# 闸自己也按"不可展开上下文"豁免，所以这句夹具代码不会先把本测试文件自己判红。
write_bad_file() {
  printf '#!/usr/bin/env bash\nV=2\nprintf "%%s\\n" "计数=$V，后面跟中文"\n' > "$1"
}

[ -e "$PROBE" ] && { echo "FAIL 探针位已被占用：$PROBE 先人工确认归属"; exit 1; }

# ===================== C0：真树绿 + scanned 与独立口径对账 =====================
(cd "$ROOT" && bash "$GATE") > "$OUT" 2>&1
RC=$?
report "C0 真树绿（命中==基线）" 0 "✅" "$RC" "$OUT"
G_SCANNED=$(sed -n 's/.*scanned=\([0-9]*\).*/\1/p' "$OUT" | head -1)
INDEPENDENT=$(git -C "$ROOT" ls-files --cached --others --exclude-standard \
  | grep -cE '\.(sh|bash)$' || true)
if [ -n "$G_SCANNED" ] && [ "$G_SCANNED" = "$INDEPENDENT" ]; then
  printf 'PASS %-44s scanned=%s == 独立口径 %s\n' "C0b scanned 对账" "$G_SCANNED" "$INDEPENDENT"
  PASS_COUNT=$((PASS_COUNT + 1))
else
  printf 'FAIL %-44s 闸印 %s，独立算 %s\n' "C0b scanned 对账" "${G_SCANNED:-无}" "$INDEPENDENT"
  FAIL_COUNT=$((FAIL_COUNT + 1))
fi

# ===================== C1：新落点必须被抓，撤掉回绿 =====================
write_bad_file "$PROBE"
(cd "$ROOT" && bash "$GATE") > "$OUT" 2>&1
RC=$?
report "C1 未跟踪新落点 ⇒ rc=1 新落点" 1 "基线里没有这个文件" "$RC" "$OUT"
grep -qF -- 'tmp-cjk-reverse-probe.sh' "$OUT" || {
  printf 'FAIL %-44s 红因没点到探针\n' "C1b 红因点名探针"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
rm -f "$PROBE"
[ -e "$PROBE" ] && { printf 'FAIL %-44s 探针没清掉\n' "C1c 撤夹具"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
(cd "$ROOT" && bash "$GATE") > "$OUT" 2>&1
report "C1c 撤夹具后回绿" 0 "✅" "$?" "$OUT"

# ============ 合成树：C2/C3/C4 都要一棵"能跑 git、面够大、基线可控"的树 ============
SYNTH="$WORK/synth"
build_synth() {  # build_synth <基线内容；空串表示不写基线>
  rm -rf "$SYNTH"
  mkdir -p "$SYNTH/scripts"
  cp "$GATE" "$SYNTH/scripts/check-shell-cjk-expansion.sh"
  printf 'logs/\n' > "$SYNTH/.gitignore"
  i=0
  while [ "$i" -lt 52 ]; do
    printf '#!/usr/bin/env bash\necho clean%s\n' "$i" > "$SYNTH/scripts/c$i.sh"
    i=$((i + 1))
  done
  git -c init.defaultBranch=main -c user.email=t@t -c user.name=t -C "$SYNTH" init -q
  git -C "$SYNTH" add -A
  if [ -n "$1" ]; then printf '%s\n' "$1" > "$SYNTH/scripts/shell-cjk-expansion.baseline"; fi
}
BASE_SELF='# 事实源：scripts/check-shell-cjk-expansion.sh
ghost.sh	3'

# -------- C2：同一份坏内容的两份，只有未忽略的那份该进判据 --------
build_synth "$BASE_SELF"
write_bad_file "$SYNTH/scripts/tracked-side-bad.sh"          # 未跟踪、未被忽略 ⇒ 该抓
mkdir -p "$SYNTH/logs/foreign-lane/clone/scripts"
write_bad_file "$SYNTH/logs/foreign-lane/clone/scripts/tracked-side-bad.sh"  # 被忽略 ⇒ 不该看见
OUT2="$WORK/c2.out"
(cd "$SYNTH" && bash "$SYNTH/scripts/check-shell-cjk-expansion.sh") > "$OUT2" 2>&1
RC=$?
report "C2 未忽略副本被抓 ⇒ rc=1 新落点" 1 "基线里没有这个文件" "$RC" "$OUT2"
grep -qF -- 'logs/foreign-lane' "$OUT2" && {
  printf 'FAIL %-44s 红因里出现了 gitignore 的取证副本\n' "C2b 忽略树不得进扫描面"
  FAIL_COUNT=$((FAIL_COUNT + 1)); }
C2_SCANNED=$(sed -n 's/.*scanned=\([0-9]*\).*/\1/p' "$OUT2" | head -1)
C2_EXPECTED=$(git -C "$SYNTH" ls-files --cached --others --exclude-standard | grep -cE '\.sh$' || true)
if [ "$C2_SCANNED" = "$C2_EXPECTED" ]; then
  printf 'PASS %-44s scanned=%s（不含 logs/ 里那份副本）\n' "C2c 合成树 scanned 对账" "$C2_SCANNED"
  PASS_COUNT=$((PASS_COUNT + 1))
else
  printf 'FAIL %-44s 闸印 %s 独立算 %s\n' "C2c 合成树 scanned 对账" "${C2_SCANNED:-无}" "$C2_EXPECTED"
  FAIL_COUNT=$((FAIL_COUNT + 1))
fi

# -------- C3：缺基线 ⇒ rc=2 --------
build_synth ""
OUT3="$WORK/c3.out"
(cd "$SYNTH" && bash "$SYNTH/scripts/check-shell-cjk-expansion.sh") > "$OUT3" 2>&1
report "C3 缺基线 ⇒ rc=2（零覆盖不判绿）" 2 "缺少基线文件" "$?" "$OUT3"

# -------- C4：命中 0 而基线非 0 ⇒ rc=2（枚举坏了不许当收敛）--------
build_synth "$BASE_SELF"
OUT4="$WORK/c4.out"
(cd "$SYNTH" && bash "$SYNTH/scripts/check-shell-cjk-expansion.sh") > "$OUT4" 2>&1
report "C4 命中0/基线非0 ⇒ rc=2" 2 "命中数为 0 但基线非 0" "$?" "$OUT4"

# -------- C5：扫描根不对 ⇒ rc=2 --------
NOROOT="$WORK/noroot"
mkdir -p "$NOROOT"
cp "$GATE" "$NOROOT/check-shell-cjk-expansion.sh"
OUT5="$WORK/c5.out"
bash "$NOROOT/check-shell-cjk-expansion.sh" > "$OUT5" 2>&1
report "C5 扫描根不对 ⇒ rc=2" 2 "扫描根不对" "$?" "$OUT5"

echo "──────── 合计 PASS=$PASS_COUNT FAIL=$FAIL_COUNT ────────"
[ "$FAIL_COUNT" = "0" ] || exit 1
exit 0
