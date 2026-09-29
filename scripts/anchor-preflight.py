#!/usr/bin/env python3
"""变异电池的**工作树**锚点预检：拿磁盘上此刻的字节验每一格锚点。

用法：
- `python3 scripts/anchor-preflight.py`（在仓库根跑，不参数 = 核 COVERED 里登记的全部电池）
- `python3 scripts/anchor-preflight.py <电池脚本名> [电池脚本名…]`（只核点名的；认不了的形状即红）
- `python3 scripts/anchor-preflight.py --selftest`（反向测：故意做坏的电池必须被点名）

为什么要有这一份：电池本体跑在 `git clone --shared` 出来的克隆里，读的是 **HEAD + 本泳道脏
`.go` 覆盖层**；一张卡改了上一卡被注码的那几行（字段换标签、写死值换成入参、用例改名），
锚点要等到克隆建好、控制组起跑之前那一行才报"命中 0 次"——那一趟的机时白烧，
而没克隆可建的电池（就地注码的 JS 族）要等到提交之后跑整电池才发现。这一份把发现时机
挪到提交前，且不建克隆、不起测试。

它只回答三件事：
1. 锚点在这棵树里还找得到吗（命中数 != 1 即报）；
2. 注码打完字节变不变、语法坏不坏（不变 ⇒ 这一格永不开火；坏 ⇒ 跑出来只会是 build failed）；
3. expect 里点名的用例现在还真存在吗（改名后的格子只会以"没人红"的形式存活）。

**覆盖自报**（这一条是本门存在的另一半）：本门只认两种格表形状，其余电池**读不了**，
而"读不了"必须说出来而不是变成一句绿。所以：
- `COVERED` 与 `UNCOVERED` 两份名单之和必须恰好等于 `scripts/mut_*.py` 的现算名单
  （`roster_problems()`）——新电池落进 `scripts/` 而没分类，本门直接红；
- 每次运行末行都印 `覆盖 X/Y 份电池`，所以"预检绿"永远不等于"全仓锚点已核"；
- 未覆盖的那些各有自己的锚点前置：带 `--check` 的那批（见 UNCOVERED 里标注）跑自己那一门的
  工作树预检，其余在整跑的"注码前置"那一行才验——**那条时机差就是本门要继续摊开的债务**。

**它不替代**提交后的 `--check` 与整电池实跑：工作树字节与 HEAD 字节可以不同，
而"注了码而门不报"只能靠真跑用例判。
"""
from __future__ import annotations

import argparse
import importlib.util
import re
import subprocess
import sys
import types
from pathlib import Path
from typing import Callable

ROOT = Path(__file__).resolve().parents[1]

SHAPE_A = "A"  # 模块级 CELLS（7 元组）+ cell_rels(cell) + apply_cell(originals, cell)
SHAPE_B = "B"  # cells()/CELLS = [(格, 说明, [(槽, 原文, 注码), …], (该红的腿, …))] + SLOT_FILES

COVERED: dict[str, str] = {
    "mut_bill_p701.py": SHAPE_A,
    "mut_collection_p703.py": SHAPE_A,
    "mut_retention_a6.py": SHAPE_B,
    "mut_write_claim_a12.py": SHAPE_B,
    "mut_ingest_dedup_r23.py": SHAPE_B,
}

