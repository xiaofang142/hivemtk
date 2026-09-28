#!/usr/bin/env python3
"""R22 第二十三轮 §6-4（SSE 排 _pendingAck）的四刀变异电池。

口径沿用 Go 侧那套：私有副本注码（这里注的是 user-web/bridge/src/core/downlink.js，
用 cp 备份 + md5 校验还原）、控制组必须干净、每格 settled 与控制组相等、
红集合与期望严格相等（上下界都算）、红了必须把红因打出来。
"""
import hashlib
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

REPO = Path("/Users/xiaofang/Documents/www/go/hivemtk/hivemtk")
BRIDGE = REPO / "user-web/bridge"
SRC = BRIDGE / "src/core/downlink.js"
TEST = "test/downlink-r23-sse-ack-drain.test.js"
LOGDIR = REPO / "docs/superpowers/specs/ledger/logs/R23sseack"

ANSI = re.compile(r"\x1b\[[0-9;]*m")
# 只抓「标记 + 整段剩余」，用例名一律取最后一个 `>` 之后的部分：verbose 行的形状是
# `✓ <文件>.test.js > <describe> > <用例名> 205ms`，层级数不固定（describe 可以套嵌），
# 在正则里写死一段 `describe >` 会把上一级名字留在 captured 里 ⇒ 红集合比对必然假红。
RESULT = re.compile(r"^\s*([✓×↓-])\s+(.*\S)\s*$")
DURATION = re.compile(r"\s*[\d.]+m?s\s*$")


def case_name(rest: str) -> str:
    return DURATION.sub("", rest.rsplit(">", 1)[-1]).strip()

T1 = "SSE 启动后，到期条目在无人再推消息的情况下被重发 ack"
T2 = "stop() 之后排水器不再触发（不许留悬空 interval）"
T3 = "排水实现只有一份：轮询与 SSE 共用 drainPendingAcks，且 ack 请求带会话归属"
T4 = "pollDownlink 仍走同一个排水函数（重构不许把轮询那条腿弄丢）"

CELLS = [
    ("S1", "SSE 的排水定时器空转（不排队列）", T1,
     "    if (stopped) return;\n    drainPendingAcks(channel, ackCred).catch((err) => log.error('SSE pendingAck 排水失败', err));\n",
     "    if (stopped) return;\n"),
    ("S2", "stop 不回收排水定时器", T2,
     "    clearInterval(ackDrainTimer);\n", ""),
    ("S3", "轮询侧不再走共享排水函数", T4,
     "  await drainPendingAcks(channel, { serverUrl, accountId, token });\n",
     "  claimDuePendingAck(channel);\n"),
    ("S4", "重试 ack 丢掉会话归属", T3,
     "          conversationId,\n",
     "          conversationId: undefined,\n"),
]


def md5(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def tally(out: str):
    """返回 (settled, skipped, [红名], [全部跑过的用例名]) —— 只认逐用例的 verbose 行。"""
    settled = skip = 0
    red = []
    ran = []
    for raw in ANSI.sub("", out).splitlines():
        m = RESULT.match(raw)
        if not m:
            continue
        mark, name = m.group(1), case_name(m.group(2))
        if mark in ("✓", "×"):
            settled += 1
            ran.append(name)
        if mark == "↓":
            skip += 1
        if mark == "×":
            red.append(name)
    return settled, skip, red, ran


def run_vitest(tag: str) -> dict:
    env = dict(os.environ)
    r = subprocess.run(["npx", "vitest", "run", TEST, "--reporter=verbose"],
                       cwd=str(BRIDGE), capture_output=True, text=True, timeout=900, env=env)
    out = r.stdout + r.stderr
    (LOGDIR / f"{tag}.log").write_text(out, encoding="utf-8")
    settled, skip, red, ran = tally(out)
    causes = [l.strip()[:180] for l in out.splitlines()
              if re.search(r"AssertionError|Error:|expected", ANSI.sub("", l))][:6]
    return {"rc": r.returncode, "settled": settled, "skip": skip, "red": red,
            "ran": ran, "causes": causes, "out": out}


def main() -> int:
    LOGDIR.mkdir(parents=True, exist_ok=True)
    bak = LOGDIR / "bak"
    bak.mkdir(exist_ok=True)
    src_bak = bak / "downlink.js.orig"
    shutil.copy2(SRC, src_bak)
    base_md5 = md5(src_bak)

    problems = []
    print("== 控制组（未注码）")
    c = run_vitest("CONTROL")
    print(f"   rc={c['rc']} settled={c['settled']} skip={c['skip']} 红={c['red'] or '—'}")
    if c["rc"] != 0 or c["settled"] == 0 or c["skip"] or c["red"]:
        for l in c["causes"]:
            print("   红因: " + l)
        print("!! 控制组不干净，后面所有读数不可信 —— 停机")
        return 2
    expect_total = c["settled"]
    if expect_total != 4:
        print(f"!! 控制组 settled={expect_total}，与预期的 4 条不符（用例被谁加/删了？）—— 停机")
        return 2
    # 用例名身份校验：期望名单必须与控制组实际跑出来的名字**逐字**相等。
    # 少了这一步，"红集合不符" 既可能是变异没杀到，也可能是驱动/用例改名 —— 前者是洞、
    # 后者是假红，两者都得分开，否则修的是判据不是驱动。
    want_names = [name for _, _, name, _, _ in CELLS]
    if sorted(set(c["ran"])) != sorted(want_names):
        print("!! 控制组跑出来的用例名与期望名单不一致 —— 停机")
        print(f"   期望={want_names}")
        print(f"   实际={c['ran']}")
        return 2

    for code, desc, name, old, new in CELLS:
        text = SRC.read_text(encoding="utf-8")
        n = text.count(old)
        if n != 1:
            problems.append(f"{code} BROKEN=锚点命中 {n} != 1（先修锚点，不许改期望）")
            print(f"{code:<4} {desc:<44} BROKEN=锚点 {n}")
            continue
        SRC.write_text(text.replace(old, new), encoding="utf-8")
        if md5(SRC) == base_md5:
            SRC.write_text(text, encoding="utf-8")
            problems.append(f"{code} BROKEN=替换后与原文相同（无效变异）")
            print(f"{code:<4} {desc:<44} BROKEN=无效变异")
            continue
        try:
            r = run_vitest(code)
        finally:
            SRC.write_text(text, encoding="utf-8")
            if md5(SRC) != base_md5:
                print(f"!! {code} 还原后 md5 不一致，停机")
                return 2
        if r["rc"] == 0 and not r["red"]:
            v = "存活=洞"
        elif not r["red"]:
            v = "BROKEN=判不了"
        elif r["settled"] != expect_total or r["skip"]:
            v = "BROKEN=没跑完"
        elif r["red"] != [name]:
            v = "BROKEN=红集合不符"
        else:
            v = "杀掉"
        print(f"{code:<4} {desc:<44} {v:<12} settled={r['settled']} 红={','.join(r['red']) or '—'}")
        if v != "杀掉":
            problems.append(f"{code} {v}：{desc}")
            for l in r["causes"]:
                print("     红因: " + l)
            if v == "BROKEN=红集合不符":
                print(f"     期望={[name]} 实际={r['red']}")

    print("\n===== 判定：" + ("四格逐刀被杀，无存活" if not problems
                            else f"{len(problems)} 格未杀/BROKEN：" + "; ".join(problems)))
    if md5(SRC) != base_md5:
        print("!! 收尾 md5 校验失败：源文件没回到注码前")
        return 2
    print(f"OK：{SRC} 逐字节还原（md5 {base_md5}），日志在 {LOGDIR}")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
