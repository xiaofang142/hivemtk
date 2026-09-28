#!/usr/bin/env python3
"""批20f（A12）变异电池：存储层独占声明的**每一半**逐格验牙。

为什么要有这条电池（而不是"14 条腿全绿"就算完）：
- A12 是一条**两半**的链子：占坑（拒发）与释放（腾坑）。只测前半，「一次浮层遮挡把评论
  永久锁死」这种坏法全绿；只测后半，「双发闸当场消失」全绿。所以 C4~C14（加本轮补的 C16）里
  release 侧与 claim 侧各占一半，且 release 侧的**时机**（C13/C14）与**条件**（C9/C10/C11）分刀——
  「defer 变同步调用」和「不看台账态」在测试里是同一种红（S3），但 production 里是两件事。
- 唯一约束本身（C2/C3/C15）是这一批的立身之本：摘掉它，其余所有 Go 代码都对，
  而那恰恰是本批立项时的事实（读后再写 = 两条腿读到同一条"没做过"）。
- C7「判出持有者却当没事」、C8「闸门没接好就当没这回事」这两刀打的是同一个方向：
  fail-close 的反面。它们必须各自红一条腿，混在一起就说明某一格其实没人看。

口径（沿用批16/17/18/19x/20c/20d 电池）：
- 控制组必须 rc==0、total>0、skip==0、且不许有任何红名；
- 每格断言 PASS+FAIL==控制组数（一条用例 panic 会带走整个二进制，"FAIL=1"看着像杀其实没跑完）；
- BUILD FAILED / panic / 红而没点名 一律判 BROKEN，**不计入杀掉**（编译红不是牙）；
- 锚点命中必须恰好一次，注码先过 gofmt -e；多处注码先在**内存里累加**、最后一次落盘；
  还原后逐文件比 md5；只在私有 --shared 克隆里注码，绝不碰共享工作树。

已知**未覆盖**的一格（写在这里而不是悄悄不留痕）：
- repository/write_claim.go「INSERT 返回 0 行、回读却查不到持有者」那条 fail-close 分支
  需要真实的并发释放窗口才能进（本包 A6 那 8 条腿抢同一把坑时无人释放，故走不到）。
  它的失效方向是「放行」，与 C8 同侧；C8 已锁住「拿不到结论就拒发」这一格的**上层消费**，
  但**这一支本身没有牙**。要补需要一个会并发 Release 的仓储层用例，本批未做。
- 同理 `migrate.go` 里 `&browsermodel.BrowserWriteClaim{}` 的登记不受本电池管辖：
  克隆里的表由 testutil 按模型标签建，摘掉登记不会让任何一条腿红。那一格由
  scripts/check-model-migration.py 管，反向验证（摘登记→该门必须红）见批次报告。

用法：python3 scripts/mut_write_claim_a12.py [--keep] [--clone DIR]
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

# 脚本在 <repo>/scripts/ 下 ⇒ 根 = 上一级。**不硬编码仓名**（改名克隆必须照样能跑）。
ROOT = Path(__file__).resolve().parent.parent

CLAIM_REL = Path("user-server/internal/browser_automation/repository/write_claim.go")
MODEL_REL = Path("user-server/internal/browser_automation/model/write_claim.go")
LEDGER_REL = Path("user-server/internal/browser_automation/service/write_ledger.go")
EXEC_REL = Path("user-server/internal/browser_automation/service/executor.go")
# 槽名 → 仓内相对路径。只此一份：电池自己按它拼克隆里的目标文件，
# scripts/anchor-preflight.py 按它在**工作树**上预验锚点（不建克隆、不跑用例）。
# 两处各写一份字典就会漂（漂了表现为"预检绿、电池说锚点没命中"）。
SLOT_FILES = {"claim": CLAIM_REL, "model": MODEL_REL,
              "ledger": LEDGER_REL, "exec": EXEC_REL}
# 本泳道脏 .go 的枚举范围。手写清单必然漏（漏一个用旧签名的文件 = 克隆里 [build failed]，
# 电池整个失声），所以按目录取 git status。
LANE_PATHS = ["user-server/internal/browser_automation",
              "user-server/internal/pkg/db",
              "user-server/internal/router"]

GO_PKGS = ["./internal/browser_automation/repository/", "./internal/browser_automation/service/"]
# 只跑本批 15 条腿（仓储 8 + 服务 7）。跑整包会把无关用例的红混进「杀掉」名单，
# 看着像牙其实是被别人撞红的。两侧用例名同前缀，一条 -run 就够。
GO_RUN = "TestWriteClaim"

# 期望被杀的腿名（简称常量，避免手抖打错整串）
R_TAKE = "TestWriteClaimTakeAndIdempotent"
R_HELD = "TestWriteClaimHeldByOtherStep"
R_OWNER = "TestWriteClaimReleaseOnlyOwns"
R_STORE = "TestWriteClaimConstraintLivesInStorage"
R_RACE = "TestWriteClaimConcurrentTakeHasOneWinner"
R_SCHEMA = "TestWriteClaimSchemaContract"
R_KEYS = "TestWriteClaimIncompleteKeyRefuses"
S_REFUSE = "TestWriteClaimGateRefusesBeforeAnyFrame"
S_RELEASE = "TestWriteClaimReleasedWhenNeverCrossed"
S_KEEP = "TestWriteClaimKeptWhenCrossed"
S_LEDGER = "TestWriteClaimKeptWhenLedgerWriteFailed"
S_READFAIL = "TestWriteClaimKeptWhenStateReadFailed"
S_UNWIRED = "TestWriteClaimGateFailCloseWhenUnwired"
S_CLAIMERR = "TestWriteClaimRepoErrorRefusesDispatch"

ANSI = re.compile(r"\x1b\[[0-9;]*m")
CONTROL = {"go": 0}

# ---------------------------------------------------------------- 锚点（取完整语句含缩进）
# 仓储层
A_HASH_GUARD = '\tif textHash == "" {\n'
A_STEP_GUARD = '\tif stepRowID == 0 {\n'
A_CONFLICT = "ON CONFLICT (task_id, text_hash) DO NOTHING"
A_ROWS_ONE = "\tif res.RowsAffected == 1 {\n\t\treturn nil, nil\n\t}"
A_SELF_IDEM = "\tif holder.StepRowID == stepRowID {\n\t\treturn nil, nil\n\t}"
A_RELEASE_WHERE = "DELETE FROM browser_write_claims WHERE step_row_id = ? AND text_hash = ?"
# 模型层：同一把复合索引跨两列，摘它必须两列一起摘（单摘一列会建成「单列唯一」，
# 那是另一种坏法而不是「没有约束」，会串了 C15 的判据）
A_IDX_TASK = 'uniqueIndex:uk_browser_write_claims_task_text,priority:1'
A_IDX_HASH = 'uniqueIndex:uk_browser_write_claims_task_text,priority:2'
# 服务层。C8 的锚点必须吃下整个 if 块：只把条件写成 `== nil && false` 的那一版
# 会在 nil 接口上调 ClaimWriteSlot 直接 panic，电池按规则判 BROKEN 而不是杀掉——
# 「删掉一条腿」不是「让它放行」，注码要模拟的是有人照抄 cmdLogRepo 的可选注入。
A_UNWIRED = ("\tif e.writeClaimRepo == nil {\n"
             '\t\treturn errors.New("存储层写声明闸门未接线：占不了坑，就无从判断另一条腿是否正在同一份文本上")\n'
             "\t}\n")
A_UNWIRED_FAIL_OPEN = "\tif e.writeClaimRepo == nil {\n\t\treturn nil // 变异：未接线视为无需裁决\n\t}\n"
# C16 的锚点吃掉「调用 + 判错」两行：只把 `return err` 摘掉会让 err 未使用而编译红，
# 那只会得到 BROKEN；注码要模拟的是「有人以为占坑报错不要紧，Warn 一句继续发」。
A_CLAIM_ERRBLOCK = ("\tholder, err := e.writeClaimRepo.ClaimWriteSlot(ctx, taskID, sessionID, stepRowID, textHash)\n"
                    "\tif err != nil {\n\t\treturn err\n\t}\n")
A_CLAIM_ERR_SWALLOW = ("\tholder, err := e.writeClaimRepo.ClaimWriteSlot(ctx, taskID, sessionID, stepRowID, textHash)\n"
                       "\tif err != nil {\n\t\treturn nil // 变异：占坑报错当作「没有持有者」放行\n\t}\n")
A_HOLDER = "\tif holder != nil {\n"
A_ATTEMPTED = "\tfor _, s := range model.StepSubmitAttemptedStates() {\n"
A_BROKEN = "\tif why, broken := e.ledgerBrokenReason(sessionID); broken {\n"
A_READERR = "\tstate, err := e.stepRepo.SubmitStateOf(readCtx, stepRowID)\n\tif err != nil {\n"
# 调用点
A_CLAIM_CALL = "\t\tif err := e.claimWriteSlot(ctx, task.ID, session.ID, stepRow.ID, writeKey); err != nil {\n"
A_DEFER_RELEASE = "\t\tdefer func() { e.releaseWriteSlot(ctx, task.ID, session.ID, stepRow.ID, writeKey) }()\n"


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def verdict(r: dict, expect_total: int) -> str:
    """三态判定：杀掉 / 存活=洞 / BROKEN（红了但判不了）。"""
    if r["rc"] == 0 and not r["killed"]:
        return "存活=洞"
    if r.get("panicked") or r.get("buildfailed") or not r["killed"]:
        return "BROKEN=判不了"
    if r["total"] != expect_total or r["failed"] < 1 or r["skipped"] > 0:
        return "BROKEN=判不了"
    return "杀掉"


def dup_report(kills: dict) -> None:
    seen = {}
    for code, names in kills.items():
        key = tuple(sorted(names))
        seen.setdefault(key, []).append(code)
    for key, codes in seen.items():
        if len(codes) > 1 and key:
            print("[Go] 同族（同一批用例被多格杀掉，判据可能重叠）：" + "≈".join(codes)
                  + f" → {' '.join(sorted(key))[:120]}")


def lane_overlays() -> list[str]:
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到本泳道脏文件清单：" + r.stderr[-200:])
    out = []
    for line in r.stdout.splitlines():
        p = line[3:].split(" -> ")[-1].strip().strip('"')
        if p.endswith(".go"):
            out.append(p)
    if not out:
        raise SystemExit("脏文件清单为空——克隆里跑的是 HEAD，测不到本批改动（宁可停机也别假绿）")
    return out


def syntax_ok_go(src: str) -> tuple[bool, str]:
    p = subprocess.run(["gofmt", "-e"], input=src, capture_output=True, text=True, timeout=120)
    return p.returncode == 0, p.stderr.strip()


def go_prepare(dst: Path) -> Path:
    clone = dst / "clone"
    if clone.exists():
        raise SystemExit(f"{clone} 已存在（换 --clone 目录或先删）")
    r = subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(clone)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("克隆失败：" + (r.stdout + r.stderr)[-400:])
    # 分支从**工作树**读，不从克隆读：--no-checkout 的克隆里 HEAD 是未 born 的符号引用，
    # `branch --show-current` 可能给空串 ⇒ 退回 master，而本仓当前分支未必是 master。
    b = subprocess.run(["git", "-C", str(ROOT), "branch", "--show-current"],
                       capture_output=True, text=True, timeout=60)
    branch = b.stdout.strip() or "master"
    c = subprocess.run(["git", "checkout", "-f", branch], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if c.returncode != 0:
        raise SystemExit("checkout 失败：" + (c.stdout + c.stderr)[-400:])
    for rel in lane_overlays():
        src = ROOT / rel
        if not src.exists():
            raise SystemExit(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
        if md5_bytes(src) != md5_bytes(tgt):
            raise SystemExit(f"覆盖后 md5 不一致（装错树/写盘失败）：{rel}")
    # .env 不进 git ⇒ 克隆里没有则依赖 DB 的用例会 skip，控制组就不干净（skip==0 是硬门）。
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path) -> dict:
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-a12mut")
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    p = subprocess.run(["go", "test", "-p", "1", "-count=1", "-v"] + GO_PKGS + ["-run", GO_RUN],
                       cwd=root, capture_output=True, text=True, timeout=1800, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = sorted(set(re.findall(r"^    --- FAIL: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    passed = len(re.findall(r"^--- PASS: ", out, re.M))
    failed = len(re.findall(r"^--- FAIL: ", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: ", out, re.M))
    return {"rc": p.returncode, "killed": killed, "total": passed + failed + skipped,
            "passed": passed, "failed": failed, "skipped": skipped, "out": out,
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "cannot use" in out or "undefined:" in out}


def cells() -> list[tuple[str, str, list[tuple[str, str, str]], str]]:
    """(格, 说明, [(文件槽, 原文, 注码), ...], 该红的腿)"""
    return [
        # ---- 仓储层：约束与判读
        ("C1", "键不完整不再拒占（空正文/零号行都往库里插）",
         [("claim", A_HASH_GUARD, A_HASH_GUARD.replace('textHash == ""', 'false')),
          ("claim", A_STEP_GUARD, A_STEP_GUARD.replace('stepRowID == 0', 'false'))], R_KEYS),
        ("C2", "占坑退回成裸 INSERT（没有 ON CONFLICT：撞约束=报错而不是判出持有者）",
         [("claim", " ON CONFLICT (task_id, text_hash) DO NOTHING", "")], R_TAKE),
        ("C3", "ON CONFLICT 的目标写错列（键的一半错了 = 库里根本没有这条约束）",
         [("claim", A_CONFLICT, "ON CONFLICT (step_row_id) DO NOTHING")], R_TAKE),
        ("C4", "插了 0 行也当占到（持有者永远查不出来，独占当场失效）",
         [("claim", A_ROWS_ONE, A_ROWS_ONE.replace("res.RowsAffected == 1", "res.RowsAffected >= 0"))],
         R_HELD),
        ("C5", "同一步重复占坑判成「别人占着」（跨过点的步被自己的声明拦死）",
         [("claim", A_SELF_IDEM, A_SELF_IDEM.replace("holder.StepRowID == stepRowID", "false"))],
         R_TAKE),
        ("C6", "释放条件从 AND 写成 OR（任何人按文本都能腾别人的坑）",
         [("claim", A_RELEASE_WHERE,
           A_RELEASE_WHERE.replace("step_row_id = ? AND text_hash = ?",
                                   "step_row_id = ? OR text_hash = ?"))], R_OWNER),
        # ---- 模型层：约束住在库里
        ("C15", "复合唯一索引两列一起摘掉（闸门退回应用层的一段 Go 代码）",
         [("model", A_IDX_TASK, "index:ix_a12_task"), ("model", A_IDX_HASH, "index:ix_a12_hash")],
         R_SCHEMA),
        # ---- 服务层：占坑侧
        ("C7", "查出持有者却当没事（闸门只剩记账，不再生效）",
         [("ledger", A_HOLDER, A_HOLDER.replace("holder != nil", "holder != nil && false"))], S_REFUSE),
        ("C8", "闸门没接线时当成放行（fail-close 折成 fail-open：照抄日志仓储的可选注入）",
         [("ledger", A_UNWIRED, A_UNWIRED_FAIL_OPEN)], S_UNWIRED),
        ("C12", "executor 根本不看占坑结论（写了闸门却没长在派发路径上）",
         [("exec", A_CLAIM_CALL, A_CLAIM_CALL.replace("; err != nil {", "; err != nil && false {"))],
         S_REFUSE),
        # C16 是本轮（二次审核批次 4-B）补的格：C7/C8/C11/C12 各管一格，但**仓储调用本身报错**
        # 这一支从未被注入过——S1 走 holder!=nil、S4 走 repo==nil，其余走 happy path，
        # 于是「把 err 吞成放行」在服务层六条腿全绿。它必须单独一刀，不能并进 C8：
        # 「闸门没接线」与「占坑报错」是两种故障，运维处置不同（文案各自钉住）。
        ("C16", "占坑报错被吞成放行（拿不到独占结论也照发＝并发下双发）",
         [("ledger", A_CLAIM_ERRBLOCK, A_CLAIM_ERR_SWALLOW)], S_CLAIMERR),
        # ---- 服务层：释放侧的条件
        ("C9", "释放不看台账态（已跨过提交点的步也把坑腾出去）",
         [("ledger", A_ATTEMPTED, A_ATTEMPTED.replace("model.StepSubmitAttemptedStates()",
                                                      "[]string{}"))], S_KEEP),
        ("C10", "本会话台账写失败过照样释放（把唯一凭据删掉，双发只剩读闸一列在守）",
         [("ledger", A_BROKEN, A_BROKEN.replace("; broken {", "; broken && false {"))], S_LEDGER),
        ("C11", "台账态读不出来当成「没发生」（判不出折向放行）",
         [("ledger", A_READERR, A_READERR.replace("if err != nil {", "if err != nil && false {"))],
         S_READFAIL),
        # ---- 服务层：释放侧的时机
        ("C13", "收尾不释放（声明表变成永久黑名单，比没有闸门更糟）",
         [("exec", A_DEFER_RELEASE,
           "\t\tdefer func() { _ = writeKey }()\n")], S_RELEASE),
        ("C14", "占坑完立刻释放（defer 变同步调用，独占窗口长度归零）",
         [("exec", A_DEFER_RELEASE,
           "\t\te.releaseWriteSlot(ctx, task.ID, session.ID, stepRow.ID, writeKey)\n")], S_KEEP),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    tmp, owned = workdir(args.clone or None, prefix="a12mut-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}", flush=True)
    problems: list[str] = []

    clone = go_prepare(tmp)
    files = {slot: clone / rel for slot, rel in SLOT_FILES.items()}
    for name, p in files.items():
        if not p.exists():
            raise SystemExit(f"注码目标文件不在克隆里：{p}")
    originals = {name: read(p) for name, p in files.items()}
    basemd5 = {name: md5_bytes(p) for name, p in files.items()}

    prepared = []
    for code, desc, edits, expect in cells():
        acc: dict[str, str] = {}
        for slot, old, new in edits:
            cur = acc.get(slot, originals[slot])
            hit = cur.count(old)
            if hit != 1:
                raise SystemExit(f"{code} 锚点在 {slot} 里命中 {hit} 次（要求恰好 1）：{old[:70]!r}")
            if old == new:
                raise SystemExit(f"{code} 注码无效（原文与注码后一致）")
            acc[slot] = cur.replace(old, new, 1)
        for slot, src in acc.items():
            if src == originals[slot]:
                raise SystemExit(f"{code} 注码无效（{slot} 替换后与原文件一致）")
            ok, err = syntax_ok_go(src)
            if not ok:
                raise SystemExit(f"{code} 注码语法坏，跑出来只会是 build failed：{err[:160]}")
        prepared.append((code, desc, acc, expect))
    print(f"\n[Go] 注码前置：{len(prepared)} 格锚点各命中一次 + 注码后语法可解析")

    r = go_run(clone)
    CONTROL["go"] = r["total"]
    print(f"[Go] 控制组 rc={r['rc']} total={r['total']} passed={r['passed']} "
          f"skip={r['skipped']} FAIL={r['killed']}")
    if r["rc"] != 0 or r["total"] == 0 or r["skipped"] > 0 or r["killed"]:
        print(r["out"][-4000:])
        raise SystemExit("[Go] 控制组不干净——后面所有红/绿都不可信")

    gkill: dict[str, set] = {}
    for code, desc, acc, expect in prepared:
        for slot, src in acc.items():
            files[slot].write_text(src, encoding="utf-8")
        for slot in acc:
            if md5_bytes(files[slot]) == basemd5[slot]:
                raise SystemExit(f"{code} 注码未生效（{slot} 与原内容一致）")
        r = go_run(clone)
        v = verdict(r, CONTROL["go"])
        if v == "杀掉" and expect not in r["killed"]:
            v = f"红了但没点出 {expect}"
        print(f"{code:<4} {desc[:58]:<60} {v:<7} total={r['total']} fail={r['failed']} "
              f"skip={r['skipped']} ｜ " + " | ".join(k[:60] for k in r["killed"][:3]))
        if not v.startswith("杀掉"):
            problems.append(f"[Go] {code} {v}：{desc}")
            print(r["out"][-3000:])
        gkill[code] = set(r["killed"])
        for slot in acc:
            files[slot].write_text(originals[slot], encoding="utf-8")
        for slot in acc:
            if md5_bytes(files[slot]) != basemd5[slot]:
                raise SystemExit(f"{code} 还原后 md5 不一致，停机")

    dup_report(gkill)
    print("[Go] 已全量还原（md5 一致）")

    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    if problems:
        print("\n===== 电池判定：有洞 =====")
        for x in problems:
            print("  ✗", x)
        return 1
    print(f"\n===== 电池判定：Go {len(prepared)} 格逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
