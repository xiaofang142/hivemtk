#!/usr/bin/env python3
"""批16 反向电池：写台账静默失效收口（A7 降级 / A8 fail-close / A11 三态）的八处变异。

口径（仓库律）：
- 只认带 `--- FAIL:` 的名字；`[build failed]` / `cannot find package` / `undefined:` 一律判「无法判定」；
- 每腿带对照自证 ran==期望 且 skip==0，否则同样判无法判定（绿但没跑过不算证据）；
- 变异用 cp 备份 + 每步 md5 比对还原，绝不 git checkout/restore。

运行位置：默认打本仓工作树（`<repo>/user-server`）；工作树被并行会话的脏文件挡住编译时改用克隆：

    MUT_ROOT=/path/to/clone/user-server python3 scripts/mut_ledger_b16.py --check
    MUT_ROOT=/path/to/clone/user-server python3 scripts/mut_ledger_b16.py

与 §7.8 留档那一轮的差别（留档证据由 /tmp 版本产出，本副本新增六件事）：
① `MUT_ROOT` 覆盖 + 项目根由 `__file__` 反推（不再硬编码克隆路径与仓名）；
② 启动时按遗留 `.bak` 还原（上一轮若被 kill，基线 md5 会取到**被注码的**文本，之后每次
   「还原」都在还原缺陷——这是本批审自己工具时发现的第二个假绿源）；
③ `--check` 先验锚点唯一（M8 的区域文本运行时现取，锚点错一条就是白等半小时）；
④ 每条红打印测试自己写的原因行，含超时/panic 时不判「已杀」（负载红 ≠ 变异被杀）；
⑤ 注码后必须真的改到字节（锚点计数过了但 md5 没变 = 替换没落地 = 一条没有牙齿的腿）；
⑥ 红集合必须**恰好**等于 must（旧口径只查 must ⊆ failed，任何把整包打红的注码都会被记成
   「已杀」——那是把「这条线有腿守着」偷换成「今天很吵」），且 rc=1 无名不判杀。

**本电池不是门禁**：它只跑 TESTS 里列的腿（`-run` 过滤子集）。提交门禁是干净克隆里的
`go build ./... && go vet ./... && go test ./...`。
"""
import hashlib
import os
import re
import shutil
import subprocess
import sys

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ROOT = os.environ.get("MUT_ROOT") or os.path.join(REPO, "user-server")
SVC = os.path.join(ROOT, "internal/browser_automation/service")
ENV = dict(os.environ)
ENV["CGO_ENABLED"] = "0"

TESTS = [
    "TestWSE2E_LedgerWriteFailureAbortsBeforeSend",
    "TestWSE2E_LedgerWriteFailureDegradesRestOfSession",
    "TestWSE2E_SentLedgerGapStillBlocksRetryRound",
    "TestWSE2E_GuardQueryFailureFailsClosed",
    "TestWSE2E_UnknownLocatorTableTreatedAsWrite",
    "TestWSE2E_RegisteredPlatformNonMatchingStepStaysReadOnly",
    "TestClassifyStepEffectThreeStates",
    # 批14 的静态顺序锁：批16 改了 recordSubmitState 签名后它的锚点从「带参数尾巴」收窄成
    # 只认状态 token，收窄后的锁必须仍能杀掉「闸门晚于落账」这类顺序改动，否则就是假锁。
    "TestSendGateOrderedBeforeSentLedger",
]
EXPECT_RAN = len(TESTS)
BAK_SUFFIX = ".b16bak"

