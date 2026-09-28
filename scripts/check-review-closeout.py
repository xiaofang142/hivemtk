#!/usr/bin/env python3
"""二次审核闭合门：台账里每条发现必须落在四种终态之一，且终态的代价要能机器核。

设计出处：docs/superpowers/specs/2026-09-22-second-review-protocol-design.md
它要治的病（同一份 spec §0 的三条实证）：
1. 审核规矩住在散文里，散文不是判据 ⇒ 这里把"什么算闭合"写成 rc；
2. "还剩几条"没有答案（闭合状态靠 `成立并已修` / `记为已知不补` 这种措辞）⇒ 判据 7 的
   **source-map 计数钉**：抽取规则按 (所属批次小节, 节标题, 形状) 三元组声明并钉期望条数，
   实抽数 ≠ 钉值即红。键里带批次小节不是冗余——`### 八、登记（看过、本批不做）` 在文中有两份
   （行 1858 属 §7.24、行 2129 属 §7.28），只按标题抽会把 6 条并成 3 条（本轮实测错过两次）。
3. "不修"不需要代价 ⇒ `blocked` 必须带 owner/due/next 且 due 一过就红；
   `locked-equivalence` 必须指点得到名的锁腿；`refuted` 必须写出"刀怎么注的、看到什么读数"。

判据的**可绕过性**也在本轮被反例逐格打过（B3 一次点出 13 格），所以这一版把所有"声明式
输入"都收成了可判的形状：路径必须仓内相对（绝对路径 / `..` / 临时目录都能把门指到仓外或
清得掉的地方）、用例名与 pkg 必须是真形状（`TestFoo.*` 能骗过 `go test -list` 的正则）、
`first_seen` 与 `id` 不可缺不可重、`repro` 必须是 {cmd, expect} 而不是自由文本、
`claim.quote` 必须落在**同一个条目**内（跨条拼接不再算原文）、`claim.section` 命中两份同名节
即判歧义红（本仓 `### 八、登记（看过、本批不做）` 实测有两份，指针落在哪一节头上门原来分不清）、
`attack/next/lock_attack`
有长度下界（"a" 不算一条攻击描述）、空台账与无 scope 都算红，
并且**每个已验的计数钉都要有台账条目兜住**（钉了 21 条承诺而节内 0 条 ⇒ 抽轴没做完）。

只读文本 + `go test -list`（不连库、不跑用例），所以可与并行 lane 同树共存；
电池才需要私有克隆，这门不需要。

用法：
  python3 scripts/check-review-closeout.py docs/superpowers/specs/ledger/R22.jsonl
  python3 scripts/check-review-closeout.py --selftest          # 门自己的反向测试
  python3 scripts/check-review-closeout.py --dump <spec>        # 打印实抽计数（写钉值时用）
"""
from __future__ import annotations

import argparse
import datetime as dt
import fnmatch
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GATE_REL = "scripts/check-review-closeout.py"
US = "user-server"
STATUSES = ("fixed", "refuted", "locked-equivalence", "blocked")
# refuted 的复验必须含动作词：只写"我认为不成立"不算复验（口径来源 §7.10 判定规矩）
ACTION_WORDS = ("注码", "注进", "注一次", "复跑", "重跑", "红因", "还原", "grep", "实测", "跑出来")
# 证据必须在仓里：/tmp 里的东西会被清理，本轮已实测"被引用为证据的 /tmp 克隆三分钟后就没了"
EPHEMERAL_PREFIXES = ("/tmp", "/private/tmp", "/var/folders", "/private/var/folders")
DUE_FMT = "%Y-%m-%d"
# Go 的测试函数名是标识符：字母（含中文）或下划线起头，后接字母/数字/下划线。
# 早先写成 `[A-Za-z][A-Za-z0-9_]*` 会把本仓大量中文命名的真腿判成非法（假红），
# 而它要拦的形状——`TestFoo.*` 这类能在 `go test -list` 命中一堆却不是点得到的那一条——仍然拦得住：
# `\w` 不含 `.`、`*`、空格、括号。
NAME_RE = re.compile(r"^[^\W\d]\w*$")
PKG_RE = re.compile(r"^\./[\w./-]+/?$")
# 一句话的攻击/下一步描述最短要多长：`attack: "a"` 形状合规而内容全无
MIN_PROSE = 12
# 反向的下界：`quote` 只是**指针**（§2 字段表：抄"该段的前若干字"）。没有上界时，
# 把整段承诺正文抄进台账照样绿——那正是 §0.1 要治的"两份事实源"：spec 一改，
# 台账里那份副本就开始说谎，而门的 quote 判据反而更"匹配"（越长越像原文）。
MAX_QUOTE = 96
# 指针的下界：短到这个数以下，"是某条的连续原文"几乎必然自动成立（一节几十条共用动词开头），
# 于是长度判据在、点名能力不在。取与 attack/expect/repro 同一个"太短＝没写"口径。
MIN_QUOTE = MIN_PROSE
SHAPES = ("bullet", "numbered", "tablerow", "prose-not-counted")
LIST_CACHE: dict[str, tuple[list[str] | None, str]] = {}
SPEC_CACHE: dict[tuple[str, int], tuple[list[str], list[tuple[str, str, int, int]]]] = {}


# ---------------------------------------------------------------- 声明式路径

def in_repo(rel, base: str) -> tuple[Path | None, str]:
    """台账里写死的每一条路径都过这道：绝对路径、`..`、临时目录各是一种逃逸。

    门的信任边界就是"仓内相对路径"——`meta.spec` 可以给成仓外任意一份同名文本，
    那等于让被审的对象自己挑选审的是哪份原文；日志给成 `/tmp/**` 则今天绿、重启后无人能复核。
    判"临时目录"只看**仓外**的路径：门本身常在 `--shared` 克隆里跑，克隆可能就住在 /tmp，
    若按字符串前缀判，那份克隆里的每一条合法证据都会被误判成逃逸。
    """
    if not isinstance(rel, str) or not rel.strip():
        return None, f"{base} 为空——写个占位串不算证据"
    if ".." in Path(rel).parts:
        return None, f"{base}={rel!r} 含 `..`，解析后跑出仓外"
    if rel.startswith(("/", "~")):
        if rel.startswith(EPHEMERAL_PREFIXES):
            return None, f"{base}={rel!r} 落在临时目录——重启后没人能复核，改填仓内路径"
        return None, f"{base}={rel!r} 是仓外绝对路径：门只能审仓内文本"
    resolved = (ROOT / rel).resolve()
    try:
        resolved.relative_to(ROOT.resolve())
    except ValueError:
        if str(resolved).startswith(EPHEMERAL_PREFIXES):
            return None, f"{base}={rel!r} 经符号链接落到临时目录（{resolved}）"
        return None, f"{base}={rel!r} 经符号链接解析到仓外（{resolved}）"
    return resolved, ""


# ---------------------------------------------------------------- 台账读取

def load_ledger(path: Path) -> tuple[dict, list[dict]]:
    meta, items = None, []
    for ln, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError as e:
            raise SystemExit(f"{path}:{ln} 不是合法 JSON：{e}")
        if obj.get("kind") == "meta":
            if meta:
                raise SystemExit(f"{path}:{ln} 出现第二行 meta")
            meta = obj
        else:
            obj["_line"] = ln
            items.append(obj)
    if not meta:
        raise SystemExit(f"{path} 缺第一行 meta（round / spec / scope 都在里面）")
    return meta, items


# ---------------------------------------------------------------- 抽取与计数钉

def blocks(lines: list[str]) -> list[tuple[str, str, int, int]]:
    """返回 (所属 ## 节, 节标题, 起, 止)。止 = 下一个同级或更高级标题前一行。"""
    heads = [(i, re.match(r"^(#{2,6})\s+(.*)$", l)) for i, l in enumerate(lines)]
    heads = [(i, len(m.group(1)), m.group(2)) for i, m in heads if m]
    out, section = [], None
    for k, (i, level, title) in enumerate(heads):
        nxt = len(lines)
        for j, lvl2, _ in heads[k + 1:]:
            if lvl2 <= level:
                nxt = j
                break
        if level == 2:
            section = title
            out.append((title, title, i + 1, nxt))
        else:
            out.append((section if section else title, title, i + 1, nxt))
    return out


