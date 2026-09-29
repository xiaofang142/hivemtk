#!/usr/bin/env python3
"""批24（§8.3-8）B 链路「出站后回 DOM 复核」的变异电池：常驻副本，16 格。

被审的东西：`BaseAdapter.sendOutbound` 尾部那段回查（`_countVisibleText` + `_verifySendLanded`）、
它带出的结论位 `sendVerified` / `extra.send_verified` 管线（types.js 的 callerExtra 透传）、
以及为容纳回查而改的外层超时预算（constants.js 的 `outboundStepTimeoutMs`、downlink.js 三处站点
与那条结果日志）。四条设计承诺各有一格对靶：

  判据   V1 计数增加而非文本命中 / V2 跨会话残留排除 / V7 轮询节拍 / V11 基线取自点击前
  结论位 V3 调用方不再声明 / V8 帧构造丢弃 callerExtra
  红线   V4 「没见着」参与 ack（这是本批唯一不可撤销的那一侧，必须最贵）
  三态   V5 早期出路折成 false / V10 结果日志被 `!!` 洗成二态
  锁     V6 外层预算丢掉回查切片 / V9 三处站点里有一处漂回裸 humanSendTimeoutMs
  归属   V12–V16 批24b：`getMessageItems()` 的 root.contains 判据，六个消费点一处一格
         （V2 是回查那一处，V13/V14/V12/V15/V16 是增量、回填、编号、getMessages、巡检）
         —— 每格绑一条腿：摘掉哪一处，红集合必须**恰好**是那条腿。

口径（与 scripts/mut_sse_ack_r23.py 同源，vitest 这一族只能在就地注码）：
- 控制组必须 rc==0、settled>0、skip==0、红名集合为空；
- 期望红名必须**逐字出现在控制组跑出来的名单里**（改名/删用例 ⇒ 停机，而不是"少跑一条还报全杀"）；
  settled 的**数字**放刀前现测、不写死：共享工作树下别的泳道随时往 batch3-hygiene.test.js 里加 it；
- 每格断言 settled==控制组、skip==0、红集合**恰好等于**期望那组（上下界都算）；
- 红必须带红因：每格整段输出落盘，杀掉与存活的格都打印首条红因行（负载红 ≠ 变异被杀）；
- 锚点命中必须恰好一次；替换后 md5 与原文相同 ⇒ 判 BROKEN=无效变异；
- 注码用 cp 备份 + 内存原文写回，逐文件比 md5 还原，绝不 git checkout/restore。

用法：
    python3 scripts/mut_send_verify_b24.py --check          # 只核锚点
    python3 scripts/mut_send_verify_b24.py --cells V2       # 单格反向（改完驱动先跑这一条）
    python3 scripts/mut_send_verify_b24.py                  # 全 16 格 = 门

**本电池不是门禁**：它只跑 TESTS 里那四个文件。提交门禁是干净克隆里的 go build/vet/test 全套
+ bridge 全量 vitest。
"""
import argparse
import hashlib
import re
import shutil
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent  # 脚本住在 <repo>/scripts/，不写死仓名
BRIDGE = REPO / "user-web/bridge"
LOGDIR = REPO / "docs/superpowers/specs/ledger/logs/B24sendverify"

CA = "src/core/channel-adapter.js"
CN = "src/core/constants.js"
DL = "src/core/downlink.js"
TY = "src/core/types.js"
FILES = [CA, CN, DL, TY]

TESTS = [
    "test/adapter-b24-send-verify.test.js",
    "test/adapter-b24b-conv-ownership.test.js",
    "test/downlink-b24-step-timeout.test.js",
    "test/batch3-hygiene.test.js",
]

ANSI = re.compile(r"\x1b\[[0-9;]*m")
RESULT = re.compile(r"^\s*([✓×↓-])\s+(.*\S)\s*$")
DURATION = re.compile(r"\s*[\d.]+m?s\s*$")

