#!/usr/bin/env python3
"""机械复量一份 spec 某一节里对外的**引用**：路径按字面解析、行号不越过文件末尾、
证据文件名必须在盘上。存在理由：文档里写「`foo.go:810` 那句是 X」时，人读起来与真命中
长得一模一样；只有把每一处坐标拿去对磁盘才能分辨。

用法：python3 docs/superpowers/specs/ledger/tools/check-spec-citations.py \
        <spec 相对路径> <该节起始标题前缀，如 "## 9. ">

判据形状（三格反向在 R22 那趟实跑里各自点过名）：
  1. 写成路径的引用（含 `/`）必须能按字面解析到仓里的一个文件；`…/x/y.go` 视为前缀省略，
     按后缀逐段匹配。匹配不到 = UNRESOLVABLE。
  2. 裸文件名引用（不含 `/`）必须在仓里存在同名文件（同一行里的包名负责消歧，本工具只判存在），
     且其行号不得越过**同名站点里最长那份**的文件末尾——裸名不查行号＝22/34 的引用可以随便写行号。
  3. 引用的行号（含区间上界）不得越过目标文件实际行数。
  4. `.log` / `.txt` / `.jsonl` / `report-*.md` / `lane-*.md` 形态的证据文件名必须在
     `docs/superpowers/specs/ledger/` 下现测存在（按 basename 找，允许目录写全或只写名字）。
退出码：有任何 UNRESOLVABLE / BEYOND EOF / missing 证据 = 1（计数为 0 也 = 1：
「一节里一条引用都没有」不是绿，是这条复量没跑）。
"""
import os
import subprocess
import sys
import re

SRC = (".go", ".js", ".ts", ".tsx", ".jsx", ".py", ".sh", ".vue", ".html", ".yaml", ".yml",
       ".sql", ".jsonc", ".json", ".md", ".jsonl", ".txt", ".tsx")


def repo_root():
    return subprocess.run(["git", "rev-parse", "--show-toplevel"],
                          capture_output=True, text=True, check=True).stdout.strip()


def main():
    if len(sys.argv) != 3:
        print(__doc__)
        return 2
    spec_rel, heading = sys.argv[1], sys.argv[2]
    root = repo_root()
    spec = os.path.join(root, spec_rel)
    lines = open(spec, encoding="utf-8").read().split("\n")
    try:
        start = next(i for i, l in enumerate(lines) if l.startswith(heading))
    except StopIteration:
        print(f"标题前缀 {heading!r} 在 {spec_rel} 里没命中 —— 复量没跑，不是绿")
        return 1
    body = lines[start:]

    byname = {}
    for dp, dn, fn in os.walk(root):
        if "/.git" in dp or "/node_modules" in dp:
            continue
        for f in fn:
            if f.endswith(SRC):
                byname.setdefault(f, []).append(os.path.join(dp, f))

    prefixes = ("user-server", "user-server/internal", "user-server/internal/browser_automation",
                "user-web", "user-web/bridge", "scripts", "docs/superpowers/specs",
                "docs/superpowers/specs/ledger", ".")

    def resolve_written(p):
        if p.startswith("/"):
            tail = p.lstrip("/")
            base = os.path.basename(tail)
            for s in byname.get(base, []):
                if os.path.relpath(s, root).endswith(tail):
                    return s
            return None
        for c in [os.path.join(root, p)] + [os.path.join(root, r, p) for r in prefixes]:
            if os.path.isfile(c):
                return c
        return None

    CITE = re.compile(r"([\w.\-/@]+\.(?:%s)):(\d+)(?:-(\d+))?"
                     % "|".join(x.lstrip(".") for x in SRC))
    full_ok, full_bad, bare, bad_line = set(), [], set(), []
    for off, l in enumerate(body):
        ln = start + off + 1
        for m in CITE.finditer(l):
            p, a, b = m.group(1), int(m.group(2)), m.group(3)
            hi = int(b) if b else a
            if "/" in p:
                f = resolve_written(p)
                if f is None:
                    full_bad.append((ln, m.group(0)))
                    continue
                full_ok.add(m.group(0))
                n = sum(1 for _ in open(f, encoding="utf-8", errors="replace"))
                if hi > n:
                    bad_line.append((ln, m.group(0), n))
            else:
                sites = byname.get(p, [])
                bare.add(m.group(0))
                if not sites:
                    full_bad.append((ln, m.group(0) + " [仓里无同名文件]"))
                    continue
                # 裸名可能有多个同名站点（本仓 `executor.go` 就有两包各一份），
                # 但无论落在哪一站，行号都不许越过**最长**那一站的末尾。
                longest = max(sum(1 for _ in open(s, encoding="utf-8", errors="replace"))
                              for s in sites)
                if hi > longest:
                    bad_line.append((ln, m.group(0), longest))

    EV = re.compile(r"[\w./\-@]+\.(?:log|txt|jsonl)|report-[\w]+\.md|lane-[\w\-]+\.md")
    ev, missing = set(), []
    for l in body:
        for m in EV.finditer(l):
            ev.add(m.group(0))
    ledger = os.path.join(root, "docs/superpowers/specs/ledger")
    for name in sorted(ev):
        base = os.path.basename(name)
        if os.path.isfile(os.path.join(root, "docs/superpowers/specs", name)):
            continue
        if any(f == base for _, _, fn in os.walk(ledger) for f in fn):
            continue
        missing.append(name)

    print(f"复量节：{spec_rel} 起于 {heading!r}（{len(body)} 行）")
    print(f"按字面可解析的路径引用：{len(full_ok)}    裸文件名引用：{len(bare)}")
    print(f"证据文件名：{len(ev)}")
    print(f"UNRESOLVABLE={len(full_bad)}  BEYOND_EOF={len(bad_line)}  MISSING_EVIDENCE={len(missing)}")
    for x in full_bad:
        print("   不可解析 ", x)
    for x in bad_line:
        print("   越过末尾 ", x)
    for x in missing:
        print("   证据缺失 ", x)
    if not (full_ok or bare or ev):
        print("计数全 0 —— 这一节没有任何对外引用？复量没跑，判红")
        return 1
    return 1 if (full_bad or bad_line or missing) else 0


if __name__ == "__main__":
    sys.exit(main())