def count_shape(lines: list[str], lo: int, hi: int, shape: str) -> int:
    body = lines[lo:hi]
    if shape == "bullet":
        # `-`、`*`、`+` 三种 marker 在 markdown 里同义；只认 `- ` 会让"新承诺用 * 起头"
        # 躲过计数钉（抽取口径窄于承诺面＝钉了个自证的数）。
        return sum(1 for l in body if re.match(r"^[-*+] \S", l))
    if shape == "numbered":
        return sum(1 for l in body if re.match(r"^\d+[.)] \S", l))
    if shape == "tablerow":
        # 一张 Markdown 表 = 表头行 + 分隔行 + 数据行。分隔行的每个格子只可能是 -、:、空格，
        # 所以"整行仅由 | - : 空格 组成"唯一地认出它；扣两张（表头 + 分隔）才是数据行。
        pipe = [l for l in body if l.startswith("|")]
        seps = [l for l in pipe if "-" in l and re.fullmatch(r"\|[ :|-]+\|", l.strip())]
        return len(pipe) - 2 * len(seps)
    raise SystemExit(f"未知形状 {shape}（只支持 {'/'.join(SHAPES)}）")


def read_spec(spec: Path) -> tuple[list[str], list[tuple[str, str, int, int]]]:
    # --dump / 抽钉值 / 条目指针三条路径必须走同一个读法与同一份缓存，
    # 否则"写钉值时数出来的"和"关门时数出来的"可以不是同一份文本。
    key = (str(spec), spec.stat().st_mtime_ns)
    if key not in SPEC_CACHE:
        lines = spec.read_text(encoding="utf-8").splitlines()
        SPEC_CACHE[key] = (lines, blocks(lines))
    return SPEC_CACHE[key]


def norm(s: str) -> str:
    """markdown 里同一句话会被折行/加空格，比对时抹掉所有空白，否则引用越像越容易假红。"""
    return re.sub(r"\s+", "", s)


ENTRY_START = re.compile(r"^(?:[-*+] |\d+[.)] |#{2,6} |\||> )")


def section_entries(lines: list[str], bs: list[tuple], sec: str) -> list[str]:
    """把该节正文切成"条目级"片段：一个列表项 / 一行表 / 一个标题下的段落各算一片。

    quote 只能在**同一片内**做子串匹配。跨片匹配是把两句话中间的空格抹掉后拼成一句，
    看着像原文逐字引用，其实原文档里从没这样说过（B3 反例：整节 join 后跨条拼接能过关）。
    """
    out = []
    for s, h, lo, hi in bs:
        if s != sec and h != sec:
            continue
        cur = []
        for l in lines[lo:hi]:
            if ENTRY_START.match(l):
                if cur:
                    out.append(norm("".join(cur)))
                cur = [l]
            elif not l.strip():
                if cur:
                    out.append(norm("".join(cur)))
                    cur = []
            else:
                cur.append(l)
        if cur:
            out.append(norm("".join(cur)))
    return [e for e in out if e]


def sections(lines: list[str], bs: list[tuple]) -> set[str]:
    return {s for s, _, _, _ in bs} | {h for _, h, _, _ in bs}


COUNTED = ("bullet", "numbered", "tablerow")


def own_segments(lines: list[str], bs: list[tuple]) -> list[tuple[str, str, int, int]]:
    """把 blocks() 的区间切成互不重叠的"某个标题自己那一段"（到下一个任意级标题前为止）。

    ## 节的区间天然包住它所有子节；直接按整节做形状计数会把同一条承诺数两遍
    （父格数一遍、子格再数一遍），覆盖率钉就成了一个没人能对得上的数。
    """
    out = []
    for k, (s, h, lo, hi) in enumerate(bs):
        nxt = bs[k + 1][2] - 1 if k + 1 < len(bs) else hi  # 下一标题所在行
        out.append((s, h, lo, min(nxt, hi)))
    return out


def coverage_totals(lines: list[str], segs: list[tuple]) -> dict[str, int]:
    """每个 ## 节的"可数承诺"总数（三种形状相加，每条只数一次）。"""
    tot: dict[str, int] = {}
    for s, _, lo, hi in segs:
        tot[s] = tot.get(s, 0) + sum(count_shape(lines, lo, hi, sh) for sh in COUNTED)
    return tot


def check_claim(it: dict, lines: list[str], bs: list[tuple]) -> tuple[list[str], str]:
    """承诺指针要有牙：section 点得到本仓 spec 里的节，quote 是那一节里某个条目的连续原文。
    只查"字段非空"＝允许条目引用一条根本不存在的承诺，台账就成了自说自话的地方。
    返回 (错误, 该条目归属的 ## 节标题)，后者用来做"钉 ↔ 条目"对账。"""
    claim = it.get("claim") or {}
    sec = (claim.get("section") or "").strip().lstrip("#").strip()
    quote = claim.get("quote") or ""
    line = f"第 {it['_line']} 行 id={it.get('id', '?')}"
    key = norm(quote)
    if not key:
        return [f"{line}：claim.quote 要抄一句原文，不能留空占位"], ""
    if len(key) > MAX_QUOTE:
        return [f"{line}：claim.quote 有 {len(key)} 字（>上限 {MAX_QUOTE}）——台账只存指针，"
                f"抄正文会让 spec 与台账变成两份事实源（spec 一改，副本就开始说谎）"], ""
    if len(key) < MIN_QUOTE:
        # 上界有牙、下界没牙是本轮反向测逼出来的形状：一条 3 个字的"指针"在同一节的条目里几乎必然
        # 撞车（本仓一节动辄几十条，共用「把」「改成」开头），于是"quote 必须是某条的连续原文"
        # 这条判据自动成立，却谁也没点名。下界取 MIN_PROSE：与 attack/expect/repro 同一个"太短＝没写"口径。
        return [f"{line}：claim.quote 只有 {len(key)} 字（<下界 {MIN_QUOTE}）——"
                f"这么短的串在同一节里几乎必然撞车，等于没点名是哪条承诺"], ""
    if not sec:
        return [f"{line}：claim.section 为空，指针没对准任何一节"], ""
    if sec not in sections(lines, bs):
        return [f"{line}：claim.section={sec!r} 在 spec 里点不到任何节（改了名或已删，指针是空的）"], ""
    entries = section_entries(lines, bs, sec)
    if not any(key in e for e in entries):
        return [f"{line}：claim.quote 在 {sec!r} 一节的任一条目里都找不到连续原文"
                f"（承诺被改写、已删，或是跨两条拼出来的）"], ""
    tops = {b[0] for b in bs if b[0] == sec or b[1] == sec}
    # 同名节（本仓实测：`### 八、登记（看过、本批不做）` 在 §7.24 与 §7.28 各一份）会让指针**看着有效**
    # 却落错账：section_entries 把两份的条目并成一个池子，所以 A 节的原文能在 B 节名下"匹配成功"，
    # 而对账按 ## 节统计条目数 ⇒ B 节的钉永远 0 条、A 节白捡 N 条。指针必须自己消歧。
    if len(tops) > 1:
        return [f"{line}：claim.section={sec!r} 同时落在 {len(tops)} 个批次节里"
                f"（{' ｜ '.join(sorted(tops)[:2])}）——同名歧义节：quote 从哪一份切的、条目算在哪一节头上，"
                f"门都分不清。改填所属 ## 批次节的完整标题"], ""
    return [], (tops.pop() if tops else sec)


# ---------------------------------------------------------------- 条目判据

def prose(it: dict, field: str, line: str) -> list[str]:
    v = it.get(field)
    if not isinstance(v, str) or not v.strip():
        return [f"{line}：缺 {field}（这条承诺被什么注码破坏、预言哪条腿红）" if field == "attack"
                else f"{line}：{field} 为空"]
    if len(v.strip()) < MIN_PROSE:
        return [f"{line}：{field} 只有 {len(v.strip())} 字（<{MIN_PROSE}），"
                f"形状合规而内容全无：{v!r}"]
    return []


