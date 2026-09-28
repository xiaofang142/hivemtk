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
#   G10 兜底闸：装架之后一条没接闸的 raise ⇒ 克隆照样回收、外层未动、停机码仍非 0
#   G11 兜底闸：显式收尾已经收过 ⇒ 兜底安静，全趟只许一条"收尾"行
#   G12 兜底闸：leave_for_evidence 打过标记 ⇒ 只出声不回收；压根没装架时整趟安静
#   REAL 静态面（六腿）：**所有**带 --clone 面的常驻电池（枚数由 grep 现取，别照抄文档里的
#       "六枚/七枚"——写死的那个数每次有人加电池就过期一次）必须 ① --clone 面／入口闸面／收尾闸面三处
#       计数相等，② 不留 `Path(args.clone …)` 直连赋值，③ 不留裸 rmtree(tmp/dst/work)，
#       ④ 入口闸那一行必须排在 `prepare(tmp)` **之前**（顺序腿的反向测：把 workdir 挪到
#       clone 之后即红，见本轮 C 组同款做法），⑤ 克隆建起来之后的每条**函数体内**退出都走 `bail()`
#       （豁免三形："已存在"/"克隆失败"/"md5 不一致"；反向测在 `$WORK/rev` 里撤掉一处 bail），
#       ⑥ 每枚电池的**每一处**装架之后都跟着一次 `dispose_at_exit` 注册（两处装架＝两处注册；
#       只挂第一处＝真跑用例那条路全程没闸），且 md5 豁免路先打 `leave_for_evidence`
#       （反向测两条各绑一条分支：摘注册⇒点名"这处装架之后没挂兜底闸"、摘标记⇒点名"没打让路标记"）
#
# 三道闸的分工（别只看一道就以为安全了）：`workdir()` 是"装架之前就把危险入参挡掉"的上界，
# `dispose()` 是"退出时才发现"的下界，`dispose_at_exit()` 是"装架之后有人忘了接闸"的兜底。
# 上界不可少的理由：电池在收尾之前会往 `tmp` 里 `git clone --shared`、写补丁、跑 `go test`——
# `--clone .` 即使最后拒删，中途也已经把克隆和改动落进了调用方的工作树（事故跑的就是
# `--clone . --check`）。下界＋上界仍不够的理由：装架函数体之外的每一条 `raise`／`sys.exit`／
# `bail` 都是一条没人接闸的退出路（枚数与条数由下面那条兜底闸腿每次 AST 现取并打印，不在文案里
# 写死——写死的下一位无从复算）。实测一趟留 73M（p503 的控制组停机，读数在库内）。
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
MODULE="$ROOT/scripts/mut_dispose.py"
FAIL=0
N=0
WORK=$(mktemp -d /tmp/mut-dispose-test.XXXXXX)
trap 'rm -rf "$WORK"' EXIT

# `$*` 不是排版：`bad "…枚里有裸中止路：" $(…)` 把命中名单放在第二个参数上，原先写 `"$1"`
# 时红只印出冒号、后面空的（本轮合并后就是这样，只能人手重跑 PY 段才捞出文件名）。
# 计数同理——汇总行原先写死"九格"，加一条腿就说谎，故现数。
ok()  { echo "  ✓ $*"; N=$((N + 1)); }
bad() { echo "  ✗ $*"; N=$((N + 1)); FAIL=$((FAIL + 1)); }

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

# hook_call <仓库根> <作业目录> <owned> <keep> <mode> —— 挂上兜底闸后按 mode 退出：
#   raise         函数体里直接 raise SystemExit（就是"这条退出路没接闸"的形状）
#   dispose-first 先走显式收尾出口再正常退（兜底那一脚应当安静）
#   leave         打让路标记后 raise（豁免路：只出声不回收）
# 必须真的从函数里 raise 出去：直接在模块末尾调 dispose 测的是老那条显式出口，证不到钩子。
hook_call() {
  python3 - "$ROOT" "$@" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]) / "scripts"))
from mut_dispose import dispose, dispose_at_exit, leave_for_evidence
root, tmp = Path(sys.argv[2]), Path(sys.argv[3])
owned, keep, mode = sys.argv[4] == "1", sys.argv[5] == "1", sys.argv[6]
dispose_at_exit(tmp, owned=owned, keep=keep, repo_root=root)


def main() -> None:
    if mode == "dispose-first":
        dispose(tmp, owned=owned, keep=keep, repo_root=root)
        return
    if mode == "leave":
        leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
    raise SystemExit("控制组不干净")


