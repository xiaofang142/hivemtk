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
      python3 scripts/mut_actionability_b17.py --check     # JS 11 + Go 7 格锚点试打，不跑 vitest 也不跑 go test
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

from mut_dispose import dispose, workdir   # 两道闸：装架前挡危险 --clone，收尾只回收私有克隆

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
# 批20c 起本泳道的复核闸门有两份用例：批17 的两处消费点 + 批20c 的第三处（comment_send）。
# 电池必须两都跑——只跑新那份的话，M10/M13 这种「单独断一条线」的变异会分不清是谁红的。
JS_TESTS = ["test/batch17-actionability.test.js", "test/batch20c-send-identity.test.js"]
SVC = "internal/browser_automation/service"
# 克隆里没有本泳道的未提交改动，必须整文件覆盖过去，被测的才是"工作区里这份代码"。
# 覆盖清单不再手写：批20b 把 appendCommandLog 的 ok 改成 *bool 之后，手写清单必然漏文件
# （漏一个用 bool 调用的旧测试 = 克隆里 [build failed]，电池整个失声）。改成按 git 脏态自动枚举
# 本泳道三个包下的 .go，谁改过谁进覆盖层。
LANE_PATHS = ["user-server/internal/browser_automation",
              "user-server/internal/model",
              "user-server/internal/migration"]


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


GO_OVERLAY_JS = ["user-web/browser_automation/src/core/primitives.js"]

GO_RUN = ("Identity|Recheck|WriteClick|ClickNear|ClickResult|NeverExecuted|ElementMoved|PreDispatch"
          "|CommentSend")

ANSI = re.compile(r"\x1b\[[0-9;]*m")

# 控制组跑出来的用例总数，逐格变异后必须一字不差地被重新结算（见 verdict）。
CONTROL = {"js": 0, "go": 0}


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


def js_run(work: Path) -> dict:
    p = subprocess.run(["npx", "vitest", "run"] + JS_TESTS, cwd=work,
                       capture_output=True, text=True, timeout=900, env=os.environ)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = [re.sub(r"\s+\d+ms$", "", ln.split("×", 1)[1].strip())
              for ln in out.splitlines() if "×" in ln]
    # 汇总行两种形状都要认：全绿时是 "Tests  30 passed (30)"，有红时是
    # "Tests  4 failed | 26 passed (30)"。旧写法只认前者 ⇒ 红轮里 passed 恒读成 0，
    # 「整文件崩了只点出 1 个名」和「30 条跑了 3 条红」在报告上长得一模一样（假杀）。
    m = re.search(r"Tests\s+(?:\d+ failed \| )?(?:\d+ skipped \| )?(\d+) passed.*?\((\d+)\)", out)
    passed = int(m.group(1)) if m else 0
    total = int(m.group(2)) if m else 0
    skipped = int(re.search(r"(\d+) skipped", out).group(1)) if re.search(r"(\d+) skipped", out) else 0
    return {"rc": p.returncode, "killed": killed, "total": total,
            "failed": total - passed - skipped, "skipped": skipped, "out": out}


