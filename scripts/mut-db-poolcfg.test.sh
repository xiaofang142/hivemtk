#!/usr/bin/env bash
# mut_db_poolcfg.py 的用例：把"四刀的预检有牙 + 危险 --clone 被挡"钉成判据。
#
# 为什么这份用例存在：`mut_db_poolcfg.py` 是 §23.22 第 6 段③ 的结转项——那"四刀 4/4 全杀"
# 原先由一枚一次性 `/tmp` 驱动产出，驱动不在树里 ⇒ 读数只能引用、无法复跑。常驻化如果只是
# 把脚本抄进 `scripts/`，判据内核（`check_cells`）坏成"恒报 0 格有问题"也没人知道，
# 那等于把一次性证据换成了**会骗人的**常驻证据。
#
# 这一族只跑**不需要 go、不需要测试库**的腿（全族杀伤要编 Go，见脚本头部的用法）：
#   T1 预检内核的五格内存反向格 ⇒ rc=0，末行"失败 0 格"
#   T2 对真源码的锚点预检（`--check-tree`）⇒ rc=0 且"4 格，0 格有问题"
#   T3 反向：把真源码里 K1 那枚锚点删掉再喂判据 ⇒ 必须点名 1 格（证明 T2 的绿不是空判）
#   T4 反向：expect 的父用例名换成本包里不存在的名字 ⇒ 必须点名 1 格（化石 expect 那条腿）
#   T5 独立复算：四枚锚点的关键行各在 `db.go` 里命中恰好 1 行（用 grep 数行，不读脚本自己的计数）
#   T6 删除闸：`--clone .` 必须**装架之前**退非 0，且工作树分毫未动（§23.22 第 1 段的事故本体）
#   T7 删除闸：`--clone <仓库根的上级>` 同样退非 0
#
# T5 与 T2 是两份独立真相：T2 走脚本自己的 `count()==1` 判据，T5 用 grep 数行——判据内核与
# 喂给它的文本若一起坏掉，只有 T5 会红。
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BATT="$ROOT/scripts/mut_db_poolcfg.py"
SRC="$ROOT/user-server/internal/pkg/db/db.go"
FAIL=0

ok() { echo "  ✓ $1"; }
bad() { echo "  ✗ $1"; FAIL=$((FAIL + 1)); }

[ -f "$BATT" ] || { echo "FATAL: 找不到 ${BATT}"; exit 1; }
[ -f "$SRC" ] || { echo "FATAL: 找不到 ${SRC}（T2/T5 的前提没了，不是树的红）"; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo "FATAL: 没有 python3 ⇒ 本用例没跑过任何东西"; exit 2; }

echo "T1 预检内核的内存反向格"
t1=$(cd "$ROOT" && python3 scripts/mut_db_poolcfg.py --selftest 2>&1)
rc=$?
echo "$t1" | tail -2
if [ "$rc" -eq 0 ] && echo "$t1" | grep -q '预检自测：5 格，失败 0 格'; then
  ok "rc=0 且末行是『失败 0 格』（5 格逐条点名）"
else
  bad "rc=${rc} 或末行读数不对（须 rc=0 ＋『失败 0 格』）"
fi

echo "T2 对真源码的锚点预检（--check-tree）"
t2=$(cd "$ROOT" && python3 scripts/mut_db_poolcfg.py --check-tree 2>&1)
rc=$?
echo "$t2" | tail -1
if [ "$rc" -eq 0 ] && echo "$t2" | grep -q '锚点校验：4 格，0 格有问题'; then
  ok "四枚锚点各命中 1 次、注码会落地、杀手用例有定义"
else
  bad "rc=${rc}：锚点或杀手名单不对（先修锚点，别改期望）"
fi

echo "T3 反向：真源码里删掉 K1 的锚点 ⇒ 判据必须点名那一格"
t3=$(python3 - "$ROOT" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]) / "scripts"))
import mut_db_poolcfg as m
root = Path(sys.argv[1])
text = (root / m.SRC_REL).read_text(encoding="utf-8")
tests = m.defined_tests(root / m.PKG_REL)
hit = text.count(m.CELLS[0][2])
bad, lines = m.check_cells(text.replace(m.CELLS[0][2], ""), m.CELLS, tests)
named = sum(1 for ln in lines if ln.startswith("  ✗"))
print(f"K1 锚点在原文件命中={hit} 删掉后判据 bad={bad} 点名行数={named}")
sys.exit(0 if (hit == 1 and bad == 1 and named == 1) else 1)
PY
)
rc=$?
echo "  $t3"
[ "$rc" -eq 0 ] && ok "删掉锚点即点名（判据不是恒 0）" || bad "rc=${rc}：锚点失守没被抓到，T2 的绿不可信"

