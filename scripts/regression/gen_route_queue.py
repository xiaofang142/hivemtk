#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""gen_route_queue.py — 由 router-list.md 生成逐路由回归队列（导航次序用）。

只做文本处理（读清单、写 TSV），不开浏览器。队列是 nr-batch.mjs 的导航输入：
每行 = 行号\\t状态\\t原始路径\\t实参化路径\\t名称\\t需登录。

实参化口径（与历轮一致）：
  :pathMatch(.*)*  → /no-such-page-xyz
  :channel_ref     → k_test_20261110
  :id / :key 等     → 1（:key 为模块键的页面另按清单注记核对）
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
LIST = ROOT / "router-list.md"
OUT = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("/tmp/n-round-queue.tsv")

ROW_RE = re.compile(r"^\| (\d+) \| (\[[x~! ]\]|\S) \| `([^`]*)` \| (\S*) \| ([^|]*) \| ([^|]*) \|")


def materialize(path: str) -> str:
    if ":pathMatch" in path:
        return "/no-such-page-xyz"
    if ":channel_ref" in path:
        return path.replace(":channel_ref", "k_test_20261110")
    return re.sub(r":([A-Za-z_]+)(\([^)]*\))?", "1", path)


def main():
    rows = []
    for ln in LIST.read_text(encoding="utf-8").splitlines():
        m = ROW_RE.match(ln)
        if not m:
            continue
        num, st, path, name, _title, login = (
            int(m.group(1)), m.group(2), m.group(3).strip(), m.group(4),
            m.group(5), m.group(6).strip())
        rp = materialize(path)
        # 与 nr-batch.mjs 的 6 列解析对齐：行号\\t状态\\t原始路径\\t实参化路径\\t名称\\t需登录
        rows.append((num, st, path, rp, name, login))
    OUT.write_text(
        "\n".join("\t".join([str(n), st, p, r, nm, lg]) for n, st, p, r, nm, lg in rows) + "\n",
        encoding="utf-8",
    )
    print(f"队列写入 {OUT}：{len(rows)} 行")


if __name__ == "__main__":
    main()
