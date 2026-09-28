#!/usr/bin/env python3
"""批19h 变异电池：Brain 动作闸门的三道锁 + 一条循环性质，逐格验牙。

为什么要有这条电池：本批的修复全是「不信任输入」类的闸门（令牌轮换后改用新令牌、
渲染出口统一转义、LLM 动作名过白名单）。闸门这类代码有个共同的失效形态——
**它长得很像在工作**：函数在、调用点在、测试也在，只有判据被"顺手简化"的那天起它才不挡东西。
只看绿的话，下面每一种改法都能带着全绿出厂：
- oneof 少一个动作（J1–J3）：白名单是从 tag 反射出来的，tag 漂一个字母，
  闸门就把一个真能力判死；三道锁（手写名单 / prompt 动作表 / dispatch case 集合）
  从三个方向读同一处改动，缺一个方向就漏一类漂移。
- 解析不出 oneof 时默认放行（J4）：这是最省事也最坏的一种"兜底"，闸门会静默常开。
- 判据写反（J5）：未知动作畅通、已知动作被拒——绿测试只要断言单边就看不见。
- 拒绝文案不截断（J6）：回显的是模型原文，长幻觉会同时撑大 prompt 与折叠台账的键名。
- 落库前闸门退回「执行后失败」（J9–J10）：本批真正的病——库里留下永远失败的步骤行。
  J9/J10 是把循环还原成修复前的两条腿跑出来的：只有真 WS + 真库回读步数才看得见「几行」。

TestB19HBrainGateStillRejectsScreenshot 是故意的等价腿：它在 J9/J10 下仍然绿——
G17 那条 screenshot 拒绝本来就在，本批只是把它折进统一闸门，行为不许变。

口径（沿用批16/17/18/19g 各电池）：控制组必须 rc==0、ran==10、skip==0；每格写完即还原并比 md5；
锚点命中恰好一次；注码先过 gofmt -e（编译红不算杀）；红了必须点出**这一格该红的那条腿**。
只在私有 --shared 克隆里注码，绝不碰共享工作树（并行会话在里面提交）。

用法：python3 scripts/mut_brain_action_b19h.py [--keep] [--clone DIR]
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
REPO_DTO = "internal/browser_automation/dto"
REPO_SVC = "internal/browser_automation/service"
DTO = "user-server/" + REPO_DTO
SVC = "user-server/" + REPO_SVC
GO_OVERLAY = [
    f"{DTO}/task.go",
    f"{DTO}/step_action.go",
    f"{DTO}/step_action_b19h_test.go",
    f"{SVC}/brain_reliability.go",
    f"{SVC}/executor.go",
    f"{SVC}/brain_action_gate_b19h_test.go",
    f"{SVC}/brain_action_gate_loop_b19h_test.go",
]
GO_PKGS = ["./internal/browser_automation/dto/", "./internal/browser_automation/service/"]
GO_RUN = "TestB19H"
RUN_N = 10

ANSI = re.compile(r"\x1b\[[0-9;]*m")

# --- 注码用的锚点（全部来自磁盘上的当前实现，命中次数由 sub_once 把关） ---
TAG_REMOVE_CLICK = ('oneof=open_tab click type ', 'oneof=open_tab type ')
TAG_ADD_HOVER = ('assert query close_tab"', 'assert query close_tab hover"')
PARSER_FAILOPEN = (
    '\tvalue, ok := bindingOneof(field.Tag.Get("binding"))\n\tif !ok {\n\t\treturn out\n\t}',
    '\tvalue, ok := bindingOneof(field.Tag.Get("binding"))\n\tif !ok {\n\t\treturn map[string]bool{"hover": true}\n\t}',
)
PREDICATE_INVERT = (
    '\tif dto.IsKnownStepAction(action) {\n\t\treturn ""\n\t}',
    '\tif !dto.IsKnownStepAction(action) {\n\t\treturn ""\n\t}',
)
ECHO_UNBOUNDED = (
    'label := truncateRunes(strings.TrimSpace(action), 40, "…")',
    'label := strings.TrimSpace(action)',
)
# 步循环退回修复前：空 action 静默跳过 + 只有 screenshot 一条腿。
# 与真·旧代码的唯一差别是少了两行注释——注释不参与判定，行为逐字一致。
LOOP_REVERT = (
    '\t\t\t// 批19h：Brain 的 steps 是 LLM 原文 Unmarshal 出来的，REST 那条 oneof 校验在这条路上\n'
    '\t\t\t// 一行都不跑——落库前先过服务端闸门（G17 的 screenshot 闸是它的一条已存在的腿）。\n'
    '\t\t\tif rej := brainPlanStepRejection(it.Action); rej != "" {\n'
    '\t\t\t\tlogger.Warnf("[BrowserExec] brain 轮内步骤被服务端拒绝 session=%d: %s", session.ID, rej)\n'
    '\t\t\t\thistory = appendHistoryBounded(history, rej, foldedHistory)\n'
    '\t\t\t\tcontinue\n'
    '\t\t\t}',
    '\t\t\tif it.Action == "" {\n'
    '\t\t\t\tcontinue // LLM 偶发空步，跳过\n'
    '\t\t\t}\n'
    '\t\t\tif it.Action == "screenshot" {\n'
    '\t\t\t\tlogger.Warnf("[BrowserExec] brain 轮内 screenshot 被服务端拒绝（抢焦点）session=%d", session.ID)\n'
    '\t\t\t\thist := "screenshot → 被拒绝（Brain 模式禁止抢焦点截图，请用 snapshot/markdown 观察）"\n'
    '\t\t\t\thistory = appendHistoryBounded(history, hist, foldedHistory)\n'
    '\t\t\t\tcontinue\n'
    '\t\t\t}',
)

SET_LOCK = "TestB19HKnownStepActionSet"
EXACT_LOCK = "TestB19HIsKnownStepActionExactMatch"
FAILCLOSED_LOCK = "TestB19HWhitelistParserFailClosed"
PROMPT_LOCK = "TestB19HPromptActionsAreWhitelisted"
DISPATCH_LOCK = "TestB19HWhitelistMatchesDispatchCases"
LEGS_LOCK = "TestB19HBrainPlanStepRejectionLegs"
ECHO_LOCK = "TestB19HRejectionEchoBounded"
LOOP_LOCK = "TestB19HBrainGateNeverPersistsRejectedSteps"
MIXED_LOCK = "TestB19HBrainGateKeepsLegalStepsInMixedPlan"


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def md5(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def sub_once(pair: tuple, tag: str):
    old, new = pair

    def apply(text: str) -> str:
        n = text.count(old)
        if n != 1:
            raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1）：{old[:90]!r}")
        return text.replace(old, new, 1)

    return apply


def cells():
    """(格号, 目标文件键, 说明, 注码, 该红的那条腿)"""
    return [
        ("J1", "task", "oneof 少一个动作名（反射白名单跟着缩）", sub_once(TAG_REMOVE_CLICK, "J1"), SET_LOCK),
        ("J2", "task", "同一处漂移：prompt 动作表锁看得见吗", sub_once(TAG_REMOVE_CLICK, "J2"), PROMPT_LOCK),
        ("J3", "task", "同一处漂移：dispatch case 集合锁看得见吗", sub_once(TAG_REMOVE_CLICK, "J3"), DISPATCH_LOCK),
        ("J4", "step_action", "解析不出 oneof 时默认放行（闸门静默常开）", sub_once(PARSER_FAILOPEN, "J4"), FAILCLOSED_LOCK),
        ("J5", "brain_reliability", "闸门判据写反（放行未知、拒已知）", sub_once(PREDICATE_INVERT, "J5"), LEGS_LOCK),
        ("J6", "brain_reliability", "拒绝文案不回显截断（长幻觉进 prompt）", sub_once(ECHO_UNBOUNDED, "J6"), ECHO_LOCK),
        ("J7", "task", "oneof 多出执行器不认的动作名", sub_once(TAG_ADD_HOVER, "J7"), EXACT_LOCK),
        ("J8", "task", "同一处漂移：白名单 vs dispatch case 集合", sub_once(TAG_ADD_HOVER, "J8"), DISPATCH_LOCK),
        ("J9", "executor", "落库前闸门退回「执行后失败」", sub_once(LOOP_REVERT, "J9"), LOOP_LOCK),
        ("J10", "executor", "同一处回退：合法步与被拒步混在一轮", sub_once(LOOP_REVERT, "J10"), MIXED_LOCK),
    ]


def syntax_ok(src: str) -> tuple:
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
        if md5(src) != md5(tgt):
            leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
            raise SystemExit(f"覆盖后 md5 不一致（装错树/写盘失败）：{rel}")
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path):
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-b19hmut")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    p = subprocess.run(["go", "test", *GO_PKGS, "-run", GO_RUN, "-count=1", "-v"],
                       cwd=root, capture_output=True, text=True, timeout=2400, env=env)
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

    # 作业目录必须由 workdir() 现开（不传 --clone 时是 mkdtemp 的独占目录）：两轮共用一个克隆时，
    # 后起的那轮会把前一轮的树建没，前一轮转头就去注码**后一轮**的文件（实测两边结论全废）。
    # 早先这里靠自己拼 `/tmp/b19h-mut-<pid>` 求独占，那只挡住了"两轮"，没挡住 `--clone .`。
    tmp, owned = workdir(args.clone or None, prefix="b19h-mut-", repo_root=ROOT)
    print(f"私有作业目录：{tmp}", flush=True)

    clone = go_prepare(tmp, owned)
    dispose_at_exit(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    u = clone / "user-server"
    TARGET = {
        "task": u / REPO_DTO / "task.go",
        "step_action": u / REPO_DTO / "step_action.go",
        "brain_reliability": u / REPO_SVC / "brain_reliability.go",
        "executor": u / REPO_SVC / "executor.go",
    }
    for k, p in TARGET.items():
        if not p.exists():
            raise SystemExit(f"注码目标缺失（{k}={p}）：覆盖清单漏文件，本格会静默无效")
    originals = {p: md5(p) for p in TARGET.values()}
    texts = {k: read(v) for k, v in TARGET.items()}

    for tag, key, _desc, apply, _expect in cells():
        mutated = apply(texts[key])
        if mutated == texts[key]:
            raise SystemExit(f"{tag} 注码无效（替换后与原文件一致）")
        ok, err = syntax_ok(mutated)
        if not ok:
            raise SystemExit(f"{tag} 注码语法坏，跑出来只会是 build failed：{err[:200]}")
    print("[注码前置] 十格锚点各命中一次 + 注码后语法可解析\n", flush=True)

    rc, killed, ran, skipped, out = go_run(clone)
    if rc != 0 or ran != RUN_N or skipped != 0:
        print(out[-4000:])
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
            if md5(tgt) != originals[tgt]:
                leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
                raise SystemExit(f"{tag} 还原失败：md5 与原文件不一致，已停机（克隆保留 {clone}）")
            if rc == 0:
                survivors.append(tag)
                print(f"{tag:<4} {desc:<38} 存活", flush=True)
            elif expect not in killed:
                broken.append(f"{tag} 红了但没点出 {expect}（killed={killed or '空=编译红'}）")
                print(f"{tag:<4} {desc:<38} 无法判定\n{out[-1800:]}", flush=True)
            else:
                print(f"{tag:<4} {desc:<38} 杀掉  {' '.join(killed)}", flush=True)
    finally:
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)

    print()
    if broken:
        print("===== 电池判定：以下格无法判定，须先修电池 =====")
        for b in broken:
            print("  " + b)
        return 2
    if survivors:
        print(f"===== 电池判定：{len(survivors)} 格存活 = 闸门有洞：{' '.join(survivors)} =====")
        return 1
    print(f"===== 电池判定：{len(cells())} 格逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
