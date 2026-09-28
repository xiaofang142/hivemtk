#!/usr/bin/env python3
"""把每份门禁件各自的 `--selftest` 汇编成一份取证（台账 P-xx 引用它）。

为什么要有这个生产件：这份取证早先是一次性手跑的产物，**仓库里没有生产方**——台账把它当证据、
闭合门逐字核它的内容，而重出它的那段 shell 只活在 `/tmp`（下一轮一开机就没有了）。引用一份
没人能重跑的读数，等于把"证据"降级成"传说"。

它只汇编、不判定被测对象：各份门禁件的判据反向测现在各自都已经是合并门禁的步骤
（`merge-gate-selftest` / `clone-selftest` / `anchor-preflight-selftest` /
`family-selftests-selftest`），这一份把它们的首尾印成一页，供协议 §九 讲"判据坏了会印什么"。

它自己有两条判据（都要能被弄红，见 `--selftest`）：
1. 成员退非 0 ⇒ 这份汇编红；
2. 成员**零输出** ⇒ 也红。这条比上一条贵：一份空文件里不会出现任何 must_say 串，
   但它也可能长得像"跑了、只是没打印"——闭合门那时会红在"断言没命中"，红因指向台账而不是指向
   "这一格根本没跑"。所以在生产方就地分开。
"""
from __future__ import annotations

import argparse
import datetime
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
LOGS_REL = Path("docs/superpowers/specs/ledger/logs")
NAME = "gate-family-selftests.log"

# 成员名单：(标题, argv)。成员都是自跑的判据反向测：不编译 Go 包、不连库、不落仓内文件。
# 汇编件自己也在名单里：它的四条反向格如果只活在 `--selftest` 的一次性手跑里，这份取证就成了
# "三份有证据、第四份靠嘴"——而它判的正是前三份有没有跑。`--selftest` 不落盘，所以这一格
# 不构成"门打开自己正在写的文件"。
MEMBERS: list[tuple[str, list[str]]] = [
    ("合并门禁判据反向测", ["python3", "scripts/merge-gate.py", "--selftest"]),
    ("克隆面判据反向测", ["python3", "scripts/run-gate-in-clone.py", "--selftest"]),
    ("锚点预检反向测", ["python3", "scripts/anchor-preflight.py", "--selftest"]),
    ("本汇编件自己的反向测", ["python3", f"scripts/{Path(__file__).name}", "--selftest"]),
]


def run_member(argv: list[str]) -> tuple[int, str]:
    p = subprocess.run(argv, cwd=ROOT, capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr


def assemble(members: list[tuple[str, list[str]]],
             runner=run_member) -> tuple[int, str]:
    """返回 (rc, 整份取证文本)。rc≠0 当且仅当有成员红或零输出。"""
    stamp = datetime.datetime.now().astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
    lines = [
        f"### {len(members)} 份门禁件各自的 --selftest 读数；测量 {stamp}",
        f"### 生产方 scripts/{Path(__file__).name}（本文件由它整体覆写）；"
        "树=共享工作树（各成员趟都不编译 Go 包、不连库、不落临时文件）",
        "",
    ]
    reds: list[str] = []
    for title, argv in members:
        rc, out = runner(argv)
        lines.append(f"$ {' '.join(argv)}")
        lines.append(out.rstrip("\n"))
        if not out.strip():
            reds.append(f"{title}（零输出＝没跑过，不是绿）")
            lines.append(f"rc={rc} 且输出为空 ⇒ 判红")
        elif rc:
            reds.append(title)
            lines.append(f"rc={rc} ⇒ 判红")
        else:
            lines.append("rc=0")
        lines.append("")
    n_green = len(members) - len(reds)
    if reds:
        lines.append(f"===== 门禁族自检：{n_green}/{len(members)} 份绿，"
                     f"红在：{'；'.join(reds)} =====")
        rc_all = 1
    else:
        lines.append(f"===== 门禁族自检：{n_green}/{len(members)} 份门禁件各自交出非空反向测读数 =====")
        rc_all = 0
    return rc_all, "\n".join(lines) + "\n"


def selftest() -> int:
    """反向测：生产方自己的两条判据要能被弄红，且好读数不误伤。

    成员用假命令，不碰真门禁件——这里判的是"汇编器有没有牙"，不是三份件的内容。
    """
    def fake(argv: list[str]) -> tuple[int, str]:
        marker = argv[-1]
        if marker.endswith("-empty"):
            return 0, ""
        if marker.endswith("-fail"):
            return 1, "读数有，但判据退非 0\n"
        return 0, "读数有、rc=0\n"

    def one(members: list[tuple[str, list[str]]]) -> tuple[int, str]:
        return assemble(members, runner=fake)

    cases = [
        ("成员零输出要红（且红因点名'没跑过'）",
         one([("空的一格", ["x", "--empty"])]),
         lambda rc, txt: rc == 1 and "零输出＝没跑过" in txt),
        ("成员退非 0 要红",
         one([("坏的一格", ["x", "--fail"])]),
         lambda rc, txt: rc == 1 and "红在：坏的一格" in txt),
        ("好读数不误伤、且汇总行报份数",
         one([("好的一格", ["x", "--ok"])]),
         lambda rc, txt: rc == 0 and "1/1 份门禁件" in txt),
        ("每位成员都要在取证里现形（少一位＝证据少一格）",
         one(MEMBERS),
         lambda rc, txt: rc == 0 and len(MEMBERS) >= 4
         and txt.count("\n$ ") == len(MEMBERS)),
    ]
    ok = 0
    for label, (rc, txt), judge in cases:
        good = judge(rc, txt)
        ok += 1 if good else 0
        print(f"[反向] {label:<34} {'判对 ✓' if good else '判错 ✗'}（实得 rc={rc}）")
    print(f"===== gate-family-selftests --selftest：{ok}/{len(cases)} 格判对 =====")
    return 0 if ok == len(cases) else 1


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--round", default="R22-closefinal")
    ap.add_argument("--selftest", action="store_true")
    a = ap.parse_args()
    if a.selftest:
        return selftest()
    rc, text = assemble(MEMBERS)
    logdir = ROOT / LOGS_REL / a.round
    logdir.mkdir(parents=True, exist_ok=True)
    (logdir / NAME).write_text(text, encoding="utf-8")
    sys.stdout.write(text)
    return rc


if __name__ == "__main__":
    sys.exit(main())
