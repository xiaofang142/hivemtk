#!/usr/bin/env python3
"""裸 go 语句门（N-2）：生产代码不得新增「不带 recover 的裸 go 语句」。

背景（2026-11-11 N-2 审计）：gin.Recovery 只保护请求 goroutine 本身，handler /
middleware / 同步 service 调用链里用裸 `go` 起的协程不在保护面内——Go 运行时任何
goroutine 未恢复的 panic 都会终止整个进程。仓内已有统一收口入口：

    utils.SafeGo(ctx, "name", func(ctx) { ... })              — 继承请求链
    utils.SafeGoDetached(ctx, "name", timeout, func(ctx){..}) — 与请求解耦+超时
    utils.SafeGoWithRetry(ctx, "name", b, fn)                 — 带退避重试
    async.SafeGo(fn)                                          — 无 ctx 场景

判据（以「裸 go 语句」为单位，不是 "go func(" 字面子串——`go ident(...)` 形态同样计）：
  1. `go func(...){BODY}(...)`：BODY 里（剥注释/字符串后）含 `recover()` ⇒ 已自带兜底，
     不计；否则计一处裸站点。
  2. `go ident(...)`（含 `go x.y.Method(...)`）：目标函数定义在**同一文件**且其体内含
     `recover()` ⇒ 不计；目标同文件但无 recover、或同文件找不到定义（跨文件目标）⇒ 计。
     跨文件目标若真已兜底（罕见），走基线登记豁免并写明理由。
  3. `_test.go` 一律忽略（测试进程 panic 本身就是用例失败，且吞 panic 会破坏排障）。
  4. `recover.go` 等自身就是 SafeGo 底层的文件同口径判（其 `go runRecover(...)` 的
     目标体里有 recover ⇒ 天然绿），不搞文件名豁免。

基线（scripts/bare-go.baseline，同仓内既有站点数棘轮门的口径）：
  格式：`<相对仓库根路径><TAB><该文件允许的裸站点数>`；空行与 `#` 注释行忽略。
  判定：现算 > 登记 ⇒ OVER 红（列出多出来的站点）；文件不在表却有站点 ⇒ NEW 红；
        登记 > 现算 ⇒ STALE 红（修掉就划掉，防基线养僵尸条目）；缺基线文件 ⇒ rc=2
        （区分「零命中」与「没扫」）。

已知盲区（口径边界，按「门禁口径盲区」规矩写明）：
  - 目标函数体调用**另一个**带 recover 的函数再由它兜底、或体内再经 SafeGo 起实际工作
    ⇒ 本门按「无 recover」计；若确属安全，基线登记豁免并写理由（人工背书）。
  - 字符串/注释剥除是简化 lexer（不处理转义里的全部边角）；复杂字面量导致的误判走基线，
    不放宽判据。
  - `go` 关键字出现在行首注释里不算（清洗后行首无 `go`）；行中注释与字符串内的 `go func(`
    不算——本门认「语句起始」。
  - 本门只锁「形状」：裸站点数不升。它不证明既有基线条目运行期安全——那些理由在基线表里。

执行入口：python3 scripts/check-bare-go.py [--repo .]；自测：bash scripts/check-bare-go.test.sh。
接 CI 时（check-ci-gate-paths.py 口径）：本脚本与 scripts/bare-go.baseline 必须在工作流
paths 触发面里登记，否则改判据不触发这道门。
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
DEFAULT_BASELINE = SCRIPT_DIR / "bare-go.baseline"

SKIP_DIRS = {"vendor", "node_modules", "__pycache__", ".git"}
GO_STATEMENT = re.compile(r"^[ \t]*go\b", re.MULTILINE)
RECOVER_CALL = re.compile(r"\brecover\s*\(\s*\)")
# `go ident(...)` / `go pkg.Type.Method(...)`：抓最后一个标识符段做目标名
GO_IDENT = re.compile(r"^[ \t]*go\s+(?:[A-Za-z_]\w*\.)*([A-Za-z_]\w*)\s*\(")
# 同文件目标函数定义：func name( / func (recv T) name( / func name[T any](
# 注意：泛型参数 `[T any]` 在 name 之后（Go 语法），不能放在 name 之前——否则
# 可选组会吃掉函数名前缀，把 NAME 匹配成最后一个字符。
FUNC_DEF = re.compile(
    r"^func\s+(?:\([^)]*\)\s*)?(?P<name>[A-Za-z_]\w*)\s*(?:\[[^\]]*\]\s*)?\(",
    re.MULTILINE,
)


def clean_go_source(text: str) -> str:
    """剥掉行注释/块注释/字符串字面量内容，保留换行与列结构（简化 lexer）。

    剥除后的文本用于：go 语句定位、大括号配平、recover() 检测——
    避免 `// go func(...)`、`"recover()"`、文档注释里的代码形状误判。
    """
    out = []
    i, n = 0, len(text)
    state = "code"  # code | line_comment | block_comment | string | raw_string | rune
    while i < n:
        c = text[i]
        nxt = text[i + 1] if i + 1 < n else ""
        if state == "code":
            if c == "/" and nxt == "/":
                state = "line_comment"
                out.append("  ")
                i += 2
                continue
            if c == "/" and nxt == "*":
                state = "block_comment"
                out.append("  ")
                i += 2
                continue
            if c == '"':
                state = "string"
                out.append(" ")
                i += 1
                continue
            if c == "`":
                state = "raw_string"
                out.append(" ")
                i += 1
                continue
            if c == "'":
                state = "rune"
                out.append(" ")
                i += 1
                continue
            out.append(c)
            i += 1
        elif state == "line_comment":
            if c == "\n":
                state = "code"
                out.append("\n")
            else:
                out.append(" ")
            i += 1
        elif state == "block_comment":
            if c == "*" and nxt == "/":
                state = "code"
                out.append("  ")
                i += 2
                continue
            out.append("\n" if c == "\n" else " ")
            i += 1
        elif state == "string":
            if c == "\\":
                out.append("  " if nxt == "\n" else "  ")
                i += 2
                continue
            if c == '"':
                state = "code"
                out.append('"')
                i += 1
                continue
            out.append("\n" if c == "\n" else " ")
            i += 1
        elif state == "raw_string":
            if c == "`":
                state = "code"
                out.append("`")
                i += 1
                continue
            out.append(c)  # raw string 保留换行结构
            i += 1
        else:  # rune
            if c == "\\":
                out.append("  ")
                i += 2
                continue
            if c == "'":
                state = "code"
                out.append("'")
                i += 1
                continue
            out.append("\n" if c == "\n" else " ")
            i += 1
    return "".join(out)


def match_braces(text: str, open_idx: int) -> int:
    """从 open_idx 的 '{' 起做配平，返回配平 '}' 的下标；失配返回 -1。"""
    depth = 0
    for i in range(open_idx, len(text)):
        c = text[i]
        if c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
            if depth == 0:
                return i
    return -1


def find_func_body_brace(text: str, open_paren_idx: int) -> int:
    """从函数签名参数表的 `(` 起找函数体 `{`。

    难点：签名里的类型可以带大括号——`func(string, interface{}, []byte)`、
    `interface{ M() }`、`struct{ A int }`、泛型约束 `[T interface{}]`。 naive
    `text.find('{')` 会把 `interface{}` 的 `{}` 当成函数体（recover.go 实测踩坑）。
    规则：跟踪 ()/[] 深度，深度为 0 的第一个 `{` 即候选；若它前邻 `interface`/
    `struct` 关键字，则整块跳过（类型字面量），继续找。
    """
    depth = 1  # open_paren_idx 指向 '(' 本身
    i = open_paren_idx + 1
    n = len(text)
    while i < n:
        c = text[i]
        if c in "([":
            depth += 1
        elif c in ")]":
            depth -= 1
        elif c == "{" and depth == 0:
            prev = text[max(0, i - 9) : i].rstrip()
            if prev.endswith(("interface", "struct")):
                close = match_braces(text, i)
                if close < 0:
                    return -1
                i = close + 1
                continue
            return i
        i += 1
    return -1


def func_decl_body(text: str, name: str) -> str | None:
    """在 cleaned 文本里找 `func ... name(` 的函数体（大括号内文本）。

    func 关键字必须在行首（缩进允许）——方法/函数定义都是顶层；方法值字面量
    `func() {` 不匹配 FUNC_DEF（name 锚定），不会误吞。
    """
    for m in FUNC_DEF.finditer(text):
        if m.group("name") != name:
            continue
        # m.end() 停在 name 后的第一个 `(`（参数表）
        brace = find_func_body_brace(text, m.end() - 1)
        if brace < 0:
            return None
        close = match_braces(text, brace)
        if close < 0:
            return None
        return text[brace : close + 1]
    return None


ANON_FUNC_START = re.compile(r"\bfunc\b\s*\(")


def anon_func_body(text: str, go_stmt_start: int) -> str | None:
    """从 `go` 语句起点找其匿名函数字面量的体。`go func(...){...}(...)` 或
    `go func(...){...}`；签名内的 interface{}/struct{}/泛型约束按
    find_func_body_brace 跳过。"""
    fm = ANON_FUNC_START.search(text, go_stmt_start)
    if not fm:
        return None
    paren = text.find("(", fm.start())
    if paren < 0:
        return None
    brace = find_func_body_brace(text, paren)
    if brace < 0:
        return None
    close = match_braces(text, brace)
    if close < 0:
        return None
    return text[brace : close + 1]


def scan_file(path: Path, repo_root: Path) -> list[tuple[int, str]]:
    """返回该文件的裸站点 [(行号, 行文本), ...]（行号为原文件 1 基）。"""
    try:
        raw = path.read_text(encoding="utf-8")
    except (UnicodeDecodeError, OSError):
        return []
    text = clean_go_source(raw)
    line_starts = [0]
    for i, ch in enumerate(text):
        if ch == "\n":
            line_starts.append(i + 1)

    def line_of(idx: int) -> int:
        lo, hi = 0, len(line_starts) - 1
        while lo < hi:
            mid = (lo + hi + 1) // 2
            if line_starts[mid] <= idx:
                lo = mid
            else:
                hi = mid - 1
        return lo + 1

    hits: list[tuple[int, str]] = []
    for m in GO_STATEMENT.finditer(text):
        # 语句起点：'go' 后必须跟空白/目标（^go\b 已保证词边界）
        stmt_idx = m.start()
        line_no = line_of(stmt_idx)
        line_text = raw[line_starts[line_no - 1] :].splitlines()[0] if line_no - 1 < len(line_starts) else ""
        rest = text[stmt_idx : stmt_idx + 200]
        protected = False
        if re.match(r"^[ \t]*go\s+func\b", rest):
            body = anon_func_body(text, stmt_idx)
            if body is not None and RECOVER_CALL.search(body):
                protected = True
        else:
            im = GO_IDENT.match(rest)
            if im:
                target = im.group(1)
                body = func_decl_body(text, target)
                if body is not None and RECOVER_CALL.search(body):
                    protected = True
        if not protected:
            hits.append((line_no, line_text.rstrip()))
    return hits


def iter_go_files(scan_root: Path):
    for path in sorted(scan_root.rglob("*.go")):
        rel_parts = path.relative_to(scan_root).parts
        if any(part in SKIP_DIRS for part in rel_parts[:-1]):
            continue
        if path.name.endswith("_test.go"):
            continue
        yield path


def parse_baseline(path: Path) -> dict[str, int]:
    if not path.is_file():
        raise FileNotFoundError(path)
    table: dict[str, int] = {}
    for ln, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        # 行内注释：`路径 数量 # 理由` —— 剥 `#` 及其后内容再解析
        hash_pos = line.find("#")
        if hash_pos >= 0:
            line = line[:hash_pos].strip()
        parts = line.split()
        if len(parts) != 2 or not parts[1].isdigit():
            raise ValueError(f"baseline 第 {ln} 行格式坏: {raw!r}（应为 `<路径> <数量>`）")
        if parts[0] in table:
            raise ValueError(f"baseline 第 {ln} 行重复文件: {parts[0]}")
        table[parts[0]] = int(parts[1])
    return table


def main() -> int:
    ap = argparse.ArgumentParser(description="裸 go 语句门（不带 recover 的 go 站点数棘轮）")
    ap.add_argument("--repo", default=".", help="仓库根（默认当前目录）")
    ap.add_argument("--baseline", default=str(DEFAULT_BASELINE), help="基线文件路径")
    args = ap.parse_args()

    repo_root = Path(args.repo).resolve()
    scan_root = repo_root / "user-server"
    if not scan_root.is_dir():
        print(f"rc=2 缺扫描根: {scan_root} 不存在（--repo 指向仓库根）")
        return 2

    current: dict[str, list[tuple[int, str]]] = {}
    for path in iter_go_files(scan_root):
        rel = str(path.relative_to(repo_root)).replace("\\", "/")
        hits = scan_file(path, repo_root)
        if hits:
            current[rel] = hits

    try:
        baseline = parse_baseline(Path(args.baseline))
    except FileNotFoundError:
        print("rc=2 缺基线文件：无法区分「零命中」与「没扫」")
        return 2
    except ValueError as e:
        print(f"rc=1 基线格式坏: {e}")
        return 1

    total = sum(len(v) for v in current.values())
    over, new, stale = [], [], []
    for rel, hits in sorted(current.items()):
        if rel not in baseline:
            new.append((rel, hits))
        elif len(hits) > baseline[rel]:
            over.append((rel, hits[baseline[rel] :]))
    for rel, allowed in sorted(baseline.items()):
        got = len(current.get(rel, []))
        if got < allowed:
            stale.append((rel, allowed, got))

    print(f"扫描 user-server：裸 go 站点现算 {total} 处 / 基线 {sum(baseline.values())} 处 / 文件 {len(baseline)} 个")
    rc = 0
    if new:
        rc = 1
        print(f"\n[NEW 红] 基线外出现裸 go 站点的文件 {len(new)} 个：")
        for rel, hits in new:
            for line_no, line_text in hits:
                print(f"  {rel}:{line_no}: {line_text.strip()}")
    if over:
        rc = 1
        print(f"\n[OVER 红] 超出基线登记数额的站点：")
        for rel, extra in over:
            for line_no, line_text in extra:
                print(f"  {rel}:{line_no}: {line_text.strip()}")
    if stale:
        rc = 1
        print(f"\n[STALE 红] 基线登记数额大于现算（修掉就划掉，不许养僵尸条目）：")
        for rel, allowed, got in stale:
            print(f"  {rel}: 登记 {allowed} / 现算 {got}")

    if rc == 0:
        print("✅ 裸 go 门绿：站点数未升，基线无漂移")
    return rc


if __name__ == "__main__":
    sys.exit(main())
