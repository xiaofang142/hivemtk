#!/usr/bin/env python3
"""异步体裸读全局门：协程体里**直接引用包级变量**，而该变量在测试里会被重新赋值 ⇒ DATA RACE。

背景（2026-09-22 · 第四十二轮 R27）：master 的 `Run unit tests (with -race)` 在
`924168ce` 之前的 `7418e489` 上抓到一条真红（CI job 106701603664）：

    Write at 0x000004023b88 by goroutine 30599:  TestN17_FeishuMediaFullChainBackfillsHubRow()
        internal/service/webhook_batchf4_msgtype_test.go:522
    Previous read at 0x000004023b88 by goroutine 30559:  (*WebhookService).persistFeishuMediaAsync.func1()
        internal/service/webhook_channel_feishu.go:427  ← 在 utils.SafeGo 的体内
    --- FAIL: TestN17_FeishuMediaFullChainBackfillsHubRow  testing.go:1712: race detected during execution of test

机理不是"两个用例同时跑"，而是**前一个用例留下的协程还没被调度到读那一行**：飞书那条异步体进
`SafeGo` 后先要 `feishuRepo.GetByID` 走一趟库，才读 `feishuTenantTokenFn`。CI 机器上 GOMAXPROCS 少、
负载高，这一趟的窗口足够下一条用例开始并写全局；本机 `-count=30` 反而不一定撞得上。⇒ 这类红**不能靠
"本地复现失败"判无**，判据是"这条形状还在多少处"，而形状是静态可数的。

修法（与本仓 `4176e599` 的 reach 族同款，不另创口径）：**进协程前把要用的包级值快照成本地变量**，
体内只读本地。生产语义不变（这些 seam 运行期从不改），变的是"用例 A 的异步体不再可能读到用例 B 装上去的替身"
——那既是 DATA RACE，也是跨用例串味。

判据（三条，任一不成立即 rc=1）：
  1. 某 package 的**确证站点**数超过 `scripts/async-global-read.baseline` 登记的数额，或出现基线里没有的
     新文件 ⇒ 新增红；
  2. 基线里登记的文件其现算数额**变小或归零** ⇒ STALE 红（逼着把修掉的划掉，防基线烂掉）；
  3. 基线格式坏（列数不对／非数字／重复文件名／缺基线文件）⇒ 红；缺基线文件退 **rc=2**（同 nil-deref 门口径：
     缺基线时"零命中"和"没扫"分不开）。

口径边界（按「门禁口径盲区」的规矩写明，别把"没数到"印成"没问题"）：
  - **只锁"测试会改写"的那批全局**：包级 `var` 的名字在同 package 的 `*_test.go` 里出现在 `=` 左边
    （元组赋值左右两侧的名字都算）。`:=` **不算**——那是造局部遮蔽，不构成"测试会并发写这个全局"。
    运行期无人写、测试也不写的常量式全局（自带锁的 `versionCache` 之类）不在本门口径里——那是另一类判据
    （要看它的守卫是否真守），混进来基线就数不出同一个缺陷；
  - **只数体内直读**：体内调用的那个函数**它自己**再读测试可改写的全局，属本门的**已知盲区**。
    建门时只点了默认实现层的四处（`FetchQQAttachment` 读 `qqAttachmentURLGuard`、`FetchTelegramMedia` 读
    `tgAPIBaseOverride`、`FetchDingTalkRobotMedia` 读 `dingtalkOpenAPIBase`、抖音取址 helper 读
    `dyAPIBaseOverride`），**这个"四处"是抽查不是枚举** —— 本轮收口后另做一次上界枚举（从本包 71 处协程区域
    沿调用图走 ≤5 层，键为"可达函数"）：40 个测试可写全局里 **16** 个可达，其中多数只经各家 seam 的
    **默认实现**（用例一装替身就走不到），另有经 `replayDelayedOutbound` 与 cron `RunOnce` 循环的链。
    ⇒ 本门绿**不等于**这类竞态已全闭；传递层的收口口径是"锁住被读的那个全局本身"（读写各收进一对
    accessor、由 `sync.RWMutex` 守，与本仓 `eada12ba` 的 `pkg/db` 三扇门同款），**不是**改这些 Fetch 的签名
    把值当参数传进去 —— `FetchTelegramMedia` 的三参签名被
    `webhook_batchf4_m01_telegram_test.go:770` 一行 `var f func(context.Context, string, string) (...) = FetchTelegramMedia`
    钉成了契约，不擅动别人的契约。进度见计划文档 `## R27` / `## R28` 两节；
  - **协程体识别**＝以 `utils.SafeGo(`/`utils.SafeGoDetached(`/`go func(` 开头的行，按大括号配平吃到
    该语法块结束；`defer func()`／`time.AfterFunc` 里再启协程的嵌套形状按同法处理，但**不外扩到整函数**；
  - 站点＝协程体内出现的"该包可测改写全局"的名字（词边界匹配），**行首是注释的行不算**；
  - 同名跨包不算：键是 `package 目录 + 变量名`，别的包里同名变量互不影响；
  - 局部变量遮蔽（`x := pkgGlobal` 之后体内用 `x`）判绿——这正是修法的形状，不需要再识别；
  - 只扫 `user-server/` 下含 `*.go` 的目录，跳过 `vendor`／`node_modules`／以 `.` 开头的目录。

⇒ 基线上的数字是**已确证的候选清单**，本门的承诺只有一句：**"这种形状新增一处就红"**。

执行入口：本地 `python3 scripts/check-async-global-read.py`；接 CI 时**触发 paths 必须含本脚本与基线文件**
（否则改判据不触发这道门，同 platform `docs-link-check.yml` 的教训）。
"""