# —— 用例名（取 verbose 行最后一个 `>` 之后的部分）——
N1 = "气泡真的出现 → sendVerified=true，且 AGENT 回声帧带 extra.send_verified=true"
N2 = "反向半边：平台静默吞（气泡始终不出现）→ 照常 ack 语义不变，但结论位必须是 false"
N3 = "基线判据：发送前窗口里就有一条同文本（客户先说过）→ 只\"存在\"不算数"
N4 = "渲染滞后容得下：气泡在 150ms 后才挂上 → 回查轮询要等到它"
N5 = "跨会话残留节点不计入：点击之后才挂进 items 的上一会话同文本气泡，不算这次发出去了"
N6 = "红线②：回查是只读 —— 不写去重容器，之后正常扫描仍能报出这些气泡"
N7 = "三态：没走到回查的出路（目标会话打不开）不带结论位，既不是 true 也不是 false"
B1 = "发送耗时越过 sendText 预算、但落在回查切片内 → 仍算已发并 ack（不得重推）"
B2 = "预算仍是上界：永远不回包的发送要在「sendText 预算 + 回查切片」内被切掉并留 pending"
B3 = "静态锁：结果日志原样透传 sendVerified（三态不许被 `!!` 洗成二态）"
H1 = "静态契约：发送全链三处均走长度感知超时，无裸 await"
H2 = "静态契约：fillAndSend 在 rawSendText 前过闸、成功后盖章"
# 批24b：`getMessageItems()` 会话归属判据的六个消费点，各绑一条腿（摘哪处红哪条）。
O1 = "_backfill 不得把上一会话残留气泡并进当前会话的历史帧"
O2 = "_scanIncremental（fallbackTimer 每 3s 走这条）不得增量上行残留气泡"
O3 = "_occurrenceInList 只数本会话的节点：残留不得把真实那条挤成 #1"
O4 = "getMessages（PollingLoop 每秒走的公开入口）不得把残留节点当本会话新消息返回"
O5 = "_collectUnseenText（巡检单个会话的一抓）不得把残留节点收进批量上行"

# (格, 说明, 文件, 锚点原文, 注码, 期望红名)
CELLS = [
    ("V1", "判据从「计数增加」退化成「文本命中」", CA,
     "      if (this._countVisibleText(text) > baseline) return true;\n",
     "      if (this._countVisibleText(text) > 0) return true;\n",
     [N3]),
    ("V2", "回查不再排除跨会话残留节点", CA,
     "      if (root && !root.contains(item)) continue;\n",
     "",
     [N5]),
    ("V3", "回声帧不再声明结论位", CA,
     "        { send_verified: sendVerified }\n",
     "        {}\n",
     [N1, N2]),
    ("V4", "红线：回查未见参与 ack（会把不可撤销的双发交回渲染滞后）", CA,
     "      sendVerified = await this._verifySendLanded(text, textBaseline, verifyMs);\n",
     "      sendVerified = await this._verifySendLanded(text, textBaseline, verifyMs);\n"
     "      if (!sendVerified) return { ok: false, rateLimited: false, notFound: false };\n",
     [N2, N3, N5]),
    ("V5", "三态：没走到回查的出路折成 false", CA,
     "        return { ok: false, rateLimited: false, notFound: true };\n      }\n      conv = opened;\n",
     "        return { ok: false, sendVerified: false, rateLimited: false, notFound: true };\n      }\n      conv = opened;\n",
     [N7]),
    ("V7", "回查不轮询（第一次没见着就判未见）", CA,
     "      await sleep(Math.min(BRIDGE_THREE_CHANNEL.sendVerifyPollMs, left));\n",
     "      return false;\n",
     [N4]),
    ("V11", "基线改在点击之后取", CA,
     "    const textBaseline = this._countVisibleText(text);\n    let ok;\n"
     "    try {\n"
     "      // B4（批3）：rawSendText 裸 await——sendText 卡死（DOM 阻塞/框架吞事件）会永久挂起\n"
     "      // 整条按会话串行的下行队列。统一 withTimeout，预算随文案长度伸缩（B7 拟人键入时长）。\n"
     "      await withTimeout(this.rawSendText(text), humanSendTimeoutMs(text), `rawSendText(${this.channel})`);\n"
     "      ok = true;\n",
     "    let textBaseline = 0;\n    let ok;\n"
     "    try {\n"
     "      // B4（批3）：rawSendText 裸 await——sendText 卡死（DOM 阻塞/框架吞事件）会永久挂起\n"
     "      // 整条按会话串行的下行队列。统一 withTimeout，预算随文案长度伸缩（B7 拟人键入时长）。\n"
     "      await withTimeout(this.rawSendText(text), humanSendTimeoutMs(text), `rawSendText(${this.channel})`);\n"
     "      textBaseline = this._countVisibleText(text);\n"
     "      ok = true;\n",
     [N1, N6, H2]),
    ("V6", "外层预算丢掉回查切片", CN,
     "  return humanSendTimeoutMs(text, baseMs) + BRIDGE_THREE_CHANNEL.sendVerifyMs;\n",
     "  return humanSendTimeoutMs(text, baseMs);\n",
     [B1, B2]),
    ("V8", "帧构造不再透传 callerExtra", TY,
     "  const extra = callerExtra && typeof callerExtra === 'object' ? { ...callerExtra } : {};\n",
     "  const extra = {};\n",
     [N1, N2]),
    ("V9", "重试站点漂回不含回查切片的裸预算", DL,
     "              outboundStepTimeoutMs(sanitized, sendTimeoutMs),\n              `sendOutbound-retry(",
     "              humanSendTimeoutMs(sanitized, sendTimeoutMs),\n              `sendOutbound-retry(",
     [H1]),
    ("V10", "结果日志把三态洗成二态", DL,
     "        sendVerified: result && result.sendVerified,\n",
     "        sendVerified: !!(result && result.sendVerified),\n",
     [B3]),
    # —— 批24b：会话归属判据的六个消费点，一处一格，摘掉必须只红它绑的那条腿 ——
    ("V12", "_occurrenceInList 不再判归属（残留挤掉真实那条的 #0 编号）", CA,
     "      if (root && node && !root.contains(node)) continue;\n", "",
     [O3]),
    ("V13", "_handleIncremental 不再判归属（增量把残留上行）", CA,
     "    if (root && item && !root.contains(item)) return;\n", "",
     [O2]),
    ("V14", "_backfill 不再判归属（残留并进当前会话历史帧）", CA,
     "      if (activeRoot && item && !activeRoot.contains(item)) { this.seenNodes.add(item); continue; }\n", "",
     [O1]),
    ("V15", "getMessages 不再判归属", CA,
     "      if (root && item && !root.contains(item)) continue;\n", "",
     [O4]),
    ("V16", "_collectUnseenText 不再判归属", CA,
     "      if (root && item && !root.contains(item)) { this.seenNodes.add(item); continue; }\n", "",
     [O5]),
]


