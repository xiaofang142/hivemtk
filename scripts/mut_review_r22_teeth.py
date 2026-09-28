#!/usr/bin/env python3
"""R22 收口电池：给「从来没有变异格罩着」的四类承诺补刀（§8.3 无牙行的复验）。

为什么是这四类（其余批次各有常驻电池，见 `ls scripts/mut_*.py`）：
- 批20 D7 审批闭环三件事（A5 放行绑载荷 / A9 收口落 judge 帧 / A10 挂起可跨进程查证）
  只有断言、没有反证——写完用例就再没人问过「把这条判据拆掉，哪条腿会红」。
- 批15 入站重复嗅探（`IsDuplicateReason` 只认结论前缀）同理：它的决定权是「客户端从此
  不再上报这条 event_id」，一次误判＝一条消息永久消失，而这种判据恰恰最容易在重构里
  从 `HasPrefix` 悄悄退化成 `Contains`（两者在测试文案上都对，只在真文案上分开）。

七刀与各自预测的红腿（**先推演后跑**；跑完红因逐条读，名单不符按 BROKEN 处理）：
- D1 放行不比对载荷（`!=` 短路）：TestSignalConfirmBindsPayload（直测）、
  TestConfirmGateFramesBindPayloadAndOmitBody（错哈希那一步 Fatal）、
  TestConfirmStatusDistinguishesGateElsewhere（错载荷/空载荷两半）、
  TestGatePayloadMustNeverBeEmpty（空载荷放行天然落不等式，拆了不等式＝空白支票回来）。
  预测四条而不是「只红直测那条」：这条判据在四处被消费，红开四处正是它 load-bearing 的证据。
- D2 judge 帧改名（A9 留痕契约）：按 Action 精确取的三条里，取不到 d7_confirm 的那些红。
  写侧读侧都用常量 ⇒ 改名对**新写的帧**自洽，破的是「已在库里的旧帧 + 按名字取的审计面」，
  测试里的字面量正是那一份旧帧（这就是它该红的理由，不是它太挑剔）。
- D2b 正文泄进 d7_wait 帧（I5 导出面）：只应红在"审计帧零正文"那条上。
- D3 wait 帧改名（A10 跨进程查证）：服务层与控制器层各自种子了一份字面量旧帧 ⇒ 两层同红。
- D4 摘掉空载荷前置 panic：闸门照样开得出来，只是任何人都能背书；红在就地炸那条。
  这一刀会让该用例真等满 1 分钟预算（没炸就被放行方缺席拖到超时），是本电池最慢的一格。
- P1 嗅探从"前缀"退化成"子串"：红在 wantFalse 那半（落库失败原文天然带 duplicate）。
- P2 删掉「方向冲突」这条结论短语：红在 wantTrue 那半（客户端会每轮重报同一条）。
  P1/P2 同打一条用例名是**设计如此**：一个函数两面，区分靠红因（"不应判重复" vs "应判重复"），
  日志里逐字留着。

口径（沿用本仓电池规矩）：
- 三个包各设控制组，必须 rc==0、settled==期望、skip==0、无红名；每格断言三包
  `settled == 各自控制组 settled`（一条用例 panic 会带走整个二进制）；
- 判据取三包红名的**并集恰好等于期望**——多一条是连带面（读红因定性），少一条是判据没牙；
- BUILD FAILED / panic / 红而没点名 一律 BROKEN，不计入杀掉；红必须读红因；
- 锚点命中恰好一次（`--check` 先验，不建克隆不跑用例）；只在私有 `--shared` 克隆里注码
  （共享工作树与并行会话同树），还原后逐文件比 md5；
- 逐格原始输出落到 `docs/superpowers/specs/ledger/logs/<轮>/`，且**每轮换一个新目录**
  （旧目录里的 `bak/` 会在下一格之前把产码退回上一轮）。

用法：
    python3 scripts/mut_review_r22_teeth.py --check          # 只验锚点
    python3 scripts/mut_review_r22_teeth.py --logs docs/superpowers/specs/ledger/logs/R25
    python3 scripts/mut_review_r22_teeth.py --cells D1,P1 --logs <新目录>
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from mut_dispose import dispose, dispose_at_exit, leave_for_evidence, workdir

ROOT = Path(__file__).resolve().parent.parent
US = "user-server"
EXEC_REL = f"{US}/internal/browser_automation/service/executor.go"
PROTO_REL = f"{US}/internal/channelgw/protocol.go"
LANE_PATHS = [f"{US}/internal"]
DEFAULT_LOGS = "docs/superpowers/specs/ledger/logs/R22-teeth"
ANSI = re.compile(r"\x1b\[[0-9;]*m")

PKG_GATE = "./internal/browser_automation/service/"
PKG_CTRL = "./internal/browser_automation/controller/"
PKG_GW = "./internal/channelgw/"
# 三条 D7 断言分散在 service / controller 两包，嗅探两条在 channelgw；过滤器只罩住
# 本电池判据真正住着的用例家族（-run 是子集口径，全量门禁另跑）。
R_GATE = ("TestSignalConfirmBindsPayload|TestPendingGateExposesAwaitingPayload|"
          "TestConfirmGateFramesBindPayloadAndOmitBody|TestConfirmGateDecisionPerOutgoing|"
          "TestConfirmStatusDistinguishesGateElsewhere|TestConfirmGateReadIsOwnershipScoped|"
          "TestGatePayloadMustNeverBeEmpty")
R_CTRL = "TestB20Confirm"
R_GW = "TestIsDuplicateReason"

# 断言腿名（与 _test.go 里的函数名一字不差，红集合按名比对）
L_BIND = "TestSignalConfirmBindsPayload"
L_PENDING = "TestPendingGateExposesAwaitingPayload"
L_FRAMES = "TestConfirmGateFramesBindPayloadAndOmitBody"
L_DECIDE = "TestConfirmGateDecisionPerOutgoing"
L_STATUS = "TestConfirmStatusDistinguishesGateElsewhere"
L_OWN = "TestConfirmGateReadIsOwnershipScoped"
L_EMPTYGATE = "TestGatePayloadMustNeverBeEmpty"
C_ELSEWHERE = "TestB20ConfirmGateElsewhereFromAuditFrame"
G_DUP_CN = "TestIsDuplicateReason_只认重复结论短语不认任意子串"
G_DUP_EN = "TestIsDuplicateReason"


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


# ---------------------------------------------------------------- 七刀
# 载荷比对（executor.go SignalConfirm）
CMP = "\tif payloadHash != g.payloadHash {\n\t\tg.mu.Lock()"
CMP_CUT = "\tif payloadHash != g.payloadHash && false {\n\t\tg.mu.Lock()"
# 两类审计帧的动作名
WAIT_FRAME = '\tgateWaitFrame    = "d7_wait"    //'
WAIT_FRAME_CUT = '\tgateWaitFrame    = "d7_gate_wait"    //'
CONF_FRAME = '\tgateConfirmFrame = "d7_confirm" //'
CONF_FRAME_CUT = '\tgateConfirmFrame = "d7_judged" //'
# d7_wait 帧的载荷字段表（正文泄进来的形状：审计面上最好看的「让人看清批的是什么」）
WAIT_MAP = ('\te.appendCommandLog(ctx, session.ID, task.ID, stepID, *seq, "event", gateWaitFrame, '
            'map[string]any{\n\t\t"payload_hash": payloadHash, "step_index": stepIndex,')
WAIT_MAP_CUT = ('\te.appendCommandLog(ctx, session.ID, task.ID, stepID, *seq, "event", gateWaitFrame, '
                'map[string]any{\n\t\t"payload_hash": payloadHash, "step_index": stepIndex,\n\t\t'
                '"preview": gate.preview,')
# 空载荷前置
PANIC_GUARD = '\tif payloadHash == "" {\n\t\tpanic("awaitConfirmGate: 空载荷哈希不得开闸门")\n\t}'
PANIC_GUARD_CUT = '\tif payloadHash == "" && false {\n\t\tpanic("awaitConfirmGate: 空载荷哈希不得开闸门")\n\t}'
# 嗅探判据
PREFIX_CALL = "\t\tif strings.HasPrefix(reason, prefix) {"
CONTAINS_CALL = "\t\tif strings.Contains(reason, prefix) {"
DIRECTION_PREFIX = '\t"msg_id exists with different direction",\n'


def cuts():
    """七刀。(code, desc, rel, old, new, 期望红名并集)"""
    return [
        ("D1", "放行不比对载荷（批了 A 却放走 B）", EXEC_REL, CMP, CMP_CUT,
         {L_BIND, L_FRAMES, L_STATUS, L_EMPTYGATE}),
        ("D2", "judge 帧改名（A9 放行留痕从审计流里消失）", EXEC_REL, CONF_FRAME, CONF_FRAME_CUT,
         {L_FRAMES, L_DECIDE}),
        ("D2b", "评论正文泄进 d7_wait 帧（I5 导出带走离线件）", EXEC_REL, WAIT_MAP, WAIT_MAP_CUT,
         {L_FRAMES}),
        ("D3", "wait 帧改名（A10 跨进程查证读不到已落库的帧）", EXEC_REL, WAIT_FRAME, WAIT_FRAME_CUT,
         {L_FRAMES, L_STATUS, C_ELSEWHERE}),
        ("D4", "摘掉空载荷前置（开一扇谁都能背书的匿名放行口）", EXEC_REL, PANIC_GUARD, PANIC_GUARD_CUT,
         {L_EMPTYGATE}),
        ("P1", "重复嗅探从前缀退化成子串（DB 失败原文被判成已存过）", PROTO_REL,
         PREFIX_CALL, CONTAINS_CALL, {G_DUP_CN}),
        ("P2", "删掉「方向冲突」结论短语（客户端每轮重报同一条）", PROTO_REL,
         DIRECTION_PREFIX, "", {G_DUP_CN}),
    ]


# ---------------------------------------------------------------- 克隆
def lane_overlays() -> tuple[list[str], list[str]]:
    """(要覆盖进克隆的脏文件, 要在克隆里删掉的文件)——同 mut_hub_media_backfill.py。

    删除必须搬：`--shared` 克隆 = HEAD + 覆盖，工作树里一条 ` D` 不搬，克隆就在跑一份
    "文件还在"的树；本泳道实测过 `license_checker.go` 这一类。
    """
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到脏文件清单：" + r.stderr[-200:])
    mods, dels = [], []
    for line in r.stdout.splitlines():
        st = line[:2]
        p = line[3:].split(" -> ")[-1].strip().strip('"')
        (dels if "D" in st else mods).append(p)
    if not mods:
        raise SystemExit("脏文件清单为空——克隆里跑的是 HEAD，测不到本批改动（宁可停机也别假绿）")
    return mods, dels


def prepare(dst: Path, owned: bool = False) -> Path:

    def bail(msg: str) -> None:
        """克隆已经建起来之后的中止路：先回收私有克隆，再出声。

        收尾闸原先只接在 `main()` 的出口上，装架函数里克隆之后的每一条 raise 都把整份
        私有克隆留在临时目录（一轮 50–70MB，而磁盘常态 98% 满）。2026-09-28 在
        `mut_bill_p701.py` 上实测一次 DIRTY 停机留 72M，这一族按同一形状补齐。
        三条**不**走这里："已存在"（那份 clone/ 不是本电池建的）、"克隆失败"（目录归属
        还没定）、"md5 不一致"（"覆盖后还是不对"的字节只活在克隆里，删了就只剩一句
        "当时红过"——与 `main()` 侧还原校验同一取舍）。
        """
        if (dst / "clone").exists():
            dispose(dst, owned=owned, keep=False, repo_root=ROOT)
        raise SystemExit(msg)
    clone = dst / "clone"
    if clone.exists():
        raise SystemExit(f"{clone} 已存在（换 --clone 目录或先删）")
    r = subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(clone)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("克隆失败：" + (r.stdout + r.stderr)[-400:])
    b = subprocess.run(["git", "checkout", "-f", "master"], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if b.returncode != 0:
        bail("checkout 失败：" + (b.stdout + b.stderr)[-400:])
    mods, dels = lane_overlays()
    for rel in mods:
        src = ROOT / rel
        if not src.exists():
            bail(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
    for rel in dels:
        (clone / rel).unlink(missing_ok=True)
    print(f"覆盖 {len(mods)} 个脏文件、同步 {len(dels)} 个删除进克隆")
    head = subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--short", "HEAD"],
                          capture_output=True, text=True).stdout.strip()
    print(f"基线 HEAD={head or '?'}（活树红可能只是并行会话的瞬时态，读数只在写明的这一版上成立）")
    hostenv = ROOT / US / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / US / ".env")
    return clone


def test_env(clone: Path) -> dict:
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-r22teeth")
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    envf = clone / US / ".env"
    if envf.exists() and "POSTGRES_TEST_PASSWORD" not in os.environ:
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD="):
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
                break
    return env


PKGS = ((PKG_GATE, R_GATE), (PKG_CTRL, R_CTRL), (PKG_GW, R_GW))
# 日志文件名要短且不含 `/`：pkg 路径直接当标签会写出「control_internal/browser_.../service.log」
# 这种带不存在目录的路径，第一条控制组就崩在 write_text 上。
LABELS = {PKG_GATE: "gate", PKG_CTRL: "ctrl", PKG_GW: "channelgw"}


def run_test(clone: Path, pkg: str, filt: str, env: dict) -> dict:
    p = subprocess.run(["go", "test", pkg, "-run", filt, "-count=1", "-v", "-timeout", "600s"],
                       cwd=clone / US, capture_output=True, text=True, timeout=900, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    top = lambda kind: len(re.findall(rf"^--- {kind}: ", out, re.M))
    redtop = sorted({m.split("/")[0] for m in re.findall(r"^--- FAIL: (\S+)", out, re.M)})
    return {"rc": p.returncode, "out": out,
            "settled": top("PASS") + top("FAIL") + top("SKIP"),
            "skipped": top("SKIP"), "red": redtop,
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "undefined:" in out or "declared and not used" in out}


def causes(out: str) -> list[str]:
    keep = [l.strip()[:220] for l in out.splitlines()
            if re.match(r"^\s{2,}\S+\.go:\d+:", l) or "panic:" in l or "Error Test" in l]
    return keep[:10]


# ---------------------------------------------------------------- 锚点自检
def check_anchors() -> int:
    """每刀锚点在**工作树**源文件里命中恰好一次；命中数不是 1 就地报，不建克隆。"""
    srcs = {rel: read(ROOT / rel) for rel in {EXEC_REL, PROTO_REL}}
    bad = 0
    for code, desc, rel, old, new, _ in cuts():
        n = srcs[rel].count(old)
        # 「残留」只对替换/删除式有意义：改名式变异若仍含原锚点＝这刀根本没落地。
        # 插入式（new 以 old 开头）天然保留锚点，报不适用，别让它看着像失效变异。
        if new.startswith(old):
            leftover = "插入式，不适用"
        else:
            leftover = f"{srcs[rel].replace(old, new, 1).count(old)}（要 0）" if n == 1 else "—"
        hit = "OK " if n == 1 else "坏 "
        if n != 1:
            bad += 1
        print(f"{hit}{code:<4} 锚点命中 {n} 次 · {desc[:40]} · 变异后锚点残留 {leftover}")
    print(("锚点全部唯一，可以跑" if bad == 0 else f"{bad} 刀的锚点不唯一——先修锚点再跑"))
    return 1 if bad else 0


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true", help="只验锚点，不建克隆、不跑用例")
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    ap.add_argument("--logs", default=DEFAULT_LOGS)
    ap.add_argument("--cells", default="", help="逗号分隔格名；终态会写明本趟是子集")
    args = ap.parse_args()
    if args.check:
        return check_anchors()

    only = {c.strip() for c in args.cells.split(",") if c.strip()}
    unknown = only - {c[0] for c in cuts()}
    if only and unknown:
        raise SystemExit(f"--cells 里有不存在的格：{sorted(unknown)}")

    logs = ROOT / args.logs
    if logs.exists() and any(logs.glob("*.log")):
        raise SystemExit(f"逐格日志目录 {args.logs} 里已有上一轮的 .log——换目录："
                         "复用旧目录会让下一格读到上一轮的产码，红的归因就不成立了")
    logs.mkdir(parents=True, exist_ok=True)
    tmp, owned = workdir(args.clone or None, prefix="r22teeth-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}\n逐格日志目录：{logs}")
    clone = prepare(tmp, owned)
    dispose_at_exit(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    env = test_env(clone)

    files = {rel: clone / rel for rel in (EXEC_REL, PROTO_REL)}
    originals = {rel: read(p) for rel, p in files.items()}
    basemd5 = {rel: md5_bytes(p) for rel, p in files.items()}

    controls = {}
    for pkg, filt in PKGS:
        c = run_test(clone, pkg, filt, env)
        label = LABELS[pkg]
        print(f"\n[{label}] 控制组 rc={c['rc']} settled={c['settled']} skip={c['skipped']} 红名={c['red']}")
        (logs / f"control_{label}.log").write_text(c["out"])
        if c["rc"] != 0 or c["settled"] == 0 or c["skipped"] > 0 or c["red"]:
            print(c["out"][-4000:])
            raise SystemExit(f"[{label}] 控制组不干净——后面所有红/绿都不可信")
        controls[pkg] = c["settled"]

    problems = []
    ran = 0
    for code, desc, rel, old, new, expect in cuts():
        if only and code not in only:
            continue
        ran += 1
        src = originals[rel]
        if src.count(old) != 1:
            problems.append(f"{code} 注码失效：锚点命中 {src.count(old)} != 1")
            print(f"{code:<4} {desc[:40]:<44} BROKEN=注码失效")
            continue
        files[rel].write_text(src.replace(old, new, 1))
        try:
            rs = {pkg: run_test(clone, pkg, filt, env) for pkg, filt in PKGS}
        finally:
            files[rel].write_text(src)
            if md5_bytes(files[rel]) != basemd5[rel]:
                leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
                raise SystemExit(f"{code} 还原后 md5 不一致，停机")

        raw = "\n".join(r["out"] for r in rs.values())
        (logs / f"{code}.log").write_text(raw)
        red = sorted(set().union(*[set(r["red"]) for r in rs.values()]))
        short = [r for r in rs.values() if r["rc"] != 0 and not r["red"]]
        if all(r["rc"] == 0 for r in rs.values()) and not red:
            v = "存活=洞"
        elif any(r["panicked"] or r["buildfailed"] for r in rs.values()) or short:
            v = "BROKEN=判不了"
        elif any(r["settled"] != controls[pkg] or r["skipped"] > 0 for pkg, r in rs.items()):
            v = "BROKEN=没跑完"
        elif set(red) != expect:
            v = "BROKEN=红集合不符"
        else:
            v = "杀掉"
        print(f"{code:<4} {desc[:40]:<44} {v:<14} settled=" +
              "+".join(str(rs[p]["settled"]) for p, _ in PKGS) + " skip=" +
              "+".join(str(rs[p]["skipped"]) for p, _ in PKGS) +
              f" 红={','.join(red) or '—'}")
        if v != "杀掉":
            problems.append(f"{code} {v}：{desc}")
            if v == "BROKEN=红集合不符":
                print(f"     期望={sorted(expect)} 实际={red}")
            for c in causes(raw):
                print("     红因: " + c)
            if v == "存活=洞":
                print(raw[-2500:])

    scope = f"{ran}/{len(cuts())} 格" + (f"（--cells {','.join(sorted(only))}）" if only else "")
    print("\n===== 判定：" + (f"{scope}，逐格被杀，无存活" if not problems
                          else f"{scope}，{len(problems)} 格未杀/BROKEN：" + "; ".join(problems)) + " =====")
    print(f"（红因逐格落在 {args.logs}/<格>.log，定性前先逐条读）")
    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