# ------------------------------------------------------------------ Go 侧
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
    # 覆盖层 = 本泳道全部脏 .go（含未跟踪的新测试文件：克隆里没有它就 [build failed] 式失声）
    # + Go 侧静态锁要读的那份扩展源码（见 verify_identity 的 JS 侧锁）。
    for rel in lane_overlays() + GO_OVERLAY_JS:
        src = ROOT / rel
        if not src.exists():
            bail(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
    # .env 不进 git，克隆里没有 ⇒ 需要 DB 的用例会 skip，控制组就不干净（skip==0 是本电池的硬门）。
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path) -> dict:
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
    # 「被结算的顶层用例数」= PASS+FAIL+SKIP。一条用例 panic 会带走整个测试二进制，
    # 后面的用例连 === RUN 都留不下：那种红看着像杀（FAIL=1），其实剩下的腿根本没跑。
    settled = (len(re.findall(r"^--- PASS: ", out, re.M))
               + len(re.findall(r"^--- FAIL: ", out, re.M))
               + len(re.findall(r"^--- SKIP: ", out, re.M)))
    passed = len(re.findall(r"^--- PASS: ", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: ", out, re.M))
    return {"rc": p.returncode, "killed": killed, "total": settled, "passed": passed,
            "failed": settled - passed - skipped, "skipped": skipped, "out": out,
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "cannot use" in out or "undefined:" in out}


def verdict(r: dict, expect_total: int) -> str:
    """三态判定：杀掉 / 存活=洞 / BROKEN（红了但判不了）。
    BROKEN 绝不能混进「杀掉」——一条崩掉整个二进制的变异，和一条真正被断言抓住的变异，
    在「rc!=0 且点了名」这个旧口径下长得完全相同（本电池从别的批次学到的假杀形态）。"""
    if r["rc"] == 0 and not r["killed"]:
        return "存活=洞"
    if r.get("panicked") or r.get("buildfailed") or not r["killed"]:
        return "BROKEN=判不了"
    if r["total"] != expect_total or r["failed"] < 1 or r["skipped"] > 0:
        return "BROKEN=判不了"
    return "杀掉"


# ------------------------------------------------------------------ 变异表
# 三份内联副本里同一行 settleBox 判据的缩进各不相同（8/12/6 空格），
# "\n + 缩进 + 该行" 因此是逐份唯一的锚——一份一处，谁被抽掉谁红。
DEADLINE = "if (Date.now() >= deadline) return { error: 'unstable' };"
INDENTS = {"M1": "        ", "M2": "            ", "M3": "      "}
RECHECK_CLICK = "          if (cmd.verify_identity && !navigated) {"
# 第三份 probe（injPostCommentSend）回传的抖动半径那一行。**必须带前导换行**：三份同形但缩进
# 各异（6/10/4 空格），不带换行时 4 空格串是 6/10 空格串的子串，锚点会命中 3 次（首跑实测被
# sub_once 拦下）；带上换行后「4 空格紧跟 jitter」只可能是提交点那一份。
JITTER_LINE = "\n    jitter_radius: Math.min(r.width, r.height) / 2,\n"


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
        ("M12", "第三份 probe 不回传抖动半径（comment_send 落点静默退化成 ±3px）"),
        ("M13", "comment_send 分支的复核单独断线（§8.2-1 尾项＝本批立的第三个消费点）"),
        ("M14", "第三份 probe 不回传 selector（本批新增那处 pathOf 断线：复核没有再解析对象）"),
        ("M15", "pathOf 退化成 tagName（拿到的是页面上第一个同标签节点，不是被点的那个）"),
        ("M16", "未请求复核也照样回 identity_checked=true（把「没跑」洗成「跑过且过了」）"),
        ("M17", "comment_send 整块复核摘掉（静态锁「三处消费」唯一的红法；M13 抽条件它看不见）"),
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
        # 批20c 起 comment_send 也用同一句 `if (cmd.verify_identity) {`（同为 10 空格缩进），
        # 单行锚点会命中两次被 sub_once 拦下 ⇒ 带上下一行的 [probe.selector 才是 click_near 那一格。
        anchor = ("\n          if (cmd.verify_identity) {\n"
                  "            try {\n"
                  "              await executeInTab(tabId, injClickIdentityCheck, [probe.selector,")
        mutated = ("\n          if (false) {\n"
                   "            try {\n"
                   "              await executeInTab(tabId, injClickIdentityCheck, [probe.selector,")
        return sub_once(src, anchor, mutated, "M10")
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
    if code == "M12":
        # 替换成 "\n" 而不是 ""：锚点带前导换行，整串删掉会把上一行注释和 `};` 黏成一行
        # （注释吃掉闭括号＝语法错，vitest 报 collection error、一条用例名都点不出来——
        # 那是"红了但没点名"，不算杀）。留一个换行才是干净的"少回传一个字段"。
        return sub_once(src, JITTER_LINE, "\n", "M12")
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
    # ---- 批20c（comment_send = 第三个复核消费点）----
    if code == "M13":
        # 条件恒假：行为腿必须红，而"数消费点"的静态锁看不见这一刀（语句还在、条件被抽空）
        anchor = ("          if (cmd.verify_identity) {\n"
                  "            try {\n"
                  "              await executeInTab(tabId, injClickIdentityCheck, [btn.selector,")
        mutated = ("          if (false) {\n"
                   "            try {\n"
                   "              await executeInTab(tabId, injClickIdentityCheck, [btn.selector,")
        return sub_once(src, anchor, mutated, "M13")
    if code == "M17":
        # 整块摘掉：静态锁（消费点 want 3）唯一的红法就是真少一处，M13 那种抽条件它看不见
        old = ("          if (cmd.verify_identity) {\n"
               "            try {\n"
               "              await executeInTab(tabId, injClickIdentityCheck, [btn.selector, btn.x, btn.y, IDENTITY_RECHECK_TOLERANCE_PX]);\n"
               "            } catch (e) {\n"
               "              throw asIdentityVerdict(e);\n"
               "            }\n"
               "            return { ok: true, sent: true, identity_checked: true };\n"
               "          }\n")
        return sub_once(src, old, "", "M17")
    if code == "M14":
        # 替换成 "\n" 的理由见 M12；这一刀摘的是"复核有没有对象"的那条路径
        return sub_once(src, "\n    selector: pathOf(btn),\n", "\n", "M14")
    if code == "M15":
        # pathOf 退化成 'button'：两份内联里只砍第二份（用后随的 `const r = settled.box` 定位）
        old = ("    return parts.join(' > ');\n  };\n  const r = settled.box;")
        return sub_once(src, old, "    return 'button';\n  };\n  const r = settled.box;", "M15")
    if code == "M16":
        return sub_once(src, "          return { ok: true, sent: true };\n",
                        "          return { ok: true, sent: true, identity_checked: true };\n", "M16")
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
        # ---- 批20c：comment_send 的第三个消费点，请求侧/结论侧各拆一刀 ----
        ("G8", "hand.go comment_send 帧不再请求复核（第三个消费点在 Go 侧断线）",
         f"user-server/{SVC}/hand.go",
         '{"action": "comment_send", "tab_id": tabID, "verify_identity": verifyIdentity}',
         '{"action": "comment_send", "tab_id": tabID}'),
        ("G9", "executor.go comment_send 的 is_write 传成死 false（判据不再喂给复核）",
         f"user-server/{SVC}/executor.go",
         "e.hand.commentSend(ctx, userID, tabID, prepReq, stepRow.IsWrite)",
         "e.hand.commentSend(ctx, userID, tabID, prepReq, false)"),
        ("G10", "post_comment 读错键取复核结论（扩展报了 true 也恒落成 null）",
         f"user-server/{SVC}/executor.go",
         '"identity_checked": sendRes["identity_checked"],',
         '"identity_checked": sendRes["identity_check"],'),
        ("G11", "post_comment 把复核结论洗成 true（旧扩展/没复核的那一格也冒充复核过）",
         f"user-server/{SVC}/executor.go",
         '"identity_checked": sendRes["identity_checked"],',
         '"identity_checked": sendRes["identity_checked"] != false,'),
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
    ap.add_argument("--check", action="store_true",
                    help="只验锚点（每格原样在装架后的那份文件里必须恰好命中 1 次且真改到字节），不放刀不跑测试")
    args = ap.parse_args()
    from battlog import tee_to  # 判定行与逐格产物同处一地（LOGDIR/00-run.log）
    tee_to(LOGDIR / "00-run.log")

    tmp, owned = workdir(args.clone, prefix="b17mut-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}")

    if args.check:
        # 锚点预检的必要性：`old` 是逐字抄来的源码片段，代码一搬家它就命中 0 次，
        # 而放刀路径上 `sub_once` 命中 0 次是 SystemExit ⇒ 整族红被读成"用例回归了"。
        # 这一趟**照样装架**——锚点必须在电池真正要改的那份字节上验（拿工作树代替克隆，
        # 等于验了一份它从不触碰的副本），只是一次测试都不跑。
        bad = 0
        total = 0
        if not args.go_only:
            # 这里不走 `js_prepare`：它做的是"把 src/ 原样 copytree + 链 node_modules"，
            # 为的是能起 vitest。锚点预检只读那一份 js 文本，而 node_modules 是 gitignore 的
            # ⇒ 为一次不跑测试的预检逼出一趟 npm install 不值；copytree 逐字复制，
            # 读 WEB/PRIM_REL 与读 work/PRIM_REL 是同一份字节。
            orig = read(WEB / PRIM_REL)
            for code, _desc in js_mutants():
                total += 1
                try:
                    mutated = apply_js(orig, code)
                except SystemExit as e:
                    bad += 1
                    print(f"  ✗ [JS] {e}")
                    continue
                if mutated == orig:
                    bad += 1
                    print(f"  ✗ [JS] {code} 注码打完了而字节没变（这一格永不开火）")
        if not args.js_only:
            clone = go_prepare(tmp, owned)
            originals = {rel: read(clone / rel) for rel in sorted({m[2] for m in go_mutants()})}
            for code, _desc, rel, old, new in go_mutants():
                total += 1
                try:
                    mutated = sub_once(originals[rel], old, new, code)
                except SystemExit as e:
                    bad += 1
                    print(f"  ✗ [Go] {e}")
                    continue
                if mutated == originals[rel]:
                    bad += 1
                    print(f"  ✗ [Go] {code} 注码打完了而字节没变（这一格永不开火）")
        print(f"锚点校验：{total} 格，{bad} 格有问题")
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
        return 1 if bad else 0

    problems = []

    if not args.go_only:
        work = js_prepare(tmp)
        prim = work / PRIM_REL
        orig = read(prim)
        base_md5 = md5_bytes(prim)
        c = js_run(work)
        dump("00-control-js", c["out"])
        print(f"\n[JS] 控制组 rc={c['rc']} total={c['total']} failed={c['failed']} "
              f"skipped={c['skipped']} 红名={c['killed']}")
        if c["rc"] != 0 or c["total"] == 0 or c["skipped"] > 0 or c["killed"]:
            print(c["out"][-3000:])
            raise SystemExit("[JS] 控制组不干净——后面所有红/绿都不可信")
        CONTROL["js"] = c["total"]
        jskill = {}
        for code, desc in js_mutants():
            try:
                prim.write_text(apply_js(orig, code))
            except SystemExit as e:
                problems.append(str(e))
                continue
            r = js_run(work)
            dump(code, r["out"])
            v = verdict(r, CONTROL["js"])
            print(f"{code:<4} {desc[:56]:<58} {v:<12} total={r['total']} fail={r['failed']} "
                  f"skip={r['skipped']} ｜ " + " | ".join(k[:64] for k in r['killed'][:2]))
            if v != "杀掉":
                problems.append(f"[JS] {code} {v}：{desc}")
                print(r["out"][-2500:])
            jskill[code] = set(r["killed"])
            prim.write_text(orig)
            if md5_bytes(prim) != base_md5:
                raise SystemExit(f"[JS] {code} 还原后 md5 不一致，停机")
        dup_report("JS", jskill)
        print("[JS] 已全量还原（md5 一致）")

    if not args.js_only:
        clone = go_prepare(tmp, owned)
        rels = sorted({m[2] for m in go_mutants()})
        files = {rel: clone / rel for rel in rels}
        originals = {rel: read(p) for rel, p in files.items()}
        gkill = {}
        basemd5 = {rel: md5_bytes(p) for rel, p in files.items()}
        c = go_run(clone)
        dump("00-control-go", c["out"])
        print(f"\n[Go] 控制组 rc={c['rc']} settled={c['total']} passed={c['passed']} "
              f"skip={c['skipped']} FAIL={c['killed']}")
        if c["rc"] != 0 or c["total"] == 0 or c["skipped"] > 0 or c["killed"]:
            print(c["out"][-4000:])
            raise SystemExit("[Go] 控制组不干净")
        CONTROL["go"] = c["total"]
        for code, desc, rel, old, new in go_mutants():
            try:
                mutated = sub_once(originals[rel], old, new, code)
            except SystemExit as e:
                problems.append(str(e))
                continue
            files[rel].write_text(mutated)
            r = go_run(clone)
            dump(code, r["out"])
            v = verdict(r, CONTROL["go"])
            gkill[code] = set(r["killed"])
            print(f"{code:<4} {desc[:56]:<58} {v:<12} settled={r['total']} fail={r['failed']} "
                  f"skip={r['skipped']} ｜ " + " | ".join(k[:60] for k in r['killed'][:2]))
            if v != "杀掉":
                problems.append(f"[Go] {code} {v}：{desc}")
                print(r["out"][-3000:])
            files[rel].write_text(originals[rel])
            if md5_bytes(files[rel]) != basemd5[rel]:
                raise SystemExit(f"[Go] {code} 还原后 md5 不一致，停机")
        dup_report("Go", gkill)
        print("[Go] 已全量还原（md5 一致）")

    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
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