def check_script_leg(leg: dict, line: str, label: str) -> list[str]:
    """脚本腿 `{script, invocation, expect}`：给"守卫是一支脚本/一道门"的改动用。

    只有 Go 腿可写的话，门禁脚本与文档这类修复就只能记成 blocked——台账会变成 Go 专用的，
    而本泳道一半的交付物不住在 Go 里。判据与 Go 腿同强度：文件必须在仓内、
    命令必须真指向那个文件（写一条不相干的命令＝点了个不存在的守卫）、expect 要写判定看哪一行。
    """
    errs = []
    sp, err = in_repo(leg.get("script"), f"{label}.script")
    if err:
        return [f"{line}：{err}"]
    if not sp.is_file():
        errs.append(f"{line}：{label} 的守卫脚本不存在：{leg.get('script')}")
    inv = leg.get("invocation")
    if not isinstance(inv, str) or len(inv.strip()) < 6:
        errs.append(f"{line}：{label}.invocation 要写整条可重跑命令（got {inv!r}）")
    elif (rel := (leg.get("script") or "").strip()) not in inv:
        errs.append(f"{line}：{label}.invocation 里没有出现 {rel!r}——命令指的不是这个守卫，"
                    f"照它跑不到任何红")
    exp = leg.get("expect")
    if not isinstance(exp, str) or len(exp.strip()) < MIN_PROSE:
        errs.append(f"{line}：{label}.expect 要写「判定看哪一行」（至少 {MIN_PROSE} 字，got {exp!r}）")
    return errs


def check_legs(legs, line: str, label: str) -> list[str]:
    """label 只用于措辞：fixed 的 legs[] 与 locked-equivalence 的 lock_leg 走同一套形状判据。

    两种合法形状：{pkg, name}（Go 用例，`go test -list` 点得到）与
    {script, invocation, expect}（守卫是脚本/门）。
    """
    errs = []
    for leg in legs:
        if not isinstance(leg, dict):
            errs.append(f"{line}：{label} 每项要是 {{pkg, name}} 或 "
                        f"{{script, invocation, expect}} 对象")
            continue
        if leg.get("script"):
            errs += check_script_leg(leg, line, label)
            continue
        if leg.get("pkg") and not leg.get("name"):
            errs.append(f"{line}：{label} 带了 pkg 却没 name（只有目录名的腿点不到具体用例）")
            continue
        pkg, name = leg.get("pkg"), leg.get("name")
        if not pkg or not name:
            errs.append(f"{line}：{label} 每项要带 pkg 与 name")
            continue
        if not PKG_RE.match(pkg) or ".." in pkg:
            errs.append(f"{line}：{label} 的 pkg={pkg!r} 不是 `./` 开头的仓内包路径"
                        f"（越界的 pkg 会让 go test 在别的目录里跑）")
            continue
        if not NAME_RE.match(name):
            errs.append(f"{line}：{label} 的 name={name!r} 不是合法 Go 用例名"
                        f"（正则式名字能在 go test -list 里命中一堆，却不是点得到的那一条腿）")
            continue
        names, why = pkg_names(pkg)
        if names is None:
            errs.append(f"{line}：{label} {name} 无法点验——包 {pkg} 当前编译不过（{why}）。"
                        f"这是**跑门的这棵树**的状态，不是这条腿不存在：等该包能编译再复跑，"
                        f"或按本仓惯例换一棵只含已提交内容的私有克隆再跑")
        elif name not in names:
            errs.append(f"{line}：{label} {name} 在 {pkg} 里 go test -list 点不到")
    return errs


def pkg_names(pkg: str) -> tuple[list[str] | None, str]:
    """一次 `go test -list '.*'` 取全包用例名；返回 (名单，或 None=该包编译不过 + 错因首行)。

    不逐腿跑有两层理由：一是同包 N 条腿只编译一次；二是**归因**——并行泳道正在同一包里写新
    文件时该包会短暂编译不过，那时"腿点不到"与"树暂时坏着"是两件事。把两者并成一格红，
    会让人去改一份根本没错的台账（本轮实测：service 包被另一泳道的 payment 系列未跟踪文件
    连着两次撞断，手工 `-list` 单验那两条腿都点得到）。
    编译不过**仍然算红**（rc≠0，不放过任何一格），只是红因写成"跑门的这棵树的状态"，
    要改的是那棵树而不是台账。
    """
    if pkg not in LIST_CACHE:
        r = subprocess.run(["go", "test", pkg, "-list", ".*"],
                           cwd=ROOT / US, capture_output=True, text=True, timeout=900)
        if r.returncode != 0:
            why = next((l.strip() for l in (r.stdout + r.stderr).splitlines() if l.strip()), pkg)
            LIST_CACHE[pkg] = (None, why[-160:])
        else:
            LIST_CACHE[pkg] = ([l.strip() for l in r.stdout.splitlines()
                                if l.strip() and not l.startswith(("ok ", "? "))], "")
    return LIST_CACHE[pkg]


def log_content_asserts(item, line: str) -> tuple[str | None, list[str], list[str], list[str]]:
    """把 `logs[]` 的一项拆成 (路径, must_say, must_not_say, 形状红)。

    为什么允许字典项：一门只做**存在性**检查的证据，会被"名字对着、内容已经换了一趟跑"的文件
    骗过去——本轮实测撞到的形状是 `merge-gate-full.log` 被一次合法的 `--only` 子集调试原地覆盖，
    文件在、mtime 新、内容却不再证明它承诺的那件事。断言绑的是**读数本身**，覆盖即红。
    """
    if isinstance(item, str):
        return None, [], [], []
    if not isinstance(item, dict):
        return None, [], [], [f"{line}：logs 项要么是路径字符串，要么是带 path 的字典，got {type(item).__name__}"]
    path = item.get("path")
    if not isinstance(path, str) or not path.strip():
        return None, [], [], [f"{line}：logs 字典项的 path 要写清（哪份证据文件）"]
    says, nots = item.get("must_say") or [], item.get("must_not_say") or []
    if not isinstance(says, list) or not isinstance(nots, list) \
            or any(not isinstance(x, str) or not x for x in says + nots):
        return None, [], [], [f"{line}：must_say / must_not_say 要是非空字符串数组"]
    if not says and not nots:
        return None, [], [], [f"{line}：字典项至少给一条断言（只写 path ＝退回存在性检查，"
                              "那不如直接写字符串；断言才是它比字符串多出来的那部分）"]
    return path, says, nots, []


LOG_READ_CAP = 1 << 20

# 终端转义码（CSI …m 这一族，vitest/jest/go 的彩色汇总都用它）。
ANSI_RE = re.compile(r"\x1b\[[0-9;]*m")


def strip_ansi(s: str) -> str:
    """判读前先脱色：断言写给**人眼在那份日志里读到的那句话**，而不是写给字节流里的转义码。

    不脱色的两种失效都不体面，而且都朝绿偏：`must_say` 凑不齐（这条至少吵，看得见），
    `must_not_say` 永远看不见（把"红在："写进彩色输出的末段汇总，门照样报绿）。
    反过来会不会朝红偏？只有当有人把断言**连转义码一起抄**时才会——那是抄本，不是读数；
    台账侧的钉全部由生成器从脱色后的窗口现取（`build-review-ledger.evidence_pin`），不走手抄这条路。
    """
    return ANSI_RE.sub("", s)


def log_window(p: Path) -> str:
    """证据日志的判读窗口＝头 LOG_READ_CAP ＋ 尾 LOG_READ_CAP 字节（小于两倍的件＝整读）。

    为什么不能只读头（本轮实测撞到的形状）：整包测试/全量门禁那份取证有 2.4MB，判定行
    （`gate_rc=0`、`--- FAIL: …`、末段汇总）全在尾巴上。只读头会出两种事：
    ① `must_say` 永远凑不齐——钉一条读不到的读数，门长红（吵，看得见）；
    ② 贵得多的一种：`must_not_say` 落在窗外时**静默看不见**。一条写在 1MB 之后的
       "不许出现"串（正是 `红在：` 这种末段汇总里的东西）根本进不了判读，
       于是门替一趟红读数背书、还报绿。
    中间那段仍然不读（判据只认头尾两段的出现与否），接缝处补一个换行，避免把
    头段的末字节和尾段的首字节粘成一条原文里没有的串。
    """
    b = p.read_bytes()
    if len(b) <= 2 * LOG_READ_CAP:
        return b.decode("utf-8", errors="replace")
    return (b[:LOG_READ_CAP] + b"\n" + b[-LOG_READ_CAP:]).decode("utf-8", errors="replace")


