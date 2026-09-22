#!/usr/bin/env python3
"""seam 全局 accessor 门：注册表里登记的每一个「被异步链路经默认实现读到的测试可写全局」，
它的每一次读和每一次写都必须在自己的那对 accessor 里、在那把锁底下。

背景（2026-09-22 · 第四十三轮 R28）：R27（`521e4f80`）把「协程体裸读包级注入点」这一类用进协程前快照收口了，
但快照挡不住**第二跳** —— 协程体调的是 seam 的默认实现（`FetchTelegramMedia`／`FetchQQAttachment`／
`sendOutbound`／cron 的 `RunOnce` …），默认实现函数体里那一句读的还是全局地址。本包的静态门当时就把它
写成「已知盲区」（`check-async-global-read.py` 文档串的第二条口径），并在 `## R28` 里做了上界枚举：
16 个测试可写全局可达。

修法沿用本仓 `eada12ba`（`internal/pkg/db`）那一套，不另创口径：全局旁边声明一把
`sync.RWMutex`，读写各收进一扇 accessor（`loadXxx()` / `storeXxx(v)`），锁内只做取值/赋值 ——
**不在持锁期间调用取到的可换函数值**（`fn := loadXxx(); fn(args)`）。竞态要断开，必须**全部**访问都
被同一把锁 synchronize，所以测试侧的改写点也一并换成 setter（读测试里那几处不算竞争，但一律走 accessor
才能让「accessor 之外 0 处访问」成为可静态核对的硬不变量）。

为什么还要一道静态门（`-race` 腿已经会红了）：腿是行为证据，它要求两条协程真的并发撞上；而「谁在锁外
摸了一把全局」是纯形状问题，一次 `grep` 级核对就够，且能在**新增**消费点的当下就红，不必等到那一条用例
恰好与某条残留协程同场。两道门钉的是同一件事的两面：本门管「有没有走门」，腿管「门里真上锁没上锁」。

判据（任一不成立即 rc=1）：
  1. 注册表里 `文件`／`getter`／`setter` 任一找不到 ⇒ 红（注册表指向的东西不在树里了，划它或补它）；
     `setter` 可写成 `文件:函数名`，指到全局本家文件之外的那个文件（见下面第二条口径边界）；
  2. getter 体内没有对该锁的 `Lock()`/`RLock()` 与配套 `Unlock`/`RUnlock`，或 setter 体内没有
     `Lock()`+`Unlock()` ⇒ 红（两扇门都得真上锁；摘掉任一条腿的锁，本门与那条 `-race` 腿同时红）；
  3. 全局名出现在「声明它的 var 块 ∪ 它自己那两扇 accessor（各自所在文件）」**之外**的任意 .go 文件里 ⇒ 红
     （含跨包直读：注册表里 `IntentEnabled` 是导出量，别的包写 `service.IntentEnabled` 就在这里红）。
  另有格式格：列数不对、重复登记同一全局、数额列含空白、`setter` 的 `文件:函数名` 形状坏 ⇒ 红；
  **缺注册表退 rc=2**（同 async-global-read 门口径：缺基线时"零命中"和"没扫"分不开）。

口径边界（按「门禁口径盲区」的规矩写明）：
  - 写侧只被测试调用的那 14 扇 setter 定义在 `internal/service/seam_guard_setters_test.go`，不在各渠道的
    生产文件里：`user-server/.golangci.yml` 是 `run.tests: false` 且启用 `unused`，于是「只被 `_test.go`
    调用的函数」在生产面上算死代码 —— `734118d9` 把 setter 留在生产文件里时，CI 的 golangci-lint 步当场
    报 14 条 unused（父笔同一作业 0 条红，本地 `make audit` 不含 golangci-lint 所以推上去前没人看见）。
    **锁与全局仍在生产文件里**：判据是「每一次读和每一次写都被同一把锁 synchronize」，与定义在哪个文件无关。
    ⇒ 只被测试调用的写侧要登记成 `seam_guard_setters_test.go:storeXxx`；写侧同时被产码调用（`IntentEnabled`、
    `bridgeChannelOnlineProbe` 那两扇）才留在本家文件。
  - 判的是**标识符出现位置**，不看控制流：注释与字符串字面量里的名字不算访问点（先把串整体抹掉，再切行尾注释）；
  - 只扫 `user-server/` 下的 `*.go`，跳过 `vendor`／`node_modules`／以 `.` 开头的目录；`_test.go` 一并扫，
    测试直接摸全局同样红（装桩必须走 setter）；
  - 一把锁守多个全局是允许的（`tgSeamMu` 同时守 `tgAPIBaseOverride` 与 `tgMaxMediaBytes`）：判据是
    「这两个全局各自的两扇门都 took 这把锁」，不涉及锁序；
  - **注册表是穷举口径**：新增一个同类全局必须显式加行，本门不会自动发现"又一个测试可写全局被异步链读到"
    —— 那要重新跑一次上界枚举（脚本口径见计划文档 `## R28`）。⇒ 本门绿只承诺"登记过的这些没被绕过去"，
    不承诺"这类竞态已全闭"。

执行入口：本地 `make audit`；单独跑 `python3 scripts/check-seam-guard.py`。
牙齿证据：`python3 scripts/mut_seam_guard_r28.py`（摘锁格 ⇒ 本门与对应 `-race` 腿一起红；锁外直读格 ⇒ 本门红）。
"""