# 变异定义：(名字, 文件, 原文, 替身, 必须点名的用例)
MUTS = [
    ("M1 prepared 台账写失败不再拦下 send", "executor.go",
     '\t\t\treturn nil, fmt.Errorf("post_comment 未提交（prepared 台账未落，点击从未发生，本会话写能力已降级）: %w", err)',
     '\t\t\t_ = err',
     {"TestWSE2E_LedgerWriteFailureAbortsBeforeSend",
      "TestWSE2E_LedgerWriteFailureDegradesRestOfSession"}),
    ("M2 会话级降级检查摘掉", "executor.go",
     "\t\tif why, broken := e.ledgerBrokenReason(session.ID); broken {",
     '\t\tif why, broken := "", false; broken {',
     {"TestWSE2E_LedgerWriteFailureDegradesRestOfSession"}),
    ("M3 越点未落账的进程内兜底不再记", "write_ledger.go",
     "\tif crossed {\n\t\te.rememberLedgerGap(taskID, textHash)\n\t}",
     "\tif false && crossed {\n\t\te.rememberLedgerGap(taskID, textHash)\n\t}",
     {"TestWSE2E_SentLedgerGapStillBlocksRetryRound"}),
    ("M4 双发闸查询失败退回 fail-open", "write_ledger.go",
     '\t\treturn fmt.Errorf("双发闸查询失败（%v）——无法确认同文本是否已有提交尝试，本写步拒绝下发；库恢复后重跑本任务即可", err)',
     "\t\treturn nil",
     {"TestWSE2E_GuardQueryFailureFailsClosed"}),
    ("M5 定位表报错不再升未知态", "write_ledger.go",
     "\t\tif stepCouldBeSubmit(step) {",
     "\t\tif false && stepCouldBeSubmit(step) {",
     {"TestWSE2E_UnknownLocatorTableTreatedAsWrite", "TestClassifyStepEffectThreeStates"}),
    ("M6 越点未落账的步仍可报成功", "executor.go",
     "\t\tif ledgerErr := errors.Join(sentLedgerErr, finalLedgerErr); ledgerErr != nil {",
     "\t\tif ledgerErr := errors.Join(sentLedgerErr, finalLedgerErr); false && ledgerErr != nil {",
     {"TestWSE2E_SentLedgerGapStillBlocksRetryRound"}),
    # M7 钉的是调用点实参：M3 已在 helper 里证明「不记缺口会漏」，但 helper 正确、
    # 调用点传 crossed=false 同样是漏——两处任一处松掉都必须有人红。
    ("M7 sent 落账调用点不再标 crossed", "executor.go",
     "model.StepSubmitSent, textHash, true)",
     "model.StepSubmitSent, textHash, false)",
     {"TestWSE2E_SentLedgerGapStillBlocksRetryRound"}),
]


EQUIV = {
    # M7 首轮跑出「全绿=存活」。判据（不是"测试没想到"，而是"这条差异不可达"）：
    # post_comment 里 sent 之后**没有**任何提前 return —— 一路走到 finalLedgerErr 那次写，
    # 而它带着同一个 (taskID|textHash) 键、crossed=true。所以：
    #   sent 写失败 + final 写也失败 → 两处都记 gap，键相同（rememberLedgerGap 去重）→ 等价；
    #   sent 写失败 + final 写成功   → 库里那行已在拦阻集合内，DB 闸门自己就拦 → 等价。
    # 也就是说 sent 那处的 crossed 实参今天是「冗余但语义正确」的一格：它声明的是
    # 「这次写跨越了不可逆点」，这个事实不依赖后一次写是否存在。保留，别为了让 M7 变红去改产码。
    # 反之，若将来有人在 sent 与终态写之间插一条提前 return，M7 立即从等价变成真漏——
    # 那时这条判据就必须重跑。
    "M7 sent 落账调用点不再标 crossed":
        "等价类：sent 与终态写同键、终态写恒可达，缺口只会多记不会漏记（理由见脚本内注释）",
}


def order_swap_mutation():
    """M8：把闸门早返整块挪到「落 sent 台账」之后——批14 静态顺序锁必须点名咬住。

    区域文本运行时从文件里取、不硬抄：抄错一个标点就是一条假「无法判定」。
    区域必须**含**sent 那一句整行：首轮我把 old 截到该行之前、又在 new 里复制了一遍该行，
    于是 `sentLedgerErr` 声明两次、编译期就被抬走（executor.go:1010: no new variables），
    那是注码自身的缺陷，不是顺序锁抓住了缺陷。
    """
    p = os.path.join(SVC, "executor.go")
    src = open(p, encoding="utf-8").read()
    i = src.index("\t\tif isSendGateReject(sendErr) {")
    k = src.index("\t\t}\n", i) + len("\t\t}\n")
    j = src.index("sentLedgerErr := e.recordSubmitState", i)
    j_end = src.index("\n", j)
    gate_block = src[i:k]
    tail = src[k:j]
    sent_line = src[j:j_end+1]
    # old 必须是**连续**片段（首轮我把 tail 结尾的缩进 rstrip 掉再拼 sent 行，锚点当场 0 命中）
    return ("M8 闸门早返挪到落 sent 之后", "executor.go",
            src[i:j_end+1], sent_line + gate_block + tail.rstrip() + "\n",
            {"TestSendGateOrderedBeforeSentLedger"})


