#!/usr/bin/env python3
"""批17 变异电池：§8.2-1(a) stable 三份内联 + (b) 点后身份复核，逐消费点验牙。

为什么要有这条电池（而不是"测试全绿"就算完）：
- (a) 是**三份内联副本**（injClick / injClickNear / injPostCommentSend）。注入函数必须自包含
  （§7.6 的断链教训），所以三份抽不成一个共享函数。共享函数只要一处有牙就能骗过"整体看像修好了"，
  三份则必须逐份注码、逐份看红——M1/M2/M3 就是为这个存在的。
- (b) 的形状约束（复核必须落在 CDP 那个 try 之外）在源码里是一条注释 + 一个位置，注释不会自己守住
  位置。M5 把复核失败改回"当成 CDP 不可用 → DOM 兜底再点一次"，若有用例盯着"零双发"它必须红；
  不红就是本批最贵的那个洞还开着。
- M6~M9 盯这条腿自身的四种偷懒形态：静默 ok、容差无限大、去掉 navigated 跳过、让只读步也付这次注入。
- G1~G4 盯 Go 侧那几行连线（帧里带不带 verify_identity、is_write 有没有喂给复核、结果记不记
  identity_checked、element_moved 有没有被误判成"从未发生"）。连线断了，扩展侧再对有毛用都没有。

口径（沿用批16 电池）：控制组必须 rc==0、ran>0、skip==0，否则整轮判"无法判定"直接停机；
每个变异体 cp 备份 + 逐次 md5 比对还原；注码必须断言命中恰好一次（静默的"没改到"会让电池自己假绿）；
报告按用例名点名被杀用例，只说"红了"不算杀；只在私有副本 / 私有 --shared 克隆里注码，
绝不碰共享工作树（并行会话在里面提交）。

用法：python3 scripts/mut_actionability_b17.py [--js-only|--go-only] [--keep] [--clone DIR]
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
import time
from pathlib import Path

# 脚本在 <repo>/scripts/ 下 ⇒ 根 = 上一级。**不硬编码仓名**（改名克隆必须照样能跑：
# 这是从别的门脚本学到的坑——定根写死仓名 ⇒ 改名克隆里 rc=1 零输出）。
ROOT = Path(__file__).resolve().parent.parent
# 逐格原始输出落进仓库树：早先只随 stdout 走、由调用方重定向到 /tmp，重启即蒸发 ⇒
# 台账里的读数没有产物可对。目录带趟次戳、不复用；`.gitignore` 需为本轮次开例外。
LOGDIR = ROOT / "docs/superpowers/specs/ledger/logs/B17action" / time.strftime("%Y%m%d-%H%M%S")


from redact import scrub  # 落盘前脱敏：常驻产物要过 gitleaks（见 scripts/redact.py 的 why）
def dump(tag, out):
    LOGDIR.mkdir(parents=True, exist_ok=True)
    (LOGDIR / (re.sub(r"[^A-Za-z0-9_.-]", "-", tag) + ".log")).write_text(scrub(out), encoding="utf-8")

WEB = ROOT / "user-web" / "browser_automation"
PRIM_REL = Path("src/core/primitives.js")
JS_TEST = "test/batch17-actionability.test.js"
SVC = "internal/browser_automation/service"
# 克隆里没有本泳道的未提交改动，必须整文件覆盖过去，被测的才是"工作区里这份代码"。
GO_OVERLAY = [
    f"user-server/{SVC}/executor.go",
    f"user-server/{SVC}/hand.go",
    f"user-server/{SVC}/write_ledger.go",
    f"user-server/{SVC}/hand_frames_b9_test.go",
    f"user-server/{SVC}/verify_identity_b17_test.go",
    "user-web/browser_automation/src/core/primitives.js",
]
GO_RUN = "Identity|Recheck|WriteClick|ClickNear|ClickResult|NeverExecuted|ElementMoved|PreDispatch"

ANSI = re.compile(r"\x1b\[[0-9;]*m")


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    """恰好命中一次，否则当场死——静默的"没改到"是最便宜的假绿。"""
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1 次）：{old[:90]!r}")
    return text.replace(old, new, 1)


# ------------------------------------------------------------------ JS 侧
def js_prepare(dst: Path) -> Path:
    work = dst / "web"
    work.mkdir(parents=True)
    for item in ("src", "test", "package.json", "vitest.config.js"):
        s = WEB / item
        if not s.exists():
            raise SystemExit(f"缺少 {s}")
        (shutil.copytree if s.is_dir() else shutil.copy2)(s, work / item)
    nm = WEB / "node_modules"
    if not nm.is_dir():
        raise SystemExit(f"{nm} 不存在——先在 user-web/browser_automation 里 npm install")
    (work / "node_modules").symlink_to(nm.resolve())
    return work


def js_run(work: Path):
    p = subprocess.run(["npx", "vitest", "run", JS_TEST], cwd=work,
                       capture_output=True, text=True, timeout=900, env=os.environ)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = [re.sub(r"\s+\d+ms$", "", ln.split("×", 1)[1].strip())
              for ln in out.splitlines() if "×" in ln]
    ran = int(re.search(r"Tests\s+(\d+) passed", out).group(1)) if re.search(r"Tests\s+(\d+) passed", out) else 0
    skipped = int(re.search(r"(\d+) skipped", out).group(1)) if re.search(r"(\d+) skipped", out) else 0
    return p.returncode, killed, ran, skipped, out


# ------------------------------------------------------------------ Go 侧
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
    # .env 不进 git，克隆里没有 ⇒ 需要 DB 的用例会 skip，控制组就不干净（skip==0 是本电池的硬门）。
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path):
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    # 公共 go-build 缓存已 68G / 卷 92% 满：缓存被 trim 时会随机报 could not import（与代码无关）。
    # 电池一律走私有缓存，红/绿才只反映代码。
    env.setdefault("GOCACHE", "/tmp/gocache-b17mut")
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


# ------------------------------------------------------------------ 变异表
# 三份内联副本里同一行 settleBox 判据的缩进各不相同（8/12/6 空格），
# "\n + 缩进 + 该行" 因此是逐份唯一的锚——一份一处，谁被抽掉谁红。
DEADLINE = "if (Date.now() >= deadline) return { error: 'unstable' };"
INDENTS = {"M1": "        ", "M2": "            ", "M3": "      "}
RECHECK_CLICK = "          if (cmd.verify_identity && !navigated) {"


def js_mutants():
    return [
        ("M1", "第 1 份 stable（injClick）到点装死：deadline 到期把最后一帧当结算"),
        ("M2", "第 2 份 stable（injClickNear）同一处装死"),
        ("M3", "第 3 份 stable（injPostCommentSend 提交点）同一处装死"),
        ("M4", "click 分支整条点后复核删掉（写步不再认身份）"),
        ("M5", "复核失败改回「当成 CDP 不可用 → DOM 兜底再点一次」（本批立的形状）"),
        ("M6", "复核跑不动时静默 ok（未知态冒充成功）"),
        ("M7", "复核容差放大到 1e9 px（等于只查 selector 解不解析）"),
        ("M8", "去掉 navigated 跳过（把跳转误判成元素挪位）"),
        ("M9", "只读步也强制复核（拿读步的时延预算替写步买单）"),
        ("M10", "click_near 分支的复核单独断线（第二个消费点）"),
        ("M11", "SW 侧 dispatch 合成一句 *_not_interactable（拆掉 Go 判「从未派发」的前提）"),
    ]


def apply_js(src: str, code: str) -> str:
    if code in INDENTS:
        anchor = "\n" + INDENTS[code] + DEADLINE
        return sub_once(src, anchor,
                        "\n" + INDENTS[code] + DEADLINE.replace("return { error: 'unstable' };", "return { box: cur };"),
                        code)
    if code == "M4":
        return sub_once(src, "\n" + RECHECK_CLICK, "\n          if (false && !navigated) {", "M4")
    if code == "M8":
        return sub_once(src, RECHECK_CLICK, "          if (cmd.verify_identity) {", "M8")
    if code == "M9":
        return sub_once(src, RECHECK_CLICK, "          if (!navigated) {", "M9")
    if code == "M10":
        return sub_once(src, "\n          if (cmd.verify_identity) {", "\n          if (false) {", "M10")
    if code == "M5":
        old = ("              await executeInTab(tabId, injClickIdentityCheck, [sel, probe.x, probe.y, IDENTITY_RECHECK_TOLERANCE_PX]);\n"
               "            } catch (e) {\n"
               "              throw asIdentityVerdict(e);\n"
               "            }")
        new = ("              await executeInTab(tabId, injClickIdentityCheck, [sel, probe.x, probe.y, IDENTITY_RECHECK_TOLERANCE_PX]);\n"
               "            } catch (e) {\n"
               "              const fb = await executeInTab(tabId, injClick, [sel, 'fallback']).catch(() => null);\n"
               "              if (fb?.ok) return { ...fb, channel: 'dom_fallback' };\n"
               "              throw e;\n"
               "            }")
        return sub_once(src, old, new, "M5")
    if code == "M6":
        old = ("            } catch (e) {\n"
               "              throw asIdentityVerdict(e);\n"
               "            }\n"
               "            return { ok: true, navigated, channel: 'cdp', identity_checked: true };")
        new = ("            } catch (e) {\n"
               "              return { ok: true, navigated, channel: 'cdp', identity_checked: true };\n"
               "            }\n"
               "            return { ok: true, navigated, channel: 'cdp', identity_checked: true };")
        return sub_once(src, old, new, "M6")
    if code == "M7":
        return sub_once(src, "const IDENTITY_RECHECK_TOLERANCE_PX = 5;",
                        "const IDENTITY_RECHECK_TOLERANCE_PX = 1e9;", "M7")
    if code == "M11":
        # 派发之后（SW 侧 dispatch 里）合成一句 *_not_interactable：Go 侧据此判「从未派发」，
        # 这个前提必须由 dispatch 段零合成来守——静态锁不许只是句注释。
        old = ("              await executeInTab(tabId, injClickIdentityCheck, [probe.selector, probe.x, probe.y, IDENTITY_RECHECK_TOLERANCE_PX]);\n"
               "            } catch (e) {\n"
               "              throw asIdentityVerdict(e);\n")
        new = ("              await executeInTab(tabId, injClickIdentityCheck, [probe.selector, probe.x, probe.y, IDENTITY_RECHECK_TOLERANCE_PX]);\n"
               "            } catch (e) {\n"
               "              throw new Error('send_button_not_interactable: SW 侧改写了复核结论');\n")
        return sub_once(src, old, new, "M11")
    raise SystemExit(f"未知变异体 {code}")


def go_mutants():
    """每个变异体的锚都写成"恰好命中一次"的形状（两处同文的字面量靠上下文行区分），
    这样一格一连线：click 与 click_near 的帧/结果各拆两刀，谁断线谁红，不互相顶包。"""
    return [
        ("G1", "hand.go click 帧不再请求点后复核（复核在 Go 侧断线）",
         f"user-server/{SVC}/hand.go",
         '"action": "click", "tab_id": tabID, "target": target,\n\t\t"verify_identity": verifyIdentity,\n',
         '"action": "click", "tab_id": tabID, "target": target,\n'),
        ("G2", "executor.go click 的 is_write 传成死 false（判据不再喂给复核）",
         f"user-server/{SVC}/executor.go",
         "e.hand.click(ctx, userID, tabID, step.Target, stepRow.IsWrite)",
         "e.hand.click(ctx, userID, tabID, step.Target, false)"),
        ("G3", "executor.go click 结果不再记 identity_checked（跑没跑过查不到）",
         f"user-server/{SVC}/executor.go",
         '"navigated": res["navigated"] == true, "channel": res["channel"],\n\t\t\t"identity_checked": res["identity_checked"],\n',
         '"navigated": res["navigated"] == true, "channel": res["channel"],\n'),
        ("G4", "write_ledger.go 把 element_moved 判成「从未发生」（挪了家却记成没动过）",
         f"user-server/{SVC}/write_ledger.go",
         'strings.Contains(msg, "_inject_timeout_") || strings.Contains(msg, "_not_found")',
         'strings.Contains(msg, "element_moved") || strings.Contains(msg, "_inject_timeout_") || strings.Contains(msg, "_not_found")'),
        ("G5", "hand.go click_near 帧不再请求复核（第二个消费点单独断线）",
         f"user-server/{SVC}/hand.go",
         '"action": "click_near", "tab_id": tabID, "anchor": anchor, "button_text": buttonText,\n\t\t"verify_identity": verifyIdentity,\n',
         '"action": "click_near", "tab_id": tabID, "anchor": anchor, "button_text": buttonText,\n'),
        ("G6", "executor.go click_near 的 is_write 传成死 false",
         f"user-server/{SVC}/executor.go",
         "e.hand.clickNear(ctx, userID, tabID, step.Anchor, step.ButtonText, stepRow.IsWrite)",
         "e.hand.clickNear(ctx, userID, tabID, step.Anchor, step.ButtonText, false)"),
        ("G7", "派发前拒绝（*_not_interactable）重新被记成提交尝试（闸门误伤回来了）",
         f"user-server/{SVC}/write_ledger.go",
         ' ||\n\t\tstrings.Contains(msg, "_not_interactable")', ""),
    ]


def dup_report(tag: str, kills: dict) -> None:
    """杀掉的用例集合逐字相同的两格 = 两条断言其实盯同一件事（覆盖重复，不是覆盖）。
    只点名不判失败：同族可能是有意为之（本批 click 与 click_near 两半本就同形），
    但要让读报告的人看得见，而不是以为"绿了 16 格"就等于 16 件事。"""
    items = sorted((k, v) for k, v in kills.items())
    for i in range(len(items)):
        for j in range(i + 1, len(items)):
            if items[i][1] and items[i][1] == items[j][1]:
                print(f"  [{tag}] 同族：{items[i][0]} 与 {items[j][0]} 杀掉的用例集合相同"
                      f"（{len(items[i][1])} 条）")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--js-only", action="store_true")
    ap.add_argument("--go-only", action="store_true")
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()
    from battlog import tee_to  # 判定行与逐格产物同处一地（LOGDIR/00-run.log）
    tee_to(LOGDIR / "00-run.log")

    tmp = Path(args.clone or tempfile.mkdtemp(prefix="b17mut-"))
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}")
    problems = []

    if not args.go_only:
        work = js_prepare(tmp)
        prim = work / PRIM_REL
        orig = read(prim)
        base_md5 = md5_bytes(prim)
        rc, killed, ran, skipped, out = js_run(work)
        dump("00-control-js", out)
        print(f"\n[JS] 控制组 rc={rc} passed={ran} skipped={skipped} 红名={killed}")
        if rc != 0 or ran == 0 or skipped > 0 or killed:
            print(out[-3000:])
            raise SystemExit("[JS] 控制组不干净——后面所有红/绿都不可信")
        jskill = {}
        for code, desc in js_mutants():
            try:
                prim.write_text(apply_js(orig, code))
            except SystemExit as e:
                problems.append(str(e))
                continue
            rc, killed, ran, skipped, out = js_run(work)
            dump(code, out)
            verdict = "杀掉" if (rc != 0 and killed) else ("存活=洞" if rc == 0 else "红了但没点名")
            print(f"{code:<4} {desc[:56]:<58} {verdict:<7} pass={ran} skip={skipped} ｜ "
                  + " | ".join(k[:64] for k in killed[:2]))
            if verdict != "杀掉":
                problems.append(f"[JS] {code} {verdict}：{desc}")
                print(out[-2500:])
            jskill[code] = set(killed)
            prim.write_text(orig)
            if md5_bytes(prim) != base_md5:
                raise SystemExit(f"[JS] {code} 还原后 md5 不一致，停机")
        dup_report("JS", jskill)
        print("[JS] 已全量还原（md5 一致）")

    if not args.js_only:
        clone = go_prepare(tmp)
        rels = sorted({m[2] for m in go_mutants()})
        files = {rel: clone / rel for rel in rels}
        originals = {rel: read(p) for rel, p in files.items()}
        gkill = {}
        basemd5 = {rel: md5_bytes(p) for rel, p in files.items()}
        rc, killed, ran, skipped, out = go_run(clone)
        dump("00-control-go", out)
        print(f"\n[Go] 控制组 rc={rc} ran={ran} skip={skipped} FAIL={killed}")
        if rc != 0 or ran == 0 or skipped > 0:
            print(out[-4000:])
            raise SystemExit("[Go] 控制组不干净")
        for code, desc, rel, old, new in go_mutants():
            try:
                mutated = sub_once(originals[rel], old, new, code)
            except SystemExit as e:
                problems.append(str(e))
                continue
            files[rel].write_text(mutated)
            rc, killed, ran, skipped, out = go_run(clone)
            dump(code, out)
            verdict = "杀掉" if (rc != 0 and killed) else ("存活=洞" if rc == 0 else "红了但没点名")
            gkill[code] = set(killed)
            print(f"{code:<4} {desc[:56]:<58} {verdict:<7} ran={ran} skip={skipped} ｜ "
                  + " | ".join(k[:60] for k in killed[:2]))
            if verdict != "杀掉":
                problems.append(f"[Go] {code} {verdict}：{desc}")
                print(out[-3000:])
            files[rel].write_text(originals[rel])
            if md5_bytes(files[rel]) != basemd5[rel]:
                raise SystemExit(f"[Go] {code} 还原后 md5 不一致，停机")
        dup_report("Go", gkill)
        print("[Go] 已全量还原（md5 一致）")

    if not args.keep:
        shutil.rmtree(tmp, ignore_errors=True)
    if problems:
        print("\n===== 电池判定：有洞 =====")
        for x in problems:
            print("  ✗", x)
        return 1
    parts = []
    if not args.go_only:
        parts.append(f"JS {len(js_mutants())} 格")
    if not args.js_only:
        parts.append(f"Go {len(go_mutants())} 格")
    print(f"\n===== 电池判定：{' + '.join(parts)} 逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
