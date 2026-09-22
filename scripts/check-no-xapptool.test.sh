#!/usr/bin/env bash
# =============================================================
# check-no-xapptool.test.sh —— 证明 scripts/check-no-xapptool.sh「能红」，
# 而不是恒绿的摆设。
#
# 判据是「增量」而不是「绝对归零」：迁移期仓库里曾有几十处存量残留，闸在夹具注入前后
# 都是红的，按绝对值判会把它自己的存量账算成夹具的错（2026-09-23 实测两仓基线已归零，
# 但判据不改回绝对值——下次再进存量时这条腿仍然有效）。
# 因此本测试断言五件事：
#   ① 注入 1 行违规 → 命中数恰好 = 基线 + 1，且新红因指向夹具
#   ①b 换成「非 ASCII 文件名」的夹具再测一遍（枚举源对中文名会失明的老 bug 的回归锁）
#   ② 撤掉夹具     → 命中数恰好回到基线（证明没留下残迹）
#   ③ 注入前后 rc 一致（基线 0 则撤掉后也必须 0）
#   ④ 双份枚举回归锁：注码打在"两条枚举源都摸得到"的 .env 形状文件上，
#      命中必须恰 +1 且该文件只点名一次（防 scanned 虚高 / 一处命中双计）
# 这样"迁移未完成"与"迁移已完成"两种仓库状态下同一条判据都成立。
#
# 夹具清理：①/①b 用的是自己新建的未追踪文件，rm -f 即可；④ 改的是仓库已有文件，
# 走 cp 备份 → 追加 → 还原 → md5 全等校验，且还原挂在 trap 上（set -e 中途死也要回位）。
# 全程不碰 git add / checkout / restore / stash —— 本仓有 180+ 文件未提交在途工作，
# 任何 git 侧操作都可能把别人的改动一起吞掉。
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

# ---- ④ 双份枚举回归锁：注码要落在"两条枚举源都摸得到"的那个文件上 ----
# ①/①b 打在仓根未追踪的 .md 上，那只证明"未追踪的文件扫得到"。但闸的枚举面是两条源并起来的：
#   源① `git ls-files …` 给 `user-web/.env.example`，源② `find`（为扫 gitignore 掉的 .env 而存在）
#   给 `./user-web/.env.example` —— 同一文件两副面孔，**去重一旦排在剥 `./` 之前就折不掉**，
#   该文件会进最终清单两次；而那份清单同时是 `xargs grep` 的输入 ⇒ 一处命中被扫两遍、
#   `FAIL … N hit(s)` 翻倍、同一行 `file:line` 印两遍。实测中招的正是
#   `.env.example` / `models.env` 这类"给人抄的示例基址"落点（本仓 5 个、platform 副本 4 个），
#   也就是本闸最该数准的地方。这条腿把那一面钉住：注入 1 行必须**恰** +1，且该文件只许点名一次。
file_md5() {
  if command -v md5 >/dev/null 2>&1; then md5 -q "$1"; else md5sum "$1" | cut -d' ' -f1; fi
}

pick_dual() {
  local f enumerated
  while IFS= read -r -d '' f; do
    f="${f#./}"
    grep -qF "\"$f\"" "$GATE" && continue          # 白名单里的项闸会跳过，注了也测不到
    enumerated="$(git ls-files --cached --others --exclude-standard -- "$f" 2>/dev/null || true)"
    [[ -n "$enumerated" ]] || continue             # 必须源①也摸得到，才谈得上"两条源各给一次"
    [[ -f "$f" ]] && { printf '%s\n' "$f"; return 0; }
  done < <(find . \( -name node_modules -o -name .git -o -name dist -o -name build -o -name vendor \) -prune \
             -o -type f \( -name '.env' -o -name '.env.*' -o -name '*.env' \) -print0)
  return 1
}