SRC = ["executor.go", "write_ledger.go"]
RUN_RE = re.compile(r"^=== RUN\s+(\S+)", re.M)
FAIL_RE = re.compile(r"^--- FAIL:\s+(\S+)", re.M)
SKIP_RE = re.compile(r"^--- SKIP:\s+(\S+)", re.M)
REASON_RE = re.compile(r"^\s{2,}\S+\.go:\d+:\s(.+)$", re.M)
TIMEOUT_RE = re.compile(r"test timed out after|panic: |^\s+context deadline exceeded", re.M)


def md5(path):
    h = hashlib.md5()
    with open(path, "rb") as f:
        h.update(f.read())
    return h.hexdigest()


def run_tests():
    cmd = ["go", "test", "-count=1", "-timeout", "900s", "-test.v",
           "-run", "^(" + "|".join(TESTS) + ")$", "./internal/browser_automation/service/"]
    p = subprocess.run(cmd, cwd=ROOT, env=ENV, capture_output=True, text=True)
    out = p.stdout + p.stderr
    ran = {m for m in RUN_RE.findall(out) if m in TESTS}
    failed = {m.split("/")[0] for m in FAIL_RE.findall(out)}
    skipped = {m for m in SKIP_RE.findall(out) if m in TESTS}
    return p.returncode, ran, failed, skipped, out


def verdict(tag, rc, ran, failed, skipped, out, must):
    if "[build failed]" in out or "cannot find package" in out or "undefined:" in out \
            or "expected declaration" in out or "declared and not used" in out:
        print(f"{tag} → 无法判定（编译/语法问题，不是变异被杀）")
        print("\n".join(out.splitlines()[-12:]))
        return False
    if skipped or len(ran) != EXPECT_RAN:
        print(f"{tag} → 无法判定（ran={len(ran)}/{EXPECT_RAN} skip={len(skipped)}）")
        return False
    if rc == 1 and TIMEOUT_RE.search(out):
        print(f"{tag} rc=1 → 无法判定：输出里有超时/panic 字样，这条红必须人工读过原因再算不算杀")
        for line in REASON_RE.findall(out)[:3]:
            print(f"   红因: {line[:170]}")
        return False
    if rc == 0:
        print(f"{tag} rc=0 ran={len(ran)}/{EXPECT_RAN} skip=0 → 绿（变异存活，要就是真洞、要就登记等价类理由）")
        return False
    if not failed:
        # rc=1 却无名：red 但没有 `--- FAIL:`（包级失败/panic 在别处）——打印「已杀」就是把没取证说成取了证
        print(f"{tag} rc=1 但没有 `--- FAIL:` 名字 → 无法判定（红而无名，等于没取证）")
        print("\n".join(out.splitlines()[-12:]))
        return False
    # 红集合必须**恰好**等于 must：多出来的红说明注码打偏（改了不相干的承重线）或整包被打红。
    # 「任何红都算杀」就是把「这条线有腿守着」偷换成「今天很吵」。
    missing, extra = sorted(must - failed), sorted(failed - must)
    if missing or extra:
        print(f"{tag} rc=1 ran={len(ran)}/{EXPECT_RAN} skip=0 → 无法判定（红集合与 must 不符）"
              f"｜缺={missing} 多={extra}")
        for line in REASON_RE.findall(out)[:3]:
            print(f"   红因: {line[:170]}")
        return False
    print(f"{tag} rc=1 ran={len(ran)}/{EXPECT_RAN} skip=0 → 红 已杀（恰好 must 集合）｜{' | '.join(sorted(failed))}")
    for line in REASON_RE.findall(out)[:2]:
        print(f"   红因: {line[:170]}")
    return True


def recover_stale_backups():
    for f in SRC:
        p = os.path.join(SVC, f)
        bak = p + BAK_SUFFIX
        if os.path.exists(bak):
            shutil.copy2(bak, p)
            os.remove(bak)
            print(f"发现上次遗留的 {f}{BAK_SUFFIX}，已按它还原 {f}")