# 本门读不了的电池，逐份列全（不是"暂时没写全"的省略号）。分成三类，理由写在括号里：
#  (i) 自带 --check：本门不重复实现它的工作树预检，接进本门只需把它那份形状登记成第三种 adapter；
#  (ii) 单文件/就地注码：格表里没有"文件"这一维（文件写在模块级常量里），要接得先给电池加槽表；
#  (iii) 格表是代码拼出来的（无静态字面表），只能靠电池自己那份 --check 或整跑。
UNCOVERED: tuple[str, ...] = (
    "mut_actionability_b17.py", "mut_bad_case_p803.py", "mut_brain_action_b19h.py",
    "mut_command_log_ok_b20b.py",
    "mut_cron_err_b19c.py", "mut_d7gate_r22lane.py", "mut_d7verdict_b20g.py",
    "mut_db_poolcfg.py", "mut_dependency_b19.py", "mut_dedupkey_shape_r22.py",
    "mut_dingtalk_msgid_r22lane.py",
    "mut_egress_pool_r30.py",
    "mut_extension_auth_b19h.py", "mut_host_gate_b19f.py", "mut_hub_media_backfill.py",
    "mut_ledger_b16.py", "mut_ledger_b16b.py", "mut_ledger_b16c.py",
    "mut_outbound_claim_r22lane.py", "mut_prune_batch_b19g.py", "mut_push_budget_b20d.py",
    "mut_reach_p503.py", "mut_review_r22_teeth.py", "mut_seam_guard_r28.py",
    "mut_send_verify_b24.py", "mut_sentcache_r22lane.py", "mut_session_err_b19d.py",
    "mut_sse_ack_r23.py", "mut_startup_hook_p702.py", "mut_step_cap_b19e.py",
    "mut_submit_enter_b18.py",
    # 2026-09-29 并入旁道那一笔带进来的第 32 枚：它的 `--check`（"只验锚点与用例名，要装架、
    # 不跑 go test"）就是上面第 (i) 类，本门不重复实现；且它的格表是「文件常量 + 原文/注码 +
    # 两列用例名」的 7 元组、没有 SHAPE_A 要的 `cell_rels`/`apply_cell`，硬接只会让适配器报错。
    "mut_webhook_ai_trigger.py",
)

ANSI = re.compile(r"\x1b\[[0-9;]*m")


# ---------------------------------------------------------------- 覆盖自报


def roster() -> list[str]:
    # `mut_dispose.py` 不是电池，是整族电池共用的删除闸模块（没有格表可核），
    # 名字却撞在 `mut_*.py` 这把 glob 上 —— 与 mut-dispose-guard.test.sh 的 REAL 静态面
    # 同一处口径：那边把它从对象集合里摘掉，这边也摘掉，否则同一份模块在两枚门里
    # 一会儿算"电池"一会儿算"闸"，名单永远对不齐。
    return sorted(p.name for p in (ROOT / "scripts").glob("mut_*.py")
                  if p.name != "mut_dispose.py")


def roster_problems() -> list[str]:
    """COVERED ∪ UNCOVERED 必须恰好 == 磁盘现算名单：多一个、少一个都红。"""
    disk = set(roster())
    listed = set(COVERED) | set(UNCOVERED)
    out = [f"scripts/{n} 是新电池，没在 COVERED/UNCOVERED 里分类" for n in sorted(disk - listed)]
    out += [f"COVERED/UNCOVERED 里的 {n} 在 scripts/ 里已经不存在（名单要跟着删）"
            for n in sorted(listed - disk)]
    dup = sorted({n for n in list(COVERED) + list(UNCOVERED)
                  if list(COVERED).count(n) + list(UNCOVERED).count(n) > 1})
    out += [f"{n} 同时出现在 COVERED 与 UNCOVERED" for n in dup]
    return out


# ---------------------------------------------------------------- 通用件


def tree_reader() -> Callable[[str], str]:
    def read(rel: str) -> str:
        p = ROOT / rel
        if not p.exists():
            raise SystemExit(f"工作树里缺文件：{rel}")
        return p.read_text(encoding="utf-8")
    return read


def gofmt_ok(src: str) -> tuple[bool, str]:
    p = subprocess.run(["gofmt", "-e"], input=src, capture_output=True, text=True, timeout=120)
    return p.returncode == 0, ANSI.sub("", p.stderr).strip()


