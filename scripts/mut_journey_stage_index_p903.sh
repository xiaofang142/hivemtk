#!/bin/bash
# 常驻变异电池：锁 customer_journey.go「按阶段列名单」这条读路径的五个判据面。
#   J1 ListByStage 退回只扫本实例 L1（＝跨实例看不见）
#   J2 索引成员不做权威回读（＝已离开该阶段的客户仍被列进名单）
#   J3 去掉「已进过这一格」哨兵（＝每次互动都往索引里推一条）
#   J4 名单不排序（＝返回顺序随写序）
#   J5 索引读失败回空表（＝把故障读成"这个阶段没有客户"）
#   J0probe -overlay 通路自证：编译器没读替换文件的话，本族任何「杀」都不可信
#
# 变异只走 `go test -overlay`，磁盘上的源码一个字节都不改（本仓是共享开发树，
# air 与别的泳道在并发编译同一个包）；每格前后都复算一次源文件 md5，变了立刻停。
#
# 用法：bash scripts/mut_journey_stage_index_p903.sh
#   LOGDIR=/path bash ...   指定取证目录（默认落在库内 ledger 的本卡轮次目录）
#   KEEP_LOG=1 bash ...     保留取证目录并在末行打印它（默认也保留，此变量只作对照开关）
#
# 退出码：0＝五刀全杀且前后控制组各跑过 ≥6 枚绑定用例、源文件 md5 前后一致；
#   1＝有刀存活/杀错/装架坏（末行 TALLY 给出分类计数）；6＝单格装架没编过；
#   7＝-overlay 通路自证失败；8＝锚点失配或控制组红/夹具缺失；9＝源文件被并发改动。
# 覆盖范围只在 TestCustomerJourney* 这族读路径用例上（`-run` 子集不是全量门禁，
# 全包回归跑 §2 步 5）；控制组以 -v 跑，PASS 名单逐枚对账，防「0 条匹配也绿」。
set -uo pipefail
export DEVELOPER_DIR=/Library/Developer/CommandLineTools

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
WS=$(dirname -- "$SCRIPT_DIR")
if [ ! -f "$WS/user-server/internal/service/customer_journey.go" ]; then
  echo "项目根推导失败：$WS 下没有 user-server/internal/service/customer_journey.go"
  exit 95
fi
cd "$WS/user-server" || exit 95

digest() {
  if command -v md5 >/dev/null 2>&1; then md5 -q "$1"
  else md5sum "$1" | awk '{print $1}'
  fi
}

JOURNEY="$WS/user-server/internal/service/customer_journey.go"
BASE_MD5=$(digest "$JOURNEY")

RUN_TAG=$(date +%Y%m%d-%H%M%S)
LOGDIR="${LOGDIR:-$WS/docs/superpowers/specs/ledger/logs/P903/$RUN_TAG}"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/mut_journey_XXXXXX")
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$LOGDIR"

TARGETS='TestCustomerJourney'
# 控制组以 -v 跑并断言这六枚绑定用例真的跑过：不带 -v 时 `--- PASS` 计数恒 0，
# "一个用例也没匹配到"和"全绿"在日志里长得一样，五刀会齐刷刷「存活」成假绿。
BOUND_TESTS="TestCustomerJourney_ListByStageSeesWritesFromAnotherInstance
TestCustomerJourney_ListByStageDropsMemberThatLeftTheStage
TestCustomerJourney_ListByStageIndexKeepsOneEntryPerCustomerPerStage
TestCustomerJourney_ListByStageOrderIsIndependentOfWriteOrder
TestCustomerJourney_ListByStageFallsBackToLocalViewWhenIndexFails
TestCustomerJourney_AutoDetectSleepingIndexesNewStage"
BOUND_MIN=6

# 测试库连接：口令只从 .env 读，脚本不落任何明文值
if [ -f "$WS/.env" ]; then
  set -a; . "$WS/.env"; set +a
fi
export POSTGRES_TEST_PORT="${DB_PORT:-8202}" EMBEDDING_ALLOW_FALLBACK=true
export POSTGRES_TEST_USER="${POSTGRES_USER:-}" POSTGRES_TEST_PASSWORD="${POSTGRES_PASSWORD:-}"

check_tree() {
  local now
  now=$(digest "$JOURNEY")
  if [ "$now" != "$BASE_MD5" ]; then
    echo "TREE_TOUCHED base=$BASE_MD5 now=$now —— 停：本电池只该改内存里的副本"
    exit 9
  fi
}