from __future__ import annotations

import re
import sys
from collections import defaultdict
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
SCAN_ROOT = REPO_ROOT / "user-server"
BASELINE = REPO_ROOT / "scripts" / "async-global-read.baseline"

SKIP_DIRS = {"vendor", "node_modules", "__pycache__"}

# 包级 var 声明块：`var (` 到配平的 `)`，或单行 `var x = ...`
VAR_BLOCK_START = re.compile(r"^var\s*\(\s*$")
VAR_SINGLE = re.compile(r"^var\s+(?P<names>[A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\b")
VAR_BLOCK_NAME = re.compile(r"^\s*(?P<names>[A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\s+(?:\w[\w\[\]\*.,<>-]*\s*)?=")

# 测试里对包级变量的改写：只认 `=`，**不认 `:=`**（`x := seam` 是造局部遮蔽，不构成"测试会写这个全局"）。
# 元组赋值 `a, b = pa, pb`（还原替身就是这一形）里左右两侧的名字都算左值：右侧重名时按"它也在被赋值"处理，
# 误收的代价只是多看一眼，漏收的代价是站点数出来少两个（首轮就是这么漏掉 wxMediaFetchFn/dyMediaStoreFn 的）。
# 先把"长得像 `=` 但不是赋值"的记号（比较、短赋值、复合赋值）整体抹成占位符，
# 剩下的第一个孤立 `=` 才是改写。占位符里不许含字母下划线，免得被当成标识符收进左值。
NON_ASSIGN = re.compile(r"==|!=|<=|>=|:=|\+=|-=|\*=|/=|%=|&=|\|=|\^=|<<=|>>=|<-|->")


def test_assigned_names(src: list[str]) -> set[str]:
    """挑出"测试里被改写过"的名字：找孤立 `=`，取它左边的全部标识符。

    `a, b = pa, pb`（还原替身就是这一形）左侧两个名字都算；`a := b` 不算（那是新建局部量，
    和包级变量并发改的不是同一块内存）。左边的标识符会连带收下 `t.Cleanup(func() { ... })`
    这类前缀里的词，交给"与包级 var 求交集"那一步过滤。
    """
    names: set[str] = set()
    for line in src:
        if COMMENT.match(line):
            continue
        probe = re.sub(r'"(?:\\.|[^"\\])*"', '""', line)
        probe = re.sub(r"`[^`]*`", "``", probe)
        probe = NON_ASSIGN.sub(" @ ", probe)
        cut = probe.find("=")
        if cut < 0:
            continue
        names.update(NAME.findall(probe[:cut]))
    return names

GO_START = re.compile(r"\b(?:utils\.)?SafeGo(?:Detached)?\s*\(|\bgo\s+func\s*\(")

COMMENT = re.compile(r"^\s*(?://|/\*)")
NAME = re.compile(r"[A-Za-z_]\w*")


def go_files(pkg: Path) -> list[Path]:
    return sorted(p for p in pkg.glob("*.go") if p.suffix == ".go")


def package_dirs() -> list[Path]:
    dirs: set[Path] = set()
    for path in SCAN_ROOT.rglob("*.go"):
        if SKIP_DIRS & set(path.parts) or any(part.startswith(".") for part in path.relative_to(SCAN_ROOT).parts):
            continue
        dirs.add(path.parent)
    return sorted(dirs)


def brace_delta(line: str) -> int:
    """粗略统计一行的花括号净增减：字符串字面量里的括号不参与。"""
    stripped = re.sub(r'"(?:\\.|[^"\\])*"', '""', line)
    stripped = re.sub(r"`[^`]*`", "``", stripped) if stripped.count("`") % 2 == 0 else stripped
    return stripped.count("{") - stripped.count("}")