def test_names() -> set[str]:
    """用例名单按**目录枚举**取，所以枚举面就是本门的失明面。

    原先只扫 `internal/`，于是打在 `cmd/api` 启动顺序上的那三格（CMD1–CMD3）被报成
    "expect 用例在工作树里找不到" —— 假红，且红的是门自己。`cmd/` 里同样有常驻用例
    （`startup_order_test.go`），必须一起进名单。
    """
    names: set[str] = set()
    for sub in ("internal", "cmd"):
        for p in (ROOT / "user-server" / sub).rglob("*_test.go"):
            names |= set(re.findall(r"^func (Test\w+)", p.read_text(encoding="utf-8"), re.M))
    return names


def check_expect(code: str, expect, names: set[str]) -> list[str]:
    """expect 可以是单个用例名，也可以是元组/列表（一格多点名）。"""
    legs = expect if isinstance(expect, (tuple, list)) else (expect,)
    out = []
    for leg in legs:
        if not isinstance(leg, str) or not leg.startswith("Test"):
            out.append(f"{code} 的 expect 不是一个 Go 用例名（本门判不了它）：{leg!r}")
        elif leg not in names:
            out.append(f"{code} 的 expect 用例在工作树里找不到：{leg}")
    return out


def check_edits(code: str, edits: list[tuple[str, str, str]], read) -> list[str]:
    """按"同一格内逐条叠加"的口径验锚点（与电池本体一致：两条锚点打同一文件时，
    第二条要在第一条打过的字节上数命中，否则会把合法的两刀读成命中 2 次）。"""
    bad: list[str] = []
    acc: dict[str, str] = {}
    original: dict[str, str] = {}
    for rel, old, new in edits:
        if rel not in original:
            try:
                original[rel] = read(rel)
            except SystemExit as exc:
                bad.append(f"{code} 注码指向工作树里没有的文件：{exc}")
                continue
        cur = acc.get(rel, original[rel])
        hit = cur.count(old)
        if hit != 1:
            bad.append(f"{code} 锚点在 {rel} 里命中 {hit} 次（要求恰好 1）：{old[:70]!r}")
            continue
        if old == new:
            bad.append(f"{code} 注码无效（原文与注码后一致＝这一格永不开火）")
            continue
        acc[rel] = cur.replace(old, new, 1)
    for rel, src in acc.items():
        if src == original[rel]:
            bad.append(f"{code}@{rel} 注码打完了而字节没变（这一格永不开火）")
        if rel.endswith(".go"):
            ok, err = gofmt_ok(src)
            if not ok:
                bad.append(f"{code}@{rel} 注码后 gofmt 语法坏，跑出来只会是 build failed：{err[:160]}")
    return bad


# ---------------------------------------------------------------- 两种形状


def normalize_b(mod) -> list[tuple[str, tuple, list[tuple[str, str, str]]]]:
    """形状 B：把一格摊平成 (格号, expect, [(仓内相对路径, 原文, 注码), …])。"""
    slot_files = getattr(mod, "SLOT_FILES", None)
    if not isinstance(slot_files, dict):
        raise SystemExit("形状 B 需要模块级 SLOT_FILES = {槽名: 仓内相对路径}")
    raw = mod.cells() if callable(getattr(mod, "cells", None)) else mod.CELLS
    out = []
    for cell in raw:
        code, _, edits, expect = cell
        flat = []
        for slot, old, new in edits:
            if slot not in slot_files:
                raise SystemExit(f"{code} 的槽 {slot!r} 不在 SLOT_FILES 里"
                                 "（新增槽要同时登记文件，否则本门读不到它）")
            flat.append((str(slot_files[slot]), old, new))
        out.append((code, expect, flat))
    return out


