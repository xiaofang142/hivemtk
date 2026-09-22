#!/usr/bin/env python3
"""R28 牙齿电池：证明「seam accessor 门 + `-race` 腿」两样都真会红，且红是**窄**的。

要断的东西有三件：
  1. **门有牙**（族 A，逐格、不需编译）：把某个全局那对 accessor 的锁逐行摘掉 ⇒ `check-seam-guard.py`
     必须退 rc=1 且点名那个全局。16 格逐格摘、逐格还原、逐格比 md5。
  2. **腿有牙，且红不溢出**（族 B，一次编译）：只摘 `tgMaxMediaBytes` 一家的锁 ⇒ 跑全部 16 条腿，
     必须**恰好** `TelegramMaxBytesIsLocked` 一条 FAIL、其余 15 条 PASS，且**每一条**竞争块都只归属到
     这一条腿（用例是串行的，竞争块里印的就是当时在跑的 `TestSeamGuard_Xxx` 栈 ⇒ 归属即归因），
     且共锁邻居 `TelegramAPIBaseIsLocked` 必须在 PASS 名单里。
     挑它做窄格有讲究：`tgAPIBaseOverride` 与它**共用一把** `tgSeamMu` ⇒ 同锁的邻居仍然绿，
     这才叫"摘这一家的锁只红这一家"，而不是"锁一摘全家红"那种没分辨力的证据。
  3. **每条腿都不是摆设**（族 C，一次编译）：16 家的锁**一次性全摘**（内存里叠完再写盘）⇒
     每条腿都必须 FAIL、0 条 PASS，且 16 个归属腿名单与登记腿名单**逐一对上**、无未归属块、
     每条腿至少一条竞争块的栈里点到它自己那家的**产码文件名**。
     窄格证"不会假红"，全摘格证"没有漏网的腿"——两界合起来才是完整证据。

为什么竞争判据认"腿名 + 文件名"而不认全局变量名（第一版在这里判错过一次，记下来免得再犯）：
`-race` 报告印的是**地址 + 调用栈帧（函数名 + file:line）**，从不印被竞争的**变量名**；摘锁后
`return tgMaxMediaBytes` 这种一扇门会被**内联**掉，栈里连函数名都没了。所以"栈里必须出现全局名"
是不可满足的判据 —— 会把好证据误判成 SURVIVED。可靠的是测试名（栈里必现，因为它就是运行中的那条用例）
与**没被内联的那一侧帧**所在的生产文件名。同理，竞争块条数是 2 而不是 1：写vs写、读vs写各算一条，
判据只认"块都归本腿"。

第二版丢过一次文件名锚点，教训记在这里：`734118d9` 把只被测试调用的 setter 移进
`seam_guard_setters_test.go`（CI 的 golangci-lint 判它们死代码）之后，窄格的两条块里
**一条产码帧都不剩**（写侧只剩 `storeXxx()` @ 测试文件，读侧被内联 ⇒ B-narrow/C-all 双双 SURVIVED，
`r28_seam_battery_20260922-232846`）。当时最容易的"修法"是把期望改成"点到 setter 所在文件"——
那是把判据改松去迁就变异，不算证据。真正的修法是把证据找回来：跑腿时对**被测包**关内联
（见 `race_legs`），读侧那一帧就回到注册表 `file` 列那个产码文件里，锚点比原来更强（两侧都在）。

为什么不在这里做"逐格 -race"：那要 16 趟整包 `-race` 编译，第一版真这么跑，到第 6 格
（`r28_seam_battery_20260922-211058/06-unlocked-dingtalkOpenAPIBase-gate.log` 是 0 字节的那一份）
就把这台共享机器的数据盘写到 100%、只剩 1.7 GB 而中断 —— 而盘满带来的红全是假红。
拆法：把"逐格"的力气花在**不需要编译**的门判据上，编译只做一次窄、一次全。

口径（照本仓电池的既有规矩）：
  - 控制组**现测**：开刀前先跑一遍 16 条腿拿名单，不写死数字（共享树下别人加用例会让写死的计数漂）；
  - 变异只在内存里算，写盘一次，跑完立刻按 md5 还原比对；还原表由 apply() 自动登记；
  - 一格的红必须是「带 DATA RACE 且栈里点到本格那条腿 + 本家产码文件」的红：只 FAIL 不报竞争 ⇒ SURVIVED/BROKEN，不算证据；
  - 编译坏 ⇒ BUILD-BROKEN，修变异不修期望；
  - 日志整份落盘，结果行不过 tail/head（管道会吃掉 rc 与排在最前的红因）。

族 R（注册表面）按"门文档串里承诺了几条判据，就几格"配：缺表 rc=2、空表 rc=2、列数不对、
字段全空白、重复登记、指向不存在的文件、指向不存在的 accessor、setter 前缀指向不存在的文件、
setter 前缀文件里没这个函数、`文件:函数名` 形状坏、锁外直读（同文件）、锁外直读（跨包）
⇒ 12 格，全部不需编译。第一版只写了 4 格，其中"registry-malformed"实际注入的是一条**格式正确但文件不存在**
的行（5 列齐全，只是 `nope.go` 不在树里）⇒ 它证的是"unknown-file"那条判据，而"格式坏"那条**一直没有格**。
名字与它所证的判据不符，比没格更坏：前者会让人以为已经证过了。
setter 前缀那三格是 `734118d9` 之后补的：CI 的 golangci-lint（`.golangci.yml` 是 `run.tests: false`
+ `unused`）把 14 扇只被测试调用的 setter 判成生产面上的死代码 ⇒ 写侧挪进
`seam_guard_setters_test.go`，注册表的 setter 列因此多了 `文件:` 前缀这副形状，前缀每开一条新分支就要有自己的格。

用法：
    python3 scripts/mut_seam_guard_r28.py            # 全族（约 3 趟整包 -race 编译）
    python3 scripts/mut_seam_guard_r28.py --only-gate   # 只跑族 A + 注册表格（不编译，改完驱动先用它验）
"""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import re
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SERVER = ROOT / "user-server"
REGISTRY = ROOT / "scripts" / "seam-guard.registry"
GATE = ROOT / "scripts" / "check-seam-guard.py"
LOGDIR = Path("/tmp") / f"r28_seam_battery_{time.strftime('%Y%m%d-%H%M%S')}"

