#!/usr/bin/env python3
"""批19c 变异电池：错误分类出口（404/400/409/500 分流）逐分支验牙。

为什么要有这条电池：本批改的是"结论的分类"，不是行为分支——测试全绿只说明
四条腿现在报对了码，说明不了每一条码都有人守着。分类出口最常被三种偷懒改坏：
- 把某类判据悄悄变成永不命中（C1/C2）：那类结论就掉回 default，用户重新看到 500/400；
- 把真故障洗成客户端错误（C3）：监控面上再也看不见这条故障；
- 把主语写错（C4/C5）：状态码全对，用户却被告知"触发器不存在"（其实是任务不归他）。
C6/C7/C8 守服务侧那句"结论有没有带对类型"——分类出口再对，服务层把 404 说成 400、
把 409 说成 400，前端拿到的就是另一条排查路径（等一等 vs 换一条任务）。

口径（沿用批16/17/18/19b 电池）：控制组必须 rc==0、ran>0、skip==0；每格 cp 备份 + md5 还原；
锚点命中恰好一次；红了必须点得出测试名（编译红不算杀）——注码要自己保证编译得过
（删分支就同时删掉那条分支独占的 var，否则就是 build failed，第一版在别的电池上吃过这亏）。
只在私有 --shared 克隆里注码，绝不碰共享工作树（并行会话在里面提交）。

用法：python3 scripts/mut_cron_err_b19c.py [--keep] [--clone DIR]
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
GO_OVERLAY = [
    f"user-server/{CTRL}/task.go",
    f"user-server/{CTRL}/cron.go",
    f"user-server/{CTRL}/cron_error_b19c_test.go",
    f"user-server/{SVC}/cron.go",
    f"user-server/{SVC}/task.go",  # NotFoundError/notFound 的定义处，不覆盖就是编译红
]
GO_PKG = CTRL
# 分流本体 + 共用它的任务侧既有契约（改了共享出口必须连任务侧一起看）
GO_RUN = "|".join([
    "TestCron", "TestTaskNotFoundWordingUnchanged", "TestPreconditionErrorsAreNotReportedAsServerFaults",
    "TestOtherConflictsKeepGenericCode", "TestHostOfflineAndUserBusyCarryDistinctCodes",
    "TestOfflineHostRefusesRunAtRequestTime", "TestOtherDomainErrorsStillReachService",
])

ANSI = re.compile(r"\x1b\[[0-9;]*m")

NF_VAR = "\t\tvar nf *basvc.NotFoundError\n"
NF_CASE = ("\t\tcase errors.As(err, &nf):\n"
           "\t\t\tresponse.Error(ctx, http.StatusNotFound, nf.Error())\n")
GORM_CASE = "case errors.Is(err, gorm.ErrRecordNotFound):"
UNKNOWN_500 = "response.Error(ctx, http.StatusInternalServerError, err.Error())"
CRON_404 = 'baErrToResponse(ctx, err, "触发器不存在")'
TASK_404 = 'baErrToResponse(ctx, err, "任务不存在")'
CRON_NOTFOUND = 'return nil, notFound("任务不存在")'
CRON_DUP = 'return nil, stateConflict("该任务已存在触发器")'
CRON_TYPE = 'return nil, invalidInput("仅 cron 类型任务可配置定时触发器")'


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1 次）：{old[:90]!r}")
    return text.replace(old, new, 1)


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
    b = subprocess.run(["git", "checkout", "-f", "master"], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if b.returncode != 0:
        bail("checkout 失败：" + (b.stdout + b.stderr)[-400:])
    for rel in GO_OVERLAY:
        src = ROOT / rel
        if not src.exists():
            bail(f"覆盖源缺失：{src}")
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
    env.setdefault("GOCACHE", "/tmp/gocache-b19cmut")
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


def cells():
    def drop_nf(s):
        s = sub_once(s, NF_VAR, "", "C1a")
        return sub_once(s, NF_CASE, "", "C1b")

    return [
        ("C1", CTRL, "NotFoundError 那一类掉回 default（= 又变 500）", drop_nf),
        ("C2", CTRL, "404 判据换成永不命中的哨兵", lambda s: sub_once(
            s, GORM_CASE, "case errors.Is(err, gorm.ErrInvalidData):", "C2")),
        ("C3", CTRL, "真故障洗成客户端错误", lambda s: sub_once(
            s, UNKNOWN_500, "response.Error(ctx, http.StatusBadRequest, err.Error())", "C3")),
        ("C4", CTRL, "触发器侧 404 说成「任务不存在」", lambda s: sub_once(
            s, CRON_404, 'baErrToResponse(ctx, err, "任务不存在")', "C4")),
        ("C5", CTRL, "任务侧 404 说成「触发器不存在」", lambda s: sub_once(
            s, TASK_404, 'baErrToResponse(ctx, err, "触发器不存在")', "C5")),
        ("C6", SVC, "创建时「任务不存在」带错类型（404 变 400）", lambda s: sub_once(
            s, CRON_NOTFOUND, 'return nil, invalidInput("任务不存在")', "C6")),
        ("C7", SVC, "重复触发器归到入参不合法", lambda s: sub_once(
            s, CRON_DUP, 'return nil, invalidInput("该任务已存在触发器")', "C7")),
        ("C8", SVC, "选错任务归成状态冲突", lambda s: sub_once(
            s, CRON_TYPE, 'return nil, stateConflict("仅 cron 类型任务可配置定时触发器")', "C8")),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    tmp, owned = workdir(args.clone or None, prefix="b19c-mut-", repo_root=ROOT)
    print(f"私有作业目录：{tmp}", flush=True)

    clone = go_prepare(tmp, owned)
    u = clone / "user-server"
    CT, CC, SC = u / CTRL / "task.go", u / CTRL / "cron.go", u / SVC / "cron.go"
    TARGET = {"C1": CT, "C2": CT, "C3": CT, "C4": CC, "C5": CT, "C6": SC, "C7": SC, "C8": SC}
    originals = {p: md5(p) for p in {CT, CC, SC}}

    rc, killed, ran, skipped, out = go_run(clone)
    if rc != 0 or ran == 0 or skipped != 0:
        print(out[-3000:])
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
        raise SystemExit(f"控制组不成立 rc={rc} ran={ran} skip={skipped}——整轮判「无法判定」")
    print(f"[控制组] rc=0 ran={ran} skip=0 全绿\n", flush=True)

    survivors, broken = [], []
    try:
        for tag, _pkg, desc, apply in cells():
            tgt = TARGET[tag]
            src = read(tgt)
            tgt.write_text(apply(src), encoding="utf-8")
            if md5(tgt) == originals[tgt]:
                broken.append(f"{tag} 注码未生效（文件与原内容一致）")
                continue
            rc, killed, ran, skipped, out = go_run(clone)
            tgt.write_text(src, encoding="utf-8")
            if md5(tgt) != originals[tgt]:
                raise SystemExit(f"{tag} 还原失败：md5 与原文件不一致，已停机（克隆保留 {clone}）")
            if rc == 0:
                survivors.append(tag)
                print(f"{tag}  {desc:<44} 存活", flush=True)
            elif not killed:
                broken.append(f"{tag} 红了但没点名（多半是编译红，不算杀）")
                print(f"{tag}  {desc:<44} 无法判定\n{out[-1500:]}", flush=True)
            else:
                print(f"{tag}  {desc:<44} 杀掉  {' '.join(killed)}", flush=True)
    finally:
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)

    print()
    if broken:
        print("===== 电池判定：以下格无法判定，须先修注码 =====")
        for b in broken:
            print("  " + b)
        return 2
    if survivors:
        print(f"===== 电池判定：{len(survivors)} 格存活 = 分类出口有洞：{' '.join(survivors)} =====")
        return 1
    print(f"===== 电池判定：{len(cells())} 格逐格被杀，无存活 =====")
    return 0


def md5(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


if __name__ == "__main__":
    sys.exit(main())
