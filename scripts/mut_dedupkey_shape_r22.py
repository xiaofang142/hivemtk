#!/usr/bin/env python3
"""二次审核协议首轮（批25 4-D）变异电池：扩展侧 `_dedupKey` 的**键形**契约。

为什么单独立一支（而不是"§8.3-17 有腿"就算完）：
- §8.3-17 的取舍那一格写了两条硬承诺（`channel-adapter.js:153-162` 上方注释）：
  「为什么 0 必须保持旧字节：`_sentKeys` 落 localStorage，键形一变就等于全部重报一遍」。
  在此之前该形状**全仓零断言**：实跑的三条已存在腿（`test/adapter-r23-occurrence-identity.test.js`
  那批）全部打在 `_canonicalMsgId` 产出的 `event_id` 上，`_dedupKey` 只喂
  `_hasSent/_markSent/_bumpOccurrence`。
- 否证方式就是本电池的两刀：把 `occurrence > 0` 注成 `>= 0`（首条键形变 `base#0`），
  补腿前整包 766 passed / rc=0 不红（`docs/.../R22-lanes/bridge-vitest-mutant-before-newlegs.txt`）
  ⇒ 注释里那句"全部重报一遍"是无人守的断言。补腿（`test/adapter-dedupkey-shape.test.js` 三格）
  之后同一刀红三条（`.../bridge-vitest-mutant-with-newlegs.txt`）。本文件把这趟一次性取证
  变成任何人可重跑的常驻电池。

口径（沿用 `scripts/mut_send_verify_b24.py`，vitest 面用不了 `--shared` 克隆 ⇒ 就地 `cp` 备份注码
+ 逐格还原比 md5）：控制组必须 rc==0、settled>0、skip==0、红名空、且期望红名都在控制组名单里；
每格判定要求**红集合恰好等于**该格预测名单（多一条＝连带面，少一条＝预测写宽）；
锚点命中恰好一次、注码与原文不得相同；末行逐文件 md5 与基线一致。

用法：python3 scripts/mut_dedupkey_shape_r22.py [--check] [--cells D1,D2]
"""
from __future__ import annotations

import argparse
import hashlib
import re
import shutil
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent  # 脚本住在 <repo>/scripts/，不写死仓名
BRIDGE = REPO / "user-web/bridge"
LOGDIR = REPO / "docs/superpowers/specs/ledger/logs/R22dedupkey"

CA = "src/core/channel-adapter.js"
FILES = [CA]
TESTS = ["test/adapter-dedupkey-shape.test.js"]

K1 = "① 首条稳定键必须是裸 base，第二条起才带 #<n>"
K2 = "② flush 到 localStorage 的首条记录仍是裸 base（注释里「落 localStorage」那半句）"
K3 = "③ 反向半边：老落盘的裸 base 记录必须被首扫命中，一条都不许多发"

# _dedupKey 的两坏形状：① 首条也带后缀（键形一变＝存量账整体失效）；
# ② 编号根本不进键（同会话第二句原话被自己的稳定键吞掉，回到 §8.3-17 立要治的那个症状）。
A_SHAPE = "    return occurrence > 0 ? `${base}#${occurrence}` : base;\n"
A_SHAPE_ZEROFIRST = "    return occurrence >= 0 ? `${base}#${occurrence}` : base;\n"
A_SHAPE_NOSUFFIX = "    return base;\n"

CELLS = [
    ("D1", "首条键形也带 #0（存量裸 base 记录整体不命中＝全部重报一遍）", CA,
     A_SHAPE, A_SHAPE_ZEROFIRST, [K1, K2, K3]),
    ("D2", "编号不进键（同内容第二条被稳定键吞掉，发生次数身份在扩展侧消失）", CA,
     A_SHAPE, A_SHAPE_NOSUFFIX, [K1, K2]),
]

ANSI = re.compile(r"\x1b\[[0-9;]*m")
RESULT = re.compile(r"^\s*([✓×↓-])\s+(.*\S)\s*$")
DURATION = re.compile(r"\s*[\d.]+m?s\s*$")


def md5(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def case_name(rest: str) -> str:
    """取 verbose 行的用例名：按 vitest 的分隔符 `" > "` 切，不能按裸 `>` 切。

    本文件第一条用例的标题以 `#<n>` 结尾，裸 `>` 一切就把名字截成空串 ⇒ 期望名单永远点不到它
    （控制组 preflight 会停机，是响的、不是假绿）。`mut_send_verify_b24.py` 那批标题里没有 `>`，
    所以这一形状在本仓是第一次出现。
    """
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
    base_md5 = {f: md5(BRIDGE / f) for f in FILES}

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
    print(f"\n===== 判定：" + (f"{done} 格逐刀被杀，无存活" if not problems
                            else f"{len(problems)} 格未杀/BROKEN：" + "; ".join(problems)))
    print(f"OK：源文件逐字节还原（" + " ".join(f"{Path(f).name}={base_md5[f][:8]}" for f in FILES)
          + f"），日志在 {LOGDIR}")
    if picked:
        print("   注：本次按 --cells 只跑了窄口子，**门禁不认这一行**，认的是不带 --cells 的全量。")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