from __future__ import annotations

import re
import sys
from collections import defaultdict
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
SCAN_ROOT = REPO_ROOT / "user-server"
REGISTRY = REPO_ROOT / "scripts" / "seam-guard.registry"

SKIP_DIRS = {"vendor", "node_modules", "__pycache__"}

COMMENT = re.compile(r"^\s*(?://|/\*)")
NAME = re.compile(r"[A-Za-z_]\w*")
VAR_BLOCK_START = re.compile(r"^var\s*\(\s*$")
VAR_SINGLE = re.compile(r"^var\s+(?P<names>[A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\b")
VAR_BLOCK_NAME = re.compile(r"^\s*(?P<names>[A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\s+(?:\w[\w\[\]\*.,<>-]*\s*)?=")
# 只带类型、不带初值的块内声明（`dyAPIBaseOverride string`／`x func(a) bool`）：初值恒为零值，
# 但同样是"声明处"，漏认它就把声明本身判成了越界访问。整行不许出现 `=`，否则交给上一条。
VAR_BLOCK_TYPED = re.compile(r"^\s*(?P<names>[A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\s+\S[^=]*$")
FUNC_START = re.compile(r"^func\s+(?:\([^)]*\)\s*)?(?P<name>[A-Za-z_]\w*)\s*\(")


def strip_noise(line: str) -> str:
    """抹掉字符串字面量、再切掉行尾注释：留在串里/注释里的名字不是访问点。"""
    out = re.sub(r'"(?:\\.|[^"\\])*"', '""', line)
    if out.count("`") % 2 == 0:
        out = re.sub(r"`[^`]*`", "``", out)
    # 只在"空白后的 //"或行首处切注释：URL 里的 `://` 前面不是空白，不会被误切。
    out = re.split(r"(?<!\S)//|\s//", out, maxsplit=1)[0]
    return out


def brace_delta(line: str) -> int:
    stripped = re.sub(r'"(?:\\.|[^"\\])*"', '""', line)
    if stripped.count("`") % 2 == 0:
        stripped = re.sub(r"`[^`]*`", "``", stripped)
    return stripped.count("{") - stripped.count("}")


def func_ranges(src: list[str], name: str) -> list[tuple[int, int]]:
    """`func <name>(` 的行区间（1-based，含收尾花括号行）。同名方法（不同 receiver）全部收进来。"""
    out: list[tuple[int, int]] = []
    i = 0
    while i < len(src):
        m = FUNC_START.match(src[i])
        if not m or m.group("name") != name:
            i += 1
            continue
        depth, started, j = 0, False, i
        while j < len(src):
            d = brace_delta(strip_noise(src[j]))
            depth += d
            if d > 0:
                started = True
            elif not started and j > i and "{" in strip_noise(src[j]):
                started = True
            if started and depth <= 0:
                break
            j += 1
        out.append((i + 1, j + 1))
        i = j + 1
    return out