spec = importlib.util.spec_from_file_location("seamgate", GATE)
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)

LOCK_LINE = r"^\s*(?:defer\s+)?{lock}\.(?:R?Lock|R?Unlock)\(\)\s*$"

# 每条腿的测试函数名（族 B/C 用它对号）
LEG = {
    "tgAPIBaseOverride": "TestSeamGuard_TelegramAPIBaseIsLocked",
    "tgMaxMediaBytes": "TestSeamGuard_TelegramMaxBytesIsLocked",
    "qqAttachmentURLGuard": "TestSeamGuard_QQAttachmentGuardIsLocked",
    "qqMaxMediaBytes": "TestSeamGuard_QQMaxBytesIsLocked",
    "dingtalkOpenAPIBase": "TestSeamGuard_DingTalkOpenAPIBaseIsLocked",
    "dyAPIBaseOverride": "TestSeamGuard_DouyinAPIBaseIsLocked",
    "dyMediaRetryBackoff": "TestSeamGuard_DouyinRetryBackoffIsLocked",
    "wechatAPIBase": "TestSeamGuard_WeChatAPIBaseIsLocked",
    "aiReplyQuietHoursFn": "TestSeamGuard_AIReplyQuietHoursFnIsLocked",
    "dingtalkWebhookHostAllowed": "TestSeamGuard_DingTalkWebhookHostAllowedIsLocked",
    "bridgeChannelOnlineProbe": "TestSeamGuard_BridgeOnlineProbeIsLocked",
    "approvalNowFn": "TestSeamGuard_ApprovalNowFnIsLocked",
    "approvalResumeTokFn": "TestSeamGuard_ApprovalResumeTokFnIsLocked",
    "humanTaskNowFn": "TestSeamGuard_HumanTaskNowFnIsLocked",
    "IntentEnabled": "TestSeamGuard_IntentEnabledIsLocked",
    "pollingLockRepo": "TestSeamGuard_PollingLockRepoIsLocked",
}
# pollingLockRepoOnce 与 pollingLockRepo 同一把锁、同一对 accessor，摘一次即同时覆盖 ⇒ 不单列一格。
MERGED_WITH = {"pollingLockRepoOnce": "pollingLockRepo"}
NARROW = "tgMaxMediaBytes"


