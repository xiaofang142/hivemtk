#!/usr/bin/env bash
# mut_webhook_ai_trigger.py 的用例：把"五刀的预检有牙 + 危险 --clone 被挡"钉成判据。
#
# 为什么这份用例存在：电池本体（`python3 scripts/mut_webhook_ai_trigger.py`）要编 Go、要测试库，
# 因此在 CI 里挂不住；但它的判据内核（`check_cells` 与 `classify`）坏成"恒报没问题／恒报 KILLED"
# 时，五格的绿就是装饰。这一族只跑**不需要 go、不需要测试库**的腿，把内核的牙钉成常驻判据。
#
#   T1 预检内核的内存反向格 ⇒ rc=0，末行"失败 0 格"
#   T2 对真源码的锚点预检（`--check-tree`）⇒ rc=0 且"5 格，0 格有问题"
#   T3 反向：把真源码里那枚锚点删掉再喂判据 ⇒ 必须五格全部点名（证明 T2 的绿不是空判）
#   T4 反向：expect 的父用例名换成本包里不存在的名字 ⇒ 必须点名 1 格（化石 expect 那条腿）
#   T5 反向：把"必须仍绿"名单同时放进"必须红"名单 ⇒ 必须点名（自相矛盾的一格不许算干净杀掉）
#   T6 独立复算：两处锚点整行各命中 1 行、三条杀手用例名各在 *_test.go 里命中（grep 数行）
#   T7 删除闸：`--clone .` 必须**装架之前**退非 0，且工作树分毫未动
#   T8 删除闸：`--clone <仓库根的上级>` 同样退非 0
#
# T6 与 T2 是两份独立真相：T2 走脚本自己的 `count()==1` 判据，T6 用 grep 数行——判据内核与
# 喂给它的文本若一起坏掉，只有 T6 会红。V5 的锚点是**另一行**（GateHandled 守卫块），所以
# T6 数两枚锚点而不是数那一行：只数触发行时，守卫块被人删走会让第五刀无声变成"锚点没牙"。
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BATT="$ROOT/scripts/mut_webhook_ai_trigger.py"
SRC="$ROOT/user-server/internal/service/webhook.go"
PKG="$ROOT/user-server/internal/service"
FAIL=0

ok() { echo "  ✓ $1"; }
bad() { echo "  ✗ $1"; FAIL=$((FAIL + 1)); }

[ -f "$BATT" ] || { echo "FATAL: 找不到 ${BATT}"; exit 1; }
[ -f "$SRC" ] || { echo "FATAL: 找不到 ${SRC}（T2/T6 的前提没了，不是树的红）"; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo "FATAL: 没有 python3 ⇒ 本用例没跑过任何东西"; exit 2; }

echo "T1 预检内核的内存反向格"
t1=$(cd "$ROOT" && python3 scripts/mut_webhook_ai_trigger.py --selftest 2>&1)
rc=$?
echo "$t1" | tail -3
if [ "$rc" -eq 0 ] && echo "$t1" | grep -q '预检自测：6＋4 格，失败 0 格'; then
  ok "rc=0 且末行是『失败 0 格』（6 格预检反向＋4 格 classify 反向）"
else
  bad "rc=${rc} 或末行读数不对（须 rc=0 ＋『失败 0 格』）"
fi

echo "T2 对真源码的锚点预检（--check-tree）"
t2=$(cd "$ROOT" && python3 scripts/mut_webhook_ai_trigger.py --check-tree 2>&1)
rc=$?
echo "$t2" | tail -1
if [ "$rc" -eq 0 ] && echo "$t2" | grep -q '锚点校验：5 格，0 格有问题'; then
  ok "两处锚点各命中 1 次、注码会落地、杀手用例有定义、红绿名单不相交"
else
  bad "rc=${rc}：锚点或杀手名单不对（先修锚点，别改期望）"
fi

echo "T3 反向：真源码里删掉那枚锚点 ⇒ 判据必须五格全部点名"
t3=$(python3 - "$ROOT" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]) / "scripts"))
import mut_webhook_ai_trigger as m
root = Path(sys.argv[1])
text = (root / m.SRC_REL).read_text(encoding="utf-8")
tests = m.defined_tests(root / m.PKG_REL)
hit = text.count(m.ANCHOR)
bad, lines = m.check_cells("", m.CELLS, tests)
named = sum(1 for ln in lines if ln.startswith("  ✗"))
print(f"锚点在原文件命中={hit} 删掉后判据 bad={bad} 点名行数={named}")
sys.exit(0 if (hit == 1 and bad == len(m.CELLS) and named == len(m.CELLS)) else 1)
PY
)
rc=$?
echo "  $t3"
[ "$rc" -eq 0 ] && ok "删掉锚点即五格点名（判据不是恒 0）" || bad "rc=${rc}：锚点失守没被抓到，T2 的绿不可信"