def md5(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def case_name(rest: str) -> str:
    return DURATION.sub("", rest.rsplit(">", 1)[-1]).strip()


def tally(out: str):
    settled = skip = 0
    red, ran = [], []
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
    return settled, skip, sorted(red), ran


def run_vitest(tag: str) -> dict:
    r = subprocess.run(["npx", "vitest", "run", *TESTS, "--reporter=verbose"],
                       cwd=str(BRIDGE), capture_output=True, text=True, timeout=1200)
    out = r.stdout + r.stderr
    (LOGDIR / f"{tag}.log").write_text(out, encoding="utf-8")
    settled, skip, red, ran = tally(out)
    causes = [l.strip()[:200] for l in ANSI.sub("", out).splitlines()
              if re.search(r"AssertionError|Error:|expected|Cannot find|is not defined", l)][:8]
    return {"rc": r.returncode, "settled": settled, "skip": skip, "red": red,
            "ran": ran, "causes": causes}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true", help="只核锚点命中一次（不注码、不跑 vitest）")
    ap.add_argument("--cells", default="", help="逗号分隔的格名；留空=全跑（窄口子不是门）")
    args = ap.parse_args()

    want = sorted({n for _, _, _, _, _, names in CELLS for n in names})
    for _c, _d, _f, _o, _n, names in CELLS:  # 一格内部不许有重名（并成一条就少一条判据）
        if len(set(names)) != len(names):
            print(f"!! 期望红名在一格里重复：{names}")
            return 2
    texts0 = {f: (BRIDGE / f).read_text(encoding="utf-8") for f in FILES}
    broken = False
    for code, _desc, f, old, new, _names in CELLS:
        n = texts0[f].count(old)
        if n != 1:
            print(f"{code} BROKEN=锚点在 {f} 命中 {n} != 1（先修锚点，不许改期望）")
            broken = True
        elif old == new:
            print(f"{code} BROKEN=锚点与注码相同（无效变异）")
            broken = True
    if args.check:
        print(f"锚点前置：{len(CELLS)} 格各命中一次" if not broken else "锚点前置有 BROKEN，见上")
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
    (LOGDIR / "bak").mkdir(exist_ok=True)
    base_md5 = {}
    for f in FILES:  # 每次运行现写备份，不复用上一轮的
        p = BRIDGE / f
        shutil.copy2(p, LOGDIR / "bak" / (Path(f).name + ".orig"))
        base_md5[f] = md5(p)

    print("== 控制组（未注码）")
    c = run_vitest("CONTROL")
    print(f"   rc={c['rc']} settled={c['settled']} skip={c['skip']} 红={c['red'] or '—'}")
    if c["rc"] != 0 or c["settled"] == 0 or c["skip"] or c["red"]:
        for l in c["causes"]:
            print("   红因: " + l)
        print("!! 控制组不干净 ⇒ 读数不可信，停机")
        return 2
    dup = [n for n in set(c["ran"]) if c["ran"].count(n) > 1]
    if dup:
        print(f"!! 控制组里有同名用例（按尾巴取名会并成一条）：{dup}")
        return 2
    missing = [n for n in want if n not in c["ran"]]
    if missing:
        print("!! 期望红名不在控制组名单里（用例被改名/删掉/漏跑）—— 停机")
        for m in missing:
            print("   缺: " + m)
        return 2
    expect_total = c["settled"]

    problems = []
    for code, desc, f, old, new, names in CELLS:
        if picked and code not in picked:
            print(f"{code:<4} {desc:<52} 跳过（--cells 未选）")
            continue
        p = BRIDGE / f
        text = p.read_text(encoding="utf-8")
        if text.count(old) != 1:
            problems.append(f"{code} BROKEN=锚点命中数变了")
            print(f"{code:<4} {desc:<52} BROKEN=锚点")
            continue
        p.write_text(text.replace(old, new), encoding="utf-8")
        if md5(p) == base_md5[f]:
            p.write_text(text, encoding="utf-8")
            problems.append(f"{code} BROKEN=替换后与原文相同（无效变异）")
            print(f"{code:<4} {desc:<52} BROKEN=无效变异")
            continue
        try:
            r = run_vitest(code)
        finally:
            p.write_text(text, encoding="utf-8")
            if md5(p) != base_md5[f]:
                print(f"!! {code} 还原后 md5 不一致，停机")
                return 2
        if r["rc"] == 0 and not r["red"]:
            v = "存活=洞"
        elif r["settled"] != expect_total or r["skip"]:
            v = "BROKEN=没跑完"
        elif not r["red"]:
            v = "BROKEN=判不了"
        elif r["red"] != sorted(names):
            v = "BROKEN=红集合不符"
        else:
            v = "杀掉"
        print(f"{code:<4} {desc:<52} {v:<12} settled={r['settled']} 红={','.join(r['red']) or '—'}")
        if r["red"]:
            print(f"     红因: {r['causes'][0] if r['causes'] else '（未匹配到红因行，见日志）'}")
        if v != "杀掉":
            problems.append(f"{code} {v}：{desc}")
            for l in r["causes"][1:]:
                print("     红因: " + l)
            if v == "BROKEN=红集合不符":
                print(f"     期望={sorted(names)} 实际={r['red']}")

    done = len(picked) if picked else len(CELLS)
    for f in FILES:
        if md5(BRIDGE / f) != base_md5[f]:
            print(f"!! 收尾 md5 校验失败：{f} 没回到注码前")
            return 2
    print("\n===== 判定：" + (f"{done} 格逐刀被杀，无存活" if not problems
                            else f"{len(problems)} 格未杀/BROKEN：" + "; ".join(problems)))
    print("OK：四个源文件逐字节还原（" + " ".join(f"{Path(f).name}={base_md5[f][:8]}" for f in FILES) + f"），日志在 {LOGDIR}")
    if picked:
        print("   注：本次按 --cells 只跑了窄口子，**门禁不认这一行**，认的是不带 --cells 的全量。")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