def md5(path: Path) -> str:
    return hashlib.md5(path.read_bytes()).hexdigest()


def build_entries() -> list[dict[str, str]]:
    """注册表由门自己解析（`run.tests:false` 那套 `文件:函数名` 前缀口径不在这里抄第二遍 ——
    两处各写一遍正是本仓犯过的"同一判据两个口径"）。"""
    entries = gate.load_registry()
    if isinstance(entries, int):
        raise RuntimeError(f"注册表读不出来（门退 rc={entries}），电池不带坏注册表开刀")
    return entries


class Mutator:
    """按注册表算「摘掉某几家锁」之后的文件内容；原文在写盘前留档，restore() 按 md5 自证。"""

    def __init__(self, entries: list[dict[str, str]]) -> None:
        self.entries = entries
        self.pristine: dict[Path, str] = {}
        self.originals: dict[Path, str] = {}

    def _path(self, rel: str) -> Path:
        path = SERVER / rel
        self.pristine.setdefault(path, md5(path))
        return path

    def unlocked(self, names: set[str]) -> dict[Path, str]:
        """返回「把这些全局的锁摘掉」之后的 {文件: 新内容}；同名多全局共文件时叠加。
        getter 在全局本家文件、setter 可能登记在另一个文件（`文件:函数名` 前缀 ⇒ 那 14 扇在
        `seam_guard_setters_test.go`），所以两扇门各自去自己的文件里摘。"""
        staged: dict[Path, list[str]] = {}
        removed: dict[str, int] = {}

        def load(rel: str) -> tuple[Path, list[str]]:
            path = self._path(rel)
            if path not in staged:
                staged[path] = path.read_text(encoding="utf-8").splitlines()
            return path, staged[path]

        for e in self.entries:
            if e["global"] not in names:
                continue
            rx = re.compile(LOCK_LINE.format(lock=re.escape(e["lock"])))
            for rel, fname in ((e["file"], e["getter"]), (e["setter_file"], e["setter"])):
                path, src = load(rel)
                ranges = gate.func_ranges(src, fname)
                if not ranges:
                    raise RuntimeError(f"{e['global']}：{fname} 不在 {rel}，变异形状不对")
                drop = {i for a, b in ranges for i in range(a, b + 1)}
                kept = [line for i, line in enumerate(src, 1) if not (i in drop and rx.match(line))]
                n = len(src) - len(kept)
                if n < 2:
                    raise RuntimeError(f"{e['global']}：{rel} 的 {fname} 只摘到 {n} 行锁操作，变异形状不对")
                removed[e["global"]] = removed.get(e["global"], 0) + n
                staged[path] = kept
        for e in self.entries:
            if e["global"] in removed:
                e["removed"] = str(removed[e["global"]])
        return {p: "\n".join(lines) + "\n" for p, lines in staged.items()}

    def apply(self, staged: dict[Path, str]) -> None:
        for path, text in staged.items():
            self.originals[path] = path.read_text(encoding="utf-8")
            path.write_text(text, encoding="utf-8")

    def restore(self) -> list[str]:
        bad = []
        for path in self.originals:
            path.write_text(self.originals[path], encoding="utf-8")
            if md5(path) != self.pristine[path]:
                bad.append(f"{path}：还原后 md5 与开刀前不一致")
        self.originals.clear()
        return bad

    def residue(self) -> list[str]:
        return [f"{p} md5 {md5(p)} != {d}" for p, d in self.pristine.items() if md5(p) != d]


def run(cmd: list[str], log: Path, cwd: Path, timeout: int) -> int:
    with log.open("w", encoding="utf-8") as fh:
        fh.write("$ " + " ".join(cmd) + f"\n(cwd {cwd})\n")
        fh.flush()
        try:
            proc = subprocess.run(cmd, cwd=cwd, stdout=fh, stderr=subprocess.STDOUT, timeout=timeout)
        except subprocess.TimeoutExpired:
            fh.write("\n[TIME-BROKEN] 超时\n")
            return -99
        return proc.returncode


