#!/usr/bin/env python3
"""批23（§6-4）SSE 排 `_pendingAck` 的四格变异电池：常驻副本。

**为什么 vitest 这一族要另写一份形状**：Go 侧那套电池只往私有 `--shared` 克隆里注码，
而 vitest 在克隆里跑不起来（`node_modules` 不进克隆、`--load-extension` 之类的安装态也不在），
所以这里按本轮定下的替代口径：**就地注码 + `cp` 备份 + 还原后逐文件比 md5**，
且注码面严格锁在 `src/core/downlink.js` 一个文件、收尾再核一次全树 md5。

口径沿用 Go 侧那套（一次读全四格，别拿子集当门）：
- 控制组必须 `rc==0`、`settled>0`、`skip==0`、红名集合为空；
- 控制组跑出来的**用例名名单**必须与 CELLS 期望逐字相等 ⇒ 用例被谁加/删/改名都当场停机，
  而不是"少跑一条还报全杀"。**这里刻意不写死 settled 的数字**：共享工作树下别的泳道随时
  往同一只 test 文件里加 it，写死就把别人的用例算成跑断（真判据是名字身份，不是数量）；
- 每格断言 `settled == 控制组 settled`、`skip == 0`、红集合**恰好等于**期望那一条（上下界都算）；
- 红了必须把红因打出来（电池只在未杀时打印，杀了的格也要能读出它是因为什么红的）；
- 锚点命中必须恰好一次；替换后 md5 与原文相同 ⇒ 判 BROKEN=无效变异（改名式/空操作式注码
  在这一族里同样要防：`S1` 摘掉整行调用而不只改文案，就是这个原因）。

首轮（2026-09-22，就地的一次性刀具）读数：`docs/.../ledger/logs/R23sseack/battery-final-run.txt`
与 `reverse-control-group-name-identity.log`，那份刀具与逐格日志原样留在 `R23sseack/first-run/`；
本文件是它的常驻副本（锚点与期望红名逐字照抄，改的只有三处：仓根由 `__file__` 推导、
控制组数量断言换成"名字身份"断言、补 `--check` / `--cells`）。
"""
import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent  # 脚本住在 <repo>/scripts/，不写死仓名
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

# (格, 说明, 该红的腿, 锚点原文, 注码)
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
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true",
                    help="只核锚点命中一次（不注码、不跑 vitest）")
    ap.add_argument("--cells", default="", help="逗号分隔的格名；留空=全跑（窄口子不是门）")
    args = ap.parse_args()

    want_names = [name for _, _, name, _, _ in CELLS]
    if len(set(want_names)) != len(CELLS):
        print("!! 期望红名有重复 —— 四格打的是四条不同的腿，重名就是有两格没拆开")
        return 2

    text0 = SRC.read_text(encoding="utf-8")
    broken = False
    for code, desc, _name, old, new in CELLS:
        n = text0.count(old)
        if n != 1:
            print(f"{code} BROKEN=锚点命中 {n} != 1（先修锚点，不许改期望）")
            broken = True
        elif old == new:
            print(f"{code} BROKEN=锚点与注码相同（无效变异）")
            broken = True
    if args.check:
        print("锚点前置：4 格各命中一次" if not broken else "锚点前置有 BROKEN，见上")
        return 1 if broken else 0
    if broken:
        return 2

    picked = [c.strip() for c in args.cells.split(",") if c.strip()]
    if picked:
        unknown = [c for c in picked if c not in {x[0] for x in CELLS}]
        if unknown:
            print(f"!! 不存在的格：{unknown}")
            return 2

    LOGDIR.mkdir(parents=True, exist_ok=True)
    src_bak = LOGDIR / "bak" / "downlink.js.orig"
    src_bak.parent.mkdir(exist_ok=True)
    shutil.copy2(SRC, src_bak)   # 每次运行现写，不复用上一轮的备份
    base_md5 = md5(SRC)

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
    # 用例名身份校验：期望名单必须与控制组实际跑出来的名字**逐字**相等。
    # 少了这一步，"红集合不符"既可能是变异没杀到，也可能是驱动/用例改名 —— 前者是洞、
    # 后者是假红，两者都得分开，否则修的是判据不是驱动。
    if sorted(set(c["ran"])) != sorted(want_names):
        print("!! 控制组跑出来的用例名与期望名单不一致 —— 停机")
        print(f"   期望={want_names}")
        print(f"   实际={c['ran']}")
        return 2

    for code, desc, name, old, new in CELLS:
        if picked and code not in picked:
            print(f"{code:<4} {desc:<44} 跳过（--cells 未选）")
            continue
        text = SRC.read_text(encoding="utf-8")
        if text.count(old) != 1:
            problems.append(f"{code} BROKEN=锚点命中数变了")
            print(f"{code:<4} {desc:<44} BROKEN=锚点")
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

    done = len(picked) if picked else len(CELLS)
    print("\n===== 判定：" + (f"{done} 格逐刀被杀，无存活" if not problems
                            else f"{len(problems)} 格未杀/BROKEN：" + "; ".join(problems)))
    if md5(SRC) != base_md5:
        print("!! 收尾 md5 校验失败：源文件没回到注码前")
        return 2
    print(f"OK：{SRC} 逐字节还原（md5 {base_md5}），日志在 {LOGDIR}")
    if picked:
        print("   注：本次按 --cells 只跑了窄口子，**门禁不认这一行**，认的是不带 --cells 的全量。")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
