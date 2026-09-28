#!/usr/bin/env python3
"""批19d 变异电池：会话域出口分流逐入口验牙。

为什么要有这条电池：批19d 把 session 的六个入口从「一律 404 会话不存在」接到共用分流上。
测试全绿只证明这六个入口现在报对了码，证明不了**每个入口各自**都被守着——
一处漏改（只改了 Get，Stop 仍写 404）在整批断言里也是绿的，因为断言是逐入口循环，
少一个入口循环就少一条腿，而循环体本身不会红。所以这里逐入口注码：
- S1..S6：把某一个入口单独退回旧的「一律 404」→ 该入口那条腿必须红（否则循环是假的）；
- S7：会话出口改成一律 500 → 反向锁必须红（真不存在的会话要拿回 404，不能被洗成服务端故障）；
- S8：404 主语写错（「任务不存在」）→ 状态码全对、文案却指向错的东西；
- S9/S10：把列表类出口也顺手接进分流 → 用户弹条里会出现驱动原文（列表的固定文案是对的）。

口径（沿用批16/17/18/19b/19c 电池）：控制组必须 rc==0、ran>0、skip==0；每格 cp 备份 + md5 还原；
锚点命中恰好一次（S1..S6 按 handler 起点定位各自那次命中，不做全局计数）；红了必须点得出测试名
（编译红不算杀）。只在私有 --shared 克隆里注码，绝不碰共享工作树（并行会话在里面提交）。
覆盖源复制后逐文件比 md5：装错树会让整轮结论作废（批19c 之后补的预检）。

用法：python3 scripts/mut_session_err_b19d.py [--keep] [--clone DIR]
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
    f"user-server/{CTRL}/session.go",
    f"user-server/{CTRL}/task.go",
    f"user-server/{CTRL}/cron.go",
    f"user-server/{CTRL}/cron_error_b19c_test.go",
    f"user-server/{CTRL}/session_error_b19d_test.go",
    f"user-server/{SVC}/task.go",   # NotFoundError/notFound 的定义处
    f"user-server/{SVC}/cron.go",
    f"user-server/{SVC}/dependency_b19_test.go",
]
GO_PKG = CTRL
GO_RUN = "Test"  # 整个 controller 包：改了共享出口必须连任务/触发器侧一起看

# 六个入口在文件里的定义顺序；注码按「该 handler 起点之后的第一次命中」定位
HANDLERS = ["Get", "ListSteps", "ListLogs", "Export", "Stop", "Confirm"]

ANSI = re.compile(r"\x1b\[[0-9;]*m")

CALL = "\t\tsessionErrToResponse(ctx, err)\n"
OLD_404 = '\t\tresponse.Error(ctx, http.StatusNotFound, "会话不存在")\n'
HELPER = '\tbaErrToResponse(ctx, err, "会话不存在")\n'
LIST_500 = '\t\tresponse.Error(ctx, http.StatusInternalServerError, "查询会话失败")\n'


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def md5(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1 次）：{old[:90]!r}")
    return text.replace(old, new, 1)


def sub_in_handler(text: str, handler: str, old: str, new: str, tag: str) -> str:
    """只替换 handler 函数体内的那一次命中：全局 count 对六个同形调用点没有意义。"""
    head = f"func (c *SessionController) {handler}(ctx *gin.Context) {{"
    if text.count(head) != 1:
        raise SystemExit(f"{tag} handler 锚点命中 {text.count(head)} 次（要求 1）：{head!r}")
    start = text.index(head)
    end = text.find("\nfunc ", start + len(head))
    if end == -1:
        end = len(text)
    body = text[start:end]
    if body.count(old) != 1:
        raise SystemExit(f"{tag} {handler} 体内锚点命中 {body.count(old)} 次（要求 1）：{old[:60]!r}")
    return text[:start] + body.replace(old, new, 1) + text[end:]


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
    env.setdefault("GOCACHE", "/tmp/gocache-b19dmut")
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
    cs = []
    for i, h in enumerate(HANDLERS, start=1):
        # h=h 默认参绑定：闭包里引循环变量的话，十格全打最后一个 handler（且照样绿）
        cs.append((
            f"S{i}", f"{h} 单独退回「一律 404 会话不存在」",
            lambda s, h=h: sub_in_handler(s, h, CALL, OLD_404, h),
        ))
    cs.append(("S7", "会话出口改成一律 500（真不存在也不给 404）", lambda s: sub_once(
        s, HELPER, '\tresponse.Error(ctx, http.StatusInternalServerError, err.Error())\n', "S7")))
    cs.append(("S8", "404 主语写成「任务不存在」", lambda s: sub_once(
        s, HELPER, '\tbaErrToResponse(ctx, err, "任务不存在")\n', "S8")))
    cs.append(("S9", "List 也接进分流（驱动原文弹给用户）", lambda s: sub_in_handler(
        s, "List", LIST_500, "\t\tsessionErrToResponse(ctx, err)\n", "S9")))
    cs.append(("S10", "ListByTask 也接进分流", lambda s: sub_in_handler(
        s, "ListByTask", LIST_500, "\t\tsessionErrToResponse(ctx, err)\n", "S10")))
    return cs


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    tmp, owned = workdir(args.clone or None, prefix="b19d-mut-", repo_root=ROOT)
    print(f"私有作业目录：{tmp}", flush=True)

    clone = go_prepare(tmp)
    SESS = clone / "user-server" / CTRL / "session.go"
    original = md5(SESS)
    sess_src = read(SESS)
    # 预检：六个入口的调用点确实各命中一次，列表文案也还是那句（注码前提不成立要当场停，别跑完才说无法判定）
    if sess_src.count(CALL) != 6 or sess_src.count(HELPER) != 1 or sess_src.count(LIST_500) != 2:
        raise SystemExit(f"锚点前提不成立：call={sess_src.count(CALL)} helper={sess_src.count(HELPER)} "
                         f"list500={sess_src.count(LIST_500)}")

    rc, killed, ran, skipped, out = go_run(clone)
    if rc != 0 or ran == 0 or skipped != 0:
        print(out[-3000:])
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
        raise SystemExit(f"控制组不成立 rc={rc} ran={ran} skip={skipped}——整轮判「无法判定」")
    print(f"[控制组] rc=0 ran={ran} skip=0 全绿\n", flush=True)

    survivors, broken = [], []
    try:
        for tag, desc, apply in cells():
            src = read(SESS)
            SESS.write_text(apply(src), encoding="utf-8")
            if md5(SESS) == original:
                broken.append(f"{tag} 注码未生效（文件与原内容一致）")
                continue
            rc, killed, ran, skipped, out = go_run(clone)
            SESS.write_text(src, encoding="utf-8")
            if md5(SESS) != original:
                raise SystemExit(f"{tag} 还原失败：md5 与原文件不一致，已停机（克隆保留 {clone}）")
            if rc == 0:
                survivors.append(tag)
                print(f"{tag:<4} {desc:<40} 存活", flush=True)
            elif not killed:
                broken.append(f"{tag} 红了但没点名（多半是编译红，不算杀）")
                print(f"{tag:<4} {desc:<40} 无法判定\n{out[-1500:]}", flush=True)
            else:
                print(f"{tag:<4} {desc:<40} 杀掉  {' '.join(killed)}", flush=True)
    finally:
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)

    print()
    if broken:
        print("===== 电池判定：以下格无法判定，须先修注码 =====")
        for b in broken:
            print("  " + b)
        return 2
    if survivors:
        print(f"===== 电池判定：{len(survivors)} 格存活 = 分流出口有洞：{' '.join(survivors)} =====")
        return 1
    print(f"===== 电池判定：{len(cells())} 格逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