main()
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

# 兜底闸的三格行为面：这一只钩子的价值全在"那条退出路压根没接闸"的时候，
# 所以夹具必须真的从函数里 raise 出去（不是调 dispose），否则测的是显式出口那条老路。
echo "G10 兜底闸：装架之后没接闸的退出 ⇒ 照样回收，且出声"
d="$WORK/g10"; mkdir -p "$d"; echo keep > "$d/sentinel"; mk_clone "$d"
out=$(hook_call "$WORK/fakerepo" "$d" 0 0 raise); rc=$?
printf '%s' "$out" | grep -q '兜底闸回收' && ok '兜底闸认出了这条中止路（打印里有"兜底闸回收"）' || bad "兜底闸没出声：$out"
[ -e "$d/clone" ] && bad '兜底闸没把克隆带走' || ok '克隆已被兜底回收'
[ -e "$d/sentinel" ] && ok '外层目录仍分毫未动' || bad '兜底闸越权删了调用方的外层'
[ "$rc" != 0 ] && ok "停机码照旧非 0（rc=${rc}，兜底不许把红改成绿）" || bad "兜底闸把退出码抹成 0：rc=$rc"

echo "G11 兜底闸：显式收尾已经收过 ⇒ 兜底那一脚必须安静（不许印第二条'收尾'）"
d="$WORK/g11"; mkdir -p "$d"; mk_clone "$d"
out=$(hook_call "$WORK/fakerepo" "$d" 0 0 dispose-first); rc=$?
printf '%s' "$out" | grep -q '兜底闸回收' && bad "已回收过还在复述：$out" || ok '已有人收过 ⇒ 兜底安静退出'
n_tail=$(printf '%s\n' "$out" | grep -c '^收尾：')
[ "$n_tail" = 1 ] && ok '整趟只有一条收尾行（取证日志里不会出现互相矛盾的两句）' \
  || bad "收尾行数=${n_tail}（应为 1）：$(printf '%s' "$out" | tr '\n' '|')"

echo "G12 兜底闸：让路标记（'现场只活在克隆里'那类豁免）⇒ 只出声、不回收"
d="$WORK/g12"; mkdir -p "$d"; mk_clone "$d"
out=$(hook_call "$WORK/fakerepo" "$d" 0 0 leave); rc=$?
printf '%s' "$out" | grep -q '收尾闸让路' && ok '让路时出声（说清为什么这次不删）' || bad "让路没出声：$out"
[ -e "$d/clone/payload.txt" ] && ok '豁免现场的克隆完整保留' || bad '把该留证的克隆删了'
# 让路那句话的前提是"有一份克隆值得留"：装架之前就停的路（入口闸、目录已存在）不该跟着出声。
d="$WORK/g12b"; mkdir -p "$d"
out=$(hook_call "$WORK/fakerepo" "$d" 0 0 leave); rc=$?
printf '%s' "$out" | grep -q '让路\|兜底闸回收' && bad "压根没装架却报回收决定：$out" \
  || ok "没有克隆 ⇒ 兜底闸完全安静（不把「没东西可删」读成一次决定）"

echo "REAL 静态面：所有带 --clone 面的电池都走 dispose，且不留裸 rmtree（枚数现取，不写死）"
# 对象集合**不含 mut_dispose.py 自己**：它的 docstring 里就写着 `Path(args.clone` 与
# `shutil.rmtree(tmp)` 两句（那是被修对象的形状，不是待修的调用点），把它算进面里
# 会同时把两处计数各抬高 1，看起来仍"相等"，于是这一格从"有一枚没接线"变成永远读不准。
cells_files=()
for f in "$ROOT"/scripts/mut_*.py; do
  [ "$(basename "$f")" = "mut_dispose.py" ] && continue
  cells_files+=("$f")
