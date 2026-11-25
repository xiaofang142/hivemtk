#!/usr/bin/env python3
"""api-list.md 勾选标记助手（**只改台账，不发任何请求**）。

用法：
  python3 scripts/mark_api_list.py 40 41-45 --mark x
  python3 scripts/mark_api_list.py --list '~'

范围：只翻 api-list.md 里「状态」列那一格；不碰方法/路径/档位/源文件/行号。
逐条 curl 验证本身由主 Agent 手工执行（任务三明令禁止脚本批量发请求），
本脚本只负责把「已经手工验过」的结论落到台账上。
"""
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from path_guard import safe_resolve  # noqa: E402  路径闸：台账读写不得越出仓库

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
# 路径固定为仓库内台账：realpath+commonpath 闸拒绝 ../ / 符号链接逃逸。
LIST = safe_resolve("docs/verification/api-list.md", ROOT)
ROW = re.compile(r"^(\|\s*)(\d+)(\s*\|\s*)\[([ xX~!-])\](\s*\|.*)$", re.M)


def parse_ranges(spec):
    out = []
    for part in spec.split(","):
        part = part.strip()
        if not part:
            continue
        if "-" in part:
            a, b = part.split("-", 1)
            out.extend(range(int(a), int(b) + 1))
        else:
            out.append(int(part))
    return out


def main():
    args = sys.argv[1:]
    if not args:
        print(__doc__)
        return 1
    if args[0] == "--list":
        want = args[1] if len(args) > 1 else "~"
        txt = open(LIST, encoding="utf-8").read()
        for m in ROW.finditer(txt):
            if m.group(4) == want:
                print(m.group(0).rstrip())
        return 0
    if "--mark" in args:
        i = args.index("--mark")
        nums = parse_ranges(",".join(args[:i]))
        mark = args[i + 1]
    else:
        print(__doc__)
        return 1
    txt = open(LIST, encoding="utf-8").read()
    want = set(nums)
    seen = set()

    def repl(m):
        n = int(m.group(2))
        if n in want:
            seen.add(n)
            return "%s%d%s[%s]%s" % (m.group(1), n, m.group(3), mark, m.group(5))
        return m.group(0)

    txt = ROW.sub(repl, txt)
    open(LIST, "w", encoding="utf-8").write(txt)
    missing = sorted(want - seen)
    if missing:
        print("未匹配到的行号：%s" % missing)
    print("已标记 %d 条为 [%s]" % (len(seen), mark))
    return 0


if __name__ == "__main__":
    sys.exit(main())
