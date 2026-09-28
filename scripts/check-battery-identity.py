#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""常驻电池的门：每一族的产物里必须写着"这轮读数测的是哪一笔字节"。

为什么立这道门（本轮修前现数，修后由 A5 的末行自己印）：取证落点迁进仓库树之后，
`docs/superpowers/specs/ledger/logs/` 下 15 个族共 562 份常驻日志、66 个轮次目录，按短语
`基线字节` 命中只有 P701 的 12 份、R30 的 1 份
⇒ 其余族的读数**在树里查不到它测于哪一笔提交**。两种失效各有形状，都得拦：
  ① 顺序失效——AiTrigger／DBPool 的驱动里本来就有这一行，却打印在 `tee_to()` **之前**：
     终端看得见、产物里没有（13/14 份产物零命中就是这么来的），改代码的人以为已经记了；
  ② 根本没有——其余驱动压根没这行，产物目录名只带墙钟戳。

两道轴，缺一不可：
  轴一（源码形状，便宜、放刀前就能判）：见 `check()` 的 A1/A2/A3。只看**代码行**——
      docstring 与行尾注释里的 `identity()`／`基线字节` 是散文，不是调用点，拿原文判会把
      已改对的驱动判成红（DBPool 的模块 docstring 第 22 行就是这句话）。
  轴二（产物实测，A4）：直接读树里该族**最近一轮**的产物，要求其中至少一份印着 `基线字节`。
      形状轴量不到的两件事由它兜住：跨代码路径的先后（`--check-tree` 那一支本就不落产物，
      身份行天然在 tee 之前）、以及"改了写路径却没跑过"（p703 族：证据写进树的能力是本轮新加的，
      没跑就没有产物）。只锁形状＝交付一份没验过的常驻件，所以 A4 才算闭合。

判据（逐枚驱动，一枚都不许跳过）：
  A1 证据打开点：`tee_to(` 或 `.write_text(` 在代码行里存在（本件的 `--check` 分流不许在 tee 之前 return）。
  A2 身份发射点：代码行里有 `identity(` 调用**或**字面 `基线字节`（P701/R30 用带自有上下文的 print，
      不强行换成助手函数——判据要能区分"没做"与"用别的方式做了"）；且产物打开之后还有一行发射：
      tee 型要求存在 `发射行号 > 首个 tee_to(`，write_text 型要求存在 `发射行号 < 某次 write_text(`。
  A3 取证落点：`.gitignore` 里有该族的**成对**例外行（目录行 + `**/*.log` 行）——只有一行等于
      "目录能建、文件不入库"，读数照样蒸发。
  A4 产物实测：该族在树里的最近一轮目录存在，且其中 ≥1 份文件印着 `基线字节`。
  A5 族归属对账：`logs/` 下每一族目录必须有归属——要么由某枚驱动反推出来（进 A4），要么在
      `NOT_A_BATTERY` 里写明"这族不是电池读数"的理由（空理由、过期条目、双身份都判红）。
      没有这一格，A4 的判据对象只来自驱动正文：树里长出一族没有驱动的目录时产物轴从来不看它，
      "两轴全绿"就成了对那假族的绿（本轮实测：磁盘 16 族、门只判 14 族，差的两族即由此登记补足）。
  A6 口径与机制对账（两半，缺一半都留假绿）：
    A6a 声明与机制相反——驱动把来树文件 `shutil.copy2()` 盖进克隆，却仍声明
        『来树未入库字节不进本轮读数』（`CLAIM_HEAD_ONLY`）。2026-09-28 复查抓到 B17action／P503／R22
        三枚正是这一形——那句话是**假身份行**，而 A4 只认"产物里有没有这句"，认不出这句是假的。
    A6b 机制无份数——同一形（`copy2` ＋ 落点从 `clone` 派生）却**一处现测份数都没有**：不传 `overlay=`，
        也没有手写的"已在 HEAD n/N"／"覆盖 N 个"。这一支比 A6a 更安静：它不撒谎，只是让身份行说不出
        本轮读了来树几份，读数从此无法复算（修 A6a 时现测：R30／P701／P703 三支全靠手写份数过关）。
        两支的正解同一条：让份数成为**读数**而不是声明（`identity(..., overlay=[...])` 或手写现测行）。