done
# 未跟踪件先点出来（旁道正在跑、还没提交的电池）：本门只对"已经进版本控制的面"负责，
# 但少掉的对象必须打印，否则「面上 N 枚」的读数会被下一位读成「这 N 枚就是全部」。
SKIP_UNTRACKED=""
SKIP_N=0
for f in "${cells_files[@]}"; do
  git -C "$ROOT" ls-files --error-unmatch "$f" >/dev/null 2>&1 && continue
  SKIP_UNTRACKED="$SKIP_UNTRACKED $(basename "$f")"
  SKIP_N=$((SKIP_N + 1))
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
  # 装架这一行要锚**调用点形状**（`= prepare(tmp`／`= go_prepare(tmp`）。原先只写
  # `prepare(tmp` 会捞到两类不是调用点的行，2026-09-28 一天里各撞一次：
  #   ① 把装架函数的形参也命名为 `tmp` ⇒ 函数定义行先命中，格子读成"入口闸排在 clone 之后"；
  #   ② 注释/文档里照抄这个字面 ⇒ 注释行先命中，同一句红话从别的行印出来。
  # 两次的红都不是产码问题，而是判据抓错了对象——抓错对象的判据既会假红也会假绿，所以这里钉死形状。
  pl=$(grep -n '= go_prepare(tmp\|= js_prepare(tmp\|= prepare(tmp' "$f" | head -1 | cut -d: -f1)
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

# 中止路腿（REAL 第五腿）：`git clone` 建起来**之后**的每一条退出都必须先过收尾闸。
# 起因是 2026-09-28 的实测：`mut_bill_p701.py --check` 走到"工作树压着并行泳道未提交字节"
# 那条 raise 时直接退出，把 72M 私有克隆留在临时目录里，而磁盘常态 98% 满——
# 收尾闸当时只接在 `main()` 的出口上，装架函数内的中止路一条都没接。
# 三条豁免与 `bail()` 的 docstring 同源："已存在"（那份 clone/ 不是本电池建的，不许删）、
# "克隆失败"（目录归属还没定）、"md5 不一致"（"覆盖后还是不对"的字节只活在克隆里，
# 删了就只剩一句"当时红过"，与 `main()` 侧还原校验同一取舍）。
abort_leg() {
  python3 - "$1" <<'PY'
import ast, pathlib, sys

EXEMPT = ("已存在", "克隆失败", "md5 不一致")
d = pathlib.Path(sys.argv[1])
obj, leak = 0, []
for p in sorted(d.glob("mut_*.py")):
    if p.name == "mut_dispose.py":
        continue
    text = p.read_text(encoding="utf-8")
    tree = ast.parse(text)
    fn = next((f for f in tree.body
               if isinstance(f, ast.FunctionDef) and f.name in ("prepare", "go_prepare")
               and '"git", "clone"' in (ast.get_source_segment(text, f) or "")), None)
    if fn is None:
        continue
    obj += 1
    seg = ast.get_source_segment(text, fn)
    if "def bail(" not in seg:
        leak.append(f"{p.name}：装架函数里没有 bail()——克隆之后的退出会留整份私有克隆")
        continue
    if "owned: bool" not in seg.split("def bail(")[0]:
        leak.append(f"{p.name}：装架函数没有 owned 形参，bail() 无从判断目录归属")
    inner = {id(n) for b in fn.body if isinstance(b, ast.FunctionDef) and b.name == "bail"
             for n in ast.walk(b)}
    def_ln = min(n.lineno for n in ast.walk(fn)
                 if isinstance(n, ast.FunctionDef) and n.name == "bail")
    for n in ast.walk(fn):
        if isinstance(n, ast.Raise) and id(n) not in inner:
            s = (ast.get_source_segment(text, n) or "").strip()
            if not any(k in s for k in EXEMPT):
                leak.append(f"{p.name}:{n.lineno} 克隆之后的裸 raise：{s.splitlines()[0][:64]}")
        if isinstance(n, ast.Call) and getattr(n.func, "id", "") == "bail" and n.lineno < def_ln:
            leak.append(f"{p.name}:{n.lineno} 在 def bail 之前调用它")
print(obj)
print("\n".join(leak))
sys.exit(1 if leak else 0)
PY
}
abort_out=$(abort_leg "$ROOT/scripts")
abort_rc=$?
abort_obj=$(printf '%s\n' "$abort_out" | head -1)
abort_bad=$(printf '%s\n' "$abort_out" | tail -n +2 | sed '/^$/d')
clone_any=$(grep -l '"git", "clone"' "${cells_files[@]}" | wc -l | tr -d ' ')
if [ "$abort_obj" != "$clone_any" ]; then
  bad "中止路腿只看到 ${abort_obj} 枚带 git-clone 装架的电池，独立计数是 ${clone_any}（判据在空转）"
elif [ "$abort_rc" = 0 ]; then
  ok "$abort_obj 枚电池在克隆之后的退出全走收尾闸（豁免三形除外）"
else
  bad "$abort_obj 枚里有裸中止路：$(printf '%s\n' "$abort_bad" | head -3 | tr '\n' ' ')"
