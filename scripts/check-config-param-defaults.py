#!/usr/bin/env python3
"""G3：参数中心的默认值必须与代码兜底一致。

为什么需要它（2026-10-11）：参数中心 DefaultValue 的语义是「**没人配置时系统现在的
行为**」，不是「更合理的目标值」。两者不一致时，升级/seed 会静默改变行为：
1.6 的 `telemetry.trace_sink_buffer` 就是这样——种子写 8192、代码写 2048，
照抄种子等于升级即把每个 trace sink 的缓冲内存翻 4 倍；而丢弃事件只在缓冲满时
暴露、越晚暴露越难定位。2d 的种子 Min/Max 也贴过平台硬上限，同理。

判据（rc=1）：
  MISMATCH   种子的 DefaultValue 与代码里该键的**实际兜底值**不一致。
  BAD_SEED   DefaultValue 过不了自己的 Min/Max（与 validateValue 同口径）。
  NO_CONST   静态解不出该键的兜底值。默认只提示不判红（`--strict` 下红），
             但每条都带**原因**——"解不出"必须是可审计的口径，不能是一个
             把所有漏网都吞掉的兜底分类。
  对账       `可比对 + 无法比对 + 前端下发 + 已下架 == 种子条目数`，
             不等 ⇒ rc=2（门自己坏了）。
  健康下界    两个 rc=2：常量池为空；`无法比对 > 可比对`。
             为什么必须有它：NO_CONST 默认不判红，于是**解析面坏掉时本门会绿**。
             实测把常量声明正则的 `re.M` 去掉，常量池 2817→0，读数
             「可比对 58 / 无法比对 128」仍退 0——那 58 条只来自结构体字段表，
             门已经只剩一层皮，而输出长得像"少量解不出"。

兜底值怎么定位（这是本门的全部难度所在）：
  早期版本按**名字猜**——把 key 折成 `Default{CamelCase(key)}` 去常量池里找同名常量。
  实测 217 条里只有 9 条猜得中，而自证行印的是「可比常量 112 个」——那是常量池的
  大小，读起来像 112 条被验过。名字与 key 之间没有约定关系：接线时按约定**保留原
  常量名**（`HTTPIngestMaxMessages` ↔ `bridge.ingest_max_messages`），因为把一次
  参数化放大成改名 diff 的代价比保持一致性大。
  现在改成按**实参位置**反查（与 G4 共用同一条判据）：`Get*(ctx, group, key, 兜底位)`
  的第四个实参就是"读不到时的行为"，它在种子表里的对应物必须是 DefaultValue。
  五种形态：

    A   直接实参位：`GetInt(ctx, "bridge", "sse_max_backlog_events", SSEMaxBacklogEvents)`
        实参可以是字面量（`true` / `16` / `"off"`）、常量名（允许带包名前缀，
        provider seam 全在 internal/app 里写 `llmpkg.DefaultHealthCheckInterval`）、
        `int(pkg.Const)` 这类转换，或 `pkg.Const * time.Second` 这类乘式。
    A0  兜底位就是 `0` 且后面**没有**回落形态：0 本身即兜底值
        （`misc.upload_max_size_mb` 的语义是"0＝不覆盖、沿用下游"）。
    B1  哨兵 + 同函数回落：`if v := GetInt(ctx, g, k, 0); v > 0 { return v }`，随后
        `return L1WindowSize`。
    B2  哨兵 + 同文件回落：`func xxxRaw(ctx) { return GetDuration(ctx, g, k, 0) }`，
        调用方写 `return positiveIntOrDH(xxxRaw(ctx), HealthScoreSwitchThreshold)`。
    B3  哨兵或直接 + 配置结构体：`d := feedbackloop.DefaultBanditConfig()` 之后
        `d.MinSamplesForExploit = fbOrInt(GetInt(ctx, g, k, 0), d.MinSamplesForExploit)`
        —— 兜底值在另一个包的 `Default*Config()` 字面量里，按 (函数名, 字段名) 查表。

口径边界（写明本门看不见什么，别把"没扫到"印成"没问题"）：
  - 常量表只认**全仓同名唯一值**：一个名字在两处取不同值 ⇒ 判为歧义，该键进
    NO_CONST（原因写"同名常量歧义"），不挑一个值冒充比过。
  - duration 两侧统一折成**秒**：种子写裸秒（`28800`），常量写 `5 * time.Minute`；
    兜底位是裸数字而种子是 duration 时也按秒处理（与 `bridge.online_grace_window`
    的"裸数字按秒"同一条口径）。
  - 兜底位是**空串**（`GetString(ctx, g, k, "")`）不算比对对象：这种写法的默认值就是
    种子行本身（seed 会把 DefaultValue 写进库），`""` 只在"整行不存在"时生效。拿它比
    种子会产出一堆假 MISMATCH，所以按原因单列进 NO_CONST。
  - **前端下发组**（`runtimePublicGroups`，判据与读取点门同源）单列不比对：
    那些键的读取点在 user-web，后端只有下发没有兜底实参，静态上没有可比对象。
  - 已下架（`Deprecated: true`）的条目单列不比对：管理端不返回、runtime 不下发，
    它的 DefaultValue 与线上行为已无一致性可言。
  - 动态拼出来的 group/key、经变量转手再赋给兜底位的写法不在本门视野内。
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
for _i, _a in enumerate(sys.argv):
    if _a == "--repo" and _i + 1 < len(sys.argv):
        REPO_ROOT = Path(sys.argv[_i + 1]).resolve()

SEEDS_REL = Path("user-server") / "internal" / "service" / "config_param_seeds.go"
RUNTIME_REL = Path("user-server") / "internal" / "controller" / "config_param_runtime.go"
SCAN_REL = Path("user-server")
# 覆盖率棘轮（见 main 里 `覆盖率失守` 那段）：登记的是**本门当前真能比对的条数**。
BASELINE_REL = Path("scripts") / "config-param-defaults.baseline"

# 条目只锚头部（与 G5 / 读取点门同形状）：把 Description 写进解析式的话，
# 一条三段拼接的 Description 就能让整条种子对本门隐形。
ENTRY_HEAD = re.compile(r'\{\s*Group:\s*"([a-z_]+)"\s*,\s*Key:\s*"([a-z0-9_]+)"')
ENTRY_START = re.compile(r'\{\s*Group:\s*"')
VALUE_TYPE = re.compile(r'ValueType:\s*"([^"]+)"')
# DefaultValue 允许两种 Go 字符串字面量：`"..."` 与反引号 `...`。
# 只认双引号时，三条整份 JSON 的种子（permission.role_permissions_json、
# agent_llm.provider_failover_policy、embedding.global_override）会被读成
# "DefaultValue 是空串"——今天它们恰好先被"兜底位是空串"那条豁免接住，所以没出声；
# 但只要谁把代码兜底改成非空串，本门就会拿 `''` 去比一份真 JSON，产出一条假 MISMATCH。
DEFAULT_VALUE = re.compile(r'DefaultValue:\s*(?:"([^"]*)"|`([^`]*)`)')
MIN_VALUE = re.compile(r'Min:\s*(?:strPtr\(\s*)?"([^"]+)"')
MAX_VALUE = re.compile(r'Max:\s*(?:strPtr\(\s*)?"([^"]+)"')
DEPRECATED = re.compile(r'Deprecated:\s*true')

# `GetInt(ctx, "g", "k", FALLBACK)`：允许一层括号（`GlobalConfigParam().GetInt(...)`），
# 不允许再嵌——真出现嵌套时不匹配，退化成"解不出兜底值"，不会放过。
GET_CALL = re.compile(
    r'Get(?:Int|Float|Bool|Duration|String)\(((?:[^()\n]|\([^()]*\))*)\)', re.M)
NAME_REF = re.compile(r"^[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*$")
# `ParamGroupMemory = "memory"` / `MemoryL1WindowSizeParam = "l1_window_size"`
STR_CONST = re.compile(r'\b([A-Za-z_]\w*)\s*=\s*"([a-z0-9_]+)"')
# 常量声明：`const X = 1024` / const 块内裸行 `X = 5 * time.Minute` / `var X time.Duration = …`
# 名字**不限大小写**：兜底位经常是包内私有常量（`faqAnswerCacheDefaultTTL`、
# `wsPushIntervalDefault`），只认大写开头会把这类接线全判成"解不出"。
# 右值取到行尾，注释在 `const_values` 里剥——**不要**写成 `(?:\s*//.*)$`：那组没有 `?`，
# 等于把"带行尾注释"变成声明的**必要条件**，实测没注释的 const 块行一条都进不了池
# （覆盖率从 177 掉到 64 就是这么来的，且 477 这个池子大小看着像正常）。
NUM_CONST = re.compile(
    r'^\s*(?:const|var)?\s*([A-Za-z_]\w*)\s*(?:[A-Za-z_][\w.]*(?:\[\])?\s+)?=\s*([^=\n]+)$',
    re.M)
# B1 的回落目标：大写开头的**裸**标识符，且后面不是成员访问或函数调用
RETURN_CONST = re.compile(r'\breturn\s+([A-Z]\w*)\b(?![\w.(])')
# B3 的两跳：赋值目标 `d.Field = orX(` 与构造调用 `d := pkg.DefaultXxxConfig()`。
# 构造调用这条**必须接住 `:=`**：写成 `(?::|=)` 时 `d := feedbackloop.DefaultBanditConfig()`
# 匹配不上（`:` 之后紧跟 `=`，而 `(?:[\w.]*\.)?` 吃不掉 `=`），于是整层 B3 静默失效、
# 20 条 feedback 种子被读成"兜底位是 0"的 A0 假 MISMATCH。
B3_TARGET = re.compile(r'\b(\w+)\.([A-Z]\w*)\s*=\s*[\w.]+\(')
FUNC_HEAD = re.compile(r'^func\s+(?:\([^)]*\)\s*)?(\w+)')
CTOR_FUNC = re.compile(r'^func\s+(Default\w+)\s*\([^)]*\)[^\n{]*\{', re.M)
CTOR_CTOR = re.compile(r'\b(\w+)\s*:?=\s*(?:[\w.]*\.)?(Default\w+)\(')
# 构造函数体里的字段初值：`MinSamplesForExploit: 100,` / `FlushInterval: 5 * time.Minute,`
CTOR_FIELD = re.compile(r'\b([A-Z]\w*)\s*:\s*([^,\n}]+)')
SENTINEL_LOOKAHEAD_LINES = 6

DUR_UNITS = {"Nanosecond": 1e-9, "Microsecond": 1e-6, "Millisecond": 1e-3,
             "Second": 1.0, "Minute": 60.0, "Hour": 3600.0}
NUMBER = re.compile(r"^-?\d[\d_]*(?:\.\d+)?$")
# 负号只在**最前面**认（`-0.5`、`-1 * time.Second`）；中间的 `-` 不解析成减法，
# 含减法的右值一律落 ('other') 由调用方记原因——本门的原则是不猜。
TIME_TERM = re.compile(r"^(?:(-?\d[\d_]*)\s*\*\s*)?time\.(\w+)(?:\s*\*\s*(\d[\d_]*))?$")
TYPE_CAST = re.compile(r"^(?:int|int8|int16|int32|int64|uint|uint64|float32|float64|time\.Duration)\((.+)\)$")


def fail(msg: str, code: int = 2):
    print(f"❌ {msg}")
    sys.exit(code)


def norm(v: float):
    return int(v) if float(v).is_integer() else v


def norm_s(v):
    """读数打印用：整数值不带 `.0`，字符串原样。"""
    return norm(v) if isinstance(v, (int, float)) else v


def split_args(inner: str) -> list[str]:
    """按逗号切分实参，忽略括号内部的逗号。"""
    out, depth, cur = [], 0, ""
    for ch in inner:
        if ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
        if ch == "," and depth == 0:
            out.append(cur.strip())
            cur = ""
        else:
            cur += ch
    out.append(cur.strip())
    return out


def parse_value(expr: str):
    """把常量右值折成 ('num'|'dur'|'bool'|'str'|'other', 值)。

    duration 一律折成**秒**（与种子里 duration 的写法同单位）；`1024 * 1024`、
    `7 * 24 * 3600`、`4 << 20` 这类位运算/乘式折成数字；
    `5*time.Minute + 30*time.Second` 按秒累加。
    解不出的返回 ('other', 原文)，由调用方归入 NO_CONST——本门的原则是不猜。
    """
    e = expr.strip()
    if e in ("true", "false"):
        return ("bool", e == "true")
    if re.fullmatch(r'"(?:[^"\\]|\\.)*"', e):
        return ("str", e[1:-1])
    shift = re.fullmatch(r'(\d[\d_]*)\s*<<\s*(\d+)', e)
    if shift:
        return ("num", int(shift.group(1).replace("_", "")) << int(shift.group(2)))
    terms = [x.strip() for x in e.split("+")]
    if len(terms) > 1 and all(terms):
        total, is_dur, ok = 0.0, False, True
        for term in terms:
            kind, val = parse_value(term)
            if kind == "dur":
                is_dur, total = True, total + float(val)
            elif kind == "num":
                total += float(val)
            else:
                ok = False
                break
        if ok:
            return ("dur", norm(total)) if is_dur else ("num", norm(total))
    factors = [x.strip() for x in e.split("*")]
    if len(factors) > 1 and all(factors):
        is_dur, secs, num, ok = False, 1.0, 1.0, True
        for fac in factors:
            tm = TIME_TERM.fullmatch(fac)
            if tm and tm.group(2) in DUR_UNITS:
                mult = 1.0
                for g in (tm.group(1), tm.group(3)):
                    if g:
                        mult *= float(g.replace("_", ""))
                is_dur, secs = True, secs * DUR_UNITS[tm.group(2)] * mult
                continue
            if NUMBER.fullmatch(fac):
                num *= float(fac.replace("_", ""))
                continue
            ok = False
            break
        if ok:
            return ("dur", norm(secs * num)) if is_dur else ("num", norm(num))
    if NUMBER.fullmatch(e):
        return ("num", norm(float(e.replace("_", ""))))
    return ("other", e)


def seed_entries(text: str):
    """[(group, key, value_type, default, min, max, deprecated)]，只锚头部 + 段内取字段。

    `default` 在**没有 DefaultValue 这个字段**时是 None（与"字段在、值为空串"是两种
    情况：后者要参与比对，前者没法比）。
    """
    heads = list(ENTRY_HEAD.finditer(text))
    out = []
    for i, m in enumerate(heads):
        end = heads[i + 1].start() if i + 1 < len(heads) else len(text)
        seg = text[m.end():end]

        def field(rx):
            hit = rx.search(seg)
            if not hit:
                return None
            return hit.group(1) if hit.group(1) is not None else hit.group(2)

        def field_s(rx):
            return field(rx) or ""

        out.append((m.group(1), m.group(2), field_s(VALUE_TYPE), field(DEFAULT_VALUE),
                    field_s(MIN_VALUE), field_s(MAX_VALUE),
                    DEPRECATED.search(seg) is not None))
    return out


def frontend_groups():
    """`GET /api/config/runtime` 下发给前端的组（与读取点门同一事实源）。

    这些组的键**在后端没有兜底实参**（读取点在 user-web），本门结构上比不了。
    解析不出来就 rc=2：宁可门停摆，也不要"前端集合为空 ⇒ 全部当可比对"的假绿。
    """
    rt = REPO_ROOT / RUNTIME_REL
    if not rt.is_file():
        fail(f"前端只读端点文件不存在: {rt}（它承载 runtimePublicGroups，本门靠它划出比不了的组）")
    text = rt.read_text(encoding="utf-8", errors="replace")
    block = re.search(r"var runtimePublicGroups\s*=\s*map\[string\]struct\{}\{(.*?)\n\}", text, re.S)
    if not block:
        fail(f"{RUNTIME_REL} 里解不出 runtimePublicGroups 的 map 字面量（形状变了，前端口径失准）")
    groups = set(re.findall(r'"([a-z0-9_]+)"\s*:', block.group(1)))
    excl_block = re.search(r"var runtimePublicKeys\s*=\s*map\[string\](?:bool|struct\{\})\{(.*?)\n\}",
                           text, re.S)
    excluded = set(re.findall(r'"([a-z0-9_]+\.[a-z0-9_]+)"\s*:', excl_block.group(1))) \
        if excl_block else set()
    if not groups:
        fail("runtimePublicGroups 解出来是空集合")
    return groups, excluded


def load_baseline() -> int:
    """读覆盖率棘轮：一行一个整数（本门真能比对的条数下界）。

    为什么需要它（本轮反向测跑出来的）：NO_CONST 默认不判红，所以**解析面退化**时
    本门照样绿。把常量声明正则的 `re.M` 去掉 ⇒ 常量池 2817→0、可比对 183→58，仍退 0；
    还原成"行尾注释是必要条件"那版（本轮真踩的坑）⇒ 可比对 95、无法比对 91，仍退 0。
    两道"多数"下界拦得住前者、拦不住后者（91 < 95）。棘轮把口径换成绝对下界：
    可比对数掉下来就得有人来这里解释一次，而不是让门悄悄变成橡皮章。
    与 `scripts/hardcode-tunable.baseline` 同形：由 `check-ci-gate-paths.py` 第 2 条
    自动派生并要求它进 CI 触发面（改基线必须重跑这道门）。
    """
    p = REPO_ROOT / BASELINE_REL
    if not p.is_file():
        fail(f"覆盖率基线不存在: {p}（跑 `python3 scripts/{Path(__file__).name} "
             f"--refresh-baseline` 生成；没有基线本门无法区分「代码真的没接」与"
             f"「门的解析面坏了」，不能绿）")
    nums = [ln.strip() for ln in p.read_text(encoding="utf-8").splitlines()
            if ln.strip() and not ln.strip().startswith("#")]
    if len(nums) != 1 or not nums[0].isdigit():
        fail(f"{BASELINE_REL} 应恰好有一行整数，实际读到 {nums}（基线坏了，不是通过）")
    return int(nums[0])


def refresh_baseline(compared: int) -> int:
    header = (
        "# 参数中心默认值门（scripts/check-config-param-defaults.py）的覆盖率棘轮。\n"
        "# 一个整数：本门当前**真能比对**的种子条数下界。它代表的是「门自己的覆盖面」，\n"
        "# 不是「代码质量分数」——可比对数低于它 ⇒ 门 rc=2（门坏了），而不是内容红。\n"
        "# 刷新：python3 scripts/check-config-param-defaults.py --refresh-baseline\n"
        "# 只在两种情况下改小：门的某条兜底形态被**改进**成别的判据、或种子条目被删；\n"
        "# 都要在同一笔里写明是哪一种。\n")
    (REPO_ROOT / BASELINE_REL).write_text(header + f"{compared}\n", encoding="utf-8")
    print(f"✅ 覆盖率基线已刷新: {BASELINE_REL}（可比对 {compared}）")
    return 0


def collect_files():
    root = REPO_ROOT / SCAN_REL
    seeds = (REPO_ROOT / SEEDS_REL).resolve()
    files = []
    for p in sorted(root.rglob("*.go")):
        if p.name.endswith("_test.go") or p.resolve() == seeds:
            continue
        rel_parts = p.relative_to(root).parts
        if any(seg in ("vendor", "migrations", "seed") for seg in rel_parts):
            continue
        try:
            files.append((p.relative_to(REPO_ROOT),
                          p.read_text(encoding="utf-8", errors="replace")))
        except OSError:
            continue
    return files


def string_consts(files):
    """IDENT -> 字面量串（组名与键名的常量共用一张表）。同名多值的键剔除，不猜。"""
    seen: dict[str, set] = {}
    for _p, text in files:
        for m in STR_CONST.finditer(text):
            seen.setdefault(m.group(1), set()).add(m.group(2))
    return {k: next(iter(v)) for k, v in seen.items() if len(v) == 1}


def const_values(files):
    """NAME -> (kind, value)；同名多值的名字单独返回（歧义集）。"""
    seen: dict[str, set] = {}
    for _p, text in files:
        for m in NUM_CONST.finditer(text):
            kind, val = parse_value(m.group(2).split("//", 1)[0])
            if kind != "other":
                seen.setdefault(m.group(1), set()).add((kind, val))
    uniq = {n: next(iter(v)) for n, v in seen.items() if len(v) == 1}
    return uniq, {n for n, v in seen.items() if len(v) > 1}


def brace_block(text: str, open_idx: int) -> str:
    depth = 0
    for i in range(open_idx, min(len(text), open_idx + 20000)):
        if text[i] == "{":
            depth += 1
        elif text[i] == "}":
            depth -= 1
            if depth == 0:
                return text[open_idx + 1:i]
    return ""


def constructor_fields(files):
    """(Default*Config 函数名, 字段名) -> (kind, value)，供 B3 查表。"""
    out: dict[tuple[str, str], tuple[str, object]] = {}
    for _p, text in files:
        for fm in CTOR_FUNC.finditer(text):
            body = brace_block(text, fm.end() - 1)
            if not body:
                continue
            for cm in CTOR_FIELD.finditer(body):
                kind, val = parse_value(cm.group(2))
                if kind != "other":
                    out.setdefault((fm.group(1), cm.group(1)), (kind, val))
    return out


def func_index(lines):
    return [(i, m.group(1)) for i, ln in enumerate(lines) if (m := FUNC_HEAD.match(ln))]


def enclosing_func(funcs, line_no: int) -> tuple[str, int]:
    name, start = "", -1
    for i, fn in funcs:
        if i <= line_no:
            name, start = fn, i
        else:
            break
    return name, start


def ctor_of_var(lines: list[str], fstart: int, upto: int, var: str) -> str:
    """在函数体内往上找 `var := pkg.Default*Config()`，返回构造函数名。"""
    for j in range(fstart, upto):
        m = CTOR_CTOR.search(lines[j])
        if m and m.group(1) == var:
            return m.group(2)
    return ""


def b3_field_on_line(lines, funcs, line_no) -> str:
    """调用行自身是不是 B3 形态，返回 `Ctor.Field`（不是则空串）。

    判据取"行"而不是"兜底实参是不是 0"：B3 的兜底位可以是 `0`、可以是 `fbMissingFloat`
    （NaN 哨兵），也可以直接写 `d.Field`——三种写法的有效默认值都是结构体字段本身，
    由 `var.Field = orX(Get*(ctx, g, k, <哨兵>), var.Field)` 这行自己声明。只认 `0`
    会把 NaN 那一类漏成 NO_CONST。
    要求 LHS 的 `var.Field` 在同一行再出现一次（作为 orX 的第二参或 Get* 的兜底位），
    否则 `d.X = orY(GetInt(...), d.Z)` 这种左右不同字段会拿错值。
    """
    if line_no >= len(lines):
        return ""
    line = lines[line_no]
    tm = B3_TARGET.search(line)
    if not tm:
        return ""
    var, field = tm.group(1), tm.group(2)
    if not re.search(rf'\b{re.escape(var)}\.{re.escape(field)}\b', line[tm.end():]):
        return ""
    _fn, fstart = enclosing_func(funcs, line_no)
    if fstart < 0:
        return ""
    ctor = ctor_of_var(lines, fstart, line_no, var)
    return f"{ctor}.{field}" if ctor else ""


def proven_positive_wrapper(lines, funcs, raw_name):
    """只认完整包装体，不以附近任意 return 猜有效默认值。"""
    targets = set()
    seconds_wrappers = set()
    target = r'(?:[A-Za-z_]\w*|\d[\d_]*(?:\s*\*\s*time\.(?:Second|Minute|Hour))?)'
    for index, (start, name) in enumerate(funcs):
        if name == raw_name:
            continue
        end = funcs[index + 1][0] if index + 1 < len(funcs) else len(lines)
        segment = '\n'.join(re.sub(r'//.*$', '', line) for line in lines[start:end]).strip()
        opening = segment.find('{')
        if opening < 0 or not segment.endswith('}'):
            continue
        body = segment[opening + 1:-1].strip()
        helper = re.fullmatch(
            rf'return\s+positive(?:Int|Duration)Or\(\s*{re.escape(raw_name)}\(\s*ctx\s*\)\s*,\s*({target})\s*\)\s*',
            body)
        if helper:
            targets.add(helper.group(1).strip())
            continue
        guarded = re.fullmatch(
            rf'if\s+(\w+)\s*:=\s*{re.escape(raw_name)}\(\s*ctx\s*\)\s*;\s*\1\s*>\s*0\s*\{{\s*'
            rf'return\s+(\1|time\.Duration\(\1\)\s*\*\s*time\.Second)\s*\}}\s*return\s+({target})\s*',
            body)
        if guarded:
            targets.add(guarded.group(3).strip())
            seconds_wrappers.add(guarded.group(2) != guarded.group(1))
    if len(targets) > 1:
        return ('conflict', ', '.join(sorted(targets)), 'B2-positive')
    if targets:
        form = 'B2-positive-seconds' if seconds_wrappers == {True} else 'B2-positive'
        return ('expression', targets.pop(), form)
    return None


def resolve_sentinel(lines, funcs, text, line_no):
    """兜底实参是 0 时往后找真正的回落目标，返回 ('const'|'field', 名字, 形态)。"""
    fn, fstart = enclosing_func(funcs, line_no)
    if fn:
        proven = proven_positive_wrapper(lines, funcs, fn)
        if proven:
            return proven
    for j in range(line_no, min(line_no + SENTINEL_LOOKAHEAD_LINES, len(lines))):
        if j > line_no and FUNC_HEAD.match(lines[j]):
            break
        sm = RETURN_CONST.search(lines[j])
        if sm:
            return ("const", sm.group(1), "B1")
    if fn:
        # B2：本函数是 `xxxRaw(ctx)`，调用方写 `return orX(xxxRaw(ctx), CONST)`
        bm = re.search(rf'\b{re.escape(fn)}\([^)]*\)\s*,\s*([A-Z]\w*)\s*\)', text)
        if bm:
            return ("const", bm.group(1), "B2")
        hit = b3_field_on_line(lines, funcs, line_no)
        if hit:
            return ("field", hit, "B3")
    return None


def resolve_fallback_arg(fb: str, consts, ambiguous, lines, funcs, line_no):
    """兜底实参 -> (kind, value, 形态)；解不出返回 (None, 原因, None)。

    返回元数**固定为 3**：调用方按 `kind is None` 分诊，不用管失败分支的第二个实参
    是原因还是值。早前这里成功返回 3 元、失败返回 2 元，调用方为了兼容两种元数
    写成了把同一个函数调两遍的三元表达式——那既是可读性问题，也让"field 形态"
    这一支永远拿不到值。
    """
    kind, val = parse_value(fb)
    if kind != "other":
        return kind, val, "A"
    # `d.Field` 形态（B3 的直接版：Get* 的兜底位就是结构体字段）
    dot = re.fullmatch(r'([a-z]\w*)\.([A-Z]\w*)', fb)
    if dot:
        _fn, fstart = enclosing_func(funcs, line_no)
        ctor = ctor_of_var(lines, fstart, line_no, dot.group(1)) if fstart >= 0 else ""
        if ctor:
            return "field", f"{ctor}.{dot.group(2)}", "B3"
        # 找不到构造点 ⇒ 它更可能是**包名限定的常量**（`llmpkg.DefaultHealthCheckInterval`），
        # 首段小写恰好与局部变量同形。这里直接判失败会把 internal/app 里的 provider seam
        # 接线整批判成"解不出"（实测 68 个键），而它们正是 3.1 五批的写法。
        # 落到下面的名字路径；两条路都走不通时原因里带上这个线索。
    inner = TYPE_CAST.sub(r'\1', fb) if TYPE_CAST.fullmatch(fb) else fb
    parts = [x.strip() for x in inner.split("*")]
    # 时间单位（`time.Second`）与包名限定常量（`svc.DefaultX`）在 NAME_REF 下同形，
    # 必须先把带 `time.` 前缀的项排除出"常量名"，否则 `svc.DefaultX*time.Second`
    # 会被当成两个名字而判"形态不认识"（实测踩过）。
    names = [p for p in parts
             if NAME_REF.fullmatch(p) and not TIME_TERM.fullmatch(p)]
    if len(names) != 1 or not all(parts):
        hint = "（形似字段但没有 Default*Config 构造点）" if dot else ""
        return None, f"兜底实参形态不认识{hint}：{fb[:48]}", None
    name = names[0].rsplit(".", 1)[-1]
    scale, is_dur = 1.0, False
    for p in parts:
        if p == names[0]:
            continue
        tm = TIME_TERM.fullmatch(p)
        if tm and tm.group(2) in DUR_UNITS:
            mult = 1.0
            for g in (tm.group(1), tm.group(3)):
                if g:
                    mult *= float(g)
            is_dur, scale = True, scale * DUR_UNITS[tm.group(2)] * mult
            continue
        if NUMBER.fullmatch(p):
            scale *= float(p.replace("_", ""))
            continue
        return None, f"兜底实参形态不认识：{fb[:48]}", None
    if name in ambiguous:
        return None, f"兜底常量 {name} 同名多值（歧义，不猜）", None
    if name not in consts:
        return None, f"兜底常量 {name} 的右值静态解不出", None
    k, v = consts[name]
    if scale != 1.0 or is_dur:
        if k not in ("num", "dur"):
            return None, f"兜底常量 {name} 不是数字，乘不了时间单位", None
        return ("dur" if (is_dur or k == "dur") else "num"), norm(float(v) * scale) if v else v, "A"
    return k, v, "A"


def fallback_sites(files, seed_pairs, ident_str, consts, ambiguous, ctors):
    """(group,key) -> [(kind, value, 出处, 形态)]；解不出的键返回原因表。"""
    sites: dict[tuple[str, str], list] = {}
    reasons: dict[tuple[str, str], str] = {}

    def resolve_target(coord, target, value, rel, form):
        """把 'const'/'field' 目标折成实际值；解不出按**键**记原因（不是按文件行，
        否则调用方拿 (group,key) 查不到这条，读数会显示"没有兜底点"而非"解不出"）。"""
        if target == "conflict":
            reasons.setdefault(coord, f"{form} @ {rel} 包装默认值冲突（不猜）：{value}")
            return None
        if target == "expression":
            kind, val, _ = resolve_fallback_arg(value, consts, ambiguous, [], [], 0)
            if kind is None:
                reasons.setdefault(coord, f"{form} @ {rel} {val}")
                return None
            return kind, val, rel, form
        if target == "field":
            ctor, field = value.split(".", 1)
            if (ctor, field) not in ctors:
                reasons.setdefault(coord, f"{form} @ {rel} 查不到 {ctor} 的字段 {field}")
                return None
            return (*ctors[(ctor, field)], rel, form)
        if value in ambiguous:
            reasons.setdefault(coord, f"兜底常量 {value} 同名多值（歧义，不猜）@ {rel}")
            return None
        if value not in consts:
            reasons.setdefault(coord,
                               f"兜底常量 {value} 的右值静态解不出（形态 {form}）@ {rel}")
            return None
        kind, val = consts[value]
        return (kind, val, rel, form)

    for path, text in files:
        lines = text.splitlines()
        funcs = func_index(lines)
        for m in GET_CALL.finditer(text):
            args = split_args(m.group(1))
            if len(args) < 4:
                continue
            g = resolve_str(args[1], ident_str)
            k = resolve_str(args[2], ident_str)
            if not g or not k or (g, k) not in seed_pairs:
                continue
            coord = (g, k)
            line_no = text.count("\n", 0, m.end())
            rel = f"{path}:{line_no + 1}"
            fb = args[3]
            # B3 优先于实参内容：这一行自己声明了有效默认值是结构体字段，
            # 兜底位写 0 / NaN / d.Field 都只是"把没配过的透传交给字段"。
            hit = b3_field_on_line(lines, funcs, line_no)
            if hit:
                got = resolve_target(coord, "field", hit, rel, "B3")
                if got:
                    add_site(sites, coord, got)
                    continue
                reasons.setdefault(coord, f"{rel} 兜底字段 {hit} 在构造函数里查不到")
                continue
            if fb == "0":
                hit = resolve_sentinel(lines, funcs, text, line_no)
                if hit is None:
                    # 0 不是哨兵而是兜底值本身（misc.upload_max_size_mb 的"0＝不覆盖"）
                    add_site(sites, coord, ("num", 0, rel, "A0"))
                    continue
                got = resolve_target(coord, hit[0], hit[1], rel, hit[2])
                if got:
                    # 只有完整包装体已证明 GetInt 值乘 time.Second，才能把
                    # duration 默认折回该整数读口的秒数。普通 duration 不猜量纲。
                    if m.group(0).startswith('GetInt(') and hit[2] == 'B2-positive-seconds' and got[0] == 'dur':
                        got = ('num', got[1], got[2], got[3])
                    add_site(sites, coord, got)
                else:
                    reasons.setdefault(coord, f"{rel} 兜底位是哨兵 0，之后解不出回落常量")
                continue
            kind, val, form = resolve_fallback_arg(fb, consts, ambiguous, lines, funcs, line_no)
            if kind == "field":
                got = resolve_target(coord, "field", val, rel, form)
                if got:
                    add_site(sites, coord, got)
                    continue
                reasons.setdefault(coord, f"{rel} 兜底字段 {val} 查不到构造点")
                continue
            if kind is None:
                reasons.setdefault(coord, f"{rel} {val}")
                continue
            add_site(sites, coord, (kind, val, rel, form))
    return sites, reasons


def add_site(sites, coord, entry):
    bucket = sites.setdefault(coord, [])
    if entry not in bucket:
        bucket.append(entry)


def resolve_str(raw: str, ident_str) -> str:
    if re.fullmatch(r'"[^"]*"', raw):
        return raw[1:-1]
    return ident_str.get(raw.rsplit(".", 1)[-1], "")


def compare_seed(coord: str, vtype: str, dv: str, kind: str, val, where: str):
    """一致返回 ""；不该硬比时返回 None（调用方归 NO_CONST）；否则一行 MISMATCH。"""
    def as_num(s):
        try:
            return float(str(s).replace("_", ""))
        except ValueError:
            return None

    if vtype == "bool":
        if kind != "bool":
            return None
        want = dv.strip().lower() in ("true", "1", "yes", "on")
        return "" if want == val else f"{where}: 种子 {dv} != 兜底 {val}"
    if vtype == "string":
        if kind != "str":
            return None
        return "" if dv == val else f"{where}: 种子 {dv!r} != 兜底 {val!r}"
    want = as_num(dv)
    if want is None:
        return None
    if vtype == "duration":
        if kind not in ("dur", "num"):
            return None
        # 兜底写裸数字时按秒处理（与 GetDuration 的"裸数字按秒"同一条口径）
        got = float(val)
        return "" if abs(got - want) < 1e-9 else f"{where}: 种子 {dv}s != 兜底 {norm_s(got)}s"
    if kind != "num":
        return None
    got = float(val)
    return "" if abs(got - want) < 1e-9 else f"{where}: 种子 {dv} != 兜底 {norm_s(got)}"


def check_bounds(coord: str, vtype: str, dv: str, mn: str, mx: str):
    """BAD_SEED：DefaultValue 过不了自己的 Min/Max（与 validateValue 同口径）。

    "同口径"是这里唯一的准绳：validateValue 只在 int / float / duration 三个分支里比 Min/Max，
    bool、string 不判，json 也**不判**——它的 json 分支只验 JSON 语法，压根不看边界。
    把 json 纳进数字判定会造出一条真实存在的假红：三条整份 JSON 的旋钮
    （permission.role_permissions_json / agent_llm.provider_failover_policy /
    embedding.global_override）都没配 Min/Max， DefaultValue 自然不是数字。
    反过来，int/float/duration 里非数字的 DefaultValue 必须继续判红：
    那种值存进库后 Get* 解析失败会静默回落代码兜底，页面显示的却是这份 DefaultValue。
    """
    if vtype in ("bool", "string", "json") or not dv:
        return ""

    def as_num(s):
        try:
            return float(str(s).replace("_", ""))
        except ValueError:
            return None

    v, lo, hi = as_num(dv), as_num(mn), as_num(mx)
    if v is None:
        return f"{coord} 的 DefaultValue {dv!r} 不是数字，过不了 Min/Max 校验"
    if lo is not None and v < lo:
        return f"{coord} 的 DefaultValue {dv} < Min {mn}"
    if hi is not None and v > hi:
        return f"{coord} 的 DefaultValue {dv} > Max {mx}"
    return ""


def main() -> int:
    strict = "--strict" in sys.argv
    refresh = "--refresh-baseline" in sys.argv
    seeds = REPO_ROOT / SEEDS_REL
    if not seeds.is_file():
        fail(f"种子文件不存在: {seeds}")
    seed_text = seeds.read_text(encoding="utf-8")
    entries = seed_entries(seed_text)
    if not entries:
        fail("种子文件里一条都没解出（本门没有可比对的对象，不是通过）")
    starts = len(ENTRY_START.findall(seed_text))
    if len(entries) != starts:
        fail(f"解析到 {len(entries)} 条，种子里却有 {starts} 个条目起点——"
             "有条目形状解不出来，本门的读数不可信")

    # 棘轮先读后扫：基线坏了要在几秒内出声。是否达标要等 `compared` 有值，判据在末尾。
    floor = None if refresh else load_baseline()

    files = collect_files()
    if not files:
        fail(f"{SCAN_REL} 下一个 Go 文件都没读到：环境或路径不对")
    ident_str = string_consts(files)
    consts, ambiguous = const_values(files)
    ctors = constructor_fields(files)
    fe_groups, fe_excluded = frontend_groups()
    seed_pairs = {(g, k) for g, k, *_rest in entries}
    sites, reasons = fallback_sites(files, seed_pairs, ident_str, consts, ambiguous, ctors)

    mismatch, bad_seed, noconst = [], [], []
    compared = retired = frontend_only = 0
    for g, k, vtype, dv, mn, mx, deprecated in entries:
        coord = f"{g}.{k}"
        if bad := check_bounds(coord, vtype, dv, mn, mx):
            bad_seed.append(bad)
        if deprecated:
            retired += 1
            continue
        # 每条种子只落进一格（可比对 / 无法比对 / 前端下发），对账才是可判的：
        # 一个键有多个兜底点时逐点都比、不一致逐条报，但计数按条目——按命中点计
        # 会让"可比对+无法比对+…"大于种子数（实测 120+87+11=218 就是这么来的）。
        skip_reasons = []
        comparable = False
        for kind, val, rel, form in sites.get((g, k), []):
            if kind == "str" and val == "":
                skip_reasons.append(f"兜底位是空串 @ {rel}：默认值就是种子行本身")
                continue
            if dv is None:
                skip_reasons.append("种子没有 DefaultValue 字段")
                continue
            verdict = compare_seed(coord, vtype, dv, kind, val,
                                   f"{coord} 形态 {form} @ {rel} 兜底 {norm_s(val)}")
            if verdict is None:
                skip_reasons.append(f"种子 {vtype} 与兜底 {kind} 量纲不同，不比对 @ {rel}")
                continue
            comparable = True
            if verdict:
                mismatch.append(verdict)
        if comparable:
            compared += 1
            continue
        if g in fe_groups and f"{g}.{k}" not in fe_excluded:
            frontend_only += 1
            continue
        why = (skip_reasons[0] if skip_reasons else
               reasons.get((g, k), "代码里没有该键的 Get* 兜底实参位"))
        noconst.append((coord, why))

    print(f"扫描面: 种子条目 {len(entries)} 条 / Go 文件 {len(files)} 个 / "
          f"常量池 {len(consts)} 个（同名歧义剔除 {len(ambiguous)}）/ "
          f"Default*Config 字段 {len(ctors)} 个 / 定位到兜底点的键 {len(sites)} 个")
    total = compared + len(noconst) + retired + frontend_only
    print(f"读数: 可比对 {compared}  无法比对 {len(noconst)}  前端下发 {frontend_only}  "
          f"已下架 {retired}  合计 {total}（应等于种子条目 {len(entries)}）")
    if total != len(entries):
        fail(f"分类对账失败：{compared}+{len(noconst)}+{retired}+{frontend_only} != "
             f"{len(entries)}（有条目既没被比对也没被归入某一类）")
    # 自身健康下界（不是内容判据，是"门还在不在干活"的判据）：
    # 本门的默认口径是 NO_CONST 不判红，于是**解析面坏掉时它会绿**——实测把
    # NUM_CONST 的正则去掉 re.M，常量池从 2817 掉到 0，读数变成
    # 「可比对 58 / 无法比对 128」却仍退 0。那 58 条是从结构体字段表来的，
    # 也就是说门已经只剩一层皮，而输出看起来像"少量解不出"。
    # 判据取"多数"而不是某个百分比：解不出的比比对的多 ⇒ 它不再是在核对默认值，
    # 是在给"解不出"打勾，当场停摆比放行一条真不一致更贵。
    if not consts:
        fail("常量池为空：Go 常量声明一条都没解析出来，本门的比对不成立（不是通过）")
    if len(noconst) > compared:
        fail(f"比对覆盖率失守：无法比对 {len(noconst)} > 可比对 {compared}——"
             "本门已在多数条目上放弃比对，请检查解析面（正则/扫描路径），"
             "而不是给这些条目挂豁免")
    for line in mismatch:
        print(f"  MISMATCH: {line}")
    for line in bad_seed:
        print(f"  BAD_SEED: {line}")
    for coord, why in sorted(noconst):
        print(f"  NO_CONST: {coord}  {why}")
    # 覆盖率棘轮（绝对下界）：上面两道"多数"下界拦得住"解析面整个坏掉"，
    # 拦不住"只剩一半覆盖面"——实测把常量声明正则还原成"行尾注释是必要条件"
    # 那一版（本轮真踩的坑），读数变成「可比对 95 / 无法比对 91」，91 < 95，
    # 门照样绿。低于基线 ⇒ rc=2（门坏了），不是 rc=1（内容红）：
    # 可比对条数掉下来本身不说明谁配错了，只说明本门看的东西变少了。
    if refresh:
        return refresh_baseline(compared)
    assert floor is not None
    if compared < floor:
        fail(f"覆盖率棘轮失守：本趟可比对 {compared} < {BASELINE_REL} 登记的 {floor}"
             f"（少 {floor - compared} 条）。要么本门的某条兜底形态不再被认出（修门），"
             "要么那些键的接线形状被换掉了（逐条确认为什么不再可比对），"
             "确认过才 `--refresh-baseline` 把基线改小并写明属于哪一种")
    if compared > floor:
        print(f"提示：可比对 {compared} 高于登记的棘轮 {floor}，"
              "跑 --refresh-baseline 抬上来（棘轮只挡下降，向上不判红）")
    if mismatch or bad_seed or (strict and noconst):
        print("❌ 参数中心默认值门未通过")
        return 1
    print(f"✅ 参数中心默认值门通过：{compared} 条比对的种子默认值都与代码兜底一致，"
          f"且全部过自己的 Min/Max")
    return 0


if __name__ == "__main__":
    sys.exit(main())