def check_anchors():
    muts = list(MUTS)
    try:
        muts.append(order_swap_mutation())
    except ValueError as exc:
        print(f"[坏] M8 区域锚点未命中：{exc}")
        return 1
    ok = True
    for name, fname, old, new, _ in muts:
        s = open(os.path.join(SVC, fname), encoding="utf-8").read()
        n = s.count(old)
        # M8 的 new 是 old 的重排，片段必然已在文件里出现一次，故只查 old 唯一。
        # 同理 M4 的替身 `return nil` 在包里本来就有 7 处——dup 只打印不判坏：
        # 只要 old 唯一，replace 就无歧义；拿 dup 当硬门会把预检变成噪声源。
        dup = 0 if name.startswith("M8") else s.count(new)
        flag = "OK" if n == 1 else "坏"
        if n != 1:
            ok = False
        print(f"[{flag}] {name}（{fname}）原文命中={n} 替身本来出现={dup}")
    srcs = ""
    for f in os.listdir(SVC):
        if f.endswith("_test.go"):
            srcs += open(os.path.join(SVC, f), encoding="utf-8").read()
    for name in TESTS:
        found = f"func {name}(" in srcs
        if not found:
            ok = False
        print(f"[{'OK' if found else '坏'}] 用例 {name} {'在' if found else '不在这个包里'}")
    print("预检:", "锚点全部唯一、用例名全部存在" if ok else "有锚点不唯一/用例名不存在，先修脚本或先在目标树上还原")
    return 0 if ok else 1


def main():
    if "--check" in sys.argv:
        return check_anchors()
    if not os.path.isdir(ROOT):
        print(f"目标树不存在：{ROOT}")
        return 2
    recover_stale_backups()

    backups = {}
    base = {}
    for f in SRC:
        p = os.path.join(SVC, f)
        base[f] = md5(p)
        backups[f] = p + BAK_SUFFIX
        shutil.copy2(p, backups[f])
    print("注码目标目录: " + ROOT)
    print("基线 md5: " + "  ".join(f"{f}={base[f][:8]}" for f in SRC))

    all_ok = True
    rc0, ran0, failed0, skip0, out0 = run_tests()
    if rc0 != 0 or failed0 or ran0 != set(TESTS) or skip0:
        print(f"对照腿（未注码）就红/没跑全/有跳过：ran={len(ran0)} skip={len(skip0)} failed={failed0}")
        print("\n".join(out0.splitlines()[-20:]))
        for f in SRC:
            shutil.copy2(backups[f], os.path.join(SVC, f))
            os.remove(backups[f])
        print("已按备份还原，未留下注码现场")
        return 2
    print(f"对照腿 rc=0 ran={len(ran0)}/{EXPECT_RAN} skip=0 → 绿，门在位")

    try:
        MUTS.append(order_swap_mutation())
    except ValueError as exc:
        print(f"M8 → 无法判定（区域锚点未命中：{exc}），顺序锁没被反向验证过")
        all_ok = False

    for name, fname, old, new, must in MUTS:
        p = os.path.join(SVC, fname)
        s = open(p, encoding="utf-8").read()
        n = s.count(old)
        if n != 1:
            print(f"{name} → 无法判定（锚点命中 {n} 次，不做含糊注码）")
            all_ok = False
            continue
        open(p, "w", encoding="utf-8").write(s.replace(old, new))
        if md5(p) == base[fname]:
            # 锚点计数过了但字节没变 = 替换没落地，这一腿从此没有牙齿，而电池会照样报结论
            print(f"{name} → 无法判定（注码后文件字节没变：锚点匹配了，替换却没落地）")
            all_ok = False
            shutil.copy2(backups[fname], p)
            continue
        rc, ran, failed, skipped, out = run_tests()
        ok = verdict(name, rc, ran, failed, skipped, out, must)
        if not ok and name in EQUIV and rc == 0 and not failed and len(ran) == EXPECT_RAN and not skipped:
            print(f"   判为等价类（不判红，理由要读）：{EQUIV[name]}")
            ok = True
        all_ok = all_ok and ok
        # 还原并逐次比对 md5
        shutil.copy2(backups[fname], p)
        if md5(p) != base[fname]:
            print(f"{name} → 还原后 md5 不一致！{md5(p)[:8]} != {base[fname][:8]}")
            return 3
        print(f"   还原 md5 一致 {base[fname][:8]}（{fname}）")

    rc, ran, failed, skipped, out = run_tests()
    if rc == 0 and not failed and ran == set(TESTS) and not skipped:
        print(f"收尾对照腿 rc=0 ran={len(ran)}/{EXPECT_RAN} skip=0 → 绿，代码回到注码前状态")
    else:
        print(f"收尾对照腿异常 rc={rc} ran={len(ran)} skip={len(skipped)} failed={failed}")
        all_ok = False

    for f in SRC:
        os.remove(backups[f])
    print("电池终态:", "OK" if all_ok else "有存活/无法判定，见上")
    return 0 if all_ok else 1


if __name__ == "__main__":
    sys.exit(main())