fi
# 红因点名腿（2026-09-28 立的）：上面那条 bad 把命中名单当**第二个参数**传（未加引号 ⇒ 逐格拆开），
# 而 `bad()` 原先写的是 `echo "  ✗ $1"` ⇒ 真红的时候只印出"…枚里有裸中止路："后面空的：本轮合并
# 后它就是 rc=1 却零个文件名，定位靠人手把 PY 段抄出来重跑才捞出唯一那枚漏的电池。红不点名＝
# 下一位重复同一趟人工捞取。这条腿把"bad 的后续参数必须出现在输出里"钉成判据。
bad_probe=$( ( bad '甲：' '乙文件名.py' ) 2>/dev/null )
case "$bad_probe" in
  *乙文件名.py*) ok '红因点名：bad 的后续参数一起打印（实得『'"$bad_probe"'』）' ;;
  *) bad "红因不点名：bad '甲：' '乙文件名.py' 只输出『${bad_probe}』——裸中止路那条红会查无对象" ;;
esac
# 反向测（没有这一步，上面那句绿只是"这段代码没报错"）：把一枚电池的 `bail("checkout 失败…")`
# 改回裸 raise，这条腿必须点名红。
REVDIR="$WORK/rev/scripts"
mkdir -p "$REVDIR"
cp "$ROOT"/scripts/mut_*.py "$REVDIR/"
python3 - "$REVDIR" <<'PY'
import pathlib, sys
d = pathlib.Path(sys.argv[1])
for p in sorted(d.glob("mut_*.py")):
    t = p.read_text(encoding="utf-8")
    if 'bail("checkout 失败' in t:
        p.write_text(t.replace('bail("checkout 失败', 'raise SystemExit("checkout 失败', 1),
                     encoding="utf-8")
        print(f"反向格：把 {p.name} 的一处 bail 改回裸 raise")
        break
else:
    raise SystemExit("反向格找不到可撤的 bail——这条腿的对象集是空的")
PY
rev_out=$(abort_leg "$REVDIR")
rev_rc=$?
if [ "$rev_rc" = 0 ]; then
  bad "反向：撤掉一处 bail 之后这条腿仍绿＝判据没牙"
else
  ok "反向：撤掉一处 bail 当场红（$(printf '%s\n' "$rev_out" | tail -n +2 | head -1 | cut -c1-48)…）"
fi

