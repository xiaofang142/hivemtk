#!/usr/bin/env python3
"""S-36 静态门：controller 不得把 err.Error() 放进 ErrorFromDB 的 fallback/data。

判据（2026-11-05 二次审计）：
  1. `response.ErrorFromDB(c, err, fallback, data...)` 的第 3 个及之后的实参
     不得出现任何 `.Error()` 调用（`err.Error()` / `e.Error()` 同样算）；
  2. `response.ErrorWithBusinessCode(c, bizerr.CodeInternal, msg, ...)` 的 msg
     不得出现 `.Error()`（5000 业务码同样是对外错误）。

为什么单独成脚本而不是 grep：调用可能跨行，错误串也可能在字符串里带逗号；
这里按括号/引号/注释做最小词法扫描，拿到完整实参再判。
排除 `_test.go` 与 response 包自身（它的 c.JSON 是唯一合法出口）。
"""

from __future__ import annotations

import pathlib
import re
import sys

TARGET = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else ".")
ROOT = TARGET / "internal"


def strip_comments_and_strings(src: str) -> str:
    """把注释和字符串内容替换成空格，保留长度与换行，便于找调用边界。"""
    out = list(src)
    i = 0
    n = len(src)
    while i < n:
        ch = src[i]
        if ch == "/" and i + 1 < n and src[i + 1] == "/":
            while i < n and src[i] != "\n":
                out[i] = " "
                i += 1
            continue
        if ch == "/" and i + 1 < n and src[i + 1] == "*":
            out[i] = out[i + 1] = " "
            i += 2
            while i + 1 < n and not (src[i] == "*" and src[i + 1] == "/"):
                if src[i] != "\n":
                    out[i] = " "
                i += 1
            if i + 1 < n:
                out[i] = out[i + 1] = " "
                i += 2
            continue
        if ch in ('"', "'", "`"):
            quote = ch
            out[i] = " "
            i += 1
            while i < n:
                if quote != "`" and src[i] == "\\":
                    out[i] = " "
                    if i + 1 < n:
                        out[i + 1] = " "
                    i += 2
                    continue
                if src[i] == quote:
                    out[i] = " "
                    i += 1
                    break
                if src[i] != "\n":
                    out[i] = " "
                i += 1
            continue
        i += 1
    return "".join(out)


def find_calls(masked: str, name: str):
    """在已脱敏源码里找 `name(` 的实参区间。返回 (start, end, args_text)。"""
    needle = name + "("
    pos = 0
    while True:
        idx = masked.find(needle, pos)
        if idx < 0:
            return
        open_idx = idx + len(name)
        depth = 0
        end = -1
        for j in range(open_idx, len(masked)):
            if masked[j] == "(":
                depth += 1
            elif masked[j] == ")":
                depth -= 1
                if depth == 0:
                    end = j
                    break
        if end < 0:
            return
        yield idx, end, masked[open_idx + 1 : end]
        pos = end + 1


def split_args(s: str) -> list[str]:
    out, cur, depth = [], [], 0
    for ch in s:
        if ch in "([{":
            depth += 1
        elif ch in ")]}":
            depth -= 1
        if ch == "," and depth == 0:
            out.append("".join(cur))
            cur = []
            continue
        cur.append(ch)
    if cur:
        out.append("".join(cur))
    return out


def main() -> int:
    violations: list[str] = []
    for path in sorted(ROOT.rglob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        if "/pkg/utils/response/" in str(path) or "/utils/response/" in str(path):
            continue
        src = path.read_text(encoding="utf-8")
        masked = strip_comments_and_strings(src)
        for name in ("ErrorFromDB", "ErrorWithBusinessCode"):
            for start, _end, args_text in find_calls(masked, name):
                args = split_args(args_text)
                if name == "ErrorFromDB":
                    payload = args[2:]
                else:
                    # ErrorWithBusinessCode：只查 CodeInternal 分支
                    if len(args) < 3 or "CodeInternal" not in args[1]:
                        continue
                    payload = args[2:]
                if any(re.search(r"\.Error\(\)", a) for a in payload):
                    line = src[:start].count("\n") + 1
                    violations.append(f"{path}:{line}: {name} 实参含 .Error()")
    if violations:
        print("❌ S-36 门禁：controller 把 .Error() 原文放进对外错误文案/data")
        for v in violations:
            print("  " + v)
        return 1
    print("✅ S-36 门禁通过：controller 未把 .Error() 放进 ErrorFromDB/CodeInternal")
    return 0


if __name__ == "__main__":
    sys.exit(main())
