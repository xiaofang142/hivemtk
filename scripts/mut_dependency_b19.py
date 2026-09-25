#!/usr/bin/env python3
"""批19b 变异电池：依赖归属门（IDOR）这一条不变式，逐判据验牙。

为什么要有这条电池：本批的修复只有两个动作（Create 入口补归属校验 + 执行期加 fail-closed 兜底），
而它的价值全在"判定只有一份、两条入口共用"这句话上。写成一格绿不算锁住——
- 删掉 Create 那一刀（D1）＝回到本批开始前的形状：任何人都能把任务挂到别人的 id 上；
- 把归属判据缩成"只看 id 存不存在"（D2/D6）＝跨用户照样过，且这是最常见的"修了但没修全"；
- 把「不存在」和「不是你的」分成两句文案（D3）＝一条可枚举 id 的存在性探针，比不修更细；
- 把判据写成一律拒绝（D5）＝安全但毁掉正常路径，反向锁必须点名。

口径（沿用批16/17/18 电池）：控制组必须 rc==0、ran>0、skip==0，否则整轮判"无法判定"停机；
每个变异体 cp 备份 + 逐次 md5 比对还原；注码必须断言命中恰好一次；红了必须点得出测试名
（编译红的"红"不算杀）；只在私有 --shared 克隆里注码，绝不碰共享工作树（并行会话在里面提交）。

两格在第一版判不了定，形状值得记下来（同类电池的通用坑）：
- D2/D6 原本只把条件里的归属判据删掉 → Go 判 dep 未使用 → build failed，红因不在测试面上；
  归属门是"读回整行再看主人"，缩成存在性检查必须连读回形状（dep, err := → _, err :=）一起改。
- D3 原本只改那一句文案字面量 → 恒等变异（归属门修好后「不存在」与「不是你的」共用同一个
  return，改字面量会一起改掉，测试看到的仍是同一句）。要验"分成两句"这个回归，注码必须
  把分支重新劈开，而不是改字符串。

用法：python3 scripts/mut_dependency_b19.py [--keep] [--clone DIR]
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
SVC = "internal/browser_automation/service"
# 克隆里没有本泳道的未提交改动，必须整文件覆盖过去，被测的才是"工作区里这份代码"。
GO_OVERLAY = [
    f"user-server/{SVC}/task.go",
    f"user-server/{SVC}/dependency_b19_test.go",
]
# 归属门的读侧（新）+ 同一批错误类型的既有契约（旧）：跑这一组才看得出"修这条断了那条"
GO_RUN = "TestB19|TestStateAndInputPreconditionsCarryTypes|TestRunTaskRejectsDraftAsStateConflict"

ANSI = re.compile(r"\x1b\[[0-9;]*m")

CREATE_CALL = (
    "\t\tif err := s.checkDependencyTarget(ctx, userID, t.ID, t.DependsOnTaskID); err != nil {\n"
    "\t\t\treturn err\n"
    "\t\t}\n"
)
TARGET_LOOKUP = (
    "\tdep, err := s.taskRepo.GetByIDAnyUser(ctx, *dependsOn)\n"
    "\tif err != nil || dep.UserID != userID {\n"
    "\t\t// 两类结论共用一句文案：分开写就等于回「这条 id 存在，但不是你的」\n"
    "\t\treturn invalidInput(\"前置任务不存在\")\n"
    "\t}\n"
)
# 「只查存在不查主人」= 本批最容易出现的"修了但没修全"形状。
# 注码必须把 dep 一并换掉：只删条件里的归属判据会留下 declared and not used，
# 那格红是编译红不是杀（第一版就在这两格上判不了定，见 §电池口径）。
EXISTENCE_ONLY = (
    "\t_, err := s.taskRepo.GetByIDAnyUser(ctx, *dependsOn)\n"
    "\tif err != nil {\n"
    "\t\treturn invalidInput(\"前置任务不存在\")\n"
    "\t}\n"
)
# 「两种结论分两句文案」＝重新劈成两个分支——注意不能只改那一句字面量：
# 归属门修好后「不存在」与「不是你的」共用同一个 return，改字面量会把两句一起改掉，
# 测试照样看到同一文案 ⇒ 那是一格恒等变异，锁不住任何东西（第一版 D3 就是这么存活的）。
SPLIT_MESSAGES = (
    "\tdep, err := s.taskRepo.GetByIDAnyUser(ctx, *dependsOn)\n"
    "\tif err != nil {\n"
    "\t\treturn invalidInput(\"前置任务不存在\")\n"
    "\t}\n"
    "\tif dep.UserID != userID {\n"
    "\t\treturn invalidInput(\"前置任务属于其他用户\")\n"
    "\t}\n"
)
SELF_RULE = "if *dependsOn == taskID {"
RUNTIME_LOOKUP = (
    "\tdep, err := s.taskRepo.GetByIDAnyUser(ctx, *t.DependsOnTaskID)\n"
    "\tif err != nil || dep.UserID != t.UserID {\n"
    "\t\treturn fmt.Errorf(\"%w: 前置任务不可用\", ErrDependencyNotMet)\n"
    "\t}\n"
)
RUNTIME_EXISTENCE_ONLY = (
    "\t_, err := s.taskRepo.GetByIDAnyUser(ctx, *t.DependsOnTaskID)\n"
    "\tif err != nil {\n"
    "\t\treturn fmt.Errorf(\"%w: 前置任务不可用\", ErrDependencyNotMet)\n"
    "\t}\n"
)


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1 次）：{old[:90]!r}")
    return text.replace(old, new, 1)


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
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path):
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-b19mut")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    p = subprocess.run(["go", "test", "./" + SVC + "/", "-run", GO_RUN, "-count=1", "-v"],
                       cwd=root, capture_output=True, text=True, timeout=1800, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = sorted(set(re.findall(r"^    --- FAIL: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    ran = len(re.findall(r"^=== RUN\s+(\S+)", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: (\S+)", out, re.M))
    return p.returncode, killed, ran, skipped, out


ALWAYS_REJECT = (
    "\tdep, err := s.taskRepo.GetByIDAnyUser(ctx, *dependsOn)\n"
    "\tif err != nil || dep.UserID == userID {\n"
    "\t\treturn invalidInput(\"前置任务不存在\")\n"
    "\t}\n"
)


def go_mutants():
    return [
        ("D1", "Create 入口不查归属（回到本批开始前的形状）",
         lambda s: sub_once(s, CREATE_CALL, "\t", "D1")),
        ("D2", "入口判据缩成「id 存在即可」",
         lambda s: sub_once(s, TARGET_LOOKUP, EXISTENCE_ONLY, "D2")),
        ("D3", "「不存在」与「不是你的」重新劈成两句文案",
         lambda s: sub_once(s, TARGET_LOOKUP, SPLIT_MESSAGES, "D3")),
        ("D4", "执行期兜底整块删掉",
         lambda s: sub_once(s, RUNTIME_LOOKUP, "", "D4")),
        ("D5", "判据写成一律拒绝（反向锁必须点名）",
         lambda s: sub_once(s, TARGET_LOOKUP, ALWAYS_REJECT, "D5")),
        ("D6", "执行期兜底只查存在不查主人",
         lambda s: sub_once(s, RUNTIME_LOOKUP, RUNTIME_EXISTENCE_ONLY, "D6")),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    dst = Path(args.clone) if args.clone else Path("/tmp/b19dep-mut")
    if dst.exists():
        raise SystemExit(f"{dst} 已存在（换 --clone 目录或先删）")
    dst.mkdir(parents=True)
    print(f"私有作业目录：{dst}", flush=True)

    clone = go_prepare(dst)
    tgt = clone / "user-server" / SVC / "task.go"
    origin = md5_bytes(tgt)

    rc, killed, ran, skipped, out = go_run(clone)
    if rc != 0 or ran == 0 or skipped != 0:
        print(out[-3000:])
        shutil.rmtree(dst, ignore_errors=True)
        raise SystemExit(f"控制组不成立 rc={rc} ran={ran} skip={skipped}——整轮判「无法判定」")
    print(f"[控制组] rc=0 ran={ran} skip=0 全绿\n", flush=True)

    survivors, broken = [], []
    try:
        for tag, desc, apply in go_mutants():
            src = read(tgt)
            mut = apply(src)
            tgt.write_text(mut, encoding="utf-8")
            rc, killed, ran, skipped, out = go_run(clone)
            if md5_bytes(tgt) == origin:
                broken.append(f"{tag} 注码未生效（文件与原内容一致）")
                continue
            tgt.write_text(src, encoding="utf-8")
            if md5_bytes(tgt) != origin:
                raise SystemExit(f"{tag} 还原失败：md5 与原文件不一致，已停机（克隆保留 {clone}）")
            if rc == 0:
                survivors.append(tag)
                print(f"{tag}  {desc:<46} 存活", flush=True)
            elif not killed:
                broken.append(f"{tag} 红了但没点名（多半是编译红，不算杀）")
                print(f"{tag}  {desc:<46} 无法判定\n{out[-1500:]}", flush=True)
            else:
                print(f"{tag}  {desc:<46} 杀掉  {' '.join(killed)}", flush=True)
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
        print(f"===== 电池判定：{len(survivors)} 格存活 = 归属门有洞：{' '.join(survivors)} =====")
        return 1
    print(f"===== 电池判定：{len(go_mutants())} 格逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