# 兜底闸腿（REAL 第六腿，2026-09-28）：上一腿只走 `prepare`/`go_prepare` 的**函数体**，
# 而实测的漏盘就长在它看不见的地方——`mut_reach_p503.py` 的 `控制组[service] 不干净` 那条
# raise 在 main 里、装架之后、收尾闸之前，一趟留 73M（读数在库内：
# `docs/superpowers/specs/ledger/logs/P503/20260928-161751/00-residue.log`，du -sk = 75196 KiB）。装架函数体
# 之外那些可达退出的枚数与条数由本腿每次现取、随重构摆动（写在文案里就等着过期），逐处插 sweep() 改不动（一半从
# `sub_once`/`lane_overlays`/`apply_js` 这类辅助函数里冒出来，它们不知道克隆在哪），
# 所以这腿断言的是"每枚电池都在装架之后挂了进程级兜底闸"＋"豁免'现场只活在克隆里'的那几处
# 都先打了让路标记"——G10–G12 证钩子真会回收／真会安静／真会让路，这一腿证现取的那几枚真挂上了。
hook_leg() {   # 参数：装架目录，其后接"未跟踪旁道件"的文件名（这些枚不在本门面上，见下面的点名）
  python3 - "$@" <<'PY'
import ast, pathlib, re, sys

PREPARE = re.compile(r"=\s*(?:go_|js)?prepare\(tmp")
d = pathlib.Path(sys.argv[1])
skip = set(sys.argv[2:])
obj, leak = 0, []
exits = 0   # 现取读数：装架之后、且不在任何 `*prepare` 函数体内的退出语句条数


def stmt_lists(root):
    """所有"语句序列"：函数体、if/else、try/finally、for/while……兜底闸必须与装架同序。"""
    out = []
    for node in ast.walk(root):
        for _, value in ast.iter_fields(node):
            if isinstance(value, list) and value and all(isinstance(v, ast.stmt) for v in value):
                out.append(value)
    return out


def is_exit(node) -> bool:
    """一条"会离开进程（或中止本趟）"的语句：raise／exit()／sys.exit()／bail()。"""
    if isinstance(node, ast.Raise):
        return True
    return (isinstance(node, ast.Expr) and isinstance(node.value, ast.Call)
            and ast.unparse(node.value.func) in ("exit", "sys.exit", "bail"))


for p in sorted(d.glob("mut_*.py")):
    if p.name == "mut_dispose.py" or p.name in skip:
        continue
    text = p.read_text(encoding="utf-8")
    if '"git", "clone"' not in text or "args.clone" not in text:
        continue
    tree = ast.parse(text)
    obj += 1
    imported = any(isinstance(n, ast.ImportFrom) and n.module == "mut_dispose"
                   and any(a.name == "dispose_at_exit" for a in n.names) for n in ast.walk(tree))
    if not imported:
        leak.append(f"{p.name}：没从 mut_dispose 导入 dispose_at_exit")
    hooks = [n for n in ast.walk(tree)
             if isinstance(n, ast.Call)
             and getattr(n.func, "id", getattr(n.func, "attr", "")) == "dispose_at_exit"]
    # 这里不写死"恰好 1 次"：注册次数该跟**装架点数**走（见下面逐处判据）。b17 有两处装架
    # （--check 一条路、真跑一条路）就是两次注册；写死 1 会把这种正确形状判成红，
    # 而"0 次/漏一处"由下面那条逐处腿点名。
    # 取一句语句的源码用"按行切片"，不用 `ast.get_source_segment`：后者每次调用都把整份
    # 模块重新按换页符切开（3.10 的实现），一文件三百句就是 O(句数×文件大小)。现测：本腿
    # 早先那样写，一整批跑一趟 90 秒，而本门要把这腿跑三趟（正面＋两条反向）⇒ 4 分半全花在
    # 取证夹具自己上，读起来像"门挂了"。上一腿（abort_leg）只在函数级取一次，不受影响。
    lines = text.splitlines()

    def seg(st) -> str:
        return "\n".join(lines[st.lineno - 1:st.end_lineno])

    # 装架点：**每一处** `clone|work|dst = (go_|js)?prepare(tmp…` 的赋值，不是只取第一处。
    # 为什么逐处：`mut_actionability_b17.py` 有两处装架——一处在 `if args.check:` 分支里、
    # 一处在真跑用例的那条路上，而注册只有第一处后面那一句 ⇒ 跑用例那条路全程没闸（那行
    # 注册压根没执行到；2026-09-28 现扫 AST 抓到）。判据因此是"每处装架之后同一段里都跟着
    # 一句注册"，摘掉任一句注册都会当场点名是哪一处装架在裸奔。
    preps = []
    for body in stmt_lists(tree):
        for i, st in enumerate(body):
            s = seg(st)
            if PREPARE.search(s) and s.lstrip().startswith(("clone", "work", "dst")):
                preps.append((body, i, st))
    if not preps:
        leak.append(f"{p.name}：找不到装架调用点（= prepare(tmp 的形状对不上）")
    else:
        # 读数口径：第一处装架之后、且不落在任何 `*prepare` 函数体内的退出。为什么排函数体：
        # 那一半由 `bail()` 与上一条腿（abort_leg）负责，混进来会把两个面数成一个数。
        body_lines = set()
        for n in ast.walk(tree):
            if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef)) and n.name.endswith("prepare"):
                body_lines |= set(range(n.lineno, n.end_lineno + 1))
        first_prep = min(st.lineno for _, _, st in preps)
        exits += sum(1 for n in ast.walk(tree)
                     if is_exit(n) and n.lineno >= first_prep and n.lineno not in body_lines)
        for body, i, st in preps:
            if not any(isinstance(x, ast.Expr)
                       and getattr(getattr(x.value, "func", None), "id", "") == "dispose_at_exit"
                       for x in body[i + 1:]):
                leak.append(f"{p.name}:{st.lineno} 这处装架之后没挂兜底闸"
                            f"（本枚注册 {len(hooks)} 次／装架点 {len(preps)} 处）")
        if len(hooks) > len(preps):
            leak.append(f"{p.name}：兜底闸注册了 {len(hooks)} 次，多于装架点 {len(preps)} 处")
    # 豁免路：`raise SystemExit(...md5...)` 之前一句必须是 leave_for_evidence(...)
    prev_end = {}
    for body in stmt_lists(tree):
        for st in body:
            prev_end.setdefault(st.end_lineno, []).append(st)
    for n in ast.walk(tree):
        if not isinstance(n, ast.Raise):
            continue
        s = seg(n).strip()
        if "md5" not in s or "SystemExit" not in s:
            continue
        before = [st for st in prev_end.get(n.lineno - 1, [])
                  if isinstance(st, ast.Expr) and isinstance(st.value, ast.Call)
                  and getattr(st.value.func, "id", "") == "leave_for_evidence"]
        if not before:
            leak.append(f"{p.name}:{n.lineno} md5 豁免路没打让路标记：{s.splitlines()[0][:56]}")
