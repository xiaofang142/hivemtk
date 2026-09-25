#!/usr/bin/env bash
# mut_dispose.py 的用例：把"电池收尾别把调用方的树带走"钉成判据。
#
# 为什么这份用例存在（2026-09-24 的实测事故，不是假想敌）：
#   `python3 scripts/mut_collection_p703.py --clone . --check` 在泳道根目录下跑完锚点预检后，
#   出口的 `shutil.rmtree(tmp)` 里 `tmp` 就是 `Path(".")` ⇒ **整棵 r45-lane 被清空**
#   （含 `.git` 与 18 项未提交字节；本轮因此重做了一遍）。
#   旧写法的意图是"我建的临时目录我带走"，但 `--clone` 允许把目录名交给调用方，
#   而代码把"调用方指定的目录"和"我创建的目录"当成同一件事——`--clone .`／`--clone <仓库根>`
#   都是合法入参，删除动作却照样执行。
#
# 每一格断言的是"哪个目录还在"，不是 rc：这一类缺陷的表现正是"一切都成功退出"。
#   G1 --clone 指来的目录：只带走里面的 clone/，外层与哨兵必须还在
#   G2 mkdtemp 出来的目录（owned）：整目录带走
#   G3 --clone . 就是仓库根：一律不删，且仓库根分毫未动（这一格就是事故本体）
#   G4 --clone 指到仓库根的上一级（工作区根）：一律不删
#   G5 clone/ 里没有 .git（不是本电池建的克隆）：不删，避免误伤同名的别人目录
#   G6 --keep：什么都不许删
#   G7 入口闸 workdir()：--clone 就是仓库根 ⇒ 装架之前就退，且出声
#   G8 入口闸：--clone 是仓库根的上级／是 /tmp 本身 ⇒ 同样退
#   G9 入口闸：不传 --clone 时 mkdtemp(prefix) 照旧且 owned=1；传安全空目录时 owned=0 且不动它
#   REAL 静态面（四腿）：六枚带 --clone 的常驻电池必须 ① --clone 面／入口闸面／收尾闸面三处
#       计数相等，② 不留 `Path(args.clone …)` 直连赋值，③ 不留裸 rmtree(tmp/dst/work)，
#       ④ 入口闸那一行必须排在 `prepare(tmp)` **之前**（顺序腿的反向测：把 workdir 挪到
#       clone 之后即红，见本轮 C 组同款做法）
#
# 两道闸的分工（别只看一道就以为安全了）：`dispose()` 是"退出时才发现"的下界，
# `workdir()` 是"装架之前就把危险入参挡掉"的上界。只有上界的理由是：电池在收尾之前
# 会往 `tmp` 里 `git clone --shared`、写补丁、跑 `go test`——`--clone .` 即使最后拒删，
# 中途也已经把克隆和改动落进了调用方的工作树（本轮事故跑的就是 `--clone . --check`）。
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
MODULE="$ROOT/scripts/mut_dispose.py"
FAIL=0
WORK=$(mktemp -d /tmp/mut-dispose-test.XXXXXX)
trap 'rm -rf "$WORK"' EXIT

ok()  { echo "  ✓ $1"; }
bad() { echo "  ✗ $1"; FAIL=$((FAIL + 1)); }

[ -f "$MODULE" ] || { echo "FATAL: 找不到 $MODULE"; exit 1; }

# call <临时根> <python 片段变量> —— 在 WORK 里造一棵"假仓库"再调 dispose
python_call() {
  python3 - "$ROOT" "$@" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]) / "scripts"))
from mut_dispose import dispose
repo_root, tmp, owned, keep = Path(sys.argv[2]), Path(sys.argv[3]), sys.argv[4] == "1", sys.argv[5] == "1"
dispose(tmp, owned=owned, keep=keep, repo_root=repo_root)
PY
}

# 造一枚"克隆"：dispose 认 `.git` 才算自己建的私有克隆
mk_clone() { mkdir -p "$1/clone/.git"; echo x > "$1/clone/payload.txt"; }

# workdir_call <仓库根> <--clone 值|"-"（不传）> <前缀> —— 走入口闸；退码非 0 即"挡在门外"
workdir_call() {
  python3 - "$ROOT" "$@" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]) / "scripts"))
from mut_dispose import workdir
repo_root, arg, prefix = Path(sys.argv[2]), sys.argv[3], sys.argv[4]
tmp, owned = workdir(None if arg == "-" else arg, prefix=prefix, repo_root=repo_root)
print(f"PASS tmp={tmp} owned={int(owned)}")
PY
}

echo "G1 --clone 交来的目录：只带走 clone/，外层必须还在"
d="$WORK/g1"; mkdir -p "$d"; echo keep > "$d/sentinel"; mk_clone "$d"
python_call "$WORK/fakerepo" "$d" 0 0
[ -e "$d/sentinel" ] && ok '外层目录与哨兵未被动' || bad '把调用方的外层目录一起删了'
[ -e "$d/clone" ] && bad 'clone/ 没被带走' || ok 'clone/ 已回收'

