#!/usr/bin/env python3
"""批19e 变异电池：编排步数上界（Create / Update 两条 tag）逐格验牙。

为什么要有这条电池：上界是**两处字面量**（创建 tag、编辑 tag），而不是一处判据。
只测创建侧的话，「编辑没加 max」这种半只眼睛的改法是绿的——而编辑恰恰是更容易被绕的一侧
（存量任务随便改）。所以每处 tag 都要有「外侧必拒 / 内侧必过」两格：
- E1/E2：把某一处 max 摘掉 → 该路径的 201 步必须变绿（绿=没拦住），点名对应测试；
- E3/E4：把某一处改成 max=0（上界写小到合法编排存不下）→ 该路径的 200 步必须变红；
- E5/E6：把某一处放大到 1000 → 201 步被收下，外侧腿必须红。
只测一侧的话，另一处的漏改不会有任何反应（这是本批最容易犯的错，所以逐处拆刀）。

口径（沿用批16/17/18/19b/19c/19d 电池）：控制组必须 rc==0、ran==3、skip==0；每格写完即还原并比 md5；
锚点命中恰好一次；红了必须点得出**这一格该红的那条腿**（编译红或别人的红都不算杀）。
注码先过一遍 gofmt -e 再开跑——第一版就是栽在这：替换串只写了 tag 字面量、把字段名一起抹掉，
六格全成 [build failed]，battery 报「红了但没点名」判 rc=2（这是电池该判的样子，但一整轮白跑）。
只在私有 --shared 克隆里注码。覆盖源复制后逐文件比 md5（装错树会让整轮结论作废）。

用法：python3 scripts/mut_step_cap_b19e.py [--keep] [--clone DIR]
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

ROOT = Path(__file__).resolve().parent.parent
CTRL = "internal/browser_automation/controller"
SVC = "internal/browser_automation/service"
DTO = "internal/browser_automation/dto"
GO_OVERLAY = [
    f"user-server/{DTO}/task.go",
    f"user-server/{CTRL}/session.go",
    f"user-server/{CTRL}/task.go",
    f"user-server/{CTRL}/cron.go",
    f"user-server/{CTRL}/cron_error_b19c_test.go",
    f"user-server/{CTRL}/session_error_b19d_test.go",
    f"user-server/{CTRL}/step_cap_b19e_test.go",
    f"user-server/{SVC}/task.go",
    f"user-server/{SVC}/cron.go",
    f"user-server/{SVC}/dependency_b19_test.go",
]
GO_PKG = CTRL
# 只跑本批三条腿：上界只有这三处断言，跑整包会把无关用例的红混进「杀掉」名单，
# 看着像牙其实是被别人撞红的（max=0 尤其明显——包里任何带 steps 的夹具都会倒）。
GO_RUN = "|".join([
    "TestB19EStepCountOverCapIsRejectedAtTheBoundary",
    "TestB19EUpdatePathSharesTheCap",
    "TestB19EStepCountAtCapStillCreates",
])
OVER = "TestB19EStepCountOverCapIsRejectedAtTheBoundary"
ATCAP = "TestB19EStepCountAtCapStillCreates"
UPDATE = "TestB19EUpdatePathSharesTheCap"

ANSI = re.compile(r"\x1b\[[0-9;]*m")
CAP = '`json:"steps" binding:"omitempty,max=200"`'
CREATE_FIELD = "\tSteps []StepItem " + CAP
UPDATE_FIELD = "\tSteps      []StepItem " + CAP


def cell(anchor: str, tag: str) -> tuple:
    # 注码必须替换**整行**（含字段名）：第一版只换掉引号里的 tag，结果写回去的是一行
    # 裸字面量 —— 六格全成 build failed。编译红不算杀（点了名也不知道是牙红了还是码坏了）。
    return (anchor, anchor.replace(CAP, tag))


TAGS = {
    "E1": (cell(CREATE_FIELD, '`json:"steps" binding:"omitempty"`'), OVER,
           "创建侧 max 摘掉（201 步照样入库）"),
    "E2": (cell(UPDATE_FIELD, '`json:"steps" binding:"omitempty"`'), UPDATE,
           "编辑侧 max 摘掉（绕开创建的口子）"),
    "E3": (cell(CREATE_FIELD, '`json:"steps" binding:"omitempty,max=0"`'), ATCAP,
           "创建侧上界写成 0（合法编排存不下）"),
    "E4": (cell(UPDATE_FIELD, '`json:"steps" binding:"omitempty,max=0"`'), UPDATE,
           "编辑侧上界写成 0"),
    "E5": (cell(CREATE_FIELD, '`json:"steps" binding:"omitempty,max=1000"`'), OVER,
           "创建侧放大到 1000"),
    "E6": (cell(UPDATE_FIELD, '`json:"steps" binding:"omitempty,max=1000"`'), UPDATE,
           "编辑侧放大到 1000"),
}


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def syntax_ok(src: str) -> tuple[bool, str]:
    """注码先过一遍 gofmt -e：语法坏的码跑出来是 build failed，点了名也判不了牙。
    第一版六格全编译红就是这么漏的，这道前置把它挡在跑测试之前。"""
    p = subprocess.run(["gofmt", "-e"], input=src, capture_output=True, text=True, timeout=120)
    return p.returncode == 0, p.stderr.strip()


def md5(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


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
    env.setdefault("GOCACHE", "/tmp/gocache-b19emut")
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

    dst = Path(args.clone) if args.clone else Path("/tmp/b19e-mut")
    if dst.exists():
        raise SystemExit(f"{dst} 已存在（换 --clone 目录或先删）")
    dst.mkdir(parents=True)
    print(f"私有作业目录：{dst}", flush=True)

    clone = go_prepare(dst)
    DTOF = clone / "user-server" / DTO / "task.go"
    original = md5(DTOF)
    src0 = read(DTOF)
    # 前置：两处锚点在现文件里各命中一次（gofmt 改了列对齐就会 0 次，那时注码静默失效）
    for name, anchor in (("Create", CREATE_FIELD), ("Update", UPDATE_FIELD)):
        if src0.count(anchor) != 1:
            raise SystemExit(f"{name} 侧锚点命中 {src0.count(anchor)} 次（要求恰好 1）：{anchor!r}")
    # 前置：每格注码后的 dto/task.go 必须仍可解析（编译红不算杀，先判掉）
    for tag, ((old, new), _expect, _desc) in TAGS.items():
        mutated = src0.replace(old, new, 1)
        if mutated == src0:
            raise SystemExit(f"{tag} 注码无效（替换后与原文件一致）")
        ok, err = syntax_ok(mutated)
        if not ok:
            raise SystemExit(f"{tag} 注码语法坏，跑出来只会是 build failed：{err[:200]}")
    print("[注码前置] 两处锚点各命中一次 + 六格注码后语法可解析\n", flush=True)

    rc, killed, ran, skipped, out = go_run(clone)
    if rc != 0 or ran != 3 or skipped != 0:
        print(out[-3000:])
        shutil.rmtree(dst, ignore_errors=True)
        raise SystemExit(f"控制组不成立 rc={rc} ran={ran}（要求 3）skip={skipped}——整轮判「无法判定」")
    print(f"[控制组] rc=0 ran={ran} skip=0 全绿\n", flush=True)

    survivors, broken = [], []
    try:
        for tag, ((old, new), expect, desc) in TAGS.items():
            src = read(DTOF)
            if src.count(old) != 1:
                broken.append(f"{tag} 锚点命中 {src.count(old)} 次，注码跳过")
                continue
            DTOF.write_text(src.replace(old, new, 1), encoding="utf-8")
            if md5(DTOF) == original:
                broken.append(f"{tag} 注码未生效（文件与原内容一致）")
                continue
            rc, killed, ran, skipped, out = go_run(clone)
            DTOF.write_text(src, encoding="utf-8")
            if md5(DTOF) != original:
                raise SystemExit(f"{tag} 还原失败：md5 与原文件不一致，已停机（克隆保留 {clone}）")
            if rc == 0:
                survivors.append(tag)
                print(f"{tag:<4} {desc:<34} 存活", flush=True)
            elif expect not in killed:
                # 红必须点名**这一格该红的那条腿**：别的用例撞红不算这格有牙。
                broken.append(f"{tag} 红了但没点出 {expect}（killed={killed or '空=编译红'}）")
                print(f"{tag:<4} {desc:<34} 无法判定\n{out[-1500:]}", flush=True)
            else:
                print(f"{tag:<4} {desc:<34} 杀掉  {' '.join(killed)}", flush=True)
    finally:
        if not args.keep:
            shutil.rmtree(dst, ignore_errors=True)

    print()
    if broken:
        print("===== 电池判定：以下格无法判定，须先修注码 =====")
        for b in broken:
            print("  " + b)
        return 2
    if survivors:
        print(f"===== 电池判定：{len(survivors)} 格存活 = 上界有一处没被守着：{' '.join(survivors)} =====")
        return 1
    print(f"===== 电池判定：{len(TAGS)} 格逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
