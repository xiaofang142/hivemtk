#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""mark.py — 维护 router-list.md 清单的行级状态与轮次证据（任务四 逐页点验辅助）。

与 scripts/gen_router_list.py 是一对：gen 管「路由索引生成（保留既有勾选）」，
mark 管「测完一行就地落状态与证据」。只做文本维护：不开浏览器、不发请求、不改代码。

CLAUDE.md 规则-1 点名允许的辅助之一（「scripts/gen_router_list.py 与 mark.py 维护清单」），
2026-11-13 实测该文件此前一直缺失（规则引用落空），本轮补齐。

用法：
  python3 scripts/mark.py --stats                     # 统计各状态条数
  python3 scripts/mark.py 217 --status x              # 第 217 行状态置 [x]
  python3 scripts/mark.py 217 --note "11-13 M轮：..."  # 该行末列追加轮次证据（自动带 ；分隔）
  python3 scripts/mark.py 217 --status ~ --note "..." # 两者同做
  python3 scripts/mark.py 217 --show                  # 打印该行当前内容

状态图例（与 router-list.md 头部一致）：
  x=逐页点验通过  ~=设计内拒绝  !=发现缺陷待修  空=未测
"""
import argparse
import re
import sys

from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
LIST = ROOT / "router-list.md"

ROW_RE = re.compile(r"^\| (\d+) \|")


def load_rows():
    lines = LIST.read_text(encoding="utf-8").splitlines()
    rows = {}
    for i, ln in enumerate(lines):
        m = ROW_RE.match(ln)
        if m:
            rows[int(m.group(1))] = i
    return lines, rows


def split_cells(line):
    # 行形如 | a | b | ... |（首尾都是 |）；split 后首元素与末元素是空串
    return line.split("|")


def join_cells(cells):
    return "|".join(cells)


def main():
    ap = argparse.ArgumentParser(description="router-list.md 行级状态/证据维护")
    ap.add_argument("row", nargs="?", type=int, help="清单行号（第一列 # 值）")
    ap.add_argument("--status", choices=["x", "~", "!", " ", ""], help="目标状态标记")
    ap.add_argument("--note", help="追加到行末的轮次证据（原样追加，前置 ；分隔）")
    ap.add_argument("--stats", action="store_true", help="打印状态统计")
    ap.add_argument("--show", action="store_true", help="打印该行当前内容")
    args = ap.parse_args()

    lines, rows = load_rows()

    if args.stats:
        counts = {"x": 0, "~": 0, "!": 0, " ": 0}
        for i in rows.values():
            cells = split_cells(lines[i])
            if len(cells) > 2:
                raw = cells[2].strip()
                # 单元格形态：[x] / [~] / [!] / 空
                key = raw.strip("[]") if raw.startswith("[") else (raw or " ")
                counts[key if key in counts else " "] += 1
        print(f"总计 {len(rows)} 行：[x]={counts['x']} [~]={counts['~']} [!]={counts['!']} [ ]={counts[' ']}")
        return

    if args.row is None:
        ap.error("需要行号或 --stats")
    if args.row not in rows:
        sys.exit(f"行号 {args.row} 不在清单中（1..{max(rows)}）")
    idx = rows[args.row]
    cells = split_cells(lines[idx])
    if len(cells) < 10:
        sys.exit(f"行 {args.row} 列数异常（{len(cells)-2} 列），拒绝盲改")

    if args.show:
        print(lines[idx])
        return

    changed = False
    if args.status is not None:
        old = cells[2].strip()
        cells[2] = f" {args.status.strip()} " if args.status.strip() else "  "
        if old != cells[2].strip():
            changed = True
    if args.note:
        last = len(cells) - 2  # 末列（倒数第二段；最后一段是行尾 | 后的空串）
        tail = cells[last].rstrip()
        sep = "" if tail.endswith(("；", ";", "|")) or not tail else "；"
        # 行末列若已有内容以 |； 结尾（历史上 gen 脚本把注记放 | 外），统一收进列内
        cells[last] = (tail + sep + args.note) if tail else args.note
        changed = True

    if not changed:
        print("无改动（未给 --status/--note）")
        return

    lines[idx] = join_cells(cells)
    LIST.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"行 {args.row} 已更新：")
    print(lines[idx][:300] + ("…" if len(lines[idx]) > 300 else ""))


if __name__ == "__main__":
    main()