print(obj)
print(f"EXITS={exits}")
print("\n".join(leak))
sys.exit(1 if leak else 0)
PY
}
hook_out=$(hook_leg "$ROOT/scripts" $SKIP_UNTRACKED)
hook_rc=$?
hook_obj=$(printf '%s\n' "$hook_out" | head -1)
# 第二行是本腿现取的读数（不是判据），从"红因"里剔出去，免得它被当成一条漏盘点名。
hook_exits=$(printf '%s\n' "$hook_out" | sed -n 's/^EXITS=//p')
hook_bad=$(printf '%s\n' "$hook_out" | tail -n +2 | grep -v '^EXITS=' | sed '/^$/d')
# 对象集＝"有 --clone 面 ∩ 有 git-clone 面 ∩ 已进版本控制"。三个条件各排一类假红：
#   · `mut_extension_auth_b19h.py` 有 --clone 面但没有 git-clone 面（JS 装架留的是 `web/`，
#     那份残留是 dispose 契约里明写不许回收的）⇒ 钩子对它永远安静，钉它没意义；
#   · 未跟踪的旁道件（本轮：`mut_kb_release_p902.py`）不在本门面上——替它挂钩子＝让别人的
#     文件忽然引用只存在于我这笔提交里的符号，他们先提交就拿到一枚 import 不到的电池。
#     点名输出，不许静默少一枚（少掉的对象不写出来就会被读成"面上那些全证过了"）。
both=0
for f in "${cells_files[@]}"; do
  grep -q 'args\.clone' "$f" || continue
  grep -q '"git", "clone"' "$f" || continue
  case " $SKIP_UNTRACKED " in *" $(basename "$f") "*) continue ;; esac
  both=$((both + 1))
done
if [ "$hook_obj" != "$both" ]; then
  bad "兜底闸腿只看到 ${hook_obj} 枚带克隆面的电池，独立对账是 ${both}（判据在空转）"
elif [ "$hook_rc" = 0 ]; then
  ok "$hook_obj 枚的每处装架之后都挂了兜底闸，md5 豁免路全带让路标记（另有 ${SKIP_N:-0} 枚未跟踪旁道件不在面上：${SKIP_UNTRACKED:-无}）"
  # 读数并进同一条断言的输出：单开一条 `ok` 会让"断言处数"随一行说明文摆动，而格数／处数是
  # 下一位对账用的，不该被文案改数。
  echo "     现取读数：装架函数体之外可达退出 ${hook_exits} 条（本腿每次现扫，不作判据、随重构摆动）"
else
  # 明细必须整体当**一个**参数传进去：`bad()` 只印 `$1`，把明细不加引号地接在后面＝红因永远不显示
  # （上面那条 abort_leg 同款写法在本轮之前一直如此——撤 bail 的那次能点名是因为它走的是 ok 那条引号支）。
  bad "$hook_obj 枚里兜底闸形状不齐：$(printf '%s\n' "$hook_bad" | head -3 | tr '\n' ' ')"
fi
# 两条反向格各自绑一条判据分支（只判"红没红"会证到错的那条，见记忆里的同款教训）：
#   a) 摘掉一枚的兜底闸注册 ⇒ 必须点名"这处装架之后没挂兜底闸"（逐处判据那条分支）；
#   b) 摘掉一枚的让路标记 ⇒ 必须点名那一行"md5 豁免路没打让路标记"。
for kind in hook leave; do
  R="$WORK/rev-$kind/scripts"; mkdir -p "$R"; cp "$ROOT"/scripts/mut_*.py "$R/"
  python3 - "$R" "$kind" <<'PY'
