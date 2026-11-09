#!/usr/bin/env python3
"""G4：新增的「可调旋钮」硬编码必须入库（只卡新增，不卡存量）。

为什么需要它：`config_params` 建起来之后，最容易退化的不是「不接旧账」而是
**继续往里加新的硬编码**。3.1 五批接了 54 条，但同一个模式还在被复制——
下一个人加一个新的阈值常量时，没有任何一道门会问他「这个要不要入库」。

判据（rc=1）：
  对 `user-server/internal` 与 `cmd` 下**非测试、非种子**的 Go 文件，找出形如

      XxxThreshold = 30
      XxxMaxSize   = 10 * 1024 * 1024
      XxxTTL       = 5 * time.Second

  的**命名常量**（名字带 Threshold/Limit/Max/Min/TTL/Timeout/Interval/Window/
  Size/Count/Concurrency/Retries/Rate/Weight/Percent/Seconds/Days/Hours 之一），
  且它**不在基线清单**里 ⇒ NEW_TUNABLE 红。

  基线（`docs/HARDCODE_INVENTORY.md` §2.1 A 桶 / hardcode-sweep 的 TUNABLE 归类）
  是显式的：存量照常编译，但要动它就得先入库。
  新增不在巡检面——写一个新常量而不入库，这道门当场红。

**为什么按名字判而不是按值判**：值判不出语义（`30` 可以是端口、版本号、协议 op 码，
也可以是阈值）。名字带这些词缀的常量在仓内有稳定语义，且误报可以由基线消化。
代价是改个名就能绕开——所以旁边配 G5 的 keystyle 门与人工审查面，不指望一道门包打天下。

口径边界（别把"没扫到"印成"没问题"）：
  - 只看命名常量赋值。函数内的裸字面量（`f(30)`）不在本门视野——那属于 INLINE_NUM，
    由 hardcode_sweep.py 体检，治理结论是"不治理"（见 HARDCODE_INVENTORY §6）。
  - 本门的**声明面**是「大写名 + 数字链」，不是「所有包级常量」（CONST_DECL 的注释记着
    这条形状是怎么定下来的）。读数由 `scripts/measure-tunable-blindspot.py` 现测（与 G4 共用
    GO_ROOTS / is_skipped / TUNABLE_SUFFIX / CONST_DECL，改形状后它会跟着变，别引用旧数）：
      1) **小写名一律看不见**（`missingThreshold = int64(20)`、`dbSinkBatchSize = 128`）——
         现测 276 个，这是文件头「改个名就能绕开」那条代价的具体形状。
      2) 名字大写但**值不是非负数字链**的看不见。现测这一类只剩 1 个：
         `internal/pkg/utils/type/type.go` 的 `OrderStatusTimeout OrderStatusType = -1`
         ——它是状态枚举的负值哨兵而非旋钮（名字撞上了 `Timeout` 词缀），
         所以**故意不扩**值形状去接它。
      同口径的「包级阈值命名数值常量」共 474 个，本门声明面覆盖 197 个（大写、形状在内）。
    报这条覆盖面的读数时，「新增未入库=0」的意思是「没有**这种形状**的新旋钮」，
    不是「仓里没有未入库的阈值」。前者是本门的判据，后者没人能保证。
  - 协议常量（错误码、端口、状态枚举、版本号）由 EXEMPT_NAME 正则豁免，命中仍打印
    但不计红。
  - `Default*` 前缀是**参数中心兜底值**的约定标记，同样豁免（见 DEFAULT_PREFIX）。
    它们不是新的硬编码旋钮，而是 provider 注入缺失时的 fallback；判红等于让
    "接线"这个动作本身变成违规。它们受 G3 与 readpoints 门约束。
  - 基线清单是**文件级**的：整份 hardcode-sweep-classified.json 的 TUNABLE 集合。
    没有那份文件时本门退 2（不是绿）。
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
for _i, _a in enumerate(sys.argv):
    if _a == "--repo" and _i + 1 < len(sys.argv):
        REPO_ROOT = Path(sys.argv[_i + 1]).resolve()

CLASSIFIED_REL = Path("hardcode-sweep") / "hardcode-sweep-classified.json"
BASELINE_REL = Path("scripts") / "hardcode-tunable.baseline"
GO_ROOTS = [Path("user-server") / "internal", Path("user-server") / "cmd"]

# 跳过目录按**相对仓库根**的路径判定，不看绝对路径。
# 踩过的坑：原先写 `seg in p.parts`，而 `p` 是 rglob 出来的绝对路径——只要
# 仓库被检出到任何含 `tmp` / `test` / `migrations` 段的位置（本仓的干净 worktree
# 验证恰好就在 /tmp 下），**每一个文件都会被跳过**，门扫出 0 条常量。
# 表现是 rc=2 而不是恒绿（本门拒绝冒充绿），但真因在路径判定，修在这里。
# 第二处同类坑（本轮修）：带斜杠的多段条目（`cmd/seed`）原先拿去比 **相对扫描根**
# 的前缀，而扫描根本身就是 `user-server/cmd`，于是那条路径永远是 `seed/...`——
# `cmd/seed` 这个排除**从未生效过**（现测：`cmd/seed` 下 14 个非测试文件、
# `CONST_DECL` 命中 0，所以一直没出声）。改成统一按相对仓库根判定，
# 与 hardcode_sweep.py 的排除口径对齐（两边的口径必须一致，否则清单说不迁、门说新增）。
SKIP_SEGMENTS = {"cmd/seed", "vendor", "migrations", "test", "tests", "tmp", "backups"}


def is_skipped(p: Path) -> bool:
    """相对仓库根判定：多段条目比对整条路径，单段条目比对路径段。"""
    rel = p.relative_to(REPO_ROOT)
    rel_str = f"/{rel.as_posix()}/"
    if any(f"/{seg}/" in rel_str for seg in SKIP_SEGMENTS if "/" in seg):
        return True
    return any(seg in SKIP_SEGMENTS for seg in rel.parts if "/" not in seg)

TUNABLE_SUFFIX = re.compile(
    r"(Threshold|Limit|Max|Min|TTL|Timeout|Interval|Window|Size|Count|Concurrency|"
    r"Retries|Rate|Weight|Percent|Seconds|Days|Hours|Cap|Ceiling|Floor|Batch)", re.I)
EXEMPT_NAME = re.compile(
    r"^(Code|Err|Status|SpanIDHexLen|TraceIDHexLen|RDays|Version|Port|DefaultPort|"
    r"ImplicitTLSPort|AGENT_RUNTIME_BUS|SSL|DSN|MaxOpenConns|MaxIdleConns|"
    r"HNSW|Dimension|TopLogprobs|SourceDim|BGEM3|"
    # 枚举成员名里**含着**阈值词缀的子串，会被 re.I 的 TUNABLE_SUFFIX 命中：
    # `Acc|ount|StatusActive` 里的 `count`、`LeadMining` 里的 `min`。
    # 这类是值域成员（状态码/类型码），不是旋钮；本门按名字判就必须给它们一个出口，
    # 否则「补声明形状」这件事会立刻变成一批假红（现测 4 条：ClueType*、AccountStatus*）。
    # 只放**真命中的两个家族**：豁免表每多一个前缀就多掩掉一批未来可能出现的真旋钮。
    r"ClueType|AccountStatus)", re.I)
# `Default*` 前缀是参数中心兜底值的约定标记：这些常量名字里带 Default，是因为
# 它们在 provider 注入缺失时当 fallback 用（3.1 五批统一留下的形态）。
# 它们**已经**是参数中心的一份子，不是新的硬编码旋钮——判红等于让"接线"这个动作
# 本身变成违规。仍受 G3（默认值与种子一致）与 readpoints 门约束。
DEFAULT_PREFIX = re.compile(r"^Default")
# 语义上限/下限：不是运维可调的旋钮，而是某个值域的数学边界。
#   - OpportunityWinProbabilityMax = 1.0：赢率是概率，上限恒为 1，调它没有意义
#     （ltc.config 的 win_probability 也按 [0,1] 校验）。
# 判据：这类常量的值只可能取「值域的极值」，改了不会让系统行为变"可调"，
# 只会让同一份数据在边界两侧给出矛盾的结论。
SEMANTIC_BOUND = re.compile(r"(ProbabilityMax|ProbabilityMin|RatioMax|RatioMin)$")
# 声明形状：三条已知缺口，逐条在这轮补掉/写明。
#   1) **值**这一侧原先只认「一个数字」或「数字 × 一个因子」，而本门文件头的判据举的例子
#      就有 `XxxMaxSize = 10 * 1024 * 1024`（两个乘号）——那种形状匹配不上，于是
#      `HTTPIngestMaxBodySize = 4 << 20`、`MaxApprovalRequestTTL = 30 * 24 * time.Hour`
#      这类声明根本不在视野里。现扩成：数字链（任意个 `*` / `<<` / `/` 因子）
#      或类型化字面量 `int|float64|(u)int(32|64)(N)`。
#   2) 原先只认 `Name = 值`，而**带 `const`/`var` 关键字的单行声明**（`const MaxWebhookBody int64 = 2 << 20`）
#      与**带类型的声明**（`Name OrderStatusType = -1`）都要求名字后紧跟 `=`，所以整类看不见。
#      这不是理论缺口：现测这类行有 115 条（其中名字带阈值词缀的 70 条），
#      也就是说「新写一个旋钮」只要多打一个 `const` 关键字就能绕过本门。现把关键字与类型都放进形状。
#      顺带把值与行尾注释之间的分隔从 `\s*` 改成 `[ \t]*`：`\s` 含换行，旧写法会把
#      下一行的注释吃进本次匹配，`finditer` 不回头，于是被吃掉那一行上的声明可能整个漏判。
#   3) **名字仍只认大写开头**——小写名是文件头「改个名就能绕开」那条代价的具体形状，
#      数量由 `scripts/measure-tunable-blindspot.py` 现产（不在本门判据面内，只出声不计红）。
CONST_DECL = re.compile(
    r'^[ \t]*(?:const\s+|var\s+)?([A-Z]\w*)(?:[ \t]+[A-Za-z_][\w.\[\]*<>]*)?[ \t]*='
    r'[ \t]*(?:[A-Za-z]\w*[ \t]*\([ \t]*)?\d[\w.]*(?:[ \t]*(?:\*|<<|/)[ \t]*[\w.]+)*[ \t]*\)?'
    r'[ \t]*(?://.*)?$', re.M)
# 兜底实参位：GetInt(ctx, "bridge", "ingest_max_messages", HTTPIngestMaxMessages)
# 取最后一个逗号分段。实参里允许一层括号（GlobalConfigParam() 之类），
# 但不允许再嵌括号——真出现嵌套时这条正则不匹配，退化成"未接线"判红，不会漏放。
GET_CALL = re.compile(
    r"Get(?:Int|Float|Bool|Duration|String)\(((?:[^()\n]|\([^()]*\))*)\)", re.M)
# 兜底实参允许**带包名前缀**的限定名。provider seam 的装配文件在 `internal/app`，
# 它写的是 `service.L1TTLHours` / `qq.QQMessageMaxLen`，而常量声明在被导入的那个包里，
# 两边都不可能在同一份文件里出现裸名。只认裸标识符的代价：
# 1) 本门的「已作参数中心兜底」只剩同包直连那几条（实测漏认 69 个名字）；
# 2) 复用同一判据的 hardcode_inventory.py 把已接线常量算回 TUNABLE，台账虚高；
# 3) 下一批照 3.1 范式新增常量并接线时，兜底位是限定名 ⇒ 判不成 WIRED、
#    又不在基线里 ⇒ 当场假红 NEW_TUNABLE。
# 代价是取末段后名字空间变宽（不同包的同名常量共享一个判定），
# 本门只需知道「这个名字确实被当过兜底值」，不区分它来自哪个包。
FALLBACK_IDENT = re.compile(r"^[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*$")
# 直连范式的哨兵写法：GetInt(ctx, group, key, 0) 之后回落到常量。
#   if v := cp.GetInt(ctx, "memory", "l1_window_size", 0); v > 0 { return v }
#   return L1WindowSize
# 兜底实参是字面量 0（"没读到值"哨兵），真正的兜底常量在随后的 return/赋值上。
# 取调用结束后的 5 行——足够覆盖上面这个两行形态，又不会跨到别的函数。
# 哨兵尾巴**只认裸名大写常量**（`return L1WindowSize`），不认限定名：这种形态的
# 右值经常是表达式或包内字段（`timeout = time.Second * 5`），取末段会把 `Second`
# 这类名字塞进免判红集合。仓内实测限定名兜底全部走的是上面那条直接实参位，
# 没有 `Get*(..., 0)` 之后回落 `pkg.Const` 的写法。
# 尾巴上的 `(?![\w.]|\s*\()` 是把「函数调用」挡在集合外：这一行的扫描窗口从
# Get* 调用所在的那一行本身开始（`lines[start_line:]`），于是
# `if v := GlobalConfigParam().GetInt(...)` 里的 `GlobalConfigParam` 会被当常量收进来
# （HEAD 的集合里就有这个名字，放宽后还会再收进一个 `GetInt`）。
# 集合只用来免判红，混进函数名就是给同名常量开后门。
SENTINEL_TAIL = re.compile(r"(?:return\s+|:=\s*|=\s*)([A-Z]\w*)\b(?![\w.]|\s*\()")
SENTINEL_LOOKAHEAD_LINES = 5


def collect_fallback_idents(text: str) -> set[str]:
    """扫出所有被当作参数中心兜底值传进去的常量名。

    这条判据比"常量名和种子 key 对得上"准确得多：接线时保留原常量名
    （避免把参数化放大成一片 diff），所以名字往往与 key 毫无关系——
    `HTTPIngestMaxMessages` 的种子 key 是 `bridge.ingest_max_messages`，
    `L1WindowSize` 的是 `memory.l1_window_size`。按名字配对只能靠一张
    人工维护的映射表，而映射表必然和代码漂移。实参位置不会漂移。

    同时覆盖两种接线范式（3.1a 定的判据）：
      1) provider seam：常量直接出现在 Get* 的兜底实参位
      2) 直连：Get* 兜底传 0，函数体随后回落到常量
    """
    out: set[str] = set()
    lines = text.splitlines()
    for m in GET_CALL.finditer(text):
        args = m.group(1)
        parts = args.split(",")
        if len(parts) < 3:
            # 少于三个实参不是本门关心的形态（(ctx, group, key) 这种没有兜底位）
            continue
        last = parts[-1].strip()
        if FALLBACK_IDENT.match(last):
            out.add(last.rsplit(".", 1)[-1])
            continue
        if last != "0":
            continue
        # 哨兵范式：从调用结束处往后找几行，看有没有回落到某个大写常量
        start_line = text.count("\n", 0, m.end())
        tail = lines[start_line:start_line + SENTINEL_LOOKAHEAD_LINES]
        for ln in tail:
            if "func " in ln:
                break
            sm = SENTINEL_TAIL.search(ln)
            if sm:
                out.add(sm.group(1))
    return out


def fail(msg: str, code: int = 2):
    print(f"❌ {msg}")
    sys.exit(code)


def load_baseline() -> set[str]:
    """读 `scripts/hardcode-tunable.baseline`：存量 TUNABLE 常量名，一行一个。

    为什么自己落一份而不直接读 hardcode-sweep-classified.json：
      1) 判据文件要和门脚本一起进 CI 触发面（`scripts/check-ci-gate-paths.py`
         的第 2 条）。那份 classified 是 210KB 的可再生产物，跟门没有稳定的
         一对一关系；独立成 `.baseline` 之后形状与其他五道门一致，
         也符合本仓 `*.baseline` 的既有约定。
      2) 本门只需要 286 个名字，不需要 file/line/value 等其余字段。
      3) 基线与生成物解耦后，重新跑 `hardcode_classify.py` 不会顺手把
         门的判据改掉——刷基线必须是人的显式动作。
    刷新：`python3 scripts/check-hardcode-tunable.py --refresh-baseline`
    （需要磁盘上存在 hardcode-sweep-classified.json）。
    """
    p = REPO_ROOT / BASELINE_REL
    if not p.is_file():
        fail(f"基线文件不存在: {p}（跑本脚本 --refresh-baseline 从 "
             f"{CLASSIFIED_REL} 生成；没有基线本门无法区分「新增」与「存量」，不能绿）")
    out: set[str] = set()
    for ln in p.read_text(encoding="utf-8").splitlines():
        name = ln.strip()
        if not name or name.startswith("#"):
            continue
        out.add(name)
    return out


def refresh_baseline() -> int:
    classified = REPO_ROOT / CLASSIFIED_REL
    if not classified.is_file():
        fail(f"刷基线需要 {CLASSIFIED_REL}（跑 scripts/hardcode_classify.py 生成）")
    data = json.loads(classified.read_text(encoding="utf-8"))
    names = sorted({r.get("name") for r in (data.get("rows") or [])
                    if r.get("bucket") == "TUNABLE" and r.get("name")})
    basename = REPO_ROOT / BASELINE_REL
    # 手工补登段必须原样保留：基线里从第一个 `# ──` 分隔线到文件尾的部分，是人工写明的
    # 「为什么不入库」理由（B1-a 四条、geo 两条）。全量重写会把它连名字带理由一起吞掉，
    # 下一次 `make audit` 又红在那些常量上——「刷基线」于是变成了「把判据删掉」。
    # 自动段只负责「扫描器认定的 TUNABLE 存量名」，手工段单独成尾、绝不覆盖。
    manual_tail = ""
    if basename.is_file():
        lines = basename.read_text(encoding="utf-8").splitlines()
        for i, ln in enumerate(lines):
            if ln.startswith("# ──"):
                manual_tail = "\n" + "\n".join(lines[i:]).rstrip() + "\n"
                break
    manual_names = {ln.strip() for ln in manual_tail.splitlines()
                    if ln.strip() and not ln.startswith("#")}
    names = [n for n in names if n not in manual_names]
    header = (
        "# 存量「可调旋钮」常量名基线（scripts/check-hardcode-tunable.py 读取）。\n"
        "# 一行一个 Go 常量名。它代表的是「存量已经数过、判定为存量待收口」，\n"
        "# 不是「这样写是对的」——本表只挡新增，不发合格证。\n"
        f"# 来源：{CLASSIFIED_REL} 的 TUNABLE 归类；刷新命令：\n"
        "#   python3 scripts/check-hardcode-tunable.py --refresh-baseline\n"
        "# 刷新前先确认这次是「把该收口的收口了」而不是「想把这笔从门里摘出去」。\n"
        "# 手工补登段（从 `# ──` 到文件尾）刷新时原样保留，不在自动段内。\n"
    )
    basename.write_text(
        header + "\n".join(names) + "\n" + manual_tail, encoding="utf-8")
    print(f"✅ 基线已刷新: {BASELINE_REL}（{len(names)} 个自动存量名 + 手工补登段保留）")
    return 0


def scan_go() -> tuple[list[tuple[str, str]], set[str]]:
    out: list[tuple[str, str]] = []
    fallback: set[str] = set()
    for root in GO_ROOTS:
        base = REPO_ROOT / root
        if not base.is_dir():
            continue
        for p in base.rglob("*.go"):
            if p.name.endswith("_test.go"):
                continue
            if is_skipped(p):
                continue
            try:
                t = p.read_text(encoding="utf-8", errors="replace")
            except OSError:
                continue
            rel = p.relative_to(REPO_ROOT).as_posix()
            for m in CONST_DECL.finditer(t):
                out.append((rel, m.group(1)))
            fallback |= collect_fallback_idents(t)
    return out, fallback


def check_alternation_shape() -> None:
    """交替表坏成「空支」时本门会静默失效，所以自检必须自己发（判红偏绿的那侧没人报警）。

    `^(A|B|)` / `^()` / `^(|A)` 都能匹配**空前缀**：`EXEMPT_NAME.match(anything)` 恒真
    ⇒ 每个候选名都被判成协议豁免，门当场变橡皮章。实测踩过：摘掉一个豁免前缀时漏删
    它前面那根竖线，读数从「兜底=3 豁免=52」跳成「兜底=0 豁免=77」且 rc=0——
    少掉的 25 个不是消失了，是被空支吞进了豁免桶。TUNABLE_SUFFIX 的空支反向（恒真）
    会把所有常量判成旋钮，同样要当场停。
    """
    for name, rx in (("TUNABLE_SUFFIX", TUNABLE_SUFFIX), ("EXEMPT_NAME", EXEMPT_NAME),
                     ("DEFAULT_PREFIX", DEFAULT_PREFIX), ("SEMANTIC_BOUND", SEMANTIC_BOUND)):
        if re.search(r"\|\||\(\||\|\)", rx.pattern):
            fail(f"{name} 的交替表里有空支（`||` / `(|` / `|)`），判据会恒真：{rx.pattern}")


def main() -> int:
    check_alternation_shape()
    if "--refresh-baseline" in sys.argv:
        return refresh_baseline()
    baseline = load_baseline()
    if not baseline:
        fail("基线 TUNABLE 集合为空")
    decls, fallback = scan_go()
    if not decls:
        fail("一个 Go 常量声明都没扫到：环境或路径不对")

    wired: list[tuple[str, str]] = []
    new: list[tuple[str, str]] = []
    exempt: list[tuple[str, str]] = []
    for path, name in decls:
        if name in baseline:
            continue
        if EXEMPT_NAME.match(name):
            exempt.append((path, name))
            continue
        if DEFAULT_PREFIX.match(name):
            exempt.append((path, name))
            continue
        if SEMANTIC_BOUND.search(name):
            exempt.append((path, name))
            continue
        if name in fallback:
            wired.append((path, name))
            continue
        if TUNABLE_SUFFIX.search(name):
            new.append((path, name))

    print(f"扫描面: Go 常量声明 {len(decls)} 个 / 基线 TUNABLE {len(baseline)} 个")
    print(f"读数: 新增未入库={len(new)}  已作参数中心兜底={len(wired)}  "
          f"豁免（协议类/参数中心兜底前缀）={len(exempt)}")
    for path, name in wired:
        print(f"  WIRED_FALLBACK: {path}: {name} 已被 Get*() 当兜底值传入参数中心")
    for path, name in new:
        print(f"  NEW_TUNABLE: {path}: {name} 名字像可调旋钮但既不在基线也不在参数中心")
    if new:
        print("❌ 新增可调旋钮门未通过：入库（config_params）或加入基线并说明为什么不用入库")
        return 1
    print("✅ 新增可调旋钮门通过：没有绕过参数中心的新旋钮常量")
    return 0


if __name__ == "__main__":
    sys.exit(main())