def race_legs(log: Path, pattern: str, timeout: int = 1200) -> int:
    """`-gcflags=hivemtk-user/internal/service=-l`：关掉本包内联，让竞争栈留住读侧那一帧。

    摘锁后的 `return tgMaxMediaBytes` 是一扇门，默认会被内联进调用它的闭包 ⇒ 栈里只剩
    `seam_guard_race_test.go`，族 B/C 的"点到本家产码文件名"就没牙了（`734118d9` 把 setter
    移进 `_test.go` 之后，写侧那帧也不再落在产码文件里 ⇒ 两帧全没，两族当场 SURVIVED）。
    只对**被测包**关内联（不是 `all=-l`）⇒ 依赖图不重编，实测整趟多花约 3s。
    """
    return run(["go", "test", "-race", "-gcflags=hivemtk-user/internal/service=-l",
                "-v", "-run", pattern, "-count=1", "./internal/service/"],
               log, SERVER, timeout)


def race_blocks(text: str) -> list[str]:
    return [b for b in re.split(r"^={5,}$", text, flags=re.M) if "WARNING: DATA RACE" in b]


def leg_states(text: str) -> dict[str, str]:
    states: dict[str, str] = {}
    for status, name in re.findall(r"^--- (PASS|FAIL|SKIP): (TestSeamGuard_\w+)", text, re.M):
        states[name] = status
    return states


LEG_RX = re.compile(r"(TestSeamGuard_\w+IsLocked)")


def attribute_blocks(blocks: list[str]) -> tuple[dict[str, list[str]], list[str]]:
    """把每条竞争块归到它栈里那条腿上。用例串行跑 ⇒ 一块只可能属于一条腿；
    真有归不到腿的块（例如别处冒出来的竞争）单列出来，两族判据都要求它为 0。"""
    per: dict[str, list[str]] = {}
    loose: list[str] = []
    for b in blocks:
        names = sorted(set(LEG_RX.findall(b)))
        if len(names) == 1:
            per.setdefault(names[0], []).append(b)
        else:
            loose.append(b)
    return per, loose


def gate_rc_and_text(log: Path) -> tuple[int, str]:
    text = log.read_text(encoding="utf-8", errors="replace")
    m = re.search(r"^rc=(-?\d+)$", text, re.M)
    return (int(m.group(1)) if m else -1), text


