#!/usr/bin/env python3
"""G6：i18n 接入质量只许涨不许跌（三条棘轮 + 一条硬门）。

为什么需要它
------------
硬编码扫描量出前端 7237 处中文文案与 i18n 词条之间的巨大落差。这个数字大到
没法一次收敛，所以本门不比"绝对覆盖率"，只比**趋势**：

  - 存量太高 ⇒ CI 永远红 ⇒ 门形同虚设。这是覆盖面治理最常见的死法。
  - 所以口径是"棘轮"：以基线文件里的数值为下限，补齐词条/改对调用会让数值变好，
    而任何一次提交让它变差就判红。

实测的三条事实（本门就按这三条建）
------------------------------------
1. 本仓 i18n 词条用**点分 key**（`common.save` / `views.system.title`），
   装在 `user-web/src/i18n/locales/*.json`，9 种语言。
2. `t()` 的调用形态混着两种：
     - `t('common.save')` —— 真 i18n，178 个，**全部**能在 zh.json 解析到（纪律良好）
     - `t('AI 置信度阈值')` —— 把中文字面量直接当 key，**740 个**。这种查找必然
       miss，vue-i18n 回落成 key 本身，于是"中文看起来正常、8 种语言全是中文"。
       它比不写 i18n 更坏：不写至少还能被扫描器数出来，写成 `t()` 之后
       扫描器的 TUNE/CN 分类仍会算它是文案，但人 review 时会以为"这页已国际化"。
3. `zh.json` 1846 条里有 **1455 条没有任何静态 `t()` 引用**——词条库是批量
   生成后与真实页面脱节了。这些是死重量，不是覆盖率问题，但同样会误导人。
   其中 `menu.*` 那批不算：它们的消费方是拼接键 `t('menu.' + key)`（见
   `backend_menu_keys`），静态扫描看不见但页面确实在用，按后端注册表逐条豁免。

消费路径必须穷举，否则门在惩罚做对的人
----------------------------------------
`LOCALE_ORPHAN` 判的是「词条库里没有消费方的词条」。但"消费方"不止 `t('字面量')`
这一种形态，真实代码里至少还有三类在运行时确实被 t() 消费、静态扫描却看不见：

  - **对象属性里存键、渲染时再消费**：`useOnboarding.ts` 的
    `titleKey: 'setup.onboarding.module.<key>.step1.title'`（全族 setup.onboarding.*）。
  - **字符串拼接的键前缀**：`user-web/src/constants/role.js` 的 `'roleName.' + v`、
    `constants/enabled.js` 的三元 `'enabledLabel.enabled' : 'enabledLabel.disabled'`。
  - **键名来自后端注册表**：`menu.*`（见 `backend_menu_keys`）。

2026-10-20 实测踩坑：`4a60d3af` 修了「8 种非中文界面吐裸中文键」这个**真实缺陷**
（往 9 个语言包补进 287 条中文字面量键，那批键正被 `t('中文')` 消费）。
本门旧口径只把**点分**键算进"被引用"，于是这批正在使用的词条被判成孤儿，
`locale_orphans` 从 1453 涨到 1833，把门判红——惩罚的正是做对的人。
故本门的"被引用集合"必须穷举消费路径（见 `collect_literal_key_refs`）。

三条棘轮（rc=1）
----------------
  - `RESOLVED_RATIO`  真 i18n 键占比 = 可解析点分键 / 全部 t() 键。
                     不许低于基线。
  - `RAW_T_CALLS`     非点分 `t()` 调用的去重条数（当前 740）。不许增加——
                     新写的中文必须先进 locales 再按点分键引。
  - `LOCALE_ORPHAN`   locales 里无人引用的词条数（当前 1455，口径见
                     `backend_menu_keys`）。不许增加——
                     页面删了就别留词条，或者反过来：词条在库里却没引到页面，
                     说明页面在写死文案。

一条硬门
--------
  - `MISSING_LOCALE`  非基准语言的词条数少于基准语言 ⇒ 该语言用户看到混排。
     这条不棘轮：语言包不齐就是硬缺陷，量小且该一次修完。

后端为什么单独算
----------------
后端 `internal/pkg/i18n` 只有百来条 msgid，与扫描出的 9013 条 API_MESSAGE 差两个
数量级。拿前端口径套后端会把基线压到 0，永远是红的。所以后端只做**消息形状**
检查（msgid 数是否与上一版持平、9 种语言是否都在），不按「中文语句有没有进 i18n」
计——后者的收敛路径由本文件的 `RAW_T_CALLS` 同一把尺子量（后端同构，见下）。

基线：`scripts/i18n-coverage-baseline.json`（首次跑 `--update` 生成）。
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

BASELINE_REL = Path("scripts") / "i18n-coverage-baseline.json"
LOCALES_DIR = Path("user-web") / "src" / "i18n" / "locales"
WEB_ROOT = Path("user-web")
WEB_SRC = Path("user-web") / "src"
GO_I18N_DIR = Path("user-server") / "internal" / "pkg" / "i18n"
#: 后端菜单权威注册表（`var SystemMenuTree`）——`menu.*` 词条的真实消费方。
GO_MENU_REL = Path("user-server") / "internal" / "model" / "menu.go"

BASE_LOCALE = "zh"

#: t() 调用。**必须同时认 `$t()`**——实测 918 个去重键里 716 个走 `$t()`，
#: 只有 203 个走裸 `t()`（vue-i18n 组合式 API 的全局属性形态，在 `<script>` 与
#: 选项式组件里是主流写法）。只认裸 `t()` 会漏掉 78% 的 i18n 使用，而且漏得
#: 悄无声息：数字看着仍然是个正数，只是全错。
#:
#: 前导否定 (?<![A-Za-z0-9_.]) 排掉 `foo.t(...)` / `point.t(...)` 这类同名方法；
#: 排除 `.` 即可，不要把 `$` 排进去——`$` 正是我们要匹配的形态。
#:
#: **还必须认 `i18n.global.t('...')` / `i18n.t('...')`**：2026-10-20 实测本仓
#: 有 736 处调用走这个形态，是**最主要**的消费方式；而上面那条前导否定恰好把
#: 「接收者带点」的形态一并排掉了（`i18n.global` 的 `t` 前面正是 `.`），
#: 于是这 736 处整类不可见。后果：词条库里的键被判成"无人引用"（假红），
#: 而真正该被盯住的中文直传（`i18n.global.t('中文')`）反倒不被计数。
#: 排除同名方法的责任交给「接收者必须字面是 i18n / i18n.global」这一条。
T_CALL = re.compile(
    r"(?<![A-Za-z0-9_.])\$?t\(\s*'([^']*)'"
    r"|\bi18n(?:\.global)?\s*\.\s*\$?t\(\s*'([^']*)'"
)
#: 真 i18n key 的形状：至少两段、每段只含字母数字下划线
DOTTED_KEY = re.compile(r"^[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+$")
#: 键前缀形状：以 '.' 结尾、点前至少一段（排除 '.' / '...' 这类噪声字面量）
PREFIX_KEY = re.compile(r"^[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*\.$")
#: 引号字面量（单引号或双引号），用于抓「不是 t() 实参、但确实是 i18n 键」的写法
QUOTED = re.compile(r"'([^'\n]*)'|\"([^\"\n]*)\"")
# Vue/HTML 属性值（双引号包裹）捕获组是属性值本身；只匹配绑定/静态属性，
# 不跨行。例：:title="tmT('a.b')" / title="字面量"
ATTR_VALUE = re.compile(r'(?::[A-Za-z_][\w.-]*|[A-Za-z_][\w.-]*)="([^"\n]*)"')

SKIP_DIRS = {"node_modules", "dist", "coverage", ".git", ".nuxt"}
#: 参与「字面量键」扫描的文件后缀——覆盖前端全部源码与用例。
#: 不含 .md：文档里的示例键不是消费方。
TEXT_SUFFIXES = {".vue", ".js", ".ts", ".jsx", ".tsx", ".mjs", ".cjs", ".html", ".json"}



def fail(msg: str, code: int = 2):
    print(f"❌ {msg}")
    sys.exit(code)


def leaf_keys(node, prefix: str = "") -> list[str]:
    out: list[str] = []
    if isinstance(node, dict):
        for k, v in node.items():
            out += leaf_keys(v, f"{prefix}.{k}" if prefix else str(k))
    else:
        out.append(prefix)
    return out


def locale_keys(name: str) -> list[str]:
    p = REPO_ROOT / LOCALES_DIR / f"{name}.json"
    if not p.is_file():
        fail(f"语言包缺失: {p}")
    try:
        data = json.loads(p.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as e:
        fail(f"语言包解析失败 {name}.json: {e}")
    return leaf_keys(data)


def backend_menu_keys() -> set[str]:
    """后端菜单注册表里的 key，折算成它们在前端的真实词条键 `menu.<key>`。

    为什么必须有这一步：菜单在 RBAC 改造后由 `GET /api/menus` 下发，标题渲染走
    `t('menu.' + menu.key)`（user-web/src/layout/Layout.vue 与 SubMenuItem.vue）。
    `T_CALL` 只能抓 `t('字面量')`，抓不到这种拼接形态，于是每一条**合法在用**的菜单
    词条都被算成"无人引用"。后果不是数字难看：往菜单里加一项（后端注册表 + 9 个语言包
    各补一条，完全正确的做法）会让 `locale_orphans` 净增、把棘轮判红。
    2026-10-12 补 systemDictionaries / systemFeatureFlags 两条时实测：
    换 HEAD 语言包 rc=0（1661），换上补齐后的语言包 rc=1（1663）——门在惩罚做对的人。

    只豁免注册表点名的那些键：语言包里一条 `menu.xxx` 若不在注册表里，它照旧算孤儿。
    这样"菜单摘掉了但词条留着"仍会被数到，豁免不是把整个 menu 命名空间放行。
    注册表读不到时直接红：宁可门停摆，也不要"豁免集为空 ⇒ 全量放行"的假绿。
    """
    p = REPO_ROOT / GO_MENU_REL
    if not p.is_file():
        fail(f"后端菜单注册表缺失: {p}")
    src = p.read_text(encoding="utf-8", errors="replace")
    block = re.search(r"var\s+SystemMenuTree\s*=\s*\[\]SystemMenuNode\{([\s\S]*?)\n\}", src)
    if not block:
        fail(f"{p} 里找不到 var SystemMenuTree——豁免集无法确定，本门不能空跑")
    keys = set(re.findall(r'Key:\s*"([A-Za-z0-9_]+)"', block.group(1)))
    if len(keys) < 20:
        fail(f"{p} 只解析出 {len(keys)} 个菜单 key，疑似解析失效")
    return {f"menu.{k}" for k in keys}


def collect_t_call_keys() -> set[str]:
    base = REPO_ROOT / WEB_SRC
    if not base.is_dir():
        fail(f"前端源码目录不存在: {base}")
    out: set[str] = set()
    for p in base.rglob("*"):
        if p.suffix not in (".js", ".vue", ".ts"):
            continue
        if any(seg in p.parts for seg in SKIP_DIRS):
            continue
        try:
            t = p.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        for m in T_CALL.finditer(t):
            key = m.group(1) if m.group(1) is not None else m.group(2)
            # 伪键：`t('menu.' + key)` 这类拼接式调用会被正则截出前缀（'menu.'）。
            # 它不是一条真键（真键不以 '.' 结尾），拼接键由 prefix 展开单独处理。
            # **但 '...' 结尾是真键**（如 t('正在为您转接人工客服，请稍候...')）——
            # 判据必须与前端提取器 i18n-extract.cjs 同款，否则这条真键会被误杀，
            # 词条被当成死重量删掉，运行时回落成裸中文。
            if key and not (key.endswith(".") and not key.endswith("...")):
                out.add(key)
    return out


def collect_literal_key_refs() -> tuple[set[str], set[str], set[str]]:
    """扫出「以字面量形态出现」的 i18n 键引用。

    为什么必须多这一层：T_CALL 只认 `t('字面量')` 的直接实参，而真实代码里
    至少还有三类拼法在运行时确实被 t() 消费、静态扫描却看不见：

      - **对象属性里存键、渲染时再消费**：`useOnboarding.ts` 的
        `titleKey: 'setup.onboarding.module.<key>.step1.title'`（整族 setup.onboarding.*）。
      - **字符串拼接的键前缀**：`user-web/src/constants/role.js` 的 `'roleName.' + v`、
        `constants/enabled.js` 的三元 `'enabledLabel.enabled' : 'enabledLabel.disabled'`。
      - **键名来自后端注册表**：`menu.*`（见 `backend_menu_keys`）。

    漏掉它们的后果不是"数字难看"，是**门在惩罚做对的人**——见文件头
    「消费路径必须穷举」一节。

    口径：返回 (整键集合, 键前缀集合, 全部引号字面量集合)。
      - 整键：源码里出现过的、且形状是点分键的引号字面量；
      - 前缀：以 '.' 结尾的引号字面量（'roleName.'），由调用方展开成该前缀下的全部词条；
      - 全部字面量：交给调用方与词条库 leaf 求交——中文整键（'图片' 这类
        "先存后消费"的键）由那一层收口。返回原始集合而不是在函数内求交，
        是因为本函数不持有词条库，且求交口径（leaf 完全相等）属于 main 的
        「消费路径」定义。

    必须是**完全相等**而不是子串包含：`ab.start` 是 `ab.startDate` 的子串，
    用包含判会把 `ab.start` 这条真死词条误判成在用。

    扫描面：user-web/ 下全部文本文件（排除 node_modules/dist/coverage/.git 与
    语言包自身），含 tests/ —— 用例里断言过的键同样算"在用"。
    """
    base = REPO_ROOT / WEB_ROOT
    if not base.is_dir():
        fail(f"前端工程目录不存在: {base}")
    literals: set[str] = set()
    for p in base.rglob("*"):
        if not p.is_file() or p.suffix not in TEXT_SUFFIXES:
            continue
        if any(seg in p.parts for seg in SKIP_DIRS):
            continue
        if p.parent == REPO_ROOT / LOCALES_DIR:
            continue
        try:
            t = p.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        for m in QUOTED.finditer(t):
            literals.add(m.group(1) if m.group(1) is not None else m.group(2))
        # HTML/Vue 属性绑定里的单引号键（:title="tmT('xxx.yyy')" 这类）：
        # QUOTED 从外层双引号起配，内层单引号键整个被吞，键就"消失"了——
        # 而它运行时确实被 t 系消费。先把属性值整段抠出来，再在内段里跑
        # 一遍 QUOTED，让内层单引号字面量重新可见。只放行 :x="..." 形态，
        # 不放开全文子串，避免把注释里的键也捞成在用。
        for attr in ATTR_VALUE.finditer(t):
            for m in QUOTED.finditer(attr.group(1)):
                literals.add(m.group(1) if m.group(1) is not None else m.group(2))
    exact = {lit for lit in literals if DOTTED_KEY.match(lit)}
    prefixes = {lit for lit in literals if PREFIX_KEY.match(lit)}
    return exact, prefixes, literals


def go_msgid_counts() -> dict[str, int]:
    """后端 i18n 包的 msgid 条数，按文件分开统计。

    口径只到"形状"：msgid 有没有增、9 种语言配不配齐。真要收敛后端 9013 条
    API_MESSAGE 中文，是另一个量级的工作，本门不假装一次做完。
    """
    base = REPO_ROOT / GO_I18N_DIR
    out: dict[str, int] = {}
    if not base.is_dir():
        return out
    for p in sorted(base.glob("*.go")):
        if p.name.endswith("_test.go"):
            continue
        try:
            t = p.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        # msgid 形态：`"xxx.yyy": "中文…"` 或 `XxxMsg = "中文…"`
        out[p.name] = len(re.findall(r'^\s*(?:\w+\s*=\s*)?"[^"]{2,}"\s*:', t, re.M))
    return out


def main() -> int:
    update = "--update" in sys.argv

    t_keys = collect_t_call_keys()
    if not t_keys:
        fail("一个 t() 调用都没扫到：T_CALL 正则或路径不对。"
             "本门此刻等同于空跑，不能绿")

    dotted = {k for k in t_keys if DOTTED_KEY.match(k)}
    raw = t_keys - dotted

    locales = sorted(p.stem for p in (REPO_ROOT / LOCALES_DIR).glob("*.json"))
    keys_per_locale = {lc: set(locale_keys(lc)) for lc in locales}
    base_keys = keys_per_locale[BASE_LOCALE]

    # 点分键是否真能在词条库里解析到 —— 解析不到的话写了 i18n 也等于没写
    unresolvable = sorted(dotted - base_keys)
    resolved = dotted & base_keys
    resolved_ratio = round(len(resolved) * 100.0 / len(t_keys), 2) if t_keys else 0.0

    # 词条库里"有消费方"的集合：四条消费路径全算（口径见文件头）。
    #   1) t('字面量') 的直接实参（含中文字面量键——4a60d3af 补的那 287 条正在被消费）
    #   2) 后端菜单注册表下发的 menu.*
    #   3) 源码里以引号字面量出现的整键（titleKey/descriptionKey 这类"先存后消费"）
    #   4) 源码里以引号字面量出现的键前缀（'roleName.' 拼接）
    #   5) 源码里以引号字面量出现、且与词条库 leaf 完全相等的**中文整键**
    #      （2026-10-23 002-multi-type-messages 补：List.vue 的 MSG_TYPE_LABELS
    #      把 '图片'/'视频' 等键存进对象、模板 $t(typeLabel(msg)) 动态消费——
    #      与 titleKey 同一形态的"先存后消费"，只是键是中文整键。不认它们
    #      就会把正在消费的词条判成孤儿，门又在惩罚做对的人。）
    lit_exact, lit_prefixes, lit_all = collect_literal_key_refs()
    lit_exact |= {lit for lit in lit_all if lit in base_keys}
    prefix_expanded = {k for k in base_keys
                        if any(k.startswith(p) for p in lit_prefixes)}
    referenced = t_keys | backend_menu_keys() | lit_exact | prefix_expanded
    orphan_locale = sorted(base_keys - referenced)
    missing_locale = sorted(
        lc for lc in locales if lc != BASE_LOCALE and len(keys_per_locale[lc]) < len(base_keys))

    go_counts = go_msgid_counts()

    print(f"扫描面: t() 去重键 {len(t_keys)} 个 / locales {len(locales)} 种 / "
          f"{BASE_LOCALE} 词条 {len(base_keys)} 条 / 后端 i18n {len(go_counts)} 文件")
    print(f"读数: 真 i18n 键={len(dotted)}（其中解析不到词条库的={len(unresolvable)}）  "
          f"中文字面量直传={len(raw)}  词条库无人引用={len(orphan_locale)}")
    print(f"      resolved_ratio={resolved_ratio}%  go_msgid={sum(go_counts.values())}")
    print(f"      消费路径分解: t()直参命中词条={len(t_keys & base_keys)} / "
          f"后端菜单注册表={len(backend_menu_keys() & base_keys)} / "
          f"字面量整键={len(lit_exact & base_keys)} / 键前缀展开={len(prefix_expanded)}")

    if unresolvable:
        print(f"  UNRESOLVABLE: {len(unresolvable)} 个点分键在 {BASE_LOCALE}.json 里没有对应词条，"
              f"调用处等于没接 i18n。样本：{unresolvable[:5]}")
    if missing_locale:
        detail = {lc: len(keys_per_locale[lc]) for lc in missing_locale}
        print(f"  MISSING_LOCALE: 词条数少于 {BASE_LOCALE} 的语言 {detail}")

    baseline_path = REPO_ROOT / BASELINE_REL
    baseline = {}
    if baseline_path.is_file():
        try:
            baseline = json.loads(baseline_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as e:
            fail(f"基线文件读不出来 {BASELINE_REL}: {e}")

    snapshot = {
        "_readme": "G6 i18n 棘轮基线，只许涨不许跌。刷新前先确认这次是"
                   "「补齐了词条 / 把中文直传改成点分键」而不是"
                   "「把还在用的文案删掉了」。",
        "_口径说明_2026_10_20": "本基线在 2026-10-20 重新测过一次：扫描器的"
                              "「被引用集合」此前漏认两类消费形态（i18n.global.t() "
                              "736 处调用、titleKey/roleName. 这类字面量键），"
                              "且只把点分键算作「被引用」，于是 4a60d3af 补进来的"
                              "287 条正在被消费的中文字面量键被误判成孤儿、门判红。"
                              "修正后重测：locale_orphans 1833→0（真清干净了 555 条"
                              "死词条），raw_t_calls 740→1167（不是新增，是那 736 处"
                              "i18n.global.t() 本来就在、只是从前看不见）。"
                              "本次刷新的是**测量口径**，不是放过新增。",
        "resolved_ratio": resolved_ratio,
        "t_call_keys": len(t_keys),
        "dotted_keys": len(dotted),
        "raw_t_calls": len(raw),
        "locale_orphans": len(orphan_locale),
        "base_locale_entries": len(base_keys),
    }

    if update:
        snapshot["locale_counts"] = {lc: len(v) for lc, v in keys_per_locale.items()}
        baseline_path.write_text(
            json.dumps(snapshot, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print(f"  基线已刷新 -> {BASELINE_REL}")
        return 0

    if not baseline:
        print("  首次运行、无基线可比。跑 `python3 scripts/check-i18n-coverage.py --update` 建基线。")
        return 0

    regressions: list[str] = []
    for field, better in (("resolved_ratio", "higher"), ("raw_t_calls", "lower"),
                          ("locale_orphans", "lower")):
        want = baseline.get(field)
        got = snapshot[field]
        if want is None:
            continue
        worse = got < want if better == "higher" else got > want
        if worse:
            regressions.append(
                f"  {field} {got} 比基线 {want} 变差（方向：{better} 更好）")

    if unresolvable or missing_locale or regressions:
        for line in regressions:
            print(line)
        print("❌ i18n 门未通过：新写的文案必须先进 locales 再按点分键引；"
              "页面删了就别留词条")
        return 1
    print("✅ i18n 门通过：真 i18n 占比不低于基线，无新增中文直传与孤儿词条，各语言包齐整")
    return 0


if __name__ == "__main__":
    sys.exit(main())