import pathlib, re, sys
d, kind = pathlib.Path(sys.argv[1]), sys.argv[2]
pat = "dispose_at_exit(tmp" if kind == "hook" else "leave_for_evidence("
for p in sorted(d.glob("mut_*.py")):
    lines = p.read_text(encoding="utf-8").splitlines()
    for i, l in enumerate(lines):
        if pat in l:
            kept = [x for j, x in enumerate(lines) if j != i]
            p.write_text("\n".join(kept) + "\n", encoding="utf-8")
            print(f"反向格[{kind}]：摘掉 {p.name}:{i+1} 的『{pat}』")
            sys.exit(0)
sys.exit(f"反向格[{kind}]：找不到可摘的『{pat}』——这条腿的对象集是空的")
PY
  [ $? = 0 ] || { bad "反向格[$kind] 夹具没做成"; continue; }
  r_out=$(hook_leg "$R" $SKIP_UNTRACKED); r_rc=$?
  r_bad=$(printf '%s\n' "$r_out" | tail -n +2 | grep -v '^EXITS=' | head -1)
  if [ "$r_rc" = 0 ]; then
    bad "反向[$kind]：摘掉之后这条腿仍绿＝判据没牙"
  elif [ "$kind" = "hook" ] && ! printf '%s' "$r_bad" | grep -q "这处装架之后没挂兜底闸"; then
    bad "反向[hook]：红了，但红在别处（读到的不是'逐处装架都有闸'那条分支）：$r_bad"
  elif [ "$kind" = "leave" ] && ! printf '%s' "$r_bad" | grep -q "没打让路标记"; then
    bad "反向[leave]：红了，但红在别处（读到的不是'让路标记'那条分支）：$r_bad"
  else
    ok "反向[$kind]：当场点名 ⇒ 这条分支有牙（$(printf '%s' "$r_bad" | cut -c1-52)…）"
  fi
done

