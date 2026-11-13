#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""mark_batch.py — N2+ 轮批量落证：读 /tmp/nr-batch.json（nr-batch.mjs 的批次结果），
把「11-13 N2轮<形态>」证据按行追加进 router-list.md。与 mark.py 同口径（清单维护，
不开浏览器、不发请求）。

用法：
  ego-browser 跑完一批（nr-batch.mjs 会写 /tmp/nr-batch.json）后：
    python3 scripts/regression/mark_batch.py <形态标签>     # 如 N2轮独立 / N2轮SaaS
  形态标签会原样进注记前缀；contentOK=false 的行自动追加「主区空挂载!」哨兵。

注意：nr-batch.json 是临时文件（/tmp），本脚本即读即弃；清单才是持久证据。
"""
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
LIST = ROOT / "router-list.md"
BATCH = Path("/tmp/nr-batch.json")


def main():
    form = sys.argv[1] if len(sys.argv) > 1 else "N2轮"
    if not BATCH.exists():
        sys.exit(f"批次结果 {BATCH} 不存在（先跑 nr-batch.mjs）")
    results = json.loads(BATCH.read_text(encoding="utf-8"))

    rows = {}
    lines = LIST.read_text(encoding="utf-8").splitlines()
    for i, ln in enumerate(lines):
        m = re.match(r"^\| (\d+) \|", ln)
        if m:
            rows[int(m.group(1))] = i

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
        clicked = r.get("clicked", "只读点验")
        badtxt = "；".join(bad) if bad else "无"
        cflag = "" if r.get("contentOK") in (True, "unknown") else "，主区空挂载!"
        note = f"11-13 {form}：{clicked}，console.error={cerr}，非2xx：{badtxt}{cflag}"
        last = len(cells) - 2
        tail = cells[last].rstrip()
        sep = "" if not tail else "；"
        cells[last] = (tail + sep + note) if tail else note
        lines[idx] = "|".join(cells)

    LIST.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"已落证 {len(results)} 行（{form}）")


if __name__ == "__main__":
    main()
