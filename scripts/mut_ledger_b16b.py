#!/usr/bin/env python3
"""批16b 反向电池：二次对抗审核四处收口（B1/B2/B3）+ 三条补齐的口径锁，共 11 处变异（M9–M19）。

每腿的口径（仓库律，与批16 电池 scripts/mut_ledger_b16.py 同源）：
- 只认带 `--- FAIL:` 的名字；`[build failed]` / `undefined:` / `declared and not used` 一律判「无法判定」；
- 每腿自证 ran==EXPECT_RAN 且 skip==0，否则不判绿（绿但没跑过不算证据）；
- **红还必须读红因**：本机与并行会话抢同一个测试库时，一条腿完全可能因超时/取消而红，
  只按名字判「已杀」就是把负载红记成变异被杀——那是变异电池最坏的一种假绿；
- 变异用 cp 备份 + 每步 md5 比对还原，绝不 git checkout/restore；
- M9 是给 §7.8 那句「真闸门失效照样红」补证据的腿：批16 电池八条全部打在台账与分类上，
  没有一条证明「D7 那道真闸门被摘掉时会红」。当时那句是推断，不是跑出来的。

运行位置：默认打本仓工作树（`<repo>/user-server`）。工作树里若有并行会话的在途脏文件导致
本泳道编译不起来（本批就遇到过：旁道 `internal/platform/sync.go` 半写状态 ⇒ 本泳道 service 包
`[build failed]`，因为它 import 它），改用干净克隆：

    MUT_ROOT=/path/to/clone/user-server python3 scripts/mut_ledger_b16b.py --check
    MUT_ROOT=/path/to/clone/user-server python3 scripts/mut_ledger_b16b.py

先跑 `--check`：一条锚点 0 命中 = 那一腿根本没注码，而电池会照样报「绿（变异存活）」，
10+ 腿每腿都要跑一遍全量用例，锚点错到跑中途才发现就是白等半小时。

批16c 复核审核线时给本副本追加的三条口径（与 mut_ledger_b16.py 同步）：注码后必须真的改到
字节；红集合必须**恰好**等于 must（旧口径只查 must ⊆ failed，任何把整包打红的注码都会被记成
「已杀」）；对照腿/收尾腿也查 skip==0（旧口径只打印不判）。

**本电池不是门禁**：它只跑 TESTS 里列的腿（`-run` 过滤子集）。提交门禁是干净克隆里的
`go build ./... && go vet ./... && go test ./...`。
"""
import hashlib
import os
import re
import shutil
import subprocess
import sys
import time

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ROOT = os.environ.get("MUT_ROOT") or os.path.join(REPO, "user-server")
# 逐格 `go test` 原始输出落进仓库树（只随 stdout 走 ⇒ 调用方重定向到 /tmp，重启即蒸发，
# 台账里的读数就没有产物可对）。目录带趟次戳、不复用；`.gitignore` 需为本轮次开例外。
LOGDIR = os.path.join(REPO, "docs/superpowers/specs/ledger/logs/B16ledgerB", time.strftime("%Y%m%d-%H%M%S"))


from redact import scrub  # 落盘前脱敏：常驻产物要过 gitleaks（见 scripts/redact.py 的 why）
def dump(tag, out):
    os.makedirs(LOGDIR, exist_ok=True)
    safe = re.sub(r"[^A-Za-z0-9_.-]", "-", tag)
    with open(os.path.join(LOGDIR, safe + ".log"), "w", encoding="utf-8") as f:
        f.write(scrub(out))
SVC = os.path.join(ROOT, "internal/browser_automation/service")
REPOLAYER = os.path.join(ROOT, "internal/browser_automation/repository")
ENV = dict(os.environ)
ENV["CGO_ENABLED"] = "0"

