#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""o_mark.py — O轮线上只读探针落证：读 /tmp/o-probe.json，把「11-13 O轮线上」证据
追加进 router-list.md。口径同 mark_batch.py；contentOK 由 rn(路由已解析)+mainLen 判定
（登录/404/chat embed 页无 .el-main 主区，按 body 挂载判定）。
"""
import json
import os
import re
import sys
from pathlib import Path

ROOT = Path(os.environ.get("REPO_ROOT", Path(__file__).resolve().parents[2]))
LIST = ROOT / "router-list.md"
BATCH = Path(os.environ.get("PROBE_JSON", "/tmp/o-probe.json"))


def main():
    if not BATCH.exists():
        sys.exit(f"{BATCH} 不存在（先跑探针）")
    results = json.loads(BATCH.read_text(encoding="utf-8"))
    lines = LIST.read_text(encoding="utf-8").splitlines()
    rows = {}
    for i, ln in enumerate(lines):
        m = re.match(r"^\| (\d+) \|", ln)
        if m:
            rows[int(m.group(1))] = i
    marked = 0
    for r in results:
        n = r.get("n")
        if n not in rows:
            print(f"行 {n} 不存在，跳过")
            continue
        idx = rows[n]
        cells = lines[idx].split("|")
        if len(cells) < 10:
            print(f"行 {n} 列数异常，跳过")
            continue
        cerr = r.get("cerr", "?")
        bad = r.get("bad") or []
        badtxt = "；".join(bad) if bad else "无"
        mounted = bool(r.get("rn")) or (r.get("mainLen") or 0) > 20
        cflag = "" if mounted else "，主区空挂载!"
        extra = ""
        if r.get("dlg"):
            extra = f"，对话框×{r['dlg']}已确认"
        if r.get("err"):
            extra += f"，探针异常:{r['err'][:60]}"
        note = f"11-13 O轮线上：只读点验，console.error={cerr}，非2xx：{badtxt}，主区渲染正常{cflag}{extra}"
        last = len(cells) - 2
        tail = cells[last].rstrip()
        # 幂等：同一 JSON 重跑时跳过，避免重复追加（不同内容的证据仍正常叠加，
        # 如邮件模块 83-86 行的「显式点击」+「只读点验」两层证据）
        if note in tail:
            print(f"行 {n} 已有相同注记，跳过")
            continue
        sep = "" if not tail else "；"
        cells[last] = (tail + sep + note) if tail else note
        lines[idx] = "|".join(cells)
        marked += 1
    LIST.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"已落证 {marked} 行（O轮线上只读）")


if __name__ == "__main__":
    main()
