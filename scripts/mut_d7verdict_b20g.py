#!/usr/bin/env python3
"""批20g 变异电池：D7 确认闸门**算出结论之后**那一步有没有人听。

为什么要有这条电池（而不是「超时用例全绿」就算完）：
- 闸门本身有腿（`TestWaitForConfirm*`、`TestAwaitConfirmGate*` 直调 `waitForConfirm` /
  `awaitConfirmGate` 校验返回值），消费点另有一道**静态锁**（钉 `confirmWaitTimedOut：`
  与 `case confirmStoppedByUser:` 两处字面量）。两样都不看 `dispatchStep` 拿到 `out`
  之后做什么——把 default 支路注成「Warnf 一句后继续往下走到 sendOnce」，
  静态锁照绿（注释里那串字面量原样保留）、超时腿照绿（它们穿不到 dispatchStep）、
  两条 WSE2E 照绿（只覆盖 granted 与 stop）。⇒「判据算得对、消费点没人管」在原有断言下不可见。
- 所以三格各钉一层，且**都不动那两处被静态锁钉住的字面量**：
  G1 消费点（结论吞掉⇒照发＝不可逆提交跨出）；
  G2 归因（是哪条预算到头说反⇒运维去调错的旋钮）；
  G3 判据本身（计时器到点头换成 granted⇒闸门只剩记账）。
  G3 与既有 `TestWaitForConfirmBudgetDecoupled` 会同族红，`dup_report` 会点名——
  同族不是坏，是「这一格有两个人在看」；反过来，只有新腿红的格才证明新腿不是摆设。

口径（沿用批16/17/18/19x/20a-f 电池）：
- 控制组必须 rc==0、total>0、skip==0、且不许有任何红名；
- 每格断言 PASS+FAIL==控制组数（一条用例 panic 会带走整个二进制，"FAIL=1"看着像杀其实没跑完）；
- BUILD FAILED / panic / 红而没点名 一律判 BROKEN，**不计入杀掉**（编译红不是牙）；
- 锚点命中必须恰好一次，注码先过 gofmt -e；还原后比 md5；
  只在私有 --shared 克隆里注码，绝不碰共享工作树。

用法：python3 scripts/mut_d7verdict_b20g.py [--keep] [--clone DIR]
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

EXEC_REL = Path("user-server/internal/browser_automation/service/executor.go")
# 本泳道脏 .go（含未跟踪的新用例文件）的枚举范围。手写清单必然漏 ⇒ 按目录取 git status。
LANE_PATHS = ["user-server/internal/browser_automation",
              "user-server/internal/pkg/db",
              "user-server/internal/router"]
GO_PKGS = ["./internal/browser_automation/service/"]
GO_RUN = "TestWSE2E_D7"
LEG = "TestWSE2E_D7ConfirmTimeoutVerdictConsumedByCaller"

# ---------------------------------------------------------------- 锚点（完整语法块，含缩进）
# G1：消费点。锚吃掉 default 支路那一句 return，注码保留注释原文（静态锁钉的就是它）。
A_CONSUME = ('\t\t\tdefault: // confirmWaitTimedOut：确认预算或执行预算先到头，why 里写明是哪条\n'
             '\t\t\t\treturn nil, fmt.Errorf("post_comment 等待人工确认超时（%s），评论未提交", why)\n')
A_CONSUME_SWALLOW = ('\t\t\tdefault: // confirmWaitTimedOut：确认预算或执行预算先到头，why 里写明是哪条\n'
                     '\t\t\t\t_ = why // 变异：闸门判超时也不拦，继续往下走到不可逆提交点\n')
# G2：归因方向（确认预算到头却说成执行预算）。
A_WHY = ('\t\treturn confirmWaitTimedOut, fmt.Sprintf("人工确认等待 %ds", '
         'int(gate.budget.Seconds()))\n')
A_WHY_FLAT = '\t\treturn confirmWaitTimedOut, "任务执行预算用尽"\n'
# G3：判据本身（计时器到点头折成放行）。
A_TIMER = ('\tcase <-timer.C:\n'
           '\t\treturn confirmWaitTimedOut, fmt.Sprintf("人工确认等待 %ds", '
           'int(gate.budget.Seconds()))\n')
A_TIMER_GRANT = ('\tcase <-timer.C:\n'
                 '\t\treturn confirmGranted, ""\n')

ANSI = re.compile(r"\x1b\[[0-9;]*m")
CONTROL = {"go": 0}


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def verdict(r: dict, expect_total: int) -> str:
    if r["rc"] == 0 and not r["killed"]:
        return "存活=洞"
    if r.get("panicked") or r.get("buildfailed") or not r["killed"]:
        return "BROKEN=判不了"
    if r["total"] != expect_total or r["failed"] < 1 or r["skipped"] > 0:
        return "BROKEN=判不了"
    return "杀掉"


def dup_report(kills: dict) -> None:
    seen: dict[tuple, list] = {}
    for code, names in kills.items():
        seen.setdefault(tuple(sorted(names)), []).append(code)
    for key, codes in seen.items():
        if len(codes) > 1 and key:
            print("[Go] 同族（同一批用例被多格杀掉，判据可能重叠）：" + "≈".join(codes)
                  + f" → {' '.join(sorted(key))[:120]}")


def lane_overlays() -> list[str]:
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到本泳道脏文件清单：" + r.stderr[-200:])
    out = [l[3:].split(" -> ")[-1].strip().strip('"') for l in r.stdout.splitlines()]
    out = [p for p in out if p.endswith(".go")]
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
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path) -> dict:
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    # 与 mut_write_claim_a12.py 共用一份缓存：两把电池注的是同一个包，换目录＝重编一次。
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


def cells() -> list[tuple[str, str, str, str]]:
    """(格, 说明, 原文, 注码)"""
    return [
        ("G1", "消费点：闸门判超时后吞掉结论，继续走到不可逆提交点", A_CONSUME, A_CONSUME_SWALLOW),
        ("G2", "归因：确认预算到头说成执行预算（运维去调错旋钮）", A_WHY, A_WHY_FLAT),
        ("G3", "判据本身：计时器到点头折成 granted", A_TIMER, A_TIMER_GRANT),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    tmp = Path(args.clone or tempfile.mkdtemp(prefix="b20gmut-"))
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}", flush=True)
    problems: list[str] = []

    clone = go_prepare(tmp)
    target = clone / EXEC_REL
    if not target.exists():
        raise SystemExit(f"注码目标文件不在克隆里：{target}")
    original = read(target)
    basemd5 = md5_bytes(target)

    prepared = []
    for code, desc, old, new in cells():
        if original.count(old) != 1:
            raise SystemExit(f"{code} 锚点命中 {original.count(old)} 次（要求恰好 1）：{old[:70]!r}")
        if old == new:
            raise SystemExit(f"{code} 注码无效（原文与注码后一致）")
        src = original.replace(old, new, 1)
        ok, err = syntax_ok_go(src)
        if not ok:
            raise SystemExit(f"{code} 注码语法坏，跑出来只会是 build failed：{err[:160]}")
        prepared.append((code, desc, src))
    print(f"[Go] 注码前置：{len(prepared)} 格锚点各命中一次 + 注码后语法可解析")

    r = go_run(clone)
    CONTROL["go"] = r["total"]
    print(f"[Go] 控制组 rc={r['rc']} total={r['total']} passed={r['passed']} "
          f"skip={r['skipped']} FAIL={r['killed']}")
    if r["rc"] != 0 or r["total"] == 0 or r["skipped"] > 0 or r["killed"]:
        print(r["out"][-4000:])
        raise SystemExit("[Go] 控制组不干净——后面所有红/绿都不可信")

    gkill: dict[str, set] = {}
    for code, desc, src in prepared:
        target.write_text(src, encoding="utf-8")
        if md5_bytes(target) == basemd5:
            raise SystemExit(f"{code} 注码未生效（与原内容一致）")
        r = go_run(clone)
        v = verdict(r, CONTROL["go"])
        if v == "杀掉" and LEG not in r["killed"]:
            v = f"红了但没点出 {LEG}"
        print(f"{code:<4} {desc[:56]:<58} {v:<7} total={r['total']} fail={r['failed']} "
              f"skip={r['skipped']} ｜ " + " | ".join(k[:60] for k in r["killed"][:3]))
        if not v.startswith("杀掉"):
            problems.append(f"[Go] {code} {v}：{desc}")
            print(r["out"][-3000:])
        gkill[code] = set(r["killed"])
        target.write_text(original, encoding="utf-8")
        if md5_bytes(target) != basemd5:
            raise SystemExit(f"{code} 还原后 md5 不一致，停机")

    dup_report(gkill)
    print("[Go] 已全量还原（md5 一致）")

    if not args.keep:
        shutil.rmtree(tmp, ignore_errors=True)
    if problems:
        print("\n===== 电池判定：有洞 =====")
        for x in problems:
            print("  ✗", x)
        return 1
    print(f"\n===== 电池判定：Go {len(prepared)} 格逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
