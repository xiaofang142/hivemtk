#!/usr/bin/env python3
"""C 相续刀 · lane 5：出站收口 cutoff 上界（O1 两格）+ 认领超时下界（O2）三刀成格并跑。

来源（二手候选，均未实际注码运行过）：
  docs/superpowers/specs/ledger/logs/b-phase/lane-5-outbound-ack-findings.md
判格表（c-phase/lane-5-outbound-verify-then-fix.md）与动手前的盘面复核：
- 与 mut_push_budget_b20d.py 的 R7/R8 **不是同一把刀**：R7/R8 注的是「不调 exhaustOutbound」
  （换 accountID 让收口空转），本电池 O1 注的是「调用时把实参 cutoff 换成 time.Now()」
  （收口照调，但「认领已超时」这半个谓词被放宽成「任何已认领」）。两把刀红的是不同的格。
- O1 按 brief 拆两格：两处调用点（轮询 ClaimPendingOutbound:84 / 补拉 FetchOutboundUndelivered:162）
  各自单独一刀，不并成一格。
- 动手前点腿（go test -list 已确认六条全在）：收口面除表里的 TestPushCapSweepTouchesOnlyOwedOutbound
  外，本泳道 02:47 那批已在 message_hub_outbound_push_cap_b20d_test.go 里补了
  TestPushCapSweepSparesUnexpiredInflightRow（自陈杀的就是 cutoff→time.Now()，但只走补拉路径）；
  O2 的腿 TestInboxOutboundClaimTimeoutCoversBridgeSendBudget 钉的是下界（<20s 才红），
  砍小 30s→5s 正打在该断言方向上。

口径（照抄 mut_push_budget_b20d.py 的克隆 + overlay + 控制组 + 判格 + md5 骨架）：
- 注码只发生在私有 `git clone --shared --no-checkout` 里，绝不碰共享工作树；
- 工作树里 user-server 的**全部**脏 .go（含未跟踪新腿）进 overlay——本仓踩过：
  新腿没进克隆 ⇒ 把「克隆里没这条腿」读成「刀存活」；service 包另有泳道在飞的文件
  不带上 = 克隆里编不出控制组，电池失声；
- 控制组放刀前现测（rc==0、skip==0、无红名），每格断言 PASS+FAIL==控制组数；
  BUILD FAILED / panic / 红而没点名 ⇒ BROKEN 不计入杀掉；锚点命中恰好一次；
- 口令只从 user-server/.env 用 awk 精确取 POSTGRES_PASSWORD 喂 POSTGRES_TEST_PASSWORD，永不打印。

用法：python3 scripts/mut_outbound_claim_r22lane.py [--keep] [--clone DIR]
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
REPO_REL = Path("user-server/internal/repository/message_hub_inbox_outbound.go")
SVC_REL = Path("user-server/internal/service/inbox_ingress_outbound.go")
# overlay 枚举范围：user-server 下**全部**脏 .go（M/??）。范围写窄会漏依赖面 ⇒ 克隆里编不过，
# 换来的是 [build failed] 式失声而不是证据。
OVERLAY_SCOPE = ["user-server"]
GO_PKGS = ["./internal/repository/", "./internal/service/"]

LEG_SWEEP_OWED = "TestPushCapSweepTouchesOnlyOwedOutbound"
LEG_SWEEP_SPARE = "TestPushCapSweepSparesUnexpiredInflightRow"
LEG_POLL_SPARE = "TestPushCapPollSweepSparesUnexpiredInflightRow"
LEG_CAPS = "TestClaimPendingOutboundCapsPushAttemptsAndTerminalizes"
LEG_RECYCLE = "TestInflightRecycleKeepsPushAttemptBudget"
LEG_FETCH = "TestFetchOutboundUndeliveredExcludesAndEscalatesExhausted"
LEG_CLAIM_TTL = "TestInboxOutboundClaimTimeoutCoversBridgeSendBudget"
GO_RUN = "|".join([LEG_SWEEP_OWED, LEG_SWEEP_SPARE, LEG_POLL_SPARE,
                   LEG_CAPS, LEG_RECYCLE, LEG_FETCH, LEG_CLAIM_TTL])

ANSI = re.compile(r"\x1b\[[0-9;]*m")
CONTROL = {"go": 0}


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def lane_overlays() -> list[str]:
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + OVERLAY_SCOPE,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到脏文件清单：" + r.stderr[-200:])
    out = []
    for line in r.stdout.splitlines():
        tag = line[:2]
        if "D" in tag:  # 工作树删掉的文件不能覆盖，克隆里也必须没有——本电池跑前核对过：无 D 项
            p = line[3:].split(" -> ")[-1].strip().strip('"')
            raise SystemExit(f"脏清单里有删除项（克隆会带 HEAD 旧文件编译）：{p}")
        p = line[3:].split(" -> ")[-1].strip().strip('"')
        if p.endswith(".go"):
            out.append(p)
    if not out:
        raise SystemExit("脏文件清单为空——克隆里跑的是 HEAD，测不到本泳道依赖的未跟踪新腿（宁可停机也别假绿）")
    return out


def verdict(r: dict, expect_total: int) -> str:
    """三态判定：杀掉 / 存活=洞 / BROKEN（红了但判不了）。"""
    if r["rc"] == 0 and not r["killed"]:
        return "存活=洞"
    if r.get("panicked") or r.get("buildfailed") or not r["killed"]:
        return "BROKEN=判不了"
    if r["total"] != expect_total or r["failed"] < 1 or r["skipped"] > 0:
        return "BROKEN=判不了"
    return "杀掉"


def dup_report(kills: dict) -> None:
    seen = {}
    for code, names in kills.items():
        key = tuple(sorted(names))
        seen.setdefault(key, []).append(code)
    for key, codes in seen.items():
        if len(codes) > 1 and key:
            print("[Go] 同族（同一批用例被多格杀掉，判据可能重叠）：" + "≈".join(codes)
                  + f" → {' '.join(sorted(key))[:120]}")


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
    # 分支从工作树读：--no-checkout 的克隆里 HEAD 是未 born 的符号引用。
    b = subprocess.run(["git", "-C", str(ROOT), "branch", "--show-current"],
                       capture_output=True, text=True, timeout=60)
    branch = b.stdout.strip() or "master"
    c = subprocess.run(["git", "checkout", "-f", branch], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if c.returncode != 0:
        bail("checkout 失败：" + (c.stdout + c.stderr)[-400:])
    overlays = lane_overlays()
    for rel in overlays:
        src = ROOT / rel
        if not src.exists():
            bail(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
        if md5_bytes(src) != md5_bytes(tgt):
            leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
            raise SystemExit(f"覆盖后 md5 不一致（装错树/写盘失败）：{rel}")
    print(f"[Go] overlay：{len(overlays)} 个脏 .go 已进克隆（含本泳道未跟踪新腿）")
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path) -> dict:
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-r22lane-outbound")
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    envf = root / ".env"
    if envf.exists():
        # awk 语义逐行精确取键（首个 POSTGRES_PASSWORD= 生效），值永不打印。
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    p = subprocess.run(["go", "test", "-p", "1", "-count=1", "-v"] + GO_PKGS
                       + ["-run", "^(" + GO_RUN + ")$"],
                       cwd=root, capture_output=True, text=True, timeout=1800, env=env)
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


# 锚点取**完整调用语句 + 各自的 Printf 行**把两处调用点分开（同一句 `exhaustOutbound(..., cutoff)`
# 在文件里命中 2 次，裸锚点会被唯一性门当场拦下）。
POLL_SWEEP = ('\tif err := r.exhaustOutbound(ctx, channel, accountID, cutoff); err != nil {\n'
              '\t\tfmt.Printf("[ClaimPendingOutbound] 到界行升级终态失败（继续认领）: %v\\n", err)\n\t}')
FETCH_SWEEP = ('\tif err := r.exhaustOutbound(ctx, channel, accountID, cutoff); err != nil {\n'
               '\t\tfmt.Printf("[FetchOutboundUndelivered] 到界行升级终态失败（继续取待推）: %v\\n", err)\n\t}')
CLAIM_TTL = "const InboxOutboundClaimTimeout = 30 * time.Second"


def go_mutants():
    """(格, 说明, [(文件槽, 原文, 注码), ...], 该红的腿)"""
    return [
        ("O1a", "轮询路径收口实参 cutoff→time.Now()（凡 inflight 皆判死，正在发的那条被就地 failed）",
         [("repo", POLL_SWEEP, POLL_SWEEP.replace("accountID, cutoff", "accountID, time.Now()"))],
         LEG_POLL_SPARE),
        ("O1b", "补拉路径收口实参 cutoff→time.Now()（同上，第三条取行路径单独一刀）",
         [("repo", FETCH_SWEEP, FETCH_SWEEP.replace("accountID, cutoff", "accountID, time.Now()"))],
         LEG_SWEEP_SPARE),
        ("O2", "InboxOutboundClaimTimeout 30s→5s（可见性窗短于桥端单次发送预算 20s ⇒ 双发窗口）",
         [("svc", CLAIM_TTL, "const InboxOutboundClaimTimeout = 5 * time.Second")],
         LEG_CLAIM_TTL),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    tmp, owned = workdir(args.clone or None, prefix="r22lane-outbound-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}", flush=True)
    problems = []

    clone = go_prepare(tmp, owned)
    dispose_at_exit(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    files = {"repo": clone / REPO_REL, "svc": clone / SVC_REL}
    for name, p in files.items():
        if not p.exists():
            raise SystemExit(f"注码目标文件不在克隆里：{p}")
    originals = {name: read(p) for name, p in files.items()}
    basemd5 = {name: md5_bytes(p) for name, p in files.items()}

    cells = []
    for code, desc, edits, expect in go_mutants():
        acc: dict[str, str] = {}
        for slot, old, new in edits:
            cur = acc.get(slot, originals[slot])
            hit = cur.count(old)
            if hit != 1:
                raise SystemExit(f"{code} 锚点在 {slot} 里命中 {hit} 次（要求恰好 1）：{old[:70]!r}")
            if old == new:
                raise SystemExit(f"{code} 注码无效（原文与注码后一致）")
            acc[slot] = cur.replace(old, new, 1)
        for slot, src in acc.items():
            ok, err = syntax_ok_go(src)
            if not ok:
                raise SystemExit(f"{code} 注码语法坏，跑出来只会是 build failed：{err[:160]}")
        cells.append((code, desc, acc, expect))
    print(f"\n[Go] 注码前置：{len(cells)} 格锚点各命中一次 + 注码后语法可解析")

    r = go_run(clone)
    CONTROL["go"] = r["total"]
    print(f"[Go] 控制组 rc={r['rc']} total={r['total']} passed={r['passed']} "
          f"skip={r['skipped']} FAIL={r['killed']}")
    if r["rc"] != 0 or r["total"] == 0 or r["skipped"] > 0 or r["killed"]:
        print(r["out"][-4000:])
        raise SystemExit("[Go] 控制组不干净——后面所有红/绿都不可信")

    gkill = {}
    for code, desc, acc, expect in cells:
        for slot, src in acc.items():
            files[slot].write_text(src, encoding="utf-8")
        for slot in acc:
            if md5_bytes(files[slot]) == basemd5[slot]:
                raise SystemExit(f"{code} 注码未生效（{slot} 与原内容一致）")
        r = go_run(clone)
        v = verdict(r, CONTROL["go"])
        if v == "杀掉" and expect not in r["killed"]:
            v = f"红了但没点出 {expect}"
        print(f"{code:<4} {desc[:56]:<58} {v:<7} total={r['total']} fail={r['failed']} "
              f"skip={r['skipped']} ｜ " + " | ".join(k[:60] for k in r["killed"][:3]))
        if not v.startswith("杀掉"):
            problems.append(f"[Go] {code} {v}：{desc}")
            print(r["out"][-3000:])
        gkill[code] = set(r["killed"])
        for slot in acc:
            files[slot].write_text(originals[slot], encoding="utf-8")
        for slot in acc:
            if md5_bytes(files[slot]) != basemd5[slot]:
                leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
                raise SystemExit(f"{code} 还原后 md5 不一致，停机")
    dup_report(gkill)
    print("[Go] 克隆内注码文件已全量还原（md5 一致）；工作树全程未被写入")

    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    if problems:
        print("\n===== 电池判定：有洞 =====")
        for x in problems:
            print("  ✗", x)
        return 1
    print(f"\n===== 电池判定：Go {len(go_mutants())} 格逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
