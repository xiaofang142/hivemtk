#!/usr/bin/env bash
# =============================================================================
# check-shell-cjk-expansion.sh —— 「$VAR 紧跟中文」防回流闸
#
# 立项原因（2026-09-22 离线部署批，实测复现于本机）：bash 3.2（macOS /bin/bash 至今是
# 3.2.57）在 **LC_CTYPE 为 UTF-8** 时，把紧跟在非 ASCII 字符前的 `$NAME` 展开坏掉——
# 实测同时吃掉「变量值的那一字节」和「其后那个中文字符的首字节」：
#   W=$(...| wc -l | tr -d ' ')   # 值为 "2"
#   printf '%s' "delete=$W，各应" | xxd -p
#     LC_CTYPE=C          → 6465 6c65 7465 3d32 efbc8c…   （正确：delete=2，各应）
#     LC_CTYPE=C.UTF-8    → 6465 6c65 7465 3db     c8c…   （错：2 和 ef 一起没了）
# 触发条件在本仓完全现实：GitHub runner 默认 LC_CTYPE=C.UTF-8；macOS 上从 GUI/脚本
# 派生的进程也常带 UTF-8 的 LC_CTYPE。同一条消息在开发机（LC_CTYPE=C）永远是对的，
# 于是这个缺陷只会在 CI 上出现——而且先坏的是**判据要读的那行字**。
#
# 危害分两档，都拦：
#   1) 消息档（红因/提示变乱码）：本批排查 rotate-admin-password.sh 时，C4/C5 两格
#      变异被误判成 WRONG-REASON，就是因为红因里的计数被吃了字节 —— 白白多查一轮。
#   2) 数据档（真把内容改坏）：拼 JSON / SQL / 正则 / 路径的行一旦命中，产出的是
#      畸形内容（例：scripts/perf/rag-bench.sh:67 的 `-d "…#$i，用于压测…"）。
#
# 修法只有一个，且不改变任何语义：给展开加花括号 —— `${VAR}` 后跟中文实测正常。
#
# 判据：仓内所有 *.sh / *.bash 的**非注释行**里，凡处于可展开上下文
# （双引号内 或 引号外）的 `$NAME`，其后一个字节不得是非 ASCII。
# 豁免：单引号内（bash 不展开，写出来就是字面量）、被反斜杠转义的 `\$`、
#       已带花括号的 `${NAME}`。
#
# 棘轮：存量残留记在 scripts/shell-cjk-expansion.baseline 里（按 文件<TAB>命中数），
# 只许降不许升；出现新落点文件、或某文件命中数超过它的基线 ⇒ 红。
#
# 覆盖面与盲区（照本仓惯例写清楚，别让绿读数大于它的证明力）：
#   - 门只保证「这种形状不再新增」，不保证脚本行为正确；也不证明 shellcheck 跑过。
#   - 逐行判引号态：跨行字符串/heredoc 里的行会被当成独立行看待。落在 heredoc 内的
#     `#` 开头行会被误当注释豁免（实测本仓当前 0 处这种形状）；heredoc 里的
#     `$VAR`+中文若不在注释行上则能正常命中。
#   - 只看 *.sh / *.bash：*.bats、Makefile 里的 recipe、以及 user-web 下的 .js 模板串
#     不在本门范围（那些语言没有这个展开规则）。
#   - 用 os.walk 而不是 shell glob 枚举，且显式打印 scanned 数 —— 之前踩过
#     「非 ASCII 文件名 glob 不上，门恒绿」的坑。
#
# 用法：bash scripts/check-shell-cjk-expansion.sh
#   rc=0 通过；rc=1 有新增落点/超过基线；rc=2 环境或判据本身有问题（含基线缺失、
#   总数为 0 但基线非 0、扫描根不对、脚本被改名导致基线失去自指）
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BASELINE_REL="scripts/shell-cjk-expansion.baseline"
BASELINE_FILE="$SCRIPT_DIR/shell-cjk-expansion.baseline"
SELF="$SCRIPT_DIR/check-shell-cjk-expansion.sh"

[ -d "$PROJECT_ROOT/scripts" ] || { echo "::error::扫描根不对（$PROJECT_ROOT 下没有 scripts/）"; exit 2; }
[ -f "$SELF" ] || { echo "::error::找不到闸自身 $SELF —— 无法做基线自指核对"; exit 2; }
if [ ! -f "$BASELINE_FILE" ]; then
  echo "::error::缺少基线文件 $BASELINE_REL —— 没有基线时本门等于零覆盖"
  exit 2
fi

# 判据本体用 python 写：bash 3.2 正是本门要拦的那个缺陷的载体，用它量它自己不可靠。
python3 - "$PROJECT_ROOT" "$BASELINE_FILE" "$SELF" "$BASELINE_REL" <<'PY'
import os, re, sys