# 行为面（不在本门里跑）。下面那条探针只证"克隆之后的 raises 都写了 bail(...)"时 bail() 真把
# 克隆带走了。那一条的夹具已进仓：`scripts/probe-fleet-bail-reverse.sh`——PATH 前面挂一枚只对
# `checkout` 退 123、其余 exec 真 git 的 shim，让每一枚带 git-clone 面的电池**不带 --check** 地在
# `--clone <空目录>` 上跑一趟，断言 ① rc≠0 ② 红因是 checkout 那一条分支（不是别的退出）③ 外层目录
# 还在（不许越权删调用方交的目录）④ 里面的 clone/ 已被收尾闸带走。它不注册进任何门／CI：要改
# PATH、要按面上一枚一次真克隆（枚数见本门兜底闸腿的现取打印）、带 JS 相的三枚还要 --go-only 才进得了装架，挂进 CI 只会得到一台恒红的机器；
# 改了 bail/dispose/prepare 的写路径之后应当手动现取一次，别只信本门的静态面。
# 读数（`bash scripts/probe-fleet-bail-reverse.sh <影子克隆> <带 .env 的主树>`，
# 测于 tip e72a7638 ＋本文件的逐处注册腿与 b17 的第二处注册）：
#   SEEN=26｜PASS=25 FAIL=0｜ENV-BROKEN 未取证=1｜无 git-clone 面而跳过=10，整趟退 2
#   —— 这一趟的 ENV-BROKEN 那一枚是 mut_egress_pool_r30.py，卡在自己的"／tmp 剩 ≥ 20 GiB"前提上
#   （见下面那条坑），当时本机 13 GiB。
# 合并旁道 22 笔之后再取一次（tip `be08a8a9`，面比上一趟多一枚 `mut_webhook_ai_trigger.py`，
# 而本机 /tmp 腾到 45 GiB）：
#   SEEN=27｜PASS=27 FAIL=0｜ENV-BROKEN 未取证=0｜无 git-clone 面而跳过=10，整趟退 0
#   两面都是真跑，不是"数变了"就换口径：`SEEN == PASS + FAIL + ENV-BROKEN` 这一趟 27=27+0+0 对得上，
#   前一趟 26=25+0+1 也对得上；跳过的 10 枚不进 SEEN（它们压根没有 git-clone 面）。
#   行为面自此 27 枚全证 ⇒ 下面那条 egress 的"只有静态面"作废，但它的前提判据还在，见该条。
#   探针自己的反向对账（在同一克隆里把 mut_actionability_b17.py 的一处 `bail("checkout 失败` 改回
#   裸 raise，`ONLY=` 单跑那一枚）：`✗ …中止后外层剩「clone 」`，FAIL=1——不撤的时候那一枚是 ✓，
#   所以 ④ 那条判据确实有牙，不是橡皮章。**这一趟还顺带定了兜底闸的覆盖面**：注册行排在装架赋值
#   **之后**，而 checkout 死在 `go_prepare` **里面**，那时注册根本没执行 ⇒ 撤掉 bail 就漏
#   （残骸 108K，是被杀在 checkout 的骨架克隆）。所以 prepare 内部靠本门腿 ⑤（bail），main 侧靠
#   腿 ⑥（逐处注册）＋ G10，三面不能互相代替。
# 四处只有真跑一趟才看得见的坑（下次动这条探针前先读）：
#   · 带 --check 是错的探针：面上多数枚压根没这个 flag（argparse rc=2，夹具压根没进 prepare），
#     另有几枚的 --check 明写"只静态预检、不装架"⇒ 同样在 prepare 之前返回。两类各是几枚随重构摆动，
#     现取：`git ls-files scripts/mut_*.py | xargs grep -L -- '--check'`（再手工排掉没 git-clone 面的）。
#     不带 --check 才真进装架，而 checkout 被杀 ⇒ 走不到跑用例那一步，一趟还是只花几十秒。
#   · JS 相排在克隆之前的三枚（b17/b18/b20d）在**任何影子克隆里都缺 node_modules**（那是
#     gitignore 的）⇒ 原先它们在 `git clone` 之前就退在"缺 node_modules"，量到的是环境不是 bail，
#     整趟读数成 PASS=22／FAIL=3（2026-09-28 在干净克隆复跑当场露出来）。这版给带 `dst / "web"`
#     面的枚子加 `--go-only`，于是外层判据能收成"必须全空"（旧的"允许恰好一个 web/ ＋ 5 MB 上界"
#     随之删掉——留着它就是把"根本没进装架"读成"回收正确"）。顺带一条同款教训：给体积上界要用
#     `du -m -s`，不带 -s 就一个子目录印一行，那串喂给 `[ -gt ]` 得 integer expression expected
#     ⇒ 判据静默失效（第一版的 web/ 上界就这样）。
#   · mut_review_r22_teeth.py 的默认 --logs 目录在 HEAD 里就带 10 份已跟踪 .log ⇒ 任何干净克隆里
#     不带参数跑都会先在 prepare 之前撞"复用旧目录"那条判据，探针要显式给它一个新目录才进得了夹具。
#   · `mut_egress_pool_r30.py` 的夹具前提是自己量 /tmp 剩 ≥ 20 GiB 的一道 ENV-BROKEN 闸，且这道闸
#     也在 prepare 之前。2026-09-28 头一趟跑的时候本机只剩 13 GiB ⇒ 它那一路只有静态面，读数就是
#     上面那句 PASS=25／ENV-BROKEN=1；腾到 45 GiB 之后同一枚真进了装架（PASS=27／ENV-BROKEN=0）。
#     所以这条不是"已修"而是**会来回摆的读数**：盘又涨满时它会退回 ENV-BROKEN，那一趟的
#     `SEEN=27｜PASS=26｜ENV-BROKEN=1` 是盘的状态、不是回收面坏了，别拿它当红去改代码。

echo
# 汇总行原先写死"九格"，而本轮现数是 **24 处断言**（ok/bad 各调一次算一处）——写死的那句数的是什么
# 口径无从对照，唯一能确定的是它不随断言数变：加一条腿它照样印"九格"＝谎报。现改成现数，并带一条
# 下界（9）——只判"半路死掉、有腿没执行"，不钉死处数（钉死会让别人加/减一条腿时本门假红，
# [[feedback-mutation-battery-hygiene]]"共享树下控制组不许写死常量"同族）。
# 两个口径一起印，因为它们数的不是一回事：$N＝**真执行到的断言**（某条腿半路死掉它就偏小），
# $G＝本文件里**摆着的用例格**（从文件自己现取，写死"九格"那种读数每加一格就过期一次）。
# 只在 FAIL=0 那条分支取 $G：红的时候没必要再解析一遍自己，红因优先。
if [ "$N" -lt 9 ]; then
  echo "===== 用例：断言只跑到 ${N} 处（下界 9）＝有腿没执行，判红 ====="; exit 1
fi
if [ "$FAIL" = 0 ]; then
  G=$(grep -c '^echo "G[0-9]' "$0")
  echo "===== 用例：${G} 格／${N} 处断言全过（失败 0 处）====="; exit 0
fi
echo "===== 用例：${N} 处断言里 $FAIL 处失败 ====="; exit 1
