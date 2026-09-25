#!/usr/bin/env python3
"""二次审核协议首轮（批25 lane-4）变异电池：钉钉生产者的官方 msgId 那一格。

为什么单独立这一格（而不是"§8.3-10 有腿"就算完）：
- 内容窗口去重的守卫（`inbox_ingress_ingest.go:145`「带 channel_msg_id 的事件跳过内容窗口」）
  有腿，但那三条腿全是**消费侧**：`TestN14_DingTalkRepeatedTextIsNotDropped` 只数 message_hub
  行数，`TestIngress_PlatformIDEventsSkipContentDedup` 手工往 `evt.Extra` 里塞 id（不经任何
  生产者），M-01 那两条只断媒体键 ⇒ 钉钉这一侧「到底有没有把官方 msgId 带进 Extra」从未被钉：
  把 `dingtalk_app.go:177` 那行改成空串，全仓照样绿（补腿前实测）。
- 同类四个生产者（`webhook_channel_qq.go:114`、`channelbot/core/core.go:203`、
  `channelbot/qq/qq.go:664`、`controller/wechat.go:285`）各有直断 Extra 的腿，只有钉钉空着
  ⇒ 本电池钉的就是这最后一格，两刀分别对应「值错」与「键没写」。

口径（沿用其余 mut_*.py 电池）：控制组必须 rc==0、total>0、skip==0、无红名；
每格断言 PASS+FAIL==控制组数；BUILD FAILED / panic / 红而没点名 ⇒ BROKEN 不计入杀掉；
锚点命中恰好一次、注码先过 gofmt -e、还原后比 md5；只在私有 --shared 克隆里注码。

用法：python3 scripts/mut_dingtalk_msgid_r22lane.py [--keep] [--clone DIR]
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

DT_REL = Path("user-server/internal/service/dingtalk_app.go")
# 覆盖面＝整棵 `user-server/internal`，不按本批文件收窄：`internal/service` 编之前要过
# `internal/pkg/db/migrate.go`，而它引用的 `browser_automation/model/{audit_digest,write_claim}.go`
# 是未跟踪产码 ⇒ 覆盖面窄过依赖树时控制组直接 `undefined: BrowserAuditDigest`（同一坑的 2026-09-22
# 实录见 mut_hub_media_backfill.py:62）。少覆盖不是"更安全"，是把克隆变成一份谁也没写过的树。
LANE_PATHS = ["user-server/internal"]
GO_PKGS = ["./internal/service/"]
GO_RUN = "TestDingTalkInboundChannelMsgIDIsOfficialMsgID"
LEG = "TestDingTalkInboundChannelMsgIDIsOfficialMsgID"

A_ID_LINE = '\t\t\t"channel_msg_id":             msg.MsgID,\n'
A_ID_EMPTY = '\t\t\t"channel_msg_id":             "",\n'

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


def lane_overlays() -> list[str]:
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到本泳道脏文件清单：" + r.stderr[-200:])
    out = [p for p in (l[3:].split(" -> ")[-1].strip().strip('"') for l in r.stdout.splitlines())
           if p.endswith(".go")]
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
    # 不另设 GOCACHE：internal/service 整包的编译产物只有一份值得复用（默认缓存里已经有），
    # 换私有缓存目录 = 每跑一次这格就重编一次四千条用例的包。
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    p = subprocess.run(["go", "test", "-p", "1", "-count=1", "-v"] + GO_PKGS + ["-run", GO_RUN],
                       cwd=root, capture_output=True, text=True, timeout=2400, env=env)
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
    return [
        ("P1", "官方 msgId 换成空串（键在、值丢：守卫判「没带 id」⇒ 走内容窗口）",
         A_ID_LINE, A_ID_EMPTY),
        ("P2", "整列不写（Extra 里根本没有 channel_msg_id）", A_ID_LINE, ""),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    tmp = Path(args.clone or tempfile.mkdtemp(prefix="dtidmut-"))
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}", flush=True)
    problems: list[str] = []

    clone = go_prepare(tmp)
    target = clone / DT_REL
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
        target.write_text(original, encoding="utf-8")
        if md5_bytes(target) != basemd5:
            raise SystemExit(f"{code} 还原后 md5 不一致，停机")

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
