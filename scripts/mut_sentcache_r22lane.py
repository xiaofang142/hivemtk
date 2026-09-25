#!/usr/bin/env python3
"""C 相续刀 · lane 5：SentCache 先删后设（O3）+ 结论位不门控 markSent（O4）两刀成格并跑。

来源（二手候选，均未实际注码运行过）：
  docs/superpowers/specs/ledger/logs/b-phase/lane-5-outbound-ack-findings.md（承诺 4 / 承诺 8）
动手前的盘面复核（brief 要求「我判错了要以盘面为准并指出」）：
- brief 表里写 O3 该红 downlink-b20d-sentcache-ttl 的「B/C 格」——**盘面已变**：02:58 之后
  该文件补了 H 格（`it('H 会话内重命中续期：evict 按最后命中时间裁…（杀：add 去掉先删）')`），
  其夹具三要件（未超界 + 重命中条目插在前 + 界外尾条在后）正是为这一刀造的。
  B/C 按 findings 自己的推演不红（B 的条目 load 即被清、C 的序被 load 的 sort 定死）。
  本电池按盘面点名 H。
- brief 表里写 O4 该红 adapter-b24-send-verify 的「反向半边」格——盘面同因已补专格：
  「红线①下半句：回查未见也必须记 markSent 账…（杀：markSent 被 if (sendVerified) 门控）」，
  spy markSent 计数 + 第二发同文本必须被 dedup 短路。本电池按盘面点名该格。

口径（照 mut_dedupkey_shape_r22.py 的形状：vitest 读的是工作树 ⇒ 就地单处精确注码）：
- 每刀先 `cp` 备份到私有临时目录，注码只改这一处；跑完 `cp` 还原并核 md5 与备份一致；
  禁止 git stash/checkout/restore（channel-adapter.js / downlink.js 另有泳道未提交改动）。
- 控制组放刀前现测：rc==0、settled>0、skip==0、红名空、期望腿名必须在控制组名单里；
  每格断言 settled==控制组数、红集合恰好等于预测名单（多一条＝连带面，少一条＝预测写宽）。
- 注码前核锚点命中恰好一次；注码与原文不得相同；收尾再逐文件 md5 与基线一致。

用法：python3 scripts/mut_sentcache_r22lane.py [--check] [--cells O3,O4]
"""
from __future__ import annotations

import argparse
import hashlib
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent  # 脚本住在 <repo>/scripts/，不写死仓名
BRIDGE = REPO / "user-web/bridge"
LOGDIR = REPO / "docs/superpowers/specs/ledger/logs/R22-lanes"

DL = "src/core/downlink.js"
CA = "src/core/channel-adapter.js"
FILES = [DL, CA]
TESTS = ["test/downlink-b20d-sentcache-ttl.test.js", "test/adapter-b24-send-verify.test.js"]

LEG_H = "H 会话内重命中续期：evict 按最后命中时间裁，落在重命中条目之后的界外尾条必须回收（杀：add 去掉先删）"
LEG_MARK = "红线①下半句：回查未见也必须记 markSent 账 —— 第二发同文本仍被内容去重层短路（杀：markSent 被 if (sendVerified) 门控）"

# O3：add() 的「先删后设」退化为「就地刷时间戳」——命中已有键不挪插入位，
# evict 的「头部即最旧」前缀不变式一遇重命中就 break，界外尾条永不清（A4 否过的插入序复活）。
ADD_ANCHOR = ("    // 先删后设：Map 保留插入序，命中已有键时也要把这条挪到「最新」端，\n"
              "    // 这样 evict 才能用「头部即最旧」的线性扫描代替排序。\n"
              "    this.mem.delete(id);\n"
              "    this.mem.set(id, at);\n")
ADD_MUTANT = ("    // 先删后设：Map 保留插入序，命中已有键时也要把这条挪到「最新」端，\n"
              "    // 这样 evict 才能用「头部即最旧」的线性扫描代替排序。\n"
              "    this.mem.set(id, at);\n")

# O4：回查未见 ⇒ 不记内容去重账——结论位漏进限流层，成了 §8.3 行 8 拒绝的第二道发送闸。
MARK_ANCHOR = ("      sendVerified = await this._verifySendLanded(text, textBaseline, verifyMs);\n"
               "      this.rateLimiter.markSent(this.channel, account, conv, text);\n")