def check_evidence_paths(ev: dict, line: str, seen: str) -> list[str]:
    errs = []
    logs = ev.get("logs")
    if logs is not None and not isinstance(logs, list):
        return [f"{line}：logs 要是数组"]
    for raw in logs or []:
        lg, says, nots, shape = log_content_asserts(raw, line)
        if shape:
            errs += shape
            continue
        lg = raw if lg is None else lg
        p, err = in_repo(lg, "证据路径")
        if err:
            if "临时目录" in err:
                errs.append(f"{line}：证据 {lg} 在临时目录——重启后没人能复核，改填持久路径或 repro")
            else:
                errs.append(f"{line}：{err}")
            continue
        if not p.is_file():
            errs.append(f"{line}：证据文件不存在或不是文件：{lg}")
        elif seen and p.stat().st_mtime < dt.datetime.strptime(seen, DUE_FMT).timestamp():
            errs.append(f"{line}：证据 {lg} 比 first_seen({seen}) 还老，不是这条的读数")
        elif says or nots:
            body = strip_ansi(log_window(p))
            for pat in says:
                if pat not in body:
                    errs.append(f"{line}：证据 {lg} 里没有『{pat[:40]}』——文件在、却不再证明这条承诺"
                                "（要么被别的趟覆盖，要么这条承诺本来就没跑过）")
            for pat in nots:
                if pat in body:
                    errs.append(f"{line}：证据 {lg} 里出现了不许出现的『{pat[:40]}』")
    return errs


def check_repro(ev: dict, line: str) -> list[str]:
    rp = ev.get("repro")
    if rp is None:
        return []
    if not isinstance(rp, dict):
        return [f"{line}：repro 要是 {{cmd, expect}}——一句自由文本没人能重跑"]
    errs = []
    for f in ("cmd", "expect"):
        v = rp.get(f)
        if not isinstance(v, str) or len(v.strip()) < 6:
            errs.append(f"{line}：repro.{f} 要写清（命令本身 + 判定看哪一行），got {v!r}")
    return errs


def check_item(it: dict, today: dt.date) -> list[str]:
    errs = []
    line = f"第 {it['_line']} 行 id={it.get('id', '?')}"
    st = it.get("status")
    if st not in STATUSES:
        return [f"{line}：status={st!r} 不在四终态 {STATUSES} 内（没有'待办'这个格子）"]
    ev = it.get("evidence") or {}
    if not (it.get("claim") or {}).get("section") or not (it.get("claim") or {}).get("quote"):
        errs.append(f"{line}：claim 要带 {{section, quote}} 两个指针对准原文")
    if not it.get("attack"):
        errs.append(f"{line}：缺 attack（这条承诺被什么注码破坏、预言哪条腿红）")
    else:
        errs += prose(it, "attack", line)

    seen = it.get("first_seen")
    # first_seen 不可缺：证据的 mtime 界、"这条什么时候开始算遗留"都靠它。缺了它，
    # 一份 2020 年的旧日志也能当"本轮复验证据"。
    if not seen:
        errs.append(f"{line}：缺 first_seen（YYYY-MM-DD，发现首次出现的日期）")
    else:
        try:
            if dt.datetime.strptime(seen, DUE_FMT).date() > today:
                errs.append(f"{line}：first_seen={seen} 在未来（{today}）")
        except ValueError:
            errs.append(f"{line}：first_seen={seen!r} 不是 {DUE_FMT}")

    if st == "fixed":
        errs += check_legs(ev.get("legs") or [], line, "legs")
        if not ev.get("legs"):
            errs.append(f"{line}：fixed 必须至少一条腿")
        errs += check_evidence_paths(ev, line, seen if is_date(seen) else "")
        errs += check_repro(ev, line)
        if not ev.get("logs") and not ev.get("repro"):
            errs.append(f"{line}：fixed 要么有 logs[]（仓内路径），要么有 repro={{cmd,expect}}")

    elif st == "refuted":
        rv = ev.get("reverify") or ""
        if not rv.strip():
            errs.append(f"{line}：refuted 必须写 reverify——刀怎么注的、看到什么读数")
        else:
            if not any(w in rv for w in ACTION_WORDS):
                errs.append(f"{line}：reverify 里没有动作词{ACTION_WORDS}，'我认为不成立'不是复验")
            if len(rv.strip()) < MIN_PROSE:
                errs.append(f"{line}：reverify 只有 {len(rv.strip())} 字（<{MIN_PROSE}）")

    elif st == "locked-equivalence":
        leg = ev.get("lock_leg")
        if not isinstance(leg, dict) or not leg.get("pkg") or not leg.get("name"):
            errs.append(f"{line}：locked-equivalence 要带 lock_leg={{pkg,name}}")
        else:
            errs += check_legs([leg], line, "lock_leg")
        if not ev.get("lock_attack"):
            errs.append(f"{line}：锁腿要配一条'行为不变'的注码证明它有牙（lock_attack）")
        else:
            errs += prose(ev, "lock_attack", line)

    elif st == "blocked":
        for f in ("owner", "due"):
            if not (ev.get(f) or "").strip():
                errs.append(f"{line}：blocked 缺 {f}（过期阻塞就是遗留，不能靠'是别人的事'存活）")
        if not (it.get("next") or "").strip():
            errs.append(f"{line}：blocked 缺 next（下一步要可执行，'以后再说'不算）")
        else:
            errs += prose({"next": it.get("next")}, "next", line)
        due = (ev.get("due") or "").strip()
        if due:
            try:
                if dt.datetime.strptime(due, DUE_FMT).date() < today:
                    errs.append(f"{line}：blocked 的 due={due} 已过期（{today}），本条算遗留未闭合")
            except ValueError:
                errs.append(f"{line}：due={due!r} 不是 {DUE_FMT}")
    return errs


def is_date(s) -> bool:
    try:
        dt.datetime.strptime(s or "", DUE_FMT)
        return True
    except (ValueError, TypeError):
        return False


# ---------------------------------------------------------------- source-map 与对账

def extract(spec: Path, scope) -> tuple[list[str], list[tuple[str, int]], dict]:
    """逐条核计数钉。返回 (错误, 展示用 [(节, 实抽数)], 已验钉的 {## 节: 承诺条数})。"""
    if not isinstance(scope, list) or not scope:
        return (["meta.scope 缺失或为空：没有 source-map，这门只会查条目形状，"
                 "永远不会发现「整节承诺没抽轴」"]), [], {}
    lines, bs = read_spec(spec)
    errs, got, verified = [], [], {}
    for p in scope:
        sec, head, shape = p.get("section"), p.get("heading"), p.get("shape")
        if shape not in SHAPES:
            errs.append(f"抽取规则形状非法：{shape!r}（只支持 {'/'.join(SHAPES)}）")
            continue
        total, hits = 0, 0
        for s, h, lo, hi in bs:
            if s == sec and fnmatch.fnmatch(h, head):
                if shape != "prose-not-counted":
                    total += count_shape(lines, lo, hi, shape)
                hits += 1
        if hits == 0:
            errs.append(f"抽取规则一条都没命中：section={sec!r} heading={head!r}"
                        f"（标题改名或被删，钉值成了摆设）")
            continue
        if hits > 1 and "*" not in head and "?" not in head:
            errs.append(f"抽取规则命中 {hits} 个同名节（{sec!r}/{head!r}）——"
                        f"键必须二级到批次小节，或用 glob 显式承认多份")
        if shape == "prose-not-counted":
            # 声明"这一节的承诺抽不出稳定计数"是允许的，但必须写下为什么；
            # 否则 prose-not-counted 就是绕过计数钉的后门（本型反例：整节 exempt 后长几条都没人管）。
            reason = (p.get("reason") or "").strip()
            if len(reason) < MIN_PROSE:
                errs.append(f"prose-not-counted 要带 reason 说明为什么抽不出可数的承诺"
                            f"（{sec}/{head}，got {reason!r}）")
            got.append((f"{sec} / {head} / prose-not-counted", 0))
            continue
        if total != p.get("count"):
            errs.append(f"计数钉不符：{sec} / {head} / {shape} 实抽 {total} 条，"
                        f"钉 {p.get('count')!r} 条（长出新承诺就补台账行，或改钉值并写理由）")
            got.append((f"{sec} / {head} / {shape}", total))
            continue
        got.append((f"{sec} / {head} / {shape}", total))
        verified[sec] = verified.get(sec, 0) + total
    return errs, got, verified


def reconcile(items: list[dict], item_secs: dict, verified: dict) -> list[str]:
    """每个**已验**的计数钉都要有台账条目兜住：钉说这一节有 N 条承诺，节内就得有 N 条抽过轴的条目。

    没有这一步，"抽轴"可以整体不做而门仍然全绿（本型反例：条目数 0 或全指向别的节 ⇒ 0 红）。
    """
    errs = []
    for sec, want in sorted(verified.items()):
        n = sum(1 for s in item_secs.values() if s == sec)
        if n < want:
            errs.append(f"节「{sec}」的计数钉已验 {want} 条承诺，台账里只有 {n} 条指向它"
                        f"——有承诺没抽轴（每条都要一行 {{claim, attack, status}}）")
    return errs