def gate_run(log: Path) -> int:
    with log.open("w", encoding="utf-8") as fh:
        proc = subprocess.run([sys.executable, str(GATE)], cwd=ROOT, stdout=fh, stderr=subprocess.STDOUT)
        fh.write(f"rc={proc.returncode}\n")
    return proc.returncode


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--only-gate", action="store_true", help="只跑族 A 与注册表格（不编译）")
    args = ap.parse_args()

    LOGDIR.mkdir(parents=True, exist_ok=True)
    entries = build_entries()
    mut = Mutator(entries)
    results: list[tuple[str, str, str]] = []
    print(f"电池日志目录 {LOGDIR}；注册表 {len(entries)} 行 ⇒ 独立锁格 {len(entries) - len(MERGED_WITH)}")

    # ---------------- 控制组（现测名单，不写死数字） ----------------
    ctrl_gate = LOGDIR / "00-control-gate.log"
    if gate_run(ctrl_gate) != 0:
        print(f"rc=2 ENV-BROKEN 变异前门就红（见 {ctrl_gate}）")
        return 2
    if args.only_gate:
        ctrl_names, ctrl_races = [], 0
    else:
        ctrl = LOGDIR / "00-control-legs.log"
        rc = race_legs(ctrl, "TestSeamGuard")
        text = ctrl.read_text(encoding="utf-8", errors="replace")
        ctrl_names = sorted(leg_states(text))
        ctrl_races = len(race_blocks(text))
        if rc != 0 or ctrl_races:
            print(f"rc=2 ENV-BROKEN 控制组不干净（rc={rc}、竞争 {ctrl_races} 条，见 {ctrl}）⇒ 不带病开刀")
            return 2
    expect_legs = sorted(LEG.values())
    if not args.only_gate and sorted(set(expect_legs) - set(ctrl_names)):
        print(f"rc=2 ENV-BROKEN 有登记全局缺腿：{sorted(set(expect_legs) - set(ctrl_names))}")
        return 2
    print(f"控制组：门绿；腿 {len(ctrl_names)} 条全绿、0 竞争" if not args.only_gate else "控制组：门绿（--only-gate 不跑腿）")

    # ---------------- 族 A：逐格摘锁 ⇒ 门红且点名 ----------------
    for idx, e in enumerate(entries, 1):
        g = e["global"]
        if g in MERGED_WITH:
            results.append((g, "SKIP", f"与 {MERGED_WITH[g]} 同锁同 accessor，摘一次即覆盖"))
            continue
        try:
            staged = mut.unlocked({g})
        except RuntimeError as err:
            results.append((g, "BROKEN", str(err)))
            print(f"  [{g}] BROKEN — {err}")
            continue
        mut.apply(staged)
        log = LOGDIR / f"A{idx:02d}-unlocked-{g}.log"
        gate_run(log)
        rc, text = gate_rc_and_text(log)
        if rc == 1 and g in text and "没对" in text:
            v, why = "KILLED", f"rc=1 且点名 {g} 的锁"
        elif rc == 1 and g in text:
            v, why = "KILLED", f"rc=1 且点名 {g}"
        else:
            v, why = ("SURVIVED" if rc == 0 else "BROKEN"), f"门退 rc={rc}（期望 1 且点名 {g}）"
        bad = mut.restore()
        results.append((g, "BROKEN" if bad else v, why + ("；" + ";".join(bad) if bad else "")))
        print(f"  [A/{g}] {v} — {why}")

    # ---------------- 族 R：注册表面与绕门格（都不需要编译） ----------------
    for cell in registry_cells():
        tag = f"R-{cell['id']}"
        log = LOGDIR / f"{tag}.log"
        kind = cell["kind"]
        before = md5(REGISTRY)
        if kind == "rename-registry":
            tmp = REGISTRY.with_suffix(".registry.r28bak")
            REGISTRY.rename(tmp)
            rc = gate_run(log)
            tmp.rename(REGISTRY)
        elif kind == "registry-text":
            original = REGISTRY.read_text(encoding="utf-8")
            mutated = cell["mut"](original)
            if mutated == original:
                results.append((cell["id"], "BROKEN", "变异没落地（注册表文本一字未改）"))
                print(f"  [R/{cell['id']}] BROKEN — 变异没落地")
                continue
            REGISTRY.write_text(mutated, encoding="utf-8")
            rc = gate_run(log)
            REGISTRY.write_text(original, encoding="utf-8")
        else:
            path = SERVER / cell["file"]
            src = path.read_text(encoding="utf-8").splitlines()
            pos = next((i for i, line in enumerate(src) if line.startswith(cell["anchor"])), None)
            if pos is None:
                results.append((cell["id"], "BROKEN", f"锚点 {cell['anchor']!r} 不在 {cell['file']}"))
                print(f"  [{cell['id']}] BROKEN — 锚点没找到")
                continue
            src.insert(pos + (0 if cell.get("top") else 1), cell["inject"])
            mut.pristine.setdefault(path, md5(path))
            mut.apply({path: "\n".join(src) + "\n"})
            rc = gate_run(log)
            bad = mut.restore()
            if bad:
                results.append((cell["id"], "BROKEN", "还原失败：" + ";".join(bad)))
                print(f"  [R/{cell['id']}] BROKEN — 还原失败")
                continue
        text = log.read_text(encoding="utf-8", errors="replace")
        restored = md5(REGISTRY) == before
        ok = rc == cell["rc"] and all(s in text for s in cell["needles"]) and restored
        v = "KILLED" if ok else ("BROKEN" if not restored else "SURVIVED")
        why = f"rc={rc}（期望 {cell['rc']}）· {cell['desc']}" + ("" if restored else "；注册表没还原")
        results.append((cell["id"], v, why))
        print(f"  [R/{cell['id']}] {v} — {why}")

    # ---------------- 族 B：窄格（只摘一家）⇒ 只有本家腿红、共锁邻居仍绿 ----------------
    # ---------------- 族 C：全摘 ⇒ 每条腿都红、每条腿各有自己的竞争块 ----------------
    if not args.only_gate:
        all_names = {e["global"] for e in entries} - set(MERGED_WITH)
        for tag, names in (("B-narrow", {NARROW}), ("C-all", all_names)):
            staged = mut.unlocked(names)
            mut.apply(staged)
            log = LOGDIR / f"{tag}-race.log"
            race_legs(log, "TestSeamGuard")
            text = log.read_text(encoding="utf-8", errors="replace")
            blocks = race_blocks(text)
            per, loose = attribute_blocks(blocks)
            states = leg_states(text)
            failed = sorted(n for n, s in states.items() if s == "FAIL")
            passed = sorted(n for n, s in states.items() if s == "PASS")
            home_of = {LEG[e["global"]]: Path(e["file"]).name for e in entries if e["global"] in names}
            hit_home = {leg: any(home_of[leg] in b for b in bs) for leg, bs in per.items()}
            if tag == "B-narrow":
                want = LEG[NARROW]
                neighbour = LEG["tgAPIBaseOverride"]
                ok = (not loose and list(per) == [want] and hit_home.get(want)
                      and failed == [want] and set(passed) == set(expect_legs) - {want}
                      and neighbour in passed)
                v, why = ("KILLED", f"{len(blocks)} 条竞争全归本腿且点到 telegram_media.go、本家 FAIL、"
                                    f"共锁邻居 {neighbour} 与其余各条仍 PASS({len(passed)})") if ok else \
                    ("SURVIVED", f"归属 {sorted(per)}、未归属 {len(loose)}、本家文件被点名 {hit_home.get(want)}、"
                                 f"FAIL {failed}、PASS {len(passed)}")
            else:
                missing = sorted(set(expect_legs) - set(per))
                no_home = sorted(leg for leg, hit in hit_home.items() if not hit)
                ok = (not loose and not missing and not no_home
                      and failed == sorted(expect_legs) and not passed)
                v, why = ("KILLED", f"{len(blocks)} 条竞争、{len(per)} 家各有自己的块且都点到本家文件、"
                                    f"{len(failed)} 条腿全 FAIL、0 条 PASS") if ok else \
                    ("SURVIVED", f"缺腿 {missing}、没点到本家文件的腿 {no_home}、未归属 {len(loose)}、"
                                 f"FAIL 差 {set(LEG.values()) ^ set(failed)}、PASS {passed}、竞争 {len(blocks)} 条")
            bad = mut.restore()
            results.append((tag, "BROKEN" if bad else v, why + ("；" + ";".join(bad) if bad else "")))
            print(f"  [{tag}] {v} — {why}")

    residue = mut.residue()
    if REGISTRY.with_suffix(".registry.r28bak").exists():
        residue.append("注册表备份没清")

    tally: dict[str, int] = {}
    for _, v, _ in results:
        tally[v] = tally.get(v, 0) + 1
    print("\n=== 汇总 ===")
    for cell, v, why in results:
        print(f"{cell}\t{v}\t{why}")
    print("计数：", tally)
    print(f"电池日志：{LOGDIR}")
    print(f"树残留：{residue or '无（全部还原且 md5 与开刀前一致）'}")
    ran = [c for c, v, _ in results if v != "SKIP"]
    not_killed = {k: v for k, v in tally.items() if k not in ("KILLED", "SKIP")}
    ok = len(ran) == tally.get("KILLED", 0) and not not_killed and not residue
    print(f"结论：判 {len(ran)} 格（另有 {tally.get('SKIP', 0)} 格合并 SKIP），全杀 ⇒ {ok}")
    return 0 if ok else 1