def check_battery(name: str, shape: str, mod, names: set[str], read) -> tuple[int, list[str]]:
    bad: list[str] = []
    if shape == SHAPE_A:
        for attr in ("CELLS", "cell_rels", "apply_cell"):
            if not hasattr(mod, attr):
                return 0, [f"{name} 没有 {attr}()，本预检认不了这个形状"]
        cells = list(mod.CELLS)
        for cell in cells:
            code = cell[0]
            # expect 先判：锚点坏了不能连带把"用例已改名"这条也一起吞掉（一次要报全）。
            if cell[2] == "go":
                bad += [f"[{name}] {m}" for m in check_expect(code, cell[6], names)]
            try:
                originals = {rel: read(rel) for rel in sorted(mod.cell_rels(cell))}
                mutated = mod.apply_cell(originals, cell)
            except SystemExit as exc:
                bad.append(f"[{name}] 锚点：{exc}")
                continue
            for rel, text in mutated.items():
                if text == originals[rel]:
                    bad.append(f"[{name}] {code}@{rel} 注码打完了而字节没变（这一格永不开火）")
        return len(cells), bad
    try:
        cells = normalize_b(mod)
    except SystemExit as exc:
        return 0, [f"[{name}] {exc}"]
    for code, expect, edits in cells:
        bad += [f"[{name}] {m}" for m in check_expect(code, expect, names)]
        bad += [f"[{name}] {m}" for m in check_edits(code, edits, read)]
    return len(cells), bad


def load(battery: str):
    path = ROOT / "scripts" / battery
    if not path.exists():
        raise SystemExit(f"找不到电池脚本：{path}")
    spec = importlib.util.spec_from_file_location(path.stem, path)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[path.stem] = mod
    spec.loader.exec_module(mod)
    return mod


# ---------------------------------------------------------------- 反向测


def selftest() -> int:
    """每一格都要红，且红在该点的那一条判据上；不红的判据等于没有。

    全部在内存里造（假模块 + 假工作树），不往 scripts/ 落临时文件。
    """
    global UNCOVERED
    # 假工作树里刻意放**两种**锚点：`X := 1` 出现两次（给"命中 2 次"那一格），
    # `Y := 1` 只出现一次（给其余各格当合法锚点）。第一版只有一种锚点，
    # "命中 2 次"那一格被它自己造出的语法坏抢了先——反向格要绑的就是"哪条分支该开火"。
    src = ('package main\n\n'
           'func one() {\n\tX := 1\n\t_ = X\n}\n\n'
           'func two() {\n\tX := 1\n\t_ = X\n}\n\n'
           'func three() {\n\tY := 1\n\t_ = Y\n}\n')
    tree = {"pkg/a.go": src, "pkg/b.go": src}
    names = {"TestReal"}
    read = tree.__getitem__

    def battery_b(old="Y := 1", new="Y := 2", slot="a", expect=("TestReal",)):
        mod = types.ModuleType("fake_b")
        mod.SLOT_FILES = {"a": "pkg/a.go"}
        mod.cells = lambda: [("C1", "说明", [(slot, old, new)], expect)]
        return mod

    def battery_a(noop: bool = False):
        mod = types.ModuleType("fake_a")
        mod.CELLS = [("C1", "说明", "go", "pkg/a.go", None, None, "TestReal")]
        mod.cell_rels = lambda cell: {"pkg/a.go"}
        if noop:
            mod.apply_cell = lambda originals, cell: {"pkg/a.go": originals["pkg/a.go"]}
        else:
            mod.apply_cell = lambda originals, cell: {"pkg/a.go": originals["pkg/a.go"] + "// x\n"}
        return mod

    # (格名, 形状, 模块, 该开火的判据关键字)
    cases = [
        ("B 锚点 0 命中", SHAPE_B, battery_b(old="Z := 1"), "命中 0 次"),
        ("B 锚点 2 命中", SHAPE_B, battery_b(old="X := 1"), "命中 2 次"),
        ("B 注码与原文相同", SHAPE_B, battery_b(new="Y := 1"), "永不开火"),
        ("B expect 改名", SHAPE_B, battery_b(expect=("TestRenamedAway",)), "找不到"),
        ("B 槽没登记", SHAPE_B, battery_b(slot="zzz"), "不在 SLOT_FILES"),
        ("B 注码后语法坏", SHAPE_B, battery_b(new="Y := 1\nfunc broken("), "gofmt 语法坏"),
        ("A 注码后字节没变", SHAPE_A, battery_a(noop=True), "字节没变"),
    ]
    bad = ran = 0
    for title, shape, mod, want in cases:
        ran += 1
        n, problems = check_battery("fake", shape, mod, names, read)
        hit = [p for p in problems if want in p]
        if len(hit) != 1:
            bad += 1
            print(f"  ✗ 反向格「{title}」判据没开火（要 1 条含 {want!r}，实得 {len(hit)}）："
                  + " / ".join(problems))
        else:
            print(f"  ✓ {title:<16} → {hit[0][:110]}")
    ran += 1
    n, problems = check_battery("fake", SHAPE_B, battery_b(), names, read)
    if problems or n != 1:
        bad += 1
        print(f"  ✗ 对照组（完好的一格）本该 0 问题，实得 {len(problems)}：" + " / ".join(problems))
    else:
        print("  ✓ 对照组：完好的一格 0 问题")

    # 覆盖名单那份"两份之和==目录现算数"的计数器也要反向：名单与磁盘对不上时必须红，
    # 否则它只是一句印出来没人看的话。
    for title, want in (("新电池没分类", "没在 COVERED/UNCOVERED 里分类"),
                        ("名单里的电池已不在磁盘", "已经不存在")):
        ran += 1
        kept_u, kept_c = UNCOVERED[0], None
        try:
            if title == "新电池没分类":
                UNCOVERED = UNCOVERED[1:]           # 从名单里摘掉一份真存在的
            else:
                COVERED["mut_zz_not_on_disk.py"] = SHAPE_B  # 名单里塞一份不存在的
                kept_c = "mut_zz_not_on_disk.py"
            got = [p for p in roster_problems() if want in p]
        finally:
            if kept_c:
                COVERED.pop(kept_c, None)
            else:
                UNCOVERED = (kept_u,) + UNCOVERED
        if len(got) != 1:
            bad += 1
            print(f"  ✗ 反向格「{title}」判据没开火（要 1 条含 {want!r}，实得 {len(got)}）")
        else:
            print(f"  ✓ {title:<16} → {got[0][:100]}")
    if bad:
        print(f"反向测：{ran - bad}/{ran} 通过 ⇒ 本门自己的判据有洞")
        return 1
    print(f"反向测：{ran}/{ran} 格各自点名到该开火的那一条判据")
    return 0