反向自测（`--selftest`）：形状轴的每一格都绑定"哪条判据分支该开火"，A4 用真临时目录做正反两格，
A5 用假集合做正反格（含"过期豁免"与"空理由"两支），A6 做两红七正控（含"该声明只在注释里 ⇒
抹散文后不许判红"与"就地注码的 copy2 ⇒ 不开火"，防止把改对了的驱动判坏），豁免那一族另配
"同形但产物没带着 ⇒ 照旧红"——否则这道门和它拦的那批驱动犯的是同一个错（恒报没问题）。
"""
from __future__ import annotations

import argparse
import ast
import io
import re
import sys
import tempfile
from pathlib import Path
from tokenize import COMMENT, generate_tokens

ROOT = Path(__file__).resolve().parents[1]
LOGROOT = "docs/superpowers/specs/ledger/logs/"
PHRASE = "基线字节"
# mut_dispose.py 是"挡危险 --clone"的公共闸，本身不产证据，不在成员名单里。
EXCLUDE = {"mut_dispose.py"}

EMIT = re.compile(r"\bidentity\(|" + PHRASE)

# 身份行里那句"本轮只读克隆 HEAD"的口径短语。它是一句**声明**，而声明可以假：
# 只要同一枚驱动又把工作树文件 `copy2` 进克隆，这句话就与机制相反（A6a 判红）。
CLAIM_HEAD_ONLY = "不进本轮读数"
# "把来树字节盖进克隆"的机制形状：落点从 `clone` 派生 ＋ 一次 `shutil.copy2(`。
# 只认这两个形状的组合，不认 `clone` 这个词——b16 那三枚就地注码的驱动也有 `clone`
# 字样（0 处克隆内落点赋值），它们的 `copy2` 是把文件备份进 `bak/`，不是覆盖进克隆。
CLONE_DST = re.compile(r"=\s*clone\s*/")
# 覆盖一旦发生，份数必须**现测**进产物（否则读数无法复算）。三种已存在的写法都算合格：
# 走 `identity(..., overlay=[...])`、手写"本卡文件已在 HEAD n/N"、打印"覆盖 {len(...)} 个…"。
QUANTIFY = ("overlay=", "已在 HEAD", "覆盖 {")

# 树里确实有轮次目录、但**不是常驻电池**的族：它们的产物由人工执行的命令重定向而来，
# 没有一枚驱动可以挂身份行。A5（族归属对账）靠这份名单放行它们，理由必须写实——
# 空字符串等于"用一行代码关掉一格判据"。
NOT_A_BATTERY = {
    "CIwiring": "某几轮 `make audit`／逐门复跑的手工重定向读数，身份在正文的 SHA 叙述里，无驱动可挂",
    "Transcript": "check-secret-containment.py `--transcripts` 那一面的一次性普查读数（键名与次数），"
                  "产物由人工命令行重定向落盘，不是电池格子",
}


def tree_families() -> list:
    """`logs/` 下现存的族目录名（轴二的地面集合，不来自任何名单）。"""
    base = ROOT / LOGROOT
    if not base.is_dir():
        return []
    return sorted(p.name for p in base.iterdir() if p.is_dir())


def accounting(covered: set, registered: dict, on_disk: list) -> list:
    """A5：树里每一族必须有归属——要么是某枚驱动的取证落点，要么在 `NOT_A_BATTERY` 里写明理由。

    这一格管的是**门的覆盖集合与磁盘集合之差**：A4 的族是从驱动正文反推出来的，
    树里长出一族没有驱动登记的目录（比如有人把一次性读数 `tee` 进 `logs/`），
    整条 A4 就从来不看它——"两轴全绿"于是成了对一族产物的假证。
    """
    bad = []
    for fam in on_disk:
        if fam in covered:
            continue
        reason = registered.get(fam)
        if reason is None:
            bad.append(f"A5 族 `{fam}` 在树里有轮次目录，却既无驱动归属、也未登记为『非电池』"
                       "⇒ 本门的产物轴从来不看它")
        elif not reason.strip():
            bad.append(f"A5 族 `{fam}` 登记为非电池但理由为空 ⇒ 豁免不许留空条目")
    for fam in sorted(set(registered) - set(on_disk)):
        bad.append(f"A5 非电池名单里的 `{fam}` 在树里已没有轮次目录 ⇒ 豁免条目过期，要么删要么改成驱动族")
    for fam in sorted(set(registered) & covered):
        bad.append(f"A5 族 `{fam}` 既有驱动归属又登记为非电池 ⇒ 两种身份不能同时成立")
    return bad


def drivers() -> list[Path]:
    """成员名单现取（不写死）：新增一枚常驻电池就自动进门，漏登记不可能。

    判据来自"它自己声明了仓库内的取证落点"：只有把产物写进 `logs/<族>` 的驱动才要求身份行，
    一次性脚本没有常驻产物，不进这道门。
    """
    out = []
    for p in sorted((ROOT / "scripts").glob("mut_*.py")):
        if p.name in EXCLUDE:
            continue
        if LOGROOT in p.read_text(encoding="utf-8"):
            out.append(p)
    # 探针件（不走 mut_ 前缀）按同一口径进门：它的 summary 也是常驻产物。
    probe = ROOT / "scripts/cap-ab-53300-probe.py"
    if probe.exists() and LOGROOT in probe.read_text(encoding="utf-8"):
        out.append(probe)
    return out


def code_lines(text: str):
    """把 docstring 与行尾注释抹成空行，返回 `[(行号, 代码文本)]`（行号不变，便于点名）。

    抹不掉或解析不了 ⇒ 返回 `None`：这道门对"读不懂的驱动"向红偏置，绝不静默放行。
    """
    lines = text.splitlines()
    blanked = [False] * (len(lines) + 2)
    try:
        tree = ast.parse(text)
    except SyntaxError:
        return None
    for node in ast.walk(tree):
        body = getattr(node, "body", None)
        # `Lambda.body` 是表达式不是语句列表 ⇒ 只认 list 型 body，否则 `body[0]` 直接 TypeError
        if not isinstance(body, list) or not body:
            continue
        first = body[0]
        if (isinstance(first, ast.Expr) and isinstance(first.value, ast.Constant)
                and isinstance(first.value.value, str)):
            for i in range(first.lineno, min(first.end_lineno, len(lines)) + 1):
                blanked[i] = True
    try:
        for tok in generate_tokens(io.StringIO(text).readline):
            if tok.type == COMMENT:
                ln = tok.start[0]
                if 1 <= ln <= len(lines) and not blanked[ln]:
                    lines[ln - 1] = lines[ln - 1][: tok.start[1]]
    except Exception:
        return None
    return [(i, ln) for i, ln in enumerate(lines, 1) if not blanked[i]]


def gitignore_pairs() -> set:
    """`.gitignore` 里已开例外的族名集合，且必须是**成对**（目录行与 `**/*.log` 行都在）。

    只出现一次的族不算数：`!…/logs/X/` 放行目录、`!…/logs/X/**/*.log` 才放行文件，
    缺后者等于"每轮建了目录、里面一份不入库"。
    """
    text = (ROOT / ".gitignore").read_text(encoding="utf-8")
    dirs, logs = set(), set()
    for line in text.splitlines():
        m = re.fullmatch(r"!docs/superpowers/specs/ledger/logs/([^/]+)/", line.strip())
        if m:
            dirs.add(m.group(1))
        m = re.fullmatch(r"!docs/superpowers/specs/ledger/logs/([^/]+)/\*\*/\*\.log", line.strip())
        if m:
            logs.add(m.group(1))
    return dirs & logs


def check(code, pairs: set, family: str, order_cleared: bool = False) -> list:
    """一枚驱动的轴一判据：返回"为什么这一枚不合格"的行列表（空＝合格）。

    `code` 必须是 `code_lines()` 的输出（散文已抹）；`None` 由调用方先挡掉。
    两种"产物打开"的形状各自要求相反的先后：
      · tee 型（`tee_to()` 之后 `print` 才进得了日志）⇒ 发射行必须**晚于**首个 tee；
      · write_text 型（探针件把行收集进 `lines` 再整体落盘）⇒ 发射行必须**早于**某次写盘。
    拿同一把尺量两种形状，就会把对的判成红、把错的判成绿。

    `order_cleared`：该族最近一轮**产物**实测已带着身份行。行号先后量不到"发射语句在某个函数里、
    而那函数在 tee 之后才被调用"（P701 就是这一形：打印在 `prepare()` 内，`main()` 先 tee 再调它）。
    这时由轴二作证据豁免——只豁免先后这一支；缺发射点、缺打开点、缺成对例外都不豁免。
    """
    bad = []
    tee = [i for i, ln in code if "tee_to(" in ln]
    wr = [i for i, ln in code if ".write_text(" in ln]
    emit = [i for i, ln in code if EMIT.search(ln)]
    if not emit:
        bad.append(f"A2 代码行里没有身份发射点（`identity(` 或字面 `{PHRASE}`）："
                   "产物里查不到这轮读的是哪一笔字节")
    elif tee:
        if not any(e > min(tee) for e in emit):
            bad.append(f"A2 顺序失效：tee 之后没有任何身份发射行（首个 `tee_to(` 在第 {min(tee)} 行，"
                       f"发射行在 {emit}）⇒ 身份行早于产物打开，只进终端不进日志")
    elif wr:
        if not any(e < w for e in emit for w in wr):
            bad.append(f"A2 顺序失效：身份发射行 {emit} 不早于任何一次 `.write_text(` {wr}"
                       "⇒ 身份行没赶上那份产物")
    else:
        bad.append("A1 代码行里既不 `tee_to(` 也不 `.write_text(`：产物打开点找不到，身份行无处可落")
    if family and family not in pairs:
        bad.append(f"A3 族 `{family}` 在 `.gitignore` 里没有成对例外行（目录行 + `**/*.log` 行）")
    # A6：身份行的**口径**不许与装架机制相反。2026-09-28 复查抓到三枚驱动（B17action／P503／R22）
    # 一边把 `ROOT/rel` 用 `shutil.copy2()` 盖进克隆、一边在 `extra` 里写"来树未入库字节不进本轮
    # 读数"——A4 量"有没有这句"，量不到"这句是不是假的"，所以这句话必须靠形状对账才守得住。
    text = "\n".join(ln for _, ln in code)
    overlaid = "shutil.copy2(" in text and CLONE_DST.search(text) is not None
    if overlaid and CLAIM_HEAD_ONLY in text:
        bad.append(f"A6a 装架把来树文件覆盖进克隆（`shutil.copy2(`）却声明『{CLAIM_HEAD_ONLY}』"
                   "⇒ 身份行与机制相反；改传 `identity(..., overlay=[...])` 让份数现测")
    if overlaid and not any(m in text for m in QUANTIFY):
        bad.append("A6b 覆盖来树字节进了克隆，全驱动却没有一个现测份数"
                   f"（`{'`／`'.join(QUANTIFY)}` 三者皆无）"
                   "⇒ 身份行说不出本轮究竟读了来树几份，读数无法复算")
    if order_cleared:
        bad = [b for b in bad if "顺序失效" not in b]
    return bad


def artifact_verdict(family: str):
    """轴二：该族最近一轮产物里到底有没有那句身份行。返回 `(判定, 说明)`。"""
    fam = ROOT / LOGROOT / family
    if not fam.is_dir():
        return "无轮次目录", f"{LOGROOT}{family}/ 不存在 ⇒ 本族的常驻产物从未落进仓库树"
    rounds = sorted((p for p in fam.iterdir() if p.is_dir()), key=lambda p: p.name)
    if not rounds:
        return "无轮次目录", f"{LOGROOT}{family}/ 下没有任何轮次目录"
    last = rounds[-1]
    files = [p for p in last.rglob("*") if p.is_file()]
    hit = [p.name for p in files if PHRASE in p.read_text(encoding="utf-8", errors="replace")]
    if not hit:
        return "缺身份行", (f"最近一轮 `{last.name}` 共 {len(files)} 份产物，"
                            f"按短语 `{PHRASE}` 命中 0 份 ⇒ 这轮读数测于哪一笔字节无从查起")
    return "有身份行", (f"最近一轮 `{last.name}` 共 {len(files)} 份产物，"
                        f"{len(hit)} 份带 `{PHRASE}`（{', '.join(sorted(hit)[:3])}"
                        f"{' 等' if len(hit) > 3 else ''}）")


def selftest() -> int:
    """反向格各自绑定"哪条判据分支该开火"，只判关键字会在拧错地方的时候假绿。"""
    cases = [
        ("T1 缺身份行 ⇒ A2 开火（不许因为 tee 在就放行）",
         "tee_to(LOGDIR / '00-run.log')\nprint('判定')\n", {"X"}, "X", "A2 代码行里没有身份发射点"),
        ("T2 身份行早于 tee ⇒ A2 顺序分支开火",
         "print(identity(ROOT))\ntee_to(LOGDIR / '00-run.log')\n", {"X"}, "X", "顺序失效"),
        ("T3 族只有目录行 ⇒ A3 开火（目录能建、文件不入库）",
         "tee_to(LOGDIR / '00-run.log')\nidentity(ROOT)\n", set(), "X", "A3 族"),
        ("T4 正控制：tee 在前、身份在后、族成对 ⇒ 合格",
         "tee_to(LOGDIR / '00-run.log')\nidentity(ROOT)\n", {"X"}, "X", ""),
        ("T5 只有 tee 没有身份行 ⇒ A2 开火（本族的原始失效形状）",
         "tee_to(LOGDIR / '00-run.log')\n", {"X"}, "X", "A2 代码行里没有身份发射点"),
        ("T6 write_text 型：身份行早于写盘 ⇒ 合格（探针件的正当形状）",
         "lines.append(identity(ROOT))\nsummary.write_text(x)\n", {"X"}, "X", ""),
        ("T7 write_text 型：身份行排在**所有**写盘之后 ⇒ 顺序分支开火",
         "a.write_text(x)\nb.write_text(y)\nlines.append(identity(ROOT))\n", {"X"}, "X", "顺序失效"),
        ("T8 既无 tee 也无 write_text ⇒ A1 开火（身份行无处可落）",
         "identity(ROOT)\n", {"X"}, "X", "A1 代码行里既不"),
        ("T9 tee 与 write_text 都有、身份在 tee 之前 ⇒ 顺序分支开火（混形不许放行）",
         "identity(ROOT)\ntee_to(LOGDIR / '00-run.log')\nsummary.write_text(x)\n", {"X"}, "X",
         "顺序失效"),
        # —— 散文不许当调用点（本轮实测踩到的两类假阳）——
        ("T10 `identity(` 只出现在行尾注释里 ⇒ A2 开火（注释不是调用点）",
         "tee_to(LOGDIR / '00-run.log')\nprint(x)  # 这里补一行 identity(ROOT) 的说明\n",
         {"X"}, "X", "A2 代码行里没有身份发射点"),
        ("T11 `基线字节` 只在模块 docstring 里 ⇒ A2 开火（散文不是发射点）",
         '"""驱动说明：基线字节＝克隆 HEAD，身份行由 identity() 打印。\n第二行"""\n'
         "tee_to(LOGDIR / '00-run.log')\nprint('判定')\n", {"X"}, "X",
         "A2 代码行里没有身份发射点"),
        ("T12 正控制：docstring 提到身份行、代码里 tee 在前 print 在后 ⇒ 合格",
         '"""说明：本件在 tee 之后打印 基线字节。\n第二行"""\n'
         "tee_to(LOGDIR / '00-run.log')\nprint('基线字节：克隆 HEAD ' + head)\n", {"X"}, "X", ""),
        ("T13 另一条代码路径上的身份行不许替 tee 路径作证 ⇒ 顺序分支仍开火",
         "if args.check_tree:\n    identity(ROOT)\n    return do_check()\n"
         "tee_to(LOGDIR / '00-run.log')\nprint('判定')\n", {"X"}, "X", "顺序失效"),
        # 本轮实测的门自身缺陷：`Lambda.body` 是表达式不是语句列表，`body[0]` 直接 TypeError。
        # 门崩了不等于门红了——这一格要求带 lambda 的驱动照样判得动。
        ("T14 驱动里有 `lambda: …` ⇒ 门不崩且照常判（正控制）",
         "HANDLERS = {'a': lambda: run()}\ntee_to(LOGDIR / '00-run.log')\nprint('基线字节')\n",
         {"X"}, "X", ""),
    ]
    failed = 0
    for name, src, pairs, family, want in cases:
        code = code_lines(src)
        bad = check(code, pairs, family) if code is not None else ["解析失败"]
        joined = " / ".join(bad)
        if want and want not in joined:
            print(f"  ✗ {name}：期望点名『{want}』，实际 {joined or '（判为合格）'}")
            failed += 1
        elif not want and bad:
            print(f"  ✗ {name}：正控制被判红：{joined}")
            failed += 1
        else:
            print(f"  ✓ {name}")

    # 轴二的三格：A4 用真临时目录证明"没目录/有目录无身份行/有身份行"三种输入各有不同判定。
    global ROOT
    real_root = ROOT
    with tempfile.TemporaryDirectory() as td:
        try:
            ROOT = Path(td)
            empt = artifact_verdict("NoSuchFamily")
            if "无轮次目录" not in empt[0]:
                print(f"  ✗ T15 族目录不存在 ⇒ A4 必须报『无轮次目录』，实际 {empt}")
                failed += 1
            else:
                print("  ✓ T15 族目录不存在 ⇒ A4 报『无轮次目录』（不是放行）")
            fam = Path(td) / LOGROOT / "X"
            (fam / "r1").mkdir(parents=True)
            (fam / "r1" / "00-run.log").write_text("判定：全杀\n", encoding="utf-8")
            got = artifact_verdict("X")
            if "缺身份行" not in got[0]:
                print(f"  ✗ T16 最近一轮无身份行 ⇒ A4 必须报『缺身份行』，实际 {got}")
                failed += 1
            else:
                print("  ✓ T16 最近一轮无身份行 ⇒ A4 报『缺身份行』")
            (fam / "r2").mkdir()
            (fam / "r2" / "00-run.log").write_text("基线字节：`abc1234`｜未入库字节 0 处\n",
                                                   encoding="utf-8")
            got = artifact_verdict("X")
            if "有身份行" not in got[0]:
                print(f"  ✗ T17 正控制：新轮次带身份行 ⇒ A4 应放行，实际 {got}")
                failed += 1
            else:
                print("  ✓ T17 正控制：最近一轮带身份行 ⇒ A4 放行")
        finally:
            ROOT = real_root

    # 轴一"先后"那一支的豁免（order_cleared）：轴二实测到该族最近一轮产物已带着身份行。
    # 三格合起来证明豁免是**有界的**——只撤"先后"，缺发射点/缺成对例外照样红。
    ex_src = "def prepare(tmp):\n    print('基线字节：克隆 HEAD ' + head)\n" \
             "def main():\n    tee_to(LOGDIR / '00-run.log')\n    prepare(tmp)\n"
    ex = [
        ("T18 正控制（P701 那一形）：发射在函数体内、调用点在 tee 之后 ⇒ 产物实测豁免'先后'",
         check(code_lines(ex_src), {"X"}, "X", order_cleared=True), []),
        ("T19 同一形状但产物没带着 ⇒ '先后'照旧红（豁免只认轴二的事实）",
         check(code_lines(ex_src), {"X"}, "X", order_cleared=False), ["顺序失效"]),
        ("T20 豁免不许顺手撤掉 A3：族无成对例外 ⇒ 仍红",
         check(code_lines(ex_src), set(), "X", order_cleared=True), ["A3 族"]),
        ("T21 豁免不许撤掉'根本没有发射点'：只有 tee ⇒ 仍红",
         check(code_lines("tee_to(LOGDIR / '00-run.log')\n"), {"X"}, "X",
               order_cleared=True), ["A2 代码行里没有身份发射点"]),
    ]
    for name, got, want in ex:
        joined = " / ".join(got)
        if all(w in joined for w in want) and (want or not got):
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {joined or '（判为合格）'}")
            failed += 1

    # A5（族归属对账）：判据对象是"门看到的族集合"与"磁盘上的族集合"之差。
    # 这四格各自绑定一支：未登记的新族／登记了但树里已没有（过期豁免）／理由为空／双身份。
    ac = [
        ("T22 树里长出无驱动、无登记的族 ⇒ A5 开火（A4 从来不看它）",
         accounting({"X"}, {"Y": "非电池的理由"}, ["X", "Y", "Z"]), ["A5 族 `Z`"]),
        ("T23 正控制：每族要么有驱动要么有带理由的登记 ⇒ 合格",
         accounting({"X"}, {"Z": "一次性普查读数"}, ["X", "Z"]), []),
        ("T24 登记的族在树里已消失 ⇒ A5 开火（豁免条目过期）",
         accounting({"X"}, {"Z": "曾经的一次性读数"}, ["X"]), ["A5 非电池名单里的 `Z`"]),
        ("T25 理由为空 ⇒ A5 开火（拿空串关掉一格）",
         accounting(set(), {"Z": "   "}, ["Z"]), ["理由为空"]),
        ("T25b 同一族既有驱动又登记为非电池 ⇒ A5 开火（双身份）",
         accounting({"Z"}, {"Z": "既是电池又登记"}, ["Z"]), ["两种身份"]),
    ]
    for name, got, want in ac:
        joined = " / ".join(got)
        if all(w in joined for w in want) and (want or not got):
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {joined or '（判为合格）'}")
            failed += 1
    # A6（口径与机制对账）：本轮复查抓到 b17/p503/R22 三枚驱动**把来树字节 copy2 进克隆**，
    # 身份行却写"来树未入库字节不进本轮读数"。A4 只认"有没有这句"，认不出这句是假的。
    ov_src = ("def prepare(clone):\n    tgt = clone / rel\n    shutil.copy2(src, tgt)\n"
              "tee_to(LOGDIR / '00-run.log')\n"
              "identity(ROOT, extra='｜本轮读私有克隆的 HEAD（来树未入库字节不进本轮读数）')\n")
    oc = [
        ("T26 覆盖来树字节却声明『不进本轮读数』⇒ A6 开火（假身份行，A4 量不到）",
         check(code_lines(ov_src), {"X"}, "X"), ["A6a 装架把来树文件覆盖进克隆"]),
        ("T27 正控制：同样有 copy2 装架，但身份行走 `overlay=` 现测份数 ⇒ A6 不开火",
         check(code_lines(ov_src.replace(
             "identity(ROOT, extra='｜本轮读私有克隆的 HEAD（来树未入库字节不进本轮读数）')",
             "identity(ROOT, overlay=files)")), {"X"}, "X"), []),
        ("T28 正控制：纯克隆 HEAD（无 copy2 装架）＋该声明 ⇒ A6 不开火（声明与机制相符）",
         check(code_lines(ov_src.replace("    shutil.copy2(src, tgt)\n", "    pass\n")),
               {"X"}, "X"), []),
        ("T29 该声明只在注释里 ⇒ 抹散文后不许凭它判红（防把改对了的驱动判坏）",
         check(code_lines(ov_src.replace("identity(ROOT, extra='｜本轮读私有克隆的 HEAD（来树未入库字节不进本轮读数）')",
                                         "identity(ROOT, overlay=files)  # 旧口径说不进本轮读数")),
               {"X"}, "X"), []),
    ]
    # A6b：A6a 的反面那一半——覆盖来树字节、身份行里却**一个可复算的份数都没有**。
    # 这一格不是假想：本轮修 A6a 时现测，`r30`／`p701`／`p703` 三支的手写身份行之所以过关，
    # 全靠它们各有一处现测份数；把那一处删掉，产物就只剩"读克隆 HEAD"这句无法核对的话。
    ovq_src = ("def prepare(clone):\n    tgt = clone / rel\n    shutil.copy2(src, tgt)\n"
               "tee_to(LOGDIR / '00-run.log')\nidentity(ROOT)\n")
    oc += [
        ("T30 覆盖来树字节、身份行无任何现测份数 ⇒ A6b 开火（读数无法复算）",
         check(code_lines(ovq_src), {"X"}, "X"), ["A6b"]),
        ("T31 正控制：同一装架，身份行走 `identity(..., overlay=...)` ⇒ A6b 不开火",
         check(code_lines(ovq_src.replace("identity(ROOT)", "identity(ROOT, overlay=mods)")),
               {"X"}, "X"), []),
        ("T32 正控制：手写行含『已在 HEAD n/N』的可复算量 ⇒ A6b 不开火",
         check(code_lines(ovq_src.replace("identity(ROOT)",
                                          "print(f'基线字节：本卡文件已在 HEAD {in_head}/{len(NEW)}')")),
               {"X"}, "X"), []),
        ("T33 正控制：打印『覆盖 {len(mods)} 个脏文件』⇒ A6b 不开火",
         check(code_lines(ovq_src.replace("identity(ROOT)",
                                          "print(f'基线字节：覆盖 {len(mods)} 个脏文件进克隆')")),
               {"X"}, "X"), []),
        ("T34 正控制：就地注码的 `copy2`（备份进 `bak/`，无克隆内落点）⇒ A6b 不开火",
         check(code_lines(ovq_src.replace("    tgt = clone / rel\n", "    tgt = bak / rel\n")),
               {"X"}, "X"), []),
    ]
    for name, got, want in oc:
        joined = " / ".join(got)
        if all(w in joined for w in want) and (want or not got):
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {joined or '（判为合格）'}")
            failed += 1
    print(f"===== 预检自测：{len(cases)} + 3 + {len(ex)} + {len(ac)} + {len(oc)} 格，"
          f"失败 {failed} 格 =====")
    return 1 if failed else 0


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--selftest", action="store_true", help="只跑内存反向格（不读仓库树）")
    ap.add_argument("--quiet-ok", action="store_true", help="全合格时只印末行")
    ap.add_argument("--no-artifact-axis", action="store_true",
                    help="只跑轴一（源码形状）：改驱动中途用，闭合判据仍靠默认两轴")
    args = ap.parse_args()
    if args.selftest:
        return selftest()

    pairs = gitignore_pairs()
    if not pairs:
        print("RED-UNNAMED：`.gitignore` 里一枚族的例外都没解析到 ⇒ 这道门没有判据对象")
        return 1
    ds = drivers()
    if not ds:
        print(f"RED-UNNAMED：scripts/ 下没有声明 `{LOGROOT}<族>` 落点的常驻驱动 ⇒ 名单枚举本身失守")
        return 1

    bad = 0
    seen_families = []
    for p in ds:
        text = p.read_text(encoding="utf-8")
        m = re.search(re.escape(LOGROOT) + r"([A-Za-z0-9_-]+)", text)
        family = m.group(1) if m else ""
        if family:
            seen_families.append(family)
    # 轴二先算：它是"产物里到底有没有"的地面事实，轴一的先后疑点由它作证据豁免（见 check 的 order_cleared）。
    verdicts = {f: artifact_verdict(f) for f in sorted(set(seen_families))}
    if args.no_artifact_axis:
        verdicts = {}

    for p in ds:
        text = p.read_text(encoding="utf-8")
        m = re.search(re.escape(LOGROOT) + r"([A-Za-z0-9_-]+)", text)
        family = m.group(1) if m else ""
        code = code_lines(text)
        if code is None:
            bad += 1
            print(f"  ✗ {p.name}（族 {family or '未声明'}）\n        A0 解析失败（ast/tokenize）"
                  "⇒ 判据读不到代码行，向红偏置")
            continue
        cleared = verdicts.get(family, ("", ""))[0] == "有身份行"
        why = check(code, pairs, family, order_cleared=cleared)
        if why:
            bad += 1
            print(f"  ✗ {p.name}（族 {family or '未声明'}）")
            for w in why:
                print(f"        {w}")
        else:
            emit_lines = [i for i, ln in code if EMIT.search(ln)]
            tee_lines = [i for i, ln in code if "tee_to(" in ln]
            waived = (cleared and tee_lines and max(emit_lines) < min(tee_lines))
            if not args.quiet_ok:
                print(f"  ✓ {p.name}（族 {family}）"
                      + ("｜顺序疑点已由最近一轮产物实测豁免（发射在函数体内、调用点在 tee 之后）"
                         if waived else ""))

    # 轴二：按族实测最近一轮产物（一枚族只判一次，多枚驱动共用同族时不重复计红）。
    on_disk = []
    if not args.no_artifact_axis:
        for family in sorted(set(seen_families)):
            verdict, note = verdicts[family]
            if verdict == "有身份行":
                if not args.quiet_ok:
                    print(f"  ✓ 产物 A4〔{family}〕{note}")
            else:
                bad += 1
                print(f"  ✗ 产物 A4〔{family}〕{note}")
        # A5：族归属对账——门的判据对象必须等于磁盘上的族集合，
        # 差额只由 `NOT_A_BATTERY` 里带理由的登记补足（A4 的族是从驱动正文反推的，
        # 树里长出一族没有驱动的目录时，整条产物轴从来不看它）。
        on_disk = tree_families()
        for w in accounting(set(seen_families), NOT_A_BATTERY, on_disk):
            bad += 1
            print(f"  ✗ {w}")
        if not args.quiet_ok:
            extra = sorted(set(on_disk) & set(NOT_A_BATTERY) - set(seen_families))
            print(f"  ✓ 归属 A5：磁盘 {len(on_disk)} 族＝驱动 {len(set(seen_families))} 族"
                  + (f"＋非电池登记 {len(extra)} 族（{', '.join(extra)}）" if extra else "，无未登记族"))

    print(f"===== 字节身份行门：{len(ds)} 枚驱动（成员现取）／"
          f"{len(set(seen_families))} 族产物实测"
          + (f"／磁盘 {len(on_disk)} 族归属对账" if on_disk else "（产物轴未开")
          + f"，{bad} 项不合格 =====")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