def registry_cells() -> list[dict]:
    """注册表面的格子：门文档串里承诺几条判据就几格，名字必须与它所证的那条对上。"""

    def app(line: str):
        return lambda txt: txt.rstrip("\n") + "\n" + line + "\n"

    def duplicate_row(txt: str) -> str:
        rows = [l for l in txt.splitlines() if l.strip() and not l.lstrip().startswith("#")]
        return txt.rstrip("\n") + "\n" + rows[0] + "\n"

    def setter_row(new: str):
        """把 `wechatAPIBase` 那一行的 setter 列换成 `new`（那一行的 setter 带 `文件:` 前缀，
        是注册表里"写侧在另一个文件"的现成样本）。变异没落地时 main 会按"文本没变"判 BROKEN。"""
        def mut(txt: str) -> str:
            old = next(l for l in txt.splitlines() if l.startswith("wechatAPIBase\t"))
            return txt.replace(old, "\t".join(old.split("\t")[:4] + [new]), 1)
        return mut

    def comments_only(txt: str) -> str:
        return "\n".join(l for l in txt.splitlines() if l.strip().startswith("#")) + "\n"

    return [
        {"id": "registry-missing", "kind": "rename-registry", "rc": 2, "needles": ["缺注册表"],
         "desc": "缺表 ⇒ rc=2（零命中与没扫分不开）"},
        {"id": "registry-empty", "kind": "registry-text", "mut": comments_only, "rc": 2,
         "needles": ["注册表是空的"], "desc": "只剩注释 ⇒ rc=2（常绿门比没有门更误导）"},
        {"id": "registry-short-row", "kind": "registry-text",
         "mut": app("short_row\tinternal/service/wechat.go"), "rc": 1, "needles": ["格式坏", "short_row"],
         "desc": "列数不对 ⇒ rc=1"},
        {"id": "registry-blank-field", "kind": "registry-text",
         "mut": app("blank_field\t \tmu\tloadX\tstoreX"), "rc": 1, "needles": ["格式坏", "blank_field"],
         "desc": "字段全空白 ⇒ rc=1（与上一格同一条 if 的两个分支，各打一刀）"},
        {"id": "registry-duplicate", "kind": "registry-text", "mut": duplicate_row, "rc": 1,
         "needles": ["重复登记"], "desc": "同一全局登记两遍 ⇒ rc=1"},
        {"id": "registry-unknown-file", "kind": "registry-text",
         "mut": app("ghost_global\tnope.go\tnopeMu\tnopeLoad\tnopeStore"), "rc": 1,
         "needles": ["不在树里", "ghost_global"], "desc": "注册表指向不存在的文件 ⇒ rc=1"},
        {"id": "registry-unknown-accessor", "kind": "registry-text",
         "mut": app("ghost_accessor\tinternal/service/wechat.go\twechatSeamMu\tnopeGetter\tnopeSetter"), "rc": 1,
         "needles": ["accessor 找不到", "nopeGetter"], "desc": "指向被改名/删除的 accessor ⇒ rc=1"},
        {"id": "setter-prefix-unknown-file", "kind": "registry-text",
         "mut": setter_row("nope_setters_test.go:storeWechatAPIBase"), "rc": 1,
         "needles": ["setter 文件不在树里", "wechatAPIBase"],
         "desc": "setter 的 `文件:` 前缀指向不存在的文件 ⇒ rc=1（14 扇 test-only 写侧都靠这个前缀定位）"},
        {"id": "setter-prefix-no-such-func", "kind": "registry-text",
         "mut": setter_row("internal/service/wechat.go:nopeStore"), "rc": 1,
         "needles": ["accessor 找不到", "nopeStore"],
         "desc": "前缀文件在树里、那里却没有这个函数 ⇒ rc=1（证的是第二分支，不是上一格那条）"},
        {"id": "setter-prefix-bad-shape", "kind": "registry-text",
         "mut": setter_row("internal/service:x:y"), "rc": 1,
         "needles": ["形状坏", "internal/service:x:y"],
         "desc": "两个冒号的 `文件:函数名` ⇒ rc=1（否则 split 会把 `x:y` 当函数名，门永远找不到它）"},
        {"id": "bare-read-same-file", "kind": "inject-bare-read", "rc": 1, "global": "tgMaxMediaBytes",
         "needles": ["accessor 之外", "tgMaxMediaBytes"],
         "file": "internal/service/telegram_media.go", "anchor": "func FetchTelegramMedia(",
         "inject": "\t_ = tgMaxMediaBytes // 变异：绕过 accessor 直读",
         "desc": "同文件锁外直读 ⇒ rc=1"},
        {"id": "bare-read-cross-package", "kind": "inject-bare-read", "rc": 1, "global": "IntentEnabled",
         "needles": ["accessor 之外", "IntentEnabled"],
         "file": "internal/controller/intent.go", "anchor": "func ", "top": True,
         "inject": "var _ = service.IntentEnabled // 变异：跨包直读导出量",
         "desc": "跨包直读导出量 ⇒ rc=1"},
    ]


if __name__ == "__main__":
    sys.exit(main())