TESTS = [
    # 批16b 九条
    "TestBlockedWriteStepReasonSurvivesCanceledCtx",
    "TestCrossedLedgerGapSuppressesAutoRetry",
    "TestPreparedLedgerFailureStillSchedulesRetry",
    "TestRunRetryRefusesTaskWithLedgerGap",
    "TestLedgerUpdateOnMissingRowFails",
    "TestLedgerGapSetGrowsOnlyPerDistinctKey",
    "TestWriteStepKeyNeverEmpty",
    "TestGenericWriteStepLedgerFailureJudgedRed",
    "TestTransientLedgerWriteFailureRecoversWithoutGap",
    # D7 真闸门的两条运行腿（M9 的靶）
    "TestWSE2E_D7GateHoldsSendUntilConfirmed",
    "TestWSE2E_D7AbortBeforeConfirmNeverSends",
]
EXPECT_RAN = len(TESTS)

# (名字, 文件, 原文, 替身, 必须点名的用例)
MUTS = [
    # —— 批16 电池没覆盖到的那道真闸门 ——
    ("M9 D7 post_comment 确认闸门摘掉", "executor.go",
     "\t\tif task.RequireConfirm {\n\t\t\tout, why := e.waitForConfirm(",
     "\t\tif false && task.RequireConfirm {\n\t\t\tout, why := e.waitForConfirm(",
     {"TestWSE2E_D7GateHoldsSendUntilConfirmed", "TestWSE2E_D7AbortBeforeConfirmNeverSends"}),

    # —— B1：0 行受影响必须算失败 ——
    ("M10 台账 0 行受影响放回 nil", "step.go",
     "\tif res.RowsAffected == 0 {",
     "\tif false && res.RowsAffected == 0 {",
     {"TestLedgerUpdateOnMissingRowFails"}),

    # —— B2：缺口不得再挂自动重试（三个独立落点各一刀）——
    ("M11 缺口查询恒为「无缺口」（接线失效）", "feedback.go",
     "\treturn f.ledgerGapFn != nil && f.ledgerGapFn(taskID)",
     "\treturn false",
     {"TestCrossedLedgerGapSuppressesAutoRetry", "TestRunRetryRefusesTaskWithLedgerGap"}),
    ("M12 OnSessionFinished 不再因缺口抑制", "feedback.go",
     "\t\tif gapBlocked {\n\t\t\tlogger.Warnf(",
     "\t\tif false && gapBlocked {\n\t\t\tlogger.Warnf(",
     {"TestCrossedLedgerGapSuppressesAutoRetry"}),
    ("M13 runRetry 侧第二道摘掉", "feedback.go",
     "\tif f.ledgerGapHere(task.ID) {\n\t\treturn fmt.Errorf(\"重试拒绝起跑:",
     "\tif false && f.ledgerGapHere(task.ID) {\n\t\treturn fmt.Errorf(\"重试拒绝起跑:",
     {"TestRunRetryRefusesTaskWithLedgerGap"}),
    # 抑制的**代价**是「未跨越时仍要重试」——把抑制写成「台账一失败就永不重试」也能绿上一条
    # 批16c 收紧（红集合要恰好等于 must）后，本腿多红了 TestCrossedLedgerGapSuppressesAutoRetry。
    # 单独跑那条腿复现过（探针日志：`挂起重试 next_retry_at=… want NULL`）：这一行同时是
    # 「该抑制」与「不该抑制」两条腿的判据，取反必然两条都红——是注码的真实连带面，不是打偏。
    ("M14 未跨越也被停摆（对照腿反向）", "feedback.go",
     "\tgapBlocked := retryDue && f.ledgerGapHere(task.ID)",
     "\tgapBlocked := retryDue && !f.ledgerGapHere(task.ID)",
     {"TestPreparedLedgerFailureStillSchedulesRetry", "TestCrossedLedgerGapSuppressesAutoRetry"}),

    # —— B3：终态文案必须活过执行 ctx ——
    # 锚点取**修复态**那一行（WithoutCancel 在位），摘掉它才是「退回挂在执行 ctx 上」。
    # 首轮我把锚点写成 RED 态的 `WithTimeout(ctx, …) // RED`：那是注码前的现场，
    # 修复后永不存在，电池会在锚点计数上判「无法判定」——看似严谨，实则一条腿没跑。
    ("M15 步终态写回挂回执行 ctx", "executor.go",
     "\twriteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stepFinalWriteBudget)",
     "\twriteCtx, cancel := context.WithTimeout(ctx, stepFinalWriteBudget)",
     {"TestBlockedWriteStepReasonSurvivesCanceledCtx"}),

    # —— 三条口径锁的牙齿 ——
    ("M16 缺口集合不再按键去重", "write_ledger.go",
     "\tif e.ledgerGaps[key] {\n\t\treturn ",
     "\tif false && e.ledgerGaps[key] {\n\t\treturn ",
     {"TestLedgerGapSetGrowsOnlyPerDistinctKey"}),
    # 批16c 收紧后本腿多红了 TestGenericWriteStepLedgerFailureJudgedRed（§7.9 早就记过这条跨批
    # 咬合，只是旧口径 must ⊆ failed 看不见「多」）。探针单独跑那条腿的原因行：
    # 「通用写步的越点未落账必须进缺口兜底集合」——键退化成空串时 rememberLedgerGap 就地早返，
    # 兜底记不住 = 同一条失效的第二面，不是注码打偏。
    ("M17 无正文写步的闸门键退化成空串", "write_ledger.go",
     '\treturn HashWriteText(step.Action + "\\x00" + step.Target + "\\x00" + step.Anchor + "\\x00" + step.ButtonText)',
     '\treturn strings.TrimSpace(step.Value)',
     {"TestWriteStepKeyNeverEmpty", "TestGenericWriteStepLedgerFailureJudgedRed"}),
    ("M18 台账写不再退避重试（1 次即判负）", "write_ledger.go",
     "\tledgerWriteAttempts  = 3",
     "\tledgerWriteAttempts  = 1",
     {"TestTransientLedgerWriteFailureRecoversWithoutGap"}),
    ("M19 通用写步「命令成功即绿」不受台账约束", "executor.go",
     "\t\t\tif ledgerErr != nil {\n\t\t\t\tmsg := fmt.Sprintf(\"写步已下发但台账未落",
     "\t\t\tif false && ledgerErr != nil {\n\t\t\t\tmsg := fmt.Sprintf(\"写步已下发但台账未落",
     {"TestGenericWriteStepLedgerFailureJudgedRed"}),
]