echo "T4 反向：expect 的父用例名换成不存在的 ⇒ 必须点名"
t4=$(python3 - "$ROOT" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]) / "scripts"))
import mut_webhook_ai_trigger as m
root = Path(sys.argv[1])
text = (root / m.SRC_REL).read_text(encoding="utf-8")
tests = m.defined_tests(root / m.PKG_REL)
cells = [list(c) for c in m.CELLS]
cells[1][4] = ["TestBatchK_RenamedAway/WeCom"]
bad, lines = m.check_cells(text, cells, tests)
named = sum(1 for ln in lines if ln.startswith("  ✗"))
print(f"化石 expect 判据 bad={bad} 点名行数={named}")
sys.exit(0 if (bad == 1 and named == 1) else 1)
PY
)
rc=$?
echo "  $t4"
[ "$rc" -eq 0 ] && ok "化石 expect 被点名" || bad "rc=${rc}：expect 指向不存在的用例却没被抓到"

echo "T5 反向：红名单与绿名单相交 ⇒ 必须点名"
t5=$(python3 - "$ROOT" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]) / "scripts"))
import mut_webhook_ai_trigger as m
root = Path(sys.argv[1])
text = (root / m.SRC_REL).read_text(encoding="utf-8")
tests = m.defined_tests(root / m.PKG_REL)
cells = [list(c) for c in m.CELLS]
cells[2][5] = sorted(list(cells[2][5]) + [m.FEISHU])
bad, lines = m.check_cells(text, cells, tests)
named = sum(1 for ln in lines if ln.startswith("  ✗"))
print(f"自相矛盾名单 判据 bad={bad} 点名行数={named}")
sys.exit(0 if (bad == 1 and named == 1) else 1)
PY
)
rc=$?
echo "  $t5"
[ "$rc" -eq 0 ] && ok "同一名字既要求红又要求绿被点名" || bad "rc=${rc}：判据自身的矛盾没被抓到"

echo "T6 独立复算：两枚锚点整行与三条杀手用例名的命中行数（grep 数行，不读脚本的计数）"
n=$(grep -c -F "if triggerAI && channel != ChannelQQ {" "$SRC")
echo "  触发块锚点命中 ${n} 行｜$(grep -n -F "if triggerAI && channel != ChannelQQ {" "$SRC" | head -1)"
if [ "$n" -eq 1 ]; then
  ok "触发块锚点整行命中 1 行"
else
  bad "触发块锚点整行命中 ${n} 行（须 1；多命中＝V1–V4 会打到别人的分支，零命中＝锚点已搬家）"
fi
g=$(grep -c -F "tgExtra.GateHandled {" "$SRC")
echo "  守卫块锚点命中 ${g} 行｜$(grep -n -F "tgExtra.GateHandled {" "$SRC" | head -1)"
if [ "$g" -eq 1 ]; then
  ok "守卫块锚点整行命中 1 行（V5 的落点）"
else
  bad "守卫块锚点整行命中 ${g} 行（须 1；零命中＝第五刀没牙，V5 会永不开火）"
fi
while IFS='|' read -r code pat; do
  n=$(grep -l -F "func ${pat}(" "$PKG"/*.go 2>/dev/null | wc -l | tr -d ' ')
  echo "  ${code} 定义所在文件数 ${n}"
  if [ "$n" -ge 1 ]; then
    ok "${code} 在用例文件里有定义"
  else
    bad "${code} 查无定义：expect 是化石，这一格会红在 no tests to run"
  fi
done <<EOF
NONQQ|TestBatchK_NonQQHomeChannelsTriggerAIExactlyOnce
QQ|TestBatchK_QQHandleJobDoesNotDoubleTriggerAI
TGGATE|TestTGGateHandledSuppressesSalesTrigger
EOF

echo "T7 删除闸：--clone .（仓库根本身）必须装架之前退，且工作树分毫未动"
before=$(cd "$ROOT" && git status --porcelain | wc -l | tr -d ' ')
t7=$(cd "$ROOT" && python3 scripts/mut_webhook_ai_trigger.py --clone . --check 2>&1)
rc=$?
echo "  $(echo "$t7" | head -2)"
after=$(cd "$ROOT" && git status --porcelain | wc -l | tr -d ' ')
if [ "$rc" -ne 0 ]; then
  ok "危险入参当轮退非 0（rc=${rc}）"
else
  bad "rc=0：--clone . 没被入口闸挡住"
fi
[ -e "$ROOT/clone" ] && bad "工作树里出现了 clone/（装架已经动过仓库根）" || ok "仓库根里没有 clone/"
if [ "$before" = "$after" ]; then
  ok "脏文件数前后一致（${before}）"
else
  bad "脏文件数从 ${before} 变成 ${after}：拒绝之前已经改过树"
fi

echo "T8 删除闸：--clone <仓库根的上级> 同样退"
t8=$(cd "$ROOT" && python3 scripts/mut_webhook_ai_trigger.py --clone "$(dirname "$ROOT")" --check 2>&1)
rc=$?
echo "  $(echo "$t8" | head -1)"
[ "$rc" -ne 0 ] && ok "上级目录当轮退非 0（rc=${rc}）" || bad "rc=0：--clone 指到上级没被挡"

echo "──────"
if [ "$FAIL" -gt 0 ]; then
  echo "===== 用例：失败 ${FAIL} 处 ====="
  exit 1
fi
echo "===== 用例：八格全过（断言失败 0 处）====="
