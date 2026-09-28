#!/usr/bin/env python3
"""R22 lane 2 变异电池：D7 闸门四把「二手候选刀」（B 相 findings#1–#4）必须成格真注。

为什么还要这条电池（而不是「补刀腿已经写进 d7_gate_b20_test.go」就算完）：
- `docs/superpowers/specs/ledger/logs/b-phase/lane-2-d7-gate-findings.md` 自陈
  「所有候选均未在真树上注码跑过」＝二手候选；02:43 按四条各写了一条腿（注释逐条标 findings#N），
  但本仓口径是「腿注释里写专杀 X 不等于 X 被注过」——`grep d7PreviewRunes scripts/*.py` 零命中。
- 本电池把四把刀做成常驻格：G1 预览截断常量、G2 judge 帧 step_index 绑定、
  G3 跨进程择新帧方向、G4 落盘 expires_at 与内存真值。四格各钉一条腿，
  注码只在私有 `git clone --shared --no-checkout` 里发生，**绝不碰共享工作树**（别的泳道在飞）。

口径（沿用 mut_d7verdict_b20g.py / 批16–20 电池）：
- 动手前 `go test -list '^<腿>$'` 逐条点到，点不到＝腿不存在，停机；
- 控制组必须 rc==0、total>0、skip==0、无红，且 total **放刀前现测**（不写死常量）；
- 每格断言 PASS+FAIL==控制组数；BUILD FAILED / panic / 红而没点名该腿 一律 BROKEN，**不计入杀掉**；
- 锚点命中恰好一次、注码原文与注码后不得相同、注码先过 gofmt -e、还原比 md5；
- 一格一文件多处注码时「内存叠完一次写盘」（本批四格分落 executor.go / session.go 两个靶）。

用法：python3 scripts/mut_d7gate_r22lane.py [--keep] [--clone DIR]
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
from mut_dispose import dispose, workdir

ROOT = Path(__file__).resolve().parent.parent

EXEC_REL = "user-server/internal/browser_automation/service/executor.go"
SESS_REL = "user-server/internal/browser_automation/service/session.go"

# 本泳道脏 .go（含未跟踪的新用例文件）的枚举范围。手写清单必然漏 ⇒ 按目录取 git status。
# 漏了新腿文件＝克隆里没有这条腿 ⇒ 电池会把「刀存活」读错（brief 点名的旧坑）。
LANE_PATHS = ["user-server/internal/browser_automation",
              "user-server/internal/pkg/db",
              "user-server/internal/router"]
GO_PKGS = ["./internal/browser_automation/service/"]

# 四条腿（findings#1–#4 各一条；G4 腿名与 brief 有漂移，以仓内真名为准）
LEGS = {
    "G1": "TestAwaitConfirmGatePreviewTruncatesSubmittedText",
    "G2": "TestConfirmGateFramesBindPayloadAndOmitBody",
    "G3": "TestGateElsewhereFollowsNewestWaitFrame",
    "G4": "TestAwaitConfirmGateFrameWritesRealDeadline",
}
GO_RUN = "^(" + "|".join(LEGS[c] for c in ("G1", "G2", "G3", "G4")) + ")$"

# ---------------------------------------------------------------- 锚点（整行含缩进与换行，逐字）
# G1 findings#1：预览截断常量翻成 0 ⇒ 审批人只剩一个省略号（A5 承诺的「看见自己批的是什么」掏空）。
A_G1 = "const d7PreviewRunes = 200\n"
M_G1 = "const d7PreviewRunes = 0\n"
# G2 findings#2：judge 帧的 step_index 置 0（**只动 judge 帧**：:497 的 d7_wait 帧是既有腿的靶子，
# 动了会把「新腿有没有牙」证成「旧腿有没有牙」。锚点带上一行的 appendCommandLog(judge…) 做上下文。）
A_G2 = ('e.appendCommandLog(ctx, session.ID, task.ID, stepID, *seq, "judge", gateConfirmFrame, map[string]any{\n'
        '\t\t"payload_hash":      payloadHash,\n'
        '\t\t"step_index":        stepIndex,\n')
M_G2 = ('e.appendCommandLog(ctx, session.ID, task.ID, stepID, *seq, "judge", gateConfirmFrame, map[string]any{\n'
        '\t\t"payload_hash":      payloadHash,\n'
        '\t\t"step_index":        0,\n')
# G3 findings#3：跨进程归因取最旧帧而非最新帧。
A_G3 = '\t\tif l.Direction == "event" && l.Action == gateWaitFrame && (latest == nil || l.Seq > latest.Seq) {\n'
M_G3 = '\t\tif l.Direction == "event" && l.Action == gateWaitFrame && (latest == nil || l.Seq < latest.Seq) {\n'
# G4 findings#4：落盘 expires_at 写成固定 24h（与内存真值 AND 的口径失效）。
A_G4 = '\t\t"expires_at": gate.expiresAt.Format(time.RFC3339Nano),\n'
M_G4 = '\t\t"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339Nano),\n'

ANSI = re.compile(r"\x1b\[[0-9;]*m")
CONTROL = {"go": 0}


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def verdict(r: dict, expect_total: int, leg: str) -> str:
    if r.get("panicked") or r.get("buildfailed"):
        return "BROKEN=判不了"
    if r["total"] != expect_total or r["skipped"] > 0:
        return "BROKEN=判不了"
    if not r["killed"]:
        return "存活=洞" if r["rc"] == 0 else "BROKEN=判不了"
    if leg not in r["killed"]:
        return f"红了但没点出 {leg}"
    if r["failed"] < 1:
        return "BROKEN=判不了"
    return "杀掉"


def fail_detail(out: str, leg: str) -> str:
    """腿的逐字红因：=== RUN 到 --- FAIL 之间的缩进消息（含 t.Fatalf/t.Errorf 原文与行号）。"""
    lines = out.splitlines()
    start = None
    for i, l in enumerate(lines):
        if l.strip() == f"=== RUN   {leg}" or l.startswith(f"=== RUN   {leg}\t"):
            start = i
        if start is not None and i > start and l.startswith(f"--- FAIL: {leg}"):
            body = [x for x in lines[start + 1:i + 1] if x.strip()]
            return "\n".join(body[-8:])
    return "（未捕获到该腿的逐字红因）"


def dup_report(kills: dict) -> None:
    seen: dict[tuple, list] = {}
    for code, names in kills.items():
        seen.setdefault(tuple(sorted(names)), []).append(code)
    for key, codes in seen.items():
        if len(codes) > 1 and key:
            print("[Go] 同族（同一批用例被多格杀掉，判据可能重叠）：" + "≈".join(codes)
                  + f" → {' '.join(sorted(key))[:120]}")


def lane_overlays() -> list[str]:
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到本泳道脏文件清单：" + r.stderr[-200:])
    out = [l[3:].split(" -> ")[-1].strip().strip('"') for l in r.stdout.splitlines()]
    out = [p for p in out if p.endswith(".go")]
    if not out:
        raise SystemExit("脏文件清单为空——克隆里跑的是 HEAD，测不到本批改动（宁可停机也别假绿）")
    return out


def syntax_ok_go(src: str) -> tuple[bool, str]:
    p = subprocess.run(["gofmt", "-e"], input=src, capture_output=True, text=True, timeout=120)
    return p.returncode == 0, p.stderr.strip()


def go_prepare(dst: Path, owned: bool = False) -> Path:

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
    b = subprocess.run(["git", "-C", str(ROOT), "branch", "--show-current"],
                       capture_output=True, text=True, timeout=60)
    branch = b.stdout.strip() or "master"
    c = subprocess.run(["git", "checkout", "-f", branch], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if c.returncode != 0:
        bail("checkout 失败：" + (c.stdout + c.stderr)[-400:])
    overlaid = lane_overlays()
    for rel in overlaid:
        src = ROOT / rel
        if not src.exists():
            bail(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
        if md5_bytes(src) != md5_bytes(tgt):
            raise SystemExit(f"覆盖后 md5 不一致（装错树/写盘失败）：{rel}")
    leg_files = [p for p in overlaid if p.endswith("d7_gate_b20_test.go")]
    if not leg_files:
        bail("四条腿所在文件未进 overlay 名单——克隆里没有这些腿，判读必假")
    print(f"[Go] overlay {len(overlaid)} 个脏 .go，含腿文件 {leg_files[0]}")
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_env(root: Path) -> dict:
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    # 与同包其它电池共用缓存：换目录＝重编一次。
    env.setdefault("GOCACHE", "/tmp/gocache-a12mut")
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    return env


def leg_present(clone: Path, leg: str) -> bool:
    r = subprocess.run(["go", "test", "-list", f"^{leg}$"] + GO_PKGS,
                       cwd=clone / "user-server", capture_output=True, text=True,
                       timeout=1800, env=go_env(clone / "user-server"))
    out = ANSI.sub("", r.stdout + r.stderr)
    if r.returncode != 0:
        raise SystemExit(f"`go test -list ^{leg}$` 失败（包编不过或腿不可枚举）——停机查归属：\n" + out[-2500:])
    return bool(re.search(rf"^{re.escape(leg)}$", out, re.M))


def go_run(clone: Path) -> dict:
    root = clone / "user-server"
    p = subprocess.run(["go", "test", "-p", "1", "-count=1", "-v"] + GO_PKGS + ["-run", GO_RUN],
                       cwd=root, capture_output=True, text=True, timeout=1800, env=go_env(root))
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = sorted(set(re.findall(r"^    --- FAIL: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    passed = len(re.findall(r"^--- PASS: ", out, re.M))
    failed = len(re.findall(r"^--- FAIL: ", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: ", out, re.M))
    return {"rc": p.returncode, "killed": killed, "total": passed + failed + skipped,
            "passed": passed, "failed": failed, "skipped": skipped, "out": out,
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "cannot use" in out or "undefined:" in out}


def cells() -> list[tuple[str, str, str, str, str]]:
    """(格, 说明, 靶文件相对路径, 原文, 注码)"""
    return [
        ("G1", "findings#1 预览截断常量 d7PreviewRunes→0（审批人只剩省略号＝回到盲签）", EXEC_REL, A_G1, M_G1),
        ("G2", "findings#2 judge 帧 step_index 置 0（批准行不再指回具体步）", EXEC_REL, A_G2, M_G2),
        ("G3", "findings#3 跨进程择新帧方向 >→<（跟着最旧帧归因）", SESS_REL, A_G3, M_G3),
        ("G4", "findings#4 d7_wait 帧 expires_at 写成固定 24h（写下的期望≠内存真值）", EXEC_REL, A_G4, M_G4),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    tmp, owned = workdir(args.clone or None, prefix="r22lane2mut-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}", flush=True)
    problems: list[str] = []

    clone = go_prepare(tmp, owned)

    # 动手前①：四条腿逐条 `go test -list` 点到（点不到＝腿不存在，别给没跑的腿建格）
    for code, leg in LEGS.items():
        if not leg_present(clone, leg):
            raise SystemExit(f"{code} 腿 {leg} 在克隆里 -list 点不到＝腿不存在，停机")
    print(f"[Go] 腿前置：4 条腿 `go test -list` 逐条点到")

    # 动手前②：注码前置（锚点唯一 / 注码非恒等 / gofmt 可解析），按靶文件各自读原文
    originals: dict[str, str] = {}
    basemd5: dict[str, str] = {}
    targets: dict[str, Path] = {}
    prepared = []
    for code, desc, rel, old, new in cells():
        target = clone / rel
        if not target.exists():
            raise SystemExit(f"注码目标文件不在克隆里：{target}")
        targets[code] = target
        if rel not in originals:
            originals[rel] = read(target)
            basemd5[rel] = md5_bytes(target)
        src = originals[rel]
        if src.count(old) != 1:
            raise SystemExit(f"{code} 锚点命中 {src.count(old)} 次（要求恰好 1）：{old[:70]!r}")
        if old == new:
            raise SystemExit(f"{code} 注码无效（原文与注码后一致）")
        mutated = src.replace(old, new, 1)
        ok, err = syntax_ok_go(mutated)
        if not ok:
            raise SystemExit(f"{code} 注码语法坏，跑出来只会是 build failed：{err[:160]}")
        prepared.append((code, desc, rel, src, mutated))
    print(f"[Go] 注码前置：{len(prepared)} 格锚点各命中一次 + 注码后语法可解析")

    r = go_run(clone)
    CONTROL["go"] = r["total"]
    print(f"[Go] 控制组 rc={r['rc']} total={r['total']} passed={r['passed']} "
          f"skip={r['skipped']} FAIL={r['killed']}")
    if r["rc"] != 0 or r["total"] == 0 or r["skipped"] > 0 or r["killed"]:
        print(r["out"][-4000:])
        raise SystemExit("[Go] 控制组不干净（含 SKIP＝库没连上）——后面所有红/绿都不可信")
    print("[Go] 控制组逐腿：" + " | ".join(
        f"{c}={LEGS[c]}" for c in ("G1", "G2", "G3", "G4")) + f"（total={r['total']}，放刀前现测）")

    gkill: dict[str, set] = {}
    for code, desc, rel, src_original, mutated in prepared:
        target = targets[code]
        leg = LEGS[code]
        target.write_text(mutated, encoding="utf-8")
        if md5_bytes(target) == basemd5[rel]:
            raise SystemExit(f"{code} 注码未生效（与原内容一致）")
        r = go_run(clone)
        v = verdict(r, CONTROL["go"], leg)
        print(f"\n{code:<4} {desc[:60]}\n     注码: {cells_src(rel, code)[1]}"
              f"\n     结果: {v:<7} total={r['total']} pass+fail={r['passed'] + r['failed']} "
              f"fail={r['failed']} skip={r['skipped']} ｜ 红名={r['killed']}")
        if v == "杀掉":
            print(f"     逐字红因（{leg}）：\n" +
                  "\n".join("       " + l for l in fail_detail(r["out"], leg).splitlines()))
        else:
            problems.append(f"[Go] {code} {v}：{desc}")
            print(r["out"][-3000:])
        gkill[code] = set(r["killed"])
        target.write_text(src_original, encoding="utf-8")
        if md5_bytes(target) != basemd5[rel]:
            raise SystemExit(f"{code} 还原后 md5 不一致，停机")

    dup_report(gkill)
    print("\n[Go] 已全量还原（md5 一致）")

    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    if problems:
        print("\n===== 电池判定：有洞 =====")
        for x in problems:
            print("  ✗", x)
        return 1
    print(f"\n===== 电池判定：Go {len(prepared)} 格（G1–G4）逐刀被杀、无存活 =====")
    return 0


def cells_src(rel: str, code: str) -> tuple[str, str]:
    for c, _, r_, old, new in cells():
        if c == code:
            return old, f"{rel}: {old.strip()} → {new.strip()}"
    raise KeyError(code)


if __name__ == "__main__":
    sys.exit(main())
