#!/usr/bin/env python3
"""临时精确抽取器：列出 user-server 下所有「包级 const/var 命名常量」候选。

用途：区分真·配置常量（应迁移 config_params）与函数内局部变量/字段赋值（噪声）。
仅打印，不修改源码。作为 hardcode_sweep.py 的 precise 增强原型。
"""
from __future__ import annotations

import re
from pathlib import Path

SERVER = Path(__file__).resolve().parent.parent / "user-server"

NUM_ASSIGN = re.compile(r'\b([A-Z][A-Za-z0-9_]*)\s*=\s*(.+)$')
LIT_OK = re.compile(r'^(\d[\d_.e+]*|true|false|time\.[A-Za-z]+|'
                    r'\d+\s*<<\s*\d+|\d+(?:\.\d+)?\s*\*\s*time\.[A-Za-z]+)$')

SKIP = ("_test.go", "_seeds.go", "_seed.go")


def strip_quotes(code: str) -> str:
    code = re.sub(r'"(?:[^"\\]|\\.)*"', '""', code)
    code = re.sub(r'`[^`]*`', '""', code)
    return code


def main() -> None:
    results: dict[str, list[tuple[int, str, str, str]]] = {}
    for p in sorted(SERVER.rglob("*.go")):
        if p.name.endswith(SKIP):
            continue
        sp = str(p)
        if "/vendor/" in sp or "/migrations/" in sp:
            continue
        text = p.read_text(encoding="utf-8", errors="replace")
        lines = text.splitlines()
        depth = 0
        in_func = False
        in_const_block = False
        in_var_block = False
        for i, raw in enumerate(lines, 1):
            code = re.sub(r"//.*$", "", raw)
            s = code.lstrip()
            # func detection: package-level 'func ' starts a body
            if not in_func and depth == 0 and s.startswith("func "):
                in_func = True

            cleaned = strip_quotes(code)
            opens = cleaned.count("{")
            closes = cleaned.count("}")
            depth += opens - closes
            if depth < 0:
                depth = 0

            # block tracking at package level
            if depth == 0:
                if re.match(r'const\s*\(', s):
                    in_const_block = True
                    in_var_block = False
                elif re.match(r'var\s*\(', s):
                    in_var_block = True
                    in_const_block = False
                elif s.startswith("const ") and not s.startswith("const ("):
                    in_const_block = False
                    m = NUM_ASSIGN.match(s[len("const "):])
                    if m:
                        results.setdefault(sp[len(str(SERVER)) + 1:], []).append(
                            (i, m.group(1), m.group(2).strip(), "const"))
                elif s.startswith("var ") and not s.startswith("var ("):
                    in_var_block = False
                    m = NUM_ASSIGN.match(s[len("var "):])
                    if m:
                        results.setdefault(sp[len(str(SERVER)) + 1:], []).append(
                            (i, m.group(1), m.group(2).strip(), "var"))
                elif s == ")" and (in_const_block or in_var_block):
                    in_const_block = False
                    in_var_block = False
            else:
                # inside a const/var block at package level (depth may be 0 for ')' only when block closes)
                if (in_const_block or in_var_block) and s and s[0].isupper():
                    m = NUM_ASSIGN.match(s)
                    if m:
                        kind = "const" if in_const_block else "var"
                        if LIT_OK.match(m.group(2).strip()):
                            results.setdefault(sp[len(str(SERVER)) + 1:], []).append(
                                (i, m.group(1), m.group(2).strip(), kind))

            if depth == 0:
                in_func = False

    total = sum(len(v) for v in results.values())
    print(f"package-level const/var literals: {total}")
    for f in sorted(results):
        for i, name, val, kind in results[f]:
            print(f"{f}:{i}\t{kind}\t{name} = {val}")


if __name__ == "__main__":
    main()