# ---------------------------------------------------------------- 入口


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("batteries", nargs="*")
    ap.add_argument("--selftest", action="store_true", help="只跑本门自己的反向测")
    args = ap.parse_args()
    if args.selftest:
        return selftest()

    roster_bad = roster_problems()
    for p in roster_bad:
        print(f"  ✗ 覆盖名单：{p}")

    targets = args.batteries or sorted(COVERED)
    names = test_names()
    read = tree_reader()
    total_cells = 0
    bad: list[str] = []
    for battery in targets:
        if battery not in COVERED:
            bad.append(f"{battery} 本门读不了（形状不在 A/B 之内）："
                       "它自带锚点前置的跑自己那门，否则按 UNCOVERED 里的分类接一种 adapter")
            continue
        mod = load(battery)
        n, problems = check_battery(battery, COVERED[battery], mod, names, read)
        total_cells += n
        bad += problems
        print(f"[{battery}] 已核 {n} 格，{len(problems)} 处问题")
    for p in bad:
        print(f"  ✗ {p}")

    print(f"覆盖 {len(COVERED)}/{len(roster())} 份电池、本次核了 {total_cells} 格；"
          f"本门读不了 {len(UNCOVERED)} 份（名单写在脚本里，两份之和==目录现算数，是硬门）")
    if roster_bad or bad:
        return 1
    print(f"已登记的 {len(COVERED)} 份电池：锚点在工作树上各命中一次、注码后语法可解析、"
          "expect 用例名全部在场")
    return 0


if __name__ == "__main__":
    sys.exit(main())
