#!/usr/bin/env python3
"""门禁自己的判据有牙没有牙：逐刀注坏四处门禁脚本的判据，看点名的反向格是否各红各的那一格。

    python3 scripts/reverse-test-gate-teeth.py            # 刀数＝--list 的行数；首尾条件行与 rc 由脚本自印
    python3 scripts/reverse-test-gate-teeth.py --list     # 只印刀谱（不碰盘）

为什么要有这支脚本（本仓口径：新校验不反向测＝没有校验）：
`merge-gate.py --selftest` / `check-review-closeout.py --selftest` 印的是「N/N 判对」，
可那个"N/N"本身只是个读数——**判据被摘掉时它照样可能全绿**（本轮实测抓到三种这种形状，见刀谱）。
这一支测的不是被测对象，而是"测被测对象的那些格"。

为什么不挂进 STEPS：这几刀是把 `scripts/*.py` 就地注坏再还原，窗口里同树的另一条泳道（或本趟
门禁的另一步）恰好去读那支脚本就会读到坏码。它跑一次约 1 分钟，改到这四处判据时手动跑、
并把日志留在仓内，比让它自己当门更安全。

每刀的纪律：锚点必须**唯一**（不唯一＝注码没落地，这一刀不算测过，直接判红而不是跳过）；
每刀跑完立刻从本脚本自己开头存的备份还原并核 md5；收尾再核一次三处文件的 md5 与开头一致。
"""
from __future__ import annotations

import argparse
import datetime
import hashlib
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# (被注的文件, 锚点=生产判据那一行, 注码=把它摘掉, 期望开火的反向格标签关键字)
KNIVES: list[tuple[str, str, str, str]] = [
    # ---- merge-gate.py：汇总落点 / --only 过滤 / 存在性支路 / 可解析支路
    ("scripts/merge-gate.py", '    failed.append(name)\n    return "红"',
     '    diag_red.append(name)\n    return "红"', "红落点"),
    ("scripts/merge-gate.py", "    missing = set(only) - {s[0] for s in steps}",
     "    missing = set()", "--only 打错步名"),
    ("scripts/merge-gate.py",
     '            if "/" in a and (a.startswith("scripts/") or a.startswith("docs/")) \\\n',
     '            if "/" in a and a.startswith("scripts/") \\\n', "点名的仓内文件缺失"),
    ("scripts/merge-gate.py", '            if a.endswith(".py"):', "            if False:",
     "坏门禁脚本"),
    # 收口轮新增的两支牙：取证文件名要带"这趟是什么形状的跑"，否则子集跑能把台账引用的全量证据
    # 原地抹掉（本轮实测撞到的形状）。
    ("scripts/merge-gate.py", '    if not only:\n        return "merge-gate-full.log"',
     '    if False:\n        return "merge-gate-full.log"', "子集跑不许写全量取证名"),
    # ---- anchor-preflight：那份"两份名单之和==目录现算数"的自证计数器
    ("scripts/anchor-preflight.py", "    listed = set(COVERED) | set(UNCOVERED)",
     "    listed = set(disk)", "新电池没分类|已经不存在"),
    # ---- run-gate-in-clone.py：两侧一致性的定义 / 路径抹平 / 覆盖名单三分类
    ("scripts/run-gate-in-clone.py", "    return bool(sum_cl) and sum_cl == sum_wt, sum_cl, sum_wt",
     "    return True, sum_cl, sum_wt", "克隆少抽几条|两侧都零输出|工作树有汇总"),
    ("scripts/run-gate-in-clone.py", '    return [l.replace(str(base), "@BASE@") for l in out.splitlines()',
     "    return [l for l in out.splitlines()", "路径痕迹抹平"),
    ("scripts/run-gate-in-clone.py", '        if state == "??" and is_foreign_untracked(p):',
     "        if False:", "覆盖/删除/外域三类"),
    # 收口轮补的三支牙，都钉"取证自己"这一类：判决行必须进脚本自己写的那份日志（否则台账钉的是
    # 一个永远不含该句的文件），汇编件必须能把"成员零输出"和"成员退非 0"分开判红。
    ("scripts/run-gate-in-clone.py", '    out = "\\n".join([*lines, "", verdict]) + "\\n"',
     '    out = "\\n".join(lines) + "\\n"', "判决行要进|判决行同样入件"),
    ("scripts/run-gate-family-selftests.py", "        if not out.strip():",
     "        if False:", "成员零输出要红"),
    ("scripts/run-gate-family-selftests.py", "        elif rc:",
     "        elif False:", "成员退非 0 要红"),
    # ---- check-review-closeout.py：source-map 在场 / prose 豁免要写理由 / ledger 必须有钉 / 指针上下界
    ("scripts/check-review-closeout.py", "    if not isinstance(scope, list) or not scope:",
     "    if False:", "无 scope"),
    ("scripts/check-review-closeout.py", "            if len(reason) < MIN_PROSE:",
     "            if False:", "prose 豁免无 reason"),
    ("scripts/check-review-closeout.py", "            if sec and sec not in verified:",
     "            if False:", "声明 ledger 却没有计数钉"),
    ("scripts/check-review-closeout.py", "    if len(key) < MIN_QUOTE:",
     "    if False:", "quote 只有 4 字"),
    ("scripts/check-review-closeout.py", "    if len(key) > MAX_QUOTE:",
     "    if False:", "quote 抄整段正文"),
    # 收口轮新增的一族：logs[] 的字典项要打开证据文件核读数（只核存在性会被"名字对着、
    # 内容换了一趟跑"的文件骗过去）。三刀分别摘掉"该说的没说""不该说的说了""只写 path 不给断言"。
    ("scripts/check-review-closeout.py", "            for pat in says:", "            for pat in []:",
     "日志断言没命中"),
    ("scripts/check-review-closeout.py", "            for pat in nots:", "            for pat in []:",
     "日志出现不许出现的串"),
    ("scripts/check-review-closeout.py", "    if not says and not nots:", "    if False:",
     "logs 字典无断言"),
    # 收口轮新增的两支牙，钉的是"读得到才算判过"这两条界。它们的失效方向都是**静默放行**，
    # 所以必须各有一格反向测盯着：
    # ① 判读窗口退化成"只读文件头" ⇒ 大文件的尾巴（`gate_rc=0`、末段"红在："）进不了判读；
    # ② 判读不脱色 ⇒ 彩色输出里那句"不许出现的"被转义码劈开，门看不见它。
    ("scripts/check-review-closeout.py",
     '    return (b[:LOG_READ_CAP] + b"\\n" + b[-LOG_READ_CAP:]).decode("utf-8", errors="replace")',
     '    return b[:LOG_READ_CAP].decode("utf-8", errors="replace")', "禁串只在尾部"),
    ("scripts/check-review-closeout.py", "            body = strip_ansi(log_window(p))",
     "            body = log_window(p)", "禁串被 ANSI 遮挡"),
]