echo "G2 owned（mkdtemp 的目录）：整目录带走"
d="$WORK/g2"; mkdir -p "$d"; mk_clone "$d"
python_call "$WORK/fakerepo" "$d" 1 0
[ -e "$d" ] && bad 'owned 目录没删干净' || ok 'owned 目录已带走'

echo "G3 --clone .＝仓库根：一律不删（事故本体）"
d="$WORK/g3repo"; mkdir -p "$d/.git" "$d/scripts"; echo keep > "$d/Makefile"
python_call "$d" "$d" 0 0
[ -e "$d/.git" ] && [ -e "$d/Makefile" ] && ok '仓库根分毫未动' || bad '仓库根被动过'
out=$(python_call "$d" "$d" 0 0 2>&1); printf '%s' "$out" | grep -q '拒绝' && ok '拒删时出声（不是静默放行）' || bad '静默跳过：打印里没有"拒绝"'

echo "G4 --clone 指到仓库根的上一级：一律不删"
ws="$WORK/g4ws"; mkdir -p "$ws/hivemtk/.git"; echo k > "$ws/hivemtk/Makefile"
echo keep > "$ws/user-side-file"
python_call "$ws/hivemtk" "$ws" 0 0
[ -e "$ws/user-side-file" ] && ok '工作区根未被删' || bad '把仓库根上一级删了'

echo "G5 clone/ 里没有 .git：不是本电池建的，不碰"
d="$WORK/g5"; mkdir -p "$d/clone"; echo theirs > "$d/clone/other.txt"
python_call "$WORK/fakerepo" "$d" 0 0
[ -e "$d/clone/other.txt" ] && ok '别人的同名目录未被删' || bad '误删同名 clone/'

echo "G6 --keep：什么都不许删"
d="$WORK/g6"; mkdir -p "$d"; mk_clone "$d"
python_call "$WORK/fakerepo" "$d" 1 1
[ -e "$d/clone/payload.txt" ] && ok '--keep 下全部保留' || bad '--keep 没被尊重'

echo "G7 入口闸：--clone 就是仓库根 ⇒ 装架前退，且出声"
d="$WORK/g7repo"; mkdir -p "$d/.git" "$d/scripts"; echo keep > "$d/Makefile"; touch "$d/sentinel-g7"
out=$(workdir_call "$d" "$d" "g7mut-" 2>&1); rc=$?
[ "$rc" -ne 0 ] && ok "危险入参当轮退非 0（rc=${rc}）" || bad "rc=${rc}：入口闸没挡住仓库根"
printf '%s' "$out" | grep -q '拒绝' && ok '退的时候出声（打印里有"拒绝"）' || bad "退码非 0 却没说明原因：$out"
[ -e "$d/.git" ] && [ -e "$d/Makefile" ] && [ -e "$d/sentinel-g7" ] \
  && ok '仓库根分毫未动（没往里塞 clone/、没写补丁）' || bad '入口闸退了但已经动过仓库根'
[ -e "$d/clone" ] && bad '危险入参仍然建出了 clone/' || ok '没有 clone/ 残留'

echo "G8 入口闸：--clone 指到上级／文件系统临时目录本身 ⇒ 退"
ws="$WORK/g8ws"; mkdir -p "$ws/hivemtk/.git"; echo keep > "$ws/user-side-file"
out=$(workdir_call "$ws/hivemtk" "$ws" "g8mut-" 2>&1); rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q '拒绝' \
  && ok '上级目录被拒（不会在其下装架）' || bad "上级入参没挡住：rc=$rc / $out"
[ -e "$ws/user-side-file" ] && ok '工作区根未被动' || bad '把仓库根上一级当作业目录用了'
out=$(workdir_call "$ws/hivemtk" "/tmp" "g8mut-" 2>&1); rc=$?
[ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q '拒绝' \
  && ok '/tmp 本身被拒' || bad "--clone /tmp 没挡住：rc=$rc / $out"

echo "G9 入口闸：合法入参照常放行（把'一律退'也判红）"
safedir="$WORK/g9safe"; mkdir -p "$safedir"; echo keep > "$safedir/readme"
out=$(workdir_call "$WORK/fakerepo" "$safedir" "g9mut-" 2>&1); rc=$?
printf '%s' "$out" | grep -q 'PASS tmp=' || bad "安全目录被误挡：rc=$rc / $out"
printf '%s' "$out" | grep -q 'owned=0' || bad "传进来的目录被判成 owned（会被整体带走）：$out"
[ -e "$safedir/readme" ] && ok '安全目录里的既有文件未动' || bad '入口闸顺手清了调用方目录'
out=$(workdir_call "$WORK/fakerepo" "-" "g9mut-" 2>&1); rc=$?
case "$out" in
  PASS*owned=1*)
    p=$(printf '%s' "$out" | sed -n 's/^PASS tmp=\(.*\) owned=1$/\1/p')
    [ -n "$p" ] && [ -d "$p" ] && ok "不传 --clone 时自建私有目录（${p}）" || bad "mkdtemp 腿没给出可核对的路径：$out"
    rm -rf "$p" ;;
  *) bad "不传 --clone 应放行且 owned=1，实得：$out" ;;
