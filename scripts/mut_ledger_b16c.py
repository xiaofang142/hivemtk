#!/usr/bin/env python3
"""批16c 反向电池：二次审核线点出的覆盖缺口逐条补腿，共 13 刀（M20–M32）。

这批的靶子与前两批不同：前两批打的是「闸门失效时会不会红」，这批打的是
「**闸门只对 effectWrite 有腿**这件事会不会红」——审核线报的七条里六条属于后者
（腿写在错误的窄集合上，注码后全套测试仍绿）。所以每刀的 `must` 都点名批16c 的新腿，
外加批16 那条只证了「钳 retries」的老腿，用来复算报告原文那句「全套测试全绿」到底对不对。
M32 打的不是行为，而是批16 电池里 M7 那条等价类的**前提**（见其行内注释）。

电池口径（与 mut_ledger_b16.py / mut_ledger_b16b.py 同源，本批另加四条自纠）：
- 只认带 `--- FAIL:` 的名字；`[build failed]` / `undefined:` / `declared and not used` 一律判「无法判定」；
- 每腿自证 ran==EXPECT_RAN 且 **skip==0**（对照腿也查，不只是打印——skip 掉的腿等于没跑）；
- 红还必须读红因：超时/panic 字样的红不算杀（抢库时的负载红会被记成「变异已杀」，最坏的假绿）；
- 【本批加】注码必须真的改到字节：写回后 md5 与基线一致 ⇒ 那一腿根本没注码，判「无法判定」；
- 【本批加】意外红上界：红集合必须恰好等于 must。多红的腿要么说明注码打偏（改了不相干的
  承重线），要么说明整包被打红——「任何红都算杀」就是把「这条线有腿守着」偷换成「今天很吵」；
- 【本批加】对照腿/收尾腿同样判 rc=0 且 skip=0 且 ran 齐全，否则不成立；
- 变异用 cp 备份 + 每步 md5 比对还原，绝不 git checkout/restore。

**本电池不是门禁**：它只跑 TESTS 里列的腿（`-run` 过滤子集）。提交门禁是干净克隆里的
`go build ./... && go vet ./... && go test ./...`（见 spec §7.9/§7.10 的取证文件名）。

运行位置：默认打本仓工作树（`<repo>/user-server`）。工作树里若有并行会话的在途脏文件导致
本泳道编译不起来，改用干净克隆：

    MUT_ROOT=/path/to/clone/user-server python3 scripts/mut_ledger_b16c.py --check
    MUT_ROOT=/path/to/clone/user-server python3 scripts/mut_ledger_b16c.py

先跑 `--check`：一条锚点 0 命中 = 那一腿根本没注码，而电池会照样报「绿（变异存活）」。
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
REPOLAYER = os.path.join(ROOT, "internal/browser_automation/repository")
ENV = dict(os.environ)
ENV["CGO_ENABLED"] = "0"

TESTS = [
    # 批16c 八条新腿
    "TestUnknownEffectStepHoldsDoubleSendGate",
    "TestUnknownEffectStepRefusedAfterLedgerDegrade",
    "TestUnknownEffectStepHoldsConfirmGate",
    "TestLedgerDegradeClearedWhenSessionEnds",
    "TestInjectTimeoutStepStaysReDispatchable",
    "TestRetryBackoffGrowsExponentially",
    "TestFindSubmitAttemptReturnsEarliestAttempt",
    "TestWriteStepKeyDistinguishesLocatorFacets",
    "TestSentToFinalLedgerWritePathHasNoEarlyReturn",
    # 批16 的两条对照老腿：一条证「钳 retries 那一格本来就有腿」（复算审核线口径），
    # 一条证「平台表在手 + 未命中发送位仍是只读」（M20 不得把只读步也拖进闸门）
    "TestWSE2E_UnknownLocatorTableTreatedAsWrite",
    "TestWSE2E_RegisteredPlatformNonMatchingStepStaysReadOnly",
]
EXPECT_RAN = len(TESTS)

UNKNOWN = "writeStep := effect.needsWriteGate()"
# 分类器末尾那条「表在手且三条推导都不命中 ⇒ 只读」的 fallthrough。
# `return effectNone, ""` 在文件里有两条（另一条在「定位表不可得」分支里，缩进多一层），
# 所以锚点必须带上前面的声明位判定，否则命中 2 次、这一腿就是空跑。
READ_FALLTHROUGH = ('\tif step.IsWrite {\n'
                    '\t\treturn effectWrite, "declared=is_write"\n'
                    '\t}\n'
                    '\treturn effectNone, ""')

# (名字, 文件, 原文, 替身, 必须点名的用例)
MUTS = [
    # M20 复算审核线 B1 的原话：闸门条件收窄回 effectWrite。四个 if 一起失效，
    # 其中「钳 retries」那一格批16 已有腿 ⇒ 这条 must 里带老腿，用来标出报告那句
    # 「全套 113 条测试全绿」是**部分错**（三格没腿、一格有腿）。
    ("M20 写闸门只认 effectWrite（unknown 步绕过四道）", "executor.go",
     "\t" + UNKNOWN,
     "\twriteStep := effect == effectWrite",
     {"TestUnknownEffectStepHoldsDoubleSendGate",
      "TestUnknownEffectStepRefusedAfterLedgerDegrade",
      "TestUnknownEffectStepHoldsConfirmGate",
      "TestWSE2E_UnknownLocatorTableTreatedAsWrite"}),

    # M21 审核线 B3：删掉收口清除（表无界增长 + 同 ID 再认领被永久钉死）
    ("M21 会话收口不清降级表", "executor.go",
     "\tdefer e.clearLedgerBroken(session.ID)",
     "\t_ = e.clearLedgerBroken",
     {"TestLedgerDegradeClearedWhenSessionEnds"}),

    # M22 审核线 A6：注入超时不再算「从未发生」⇒ 记成尝试，可重下发的内容被永久堵死
    ("M22 isNeverExecuted 摘掉 inject_timeout 分支", "write_ledger.go",
     '\treturn strings.Contains(msg, "_inject_timeout_") || strings.Contains(msg, "_not_found")',
     '\treturn strings.Contains(msg, "_not_found")',
     {"TestInjectTimeoutStepStaysReDispatchable"}),

    # M23 审核线 A7：指数退避退化成线性
    ("M23 步重试退避改线性", "timeouts.go",
     "\treturn time.Duration(baseMs*(1<<(attempt-1))) * time.Millisecond",
     "\treturn time.Duration(baseMs*attempt) * time.Millisecond",
     {"TestRetryBackoffGrowsExponentially"}),

    # M24 审核线 A8：双发闸回查取最新一次尝试（同一份库两种结论）
    ("M24 台账回查改成取最新", "step.go",
     '\tif err := q.Order("id asc").First(&row).Error; err != nil {',
     '\tif err := q.Order("id desc").First(&row).Error; err != nil {',
     {"TestFindSubmitAttemptReturnsEarliestAttempt"}),

    # M25 审核线 B4：闸门键丢掉定位面两个字段（不同位点塌成一个键）
    ("M25 闸门键丢掉 Anchor/ButtonText", "write_ledger.go",
     '\treturn HashWriteText(step.Action + "\\x00" + step.Target + "\\x00" + step.Anchor + "\\x00" + step.ButtonText)',
     '\treturn HashWriteText(step.Action + "\\x00" + step.Target)',
     {"TestWriteStepKeyDistinguishesLocatorFacets"}),

    # M26 反向界：批7 刻意收窄的「表在手 + 未命中发送位 = 只读」不许被越改越宽——
    # 把只读 fallthrough 判成 unknown，搜索腿就会被钳掉重试、并在重试轮里被当双发跳过。
    # 意外收获（首轮实测）：这条 fallthrough 也覆盖 open_tab/snapshot 这类**必然重复**的
    # 结构步，一刀下去 leg⑤ 的第二轮连 open_tab 都被双发闸拦死、步行根本不再落库——
    # 这正是「判得过宽」在真机上的形状（把每一次开页都当成不可逆提交），所以它也是 must。
    ("M26 未命中发送位也判成写步", "write_ledger.go",
     READ_FALLTHROUGH,
     READ_FALLTHROUGH.replace('\treturn effectNone, ""', '\treturn effectUnknown, "mutated=过宽"'),
     {"TestWSE2E_RegisteredPlatformNonMatchingStepStaysReadOnly",
      "TestInjectTimeoutStepStaysReDispatchable"}),

    # —— M20 是一起失效的四道闸；下面 M27–M31 逐格下刀，为的是**每条新腿各自有牙**——
    # 只证「一起摘掉会红」不够：那可能只有一条腿在挡，其余三条仍然没腿。
    ("M27 双发闸+降级拒绝整块只认 effectWrite", "executor.go",
     "\twriteKey := \"\"\n\tif writeStep {\n\t\t// 批16（A7）",
     "\twriteKey := \"\"\n\tif effect == effectWrite {\n\t\t// 批16（A7）",
     {"TestUnknownEffectStepHoldsDoubleSendGate",
      "TestUnknownEffectStepRefusedAfterLedgerDegrade"}),

    ("M28 D7 确认闸门只认 effectWrite", "executor.go",
     "\tif writeStep && step.Action != \"post_comment\" && task.RequireConfirm {",
     "\tif effect == effectWrite && step.Action != \"post_comment\" && task.RequireConfirm {",
     {"TestUnknownEffectStepHoldsConfirmGate"}),

    ("M29 降级表不再拦在写步下发之前", "executor.go",
     "\t\tif why, broken := e.ledgerBrokenReason(session.ID); broken {",
     "\t\tif why, broken := e.ledgerBrokenReason(session.ID); false && broken {",
     {"TestUnknownEffectStepRefusedAfterLedgerDegrade"}),

    # 复算审核线 B1 原话的第一格：只钳 retries。批16 那条老腿当时就有牙（本电池首轮实测）。
    ("M30 只把「钳 retries」收窄回 effectWrite", "executor.go",
     "\tif writeStep {\n\t\tretries = 0",
     "\tif effect == effectWrite {\n\t\tretries = 0",
     {"TestWSE2E_UnknownLocatorTableTreatedAsWrite"}),

    # 失败路径的台账落点（unknown 步 WS 超时 ⇒ 记 unattributed）同样只有老腿在挡。
    ("M31 失败路径台账只给 effectWrite 落", "executor.go",
     "\t\tlastErr = err.Error()\n\t\tif writeStep {",
     "\t\tlastErr = err.Error()\n\t\tif effect == effectWrite {",
     {"TestWSE2E_UnknownLocatorTableTreatedAsWrite"}),

    # 批16 把 M7 判成等价类，靠的是「sent 写与终态写之间没有早返、且两次写同键」这条控制流事实。
    # 注码刻意选**行为不变**的 `if false { return }`：这条腿是静态锁，它的牙就是「文本里长出了
    # 早返」这件事本身——用一个会改行为的注码来证明它有牙，反而证不到点上（行为一变，别的腿
    # 全红，锁红不红都无从分辨）。
    ("M32 两个台账落点之间长出早返（M7 等价类的前提）", "executor.go",
     "\t\tverified, evidence := e.finalizeComment(",
     "\t\tif false {\n\t\t\treturn nil, nil\n\t\t}\n\t\tverified, evidence := e.finalizeComment(",
     {"TestSentToFinalLedgerWritePathHasNoEarlyReturn"}),
]

EQUIV = {}

FILES = {
    "executor.go": os.path.join(SVC, "executor.go"),
    "write_ledger.go": os.path.join(SVC, "write_ledger.go"),
    "timeouts.go": os.path.join(SVC, "timeouts.go"),
    "step.go": os.path.join(REPOLAYER, "step.go"),
}
BAK_SUFFIX = ".b16cbak"

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


def complete(ran, skipped):
    """跑全且零跳过——否则这条腿的「绿」不构成证据。"""
    return len(ran) == EXPECT_RAN and not skipped


def verdict(tag, rc, ran, failed, skipped, out, must):
    if "[build failed]" in out or "cannot find package" in out or "undefined:" in out \
            or "expected declaration" in out or "declared and not used" in out:
        print(f"{tag} → 无法判定（编译/语法问题，不是变异被杀）")
        print("\n".join(out.splitlines()[-12:]))
        return False
    if not complete(ran, skipped):
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
    missing = sorted(must - failed)
    extra = sorted(failed - must)
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
        n = s.count(old)
        flag = "OK" if n == 1 else "坏"
        if n != 1:
            ok = False
        print(f"[{flag}] {name}（{fname}）原文命中={n}")
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
    rc0, ran0, failed0, skip0, out0 = run_tests()
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
        rc, ran, failed, skipped, out = run_tests()
        ok = verdict(name, rc, ran, failed, skipped, out, must)
        if not ok and name in EQUIV and rc == 0 and not failed and complete(ran, skipped):
            print(f"   判为等价类（不判红，理由要读）：{EQUIV[name]}")
            ok = True
        all_ok = all_ok and ok
        shutil.copy2(backups[fname], p)
        if md5(p) != base[fname]:
            print(f"{name} → 还原后 md5 不一致！{md5(p)[:8]} != {base[fname][:8]}")
            return 3
        print(f"   还原 md5 一致 {base[fname][:8]}（{fname}）")

    rc, ran, failed, skipped, out = run_tests()
    if rc == 0 and not failed and complete(ran, skipped) and ran == set(TESTS):
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