COV_STATUSES = ("ledger", "out-of-scope")


def check_coverage(lines: list[str], segs: list[tuple], cov) -> list[str]:
    """spec 里**每一个有可数承诺的 ## 节**都要在 meta.coverage 声明归属，且声明的总数对得上。

    治的病：计数钉只守已经声明过的节，没进 scope 的节长多少条承诺门照样全绿。本轮实测
    收口面（§八 + §6 + §8.3）之外还有 18 个带列表项/表格的节——不写成一格声明，
    就是给下一轮留一个"没人看守的目录"。
    out-of-scope 也钉总数：声明"这节不归本台账管"不等于"这节从此不许长东西"。
    """
    if not isinstance(cov, list):
        return [f"meta.coverage 要是列表（got {type(cov).__name__}）："
                f"每个有可数承诺的 ## 节都要声明它是抽进台账还是写明理由不抽"]
    tot = coverage_totals(lines, segs)
    errs: list[str] = []
    seen: dict[str, int] = {}
    declared: set[str] = set()
    for i, e in enumerate(cov):
        if not isinstance(e, dict):
            errs.append(f"meta.coverage 第 {i + 1} 项要是个对象（section/total/status/reason）")
            continue
        sec = (e.get("section") or "").strip()
        if not sec:
            errs.append(f"meta.coverage 第 {i + 1} 项缺 section")
            continue
        if sec in seen:
            errs.append(f"meta.coverage 里节「{sec}」声明了两次（第 {seen[sec]} 项与第 {i + 1} 项）"
                        f"——同一节有两个归属，闭合面就说不清按哪个算")
            continue
        seen[sec] = i + 1
        declared.add(sec)
        if sec not in tot:
            errs.append(f"meta.coverage 声明的节「{sec}」在 spec 里不存在（改了名或已删，声明成了摆设）")
            continue
        st = e.get("status")
        if st not in COV_STATUSES:
            errs.append(f"节「{sec}」的归属状态非法：{st!r}（只支持 {'/'.join(COV_STATUSES)}）")
        if st == "out-of-scope" and len((e.get("reason") or "").strip()) < MIN_PROSE:
            errs.append(f"节「{sec}」声明 out-of-scope 却不写理由（或短于 {MIN_PROSE} 字）："
                        f"不修要有代价，静默划出去等于盲区")
        got = tot[sec]
        if e.get("total") != got:
            errs.append(f"节「{sec}」承诺总数实抽 {got} 条，声明 {e.get('total')!r} 条——"
                        f"节里长了/删了承诺就要同步声明，别让门对着旧数自证")
    for sec, n in sorted(tot.items()):
        if n > 0 and sec not in declared:
            errs.append(f"节「{sec}」有 {n} 条可数承诺，meta.coverage 里没声明它的归属——"
                        f"未声明的节就是盲区：新增长出的承诺没有东西会红。"
                        f"要么进 scope 抽轴成台账条目，要么 out-of-scope 并写明理由")
    return errs


def check_ledger_pins(cov, verified: dict) -> list[str]:
    """coverage 里写 `ledger` 的节必须真有已验的计数钉，否则"进闭合面"是一句空话。

    判据 9 只保证"每个有可数承诺的节都声明了归属"，而抽轴对账（reconcile）走的是 **已验的钉**：
    一节可以先声明 `ledger`、再在 `meta.scope` 里漏掉它的钉 ⇒ verified 没这个键、reconcile 不看它，
    于是"这一节有几条承诺、台账抽了几条"永远不会红。声明与对账分在两个字段里，正是这一格要接上的缝。
    """
    if not isinstance(cov, list):
        return []
    errs = []
    for e in cov:
        if isinstance(e, dict) and (e.get("status") or "").strip() == "ledger":
            sec = (e.get("section") or "").strip()
            if sec and sec not in verified:
                errs.append(f"节「{sec}」在 coverage 里声明为 ledger（进闭合面），但 meta.scope 里"
                            f"没有它已验的计数钉——这一节有几条承诺、台账抽了几条轴，门一个字都不对账。"
                            f"要么补 scope 钉，要么改 out-of-scope 并写明理由")
    return errs


def run(ledger: Path, today: dt.date) -> tuple[list[str], dict]:
    errs: list[str] = []
    meta, items = load_ledger(ledger)
    spec, err = in_repo(meta.get("spec"), "meta.spec")
    if err:
        return [f"meta.spec：{err}"], {"round": meta.get("round", "?"), "items": len(items),
                                       "by_status": {}, "unstatused": 0, "pins": []}
    if not spec.is_file():
        return [f"meta.spec 指向的文件不存在：{meta.get('spec')}"], {"round": meta.get("round", "?"),
                                                                    "items": len(items),
                                                                    "by_status": {}, "unstatused": 0,
                                                                    "pins": []}
    errs, got, verified = extract(spec, meta.get("scope"))
    lines, bs = read_spec(spec)
    # scope 整体缺失时不再叠一条 coverage 红：那时 extract 已经报"没有 source-map"，
    # 两条同因的红只会让人以为门坏了两回。
    if meta.get("scope"):
        errs += check_coverage(lines, own_segments(lines, bs), meta.get("coverage"))
        errs += check_ledger_pins(meta.get("coverage"), verified)

    if not items:
        errs.append("台账一行条目都没有：空台账不等于「本轮没有发现」——"
                    "Phase A 没做就得在 meta 里写明依据，否则这关是白过的")
    seen_ids: dict[str, int] = {}
    item_secs: dict[int, str] = {}
    for it in items:
        cl_errs, sec = check_claim(it, lines, bs)
        item_secs[it["_line"]] = sec
        errs += cl_errs
        errs += check_item(it, today)
        iid = (it.get("id") or "").strip()
        if not iid:
            errs.append(f"第 {it['_line']} 行：缺 id（后续轮次要按 id 追这条有没有回归）")
        elif iid in seen_ids:
            errs.append(f"第 {it['_line']} 行：id={iid} 与第 {seen_ids[iid]} 行重复"
                        f"（两条不同的发现共用一个 id，闭合状态就说不清是哪条）")
        else:
            seen_ids[iid] = it["_line"]
    errs += reconcile(items, item_secs, verified)

    n = {s: sum(1 for i in items if i.get("status") == s) for s in STATUSES}
    other = sum(1 for i in items if i.get("status") not in STATUSES)
    return errs, {"round": meta.get("round", "?"), "items": len(items), "by_status": n,
                  "unstatused": other, "pins": got}


# ---------------------------------------------------------------- 门自己的反向测试

# 规格文本用**仓内**夹具而不是 tempdir：绝对路径本身就是这门要拦的一种逃逸，
# 自检若走绝对路径就等于用被测判据之外的通道喂输入。
FIXTURE_REL = "docs/superpowers/specs/ledger/fixtures/selftest-spec.md"
GOOD_LOG = "docs/superpowers/specs/ledger/logs/R22/control_repository.log"
# 「证据比 first_seen 还老」这一格要真造一份旧文件：拿仓里现成的文件当靶子，
# 会随别人 checkout 一次就时灵时不灵，所以自检自己写、自己删。
OLD_LOG_REL = ".selftest-old-mtime.log"
EPOCH = dt.datetime(2020, 1, 1).timestamp()
# 判读窗口这一族要一份**比两倍窗口还长**的取证：小文件等于整读，永远测不出
# "尾巴上的串进不了判读"这个形状（本轮实测：台账里 2.4MB 的那份整包门禁件，
# `gate_rc=0` 与全部 `--- FAIL` 都在 2.4MB 之后）。同 OLD_LOG_REL 的口径：自检自己写、自己删。
BIG_LOG_REL = ".selftest-window.log"
BIG_TAIL_SAY = "尾部判定：这行落在 2MB 窗口之外"
BIG_TAIL_NOT = "尾部禁串：这行落在 2MB 窗口之外"
# 彩色输出这一族：vitest/jest 的汇总行在**磁盘上**带 ANSI 转义，人眼里才是那句干净的话。
# 本轮实测——台账引用的 5 份前端取证里，`Test Files …` 全是
# `\x1b[2m Test Files \x1b[22m \x1b[31m1 failed\x1b[39m…` 这种形状，
# 拿干净串去整子串匹配必然不中：`must_say` 永远凑不齐（吵，看得见），
# `must_not_say` 永远看不见（静默放行，贵）。所以判读前先脱色。
ANSI_LOG_REL = ".selftest-ansi.log"
ANSI_SAY = "Test Files 1 failed"
ANSI_NOT = "Tests 3 failed"