def decl_ranges(src: list[str], name: str) -> list[tuple[int, int]]:
    """声明该全局的 `var (...)` 块 / 单行 `var` 的行区间。"""
    out: list[tuple[int, int]] = []
    i = 0
    while i < len(src):
        line = src[i]
        if VAR_BLOCK_START.match(line):
            j, specs = i + 1, []
            while j < len(src) and not re.match(r"^\)", src[j]):
                specs.append(src[j])
                j += 1
            for spec in specs:
                m = VAR_BLOCK_NAME.match(spec) or VAR_BLOCK_TYPED.match(spec)
                if m and name in [x.strip() for x in m.group("names").split(",")]:
                    out.append((i + 1, j + 1))
                    break
            i = j + 1
            continue
        m = VAR_SINGLE.match(line)
        if m and name in [x.strip() for x in m.group("names").split(",")]:
            out.append((i + 1, i + 1))
        i += 1
    return out


def inside(lineno: int, ranges: list[tuple[int, int]]) -> bool:
    return any(a <= lineno <= b for a, b in ranges)


def go_files() -> list[Path]:
    out = []
    for path in SCAN_ROOT.rglob("*.go"):
        rel = path.relative_to(SCAN_ROOT)
        if SKIP_DIRS & set(path.parts) or any(part.startswith(".") for part in rel.parts):
            continue
        out.append(path)
    return sorted(out)


def load_registry() -> list[dict[str, str]] | int:
    if not REGISTRY.is_file():
        print(f"rc=2 缺注册表 {REGISTRY.relative_to(REPO_ROOT)}（缺注册表时零命中与没扫分不开）", file=sys.stderr)
        return 2
    entries: list[dict[str, str]] = []
    seen: set[str] = set()
    for lineno, raw in enumerate(REGISTRY.read_text(encoding="utf-8").splitlines(), 1):
        line = raw.rstrip()
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) != 5 or any(not p.strip() for p in parts):
            print(f"注册表第 {lineno} 行格式坏（要 5 列 TAB 分隔：全局/文件/锁/getter/setter）：{raw}", file=sys.stderr)
            return 1
        g, rel, lock, getter, setter = (p.strip() for p in parts)
        if g in seen:
            print(f"注册表重复登记全局：{g}", file=sys.stderr)
            return 1
        seen.add(g)
        # setter 可写成 `文件:函数名`：只被测试调用的写侧必须待在 `_test.go` 里（见文件头第三条口径）。
        setfile, setname = (rel, setter)
        if ":" in setter:
            setfile, setname = (s.strip() for s in setter.split(":", 1))
            if not setfile or not setname or ":" in setname:
                print(f"注册表第 {lineno} 行 setter 的 `文件:函数名` 形状坏：{setter}", file=sys.stderr)
                return 1
        entries.append({"global": g, "file": rel, "lock": lock, "getter": getter,
                        "setter": setname, "setter_file": setfile})
    if not entries:
        print("rc=2 注册表是空的（一道常绿门比没有门更误导）", file=sys.stderr)
        return 2
    return entries


