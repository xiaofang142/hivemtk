#!/usr/bin/env python3
"""批20b（A2）变异电池：把「下发了」与「平台确认了」拆成两件事这件事，逐刀验牙。

为什么现在要有这个文件（§7.24 的读数本来是在 /tmp 里两个一次性脚本跑出来的）：
- spec 里那一行把 `/tmp/b20b_remute.py` + `/tmp/b20b_remute2.py` 当成"跑法"来引用，而 /tmp 会随重启清空，
  引用一个不存在的东西比不引用更糟——**复现路径必须落在仓里**。六刀的形状、锚点与判据全部照抄那两份脚本，
  只是搬进常驻文件并补上后面几条口径。
- 那两份脚本是**就地注共享工作树**（`shutil.copy2` 备份 → 改 → 跑 → 还原）。本泳道与并行会话共用同一棵树，
  就地注码等于在别人脚下改文件；这里改成只在私有 `--shared` 克隆里注（口径同批16/17/18/19x/20c/20d/20f/22）。
- 八刀分打两个包，所以有**两个控制组**：`-run TestB20B`（service）与
  `-run TestCommandLogOkTriState`（migrations）。腿数一律放刀前现测、不写死常量
  （§7.24 那份原文写的是"5 条腿/4 条腿"，本泳道补了失败回包那条窄腿之后 service 侧已经是 6 条——
  写死的计数只会让"某一侧整包没跑"看似的绿）。混成一个总数同理不行。
- 八刀各自打的是同一条承诺的不同侧面，缺一个就有一侧没人看：
  M1 打"command 帧不许带结论"（A2 的那根轴本身）；M2 打"确认帧的名字就是那个名字"（改名式失配——
  帧照样落、审计流里查不到，是 §7.24 里点名最容易漏的一类）；M3 打"落流侧脱敏"（I5 导出把正文带进离线件）；
  M4 打"存量回填真的覆盖到旧 command 帧"；M5/M6 打"列的可空且无默认"——这两刀不是代码风格，
  是**存量库不改列则新代码第一次写 nil 就炸**的原形（M5 的坏读数是一条 23502，M6 的是 gorm 把 nil 折回 false）。
- **M7/M8 是 C 相续刀（lane 3）补的两格**，打的是 M1 没碰的那一面：M1 折的是 **command 帧**的 ok
  （`nil`→`verdict(true)`），而**失败回包（event 帧）**的取值全仓没有一条腿读过——
  `TestB20BCommandFramesCarryNoVerdict` 对 event 帧只断 `l.Ok == nil` 不成立（它的注释自陈
  "真/假由各帧自己的腿判"，而那些腿各自判的是 happyReply 与错误归类，`err != nil` 那条出路写下的
  ok 从未被读过）。于是 executor.go 里 `verdict(err == nil)`（comment_send 回包帧）与
  `verdict(false)`（步失败回包帧）两处换成常量 `verdict(true)` 都无人红，而 accepted 轴重新退化成
  常量正是本行立项要消灭的形状。**两处各成一刀**（B 相那张表也这么要求），腿是本泳道补的
  `TestB20BFailedDispatchEventFramesSayFailed`：真跑一次 dispatchStep 失败出路
  （扩展侧对 comment_send 回 `send_button_not_interactable`，零副作用早返、帧数确定），
  把那一路上落的两帧各钉一格，并先断 payload 的 error 文本非空（否则就是断在一帧成功回包上）。
  两刀的期望红集合相同（同一条腿里的两个断言），这不叫重复格：注掉任一处都只红那一处对应的断言。
- **一处形状漂移要记在这里**：§7.24 那行把 M4 写成"把回填范围写成不命中的 `direction`"，磁盘上的**原始**电池
  `/tmp/b20b_battery.sh:60-61` 确实是这一形（`AND 1 = 0`），而后来那两份**复跑**脚本
  （`/tmp/b20b_remute.py` / `b20b_remute2.py` / `b20b_mut_rerun.py`）把它换成了"整段回填循环摘除"。
  **常驻副本取 §7.24 / 原始电池那一形**，理由不是偏好，是后一种形状实测跑不到判据：把 `for { … }` 整段换成
  一句注释会让 `Up()` 少掉唯一一条出口 return，Go 直接报 `missing return` ⇒ `settled=0`、
  一条用例名都点不出来（本文件首轮实跑读数：`/tmp/b20b_battery.log:9` =
  `M4 BROKEN=判不了 settled=0 红=—`）。编译红不是牙（口径同上），注码必须打在**语义**上而不是**语法**上。

口径（沿用批16/17/18/19x/20c/20d/20f/22 电池）：
- 控制组必须 rc==0、settled>0、skip==0、且不许有任何红名；
- 每格断言 `settled == 该包控制组 settled`（一条用例 panic 会带走整个二进制，"FAIL=1"看着像杀其实没跑完）、
  `skip==0`、红而没点名判红、红集合**恰好等于**期望集合（多一条就是别人被牵连，不算这刀的牙）；
- BUILD FAILED / panic / 红而无名 一律 BROKEN，**不计入杀掉**；
- 锚点命中必须恰好一次；还原后逐文件比 md5；只在私有克隆里注码，绝不碰共享工作树。

用法：python3 scripts/mut_command_log_ok_b20b.py [--keep] [--clone DIR]
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
US = "user-server"
EXEC_REL = f"{US}/internal/browser_automation/service/executor.go"
MIG_REL = f"{US}/internal/migration/migrations/v3_44_0_browser_command_log_ok_tristate_migration.go"
LANE_PATHS = [f"{US}/internal/browser_automation", f"{US}/internal/model", f"{US}/internal/migration"]
ANSI = re.compile(r"\x1b\[[0-9;]*m")

PKG_SERVICE = "./internal/browser_automation/service/"
PKG_MIG = "./internal/migration/migrations/"
R_SVC = "TestB20B"
R_MIG = "TestCommandLogOkTriState"

# 每格期望的红集合（顶层用例名）。写这条式的依据是"这刀破坏了哪条承诺 ⇒ 只有那条腿会红"，
# 不是先跑一遍把结果抄回来。
L_NOVERDICT = "TestB20BCommandFramesCarryNoVerdict"
L_JUDGE = "TestB20BWriteStepEmitsConfirmJudgeFrame"
L_UNVER = "TestB20BUnverifiedWriteConfirmFrameSaysSo"
L_BACKFILL = "TestCommandLogOkTriStateMigration_UpAndBackfill"
# M7/M8（C 相续刀）：失败出路的 event 回包帧必须说「败」——两条格共用的那条窄腿
L_FAILEDEVENT = "TestB20BFailedDispatchEventFramesSayFailed"


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag}: 锚点命中 {n} != 1（先修锚点，不许改期望）")
    out = text.replace(old, new)
    if out == text:
        raise SystemExit(f"{tag}: 替换后内容与原文相同（无效变异）")
    return out


def cuts():
    """六刀。(code, desc, rel文件, old, new, 包, -run, 期望红集合)"""
    return [
        ("M1", "command 帧退回带结论（A2 那根轴又折回一列）", EXEC_REL,
         'e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "command", step.Action, cmdPayload, 0, nil)',
         'e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "command", step.Action, cmdPayload, 0, verdict(true))',
         PKG_SERVICE, R_SVC, {L_NOVERDICT}),
        ("M2", "确认帧改名（帧照落、审计流里查不到这个名字）", EXEC_REL,
         '\twriteConfirmFrame = "write_confirm"', '\twriteConfirmFrame = "write_confirm_x"',
         PKG_SERVICE, R_SVC, {L_JUDGE, L_UNVER}),
        ("M3", "摘掉落流侧 evidence 脱敏（正文进离线件）", EXEC_REL,
         'map[string]any{"result": json.RawMessage(auditEventPayload(result))}, dur, verdict(true))',
         'map[string]any{"result": json.RawMessage(result)}, dur, verdict(true))',
         PKG_SERVICE, R_SVC, {L_JUDGE}),
        ("M4", "回填范围不命中（旧下发帧的假 ✓ 一条都没被改）", MIG_REL,
         "WHERE direction = 'command' AND ok IS NOT NULL",
         "WHERE direction = 'command_never_matches' AND ok IS NOT NULL",
         PKG_MIG, R_MIG, {L_BACKFILL}),
        ("M5", "不摘 NOT NULL（新代码第一次写 nil 就炸）", MIG_REL,
         "`ALTER TABLE browser_command_log ALTER COLUMN ok DROP NOT NULL`", "`SELECT 1`",
         PKG_MIG, R_MIG, {L_BACKFILL}),
        ("M6", "不摘 DEFAULT（gorm 会把 nil 折回 false）", MIG_REL,
         "`ALTER TABLE browser_command_log ALTER COLUMN ok DROP DEFAULT`", "`SELECT 1`",
         PKG_MIG, R_MIG, {L_BACKFILL}),
        # ---- C 相续刀（lane 3）：失败回包那两处 ok 各自一刀。M1 钉的是 command 帧
        # （`ok=nil` 那一轴），这两刀钉的是 event 帧的**取值**：全仓原本没有一条腿读过
        # `err != nil` 那条出路写下的 ok（三条 b20b 腿走 happyReply，b21/b20c 腿只判错误归类），
        # 所以两处换成常量 true 都无人红——那正是 A2 立项要消灭的"accepted 轴退化成常量"。
        # 钉它们的是本泳道补的窄腿 TestB20BFailedDispatchEventFramesSayFailed
        # （真 dispatchStep 失败出路：comment_send 回 send_button_not_interactable）。
        ("M7", "comment_send 的失败回包帧带常量结论（`verdict(err == nil)`→`verdict(true)`）", EXEC_REL,
         'map[string]any{"error": sendErrText(err)}, time.Since(start).Milliseconds(), verdict(err == nil))',
         'map[string]any{"error": sendErrText(err)}, time.Since(start).Milliseconds(), verdict(true))',
         PKG_SERVICE, R_SVC, {L_FAILEDEVENT}),
        ("M8", "步失败的 event 回包帧带常量结论（`verdict(false)`→`verdict(true)`）", EXEC_REL,
         'e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action, '
         'map[string]any{"error": lastErr}, dur, verdict(false))',
         'e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", step.Action, '
         'map[string]any{"error": lastErr}, dur, verdict(true))',
         PKG_SERVICE, R_SVC, {L_FAILEDEVENT}),
    ]


def lane_overlays() -> tuple[list[str], list[str]]:
    """返回 (要覆盖进克隆的脏文件, 要在克隆里删掉的文件)。口径与 mut_hub_media_backfill.py 一致。

    两条都要搬：`--shared` 克隆 = HEAD + 覆盖，工作树里一条 ` D` 若不搬，克隆就在跑一份
    "文件还在"的树（它的绿不代表工作树的绿）；覆盖面若压回 `.go`，判据住在 md/sh 里的那部分
    就永远不进电池——复验面窄于改动面。
    """
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到本泳道脏文件清单：" + r.stderr[-200:])
    mods, dels = [], []
    for line in r.stdout.splitlines():
        st = line[:2]
        p = line[3:].split(" -> ")[-1].strip().strip('"')
        if "D" in st:
            dels.append(p)
        else:
            mods.append(p)
    if not mods:
        raise SystemExit("脏文件清单为空——克隆里跑的是 HEAD，测不到本批改动（宁可停机也别假绿）")
    return mods, dels


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
    mods, dels = lane_overlays()
    for rel in mods:
        src = ROOT / rel
        if not src.exists():
            raise SystemExit(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
    for rel in dels:
        (clone / rel).unlink(missing_ok=True)
    print(f"覆盖 {len(mods)} 个脏文件、同步 {len(dels)} 个删除进克隆")
    hostenv = ROOT / US / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / US / ".env")
    return clone


def run_test(clone: Path, pkg: str, filt: str) -> dict:
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-b20bmut")
    envf = clone / US / ".env"
    pw = ""
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD="):
                pw = line.split("=", 1)[1].strip()
                break
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    if pw and "POSTGRES_TEST_PASSWORD" not in os.environ:
        env["POSTGRES_TEST_PASSWORD"] = pw
    p = subprocess.run(["go", "test", pkg, "-run", filt, "-count=1", "-v"],
                       cwd=clone / US, capture_output=True, text=True, timeout=1800, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    top = lambda kind: len(re.findall(rf"^--- {kind}: ", out, re.M))
    redtop = sorted({m.split("/")[0] for m in re.findall(r"^--- FAIL: (\S+)", out, re.M)})
    return {"rc": p.returncode, "out": out,
            "settled": top("PASS") + top("FAIL") + top("SKIP"),
            "passed": top("PASS"), "skipped": top("SKIP"), "red": redtop,
            "rednames": sorted(re.findall(r"^--- FAIL: (\S+)", out, re.M)),
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "cannot use" in out or "undefined:" in out}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    ap.add_argument("--cells", default="",
                    help="只跑这些格（逗号分隔，如 M4）；控制组照跑，用于修单格后不复跑全电池")
    args = ap.parse_args()
    only = {c.strip() for c in args.cells.split(",") if c.strip()}
    # 差集不是对称差：--cells M4 时"全部格名"里有五个不在请求里，对称差会把它们报成不存在的格。
    unknown = only - {c[0] for c in cuts()}
    if only and unknown:
        raise SystemExit(f"--cells 里有不存在的格：{sorted(unknown)}")

    tmp = Path(args.clone or tempfile.mkdtemp(prefix="b20bmut-"))
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}")
    clone = prepare(tmp)

    files = {rel: clone / rel for rel in (EXEC_REL, MIG_REL)}
    originals = {rel: read(p) for rel, p in files.items()}
    basemd5 = {rel: md5_bytes(p) for rel, p in files.items()}

    controls = {}
    for pkg, filt in ((PKG_SERVICE, R_SVC), (PKG_MIG, R_MIG)):
        c = run_test(clone, pkg, filt)
        label = "service" if pkg == PKG_SERVICE else "migrations"
        print(f"\n[{label}] 控制组 rc={c['rc']} settled={c['settled']} passed={c['passed']} "
              f"skip={c['skipped']} 红名={c['rednames']}")
        if c["rc"] != 0 or c["settled"] == 0 or c["skipped"] > 0 or c["red"]:
            print(c["out"][-4000:])
            raise SystemExit(f"[{label}] 控制组不干净——后面所有红/绿都不可信")
        controls[pkg] = c["settled"]

    problems = []
    ran = 0
    for code, desc, rel, old, new, pkg, filt, expect in cuts():
        if only and code not in only:
            continue
        ran += 1
        src = originals[rel]
        try:
            mutated = sub_once(src, old, new, code)
        except SystemExit as e:
            problems.append(str(e))
            print(f"{code:<4} {desc[:54]:<56} BROKEN=注码失效")
            continue
        files[rel].write_text(mutated)
        try:
            r = run_test(clone, pkg, filt)
        finally:
            files[rel].write_text(src)
            if md5_bytes(files[rel]) != basemd5[rel]:
                raise SystemExit(f"{code} 还原后 md5 不一致，停机")
        if r["rc"] == 0 and not r["red"]:
            v = "存活=洞"
        elif r["panicked"] or r["buildfailed"] or not r["red"]:
            v = "BROKEN=判不了"
        elif r["settled"] != controls[pkg] or r["skipped"] > 0:
            v = "BROKEN=没跑完"
        elif set(r["red"]) != expect:
            v = "BROKEN=红集合不符"
        else:
            v = "杀掉"
        print(f"{code:<4} {desc[:54]:<56} {v:<14} settled={r['settled']} "
              f"skip={r['skipped']} 红={','.join(r['red']) or '—'}")
        if v != "杀掉":
            problems.append(f"{code} {v}：{desc}")
            if v == "BROKEN=红集合不符":
                print(f"     期望={sorted(expect)} 实际={r['red']}")
            cause = [l.strip()[:170] for l in r["out"].splitlines()
                     if "Error:" in l or "mismatch" in l or "want" in l]
            for c in cause[:6]:
                print("     红因: " + c)
            if v == "存活=洞":
                print(r["out"][-2500:])

    # 子集运行时必须在判定行上写清楚跑了几个：一屏 "六格" 的字样配上只跑了两格的读数，
    # 就是另一份"把没跑的说成跑了"的记录。
    scope = f"{ran}/{len(cuts())} 格" + (f"（--cells {','.join(sorted(only))}）" if only else "")
    print("\n===== 判定：" + (f"{scope}，逐格被杀，无存活" if not problems
                            else f"{scope}，{len(problems)} 格未杀/BROKEN：" + "; ".join(problems)) + " =====")
    if not args.keep:
        shutil.rmtree(tmp, ignore_errors=True)
    else:
        print(f"保留作业目录：{tmp}")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
