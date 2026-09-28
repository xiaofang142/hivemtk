#!/usr/bin/env python3
"""批19f 变异电池：Host 通道的回环门 + 轮换文案，逐分支验牙。

为什么要有这条电池：本批修的是「门看哪个地址」和「一句话与一段行为对不对得上」。
两类病各自有反向的错法，只测一侧的话另一侧随便改都是绿的：
- 门反向（F3）：把「非回环即拒」写成「回环即拒」——所有真机 Host 当场连不上，
  而「攻击者进不来」那条腿照样绿（它本来就不该进）；所以必须有「本机对端仍要能走到 token 门」的腿；
- 门摘掉（F2）/ 退回采信表头（F1）/ 畸形地址判不出就放行（F4）：三条都是把撤销/防护写松，
  各由一条腿点名（F4 只有 fail-closed 那条腿管得着，别的腿用的都是合法地址）；
- 文案（F5）与 prev 生命周期（F6/F7）是一条锁的两半：文案说有期限而实现没有（原病），
  或反过来给 prev 加了期限却没改文案（将来病），两种都要红。

口径（沿用批16/17/18/19 各电池）：控制组必须 rc==0、ran==用例数、skip==0；每格 cp 备份 + md5 还原；
锚点命中恰好一次；注码先过 gofmt -e（编译红不算杀，b19e 第一版就是整轮栽在这）；
红了必须点出**这一格该红的那条腿**（别人的红不算这格有牙）。
只在私有 --shared 克隆里注码，绝不碰共享工作树（并行会话在里面提交）。

用法：python3 scripts/mut_host_gate_b19f.py [--keep] [--clone DIR]
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
CTRL = "internal/browser_automation/controller"
SVC = "internal/browser_automation/service"
DTO = "internal/browser_automation/dto"
GO_OVERLAY = [
    f"user-server/{CTRL}/host.go",
    f"user-server/{CTRL}/task.go",
    f"user-server/{CTRL}/cron.go",
    f"user-server/{CTRL}/session.go",
    f"user-server/{CTRL}/cron_error_b19c_test.go",
    f"user-server/{CTRL}/session_error_b19d_test.go",
    f"user-server/{CTRL}/step_cap_b19e_test.go",
    f"user-server/{CTRL}/host_loopback_b19f_test.go",
    f"user-server/{DTO}/task.go",
    f"user-server/{SVC}/task.go",
    f"user-server/{SVC}/cron.go",
    f"user-server/{SVC}/host_token.go",
    f"user-server/{SVC}/dependency_b19_test.go",
]
GO_PKG = CTRL
SPOOF = "TestB19FSpoofedLoopbackHeaderDoesNotOpenHostChannel"
PEER = "TestB19FLoopbackPeerStillReachesTokenGate"
FWD = "TestB19FLocalHostBehindForwarderIsNotLockedOut"
MALFORMED = "TestB19FUnparseablePeerAddressIsRefused"
MESSAGE = "TestB19FResetTokenMessageMatchesPrevTokenLifetime"
GO_RUN = "|".join([SPOOF, PEER, FWD, MALFORMED, MESSAGE])
RUN_N = 5

ANSI = re.compile(r"\x1b\[[0-9;]*m")

GATE = "if ip := net.ParseIP(ctx.RemoteIP()); ip == nil || !ip.IsLoopback() {"
GATE_PEER_ONLY = "ip == nil || !ip.IsLoopback()"
MSG = 'response.Success(ctx, gin.H{"token": token}, "已重置；旧 token 仍可用，直到下一次重置")'
PREV_BLOCK = ('\tif v, err := kvRepo.Get(ctx, hostTokenPrevKey); err == nil && strings.TrimSpace(v) != "" {\n'
              "\t\tcandidates = append(candidates, strings.TrimSpace(v))\n"
              "\t}\n")
PREV_WRITE = ('\tif old != "" {\n'
              '\t\tif _, err := kvRepo.Upsert(ctx, hostTokenPrevKey, old); err != nil {\n')


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def md5(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1）：{old[:90]!r}")
    return text.replace(old, new, 1)


def syntax_ok(src: str) -> tuple:
    p = subprocess.run(["gofmt", "-e"], input=src, capture_output=True, text=True, timeout=120)
    return p.returncode == 0, p.stderr.strip()


def cells():
    def f6(s):
        # 摘掉 prev 候选块：旧 token 立刻失效（撤销变「立即」），但文案说的不是这个
        return sub_once(s, PREV_BLOCK, "", "F6")

    def f7(s):
        # 轮换不再把旧值写进 prev：与 F6 同一条腿的另一半（一处是读侧、一处是写侧）
        return sub_once(s, PREV_WRITE, "\tif false && old != \"\" {\n"
                                       "\t\tif _, err := kvRepo.Upsert(ctx, hostTokenPrevKey, old); err != nil {\n", "F7")

    return [
        ("F1", "GATE", "门改回看 ClientIP（一行表头就能伪成本机）",
         lambda s: sub_once(s, GATE, GATE.replace("ctx.RemoteIP()", "ctx.ClientIP()"), "F1"), SPOOF),
        ("F2", "GATE", "回环门整体摘掉",
         lambda s: sub_once(s, GATE_PEER_ONLY, "false && (" + GATE_PEER_ONLY + ")", "F2"), SPOOF),
        ("F3", "GATE", "门写反（回环反而被拒：真机全挂）",
         lambda s: sub_once(s, GATE_PEER_ONLY, "ip == nil || ip.IsLoopback()", "F3"), PEER),
        ("F4", "GATE", "地址判不出时当成放行",
         lambda s: sub_once(s, GATE_PEER_ONLY, "ip != nil && !ip.IsLoopback()", "F4"), MALFORMED),
        ("F5", "HOST", "文案退回未实现的 24h 承诺",
         lambda s: sub_once(s, MSG, MSG.replace("旧 token 仍可用，直到下一次重置", "旧 token 24h 内仍可用"), "F5"), MESSAGE),
        ("F6", "TOKN", "读侧不再接受 prev（撤销变立即，与文案不符）", f6, MESSAGE),
        ("F7", "TOKN", "写侧不再落 prev（轮换即丢旧 token）", f7, MESSAGE),
    ]


def go_prepare(dst: Path) -> Path:
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
        raise SystemExit("checkout 失败：" + (b.stdout + b.stderr)[-400:])
    for rel in GO_OVERLAY:
        src = ROOT / rel
        if not src.exists():
            raise SystemExit(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
        if md5(src) != md5(tgt):
            raise SystemExit(f"覆盖后 md5 不一致（装错树/写盘失败）：{rel}")
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path):
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-b19fmut")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    p = subprocess.run(["go", "test", "./" + GO_PKG + "/", "-run", GO_RUN, "-count=1", "-v"],
                       cwd=root, capture_output=True, text=True, timeout=1800, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = sorted(set(re.findall(r"^    --- FAIL: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    ran = len(re.findall(r"^=== RUN\s+(\S+)", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: (\S+)", out, re.M))
    return p.returncode, killed, ran, skipped, out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    tmp, owned = workdir(args.clone or None, prefix="b19f-mut-", repo_root=ROOT)
    print(f"私有作业目录：{tmp}", flush=True)

    clone = go_prepare(tmp)
    u = clone / "user-server"
    TARGET = {"GATE": u / CTRL / "host.go", "HOST": u / CTRL / "host.go", "TOKN": u / SVC / "host_token.go"}
    originals = {p: md5(p) for p in set(TARGET.values())}

    # 前置：锚点各命中一次 + 注码后语法可解析（编译红不算杀，先判掉）
    texts = {k: read(v) for k, v in TARGET.items()}
    for tag, key, _desc, apply, _expect in cells():
        mutated = apply(texts[key])
        if mutated == texts[key]:
            raise SystemExit(f"{tag} 注码无效（替换后与原文件一致）")
        ok, err = syntax_ok(mutated)
        if not ok:
            raise SystemExit(f"{tag} 注码语法坏，跑出来只会是 build failed：{err[:200]}")
    print("[注码前置] 七格锚点各命中一次 + 注码后语法可解析\n", flush=True)

    rc, killed, ran, skipped, out = go_run(clone)
    if rc != 0 or ran != RUN_N or skipped != 0:
        print(out[-3000:])
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
        raise SystemExit(f"控制组不成立 rc={rc} ran={ran}（要求 {RUN_N}）skip={skipped}——整轮判「无法判定」")
    print(f"[控制组] rc=0 ran={ran} skip=0 全绿\n", flush=True)

    survivors, broken = [], []
    try:
        for tag, key, desc, apply, expect in cells():
            tgt = TARGET[key]
            src = read(tgt)
            tgt.write_text(apply(src), encoding="utf-8")
            if md5(tgt) == originals[tgt]:
                broken.append(f"{tag} 注码未生效（文件与原内容一致）")
                continue
            rc, killed, ran, skipped, out = go_run(clone)
            tgt.write_text(src, encoding="utf-8")
            # originals 按**文件路径**建表（两个角色 GATE/HOST 指同一个 host.go），
            # 拿角色名去查就是 KeyError——第一版崩在这里：F1 的码还留在克隆里、
            # 一格结论都没产出，而日志尾部看着像跑完了。
            if md5(tgt) != originals[tgt]:
                raise SystemExit(f"{tag} 还原失败：md5 与原文件不一致，已停机（克隆保留 {clone}）")
            if rc == 0:
                survivors.append(tag)
                print(f"{tag:<4} {desc:<38} 存活", flush=True)
            elif expect not in killed:
                broken.append(f"{tag} 红了但没点出 {expect}（killed={killed or '空=编译红'}）")
                print(f"{tag:<4} {desc:<38} 无法判定\n{out[-1500:]}", flush=True)
            else:
                print(f"{tag:<4} {desc:<38} 杀掉  {' '.join(killed)}", flush=True)
    finally:
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)

    print()
    if broken:
        print("===== 电池判定：以下格无法判定，须先修注码 =====")
        for b in broken:
            print("  " + b)
        return 2
    if survivors:
        print(f"===== 电池判定：{len(survivors)} 格存活 = 门/文案有洞：{' '.join(survivors)} =====")
        return 1
    print(f"===== 电池判定：{len(cells())} 格逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