def main() -> int:
    if not SCAN_ROOT.is_dir():
        print(f"rc=2 找不到扫描根：{SCAN_ROOT}", file=sys.stderr)
        return 2
    entries = load_registry()
    if isinstance(entries, int):
        return entries

    sources: dict[Path, list[str]] = {}
    files = go_files()
    for f in files:
        sources[f] = f.read_text(encoding="utf-8", errors="replace").splitlines()

    # 一遍扫完所有注册全局的出现位置：17 个名字合成一条交替式，每个文件只过一遍
    # （逐个全局各扫一遍是 17 趟，实测把门拖到 40s，电池里 20 格就是十三分钟）。
    combined = re.compile(r"(?<!\w)(" + "|".join(map(re.escape, [e["global"] for e in entries])) + r")(?!\w)")
    mentions: dict[str, list[tuple[Path, int]]] = defaultdict(list)
    for f in files:
        for i, line in enumerate(sources[f], 1):
            if COMMENT.match(line):
                continue
            for m in combined.finditer(strip_noise(line)):
                mentions[m.group(1)].append((f, i))

    rc = 0
    for e in entries:
        g, lock = e["global"], e["lock"]
        home = SCAN_ROOT / e["file"]
        sethome = SCAN_ROOT / e["setter_file"]
        if home not in sources:
            rc = 1
            print(f"SEAM-GUARD：{g} 登记的文件不在树里：{e['file']}")
            continue
        if sethome not in sources:
            rc = 1
            print(f"SEAM-GUARD：{g} 登记的 setter 文件不在树里：{e['setter_file']}")
            continue
        src = sources[home]
        getter = func_ranges(src, e["getter"])
        setter = func_ranges(sources[sethome], e["setter"])
        if not getter or not setter:
            rc = 1
            print(f"SEAM-GUARD：{g} 的 accessor 找不到（getter {e['getter']} @ {e['file']}"
                  f" / setter {e['setter']} @ {e['setter_file']}）")
            continue
        # 上锁判据：两扇门各自体内必须出现「对该锁的 Lock/RLock」与配套 Unlock。
        for label, rng, kinds, body_src in (("getter", getter, ("Lock", "RLock"), src),
                                            ("setter", setter, ("Lock",), sources[sethome])):
            want = " 或 ".join(f"{lock}.{k}()" for k in kinds)
            body = "\n".join(body_src[rng[0][0] - 1:rng[-1][1]])
            if not any(re.search(rf"\b{re.escape(lock)}\.{k}\(\)", body) for k in kinds):
                rc = 1
                print(f"SEAM-GUARD：{g} 的 {label} {e['getter' if label == 'getter' else 'setter']} 没对 {lock} 上锁"
                      f"（要 {want}）@ {e['file' if label == 'getter' else 'setter_file']}:{rng[0][0]}")
            elif not re.search(rf"\b{re.escape(lock)}\.(Unlock|RUnlock)\(\)", body):
                rc = 1
                print(f"SEAM-GUARD：{g} 的 {label} {e['setter']} 上了 {lock} 却没有配套 Unlock"
                      f" @ {e['file' if label == 'getter' else 'setter_file']}:{rng[0][0]}")

        decl = decl_ranges(src, g)
        if not decl:
            rc = 1
            print(f"SEAM-GUARD：{g} 在登记文件 {e['file']} 里找不到 var 声明处")
        allowed: dict[Path, list[tuple[int, int]]] = {home: decl + getter}
        allowed[sethome] = allowed.get(sethome, []) + setter
        hits = [f"{f.relative_to(REPO_ROOT)}:{i}" for f, i in mentions.get(g, [])
                if not inside(i, allowed.get(f, []))]
        if hits:
            rc = 1
            print(f"SEAM-GUARD：{g} 在 accessor 之外被直接引用 {len(hits)} 处（读写只走 "
                  f"{e['getter']}()／{e['setter']}()）：")
            for h in hits[:12]:
                print(f"    {h}")
            if len(hits) > 12:
                print(f"    …另有 {len(hits) - 12} 处")

    if rc == 0:
        print(f"OK seam 全局 accessor 门：项目根 {REPO_ROOT}，登记 {len(entries)} 个全局，"
              f"扫 {len(files)} 个 .go 文件 ⇒ accessor 之外 0 处访问")
        print(f"    注册表 {REGISTRY.relative_to(REPO_ROOT)}；摘锁/绕门的牙齿证据见 scripts/mut_seam_guard_r28.py")
    else:
        print(f"项目根 {REPO_ROOT}（扫描根 {SCAN_ROOT}）")
    return rc


if __name__ == "__main__":
    sys.exit(main())