count_prefix() { # count_prefix <日志> <路径> —— 只数**以该路径开头**的点名行（`.env.example`
  # 是 `user-web/.env.example` 的子串，用包含匹配会把别人的行也算进来）
  awk -v p="$2" 'index($0, p ":") == 1 { n++ } END { printf "%d\n", n + 0 }' "$1"
}

DUAL="$(pick_dual || true)"
if [[ -z "$DUAL" ]]; then
  # 显式 SKIP：这棵树没有"两条源都摸得到"的文件 ⇒ 双计面无注码可打。
  # 不许静默当通过（缺证据的一格要印出来，否则下次改宽口径没人知道少了什么）。
  echo "  SKIP ④：本树没有同时被两条枚举源摸到的 .env 形状文件，双份枚举面未测（不是测过且通过）"
else
  DUAL_BAK="$(mktemp "${TMPDIR:-/tmp}/nxt-dual.XXXXXX")"
  cp "$DUAL" "$DUAL_BAK"
  DUAL_MD5_0="$(file_md5 "$DUAL")"
  # 前两条腿的夹具是自己新建的未追踪文件（rm -f 即可）；这条腿**改的是仓库里已有的文件**，
  # 所以还原必须挂在任何退出路径上（含 set -e 的中途死法）。
  restore_dual() {
    [[ -n "${DUAL_BAK:-}" && -f "$DUAL_BAK" ]] || return 0
    cp "$DUAL_BAK" "$DUAL"
    rm -f "$DUAL_BAK"
    DUAL_BAK=""
  }
  trap 'rm -f "$TMP" "$TMP_CN"; restore_dual' EXIT

  printf '\n# fixture 见 https://hiveuser.xapptool.cn/\n' >> "$DUAL"
  if bash "$GATE" >/tmp/nxt-dual.log 2>&1; then
    echo "BROKEN: 往 $DUAL 注入违规后闸仍绿（该文件没进枚举，或白名单误伤）" >&2
    tail -3 /tmp/nxt-dual.log >&2
    restore_dual
    exit 1
  fi
  DUAL_HITS="$(count_prefix /tmp/nxt-dual.log "$DUAL")"
  DUAL_COUNT="$(read_count /tmp/nxt-dual.log)"
  if [[ "$DUAL_COUNT" != "$((BASE_COUNT + 1))" ]]; then
    echo "BROKEN: 往 $DUAL 注入 1 行后命中数应为 $((BASE_COUNT + 1))，实为 $DUAL_COUNT" >&2
    restore_dual
    exit 1
  fi
  if [[ "$DUAL_HITS" != "1" ]]; then
    echo "BROKEN: $DUAL 的一处命中被点名 $DUAL_HITS 次（应为 1 次；同一文件进了清单两次＝去重在剥 ./ 之前做）" >&2
    restore_dual
    exit 1
  fi
  restore_dual
  if [[ "$(file_md5 "$DUAL")" != "$DUAL_MD5_0" ]]; then
    echo "BROKEN: 还原后 $DUAL 的 md5 与注码前不等（备份链断了，别再信下面的绿）" >&2
    exit 1
  fi
  if bash "$GATE" >/tmp/nxt-dual-after.log 2>&1; then
    DUAL_AFTER_RC=0
  else
    DUAL_AFTER_RC=1
  fi
  DUAL_AFTER="$(read_count /tmp/nxt-dual-after.log)"
  if [[ "$DUAL_AFTER_RC" != "$BASE_RC" || "$DUAL_AFTER" != "$BASE_COUNT" ]]; then
    echo "BROKEN: 撤掉 $DUAL 的夹具后未回到基线（rc $BASE_RC→$DUAL_AFTER_RC，命中 $BASE_COUNT→$DUAL_AFTER）" >&2
    exit 1
  fi
  echo "  ✓ 双份枚举：$DUAL 注入 1 行 → 命中恰 +1 且只点名 1 次；还原 md5 全等、闸回基线"
fi

echo "PASS check-no-xapptool.test.sh（红→归位双向增量 + 双份枚举唯一性均已实测）"