def fixture_long_entry() -> str:
    """夹具 §6 里那条 >96 字的编号项全文，当"抄正文"的靶子。

    从夹具现读而不是在源码里手抄一份：夹具改一个字，这格的期望就悄悄对不上（那是
    "改期望不改变异"的反面）。读不到就直接停——这一格不能悄悄退化成正则匹配不到的空靶。
    """
    lines = (ROOT / FIXTURE_REL).read_text(encoding="utf-8").splitlines()
    for i, l in enumerate(lines):
        if not l.startswith("1. ") or "去重键缺会话维度" not in l:
            continue
        frag = [l]
        for nxt in lines[i + 1:]:
            if not nxt.strip() or ENTRY_START.match(nxt):
                break
            frag.append(nxt)
        entry = "\n".join(frag)
        if len(norm(entry)) <= MAX_QUOTE:
            raise SystemExit(f"夹具那条靶子短于上界 {MAX_QUOTE}，这一格证不了上界有牙")
        return entry
    raise SystemExit("夹具里找不到『去重键缺会话维度』那条靶子（改了名要同步改这里，别留一格空靶）")


def selftest(today: dt.date) -> int:
    """每种坏台账各造一条 ⇒ 各自隔离地红；再加一份全合法快照 ⇒ 0 红。缺任一侧都不算这门有牙。

    隔离的口径：一条坏形态只能踩它自己那一格。红了但混进别格的红＝这条判据其实没在管事
    （去掉它门照样红，谁也不会发现），所以 `stray` 非空即判没隔离。
    有些形态天然会连带触发对账（例如把 claim 改坏 ⇒ 该节条目数掉一条），这类要在 meta_over
    里把计数钉换成不参与 verified 的形状，让"只该红的那一格"真的只红那一格。
    """
    if not (ROOT / FIXTURE_REL).exists():
        print(f"selftest 缺仓内夹具 {FIXTURE_REL}（门的路径判据不接受仓外 spec）")
        return 2
    if not (ROOT / GOOD_LOG).is_file():
        print(f"selftest 需要一个仓内存在的日志当合法证据；缺 {GOOD_LOG}")
        return 2
    sec6 = "6. 自检节（本批不修）"
    sec7 = "7. 只有散文的一节"
    # 取证首尾两行由门自己印（之前那两行 `###` 是手写的，其中"台账已是第二遍那版"这句
    # 根本不是我这一趟能测的东西——`--selftest` 只吃合成夹具，不读任何台账）。
    print(f"### {GATE_REL} --selftest 读数；测量 "
          f"{dt.datetime.now().astimezone():%Y-%m-%dT%H:%M:%S%z}")
    print()
    print(f"$ python3 {GATE_REL} --selftest")
    pin = {"section": sec6, "heading": sec6, "shape": "numbered", "count": 1, "note": "自检钉"}
    prose_pin = {"section": sec7, "heading": sec7, "shape": "prose-not-counted",
                 "reason": "这一节的承诺写在整段散文里，没有可数的条目形状"}
    # 覆盖率声明：sec6 有 2 条可数承诺（1 numbered + 1 bullet）抽轴进台账；sec7 是 0 条，不必声明。
    cov6 = {"section": sec6, "total": 2, "status": "ledger"}
    oos7 = {"section": sec7, "total": 0, "status": "out-of-scope",
            "reason": "只有散文，没有可数承诺，抽不出条目级指针"}
    # 去掉 sec6 计数钉的那几格（claim 坏 ⇒ 条目数掉一条，本来就会连带触发对账）要同时把
    # 它的归属改成 out-of-scope：否则新加的"声明 ledger 却没有计数钉"会把它们全带红（实测 8 格混红）。
    cov6_oos = {"section": sec6, "total": 2, "status": "out-of-scope",
                "reason": "自检夹具里这一节只用来喂指针判据，不当闭合面对账的靶子"}
    meta = {"kind": "meta", "round": "SELFTEST", "spec": FIXTURE_REL,
            "scope": [pin, prose_pin], "coverage": [cov6, oos7]}
    claim6 = {"section": sec6, "quote": "去重键缺会话维度时，回填会命中同一条官方消息的另一会话行"}
    claim7 = {"section": sec7, "quote": "这一节的承诺写在整段散文里，抽不出稳定的条目形状，"
                                        "所以只能按 prose-not-counted 声明并写明理由。"}
    base = {"kind": "item", "id": "ST-OK", "claim": claim6,
            "attack": "把 conversation_id 从回填的 WHERE 里去掉",
            "status": "fixed", "first_seen": "2026-09-01",
            "evidence": {"legs": [{"pkg": "./internal/repository/",
                                   "name": "TestSetInboundMediaURLsScopesByConversation"}],
                         "logs": [GOOD_LOG]}}
    assert len(base["attack"]) >= MIN_PROSE

    def mk(**over):
        it = json.loads(json.dumps(base))
        it.update(over)
        return it

    def wo(*keys):
        return {k: v for k, v in json.loads(json.dumps(base)).items() if k not in keys}

    def ev(**over):
        e = json.loads(json.dumps(base["evidence"]))
        e.update(over)
        return e

    leg = base["evidence"]["legs"][0]["name"]
    # 脚本腿的正例：这门自己就是"守卫不住在 Go 里"的那一类改动，用它当靶子最省。
    sleg = {"script": GATE_REL, "invocation": f"python3 {GATE_REL} --selftest",
            "expect": "种坏形态各自隔离地红，合法快照"}
    # 每条 = (名字, 台账条目列表, meta 覆盖, 期望红因族：第一项必须出现，其余是这一格一次报全的允许项)
    UNC = ("go test -list 点不到",)
    bad: list[tuple[str, list, dict, tuple]] = [
        ("缺 status", [wo("status")], {}, ("不在四终态",)),
        ("非法 status", [mk(status="wontfix")], {}, ("不在四终态",)),
        ("缺 id", [wo("id")], {}, ("缺 id",)),
        ("重复 id", [mk(), mk()], {}, ("id=ST-OK 与第",)),
        ("腿名点不到", [mk(evidence=ev(legs=[{"pkg": "./internal/repository/",
                                       "name": "TestNoSuchLegAtAll"}]))], {}, UNC),
        # 这一格打的是 `pkg_names` 那条**新分叉**：包跑不出名单时红因要写成"这棵树的状态"，
        # 而不是把"腿不存在"与"树暂时编译不过"并成一格（本轮实测：并行泳道的未跟踪文件
        # 连着两次把 ./internal/service/ 撞断，台账一字未动却红两条）。
        # 用一个不存在的包当靶子：它同样走 rc≠0 那条支，红因由 go 自己那行给出。
        ("腿所在包跑不出名单", [mk(evidence=ev(legs=[
            {"pkg": "./internal/selftest_nocompile_pkg/", "name": leg}]))], {},
         ("无法点验", "编译不过")),
        ("腿名是正则", [mk(evidence=ev(legs=[{"pkg": "./internal/repository/",
                                      "name": "TestSet.*"}]))], {}, ("不是合法 Go 用例名",)),
        ("pkg 越界", [mk(evidence=ev(legs=[{"pkg": "../../../etc/", "name": leg}]))], {},
         ("不是 `./` 开头",)),
        ("带 pkg 无 name", [mk(evidence=ev(legs=[{"pkg": "./internal/repository/"}]))], {},
         ("带了 pkg 却没 name",)),
        ("守卫脚本不存在", [mk(evidence=ev(legs=[dict(
            sleg, script="scripts/nope-guard.py",
            invocation="python3 scripts/nope-guard.py --selftest")]))], {}, ("守卫脚本不存在",)),
        ("invocation 指不到脚本", [mk(evidence=ev(legs=[dict(
            sleg, invocation="python3 scripts/other-thing.py --selftest")]))], {}, ("没有出现",)),
        ("脚本腿 expect 太短", [mk(evidence=ev(legs=[dict(sleg, expect="红")]))], {},
         ("expect 要写",)),
        ("日志不存在", [mk(evidence=ev(logs=[
            "docs/superpowers/specs/ledger/logs/R22/nope.log"]))], {}, ("不存在或不是文件",)),
        ("日志早于 first_seen", [mk(evidence=ev(logs=[OLD_LOG_REL]))], {}, ("还老",)),
        ("first_seen 在未来", [mk(first_seen="2030-01-01")], {}, ("在未来", "还老")),
        ("日志在 /tmp", [mk(evidence=ev(logs=["/tmp/whatever.log"]))], {}, ("临时目录",)),
        ("日志用 .. 逃逸", [mk(evidence=ev(logs=["../outside.log"]))], {}, ("含 `..`",)),
        ("日志是空串", [mk(evidence=ev(logs=[""]))], {}, ("为空",)),
        # 下面这族钉的是**内容断言**（本轮加）：只做存在性检查的证据会被"文件名对着、内容换了
        # 一趟跑"骗过去——实测形状是 merge-gate 的 `--only` 子集调试把台账引用的全量取证
        # 原地覆盖，文件在、mtime 更新，门照样绿。断言绑读数本身，覆盖即红。
        ("日志断言没命中", [mk(evidence=ev(logs=[
            {"path": GOOD_LOG, "must_say": ["这句话那份日志里绝对没有"]}]))], {}, ("里没有『",)),
        ("日志出现不许出现的串", [mk(evidence=ev(logs=[
            {"path": GOOD_LOG, "must_not_say": ["USER_JWT_SECRET"]}]))], {}, ("不许出现的",)),
        # 上一条用的是小文件（整读），这一条钉的是**大文件的尾巴**：窗口一旦退化成"只读文件头"，
        # 禁串就永远进不了判读，门会替一趟写着"红在："的读数报绿——静默失效，比假红贵。
        ("禁串只在尾部", [mk(evidence=ev(logs=[
            {"path": BIG_LOG_REL, "must_not_say": [BIG_TAIL_NOT]}]))], {}, ("不许出现的",)),
        # 同一条"静默放行"的失效，换一个遮挡源：串在文件里，只是被转义码劈开了。
        # 不脱色 ⇒ 这一格判不出红（门看不见禁串），而"禁串只在尾部"那格照样红——
        # 两格分开钉，才不会把"窗口退化"与"没脱色"两件事混成一个读数。
        ("禁串被 ANSI 遮挡", [mk(evidence=ev(logs=[
            {"path": ANSI_LOG_REL, "must_not_say": [ANSI_NOT]}]))], {}, ("不许出现的",)),
        ("logs 字典缺 path", [mk(evidence=ev(logs=[{"must_say": ["PASS"]}]))], {}, ("path 要写清",)),
        ("logs 字典无断言", [mk(evidence=ev(logs=[{"path": GOOD_LOG}]))], {}, ("至少给一条断言",)),
        ("must_say 不是数组", [mk(evidence=ev(logs=[
            {"path": GOOD_LOG, "must_say": "PASS"}]))], {}, ("要是非空字符串数组",)),
        ("logs 项是数字", [mk(evidence=ev(logs=[7]))], {}, ("要么是路径字符串",)),
        ("缺 first_seen", [wo("first_seen")], {}, ("缺 first_seen",)),
        ("attack 只有一字", [mk(attack="a")], {}, ("只有 1 字",)),
        ("缺 attack", [wo("attack")], {}, ("缺 attack",)),
        ("repro 自由文本", [mk(evidence=ev(repro="我已经看过了"))], {}, ("{cmd, expect}",)),
        ("repro 缺 expect", [mk(evidence=ev(repro={"cmd": "python3 scripts/x.py",
                                               "expect": ""}))], {}, ("repro.expect",)),
        ("spec 绝对路径", [mk()], {"spec": "/etc/hosts"}, ("meta.spec", "仓外绝对路径")),
        ("spec 指不到文件", [mk()], {"spec": FIXTURE_REL + ".nope"}, ("指向的文件不存在",)),
        ("无 scope", [mk()], {"scope": []}, ("meta.scope",)),
        ("空台账", [], {"scope": [prose_pin], "coverage": [cov6_oos, oos7]}, ("一行条目都没有",)),
        # 钉自己坏掉的两格天然带两条红：钉没验成 ⇒ 该节"没有已验的计数钉"，
        # 与新增的 ledger-无钉判据同因，所以收进 allow 项而不是假装它只该响一次。
        ("钉的节名点不到", [mk()], {"scope": [dict(pin, heading="根本没这一节"), prose_pin]},
         ("一条都没命中", "没有它已验的计数钉")),
        ("计数钉不符", [mk()], {"scope": [dict(pin, count=3), prose_pin]},
         ("计数钉不符", "没有它已验的计数钉")),
        ("钉已验但没抽轴", [mk(claim=claim7)], {}, ("没抽轴",)),
        ("prose 豁免无 reason", [mk()],
         {"scope": [pin, {k: v for k, v in prose_pin.items() if k != "reason"}]},
         ("prose-not-counted 要带 reason",)),
        ("等价类无锁腿", [mk(status="locked-equivalence", evidence={})], {},
         ("lock_leg", "lock_attack")),
        ("等价类锁腿点不到", [mk(status="locked-equivalence", first_seen="2026-09-01",
                            evidence={"lock_leg": {"pkg": "./internal/repository/",
                                                   "name": "TestEquivalentLegNeverWritten"},
                                      "lock_attack": "注码后行为不变，只有这条锁腿红"})], {}, UNC),
        ("blocked 已过期", [mk(status="blocked", next="接上真机后复跑这条腿并回读日志",
                           evidence={"owner": "用户", "due": "2020-01-01"})], {}, ("已过期",)),
        ("blocked 缺 next", [mk(status="blocked",
                            evidence={"owner": "用户", "due": "2026-12-31"})], {}, ("缺 next",)),
        ("blocked 缺 owner/due", [mk(status="blocked", next="接上真机后复跑这条腿并回读日志",
                                 evidence={})], {}, ("缺 owner", "缺 due")),
        ("refuted 无动作词", [mk(status="refuted", evidence={"reverify": "我认为这条不成立"})],
         {}, ("动作词", "reverify 只有")),
        ("refuted 空 reverify", [mk(status="refuted", evidence={})], {}, ("必须写 reverify",)),
        # 靶子原来只有 8 字，加了下界之后这一格红的是"太短"而不是"非原文"——两格撞在同一支上，
        # 等于"非原文"那条判据没人测了。把靶子加长到既过下界、又确实不在 spec 里。
        ("quote 非原文", [mk(claim={"section": sec6, "quote": "第九项是编出来的，spec 里根本没有这一句承诺"})],
         {"scope": [prose_pin], "coverage": [cov6_oos, oos7]}, ("找不到连续原文",)),
        ("quote 跨条拼接", [mk(claim={"section": sec6,
                                 "quote": "回填会命中同一条官方消息的另一会话行\n"
                                          "- 第二条只是用来做跨条目拼接的靶子"})],
         {"scope": [prose_pin], "coverage": [cov6_oos, oos7]}, ("找不到连续原文",)),
        ("section 点不到节", [mk(claim={"section": "9. 不存在的小节", "quote": claim6["quote"]})],
         {"scope": [prose_pin], "coverage": [cov6_oos, oos7]}, ("点不到任何节",)),
        # 两份同名小节（夹具 §8/§9 各一份 `### 附、两份同名的小节`）：quote 在两份里都找得到，
        # 但对账按 ## 节统计 ⇒ 不早停的话，A 节的原文会被记到 B 节账上（本仓真 spec 实测错过一次）。
        ("section 命中两份同名节",
         [mk(claim={"section": "附、两份同名的小节", "quote": "指针对到 A 节时该算进 A 节的账"})],
         {"scope": [prose_pin], "coverage": [cov6_oos, oos7]}, ("同时落在",)),
        # 指针上界这一格用夹具 §6 那条 bullet 的**全文**当 quote：它是合法原文、单条内连续，
        # 旧版门对它完全放行——"不复制正文"这条承诺当时没有执行者。
        ("quote 抄整段正文", [mk(claim={"section": sec6, "quote": fixture_long_entry()})],
         {"scope": [prose_pin], "coverage": [cov6_oos, oos7]}, ("只存指针",)),
        ("quote 为空", [mk(claim={"section": sec6, "quote": ""})], {"scope": [prose_pin], "coverage": [cov6_oos, oos7]},
         ("claim.quote 要抄一句原文", "claim 要带")),
        # 下界这一格与上一格（上界）成对：短到 4 字的串在同一节里几乎必然撞车，
        # "是某条的连续原文"会自动成立 ⇒ 指针判据看着过了，其实谁也没点名。
        # 放在"能不能匹配上原文"之前测，否则这一格红的是匹配、不是长度（绑不住开火的支路）。
        ("quote 只有 4 字", [mk(claim={"section": sec6, "quote": "去重键缺"})],
         {"scope": [prose_pin], "coverage": [cov6_oos, oos7]}, ("只有 4 字",)),
        ("节未声明归属", [mk()], {"coverage": [oos7]}, ("没声明它的归属",)),
        ("声明的总数不符", [mk()], {"coverage": [dict(cov6, total=9), oos7]}, ("承诺总数实抽",)),
        ("声明不存在的节", [mk()], {"coverage": [cov6, dict(oos7, section="9. 没这一节")]},
         ("不存在",)),
        ("归属状态非法", [mk()], {"coverage": [dict(cov6, status="maybe"), oos7]},
         ("归属状态非法",)),
        ("重复声明同一节", [mk()], {"coverage": [cov6, dict(cov6), oos7]}, ("声明了两次",)),
        # coverage 与 scope 是两个字段：这一格打的是"声明进闭合面、却没给计数钉"这条缝
        # （去掉 sec6 的钉 ⇒ verified 空 ⇒ 原本的 reconcile 根本不看这一节，抽轴可以整体不做而门全绿）。
        ("声明 ledger 却没有计数钉", [mk()],
         {"scope": [prose_pin], "coverage": [cov6, oos7]}, ("没有它已验的计数钉",)),
    ]

    old_log = ROOT / OLD_LOG_REL
    old_log.write_text("mtime 夹具：内容无人读，只用来证明证据比 first_seen 还老\n",
                       encoding="utf-8")
    os.utime(old_log, (EPOCH, EPOCH))
    big_log = ROOT / BIG_LOG_REL
    big_log.write_bytes(b"x" * (2 * LOG_READ_CAP + 4096)
                        + f"{BIG_TAIL_SAY}\n{BIG_TAIL_NOT}\n".encode("utf-8"))
    ansi_log = ROOT / ANSI_LOG_REL
    # 转义码**夹在词中间**放（照 vitest 的真实形状），这样"整行原样匹配"与"脱色后匹配"
    # 两种实现给出的结果不同——把码只放行首放行尾的夹具测不出这条差别，那等于没造靶子。
    ansi_log.write_bytes(
        f"\x1b[2m{ANSI_SAY[:11]}\x1b[22m\x1b[31m{ANSI_SAY[11:]}\x1b[39m\n"
        f"\x1b[2m{ANSI_NOT[:6]}\x1b[22m\x1b[31m{ANSI_NOT[6:]}\x1b[39m\n".encode("utf-8"))
    reds, tmp = 0, Path(tempfile.mkdtemp(prefix="closeout-selftest-"))
    try:
        for i, (label, its, over, want) in enumerate(bad):
            m = dict(meta)
            m.update(over)
            p = tmp / f"bad{i}.jsonl"
            p.write_text("\n".join([json.dumps(m)] + [json.dumps(x) for x in its]) + "\n",
                         encoding="utf-8")
            errs, _ = run(p, today)
            own = any(want[0] in e for e in errs)
            stray = [e for e in errs if not any(k in e for k in want)]
            tag = "红 ✓" if own and not stray else (
                "没红 ✗（门漏判这一类）" if not errs else
                "红了但原因不对 ✗" if not own else "混进别格的红 ✗ " + stray[0][:52])
            print(f"[反向 {i+1:>2}] {label:<20} {tag:<26} {errs[:1]}")
            reds += 1 if own and not stray else 0
        good_p = tmp / "good.jsonl"
        # 三种形状各出现一次：Go 腿、脚本腿、中文命名的 Go 腿。
        # 最后那条不是装饰——本仓大量用例名带中文， NAME_RE 早先只认 ASCII 时它会被判"非法用例名"，
        # 只测坏形状测不出这种"把好腿判成坏"的假红。
        good_p.write_text("\n".join([
            json.dumps(meta), json.dumps(mk()),
            json.dumps(mk(id="ST-SCRIPT", evidence=ev(legs=[sleg]))),
            json.dumps(mk(id="ST-CN", evidence=ev(legs=[{
                "pkg": "./internal/channelgw/",
                "name": "TestIsDuplicateReason_只认重复结论短语不认任意子串"}]))),
            # 内容断言的**正例**也要在合法快照里出现一次：只测坏形状的话，
            # "把 must_say 写成日志里其实没有的串"这种门会一路绿到底，没人发现它拦不住任何东西。
            json.dumps(mk(id="ST-LOGSAY", evidence=ev(logs=[
                {"path": GOOD_LOG, "must_say": ["--- PASS"], "must_not_say": ["--- FAIL"]}]))),
            # 判读窗口的正例：钉一条**只在尾巴上**的读数。窗口退化成只读头时，这一格会把
            # 合法快照判红（合法快照要求 0 红），与上面"禁串只在尾部"那格一前一后咬住同一条界。
            json.dumps(mk(id="ST-WINDOW", evidence=ev(logs=[
                {"path": BIG_LOG_REL, "must_say": [BIG_TAIL_SAY]}]))),
            # 脱色的正例：钉一条**只在脱色后才出现**的读数。反过来若实现把整行原文当断言域，
            # 这一格会把合法快照判红——与"禁串被 ANSI 遮挡"那格一前一后咬住同一条界。
            json.dumps(mk(id="ST-ANSI", evidence=ev(logs=[
                {"path": ANSI_LOG_REL, "must_say": [ANSI_SAY]}]))),
        ]) + "\n", encoding="utf-8")
        gerrs, _ = run(good_p, today)
        print(f"[合法] 好台账 0 红 {'✓' if not gerrs else '✗ ' + str(gerrs[:2])}")
    finally:
        old_log.unlink(missing_ok=True)
        big_log.unlink(missing_ok=True)
        ansi_log.unlink(missing_ok=True)
    print(f"===== selftest：{reds}/{len(bad)} 种坏形态各自隔离地红，合法快照 "
          f"{'0' if not gerrs else '非 0'} 红 =====")
    return 0 if reds == len(bad) and not gerrs else 1


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("ledger", nargs="?")
    ap.add_argument("--selftest", action="store_true")
    ap.add_argument("--dump", metavar="SPEC", help="打印该 spec 各节的实抽条目数（写钉值时用）")
    ap.add_argument("--today", default="", help="YYYY-MM-DD，测 due 门用（默认取本机今天）")
    args = ap.parse_args()
    today = (dt.datetime.strptime(args.today, DUE_FMT).date() if args.today else dt.date.today())

    if args.selftest:
        return selftest(today)
    if args.dump:
        spec = Path(args.dump)
        spec = spec if spec.is_absolute() else ROOT / spec
        if not spec.is_file():
            # 打裸 traceback 会让人以为"门坏了"，而它其实只说明参数指向的文件不在。
            print(f"--dump 的文件不存在：{args.dump}")
            return 2
        lines, bs = read_spec(spec)
        segs = own_segments(lines, bs)
        for s, h, lo, hi in bs:
            c = {sh: count_shape(lines, lo, hi, sh) for sh in ("bullet", "numbered", "tablerow")}
            if any(c.values()) or s == h:
                print(f"{s[:40]:<42} | {h[:44]:<46} bullet={c['bullet']:<3} "
                      f"num={c['numbered']:<3} row={c['tablerow']}")
        print("---- 每个 ## 节的承诺总数（写 meta.coverage 时照这一列抄）----")
        for sec, n in sorted(coverage_totals(lines, segs).items()):
            if n:
                print(f"{n:>4}  {sec}")
        return 0
    if not args.ledger:
        raise SystemExit("要给台账路径，或 --selftest / --dump")
    lp = Path(args.ledger)
    errs, info = run(lp if lp.is_absolute() else ROOT / lp, today)
    by = " ".join(f"{k}={v}" for k, v in info["by_status"].items())
    print(f"轮次 {info['round']}：条目 {info['items']}（{by} 非法status {info['unstatused']}）")
    for label, n in info["pins"]:
        print(f"  钉 {label} → 实抽 {n}")
    for e in errs:
        print("❌ " + e)
    print(f"===== 闭合判定：{'台账全部落在四终态且证据可核，计数钉全对且每条都有条目兜住' if not errs else f'{len(errs)} 条不成立'} =====")
    return 1 if errs else 0


if __name__ == "__main__":
    sys.exit(main())