esac

echo "REAL 静态面：六枚带 --clone 的电池都走 dispose，且不留裸 rmtree"
# 对象集合**不含 mut_dispose.py 自己**：它的 docstring 里就写着 `Path(args.clone` 与
# `shutil.rmtree(tmp)` 两句（那是被修对象的形状，不是待修的调用点），把它算进面里
# 会同时把两处计数各抬高 1，看起来仍"相等"，于是这一格从"有一枚没接线"变成永远读不准。
cells_files=()
for f in "$ROOT"/scripts/mut_*.py; do
  [ "$(basename "$f")" = "mut_dispose.py" ] && continue
  cells_files+=("$f")
done
n_clone=$(grep -l 'args\.clone' "${cells_files[@]}" | wc -l | tr -d ' ')
n_workdir=$(grep -l 'workdir(args\.clone' "${cells_files[@]}" | wc -l | tr -d ' ')
n_dispose=$(grep -l 'dispose(tmp' "${cells_files[@]}" | wc -l | tr -d ' ')
[ "$n_clone" = "$n_workdir" ] && [ "$n_clone" = "$n_dispose" ] && [ "$n_clone" -gt 0 ] \
  && ok "全部 ${n_clone} 枚走 workdir+dispose（--clone 面 ${n_clone}／入口闸 ${n_workdir}／收尾闸 ${n_dispose}）" \
  || bad "--clone 面 ${n_clone} ≠ 入口闸 ${n_workdir} ≠ 收尾闸 ${n_dispose}（有一枚没接满两道闸）"
# 只走 `Path(args.clone or mkdtemp(...))` 的旧形状＝入口那道闸根本没机会开火：
# 危险值一进一出就被赋给 tmp，随后电池照常往它身上装架。这一格把旧形状判红。
raw=$(grep -n 'Path(args\.clone' "${cells_files[@]}" | wc -l | tr -d ' ')
[ "$raw" = 0 ] && ok '没有残留的 `Path(args.clone …)` 直连赋值' || bad "仍有 $raw 处直连赋值（入口闸被绕过）"
bare=$(grep -n 'shutil.rmtree(\(tmp\|dst\|work\)' "${cells_files[@]}" | wc -l | tr -d ' ')
[ "$bare" = 0 ] && ok '没有裸 rmtree(tmp/dst/work) 站点' || bad "仍有 $bare 处裸 rmtree"
# 顺序腿：光"接了 workdir"不够，还得**在装架之前**。三枚电池（b17/hub/p503）的参数校验在
# workdir 之前，其余电池一过 workdir 就 `git clone --shared`——若入口闸排在 clone 之后，
# 它就永远只是"收尾闸的复读"，危险目录早在 clone 那一步被写脏了。
late=0
ord_seen=0
for f in "${cells_files[@]}"; do
  # 只判"真收 --clone 的电池"：另外几枚（ledger_b16*／seam_guard_r28／startup_hook_p702）
  # 压根没有 --clone 面，把它们算进来会把"没有这个面"读成"这个面接错了"。
  grep -q 'args\.clone' "$f" || continue
  ord_seen=$((ord_seen + 1))
  wl=$(grep -n 'workdir(args\.clone' "$f" | head -1 | cut -d: -f1)
  pl=$(grep -n 'prepare(tmp' "$f" | head -1 | cut -d: -f1)
  [ -n "$wl" ] && [ -n "$pl" ] && [ "$wl" -lt "$pl" ] && continue
  late=$((late + 1))
  echo "  · $(basename "$f") 顺序不对：workdir 在第 ${wl:-无} 行、装架在第 ${pl:-无} 行"
done
if [ "$ord_seen" != "$n_clone" ]; then
  bad "顺序腿只看到 ${ord_seen} 枚有 --clone 面的电池，对不上集合数 ${n_clone}（判据在空转）"
elif [ "$late" = 0 ]; then
  ok "$ord_seen 枚都在装架之前过入口闸"
else
  bad "$late 枚的入口闸排在 clone 之后"
fi

echo
if [ "$FAIL" = 0 ]; then echo "===== 用例：九格全过（断言失败 0 处）====="; exit 0; fi
echo "===== 用例：$FAIL 处断言失败 ====="; exit 1