MARK_MUTANT = ("      sendVerified = await this._verifySendLanded(text, textBaseline, verifyMs);\n"
               "      if (sendVerified) this.rateLimiter.markSent(this.channel, account, conv, text);\n")

CELLS = [
    ("O3", "add() 去掉先删：重命中就地刷时间戳不挪位 ⇒ evict 前缀快路径停在原地（杀面=H 格）",
     DL, ADD_ANCHOR, ADD_MUTANT, [LEG_H]),
    ("O4", "markSent 包进 if (sendVerified)：回查未见不记去重账 ⇒ 第二句同文案直过（杀面=红线①下半句格）",
     CA, MARK_ANCHOR, MARK_MUTANT, [LEG_MARK]),
]

ANSI = re.compile(r"\x1b\[[0-9;]*m")
RESULT = re.compile(r"^\s*([✓×↓-])\s+(.*\S)\s*$")
DURATION = re.compile(r"\s*[\d.]+m?s\s*$")


def md5(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def case_name(rest: str) -> str:
    """按 vitest verbose 的分隔符 " > " 切尾巴；标题里的 > 不参与（dedupkey 那轮的教训）。"""
    return DURATION.sub("", rest.rsplit(" > ", 1)[-1]).strip()


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
    (LOGDIR / f"sentcache-{tag}.log").write_text(out, encoding="utf-8")
    settled, skip, red, ran = tally(out)
    causes = [l.strip()[:200] for l in ANSI.sub("", out).splitlines()
              if re.search(r"AssertionError|Error:|expected|Cannot find|is not defined", l)][:8]
    m = re.search(r"Tests\s+(.+)$", ANSI.sub("", out), re.M)
    summary = m.group(1).strip() if m else "（未读到 Tests 行）"
    return {"rc": r.returncode, "settled": settled, "skip": skip, "red": red,
            "ran": ran, "causes": causes, "summary": summary}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true", help="只核锚点命中一次（不注码、不跑 vitest）")
    ap.add_argument("--cells", default="", help="逗号分隔的格名；留空=全跑（窄口子不是门）")
    args = ap.parse_args()

    want = sorted({n for _, _, _, _, _, names in CELLS for n in names})
    for _c, _d, _f, _o, _n, names in CELLS:  # 一格内部不许有重名
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
    backup_dir = Path(tempfile.mkdtemp(prefix="r22lane-sentcache-"))
    backups = {}
    for f in FILES:  # cp 备份：还原走 cp，双保险（写回文本 + 备份文件都在）
        bp = backup_dir / Path(f).name
        shutil.copy2(BRIDGE / f, bp)
        backups[f] = bp
    base_md5 = {f: md5(BRIDGE / f) for f in FILES}

    print("== 控制组（未注码）")
    c = run_vitest("CONTROL")
    print(f"   rc={c['rc']} settled={c['settled']} skip={c['skip']} 红={c['red'] or '—'} ｜ Tests {c['summary']}")
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
            shutil.copy2(backups[f], p)
            problems.append(f"{code} BROKEN=替换后与原文相同（无效变异）")
            print(f"{code:<4} {desc:<52} BROKEN=无效变异")
            continue
        try:
            r = run_vitest(code)
        finally:
            shutil.copy2(backups[f], p)  # cp 还原
            if md5(p) != base_md5[f] or md5(p) != md5(backups[f]):
                print(f"!! {code} 还原后 md5 与备份不一致，停机")
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
        print(f"{code:<4} {desc:<52} {v:<12} settled={r['settled']} ｜ Tests {r['summary']} "
              f"｜ 红={','.join(r['red']) or '—'}")
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
        if md5(BRIDGE / f) != base_md5[f] or md5(BRIDGE / f) != md5(backups[f]):
            print(f"!! 收尾 md5 校验失败：{f} 没回到注码前")
            return 2
    shutil.rmtree(backup_dir, ignore_errors=True)
    print(f"\n===== 判定：" + (f"{done} 格逐刀被杀，无存活" if not problems
                            else f"{len(problems)} 格未杀/BROKEN：" + "; ".join(problems)))
    print(f"OK：工作树两枚文件逐字节还原（" + " ".join(f"{Path(f).name}={base_md5[f][:8]}" for f in FILES)
          + f"），日志在 {LOGDIR}")
    if picked:
        print("   注：本次按 --cells 只跑了窄口子，**门禁不认这一行**，认的是不带 --cells 的全量。")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