run_cell() {
  local name=$1 expect_fail=$2 out rc log
  log="$LOGDIR/$name.log"
  if [ "$name" = "CTRL" ] || [ "$name" = "POST-CTRL" ]; then
    out=$(go test -count=1 -v -run "$TARGETS" ./internal/service/ 2>&1); rc=$?
  else
    out=$(go test -count=1 -run "$TARGETS" -overlay "$WORK/$name.json" ./internal/service/ 2>&1); rc=$?
  fi
  printf '%s\n' "$out" > "$log"
  echo "[$name] rc=$rc fail=$(grep -c '^--- FAIL' "$log") pass=$(grep -cE '^ *--- PASS: TestCustomerJourney' "$log")"
  grep -E '^(ok|FAIL|# )' "$log" | head -4
  grep '^--- FAIL' "$log" | head -6
  if [ -n "$expect_fail" ]; then
    if [ "$rc" = 0 ]; then
      # 变异体跑绿了＝这一刀没被任何断言接住（「判据没牙」），不能和「杀错人」混成一类
      echo "[$name] SURVIVED：变异体全绿，绑定用例一枚都没接到红"
      return 0
    fi
    if ! grep -q -- '--- FAIL' "$log"; then
      # 进程级失败（装架编译不过 / panic 把整包带走）：这一格读数无效，不算杀也不算活
      echo "[$name] BROKEN：rc=$rc 但日志里零行「--- FAIL」，读数无效"
      return 6
    fi
    if ! grep -q -- "--- FAIL: $expect_fail" "$log"; then
      echo "[$name] WRONG_KILL：判据面没按预期开火（期望 ${expect_fail}）"
      return 7
    fi
    return 1
  fi
  # 控制组额外断言：绑定用例真的一枚枚跑过（测试文件被并行泳道改名时，
  # `-run` 会静默匹配 0 条并回 ok，那时的「五刀全存活」是夹具没了而不是产品坏了）
  local npass
  npass=$(grep -cE '^ *--- PASS: TestCustomerJourney' "$log")
  if [ "$npass" -lt "$BOUND_MIN" ]; then
    echo "[$name] FIXTURE_MISSING：绑定用例只跑过 $npass 枚（下界 ${BOUND_MIN}）"
    return 8
  fi
  local missing="" t
  for t in $BOUND_TESTS; do
    grep -q -- "--- PASS: $t" "$log" || missing="$missing $t"
  done
  if [ -n "$missing" ]; then
    echo "[$name] FIXTURE_MISSING：这些绑定用例没跑过 →$missing"
    return 8
  fi
  return $rc
}

cat > "$WORK/patch.py" <<'PY'
import sys

EDITS = {
    # 只扫本实例 L1：把权威读那一段整体短路掉
    "J1": (
        "\tif s.cache == nil {\n\t\treturn s.listByStageLocal(stage)\n\t}\n\tmembers, err := s.cache.LRange(ctx, journeyStageIndexKey(stage), 0, -1)",
        "\tif true { // MUT-J1 退回只扫 L1\n\t\treturn s.listByStageLocal(stage)\n\t}\n\tmembers, err := s.cache.LRange(ctx, journeyStageIndexKey(stage), 0, -1)",
        "MUT-J1",
    ),
    # 成员不做权威回读：旧阶段的索引条目直接算名单
    "J2": (
        "\t\tif s.authoritativeStage(ctx, cid) == stage {\n\t\t\tids = append(ids, cid)\n\t\t}",
        "\t\t_ = ctx // MUT-J2 去掉权威回读\n\t\tids = append(ids, cid)",
        "MUT-J2",
    ),
    # 去掉哨兵短路：每一次 persistState（含 Touch）都往索引里推一条
    "J3": (
        "\tif seen {\n\t\treturn\n\t}",
        "\t_ = seen // MUT-J3 去掉哨兵短路",
        "MUT-J3",
    ),
    # 名单不排序：顺序随写入到达序
    "J4": (
        "\tsort.Strings(ids)\n\treturn ids",
        "\t// MUT-J4 不排序\n\treturn ids",
        "MUT-J4",
    ),
    # 索引读失败回空表：故障与"这个阶段没人"同形
    "J5": (
        "\t\treturn s.listByStageLocal(stage)\n\t}\n\tids := []string{}",
        "\t\treturn []string{} // MUT-J5 故障回空表\n\t}\n\tids := []string{}",
        "MUT-J5",
    ),
    # -overlay 通路自证：编译红因里必须印出这个标识符
    "J0probe": (
        "func journeyStageIndexKey(stage JourneyStage) string {",
        "var MUT_J0probe_sentinel = MUT_J0probe_undefined{}\n\nfunc journeyStageIndexKey(stage JourneyStage) string {",
        "MUT_J0probe_sentinel",
    ),
}

path, tag = sys.argv[1], sys.argv[2]
old, new, marker = EDITS[tag]
s = open(path).read()
n = s.count(old)
if n != 1:
    sys.stderr.write("anchor count=%d (want 1)\n" % n)
    sys.exit(1)
out = s.replace(old, new)
if out.count(marker) != 1:
    sys.stderr.write("marker count=%d (want 1)\n" % out.count(marker))
    sys.exit(2)
open(path, "w").write(out)
print(marker)
PY

