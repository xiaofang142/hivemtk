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
#   REAL 静态面（五腿）：**所有**带 --clone 面的常驻电池（枚数由 grep 现取，别照抄文档里的
#       "六枚/七枚"——写死的那个数每次有人加电池就过期一次）必须 ① --clone 面／入口闸面／收尾闸面三处
#       计数相等，② 不留 `Path(args.clone …)` 直连赋值，③ 不留裸 rmtree(tmp/dst/work)，
#       ④ 入口闸那一行必须排在 `prepare(tmp)` **之前**（顺序腿的反向测：把 workdir 挪到
#       clone 之后即红，见本轮 C 组同款做法），⑤ 克隆建起来之后的每条退出都走 `bail()`
#       （豁免三形："已存在"/"克隆失败"/"md5 不一致"；反向测在 `$WORK/rev` 里撤掉一处 bail）
#
# 两道闸的分工（别只看一道就以为安全了）：`dispose()` 是"退出时才发现"的下界，
# `workdir()` 是"装架之前就把危险入参挡掉"的上界。只有上界的理由是：电池在收尾之前
# 会往 `tmp` 里 `git clone --shared`、写补丁、跑 `go test`——`--clone .` 即使最后拒删，
# 中途也已经把克隆和改动落进了调用方的工作树（本轮事故跑的就是 `--clone . --check`）。
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

echo "REAL 静态面：所有带 --clone 面的电池都走 dispose，且不留裸 rmtree（枚数现取，不写死）"
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
  bad "$abort_obj 枚里有裸中止路：" $(printf '%s\n' "$abort_bad" | head -3 | tr '\n' ' ')
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

# 行为面（不在本门里跑）。上面那条腿只证"克隆之后的 raises 都写了 bail(...)"，不证 bail() 真把
# 克隆带走了。那一条的夹具已进仓：`scripts/probe-fleet-bail-reverse.sh`——PATH 前面挂一枚只对
# `checkout` 退 123、其余 exec 真 git 的 shim，让每一枚带 git-clone 面的电池**不带 --check** 地在
# `--clone <空目录>` 上跑一趟，断言 ① rc≠0 ② 红因是 checkout 那一条分支（不是别的退出）③ 外层目录
# 还在（不许越权删调用方交的目录）④ 里面的 clone/ 已被收尾闸带走。它不注册进任何门／CI：要本地
# node_modules＋改 PATH＋26 次真克隆，挂进 CI 只会得到一台恒红的机器；改了 bail/dispose/prepare 的
# 写路径之后应当手动现取一次，别只信本门的静态面。
# 读数（`bash scripts/probe-fleet-bail-reverse.sh <干净克隆> <带 .env 的主树>`，测于 tip 2c765be1）：
#   SEEN=26｜PASS=25 FAIL=0｜ENV-BROKEN 未取证=1｜无 git-clone 面而跳过=10，整趟退 2
#   探针自己的反向对账（在克隆里把 mut_actionability_b17.py 的一处 `bail("checkout 失败` 改回裸
#   raise，`ONLY=` 单跑那一枚）：`✗ …中止后外层剩「clone web 」`，FAIL=1、退 1——不撤的时候那一枚
#   是 ✓，所以 ④ 那条判据确实有牙，不是橡皮章。
# 四处只有真跑一趟才看得见的坑（下次动这条探针前先读）：
#   · 带 --check 是错的探针：26 枚里 16 枚没这个 flag（argparse rc=2，夹具压根没进 prepare），
#     另 4 枚的 --check 明写"只静态预检、不装架"⇒ 同样在 prepare 之前返回。不带 --check 才真进
#     装架，而 checkout 被杀 ⇒ 走不到跑用例那一步，一趟还是只花几秒。
#   · 有 `dst / "web"` 面的那几枚（JS 装架）中止后留一个 web/ 是 **dispose 契约里写明不许回收**的
#     ——它把调用方的 node_modules 拷/链了进来，rmtree 会顺着走到别人的依赖树上。所以探针判的是
#     "外层恰好剩 web/"，不是一律"外层必须空"；给它加体积上界时要用 `du -m -s`，不带 -s 就一个子
#     目录印一行，那串喂给 `[ -gt ]` 得 integer expression expected ⇒ 判据静默失效（第一版就这样）。
#   · mut_review_r22_teeth.py 的默认 --logs 目录在 HEAD 里就带 10 份已跟踪 .log ⇒ 任何干净克隆里
#     不带参数跑都会先在 prepare 之前撞"复用旧目录"那条判据，探针要显式给它一个新目录才进得了夹具。
#   · 行为面唯一没证到的那枚是 mut_egress_pool_r30.py：它自己的前提是 /tmp 剩 ≥ 20 GiB（本机 14），
#     这道 ENV-BROKEN 闸也在 prepare 之前，所以它的 bail 那一路只有静态面。没腾出那 7 GiB 之前，
#     不许把上面那句 PASS=25 读成 26 枚全证。

echo
# 汇总行原先写死"九格"，而本轮现数是 **24 处断言**（ok/bad 各调一次算一处）——写死的那句数的是什么
# 口径无从对照，唯一能确定的是它不随断言数变：加一条腿它照样印"九格"＝谎报。现改成现数，并带一条
# 下界（9）——只判"半路死掉、有腿没执行"，不钉死处数（钉死会让别人加/减一条腿时本门假红，
# [[feedback-mutation-battery-hygiene]]"共享树下控制组不许写死常量"同族）。
if [ "$N" -lt 9 ]; then
  echo "===== 用例：断言只跑到 ${N} 处（下界 9）＝有腿没执行，判红 ====="; exit 1
fi
if [ "$FAIL" = 0 ]; then
  echo "===== 用例：${N} 处断言全过（失败 0 处）====="; exit 0
fi
echo "===== 用例：${N} 处断言里 $FAIL 处失败 ====="; exit 1
