#!/usr/bin/env python3
"""裸 goroutine 收口门（CC-01 刀2 防回潮）：业务面 `go func(` 直 spawn 必须体内 recover 或带豁免标记。

背景（§3.15 CC-01 审计论证）：user 仓业务面裸 spawn 的 goroutine 一旦 panic，
gin.Recovery（router.go 请求协程护圈）护不到——业务 goroutine panic = 整进程崩溃。
刀1 已把高风险业务点统一收口到 utils.SafeGo / utils.SafeGoDetached（43+ 处在用惯例），
并在 utils/recover.go 的形状上补齐 recover；本门防回潮：新增裸 spawn 必须自证安全。

判据（对每个 spawn 站点独立判，嵌套 spawn 各判各的）：
  1. spawn 的**闭包体自身**含 `recover(` ⇒ 安全（defer recover 在 goroutine 栈上才真护得住；
     spawn 语句之后 30 行窗内的 recover 是审计口径的历史近似，本门用闭包体精确判定）；
  2. 否则须带豁免标记 `gospawn-ok: <理由>`（行级：同行或紧邻上一行；文件级：
     `gospawn-ok-file: <理由>`）。现有豁免=等待型 helper（wg.Wait→close(done)，无业务
     panic 面）+ os.Exit 自尽 helper + 纯内存负载模拟器；
  3. 两者都不满足 ⇒ 红。

排除项（均设计内，有论证）：
  - cmd/**：CLI 工具/进程入口，进程生命周期由 supervisor 接管（同 check-stdout-print 口径）；
  - _test.go：测试进程崩了测试本来就红；
  - internal/pkg/testutil/：测试基建；
  - user-server/scripts/、user-server/tests/：冒烟/压测脚本（非服务进程）；
  - internal/pkg/utils/async/：recover 实现层自身（本门要保护的底层，同
    user-server/scripts/check_naked_goroutine.sh 既有豁免口径）。

实现形态沿 check-error-leak.py：整文件做注释/字符串掩码（保长度保换行），
在掩码源上定位 spawn 与大括号配平，避免注释/字符串里的字面量误报。
--self-test 模式：对内嵌夹具跑真扫描器（TEETH）——无 recover 探针必须红、
体内 recover 与标记豁免必须绿、等待型无标记必须红，四枚控制含两向极性。
"""

from __future__ import annotations

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1] / "user-server"

# 排除目录（相对 user-server/）：cmd 入口 / 测试基建 / 冒烟压测脚本 / recover 实现层
EXCLUDE_PARTS = (
    ("cmd",),
    ("internal", "pkg", "testutil"),
    ("internal", "pkg", "utils", "async"),
)

EXCLUDE_TOPDIRS = {"scripts", "tests"}

SPAWN_RE = re.compile(r"(?m)^[ \t]*go[ \t]+func[ \t]*\(")
RECOVER_RE = re.compile(r"\brecover\s*\(")
MARKER_RE = re.compile(r"gospawn-ok(?:-file)?")


def mask_source(src: str) -> str:
    """注释与字符串/字符/raw string 内容替换成空格，保长度保换行（check-error-leak 同法）。"""
    out = list(src)
    i, n = 0, len(src)
    while i < n:
        ch = src[i]
        if ch == "/" and i + 1 < n and src[i + 1] == "/":
            while i < n and src[i] != "\n":
                if src[i] != "\n":
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
                if src[i] == "\n" and quote != "`":
                    # 未闭合的普通字符串到行尾：留给编译器报错，这里不外扩
                    break
                if src[i] == quote:
                    out[i] = " "
                    i += 1
                    break
                out[i] = " "
                i += 1
            continue
        i += 1
    return "".join(out)


def closure_body(masked: str, open_brace_idx: int) -> tuple[int, bool]:
    """从掩码源的 `{` 起做括号配平，返回 (结束下标, 是否配平成功)。"""
    depth = 0
    i, n = open_brace_idx, len(masked)
    while i < n:
        c = masked[i]
        if c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
            if depth == 0:
                return i, True
        i += 1
    return n - 1, False


def scan_source(src: str, rel: str) -> list[str]:
    """对单文件跑真扫描，返回违规行描述（--self-test 与主扫描共用）。"""
    masked = mask_source(src)
    lines = src.splitlines()
    violations = []

    has_file_marker = "gospawn-ok-file" in src

    for m in SPAWN_RE.finditer(masked):
        start = m.start()
        line_no = masked.count("\n", 0, start) + 1
        # 找本行的原始文本（用于标记豁免）
        line_text = lines[line_no - 1] if line_no - 1 < len(lines) else ""
        prev_text = lines[line_no - 2] if line_no >= 2 else ""
        if has_file_marker:
            continue
        if MARKER_RE.search(line_text) or MARKER_RE.search(prev_text):
            continue
        # 闭包体：spawn 匹配点之后的第一个 `{`
        brace = masked.find("{", m.end() - 1)
        if brace < 0:
            violations.append(f"{rel}:{line_no}: go func( 后找不到闭包体（形态异常，人工确认）")
            continue
        end, ok = closure_body(masked, brace)
        body = masked[brace:end]
        if not ok:
            violations.append(f"{rel}:{line_no}: 闭包体配平失败（可能触发掩码盲区，人工确认）")
            continue
        if RECOVER_RE.search(body):
            continue
        violations.append(
            f"{rel}:{line_no}: 裸 go func( 体内无 recover（panic=整进程崩溃）："
            f"改 utils.SafeGo / SafeGoDetached，或体内自带 defer recover，或加 gospawn-ok 标记"
        )
    return violations


