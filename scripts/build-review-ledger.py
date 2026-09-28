#!/usr/bin/env python3
"""从 spec 生成二次审核台账（jsonl）：Phase A 的抽取必须是**可重跑的**，不是一篇手抄。

为什么住在仓里：协议的 A 相（抽轴）产出的是"每条承诺一行 {claim, attack, status}"。上一版把它
写在 /tmp 的一次性脚本里，于是台账成了一份抄本——spec 改了字、条目序号漂移，重跑不出来，
只能重抄（本轮闭合门为此新加的 `claim.quote ≤96 字` 判据，正是为了逼人留指针而不是留抄本）。
本脚本用**闭合门自己那套解析函数**（import 之，不另写一份正则），保证"生成时数出来的条目"与
"关门时对账的条目"是同一次抽取的结果。

用法：
    python3 scripts/build-review-ledger.py --inventory <round>     # 列出该轮 spec 里每条承诺的 (节, 序号, 原文首 72 字)
    python3 scripts/build-review-ledger.py <round> [out.jsonl]     # 生成台账（写到 stdout 或指定文件）

round 见 ROUNDS 表；生成后必须跑一次闭合门（scripts/check-review-closeout.py）才算数。

**自引用两趟法**（`--bootstrap`）：本协议台账里"合并门禁那趟 rc=0"这一条，引用的正是接下来那趟
门禁的产物；不加开关时生成器硬停（默认口径＝没有读数不入账），加开关时照样写出**最终那份字节**、
只把待跑名单打到 stderr。跑完那趟再执行一次闭合门：它读到的是对同一份字节的真 rc=0 运行，
引用的文件那时都在盘上。缺的那趟没跑就想入账，是本仓最贵的一类假绿。
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import os
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GATE = ROOT / "scripts" / "check-review-closeout.py"

# --bootstrap 打开时收集"引用的证据日志还不存在"的名单（见 main 里的说明）。
BOOTSTRAP = False
PENDING: list[str] = []

_spec = importlib.util.spec_from_file_location("closeout_gate", GATE)
if _spec is None or _spec.loader is None:
    raise SystemExit(f"读不到闭合门：{GATE}")
gate = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(gate)


def load_sibling(name: str):
    """按**路径**加载 scripts/ 里的同目录模块，而不是 `import name`。

    裸 import 是赌 `sys.path[0]` 就是 scripts/：从别处用 importlib 载入本文件时这个赌注会输，
    症状是"未知轮次 r22"或直接 ImportError——两者都长得像"台账没生成"，而不像路径没对上。
    """
    p = ROOT / "scripts" / f"{name}.py"
    if not p.is_file():
        raise SystemExit(f"载入 {name} 失败：{p} 不在（搬走条目表时别只搬一半）")
    spec = importlib.util.spec_from_file_location(name, p)
    if spec is None or spec.loader is None:
        raise SystemExit(f"载入 {name} 失败：{p} 不能被 spec_from_file_location 认")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def spec_path(rel: str) -> Path:
    p = ROOT / rel
    if not p.is_file():
        raise SystemExit(f"spec 不存在：{p}")
    return p


def fragments(rel: str) -> list[tuple[str, int, str]]:
    """[(所属 ## 节, 节内序号, 该条目的原文（未抹空白）)]，与门里的 section_entries 同序。"""
    lines, bs = gate.read_spec(spec_path(rel))
    out = []
    for sec in dict.fromkeys(b[0] for b in bs):
        entries = []
        for s, h, lo, hi in bs:
            if s != sec and h != sec:
                continue
            cur = []
            for l in lines[lo:hi]:
                if gate.ENTRY_START.match(l):
                    if cur:
                        entries.append("\n".join(cur))
                    cur = [l]
                elif not l.strip():
                    if cur:
                        entries.append("\n".join(cur))
                        cur = []
                else:
                    cur.append(l)
            if cur:
                entries.append("\n".join(cur))
        entries = [e for e in entries if e.strip()]
        # 与 section_entries 一致：只留能对上"可数形状或标题段"的片段；序号按门里的顺序给。
        for i, e in enumerate(entries, 1):
            out.append((sec, i, e))
    return out


def pins_and_coverage(rel: str, ledger_secs: set[str], oos_reasons: dict[str, str]) -> dict:
    """按**实测**读数生成 meta.scope（逐节逐形状钉）与 meta.coverage（逐节声明归属）。"""
    p = spec_path(rel)
    lines, bs = gate.read_spec(p)
    segs = gate.own_segments(lines, bs)
    tot = gate.coverage_totals(lines, segs)
    scope, cov = [], []
    for sec in dict.fromkeys(b[0] for b in bs):
        shapes = {sh: gate.count_shape(lines, lo, hi, sh)
                  for s, _, lo, hi in bs if s == sec
                  for sh in gate.COUNTED}
        shapes = {k: v for k, v in shapes.items() if v}
        if sec not in ledger_secs:
            cov.append({"section": sec, "total": tot.get(sec, 0), "status": "out-of-scope",
                        "reason": oos_reasons[sec]})
            continue
        cov.append({"section": sec, "total": tot.get(sec, 0), "status": "ledger"})
        for sh, n in shapes.items():
            scope.append({"section": sec, "heading": sec, "shape": sh, "count": n,
                          "note": "本轮实测抽取，值来自 --inventory 同一条解析路径"})
    return {"scope": scope, "coverage": cov}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("round")
    ap.add_argument("out", nargs="?")
    ap.add_argument("--inventory", action="store_true")
    ap.add_argument("--chars", type=int, default=44, help="quote 取每条原文前 N 字（≤门的 96 字上界）")
    ap.add_argument("--bootstrap", action="store_true",
                    help="允许引用**本次生成之后才跑得出来**的证据日志（自引用两趟法），"
                         "生成后把待跑名单打到 stderr；不加这个开关就硬停")
    a = ap.parse_args()
    if a.round not in ROUNDS:
        raise SystemExit(f"未知轮次 {a.round}（可选：{' '.join(sorted(ROUNDS))}）")
    # 门的指针有上下界（check-review-closeout.py 的 MIN_PROSE/MAX_QUOTE）。抽取器不跟着限，
    # `--chars 4` 就会生成一整份"看着合法、其实谁也没点名"的台账，要等闭合门跑完才红——
    # 而那一趟是 40 分钟的 Go 全量之后。宁可在这里停：生成侧与判据侧同一条界。
    if not 12 <= a.chars <= 96:
        raise SystemExit(f"--chars={a.chars} 落在门的指针界外 [12, 96]："
                         "太短＝同一节里必然撞车，太长＝抄正文（两份事实源）")
    cfg = ROUNDS[a.round]
    global BOOTSTRAP
    BOOTSTRAP = a.bootstrap
    if a.inventory:
        for sec, i, e in fragments(cfg["spec"]):
            flat = re.sub(r"\s+", "", e)
            print(f"{sec[:34]:<34} #{i:<3} {len(flat):>4}字  {flat[:72]}")
        return 0
    rows = cfg["build"](cfg["spec"], a.chars)
    pinned = pin_evidence_logs(rows)
    out = "\n".join(json.dumps(r, ensure_ascii=False) for r in rows) + "\n"
    if a.out:
        p = Path(a.out)
        p.write_text(out, encoding="utf-8")
        print(f"{p}：{len(rows) - 1} 条 + meta，证据读数现取钉住 {pinned} 条")
    else:
        sys.stdout.write(out)
    if PENDING:
        print(f"--bootstrap：{len(PENDING)} 份证据日志还不存在，写完台账后必须把它们跑出来"
              f"（跑完再复跑闭合门才算数）：\n  " + "\n  ".join(PENDING), file=sys.stderr)
    return 0


# ---------------------------------------------------------------- 公共形状

LEDGER_DIR = "docs/superpowers/specs/ledger/"
LOG = LEDGER_DIR + "logs/"
GATE_SCRIPT = "scripts/check-review-closeout.py"
BATTERY = "scripts/mut_ingest_dedup_r23.py"


def sleg(script: str, invocation: str, expect: str) -> dict:
    """脚本腿：守卫是一支脚本/一道门时的可点形状（门判：脚本在仓内 + 命令里逐字出现它 + expect ≥12 字）。"""
    return {"script": script, "invocation": invocation, "expect": expect}


def elog(path: str, says: list[str], nots: list[str] = ()) -> dict:
    """`logs[]` 的**手写**断言形状：给"后一趟跑会把前一趟原地盖掉、且断言要绑语义"的那几类读数。

    收口家族（闭合门/自检/克隆门/合并门禁自己的取证）走这里——它们由本轮反复重出，
    且 `evidence_pin()` 现取的判定行对它们来说顺序是反的（台账生成在前、取证重出在后），
    所以**只钉语义串、不钉判定行**，"这条读数出自最终那趟台账"由 §九 记的两趟交叉复验承担。
    其余一次性取证不必手写：`pin_evidence_logs()` 在生成时现取判定行入账。
    """
    return {"path": path, "must_say": list(says), "must_not_say": list(nots)}


# 没有 `=====` 判定行、又确实要钉的取证（vitest 汇总表、单包 rc 尾巴、被否掉的夹具红证、
# 存活取证那一类"记的就是红"的件）：逐文件点名**一句稳定行**。宁可在这里补一行，
# 也不要留一条只核"文件在不在"的引用——那正是本轮 `merge-gate-full.log` 被一次合法的
# `--only` 子集原地盖掉却没惊动任何人的形状。
MANUAL_PIN: dict[str, str] = {
    "B24sendverify/run2-16cells/CONTROL.log": " Test Files  4 passed (4)",
    "B24sendverify/run2-16cells/V12.log": "      Tests  1 failed | 23 passed (24)",
    "B24sendverify/run2-16cells/V16.log": " Test Files  1 failed | 3 passed (4)",
    "R22-lanes/a6a2-newlegs-fail-text-r22lane3.log": "[控制组] repository 跑完 4/4 PASS=4 FAIL=0 SKIP=0",
    "R22-lanes/a6a2-oldlegs-under-knife-r22lane3.log": "D20 [repository] 存活（旧腿全绿） ｜ 跑完 19/19 PASS=19 FAIL=0 SKIP=0",
    "R22-lanes/bridge-vitest-mutant-before-newlegs.txt": " Test Files  58 passed (58)",
    "R22-lanes/bridge-vitest-mutant-dedupkey-only.txt": " Test Files  1 failed (1)",
    "R22-lanes/bridge-vitest-mutant-with-newlegs.txt": " Test Files  1 failed | 58 passed (59)",
    "R22-lanes/mut_retention_a6-d23-cell-r22close.log": "[Go] 控制组 rc=1 total=30 passed=29 skip=0",
    "R22-lanes/service-channelgw-gate-full.log": "gate_rc=0",
    "R22-lanes/service-full-mut-batchecho.log": "--- FAIL: TestHandleIngressBatch_SelfEchoPersistsNothingAndDupPersists",
    "R22-teeth/D1.log": "--- FAIL: TestSignalConfirmBindsPayload",
    # 每刀钉**该刀杀到的那一条**，不是"这一族里随便一条 FAIL"：三条腿同名 FAIL 行会被
    # 别刀的日志满足，那等于没钉（D1 杀 4 条、D2 杀 2 条、D3 杀 3 条，各取其独有的一条）。
    "R22-teeth/D2.log": "--- FAIL: TestConfirmGateFramesBindPayloadAndOmitBody",
    "R22-teeth/D3.log": "--- FAIL: TestB20ConfirmGateElsewhereFromAuditFrame",
    "R23dedup/CONTROL.log": "ok  \thivemtk-user/internal/service",
    "R23dedup/K1-redis-key-loses-conversation.log": "--- FAIL: TestIngress_ContentDedupKeyCarriesConversation",
    "R23sseack/CONTROL.log": " Test Files  1 passed (1)",
    "R23sseack/S1.log": " Test Files  1 failed (1)",
}


# 判定行的形状：门与所有电池都用 `===== …… =====` 收尾，但它可能缩进、也可能被克隆面
# 那份转写加上 `  | ` 前缀（实测：`gate-in-shared-clone.log` 里的两条就是 `  | ===== …`）。
# 只认裸行首会把这类整件漏成"没有判定行"，退成只核存在性的引用。
VERDICT_RE = re.compile(r"^\s*(?:\|\s*)?=====")
# 纯 `=====` 分隔线（文档里当横线用）长度不够，钉了等于没钉，所以设一条下限。
MIN_VERDICT_LEN = 12


def evidence_pin(path: str) -> dict | None:
    """把一条只查存在性的引用升级成"内容仍是生成这一刻的那份读数"。

    判定行**现取**（窗口内全部 `=====` 行，去重后整条入账），不手抄——抄来的数会漂
    （本轮两次：自检 51→57、门牙 12→17），而漂移恰好只在"证据已经换了趟、断言还在替旧读数
    背书"时才咬人，也就是判据 3 要拦的那件事。一份取证里多条判定行全钉，不是为了啰嗦：
    克隆面那份同时记着"闭合判定绿"与"自检 n/n"两条，少钉一条就等于放行"只剩一条的那次覆盖"。
    窗口与脱色都走门自己那两个函数（`gate.log_window` / `gate.strip_ansi`）：界不一致时，
    这里取得到、门判不到的读数会被钉成一条永远红着的断言，所以先证"钉得住"再钉。
    """
    p = ROOT / path
    if not p.is_file():
        return None  # --bootstrap 阶段：不存在那条已经在 build 里拦过／记进 PENDING，这里不重复报
    window = gate.strip_ansi(gate.log_window(p))
    says = list(dict.fromkeys(
        ln.lstrip().lstrip("|").strip()
        for ln in window.splitlines() if VERDICT_RE.match(ln)))
    says = [s for s in says if len(s) >= MIN_VERDICT_LEN]  # 纯 `=====` 分隔线不是读数，钉了等于没钉
    if not says:
        one = next((v for k, v in MANUAL_PIN.items() if path.endswith(k)), None)
        if one is None:
            raise SystemExit(f"{path}：判读窗口内没有 `=====` 判定行、也不在 MANUAL_PIN 里"
                             " ⇒ 只能退成只核存在性的引用，而那挡不住「事后被另一趟原地盖掉」")
        says = [one]
    for s in says:
        if s not in window:
            raise SystemExit(f"{path}：要钉的读数落在门的判读窗口"
                             f"（头尾各 {gate.LOG_READ_CAP} 字节、脱色后）之外，钉了必红")
    return {"path": path, "must_say": says}


def pin_evidence_logs(rows: list[dict]) -> int:
    """就地给两份台账里所有字符串形状的 `logs[]` 补内容断言；返回被升级的条数。

    `VERDICT_PINNED` 里那几份已经是字典形状（手写断言），但手写的都是**不带数的语义串**——
    对自引用的那几条这是必须的（跑闭合门时本轮的判定行还没写下），代价是"台账写 59 种、
    证据却还是上一版门的 57/57"这种漂移门看不见。这三份由电池在门之前跑完、不自引用，
    所以连它们自己的末行判定一起钉上，把那个缺口关掉。
    """
    pinned = 0
    for r in rows:
        logs = (r.get("evidence") or {}).get("logs")
        if not isinstance(logs, list):
            continue
        upgraded = []
        for e in logs:
            if isinstance(e, str):
                pinned += 1
                upgraded.append(evidence_pin(e) or e)
                continue
            if e.get("path") in VERDICT_PINNED:
                extra = (evidence_pin(e["path"]) or {}).get("must_say") or []
                have = set(e.get("must_say") or [])
                e["must_say"] = list(e.get("must_say") or []) + [x for x in extra if x not in have]
                pinned += 1
            upgraded.append(e)
        r["evidence"]["logs"] = upgraded
    return pinned


def gate_step_counts() -> tuple[str, str]:
    """(必须绿的门步数, 总步数)：**现读** `merge-gate.py` 的 STEPS 与 DIAG，不在台账里手抄。

    手抄的数会变成抄本（本轮撞过两次：自检 51→57、门牙 12→17）。现读的代价是"门一加步骤，
    旧的全量取证立刻不再满足断言"——这不是麻烦，正是要的性质：证据必须出自**当前这套门**的跑，
    少跑一趟就该红，而不是继续绿着替一趟没跑过的读数背书。
    """
    import importlib.util as _u

    spec = _u.spec_from_file_location("_mg_for_step_count", ROOT / "scripts/merge-gate.py")
    m = _u.module_from_spec(spec)
    spec.loader.exec_module(m)
    return str(len(m.STEPS) - len(m.DIAG)), str(len(m.STEPS))


_CELLS_LOG: str | None = None
_CELLS_LOADED = False


def selftest_log() -> str | None:
    """台账引用的那份自检日志正文；没跑出来时返回 None（＝只算数不查账）。"""
    global _CELLS_LOG, _CELLS_LOADED
    if not _CELLS_LOADED:
        p = ROOT / P_SELFTEST_LOG["path"]
        _CELLS_LOG = p.read_text(encoding="utf-8") if p.is_file() else None
        _CELLS_LOADED = True
    return _CELLS_LOG


def cells(*names: str) -> str:
    """把"这条判据由哪几格反向测钉住"写成格名清单：**生成时就地跟自检日志对账**，格数现算。

    两条各自撞过的失效形状一起管：
    1. 手写「共 N 格」——加一格就漂（本轮两次：自检 51→57、门牙 12→17），而闭合门只核 legs 形状
       与 `claim.quote` 指针，**核不到 expect 里的数字**，所以漂了也没人看见；
    2. 手写一个日志里根本不存在的格名——等于台账替一格没建过的反向测背书。
    现在 N 由 `len(names)` 现算，名单逐个查在不在日志里；日志还没跑出来（--bootstrap 阶段）
    就只算数不查账，等日志落盘后重新生成一次即完成对账。

    记号约定（写死在这里，别靠读的人猜）：expect 里**反引号**标的＝自检日志里的格名，会被逐个查账；
    要引日志行原文或节名一律用「」，不参与查账——P-41 那几处引的是别的门禁件的末行。
    """
    log = selftest_log()
    if log is not None:
        absent = [n for n in names if n not in log]
        if absent:
            raise SystemExit(f"格名 {absent} 在 {P_SELFTEST_LOG['path']} 里找不到"
                             f" ⇒ 台账在替不存在的反向格背书（格子改名要同步这里）")
    return "、".join(f"`{n}`" for n in names) + f" 共 {len(names)} 格"


def selftest_leg(expect: str) -> dict:
    return sleg(GATE_SCRIPT, f"python3 {GATE_SCRIPT} --selftest", expect)


def gate_leg(on: str, expect: str) -> dict:
    return sleg(GATE_SCRIPT, f"python3 {GATE_SCRIPT} {on}", expect)


def batt_leg(invocation_tail: str, expect: str) -> dict:
    return sleg(BATTERY, f"python3 {BATTERY}{invocation_tail}", expect)


def gleg(pkg: str, name: str) -> dict:
    return {"pkg": pkg, "name": name}


# ---------------------------------------------------------------- r22-protocol：本协议自身那 41 条

# 收口轮（R22-closefinal）重出的读数型证据。为什么重出而不是沿用：旧那几份记的是**当时**的台账
# （51 格自检、40 条条目、克隆面"有红"因当时两份证据文件还没产出），而协议 §3 判据 3 现在要求
# 证据"内容仍是这条承诺的读数"——沿用＝拿一份不再证明该证明之事的文件当证据。
# 旧文件一律原样留在 `logs/R22-protocol/`，§九 的叙述还要拿它们讲"失效长什么样"。
CLOSE = LOG + "R22-closefinal/"
PROVISIONAL = os.environ.get("R22_PROVISIONAL") == "1"
PREV = LOG + "R22-protocol/"
P_SELFTEST_LOG = elog(CLOSE + "selftest-closeout.log",
                      ["种坏形态各自隔离地红，合法快照 0 红"], nots=["✗"])
P_GATE_R22_LOG = elog(CLOSE + "closeout-r22.log",
                      ["闭合判定：台账全部落在四终态且证据可核，计数钉全对且每条都有条目兜住"],
                      nots=["不成立"])
P_GATE_SELF_LOG = (PREV + "gate-r22-protocol.log" if PROVISIONAL else
                   elog(CLOSE + "closeout-r22-protocol.log",
                        ["闭合判定：台账全部落在四终态且证据可核，计数钉全对且每条都有条目兜住"],
                        nots=["不成立"]))
# 自指阶梯（只在本轮收口这一趟用）：`gate-in-shared-clone.log` 与 `merge-gate-full.log`
# 都是**门自己这一趟写**的文件——闭合门不许打开自己正在写的那份（写到第 33 步时末行还不存在），
# 而克隆那趟又要求工作树里的门全绿才印得出「rc=0 且逐字一致」。两份互相依赖 ⇒ 同一轮里
# 不可能同时闭合。所以第一遍（`R22_PROVISIONAL=1`）把这两份引用成**上一轮的旧取证文件**
# （只核存在性）先把本轮其余读数跑绿，跑完再把断言接回本轮这一份。
# 旧文件一律留在 `logs/R22-protocol/` 不动，§九 还要拿它们讲失效长什么样。
P_CLONE_LOG = (PREV + "gate-in-shared-clone.log" if PROVISIONAL else
               elog(CLOSE + "gate-in-shared-clone.log",
                    ["克隆面：rc=0，门在私有克隆里跑得出与工作树逐字一致的读数"],
                    nots=["有红，见上", "判错"]))
P_GATE_FAMILY_LOG = elog(CLOSE + "gate-family-selftests.log",
                         ["种坏读数各自拦停/判红，绿读数不误伤", "格判对",
                          "格各自点名到该开火的那一条判据"], nots=["判错", "✗"])
GATED, TOTAL = gate_step_counts()
# 全量取证只钉**开头那两行**（跑的是哪条命令、名单里几步），不钉末行判定——末行是这一趟
# 最后才写下的，跑闭合门那一步时读不到。把"没红"写成 `must_not_say` 反而两头都成立：
# 本趟跑到那一步时判定行还没写 ⇒ 绿；**下一趟**再核这份已写完的文件时，判定行算数 ⇒ 跨趟对账。
P_MERGE_LOG = (PREV + "merge-gate-full.log" if PROVISIONAL else
               elog(CLOSE + "merge-gate-full.log",
                    ["$ python3 scripts/merge-gate.py --round R22-closefinal",
                     f"步骤名单（{TOTAL} 步，全程不带 -run）"],
                    nots=["红在：", "--only"]))
# 注码合法地会让**被摘的那几格**印「判据没开火」——那正是这一刀要的效果，所以不许出现的
# 串只能钉电池自己的判决，不能钉格子的。
P_TEETH_BATTERY_LOG = elog(CLOSE + "gate-teeth-battery.log",
                           ["刀各自点名到该开火的那一格"], nots=["没开火＝该格无牙"])
# 这三份在门之前由电池跑完、不被本轮那趟写 ⇒ 连末行判定一起钉（见 `pin_evidence_logs`）。
# 不自收口轮这三份的其余手写断言仍只钉语义串：`merge-gate-full.log` 的末行、克隆那趟的读数、
# 本协议台账自己的闭合读数，都在"读它的那一步"之后才写下，钉了就是钉一条当时必定红着的断言。
VERDICT_PINNED = {P_SELFTEST_LOG["path"], P_GATE_FAMILY_LOG["path"], P_TEETH_BATTERY_LOG["path"]}
P_BATT_R3 = LOG + "R23dedup/run3-16cells-overlayfix/full-run-16cells.log"
P_BATT_R4 = LOG + "R23dedup/run4-t1-literal-fix/t1-cell-run4.log"
P_BATT_R5 = LOG + "R23dedup/run5-16cells-after-t1fix/full-run-16cells.log"
R22_LEDGER = LEDGER_DIR + "R22.jsonl"
LEDGER_R22_SELF = LEDGER_DIR + "R22-protocol.jsonl"
MAIN_SPEC_REL = "docs/superpowers/specs/2026-09-19-browser-automation-write-ledger-design.md"
LOCK_LEG = gleg("./internal/browser_automation/service/",
                "TestSentToFinalLedgerWritePathHasNoEarlyReturn")

# 自检日志里逐字存在的格名，成族列在这里；每条都经 `cells()` 和日志对过账（日志在＝逐个查，
# 不在＝只算数）。下面这些名字一旦和格子改名脱节，生成器当场 SystemExit。
C_LEG8 = cells("腿名点不到", "腿所在包跑不出名单", "腿名是正则", "pkg 越界", "带 pkg 无 name",
               "守卫脚本不存在", "invocation 指不到脚本", "脚本腿 expect 太短")
C_CLAIM7 = cells("quote 非原文", "quote 跨条拼接", "quote 为空", "quote 抄整段正文",
                 "quote 只有 4 字", "section 点不到节", "section 命中两份同名节")
C_EVID_OLD5 = cells("日志不存在", "日志早于 first_seen", "日志在 /tmp", "日志用 .. 逃逸",
                    "日志是空串")
C_EVID_CONTENT6 = cells("日志断言没命中", "日志出现不许出现的串", "logs 字典缺 path",
                        "logs 字典无断言", "must_say 不是数组", "logs 项是数字")
# 收口轮再加的两格，钉的都是"读不到＝判不到"这一类静默放行：断言域要含**尾部**、要**脱色**。
C_EVID_WINDOW2 = cells("禁串只在尾部", "禁串被 ANSI 遮挡")
C_REPRO2 = cells("repro 自由文本", "repro 缺 expect")
C_COVER5 = cells("节未声明归属", "声明的总数不符", "声明不存在的节", "归属状态非法",
                 "重复声明同一节")


# expect 里用反引号标出来、但**不是格名**的东西（形状/字段名/命令），逐个写明豁免理由；
# 名单外又查不到＝当场红，宁可漏跑也不要台账替一格不存在的反向测背书。
NOT_CELL_NAMES = {"--selftest", "logs", "logs[]", "path", "must_say", "must_not_say",
                  "go test -list", "--bootstrap", "--check", "N", "repro", "legs",
                  "expect", "quote", "attack", "status", "cmd", "rc", "pkg", "name"}


def audit_expect_names(items: list[tuple]) -> None:
    """把每行 expect 里反引号标的**格名**逐个拿去和自检日志对账。

    `cells()` 只覆盖"成族列在常量里"的那几处，剩下的行内枚举（"`缺 id`、`重复 id` 两格红"）
    照样会随格子改名漂。这一趟把**全表**扫掉：日志里没有的名字直接 SystemExit，
    于是"格子改了名"从一条静默失效的抄本变成一次生成不过。
    """
    log = selftest_log()
    if log is None:
        return
    bad: list[str] = []
    for iid, _sec, _ordn, _attack, status, payload in items:
        legs = payload[0] if status == "fixed" and isinstance(payload, tuple) else []
        for leg in legs:
            expect = leg.get("expect", "") if isinstance(leg, dict) else ""
            for tok in re.findall(r"`([^`]+)`", expect):
                name = re.sub(r"\s*(红|绿)?\s*✓?\s*$", "", tok).strip()
                if not name or name in NOT_CELL_NAMES or len(name) < 3:
                    continue
                if name not in log:
                    bad.append(f"{iid}: `{tok}`")
    if bad:
        raise SystemExit("以下 expect 里反引号标的名字在 "
                         f"{P_SELFTEST_LOG['path']} 里查不到 ⇒ 台账替不存在的格子背书：\n  "
                         + "\n  ".join(bad))


def closeout_cells() -> str:
    """自检格数从**台账引用的那份日志**末行读出来，不在本脚本里手抄一个数。

    本轮撞到的形状：`expect` 里手写了「末行 50/50 汇总」这种**总格数**，以及逐格行首的
    `[反向 N]` **序号**，之后自检又加了两格 ⇒ 读数与序号全成了抄本。闭合门只核 `claim.quote` 指针与 legs 的形状，
    **核不到 expect 里的数字**——所以这类数一旦写死就只能靠人记得回来改（没人记得）。
    格数改成读日志；序号干脆不写（每格的名字唯一，够定位，少一个会漂移的数）。
    日志不在时退成字面 `N`：那条 leg 的 expect 会带着一个显眼的待填位，而 --bootstrap
    的待跑名单本来就会点名这份文件。P_SELFTEST_LOG 自收口轮起是 `logs[]` 的字典形状
    （要内容断言），所以这里取它的 `path`——读数与断言绑的是同一份文件，不能各指一处。
    """
    global _CELLS
    if _CELLS is None:
        p = ROOT / P_SELFTEST_LOG["path"]
        if not p.is_file():
            _CELLS = "N"
        else:
            m = re.search(r"===== selftest：([1-9]\d*)/\1", p.read_text(encoding="utf-8"))
            if not m:
                raise SystemExit(f"{P_SELFTEST_LOG['path']} 里没有 `===== selftest：N/N 种坏形态` 末行"
                                 "（那不是这门的自测量程读数，别拿它当格数的来源）")
            _CELLS = m.group(1)
    return _CELLS


_CELLS: str | None = None

# (id, 节序号前缀, 节内条目序号, attack, status, 载荷)
# 载荷按 status 取：fixed→(legs, logs|repro)；refuted→reverify 原文；blocked→{"owner","due","next"}
# （blocked 是唯一"本轮没有腿"的终态，也是唯一**不需要**日志的：它把代价写成 owner+due，
#  而 due 一过门就会把它喊红——所以这一格绝不能顺手写成 fixed。）
PROTOCOL_ITEMS: list[tuple] = [
    # ---- §1 承诺轴
    ("P-01", "1.", 1, "把某条台账的 claim.quote 从「原文前 44 字」改成整段正文抄写（>96 字）"
     "⇒ 预言 --selftest 的「quote 抄整段正文」格红",
     "fixed", ([selftest_leg(f"看逐格行 `quote 抄整段正文 红 ✓` 与末行 {closeout_cells()}/{closeout_cells()} 汇总")],
               [P_SELFTEST_LOG])),
    ("P-02", "1.", 2, "删掉任一条目的 attack 字段，或把它写成「a」这种只有形状的一字串"
     "⇒ 「缺 attack」「attack 只有一字」两格各自隔离地红",
     "fixed", ([selftest_leg("逐格行 `attack 只有一字`、`缺 attack` 两格均标 红 ✓")],
               [P_SELFTEST_LOG])),
    ("P-03", "1.", 3, "把某一格的注码截成半个语法块（摘掉整块 if 致使少掉出口 return）"
     "⇒ 电池注码前置判「锚点命中次数≠1 / 注码后 gofmt -e 不可解析」并停机，编译红进不了判据",
     "fixed", ([batt_leg(" --check", "末行印「注码前置：16 格锚点各命中一次 + 注码后语法可解析」，"
                         "任一锚点命中 0 或 >1 次即 SystemExit")], [P_BATT_R5, P_BATT_R3])),
    ("P-04", "1.", 4, "在主 spec 的 §八/§6/§8.3 里新加一条承诺而不加台账行"
     "⇒ R22.jsonl 的「计数钉不符」或「承诺总数实抽」红（本节那串 2026-09-22 的数就是这条钉的读数）",
     "fixed", ([gate_leg(R22_LEDGER, "汇总行 items=N 与 by_status 四格相加相等（N 随台账长，不在此手抄）；"
                            "任一节实抽≠钉值即 rc≠0")], [P_GATE_R22_LOG])),
    ("P-05", "1.", 5, "把某条 claim.section 只填节标题「八、登记（看过、本批不做）」而不带所属批次小节"
     "⇒ 「section 命中两份同名节」红（两份并成一个池子，A 节原文会在 B 节名下匹配成功）",
     "fixed", ([selftest_leg("逐格行 `section 命中两份同名节 红 ✓`（夹具 §8/§9 各一份同名节当靶子）")],
               [P_SELFTEST_LOG])),

    # ---- §2 台账字段（7 行表 + 4 个 status 形状）
    ("P-06", "2.", 4, "删掉某行的 id，或让两条不同发现复用同一个 id"
     "⇒ 「缺 id」「id=… 与第 N 行重复」两格红",
     "fixed", ([selftest_leg("`缺 id`、`重复 id` 各自只红自己那一格")], [P_SELFTEST_LOG])),
    ("P-07", "2.", 5, "把 quote 改写成原文没有的句子、或跨两个条目各切一截拼接、或整段抄正文"
     "⇒ 「quote 非原文」「quote 跨条拼接」「quote 为空」「section 点不到节」「quote 抄整段正文」"
     "「quote 只有 4 字」六格红",
     "fixed", ([selftest_leg(f"claim 指针族 {C_CLAIM7} 全标 红 ✓")], [P_SELFTEST_LOG])),
    ("P-08", "2.", 6, "把 attack 列从字段表里划掉，条目只留 status 与证据"
     "⇒ 「缺 attack」红：审核轴退回「再读一遍找感觉」",
     "fixed", ([selftest_leg("`缺 attack 红 ✓`；摘掉该判据后门照样放行空 attack 的台账")],
               [P_SELFTEST_LOG])),
    ("P-09", "2.", 7, "给一条发现写 status=wontfix，或整格留空"
     "⇒ 「不在四终态 (fixed/refuted/locked-equivalence/blocked) 内」红——没有「待办」这个格子",
     "fixed", ([selftest_leg("`缺 status`、`非法 status` 两格红 ✓")], [P_SELFTEST_LOG])),
    ("P-10", "2.", 8, "把某条 fixed 的 legs 留空、或把 logs 指向 /tmp 下的日志"
     "⇒ 「fixed 必须至少一条腿」「证据在临时目录——重启后没人能复核」红",
     "fixed", ([selftest_leg(f"腿族 {C_LEG8} 与证据族 {C_EVID_OLD5} 逐格红 ✓")], [P_SELFTEST_LOG])),
    ("P-11", "2.", 9, "blocked 条目不写 next（下一步）"
     "⇒ 「blocked 缺 next（下一步要可执行，'以后再说'不算）」红",
     "fixed", ([selftest_leg("`blocked 缺 next 红 ✓`")], [P_SELFTEST_LOG])),
    ("P-12", "2.", 10, "删掉 first_seen，或拿一份比它更早的旧日志当这条的复验证据"
     "⇒ 「缺 first_seen」「证据比 first_seen 还老」「first_seen 在未来」三格红",
     "fixed", ([selftest_leg("`日志早于 first_seen`/`first_seen 在未来`/`缺 first_seen` 三格各自红 ✓")],
               [P_SELFTEST_LOG])),
    ("P-13", "2.", 12, "把某条 Go 腿的 pkg 去掉只留用例名，或把用例名写成正则 `TestSet.*`"
     "⇒ 「带了 pkg 却没 name」「不是合法 Go 用例名」红（正则能在 -list 里命中一堆，却不是点得到的那一条）",
     "fixed", ([selftest_leg(f"腿族逐格红 ✓：{cells('腿名点不到','腿所在包跑不出名单','腿名是正则','pkg 越界','带 pkg 无 name')}"),
                LOCK_LEG], [P_SELFTEST_LOG])),
    ("P-14", "2.", 13, "把某条 refuted 的 reverify 写成「我认为这条不成立」"
     "⇒ 「reverify 里没有动作词……'我认为不成立'不是复验」红",
     "fixed", ([selftest_leg("`refuted 无动作词`、`refuted 空 reverify` 两格红 ✓")],
               [P_SELFTEST_LOG])),
    ("P-15", "2.", 14, "把 lock_leg 指向一个从未写过的用例名，或不写 lock_attack"
     "⇒ 「要带 lock_leg」「锁腿要配一条行为不变的注码证明它有牙」红；名字真存在才过得了 go test -list",
     "fixed", ([selftest_leg("`等价类无锁腿`、`等价类锁腿点不到` 两格红 ✓"), LOCK_LEG],
               [P_SELFTEST_LOG])),
    ("P-16", "2.", 15, "把 blocked 的 due 写成 2020-01-01（或删掉 owner）"
     "⇒ 「due 已过期，本条算遗留未闭合」「缺 owner」「缺 due」红——过期阻塞不能靠「是别人的事」存活",
     "fixed", ([selftest_leg("`blocked 已过期`、`blocked 缺 owner/due` 红 ✓"),
                gate_leg(R22_LEDGER, "R22-31/R22-34 两条真 blocked 条目带 owner/due=2026-10-31，"
                                     "门对整份台账 rc=0")], [P_SELFTEST_LOG, P_GATE_R22_LOG])),

    # ---- §3 九条判据（+3 段散文承诺）
    ("P-17", "3.", 2, "给一条台账行写 status=TODO 或不写 status ⇒ 判据 1 红",
     "fixed", ([selftest_leg("第 1 格族：`缺 status`/`非法 status` 各红自己那格，不混进对账红")],
               [P_SELFTEST_LOG])),
    ("P-18", "3.", 3, "把一条 fixed 的 Go 腿换成不存在的用例名；再把一条脚本腿的 invocation 指到别的脚本"
     "⇒ 判据 2 的两种形状各红一次（go test -list 点不到 / invocation 里没有出现该路径）",
     "fixed", ([selftest_leg(f"腿族 {C_LEG8} 全红 ✓")], [P_SELFTEST_LOG])),
    ("P-19", "3.", 4, "把 logs 换成 `/tmp/whatever.log`，或把 repro 写成一句自由文本"
     "⇒ 判据 3 红：不在仓里的东西不叫证据；repro 要 {cmd, expect}。"
     "再加一型（2026-09-23 实测撞到的）：证据文件**在、也新**，内容却是另一趟跑的读数"
     "（merge-gate 的 `--only` 子集调试把台账引用的全量取证原地覆盖）⇒ 判据 3 的"
     "内容断言支红：`logs[]` 的字典项要写 `must_say`/`must_not_say`，命中与否直接对读数核",
     "fixed", ([selftest_leg(f"证据族逐格红 ✓：{C_EVID_OLD5}＋{C_REPRO2}＋本轮新增的内容断言支 {C_EVID_CONTENT6}"
                        f"＋两条「读不到＝判不到」的界 {C_EVID_WINDOW2}")],
               [P_SELFTEST_LOG])),
    ("P-20", "3.", 5, "把 reverify 留空 ⇒ 判据 4 红「refuted 必须写 reverify——刀怎么注的、看到什么读数」",
     "fixed", ([selftest_leg("`refuted 空 reverify 红 ✓`")], [P_SELFTEST_LOG])),
    ("P-21", "3.", 6, "把 lock_leg 的用例名写错一个字母 ⇒ 判据 5 红（go test -list 在**它所属的那个包**里点不到）",
     "fixed", ([selftest_leg("`等价类锁腿点不到 红 ✓`"), LOCK_LEG], [P_SELFTEST_LOG])),
    ("P-22", "3.", 7, "blocked 少 owner、少 due，或 due 一过 ⇒ 判据 6 红（三种缺法各种一格）",
     "fixed", ([selftest_leg(f"{cells('blocked 已过期','blocked 缺 next','blocked 缺 owner/due')} 各自红 ✓")], [P_SELFTEST_LOG])),
    ("P-23", "3.", 8, "改 spec 标题让钉点不到；或往该节加一条承诺而不改钉值；或把 prose 豁免的 reason 删掉"
     "⇒ 判据 7 的三条支各红一次（一条都没命中 / 计数钉不符 / prose-not-counted 要带 reason）",
     "fixed", ([selftest_leg("`钉的节名点不到`/`计数钉不符`/`prose 豁免无 reason`/`钉已验但没抽轴` 四格红 ✓")],
               [P_SELFTEST_LOG])),
    ("P-24", "3.", 9, "把某条 quote 换成 spec 里没有的句子 ⇒ 判据 8 红「找不到连续原文」；"
     "换成 >96 字的正文抄本 ⇒ 同判据的指针上界支红",
     "fixed", ([selftest_leg(f"claim 指针族 {C_CLAIM7} 红 ✓"
                            "（`quote 抄整段正文`＝>96 字上界，`quote 只有 4 字`＝<12 字下界，"
                            "短到在同一节里必然撞车＝没点名是哪条承诺）")],
               [P_SELFTEST_LOG])),
    ("P-25", "3.", 10, "整节不写进 meta.coverage；或声明 status=ledger 却不补它的 scope 钉；"
     "或把声明的 total 改成别的数 ⇒ 判据 9 的三条支各红一次",
     "fixed", ([selftest_leg(f"coverage 族 {C_COVER5}＋本轮新加的 `声明 ledger 却没有计数钉` 全红 ✓")],
               [P_SELFTEST_LOG])),
    ("P-26", "3.", 11, "让门去连库或跑用例（把 pkg_names 的 `-list` 换成不带过滤的 go test）"
     "⇒ 与并行 lane 同树时门会互踩、且慢到没人愿意跑第二次；自洽性由「私有 --shared 克隆里 rc=0」那趟证",
     "fixed", ([gate_leg(R22_LEDGER, "门只读文本 + `go test -list`：不打印任何 DB 连接信息、"
                            "逐包只编译一次；克隆那趟 rc=0 且汇总行条目数与工作树一致")],
               [P_CLONE_LOG, P_GATE_R22_LOG])),
    ("P-27", "3.", 12, "把 check_claim 整个换成 pass（摘掉一条判据）"
     f"⇒ --selftest 的分子立刻掉到 {closeout_cells()} 以下（本轮实测：早先摘一次得 11/13），装回去补满、还原后 md5 与摘除前一致",
     "fixed", ([selftest_leg(f"末行 `===== selftest：{closeout_cells()}/{closeout_cells()} 种坏形态各自隔离地红，"
                          "合法快照 0 红 =====`，且合法快照那行也印 0 红 ✓")], [P_SELFTEST_LOG])),
    ("P-28", "3.", 13, "只看「报错文本里出现关键字」而不判 stray"
     "⇒ 一格带两条别格的红也算过，判据其实没在管事；门对每格断言 own and not stray",
     "fixed", ([selftest_leg("每格尾标必须是「红 ✓」；出现「混进别格的红 ✗ <首条红因>」即不计入 reds "
                            "（本轮实测：新加判据时一次泄漏 8 格，读作 47/49）")], [P_SELFTEST_LOG])),
    ("P-41", "3.", 14, "把「门自己必须有反向测试」这条只管闭合门、不管另外几份门禁件"
     "⇒ 注码：把 merge-gate 的 tally 改回 `name in DIAG and '红' or '绿'` 那种内联式、"
     "或把 clone 的 clone_agrees 里 `bool(sum_cl)` 那半边摘掉（两侧同空即算一致）⇒ 各自 --selftest 掉分子；"
     "或把汇编件的成员名单删一位而汇总行照印「份数够」⇒ 它自己的「每位成员都要在取证里现形」那格红"
     "（本轮实测撞到的形状：那页取证记「三份」而成员只列了三家，第四家＝汇编件自己从没被测过）；"
     "而「摘掉判据 ⇒ 反向格必须不再红」这一格本身由门牙电池判（刀数不手抄，看末行 k/k），锚点不唯一＝判错不是跳过",
     "fixed", ([sleg("scripts/merge-gate.py", "python3 scripts/merge-gate.py --selftest",
                     "末行「merge-gate --selftest：k/k 种坏读数各自拦停/判红，绿读数不误伤」"
                     "（k 随探针谱长，不在此手抄；证据日志那侧的断言是「无一格判错」）"),
                sleg("scripts/run-gate-in-clone.py", "python3 scripts/run-gate-in-clone.py --selftest",
                     "末行「run-gate-in-clone --selftest：k/k 格判对」，且「两侧都零输出要红」那格开火"),
                sleg("scripts/run-gate-family-selftests.py",
                     "python3 scripts/run-gate-family-selftests.py --selftest",
                     "末行「gate-family-selftests --selftest：k/k 格判对」，且"
                     "「每位成员都要在取证里现形（少一位＝证据少一格）」那格开火；k＝成员数，不在此手抄"),
                sleg("scripts/reverse-test-gate-teeth.py",
                     "python3 scripts/reverse-test-gate-teeth.py",
                     "末行「门牙反向测：k/k 刀各自点名到该开火的那一格」，且没有任何一格印"
                     "「没开火＝该格无牙」；每刀还原后按 md5 复核")],
               [P_GATE_FAMILY_LOG, P_TEETH_BATTERY_LOG])),

    # ---- §6 本轮收口面
    ("P-29", "6.", 1, "把两份同名「八、登记」的其中三条合并记成一条，或整份不抽轴"
     "⇒ 两条 bullet 钉各 3 条，少一条即「节…的计数钉已验 N 条承诺，台账里只有 M 条」红",
     "fixed", ([gate_leg(R22_LEDGER, "scope 里 7.24/7.28 两份「八、登记」钉各 count=3；"
                            "汇总行 rc=0（条目总数随台账长，不在此手抄）")], [P_GATE_R22_LOG])),
    ("P-30", "6.", 2, "把 §6 四项里落在并行 lane 在途文件上的那条直接代偿改掉（或记成 fixed）"
     "⇒ 判据 6 只认带 owner/due/next 的 blocked；代偿＝改别人的期望，本轮 §5 纪律禁止",
     "fixed", ([gate_leg(R22_LEDGER, "§6 钉 numbered count=4 且四条抽轴齐；blocked 条目 due 未过期")],
               [P_GATE_R22_LOG])),
    ("P-31", "6.", 3, "把 §8.3 的 21 行整表划成 out-of-scope（不再逐行判）"
     "⇒ 该节 tablerow 钉 count=21 与实际抽轴数对不上即红；声明 ledger 却不钉则踩「没有它已验的计数钉」",
     "fixed", ([gate_leg(R22_LEDGER, "scope 里 §8 的 tablerow 钉 count=21；台账指向该节的条目数 ≥21")],
               [P_GATE_R22_LOG])),
    ("P-32", "6.", 4, "把「越界三条红」按立项时那份报告原样入库（两处 MD004 + 一处 MD031）"
     "⇒ 复跑 markdownlint-cli2 走仓里 .markdownlint-cli2.jsonc 的 glob，实测 0 issue 且 MD031 是关掉的；"
     "台账里这条只能记 refuted 并写下这个读数",
     "fixed", ([gate_leg(R22_LEDGER, "该条 reverify 非空且含动作词（grep/实测/复跑 一族），rc=0")],
               [P_GATE_R22_LOG])),
    ("P-33", "6.", 5, "把 #28 真机腿写成 fixed（「已覆盖」）"
     "⇒ 门不会拦这句话，但合并门禁里没有一条真机腿；协议 §2 的 blocked 形状要求 owner/due/next 齐备，"
     "缺任一格即红，写成 fixed 就是撒谎",
     "fixed", ([gate_leg(R22_LEDGER, "R22-31 为 blocked：owner=用户（要本机宿主与真机设备）、"
                            "due=2026-10-31、next 写明「用户接上宿主后跑 scripts/e2e_browser_real.py "
                            "的伪造域名档」——三条真机腿全在别人的设备上，本轮造不出腿")],
               [P_GATE_R22_LOG])),
    ("P-34", "6.", 6, "在本泳道偷偷 commit/push 那十二批未提交改动"
     "⇒ 门读不到任何仓内证据能拦它（工作树状态不在台账判据里）；本轮的收口形状＝该条记 blocked，"
     "owner=并行 task #59 + 用户真机回归，本泳道全程零 commit（git status 里我的改动仍在树里）",
     "blocked", {"owner": "并行 task #59（提交/推送归它）+ 用户（真机回归是提交前置）",
                 "due": "2026-10-31",
                 "next": "用户真机回归通过后由 task #59 逐 hunk `git add` 显式路径提交并推送双远端；"
                         "本泳道交出的只有工作树与 logs/ 里的取证，"
                         "复现判据：`git status --porcelain -- scripts/ "
                         "docs/superpowers/specs/2026-09-22-second-review-protocol-design.md` "
                         "仍全部为 M/??（不写「HEAD 与开工一致」：其它 lane 一直在提交，"
                         "HEAD 会漂，那条判据会在没人替我 commit 时也自己变红）"}),
    # §6#7 与 §8#5 是真没有执行者的两条：如实记 refuted，不造腿。
    ("P-35", "6.", 7, "把主 spec §九 里的「无执行者清单」整段删掉",
     "refuted", "注码复跑：删掉那一节后 `python3 scripts/check-review-closeout.py docs/superpowers/specs/"
     "ledger/R22.jsonl` 仍 rc=0，全仓 grep「无执行者」只命中 spec 正文与台账本身 ⇒ 这条承诺没有任何仓内"
     "判据读它，实测读数如上；落点仍是主 spec §九（人读），协议 §7 第 5 条明令本轮不把四十条口径全改写成脚本"),

    # ---- §8 验收判据
    ("P-36", "8.", 1, "让门带着一条缺证据的台账跑 ⇒ 它必须 rc≠0；本轮两份台账都要 rc=0",
     "fixed", ([gate_leg(R22_LEDGER, "汇总行「闭合：… rc=0」（R22 台账）"),
                gate_leg(LEDGER_R22_SELF, "汇总行 rc=0（本协议自身台账）")],
               [P_GATE_R22_LOG, P_GATE_SELF_LOG])),
    ("P-37", "8.", 2, "把 --selftest 里任一种坏形态的判据摘掉，或只跑合法快照那一侧"
     "⇒ 汇总行的分子不再等于格数；合法快照若非 0 红＝门在假红",
     "fixed", ([selftest_leg(f"两侧都要出现：`{closeout_cells()}/{closeout_cells()} 种坏形态各自隔离地红` "
                            "+ `合法快照 0 红`，整行 rc=0")], [P_SELFTEST_LOG])),
    ("P-38", "8.", 3, "用 `-run` 过滤一个子集冒充全量门禁（本轮 §0.1 记的就是这类假绿）"
     "⇒ 合并门禁那趟必须不带 -run：go build ./... && go vet ./... && go test ./internal/... 全量；"
     "markdownlint / gofmt / check-architecture.sh 三条越界红在同趟复量",
     "fixed", ([gate_leg(R22_LEDGER, "同趟日志末尾门 rc=0；§6 那三条越界红不再复现")],
               [P_MERGE_LOG, P_GATE_R22_LOG],
               {"cmd": "cd user-server && go build ./... && go vet ./... && "
                       "go test ./internal/... -count=1 -timeout 40m",
                "expect": "全部包 ok，无 `--- FAIL`、无 `[build failed]`；读数以 merge-gate-full.log "
                          "全文为准（不带任何 -run 过滤）"})),
    ("P-39", "8.", 4, "把某条 blocked 的 due 改成昨天，或某条 refuted 的 reverify 改成「我认为不成立」"
     "⇒ 判据 6 / 判据 4 各红一次（这两格本轮真被自检种过）",
     "fixed", ([gate_leg(R22_LEDGER, "rc=0 且 by_status 里 blocked 的每条都带 owner/due/next")],
               [P_GATE_R22_LOG])),
    ("P-40", "8.", 5, "不往主 spec 追加「§九 二次审核协议落地实证」这一节",
     "refuted", "注码复跑：把 §九 的标题从主 spec 里摘掉（内存里改、不落盘）后复跑闭合门，"
     "R22.jsonl 仍 rc=0——主 spec 的 coverage 钉只数「条目形」承诺，一节散文的缺失不会红 ⇒ 这条验收判据"
     "没有仓内执行者，靠人读 §九；本轮以 §九 实跑读数（含被我否决的条目）为其唯一证据"),
]


PROTOCOL_LEDGER_SECS = ("1.", "2.", "3.", "6.", "8.")

# 不抽进本台账的节，各自写明为什么（判据 9：out-of-scope 也要钉总数并写理由，静默划出去＝盲区）。
PROTOCOL_OOS = {
    "0.": "三条是**诊断与实证**（本轮跑出来为什么必须换做法），不是可指认的承诺；"
          "它们的承接面就是本协议 §1–§3 本身，那三节的每一条承诺在本台账里有行",
    "4.": "A–E 五相是**流程纪律**（谁在什么相位做什么），仓内没有任何判据读它——"
          "注码进流程里不会让任何腿红；其执行者是本轮 A–E 的实跑日志与主 spec §九 的逐相读数",
    "5.": "越界改动纪律约束的是**我改别人文件的手法**（单行精确匹配、改前读改后核 md5、产出清单），"
          "本轮不 commit ⇒ 没有任何仓内判据能读到它；落点＝主 spec §九 的「我动了你哪一行」清单",
    "7.": "五条是**范围声明（明确不做）**，不是发现；其中唯一有机器执行者的一条"
          "（不把承诺正文搬进台账）已由 §3 判据 8 的 96 字指针上界收口，见本台账 P-01/P-24 两行",
}


def resolve_title(titles: list[str], prefix: str) -> str:
    hits = [t for t in titles if t.startswith(prefix)]
    if len(hits) != 1:
        raise SystemExit(f"节号前缀 {prefix!r} 命中 {len(hits)} 个 ## 节（要唯一才敢自动填标题）")
    return hits[0]


def build_protocol(spec_rel: str, chars: int) -> list[dict]:
    audit_expect_names(PROTOCOL_ITEMS)
    frags = fragments(spec_rel)
    titles = list(dict.fromkeys(s for s, _, _ in frags))
    by_key = {(s, i): gate.norm(e) for s, i, e in frags}
    rows = [{"kind": "meta", "round": "R22-protocol", "spec": spec_rel,
             **pins_and_coverage(spec_rel,
                                 {resolve_title(titles, p) for p in PROTOCOL_LEDGER_SECS},
                                 {resolve_title(titles, k): v for k, v in PROTOCOL_OOS.items()})}]
    for iid, sec_pref, ordn, attack, status, payload in PROTOCOL_ITEMS:
        sec = resolve_title(titles, sec_pref)
        flat = by_key.get((sec, ordn))
        if not flat:
            raise SystemExit(f"{iid}：节「{sec[:20]}」第 {ordn} 条抽不出原文（条目序号漂了，要重数）")
        it = {"kind": "item", "id": iid,
              "claim": {"section": sec, "quote": flat[:chars]},
              "attack": attack, "status": status, "first_seen": "2026-09-22"}
        if status == "fixed":
            legs, rest = payload[0], payload[1]
            ev = {"legs": legs}
            if isinstance(rest, list):
                # logs 项允许两种形状：纯路径字符串，或 {path, must_say, must_not_say} 字典。
                # 存在性检查对字典只核 path 在不在，**内容断言由闭合门读文件兑现**——
                # 生成器不重复实现一遍判据，否则"两份真相"迟早对不上（协议 §0.3 记的那类失效）。
                def _lp(p):
                    return p["path"] if isinstance(p, dict) else str(p)
                missing = [_lp(p) for p in rest if not (ROOT / _lp(p)).is_file()]
                if missing:
                    if not BOOTSTRAP:
                        raise SystemExit(f"{iid}：证据日志还不存在，先跑出来再入账：{' '.join(missing)}")
                    for p in missing:
                        if p not in PENDING:
                            PENDING.append(p)
                ev["logs"] = rest
            else:
                ev["repro"] = rest
            if len(payload) > 2:
                ev["repro"] = payload[2]
            it["evidence"] = ev
        elif status == "refuted":
            it["evidence"] = {"reverify": payload}
        elif status == "blocked":
            # blocked 的形状＝owner/due 进 evidence、next 进条目（与 R22.jsonl 里那两条真 blocked 同形，
            # 门读的就是这两个位置）。它**不需要腿**：本轮造不出腿正是它记 blocked 的理由。
            lack = {"owner", "due", "next"} - set(payload)
            if lack:
                raise SystemExit(f"{iid}：blocked 缺 {'/'.join(sorted(lack))}（判据 6 三格缺一即红）")
            it["evidence"] = {k: payload[k] for k in ("owner", "due")}
            it["next"] = payload["next"]
        else:
            raise SystemExit(f"{iid}：status={status!r} 不是生成器认识的三种载荷形状之一")
        rows.append(it)
    return rows


def build_r22(spec_rel: str, chars: int) -> list[dict]:
    """收口面台账（38 条）：条目表、现测计数钉与 coverage 归属都住在 scripts/ledger_r22.py。

    为什么拆两个文件：那份表按**节**分六族（§8.3 的 21 行、§6 四项、两份同名「八、登记」、
    §7.29 四条、§7.30 三条），每族的切片器不同（表格取列、编号列表取第 n 项、同名节要选第几份），
    和本协议台账那份"一节一条"的扁平表不是一种形状；合进一份文件会把两套切片口径搅在一起，
    反而看不出哪条 quote 走的哪条路。
    """
    return load_sibling("ledger_r22").build(spec_rel, chars)


ROUNDS: dict[str, dict] = {
    "r22": {
        "spec": "docs/superpowers/specs/2026-09-19-browser-automation-write-ledger-design.md",
        "build": build_r22,
    },
    "r22-protocol": {
        "spec": "docs/superpowers/specs/2026-09-22-second-review-protocol-design.md",
        "build": build_protocol,
    },
}

if __name__ == "__main__":
    sys.exit(main())