# 与 M11 重叠，但 M12/M13 各自单独点一个消费方，因此没有等价类可登记
EQUIV = {}

FILES = {
    "executor.go": os.path.join(SVC, "executor.go"),
    "write_ledger.go": os.path.join(SVC, "write_ledger.go"),
    "feedback.go": os.path.join(SVC, "feedback.go"),
    "step.go": os.path.join(REPOLAYER, "step.go"),
    "timeouts.go": os.path.join(SVC, "timeouts.go"),
}
BAK_SUFFIX = ".b16bbak"

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


def run_tests(tag="00-control"):
    cmd = ["go", "test", "-count=1", "-timeout", "900s", "-test.v",
           "-run", "^(" + "|".join(TESTS) + ")$", "./internal/browser_automation/service/"]
    p = subprocess.run(cmd, cwd=ROOT, env=ENV, capture_output=True, text=True)
    out = p.stdout + p.stderr
    dump(tag, out)
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
        print(f"{tag} rc=1 但没有 `--- FAIL:` 名字 → 无法判定（红而无名，等于没取证）")
        print("\n".join(out.splitlines()[-12:]))
        return False
    # 红集合必须**恰好**等于 must：多出来的红说明注码打偏（改了不相干的承重线）或整包被打红。
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


def recover_stale_backups(used):
    """上一次电池若被中途 kill，注码现场会留在工作树里（.bak 还在）。
    先按备份还原再算基线 md5，否则基线取的就是**被注码的**文本，之后每次「还原」都在还原缺陷。"""
    for f in used:
        bak = FILES[f] + BAK_SUFFIX
        if os.path.exists(bak):
            shutil.copy2(bak, FILES[f])
            os.remove(bak)
            print(f"发现上次遗留的 {os.path.basename(bak)}，已按它还原 {f}")