def excluded(path: pathlib.Path) -> bool:
    rel_parts = path.relative_to(ROOT).parts
    if path.name.endswith("_test.go"):
        return True
    for parts in EXCLUDE_PARTS:
        if rel_parts[: len(parts)] == parts:
            return True
    if rel_parts[0] in EXCLUDE_TOPDIRS:
        return True
    return False


SELFTEST_FIXTURES = [
    # (名字, 源码, 期望违规数)
    (
        "探针：体内无 recover、无标记 ⇒ 必红",
        "package app\n\nfunc f() {\n\tgo func() {\n\t\tdoWork()\n\t}()\n}\n",
        1,
    ),
    (
        "体内自带 defer recover ⇒ 绿（真护得住：recover 在 goroutine 栈上）",
        "package app\n\nfunc f() {\n\tgo func() {\n\t\tdefer func() {\n\t\t\tif r := recover(); r != nil {\n\t\t\t}\n\t\t}()\n\t\tdoWork()\n\t}()\n}\n",
        0,
    ),
    (
        "行级标记豁免 ⇒ 绿（等待型 helper 口径）",
        "package app\n\nfunc f() {\n\t// gospawn-ok: 等待型 helper（wg.Wait→close done），无业务 panic 面\n\tgo func() {\n\t\twg.Wait()\n\t\tclose(done)\n\t}()\n}\n",
        0,
    ),
    (
        "等待型无标记 ⇒ 必红（极性对：证明标记是必须的，不是自动放行）",
        "package app\n\nfunc f() {\n\tgo func() {\n\t\twg.Wait()\n\t\tclose(done)\n\t}()\n}\n",
        1,
    ),
    (
        "recover 在 spawn 之后但体外 ⇒ 必红（30 行窗近似的历史盲区收口）",
        "package app\n\nfunc g() {\n\tgo func() {\n\t\tdoWork()\n\t}()\n\tdefer func() {\n\t\trecover()\n\t}()\n}\n",
        1,
    ),
    (
        "字符串/注释里的字面量 go func( ⇒ 绿（掩码不误报）",
        "package app\n\nvar s = \"go func() { fake }()\"\n\n// go func() { 注释里也不算 }\nfunc f() {\n\tgo func() {\n\t\tdefer recover()\n\t\tdoWork()\n\t}()\n}\n",
        0,
    ),
    (
        "嵌套 spawn 各判各的：外层有 recover 不救内层 ⇒ 红 1",
        "package app\n\nfunc f() {\n\tgo func() {\n\t\tdefer func() { recover() }()\n\t\tgo func() {\n\t\t\tdoWork()\n\t\t}()\n\t}()\n}\n",
        1,
    ),
]


def self_test() -> int:
    failures = 0
    for name, src, want in SELFTEST_FIXTURES:
        got = scan_source(src, "selftest.go")
        status = "PASS" if len(got) == want else "FAIL"
        if status == "FAIL":
            failures += 1
        print(f"  [{status}] {name}（期望违规 {want}，实得 {len(got)}）")
        for g in got:
            print(f"        {g}")
    if failures:
        print(f"❌ check-naked-goroutine --self-test：{failures} 枚控制未过")
        return 1
    print(f"✅ check-naked-goroutine --self-test：{len(SELFTEST_FIXTURES)} 枚控制全过（含两向极性）")
    return 0


def main() -> int:
    if "--self-test" in sys.argv:
        return self_test()

    if not ROOT.is_dir():
        print(f"❌ 找不到 user-server 根目录: {ROOT}", file=sys.stderr)
        return 2

    scanned = 0
    violations: list[str] = []
    for p in sorted(ROOT.rglob("*.go")):
        if excluded(p):
            continue
        scanned += 1
        try:
            src = p.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            violations.append(f"{p.relative_to(ROOT)}: 非 UTF-8 源文件，人工确认")
            continue
        violations.extend(scan_source(src, str(p.relative_to(ROOT))))

    if violations:
        print(
            f"❌ CC-01 门禁：{len(violations)} 处业务面裸 go func( 体内无 recover 且无豁免标记（扫描 {scanned} 文件）：",
            file=sys.stderr,
        )
        for v in violations:
            print("  " + v, file=sys.stderr)
        return 1
    print(
        f"check-naked-goroutine: 0 hit（扫描 {scanned} 文件；cmd/_test/测试基建/async 实现层按口径排除；"
        f"体内 recover 或 gospawn-ok 标记放行；等待型 helper/os.Exit/纯内存模拟为既有登记豁免）"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
