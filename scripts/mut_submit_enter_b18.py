#!/usr/bin/env python3
"""批18 变异电池：type+submit_on_enter 的「提交键失败」这条线，逐消费点验牙。

为什么要有这条电池（而不是"测试全绿"就算完）：
- 本批的修复只有一处行为改变（扩展侧不再吞 pressEnter 的失败），但它的**价值全在两端接线**：
  扩展上抛的字符串 → Go 的 isNeverExecuted 必须判"不是从未发生" → 台账必须记 unattributed。
  任何一段断了，库里都长得跟"评论发出去了"一样。G1~G3 各盯一段连线/契约，J1~J7 盯扩展侧的七种偷懒形态。
- J2 与 G3 是同一件事的两半，且**故意跨语言**：J2 只改测试面断言（红因是文案里含 not_found token），
  G3 只改扩展源码里那个抛出字面量（Go 契约腿必须红）。把"契约"写成两段各自自证的平行文字，
  是这类跨语言锁最常见的假绿形态。
- J3/J4 盯"修过头"的两个方向：兜底通道补按 CDP Enter = 双发；把提交挪回输入 try = 一次步骤重打一遍正文。

口径（沿用批16/17 电池）：控制组必须 rc==0、ran>0、skip==0，否则整轮判"无法判定"停机；
每个变异体 cp 备份 + 逐次 md5 比对还原；注码必须断言命中恰好一次；只在私有副本 / 私有 --shared
克隆里注码，绝不碰共享工作树（并行会话在里面提交）。

用法：python3 scripts/mut_submit_enter_b18.py [--js-only|--go-only] [--keep] [--clone DIR]
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
WEB = ROOT / "user-web" / "browser_automation"
PRIM_REL = Path("src/core/primitives.js")
PRIM_REL_CLONE = Path("user-web/browser_automation/src/core/primitives.js")
JS_TEST = "test/batch18-submit-enter.test.js"
SVC = "internal/browser_automation/service"
# 克隆里没有本泳道的未提交改动，必须整文件覆盖过去，被测的才是"工作区里这份代码"。
# primitives.js 也在覆盖清单里：Go 契约腿直接读这份源码，不覆盖就是拿 HEAD 那份跑契约。
GO_OVERLAY = [
    f"user-server/{SVC}/executor.go",
    f"user-server/{SVC}/hand.go",
    f"user-server/{SVC}/write_ledger.go",
    f"user-server/{SVC}/submit_enter_b18_test.go",
    "user-web/browser_automation/src/core/primitives.js",
]
GO_RUN = "SubmitKey"

ANSI = re.compile(r"\x1b\[[0-9;]*m")

# 本批修复后的整段（七格变异都从这段形状出发；锚点唯一性由 sub_once 逐格自证）
SUBMIT_BLOCK = (
    "          if (submit && res.channel === 'cdp') {\n"
    "            try {\n"
    "              await cdpInput.pressEnter(tabId);\n"
    "            } catch (e) {\n"
    "              throw new Error('submit_key_not_dispatched: ' + String(e?.message || e), { cause: e });\n"
    "            }\n"
    "          }"
)

# J7 的锚点：整个 primitives.js 里 `{ cause: e }` 只有这一处（input.js 那处不在本电池范围内）
CAUSE_ARG = "throw new Error('submit_key_not_dispatched: ' + String(e?.message || e), { cause: e });"


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
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
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path):
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-b18mut")
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
def js_mutants():
    return [
        ("J1", "本批缺陷本体：pressEnter 挂回空回调把失败吞掉"),
        ("J2", "上抛文案换成含 *_not_found 的形状（Go 判成「从未派发」）"),
        ("J3", "去掉「只走 trusted 通道才补按」的守卫（兜底也按＝双发）"),
        ("J4", "提交键挪回输入 try 内（失败即触发兜底，整步重打正文）"),
        ("J5", "无条件按 Enter（丢掉 submit_on_enter 判据）"),
        ("J6", "提交键整段永不执行（改完形状、忘了真的按）"),
        ("J7", "丢掉 cause：上抛只剩一句话，排障时查不到是哪条 CDP 命令拒的"),
    ]


def apply_js(src: str, code: str) -> str:
    if code == "J1":
        return sub_once(src, SUBMIT_BLOCK,
                        "          if (submit && res.channel === 'cdp') {\n"
                        "            await cdpInput.pressEnter(tabId).catch(() => {});\n"
                        "          }", code)
    if code == "J2":
        return sub_once(src, "'submit_key_not_dispatched: '", "'element_not_found_by_key: '", code)
    if code == "J3":
        return sub_once(src, "if (submit && res.channel === 'cdp') {", "if (submit) {", code)
    if code == "J5":
        return sub_once(src, "if (submit && res.channel === 'cdp') {", "if (res.channel === 'cdp') {", code)
    if code == "J6":
        return sub_once(src, "if (submit && res.channel === 'cdp') {",
                        "if (false && submit && res.channel === 'cdp') {", code)
    if code == "J7":
        return sub_once(src, CAUSE_ARG,
                        "throw new Error('submit_key_not_dispatched: ' + String(e?.message || e));", code)
    if code == "J4":
        src = sub_once(src, "            res = { ok: true, editable: true, channel: 'cdp' };",
                       "            if (submit) { await cdpInput.pressEnter(tabId); }\n"
                       "            res = { ok: true, editable: true, channel: 'cdp' };", code)
        return sub_once(src, SUBMIT_BLOCK, "", code)
    raise SystemExit(f"未知变异体 {code}")


def go_mutants():
    return [
        ("G1", "isNeverExecuted 把提交键失败判成「从未发生」（台账留空＝放行双发）",
         f"user-server/{SVC}/write_ledger.go",
         'return strings.Contains(msg, "_inject_timeout_")',
         'return strings.Contains(msg, "submit_key_not_dispatched") || strings.Contains(msg, "_inject_timeout_")'),
        ("G2", "通用写步的「结局未知」不再记提交尝试（default 分支直接留空）",
         f"user-server/{SVC}/write_ledger.go",
         "\t\tstate = model.StepSubmitUnattributed // 结果未知 → 记成尝试，交人来判",
         "\t\treturn nil"),
        ("G3", "扩展侧抛出字面量改名（跨语言契约：Go 腿必须发现另一边断了）",
         str(PRIM_REL_CLONE),
         CAUSE_ARG,
         "throw new Error('submit_key_lost: ' + String(e?.message || e), { cause: e });"),
    ]


def dup_report(tag: str, kills: dict) -> None:
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

    tmp = Path(args.clone or tempfile.mkdtemp(prefix="b18mut-"))
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}")
    problems = []

    if not args.go_only:
        work = js_prepare(tmp)
        prim = work / PRIM_REL
        orig = read(prim)
        base_md5 = md5_bytes(prim)
        rc, killed, ran, skipped, out = js_run(work)
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
        basemd5 = {rel: md5_bytes(p) for rel, p in files.items()}
        gkill = {}
        rc, killed, ran, skipped, out = go_run(clone)
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