def md5(path: Path) -> str:
    return hashlib.md5(path.read_bytes()).hexdigest()


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--list", action="store_true", help="只印刀谱")
    a = ap.parse_args()
    if a.list:
        for i, (f, _, _, want) in enumerate(KNIVES, 1):
            print(f"{i:>2}  {f:<32} 摘掉那一行 ⇒ 期望开火：{want}")
        return 0

    files = sorted({ROOT / f for f, _, _, _ in KNIVES})
    bak = Path(tempfile.mkdtemp(prefix="gate-teeth-"))
    start = {}
    for p in files:
        shutil.copy2(p, bak / p.name)
        start[p] = md5(p)
    # 这份取证的首尾由脚本自己印：之前那两行 `###` 是我手写的，其中"第 20/21/22 刀是本轮补的三支牙"
    # 和文件正文的 `[刀 N]` 编号直接对不上（真正的编号是 10/11/12）——手写的条件句就是这样漂的。
    # 现在刀数取 `len(KNIVES)`、编号取打印序、被测字节取注码前 md5，全是脚本自己知道的量。
    stamp = datetime.datetime.now().astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
    print(f"### 门禁判据反向测（{len(KNIVES)} 刀，编号即下面 `[刀 N]` 的打印序）；"
          f"生产方 scripts/{Path(__file__).name}；测量 {stamp}")
    print("### 注码前被测件 md5：" + " ".join(f"{p.name}={start[p]}" for p in files))
    print()
    print(f"$ python3 scripts/{Path(__file__).name}")
    killed = 0
    try:
        for i, (rel, anchor, mutation, want) in enumerate(KNIVES, 1):
            src = ROOT / rel
            text = src.read_text(encoding="utf-8")
            if text.count(anchor) != 1:
                print(f"[刀 {i:>2}] {rel} 锚点出现 {text.count(anchor)} 次"
                      f"（不唯一＝注码没落地，这一刀不算测过）判错 ✗")
                continue
            src.write_text(text.replace(anchor, mutation, 1), encoding="utf-8")
            p = subprocess.run([sys.executable, rel, "--selftest"], cwd=ROOT,
                               capture_output=True, text=True)
            shutil.copy2(bak / src.name, src)
            if md5(src) != start[src]:
                print(f"[刀 {i:>2}] !! 还原失败 {rel} md5 变了，立刻停")
                return 2
            reds = [l.strip() for l in p.stdout.splitlines() if "✗" in l]
            tail = p.stdout.splitlines()[-1] if p.stdout.splitlines() else "(零输出＝注码后脚本根本没跑起来)"
            hit = [l for l in reds if any(k in l for k in want.split("|"))]
            killed += 1 if hit else 0
            print(f"[刀 {i:>2}] {rel.split('/')[-1]:<26} 摘：{want:<28} "
                  f"{'点名到那一格 ✓' if hit else '没开火＝该格无牙 ✗'}")
            print(f"        rc={p.returncode} 汇总行「{tail}」 判错格数={len(reds)}")
            for l in hit:
                print(f"        · {l[:120]}")
    finally:
        for p in files:
            if md5(p) != start[p]:
                shutil.copy2(bak / p.name, p)
                print(f"收尾补还原 {p}")
        shutil.rmtree(bak, ignore_errors=True)
    print(f"===== 门牙反向测：{killed}/{len(KNIVES)} 刀各自点名到该开火的那一格 =====")
    rc = 0 if killed == len(KNIVES) else 1
    print(f"rc={rc}")
    return rc


if __name__ == "__main__":
    sys.exit(main())
