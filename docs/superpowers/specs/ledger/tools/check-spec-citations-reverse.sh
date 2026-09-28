#!/usr/bin/env bash
# check-spec-citations.py 的门牙反向测：三种坏引用各自红在正确的桶里，控制组绿。
# 用法：bash docs/superpowers/specs/ledger/tools/check-spec-citations-reverse.sh
# 设计约束：**绝不写回仓里的任何文件**——注码只打在 /tmp 的副本上（原文件逐字节不动，
# 所以本脚本没有"还原"这一步要做，也就没有还原失败这一类风险）。
set -u
cd "$(git rev-parse --show-toplevel)" || exit 1
TOOL=docs/superpowers/specs/ledger/tools/check-spec-citations.py
SPEC=docs/superpowers/specs/2026-09-19-browser-automation-write-ledger-design.md
HEAD='## 9. '
fail=0

# G2 的靶子必须是**裸文件名**引用：工具早期版本的裸名分支只判"文件在不在"、不查行号，
# 拿带路径的引用注行号会红，却证不到裸名那条分支——第一版就是这么漏过去的。
python3 - "$SPEC" <<'PY'
import pathlib, sys
text = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
i = text.index("## 9. ")
head, body = text[:i], text[i:]
jobs = [("badpath", "repository/retention_a6_test.go", "nosuchdir_zz/retention_a6_test.go"),
        ("badline", "migrate.go:383", "migrate.go:999999"),
        ("badev", "a6a2-newlegs-fail-text-r22lane3.log", "a6a2-ZZZ-no-such-evidence.log")]
for name, old, new in jobs:
    b = body.replace(old, new, 1)
    assert b != body, f"注码没落地：{name}（锚点 {old!r} 不在节内，别把红读成没红）"
    pathlib.Path(f"/tmp/cite_{name}.md").write_text(head + b, encoding="utf-8")
print("三份坏副本装在 /tmp，原文件未动")
PY

cell() { # $1=标签 $2=spec 路径 $3=期望桶关键字
  local out rc
  out=$(python3 "$TOOL" "$2" "$HEAD" 2>&1); rc=$?
  if [ "$3" = "绿" ]; then
    [ "$rc" = "0" ] && { echo "✓ $1 rc=0"; return; }
    echo "判错 ✗ $1：控制组不绿，后面所有红/绿都不可信"; echo "$out" | tail -4; fail=1; return
  fi
  if [ "$rc" = "0" ]; then echo "判错 ✗ $1：坏引用没让工具红"; fail=1; return; fi
  if ! echo "$out" | grep -q "$3"; then
    echo "判错 ✗ $1：红了但桶不是 $3"; echo "$out" | tail -4; fail=1; return
  fi
  echo "✓ $1 rc=$rc 且点名 $3"
}

cell "控制组（原样）"            "$SPEC"      "绿"
cell "G1 路径写歪"               /tmp/cite_badpath.md  "UNRESOLVABLE=[1-9]"
cell "G2 裸名引用行号越界"        /tmp/cite_badline.md  "BEYOND_EOF=[1-9]"
cell "G3 证据文件名查无"          /tmp/cite_badev.md    "MISSING_EVIDENCE=[1-9]"
rm -f /tmp/cite_badpath.md /tmp/cite_badline.md /tmp/cite_badev.md

n=4
echo "===== 引用复量反向测：$([ "$fail" = 0 ] && echo "$n/$n 种坏形态各自点名到该开火的那一格" || echo '有格判错') ====="
exit "$fail"