root, baseline_file, self_path, baseline_rel = sys.argv[1:5]
# 前置是「可展开的 $NAME」，后置是非 ASCII。(?<![$\\]) 只排 `\$`（转义）与 `$$` 的第二个 $；
# 名字字符打头的前缀（"v$ver（"）不豁免 —— 上一版把 \w 放进后视里，漏掉了 website/deploy.sh:87。
PAT = re.compile(r'(?<![$\\])\$([A-Za-z_][A-Za-z0-9_]*)(?=[^\x00-\x7f])')

def expandable(line, idx):
    """该位置的 $ 是否会被 bash 展开：单引号内不展开，其余按双引号奇偶照常展开。"""
    d = s = esc = False
    for i, ch in enumerate(line):
        if i >= idx:
            break
        if esc:
            esc = False
            continue
        if ch == '\\':
            esc = True
            continue
        if not d and ch == "'":
            s = not s
        elif not s and ch == '"':
            d = not d
        # 行内注释起点（前面已有空格且未被引号包住）之后的都算注释
        if ch == '#' and not d and not s and (i == 0 or line[i - 1] in ' \t'):
            return False
    return not s

hits, scanned, skipped_decode = {}, 0, []
for dirpath, dirs, files in os.walk(root):
    dirs[:] = sorted(x for x in dirs if x not in
                     {".git", "node_modules", "dist", "vendor", ".qoder", ".worktrees", "build", "testdata"})
    for fn in sorted(files):
        if not fn.endswith((".sh", ".bash")):
            continue
        p = os.path.join(dirpath, fn)
        try:
            lines = open(p, encoding="utf-8").read().split("\n")
        except (UnicodeDecodeError, OSError) as e:
            skipped_decode.append(f"{os.path.relpath(p, root)}: {e}")
            continue
        scanned += 1
        for n, line in enumerate(lines, 1):
            if line.lstrip().startswith("#"):
                continue
            for m in PAT.finditer(line):
                if not expandable(line, m.start()):
                    continue
                hits.setdefault(os.path.relpath(p, root), []).append((n, line.strip()[:110]))

# 自指：基线文件里必须写着本闸的文件名，否则哪天改名会把基线孤儿化（门恒绿而没人知道）。
base_txt = open(baseline_file, encoding="utf-8").read()
if "check-shell-cjk-expansion.sh" not in base_txt:
    print("::error::基线里没提到本闸的文件名 —— 改名会让基线孤儿化，先补注释行")
    sys.exit(2)

baseline = {}
comment_only = 0
for raw in base_txt.split("\n"):
    line = raw.strip()
    if not line or line.startswith("#"):
        comment_only += 1
        continue
    parts = line.split("\t")
    if len(parts) != 2 or not parts[1].isdigit():
        print(f"::error::基线行格式应为 文件<TAB>命中数，实为「{raw}」")
        sys.exit(2)
    baseline[parts[0]] = int(parts[1])
if not baseline:
    print("::error::基线里没有任何条目 —— 要么门坏了，要么该把基线写成 0 条（一次显式决策）")
    sys.exit(2)

total = sum(len(v) for v in hits.values())
base_total = sum(baseline.values())
print(f"scanned={scanned} 个 shell 文件，命中 {total} 处（基线 {base_total} 处）")
if skipped_decode:
    print("::error::有文件按 UTF-8 读不出来，本门无法判它：\n  " + "\n  ".join(skipped_decode))
    sys.exit(2)
if total == 0 and base_total > 0:
    print("::error::命中数为 0 但基线非 0 —— 先确认枚举与正则仍然有效，别把它当收敛")
    sys.exit(2)

bad = []
for f, items in sorted(hits.items()):
    allow = baseline.get(f)
    if allow is None:
        bad.append((f, len(items), "基线里没有这个文件（新落点）", items))
    elif len(items) > allow:
        bad.append((f, len(items), f"超过基线 {allow}", items))
gone = [f for f in baseline if f not in hits]
if gone:
    print("提示：以下基线条目已归零，请把它们从基线里删掉，把收敛固化：" + ", ".join(sorted(gone)))

if bad:
    print("::error::出现「$VAR 紧跟中文」的 bash 3.2 字节吞噬形状：")
    for f, n, why, items in bad:
        print(f"  {f}  {n} 处  ——  {why}")
        for ln, text in items:
            print(f"    :{ln} {text}")
    print("改法：给展开加花括号（$FOO 后跟中文 ⇒ ${FOO} 后跟中文），语义不变。")
    sys.exit(1)

if total > base_total:
    print("::error::总数超过基线（{} > {}）—— 逐条落点见上".format(total, base_total))
    sys.exit(1)

print("✅ 未发现新的「$VAR+中文」展开形状（基线内的残留属别的泳道热文件，见基线注释）")
PY