mk_cell() {
  local name=$1 marker
  cp "$JOURNEY" "$WORK/$name.go" || return 8
  if ! marker=$(python3 "$WORK/patch.py" "$WORK/$name.go" "$name"); then
    echo "[$name] PATCH_FAILED: $marker"
    return 8
  fi
  echo "[$name] landed_marker=$marker"
  python3 -c '
import json, sys
sys.stdout.write(json.dumps({"Replace": {sys.argv[1]: sys.argv[2]}}))
' "$JOURNEY" "$WORK/$name.go" > "$WORK/$name.json" || return 8
  python3 -c '
import json, sys
d = json.load(open(sys.argv[1]))
assert len(d["Replace"]) == 1, d
assert d["Replace"][sys.argv[2]] == sys.argv[3], d
' "$WORK/$name.json" "$JOURNEY" "$WORK/$name.go" || return 8
  return 0
}

echo "REPO=$WS"
echo "BASE_MD5=$BASE_MD5"
echo "LOGDIR=$LOGDIR"
echo
echo "=== J0probe：-overlay 通路自证（期望编译红，红因印出 undefined: MUT_J0probe_undefined） ==="
mk_cell "J0probe" || exit 8
probe_out=$(go test -count=1 -run "$TARGETS" -overlay "$WORK/J0probe.json" ./internal/service/ 2>&1)
probe_rc=$?
printf '%s\n' "$probe_out" > "$LOGDIR/J0probe.log"
echo "[J0probe] rc=$probe_rc"
printf '%s\n' "$probe_out" | grep -E 'MUT_J0probe_undefined|^FAIL|^# ' | head -4
if [ "$probe_rc" = 0 ] || ! printf '%s' "$probe_out" | grep -q 'MUT_J0probe_undefined'; then
  echo "OVERLAY_DEAD：编译器没读替换文件，本族任何「杀」都不可信"
  exit 7
fi
check_tree

echo
echo "=== 控制组（不带 overlay，须绿） ==="
run_cell "CTRL" "" || { echo "CONTROL_RED rc=$?"; exit 8; }
check_tree

echo
echo "=== J1: 退回只扫本实例 L1 ==="
mk_cell "J1" || exit 8
run_cell "J1" "TestCustomerJourney_ListByStageSeesWritesFromAnotherInstance"; j1=$?
check_tree

echo
echo "=== J2: 成员不做权威回读 ==="
mk_cell "J2" || exit 8
run_cell "J2" "TestCustomerJourney_ListByStageDropsMemberThatLeftTheStage"; j2=$?
check_tree

echo
echo "=== J3: 去掉哨兵短路 ==="
mk_cell "J3" || exit 8
run_cell "J3" "TestCustomerJourney_ListByStageIndexKeepsOneEntryPerCustomerPerStage"; j3=$?
check_tree

echo
echo "=== J4: 名单不排序 ==="
mk_cell "J4" || exit 8
run_cell "J4" "TestCustomerJourney_ListByStageOrderIsIndependentOfWriteOrder"; j4=$?
check_tree

echo
echo "=== J5: 索引读失败回空表 ==="
mk_cell "J5" || exit 8
run_cell "J5" "TestCustomerJourney_ListByStageFallsBackToLocalViewWhenIndexFails"; j5=$?
check_tree

echo
echo "=== 还原后复测控制组（不带 overlay 仍须绿） ==="
run_cell "POST-CTRL" "" || echo "POST_CTRL_RED=$?：撤码没撤干净或夹具缺失"
NOW_MD5=$(digest "$JOURNEY")
echo "TREE_MD5=$NOW_MD5 BASE=$BASE_MD5 UNCHANGED=$([ "$NOW_MD5" = "$BASE_MD5" ] && echo yes || echo no)"

# 五刀分类计数：只有「杀」（rc=1，红因正是绑定用例）算过；存活/杀错/装架坏一律让电池整体红。
killed=0; survived=0; wrong=0; broken=0
for pair in "J1=$j1" "J2=$j2" "J3=$j3" "J4=$j4" "J5=$j5"; do
  v=${pair#*=}
  case "$v" in
    1) killed=$((killed + 1)) ;;
    0) survived=$((survived + 1)) ;;
    7) wrong=$((wrong + 1)) ;;
    *) broken=$((broken + 1)) ;;
  esac
done
echo "TALLY killed=$killed survived=$survived wrong_kill=$wrong broken=$broken (共 5 刀)"
echo "SUMMARY J1=$j1 J2=$j2 J3=$j3 J4=$j4 J5=$j5 (1＝杀；0＝存活；7＝杀错判据；6＝装架坏；8＝夹具/环境缺)"
echo "LOGDIR=$LOGDIR"
if [ "$NOW_MD5" != "$BASE_MD5" ]; then
  echo "RESULT=BAD_TREE 源文件被改动过，本电池的前提没了"
  exit 9
fi
if [ "$killed" != 5 ]; then
  echo "RESULT=NOT_ALL_KILLED killed=$killed/5 survived=$survived wrong_kill=$wrong broken=$broken"
  exit 1
fi
echo "RESULT=OK killed=5/5（J0probe 自证通路 + 前后控制组各 $BOUND_MIN 枚绑定用例绿）"