def check_anchors():
    ok = True
    for name, fname, old, new, must in MUTS:
        s = open(FILES[fname], encoding="utf-8").read()
        n, dup = s.count(old), s.count(new)
        # 只以 old 唯一为硬门：替身本来就出现在包里很常见（如 `return nil`），
        # 那不构成注码歧义，拿 dup 判坏只会把预检变成噪声源。
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
    used = sorted({f for _, f, _, _, _ in MUTS})
    if "--check" in sys.argv:
        return check_anchors()
    from battlog import tee_to  # 判定行与逐格产物同处一地（LOGDIR/00-run.log）
    tee_to(os.path.join(LOGDIR, "00-run.log"))
    if not os.path.isdir(ROOT):
        print(f"目标树不存在：{ROOT}")
        return 2
    recover_stale_backups(used)

    backups, base = {}, {}
    for f in used:
        p = FILES[f]
        base[f] = md5(p)
        backups[f] = p + BAK_SUFFIX
        shutil.copy2(p, backups[f])
    print("注码目标目录: " + ROOT)
    print("基线 md5: " + "  ".join(f"{f}={base[f][:8]}" for f in used))

    all_ok = True
    rc0, ran0, failed0, skip0, out0 = run_tests("00-control-open")
    if rc0 != 0 or failed0 or ran0 != set(TESTS) or skip0:
        print(f"对照腿（未注码）就红/没跑全/有跳过：ran={len(ran0)} skip={len(skip0)} failed={failed0}")
        print("\n".join(out0.splitlines()[-25:]))
        for f in used:
            shutil.copy2(backups[f], FILES[f])
            os.remove(backups[f])
        print("已按备份还原，未留下注码现场")
        return 2
    print(f"对照腿 rc=0 ran={len(ran0)}/{EXPECT_RAN} skip=0 → 绿，门在位")

    for name, fname, old, new, must in MUTS:
        p = FILES[fname]
        s = open(p, encoding="utf-8").read()
        n = s.count(old)
        if n != 1:
            print(f"{name} → 无法判定（锚点命中 {n} 次，不做含糊注码）")
            all_ok = False
            continue
        open(p, "w", encoding="utf-8").write(s.replace(old, new))
        if md5(p) == base[fname]:
            print(f"{name} → 无法判定（注码后文件字节没变：锚点匹配了，替换却没落地）")
            all_ok = False
            shutil.copy2(backups[fname], p)
            continue
        rc, ran, failed, skipped, out = run_tests(name)
        ok = verdict(name, rc, ran, failed, skipped, out, must)
        if not ok and name in EQUIV and rc == 0 and not failed and len(ran) == EXPECT_RAN and not skipped:
            print(f"   判为等价类（不判红，理由要读）：{EQUIV[name]}")
            ok = True
        all_ok = all_ok and ok
        shutil.copy2(backups[fname], p)
        if md5(p) != base[fname]:
            print(f"{name} → 还原后 md5 不一致！{md5(p)[:8]} != {base[fname][:8]}")
            return 3
        print(f"   还原 md5 一致 {base[fname][:8]}（{fname}）")

    rc, ran, failed, skipped, out = run_tests("99-control-close")
    if rc == 0 and not failed and ran == set(TESTS) and not skipped:
        print(f"收尾对照腿 rc=0 ran={len(ran)}/{EXPECT_RAN} skip=0 → 绿，代码回到注码前状态")
    else:
        print(f"收尾对照腿异常 rc={rc} ran={len(ran)} skip={len(skipped)} failed={failed}")
        all_ok = False

    for f in used:
        os.remove(backups[f])
    print("电池终态:", "OK" if all_ok else "有存活/无法判定，见上")
    return 0 if all_ok else 1


if __name__ == "__main__":
    sys.exit(main())