def package_level_vars(src: list[str]) -> set[str]:
    names: set[str] = set()
    in_block = False
    for line in src:
        if in_block:
            if re.match(r"^\)", line):
                in_block = False
                continue
            m = VAR_BLOCK_NAME.match(line)
            if m:
                names.update(n.strip() for n in m.group("names").split(","))
            continue
        if VAR_BLOCK_START.match(line):
            in_block = True
            continue
        m = VAR_SINGLE.match(line)
        if m:
            if re.match(r"^var\s+func\b", line):
                continue
            names.update(n.strip() for n in m.group("names").split(","))
    return names


def async_bodies(src: list[str]) -> list[tuple[int, list[str]]]:
    """返回 [(起始行号 1-based, 体内容行)]。起始行即 `SafeGo(`／`go func(` 所在行。"""
    bodies: list[tuple[int, list[str]]] = []
    i = 0
    n = len(src)
    while i < n:
        line = src[i]
        if COMMENT.match(line) or not GO_START.search(line):
            i += 1
            continue
        depth = 0
        started = False
        body: list[str] = []
        j = i
        while j < n:
            cur = src[j]
            body.append(cur)
            d = brace_delta(cur)
            depth += d
            if d > 0:
                started = True
            elif not started and j > i and "{" in cur:
                started = True
            if started and depth <= 0:
                break
            j += 1
        bodies.append((i + 1, body))
        i = j + 1
    return bodies


def main() -> int:
    if not SCAN_ROOT.is_dir():
        print(f"rc=2 找不到扫描根：{SCAN_ROOT}", file=sys.stderr)
        return 2
    if not BASELINE.is_file():
        print(f"rc=2 缺基线文件 {BASELINE.relative_to(REPO_ROOT)}（缺基线时零命中与没扫分不开）", file=sys.stderr)
        return 2

    registered: dict[str, int] = {}
    for lineno, raw in enumerate(BASELINE.read_text(encoding="utf-8").splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) != 2:
            parts = line.rsplit(None, 1)
        if len(parts) != 2:
            print(f"基线第 {lineno} 行格式坏（要 `<相对路径>\\t<数额>`）：{raw}", file=sys.stderr)
            return 1
        rel, count = parts[0].strip(), parts[1].strip()
        if not count.isdigit():
            print(f"基线第 {lineno} 行数额不是数字：{raw}", file=sys.stderr)
            return 1
        if rel in registered:
            print(f"基线重复登记文件：{rel}", file=sys.stderr)
            return 1
        registered[rel] = int(count)

    found: dict[str, list[tuple[int, str]]] = defaultdict(list)
    packages_scanned = 0
    for pkg in package_dirs():
        files = go_files(pkg)
        if not files:
            continue
        packages_scanned += 1
        test_assigned: set[str] = set()
        non_test: list[Path] = []
        for f in files:
            src = f.read_text(encoding="utf-8", errors="replace").splitlines()
            if f.name.endswith("_test.go"):
                test_assigned |= test_assigned_names(src)
            else:
                non_test.append(f)
        if not test_assigned or not non_test:
            continue
        for f in non_test:
            src = f.read_text(encoding="utf-8", errors="replace").splitlines()
            pkgvars = package_level_vars(src) & test_assigned
            if not pkgvars:
                continue
            matchers = {name: re.compile(r"\b" + re.escape(name) + r"\b") for name in pkgvars}
            for start_line, body in async_bodies(src):
                for offset, line in enumerate(body):
                    if COMMENT.match(line):
                        continue
                    for name, rx in matchers.items():
                        if rx.search(line):
                            found[str(f.relative_to(REPO_ROOT))].append((start_line + offset, name))

    rel_files = sorted(found)
    current = {rel: len(found[rel]) for rel in rel_files}
    rc = 0
    new_files = [rel for rel in rel_files if rel not in registered]
    grew = [rel for rel in rel_files if rel in registered and current[rel] > registered[rel]]
    shrunk = [rel for rel in registered if rel not in found or current.get(rel, 0) < registered[rel]]

    if new_files or grew:
        rc = 1
        print("异步体裸读包级可变全局（进协程前先快照成本地值）：")
        print(f"  项目根 {REPO_ROOT}（扫描根 {SCAN_ROOT}）")
        for rel in new_files:
            for line, name in found[rel]:
                print(f"  {rel}:{line} 在协程体里引用了测试会改写的全量 {name}")
        for rel in grew:
            print(f"  {rel} 站点数 {registered[rel]} → {current[rel]}（超过基线）")
    for rel in shrunk:
        rc = 1
        print(f"  STALE：{rel} 基线登记 {registered[rel]}，现算 {current.get(rel, 0)} ⇒ 把修掉的从基线里划掉")

    if rc == 0:
        total = sum(current.values())
        print(f"OK 异步体裸读全局门：项目根 {REPO_ROOT}，扫 {packages_scanned} 个 package，"
              f"站点 {total}（= 基线合计 {sum(registered.values())}）")
    return rc


if __name__ == "__main__":
    sys.exit(main())