echo "T4 反向：expect 的父用例名换成不存在的 ⇒ 必须点名"
t4=$(python3 - "$ROOT" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]) / "scripts"))
import mut_db_poolcfg as m
root = Path(sys.argv[1])
text = (root / m.SRC_REL).read_text(encoding="utf-8")
tests = m.defined_tests(root / m.PKG_REL)
cells = [list(c) for c in m.CELLS]
cells[1][4] = "TestNoSuchFallbacks/只缺_MaxOpenConns⇒补_20"
bad, lines = m.check_cells(text, cells, tests)
named = sum(1 for ln in lines if ln.startswith("  ✗"))
print(f"化石 expect 判据 bad={bad} 点名行数={named}")
sys.exit(0 if (bad == 1 and named == 1) else 1)
PY
)
rc=$?
echo "  $t4"
[ "$rc" -eq 0 ] && ok "化石 expect 被点名" || bad "rc=${rc}：expect 指向不存在的用例却没被抓到"

echo "T5 独立复算：四枚锚点关键行在 db.go 里的命中行数（grep 数行，不读脚本的计数）"
# 用 here-doc 而不是管道：管道右侧的 while 在子 shell 里跑，FAIL 计数会丢（bash 检查四死法之一）
while IFS='|' read -r code pat; do
  n=$(grep -c -F "$pat" "$SRC")
  echo "  ${code} 命中 ${n} 行｜$(grep -n -F "$pat" "$SRC" | head -1)"
  if [ "$n" -eq 1 ]; then
    ok "${code} 锚点关键行命中 1 行"
  else
    bad "${code} 锚点关键行命中 ${n} 行（须 1；多命中＝判据会打到别人的分支，零命中＝锚点已搬家）"
  fi
done <<EOF
K1|sqlDB.SetMaxOpenConns(poolConfig.MaxOpenConns)
K2|poolConfig.MaxOpenConns = 20
K3|poolConfig.ConnMaxLifetime = int((30 * time.Minute).Seconds())
K4|poolConfig = config.DefaultPoolConfig
EOF

echo "T6 删除闸：--clone .（仓库根本身）必须装架之前退，且工作树分毫未动"
before=$(cd "$ROOT" && git status --porcelain | wc -l | tr -d ' ')
t6=$(cd "$ROOT" && python3 scripts/mut_db_poolcfg.py --clone . --check 2>&1)
rc=$?
echo "  $(echo "$t6" | head -2)"
after=$(cd "$ROOT" && git status --porcelain | wc -l | tr -d ' ')
if [ "$rc" -ne 0 ]; then
  ok "危险入参当轮退非 0（rc=${rc}）"
else
  bad "rc=0：--clone . 没被入口闸挡住（这就是 §23.22 第 1 段那起事故的形态）"
fi
[ -e "$ROOT/clone" ] && bad "工作树里出现了 clone/（装架已经动过仓库根）" || ok "仓库根里没有 clone/"
if [ "$before" = "$after" ]; then
  ok "脏文件数前后一致（${before}）"
else
  bad "脏文件数从 ${before} 变成 ${after}：拒绝之前已经改过树"
fi

echo "T7 删除闸：--clone <仓库根的上级> 同样退"
t7=$(cd "$ROOT" && python3 scripts/mut_db_poolcfg.py --clone "$(dirname "$ROOT")" --check 2>&1)
rc=$?
echo "  $(echo "$t7" | head -1)"
[ "$rc" -ne 0 ] && ok "上级目录当轮退非 0（rc=${rc}）" || bad "rc=0：--clone 指到上级没被挡"

echo "──────"
if [ "$FAIL" -gt 0 ]; then
  echo "===== 用例：失败 ${FAIL} 处 ====="
  exit 1
fi
echo "===== 用例：七格全过（断言失败 0 处）====="
