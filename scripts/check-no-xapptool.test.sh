#!/usr/bin/env bash
# =============================================================
# check-no-xapptool.test.sh —— 证明 scripts/check-no-xapptool.sh「能红」，
# 而不是恒绿的摆设。
#
# 判据是「增量」而不是「绝对归零」：迁移期仓库里本就还有几十处存量残留，
# 闸在夹具注入前后都必须红，因此本测试断言四件事：
#   ① 注入 1 行违规 → 命中数恰好 = 基线 + 1，且新红因指向夹具
#   ①b 换成「非 ASCII 文件名」的夹具再测一遍（枚举源对中文名会失明的老 bug 的回归锁）
#   ② 撤掉夹具     → 命中数恰好回到基线（证明没留下残迹）
#   ③ 注入前后 rc 一致（基线 0 则撤掉后也必须 0）
# 这样迁移未完成时它仍然有效（证明闸摸得到未追踪文件、减去得掉），
# 迁移完成后基线自然变成 0，等价于"归零"断言。
#
# 夹具清理只准 rm -f：本仓有 180+ 文件未提交在途工作，
# 任何 git add / checkout / restore / stash 都可能把别人的改动一起吞掉。
# =============================================================
set -eo pipefail

# 与被测闸同口径：夹具注入在**调用时所在的仓**，这样同一份反向测试能在
# hivemtk 与 hivemtk-platform 两棵树上各跑一遍、各证一次"这棵树的闸有牙"。
# 曾跟被测脚本一样把根钉在脚本所在仓，于是"platform 侧三腿 PASS"的记录数的是 hivemtk。
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "$ROOT" ]]; then
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi
cd "$ROOT"

# 闸按"脚本自己所在仓"给不了（platform 仓里没有这份脚本），必须按本测试的位置找它。
GATE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-no-xapptool.sh"
[[ -f "$GATE" ]] || { echo "BROKEN: 找不到被测闸 $GATE" >&2; exit 1; }

# 落在仓根：两棵树都一定有这个目录，且未跟踪的 .md 属于闸的枚举面①。
TMP="no-xapptool-fixture.md"
TMP_CN="旧域回流夹具-非ASCII名.md"
trap 'rm -f "$TMP" "$TMP_CN"' EXIT

# read_count <log> —— 从闸的 FAIL 摘要行取命中数；绿时为 0
# 注：管道必须带 || true。pipefail 下 grep 无匹配退 1 会让赋值语句失败，
# 叠加 set -e 就在"基线为 0"这一支直接掐死脚本（正是迁移完成后该绿的场景）。
read_count() {
  local n
  n="$( { grep -E '^FAIL no-xapptool: [0-9]+ hit\(s\)' "$1" || true; } | head -1 | tr -dc '0-9')"
  printf '%s\n' "${n:-0}"
}

# ---- 基线（夹具尚未写入） ----
if bash "$GATE" >/tmp/nxt-base.log 2>&1; then
  BASE_RC=0
else
  BASE_RC=1
fi
BASE_COUNT="$(read_count /tmp/nxt-base.log)"

# ---- ① 注入违规必须红，且红因是夹具、增量恰为 1 ----
printf '# fixture\n\nsee https://hive.xapptool.cn/\n' > "$TMP"

if bash "$GATE" >/tmp/nxt-red.log 2>&1; then
  echo "BROKEN: 注入违规后闸仍绿（枚举源没扫到未追踪夹具 / 白名单误伤 / grep 未命中）" >&2
  tail -3 /tmp/nxt-red.log >&2
  exit 1
fi
if ! grep -q 'no-xapptool-fixture.md' /tmp/nxt-red.log; then
  echo "BROKEN: 闸红了但红因不是注入的夹具（假红，别当通过）" >&2
  tail -5 /tmp/nxt-red.log >&2
  exit 1
fi
RED_COUNT="$(read_count /tmp/nxt-red.log)"
EXPECTED_RED=$((BASE_COUNT + 1))
if [[ "$RED_COUNT" != "$EXPECTED_RED" ]]; then
  echo "BROKEN: 注入 1 行后命中数应为 ${EXPECTED_RED}，实为 ${RED_COUNT}（判据没量到增量）" >&2
  exit 1
fi
echo "  ✓ 红：夹具被拦下（基线 $BASE_COUNT → ${RED_COUNT}，新增 1 行命中即夹具）"

# ---- ①b 非 ASCII 文件名的夹具也必须被扫到 ----
# 这一条是补上闸自己的假绿：git ls-files 不带 -z 时按 core.quotePath=true 把中文
# 文件名转义成 "\347\247…" 并加引号，枚举出来的路径在盘上不存在，
# 于是 [[ -f ]] 判假、静默 continue —— 实测 docs/architecture/FRP私域部署指南.md
# 的 14 处旧域就这么溜过去、闸还印 "OK 0 hits"。只测 ASCII 夹具抓不到这个洞。
rm -f "$TMP"
printf '# fixture（非 ASCII 文件名）\n\nsee https://hiveuser.xapptool.cn/\n' > "$TMP_CN"

if bash "$GATE" >/tmp/nxt-red-cn.log 2>&1; then
  echo "BROKEN: 中文名夹具里的违规没被拦下（枚举源又被非 ASCII 路径噎住了）" >&2
  tail -3 /tmp/nxt-red-cn.log >&2
  exit 1
fi
if ! grep -q '旧域回流夹具-非ASCII名.md' /tmp/nxt-red-cn.log; then
  echo "BROKEN: 闸红了但红因不是中文名夹具（假红，别当通过）" >&2
  tail -5 /tmp/nxt-red-cn.log >&2
  exit 1
fi
CN_COUNT="$(read_count /tmp/nxt-red-cn.log)"
if [[ "$CN_COUNT" != "$((BASE_COUNT + 1))" ]]; then
  echo "BROKEN: 中文名夹具注入后命中数应为 $((BASE_COUNT + 1))，实为 $CN_COUNT" >&2
  exit 1
fi
echo "  ✓ 红：非 ASCII 文件名夹具同样被拦下（基线 $BASE_COUNT → ${CN_COUNT}）"

# ---- ② 撤掉全部夹具必须回到基线 ----
rm -f "$TMP" "$TMP_CN"

if bash "$GATE" >/tmp/nxt-green.log 2>&1; then
  AFTER_RC=0
else
  AFTER_RC=1
fi
AFTER_COUNT="$(read_count /tmp/nxt-green.log)"
if [[ "$AFTER_RC" != "$BASE_RC" ]]; then
  echo "BROKEN: 撤夹具后 rc 由 $BASE_RC 变成 ${AFTER_RC}（夹具留了残迹，或闸不稳定）" >&2
  tail -5 /tmp/nxt-green.log >&2
  exit 1
fi
if [[ "$AFTER_COUNT" != "$BASE_COUNT" ]]; then
  echo "BROKEN: 撤夹具后命中数 $AFTER_COUNT ≠ 基线 ${BASE_COUNT}（残迹未清）" >&2
  tail -5 /tmp/nxt-green.log >&2
  exit 1
fi
if [[ "$BASE_RC" == "0" ]]; then
  grep -q 'OK no-xapptool: 0 hits' /tmp/nxt-green.log
  echo "  ✓ 绿：基线已归零，撤夹具后仍为 0 hits"
else
  echo "  ✓ 归位：撤夹具后回到基线 $BASE_COUNT 处（迁移期存量，非本测试引入）"
fi

echo "PASS check-no-xapptool.test.sh（红→归位双向增量均已实测）"
