#!/usr/bin/env python3
"""T-P5-03 变异电池：Active 出域闸门的每一道"拦住"都必须真的拦得住。

为什么要有这条电池（而不是"测试全绿"就算完）：
本卡的形状是「一个节点 + 一条跨进程的回读链路 + 四五种拒绝处置」。这种形状最常见的假绿是
**拒绝路径其实没生效，只是恰好没有任何东西去试它**：例如幂等短路写坏了但没人重跑过节点、
点火回读那一跳断了但端到端用例其实走的是另一条支。逐格注码 = 把每条"拦住"各自改坏一次，
看哪条用例红、红得点不点名。

口径（沿用批16/17/18 电池）：
- 只在私有 `--shared` 克隆里注码，绝不碰共享工作树（并行会话在里面提交）。
- 控制组必须 rc==0、ran==该 runner 的登记数、skip==0，否则整轮判"无法判定"停机。
- 每格注码前断言锚点命中恰好一次；还原后逐文件比 md5，不一致立即停机。
- 每格都带一个 expect 杀手（用例名子串）。红了但杀手不在红名单里 = 判问题：
  那说明"改坏了这件事"是被别的用例偶然发现的，本卡真正的判据仍然没有牙。
- 变异体把测试改成编译不过 / 整个二进制连不上库，分别判 BUILD-BROKEN / ENV-BROKEN，
  两者都算"未杀掉"，且不许靠改期望蒙过去。

预期存活的格子：**无**。第一版这里登记过一格"预期存活"（X1 `OneID:` 传参），理由是
"服务侧只在 `customer_id` 为空时才用它查身份，本卡所有用例都带 customer_id ⇒ 没有用例能看见它"。
这个理由成立时的正确动作不是留档，而是补一条 `customer_id` 为空的用例把这条回退档变成可证的契约
（见 TestReachSend_ResolvesIdentityFromOneIDWhenExecutionHasNoCustomerID）—— 否则"回退档"三个字
就是"一段没人用过的代码"的美化说法，它在生产里到底能不能工作谁也不知道。
router 装配侧（M18~M20）原本登记为盲区，本卡改用**源码形状锁**补上：
`internal/router/reach_sender_assembly_test.go`。锁看得见"这一行在不在、次序对不对、
交的是不是同一个构造点"，看不见"这一行是否真的被执行到"—— 后者仍属装配期事实，
只能靠 `T-P5-03` 那三条 service 侧的 fail-closed 用例兜住方向（未装配 ⇒ 不发，不是放行）。

用法：python3 scripts/mut_reach_p503.py [--keep] [--clone DIR] [--only M1,M4]
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SVC = "internal/service"
RTR = "internal/router"
PREFIX = "user-server/"

# 本卡改动集 + 被 P1 格子注码的既有文件。克隆里没有本泳道的未提交改动，
# 必须整文件覆盖过去，被测的才是"工作区里这份代码"。
OVERLAY = [
    f"{PREFIX}{SVC}/sop_reach_send.go",
    f"{PREFIX}{SVC}/sop_reach_send_test.go",
    f"{PREFIX}{SVC}/sop.go",
    f"{PREFIX}{SVC}/sop_node_executor.go",
    f"{PREFIX}{SVC}/sop_node_executors.go",
    f"{PREFIX}{SVC}/sop_node_executors_test.go",
    f"{PREFIX}{SVC}/sop_dispatcher.go",
    f"{PREFIX}{SVC}/sop_compensation_inventory_test.go",
    f"{PREFIX}{SVC}/proactive_reach.go",
    f"{PREFIX}{SVC}/proactive_reach_gate_test.go",
    f"{PREFIX}{SVC}/sop_approval_resume.go",
    f"{PREFIX}{RTR}/service_routes.go",
    f"{PREFIX}{RTR}/reach_sender_assembly_test.go",
]

GO_PKG = "./" + SVC + "/"
GO_RUN = ("TestReachSend|TestSOPReachSender|TestNodeExecutorFiles"
          "|TestRegisterAllNodeExecutors_AllTypesRegistered"
          "|TestNodeExecutor_CompensationInventory")

ROUTER_PKG = "./" + RTR + "/"
ROUTER_RUN = "TestRouterWiresGatedSenderToSOPLane"

# runner 名 → (包, -run 正则, 控制组应有 `=== RUN` 行数)
# 控制组数字要实测填（2026-09-21 覆盖克隆：service=26、router=1）。改测试集时同步改：
# 数字只降不升通常意味着某格注码把测试编译坏了，而不是"少跑了一个也照样绿"。
RUNNERS = {
    "service": (GO_PKG, GO_RUN, 26),
    "router": (ROUTER_PKG, ROUTER_RUN, 1),
}

ANSI = re.compile(r"\x1b\[[0-9;]*m")
REACH = f"{PREFIX}{SVC}/sop_reach_send.go"
DISP = f"{PREFIX}{SVC}/sop_dispatcher.go"
SOPT = f"{PREFIX}{SVC}/sop.go"
REG = f"{PREFIX}{SVC}/sop_node_executors.go"
PR = f"{PREFIX}{SVC}/proactive_reach.go"
ROUTER = f"{PREFIX}{RTR}/service_routes.go"

# (代号, 说明, 文件, 原样, 改成, 预期杀手用例名子串；None = 预期存活, runner)
# 静态锁的格子必须用**删行**而不是 `if false {}`：源码形状类判据看不见"包起来的死代码"，
# 用它注码会得到一次假杀（行还在、计数还是 1）。
CELLS = [
    ("M1", "幂等短路失效（重跑/恢复重投会二次外发）", REACH,
     "if hasSideEffect(ec.Execution, sentKey) {",
     "if false && hasSideEffect(ec.Execution, sentKey) {",
     "TestReachSend_AlreadySentIsNotSentAgain"),
    ("M2", "内容空也去占用审批位并继续外发", REACH,
     'if content == "" {',
     'if false && content == "" {',
     "TestReachSend_EmptyContentFailsBeforeApproval"),
    ("M3", "审批运行时未装配时判\"完成且已批准\"（处置从失败变成放行状）", REACH,
     "\t\t\treturn reachSendFailure(nodeID,\n"
     '\t\t\t\t"reach_send: 审批运行时未装配（旗子 off / DB 句柄缺失），未放行"), nil\n',
     "\t\t\treturn &NodeExecResult{Status: NodeStatusCompleted, Output: model.JSONMap{"
     "ApprovalOutcomeStatusKey: model.ApprovalStatusApproved}}, nil\n",
     "TestReachSend_ApprovalRuntimeUnwiredFailsClosed"),
    ("M3b", "真·fail-open：未装配时跳过整个审批腿并**照常发出去**", REACH,
     "\t\tb := GetApprovalResumeBridge()\n"
     "\t\tif b == nil {\n"
     "\t\t\treturn reachSendFailure(nodeID,\n"
     '\t\t\t\t"reach_send: 审批运行时未装配（旗子 off / DB 句柄缺失），未放行"), nil\n'
     "\t\t}\n"
     "\t\tres, err := b.ExecuteApprovalWait(ctx, ec)\n"
     "\t\tif err != nil || res == nil {\n"
     "\t\t\treturn res, err\n"
     "\t\t}\n"
     "\t\tif res.Status != NodeStatusCompleted {\n"
     "\t\t\t// Waiting（挂起等裁决）与 Failed（入队失败/无库）都意味着\"这次没拿到批准\"⇒ 零外发。\n"
     "\t\t\treturn res, nil\n"
     "\t\t}\n"
     "\t\t// C2 的同步退化态（策略当场放行）：结论在这一次的 Output 里，带下去做回显。\n"
     "\t\toutcome = res.Output\n"
     "\t}",
     "\t\tb := GetApprovalResumeBridge()\n"
     "\t\tif b == nil {\n"
     "\t\t\toutcome = model.JSONMap{ApprovalOutcomeStatusKey: model.ApprovalStatusApproved}\n"
     "\t\t} else {\n"
     "\t\t\tres, err := b.ExecuteApprovalWait(ctx, ec)\n"
     "\t\t\tif err != nil || res == nil {\n"
     "\t\t\t\treturn res, err\n"
     "\t\t\t}\n"
     "\t\t\tif res.Status != NodeStatusCompleted {\n"
     "\t\t\t\treturn res, nil\n"
     "\t\t\t}\n"
     "\t\t\toutcome = res.Output\n"
     "\t\t}\n"
     "\t}",
     "TestReachSend_ApprovalRuntimeUnwiredFailsClosed"),
    ("M4", "外发服务未装配伪装成\"正常跳过\"", REACH,
     'return reachSendFailure(nodeID, "reach_send: 外发服务未装配（装配点未接住装了闸门的实例），未发送"), nil',
     'return &NodeExecResult{Status: NodeStatusSkipped, '
     'Output: model.JSONMap{outputKeyReachSkip: "sender_unwired"}}, nil',
     "TestReachSend_SenderUnwiredFailsClosed"),
    ("M5", "审批结论这一关整个失效（任何结论都当作已批准往下走）", REACH,
     "if status := outcomeString(outcome); status != model.ApprovalStatusApproved {",
     'if status := outcomeString(outcome); status == "no_such_approval_status" {',
     "TestReachSend_NonApprovedVerdictSendsNothing"),
    ("M5b", "只认 allowed 位不认状态位（把\"拒了\"读成放行）", REACH,
     "if status := outcomeString(outcome); status != model.ApprovalStatusApproved {",
     'if status := outcomeString(outcome); status == "" && outcome[ApprovalOutcomeAllowedKey] != true {',
     "TestReachSend_NonApprovedVerdictSendsNothing"),
    ("M6", "没有结论时不去问门、也不挂起（直接按空结论处置）", REACH,
     "if outcome == nil {",
     "if false && outcome == nil {",
     "TestReachSend_NoVerdictParksAndSendsNothing"),
    ("M7", "退订不再判跳过，落到 default 变成可重试失败", REACH,
     "case errors.Is(err, ErrDoNotContact):",
     "case false && errors.Is(err, ErrDoNotContact):",
     "TestReachSend_DoNotContactCustomerSendsNothing"),
    ("M8", "频控不再判跳过，落到 default 变成可重试失败", REACH,
     "case errors.Is(err, ErrReachCooldown):",
     "case false && errors.Is(err, ErrReachCooldown):",
     "TestReachSend_InsideCooldownSendsNothing"),
    ("M9", "发送前闸门拒绝落到 default ⇒ 回头重试再挂一次审批", REACH,
     "case errors.Is(err, ErrReachApprovalDenied):",
     "case false && errors.Is(err, ErrReachApprovalDenied):",
     "TestReachSend_PreSendGateDenialDoesNotRePark"),
    ("M10", "成功外发不写幂等键（重启后重投会二次发送）", REACH,
     "SideEffects: []string{sentKey},",
     "SideEffects: nil,",
     "TestReachSend_ApprovedVerdictSendsOnceThroughCustomerPath"),
    ("M11", "发出去的渠道不回显（事后查不到这次发到哪个渠道）", REACH,
     "out[outputKeyReachChannel] = resp.Channel",
     'out[outputKeyReachChannel] = ""',
     "TestReachSend_ApprovedVerdictSendsOnceThroughCustomerPath"),
    ("M12", "图上的渠道偏好传不出去（选路自己漂回默认顺序）", REACH,
     "PreferredChannels: reachNodeStringSliceConfig(ec.Node.Config, reachNodeFieldPreferredChannel),",
     "PreferredChannels: nil,",
     "TestReachSend_PreferredChannelReachesTheService"),
    ("M13", "点火后不把审批结论递回执行器（本卡新接的那一跳失效）", DISP,
     "if task.TimerFired && task.WaitEvent == WaitEventApproval {",
     "if false && task.TimerFired && task.WaitEvent == WaitEventApproval {",
     "TestReachSendEndToEnd_ApproveThenFireSendsExactlyOnce"),
    ("M14", "回读了结论但丢掉（同族的另一半：赋值那一侧）", DISP,
     "approvalOutcome = GetApprovalResumeBridge().ResolveOnFire(ctx, task)",
     "approvalOutcome = nil",
     "TestReachSendEndToEnd_ApproveThenFireSendsExactlyOnce"),
    ("M15", "点火短路误伤外发节点（把 reach_send 当 wait 一样跳过）", DISP,
     "if (task.SkipWait || task.TimerFired) && node.Type == SOPNodeTypeWait {",
     "if (task.SkipWait || task.TimerFired) && (node.Type == SOPNodeTypeWait || node.Type == SOPNodeTypeReachSend) {",
     "TestReachSendEndToEnd_ApproveThenFireSendsExactlyOnce"),
    ("M16", "节点类型没登记进受支持集合（图存不进去）", SOPT,
     "\tSOPNodeTypeReachSend: true,\n",
     "",
     "TestReachSendEndToEnd_ApproveThenFireSendsExactlyOnce"),
    ("M17", "执行器漏注册（图上这一步静默退化成 Noop=完成）", REG,
     "\treg(NewReachSendExecutor())",
     "\tif false {\n\t\treg(NewReachSendExecutor())\n\t}",
     "TestReachSend_RegisteredInExecutorRegistry"),
    ("P1", "闸门判定键改从执行数据取（图里的一步可伪造归因键）", PR,
     "Key:   reachApprovalKey(customer.UnifiedID, customer.ID, channel, recipient),",
     "Key:   reachApprovalKey(req.OneID, customer.ID, channel, recipient),",
     "TestReachSend_GateKeyComesFromCustomerRowNotExecutionData"),
    ("X1", "OneID 传参删掉（执行行没有 customer_id 时再也查不到人）", REACH,
     "OneID:             reachNodeStringConfig(ec.ExecutionData, reachNodeOneIDKey),",
     'OneID:             reachNodeStringConfig(ec.ExecutionData, "oneID_wrong_key"),',
     "TestReachSend_ResolvesIdentityFromOneIDWhenExecutionHasNoCustomerID"),
    # —— router 装配侧（跑 ./internal/router/ 的源码形状锁）——
    ("M18", "装配点整行删掉（图上的外发从此永远\"未装配\"，service 用例一条不红）", ROUTER,
     "\tservice.SetSOPReachSender(proactiveSvc)\n", "",
     "TestRouterWiresGatedSenderToSOPLane"),
    ("M19", "先借后装：sender 装在了闸门之前（借出去一个没装 W-1 门的实例）", ROUTER,
     "\tif !app.AttachReachGate(proactiveSvc) {\n"
     "\t\tapp.LogReachGateSkippedAssemblyPoint(\"router.setupProactiveReachRoutes\")\n"
     "\t}\n"
     "\t// T-P5-03：SOP 的 reach_send 节点复用**这同一个**已装闸门实例，而不是自己 new 一个。\n"
     "\t// 注册节点执行器发生在 cmd/api 启动期的 InitSOPExecutionDispatcher 调用点（早于此处），\n"
     "\t// 所以只能像上面那样事后注入全局；\n"
     "\t// 若这里改成新建实例，退订/频控/闸门三判据就会出现两套互相看不见对方状态的手工装配路径。\n"
     "\tservice.SetSOPReachSender(proactiveSvc)\n",
     "\tservice.SetSOPReachSender(proactiveSvc)\n"
     "\tif !app.AttachReachGate(proactiveSvc) {\n"
     "\t\tapp.LogReachGateSkippedAssemblyPoint(\"router.setupProactiveReachRoutes\")\n"
     "\t}\n",
     "TestRouterWiresGatedSenderToSOPLane"),
    ("M20", "交给 SOP 侧的是**另一个**没装门的外发实例（发得出短信，只是门不在之路上）", ROUTER,
     "\tservice.SetSOPReachSender(proactiveSvc)\n",
     "\tservice.SetSOPReachSender(service.NewProactiveReachService(db, nil))\n",
     "TestRouterWiresGatedSenderToSOPLane"),
]

# 格子默认跑 service 包那套行为用例；这几格跑 router 包的源码形状锁。
CELL_RUNNER = {"M18": "router", "M19": "router", "M20": "router"}

TALLY = ("KILLED", "SURVIVED", "RED-UNNAMED", "BUILD-BROKEN", "ENV-BROKEN", "NO-RUN")


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1 次）：{old[:90]!r}")
    return text.replace(old, new, 1)


def prepare(dst: Path) -> Path:
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
    for rel in OVERLAY:
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


def go_run(clone: Path, runner: str):
    pkg, run, _ = RUNNERS[runner]
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOFLAGS", "-mod=mod")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    try:
        p = subprocess.run(["go", "test", pkg, "-run", run, "-count=1", "-v"],
                           cwd=root, capture_output=True, text=True, timeout=1800, env=env)
        out, rc = p.stdout + p.stderr, p.returncode
    except subprocess.TimeoutExpired as e:
        out, rc = (e.stdout or "") + (e.stderr or ""), -9
    out = ANSI.sub("", out)
    killed = sorted(set(re.findall(r"^    --- FAIL: ([^\s/]+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    ran = len(re.findall(r"^=== RUN\s+(\S+)", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: (\S+)", out, re.M))
    return rc, killed, ran, skipped, out


def classify(rc: int, killed: list, ran: int, skipped: int, out: str, expect_ran: int) -> str:
    if "connection refused" in out or "dial tcp" in out or "no such host" in out:
        return "ENV-BROKEN"
    if "[build failed]" in out or "cannot find" in out or "undefined:" in out or rc == -9:
        return "BUILD-BROKEN"
    if ran == 0:
        return "NO-RUN"
    if skipped > 0:
        return "ENV-BROKEN"
    if killed:
        # 注意：panic 会中止二进制 ⇒ 后面的用例一条都不 RUN。这里仍判"杀掉"（红因是真的），
        # 但 main 里会因 ran 少于控制组而额外记一条问题，逼人工看一眼红因。
        return "KILLED"
    if ran < expect_ran:
        # 少跑到 = 有用例在注码后的二进制里根本没参与（编译坏半截、或 -run 名单被改窄）
        return "NO-RUN"
    if rc != 0:
        return "RED-UNNAMED"
    return "SURVIVED"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    ap.add_argument("--only", default="", help="只跑这些代号（逗号分隔），用于定位问题")
    args = ap.parse_args()

    cells = CELLS
    if args.only:
        want = {c.strip() for c in args.only.split(",") if c.strip()}
        cells = [c for c in CELLS if c[0] in want]
        missing = want - {c[0] for c in cells}
        if missing:
            raise SystemExit(f"未知代号：{sorted(missing)}")

    tmp = Path(args.clone or tempfile.mkdtemp(prefix="p503mut-"))
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}")
    clone = prepare(tmp)

    rels = sorted({c[2] for c in cells})
    files = {rel: clone / rel for rel in rels}
    originals = {rel: read(p) for rel, p in files.items()}
    basemd5 = {rel: md5_bytes(p) for rel, p in files.items()}

    def control(name: str) -> None:
        rc, killed, ran, skipped, out = go_run(clone, name)
        bad = rc != 0 or ran != RUNNERS[name][2] or skipped or killed
        # 控制组没有"注码"这回事 ⇒ 借用格子口径打出的 SURVIVED 会被读成"有一格活下来了"，
        # 所以这里单独给一个 CLEAN/DIRTY 标签，判据本身不变。
        print(f"控制组[{name}] {'DIRTY' if bad else 'CLEAN'} rc={rc} "
              f"ran={ran}/{RUNNERS[name][2]} skip={skipped} FAIL={killed}")
        if bad:
            print(out[-4000:])
            raise SystemExit(f"控制组[{name}] 不干净——它下游所有格子的红/绿都不可信"
                             "（ran 对不上多半是某格没编译或被过滤掉）")

    control("service")
    control("router")

    problems: list[str] = []
    tally = {k: 0 for k in TALLY}
    killmap: dict[str, set] = {}
    for code, desc, rel, old, new, expect in cells:
        runner = CELL_RUNNER.get(code, "service")
        expect_ran = RUNNERS[runner][2]
        try:
            files[rel].write_text(sub_once(originals[rel], old, new, code))
        except SystemExit as e:
            problems.append(str(e))
            continue
        rc, killed, ran, skipped, out = go_run(clone, runner)
        v = classify(rc, killed, ran, skipped, out, expect_ran)
        tally[v] += 1
        killmap[code] = set(killed)
        if v == "KILLED" and ran < expect_ran:
            problems.append(f"{code} 杀了但 ran={ran}<{expect_ran}：疑似 panic 中止，红因要人工看")

        if v == "KILLED":
            if ran == 0:
                problems.append(f"{code} 杀红了但一个用例都没跑（判据不成立）")
            if expect is None:
                print(f"{code:<4} {desc[:54]:<56} 被杀（预期存活，说明该格其实有牙）｜ {len(killed)} 条")
            elif not any(expect in k for k in killed):
                problems.append(f"{code} 红了但不是预期杀手发现的（{expect}）：{sorted(killed)[:4]}")
            else:
                print(f"{code:<4} {desc[:54]:<56} 杀掉 by {expect}｜共 {len(killed)} 条")
        elif v == "SURVIVED":
            if expect is None:
                print(f"{code:<4} {desc[:54]:<56} 存活（预期，已登记盲区）")
            else:
                problems.append(f"{code} 存活 = 洞：{desc}")
                print(f"{code:<4} {desc[:54]:<56} 存活=洞 ran={ran} rc={rc}")
                print(out[-2500:])
        else:
            problems.append(f"{code} 判为 {v}（不是干净的\"杀掉\"）：{desc}")
            print(f"{code:<4} {desc[:54]:<56} {v} ran={ran} skip={skipped} rc={rc}")
            print(out[-2500:])

        files[rel].write_text(originals[rel])
        if md5_bytes(files[rel]) != basemd5[rel]:
            raise SystemExit(f"{code} 还原后 md5 不一致，停机")

    # 同族提示：两格杀掉完全相同的用例集合 ⇒ 其中一格可能是冗余的（只报，不判负）
    items = sorted(killmap.items())
    for i in range(len(items)):
        for j in range(i + 1, len(items)):
            if items[i][1] and items[i][1] == items[j][1]:
                print(f"  [同族] {items[i][0]} 与 {items[j][0]} 杀掉的用例集合相同（{len(items[i][1])} 条）")

    print("\n计数：", " ".join(f"{k}={tally[k]}" for k in TALLY))
    print("全部格子已还原（逐文件 md5 与基线一致）")

    if not args.keep:
        shutil.rmtree(tmp, ignore_errors=True)
    if problems:
        print("\n===== 电池判定：有洞 =====")
        for x in problems:
            print("  ✗", x)
        return 1
    print(f"\n===== 电池判定：{len(cells)} 格逐格核验，无未登记存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
