#!/usr/bin/env python3
"""测试解引用门：用例不许在**被调函数能交回 (nil, nil)** 的返回值上"未判空即解引用"。

背景（2026-09-22 · 渠道审计批M-10 建门；来路登记在 `docs/architecture/CHANNEL_INTEGRATION_AUDIT_2026-09.md`
§23.16 第 8 条，判据收紧轨迹与全部取证在 §23.17）：
obs_config 那条链上实测过两次同款缺陷——产码 `GetDefault`／`GetByID` 在"没找到"时交回
`(nil, nil)`（把 `ErrRecordNotFound` 吞成成功），而用例的形状是

    cfg, err := repo.GetDefault(...)
    if err != nil { t.Fatalf(...) }        // ← 这道判空对 (nil, nil) 完全无效
    if cfg.IsDefault { ... }               // ← 这里 panic

`err != nil` 挡不住 `(nil, nil)`，所以这类用例**平时绿、真出错时把整个测试二进制带走**
（一条用例 panic ⇒ 同包后面未跑的腿一律不留痕，只剩一条 `--- FAIL`）。本门把"这种形状还在多少处"
从读码换成跑码，并锁住不再新增。

判据（三条，任一不成立即 rc=1）：
  1. 某测试文件的**确证站点**数（类型解析成功的那批）超过 `scripts/test-nil-deref.baseline`
     登记的数额，或出现基线里没有的新文件 ⇒ 新增红；
  2. 基线里登记的文件其现算数额**变小或归零** ⇒ STALE 红（防基线烂掉、也逼着把修掉的划掉）；
  3. 基线格式坏（列数不对／非数字／重复文件名）⇒ 红。

口径边界（按「门禁口径盲区」的规矩写明，别把"没数到"印成"没问题"）：
  - **必须解析到被调类型才算确证**。产码一侧的键是 `类型.方法名`（普通函数是裸名），测试一侧要先把
    接收者变量解析成类型（`x := NewFoo()`／`x = &Foo{}`／`var x *Foo`／`x.Foo()` 的同文件构造点）。
    这一条是**本门的存在前提**：早先按裸方法名相接得到 250 处，实测其中 `short_link_test.go` 的 26 处
    全是假阳——`internal/service` 里带裸 `return nil, nil` 的 `Create` 只有 `HTTPAfterSaleClient` 一个，
    而那 26 处的接收者是 `*ShortLinkService`。⇒ 名字相接在大包里等于噪声生成器，不收；
  - 接收者类型解析不出来的调用点归入**存疑**，只印数、不进基线、不判红（口径：宁可少锁，不制造假红）；
  - 产码一侧只认**字面** `return nil, nil`、且**首个返回类型是裸指针**的函数。交回 nil 切片／nil 映射的
    那一批（`GetShortTermMemory`、`Rerank`、`FindConversationIDsMissingInbox` …）不算本门口径：nil 切片上
    `range`／`len()` 安全，用例踩的是 `x[0]` 越界，那是"该断 `len(x)==0`"的另一类形状，混进来基线就
    数不出同一个缺陷。收紧的实测轨迹（产码函数键 ／ 确证站点；每档判据另做成变体重跑核对过，见 §23.17 第 2 段）：
      · 裸方法名相接（同包）、结果表不看形态：241 ／ 119（13 个文件）——`short_link_test.go` 一处中 26 条，
        全是跨类型同名，等于噪声生成器；
      · 键改成 `类型.方法名`、结果表不看形态：**268 ／ 43**（21 个文件）——`[]*KnowledgeDocument` 这类切片全进来；
      · 要求结果表**任意位置**含 `*`：153 ／ 9（5 个文件）——`[]*T` 仍被"含 `*`"放过，
        `knowledge_document_test.go:149` 逐条读源码核到假阳（用例只用 `len(got)`／`got[0]`）；
      · 只认**首项是裸指针**（`[]`／`map[`／`chan` 前缀一律排除）：**114 ／ 8**，这 8 处（QQ 4 + cond_tree 2 +
        repository 2）本轮全部就地判空收口，基线遂以**零条目**入库（现存 0）。
  - 产码一侧的 `return nil, err`／`return nil, errors.…`（交回真错误那一支）**不算**——那种形状下
    `err != nil` 就是判空；
  - "判过空"只认对被赋值变量本身的 `== nil`／`!= nil`、`assert/require.Nil/NotNil`；**不认** `err != nil`
    （这正是本门的存在理由）；
  - 站点作用域＝"该赋值行 → 下一个顶层 `func` 或行首 `}`"，段外的判空不算；
  - 左值被 `_` 丢弃、或赋值后在本段内从未出现 `v.`／`v[`／`*v` 的，不算站点；
  - 只扫同 package 目录内的 `*_test.go`；产码内部的自调用面、跨包调用面不在本门视野内。

⇒ 基线上的数字是**已确证的候选清单**，不是缺陷清单：多数站点由夹具保证走非 nil 路径。本门的承诺只有一句
  ——**"这种形状新增一处就红"**，不承诺"现存这些都要改"。

执行入口：本地 `python3 scripts/check-test-nil-deref.py`；接 CI 时**触发 paths 必须含本脚本与基线文件**
（否则改判据不触发这道门，同 platform `docs-link-check.yml` 的教训）。
"""

from __future__ import annotations

import re
import sys
from collections import defaultdict
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
SCAN_ROOT = REPO_ROOT / "user-server"
BASELINE = REPO_ROOT / "scripts" / "test-nil-deref.baseline"

# 产码：函数头（含方法接收者）与裸两条 nil 返回
FN_HEAD = re.compile(
    r"^func\s+(?:\(\s*\w+\s+\*?(?P<recv>[A-Za-z_]\w*)\s*\)\s*)?(?P<name>[A-Za-z_]\w*)\s*\("
)
NIL_NIL_RETURN = re.compile(r"^\s*return\s+nil\s*,\s*nil\s*$")

# 测试：`v, err := Callee(` ／ `v, _ := Callee(`，Callee 可带一层接收者
ASSIGN = re.compile(
    r"^(\s*)(?P<lhs>[A-Za-z_]\w*)\s*,\s*(?:err|_)\s*:?=\s*"
    r"(?:(?P<recv>[A-Za-z_]\w*)\s*\.\s*)?(?P<callee>[A-Za-z_]\w*)\s*\("
)
# 同文件里的构造点：接收者变量 → 类型
CTOR = re.compile(
    r"\b(?P<v>[A-Za-z_]\w*)\s*:?=\s*(?:&\s*|\*?\s*)?(?:(?P<t>[A-Za-z_]\w*)\s*\{|"
    r"(?P<f>New[A-Za-z_]\w*)\s*\()"
)
VAR_DECL = re.compile(r"\bvar\s+(?P<v>[A-Za-z_]\w*)\s+\*?(?P<t>[A-Za-z_]\w*)\b")
DEREF_TPL = r"(?:{v}\s*\.|{v}\s*\[|(?<![\w.])\*\s*{v}\b)"
NIL_CHECK_TPL = (
    r"(?:if\s+{v}\s*(?:==|!=)\s*nil"
    r"|(?:assert|require)\.(?:Nil|NotNil)\([^)]*\b{v}\b"
    r"|\b{v}\s*(?:==|!=)\s*nil)"
)
FUNC_END = re.compile(r"^(?:func\b|\})")


def strip_line_comments(text: str) -> str:
    """剥掉 `//` 行注释：注释里提一句"X 可能返回 nil"不能当判空。"""
    return re.sub(r"//[^\n]*", "", text)


# 结果表**首项**的形态：只认"裸指针"，即至多带一个名字前缀后紧跟 `*`。
FIRST_RESULT_POINTER = re.compile(r"^(?:\w+\s+)?\*")
# 显式排除的复合形态：它们在 `return nil, nil` 下踩的不是空指针，
# 而是 range／下标／收取值 —— 判据要写 `len(x)==0`，与本门是两类缺陷。
NON_POINTER_SHAPE = re.compile(r"^(?:\w+\s+)?(?:\[\]|map\[|chan |<-\s*chan\b)")


def result_segment(line: str, head: re.Match[str]) -> str:
    """函数头里**参数表之后**那段（结果表），没有结果表返回空串。"""
    # 从**方法名之后**起找参数表：从行首找会先撞上接收者那对括号，
    # 把 `func (s *SOPService) Create(req *CreateRequest) error` 里的指针参数误认成指针返回。
    start = line.find("(", head.end("name"))
    if start < 0:
        return ""
    depth = 0
    for i in range(start, len(line)):
        ch = line[i]
        depth += ch == "("
        depth -= ch == ")"
        if depth == 0:
            return line[i + 1 :].strip().lstrip("(").rstrip(")")
    return ""


def first_result_is_pointer(line: str, head: re.Match[str]) -> bool:
    """首个返回类型是否为裸指针。

    只锁返回类型为指针的产码函数：`return nil, nil` 交回 nil 切片／nil 映射时，
    `range`／`len()` 都安全，用例真正会踩的是 `x[0]` 这种"空切片下标越界"——那是另一类
    形状（该断 `len(x)==0` 而不是 `x == nil`），混进来会让基线数不出"同一个缺陷"。
    实测轨迹：结果表不看形态 43 处 ⇒ 要求"任意位置含 `*`" 9 处（`[]*KnowledgeDocument` 仍漏进来，逐条读源码
    核为假阳）⇒ 只认"首项是裸指针" 8 处，那 8 处已在本轮判空收口。
    """
    segment = result_segment(line, head)
    if not segment or NON_POINTER_SHAPE.match(segment):
        return False
    return bool(FIRST_RESULT_POINTER.match(segment))


def producers_by_dir() -> dict[Path, set[str]]:
    """dir → 可交回 (nil, nil) 且**首个返回类型是指针**的函数键集合。"""
    out: dict[Path, set[str]] = defaultdict(set)
    for go in SCAN_ROOT.rglob("*.go"):
        if go.name.endswith("_test.go") or "vendor" in go.parts:
            continue
        lines = strip_line_comments(go.read_text(encoding="utf-8", errors="replace")).splitlines()
        cur: str | None = None
        for ln in lines:
            head = FN_HEAD.match(ln)
            if head:
                recv, name = head.group("recv"), head.group("name")
                cur = f"{recv}.{name}" if recv else name
                if not first_result_is_pointer(ln, head):
                    cur = None
                continue
            if NIL_NIL_RETURN.match(ln) and cur:
                out[go.parent].add(cur)
    return out


def var_types(lines: list[str]) -> dict[str, str]:
    """同文件内的 变量 → 类型。`NewFoo()` 归到 `Foo`（构造函数命名约定）。"""
    out: dict[str, str] = {}
    for ln in lines:
        for m in CTOR.finditer(ln):
            v = m.group("v")
            t = m.group("t") or m.group("f")
            if not t:
                continue
            if t.startswith("New"):
                t = t[3:]
            out.setdefault(v, t)
        for m in VAR_DECL.finditer(ln):
            out.setdefault(m.group("v"), m.group("t"))
    return out


def scan(producers: dict[Path, set[str]]) -> tuple[dict[str, list[int]], dict[str, list[int]]]:
    """返回 (确证站点, 存疑站点)，均按 相对路径 → 行号列表。"""
    confirmed: dict[str, list[int]] = defaultdict(list)
    unresolved: dict[str, list[int]] = defaultdict(list)
    for dir_path, names in producers.items():
        if not names:
            continue
        # 本包生产者的**方法名**（键的点号后半段）：接收者类型解析不出来时，靠它区分
        # "本包确有同名生产者、只是这条调用点认不出类型"（存疑）与"压根不相干"（放过）。
        method_names = {n.rsplit(".", 1)[-1] for n in names}
        for test in sorted(dir_path.glob("*_test.go")):
            lines = strip_line_comments(
                test.read_text(encoding="utf-8", errors="replace")
            ).splitlines()
            types = var_types(lines)
            rel = str(test.relative_to(REPO_ROOT))
            for idx, line in enumerate(lines):
                m = ASSIGN.match(line)
                if not m:
                    continue
                lhs = m.group("lhs")
                if lhs == "_":
                    continue
                recv, callee = m.group("recv"), m.group("callee")
                if recv:
                    key = f"{types.get(recv, '')}.{callee}"
                    resolved = recv in types
                else:
                    key, resolved = callee, True
                if key not in names:
                    if resolved or callee not in method_names:
                        continue
                    # 接收者类型解析不出来，但本包确有同名方法是生产者 ⇒ 存疑
                    confirmed_hit = False
                else:
                    confirmed_hit = True
                scope: list[str] = []
                for nxt in lines[idx + 1 :]:
                    if FUNC_END.match(nxt):
                        break
                    scope.append(nxt)
                body = "\n".join(scope)
                pat = DEREF_TPL.format(v=re.escape(lhs))
                dm = re.search(pat, body)
                if not dm:
                    continue
                gm = re.search(NIL_CHECK_TPL.format(v=re.escape(lhs)), body)
                if gm and gm.start() < dm.start():
                    continue
                if resolved and confirmed_hit:
                    confirmed[rel].append(idx + 1)
                else:
                    unresolved[rel].append(idx + 1)
    return confirmed, unresolved


def load_baseline() -> dict[str, int]:
    out: dict[str, int] = {}
    if not BASELINE.exists():
        # 缺基线不算绿：一条站点都不报的"绿"和"门没东西可比"是两回事，
        # 少了这一条，误删/漏提交基线文件会把门变成空转。
        print(f"ENV-BROKEN 缺基线文件：{BASELINE}（可以是零条目，但不能不存在）")
        sys.exit(2)
    for raw in BASELINE.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) != 2 or not parts[1].isdigit():
            print(f"BASELINE-BAD 行（需 `路径<TAB>数额`）：{raw}")
            sys.exit(1)
        if parts[0] in out:
            print(f"BASELINE-DUP 文件重复登记：{parts[0]}")
            sys.exit(1)
        out[parts[0]] = int(parts[1])
    return out


def main() -> int:
    if not SCAN_ROOT.is_dir():
        print(f"ENV-BROKEN 找不到扫描根：{SCAN_ROOT}")
        return 2
    producers = producers_by_dir()
    confirmed, unresolved = scan(producers)
    baseline = load_baseline()
    live = {f: len(v) for f, v in confirmed.items()}
    sites = sum(live.values())
    unknown = sum(len(v) for v in unresolved.values())

    red: list[str] = []
    for f, n in sorted(live.items()):
        b = baseline.get(f)
        if b is None:
            red.append(f"NEW  {f} 现算确证 {n} 处，基线无登记")
        elif n > b:
            red.append(f"OVER {f} 现算 {n} > 基线 {b}（新增 {n - b} 处）")
    for f, b in sorted(baseline.items()):
        n = live.get(f, 0)
        if n < b:
            red.append(f"STALE {f} 基线 {b}，现算只剩 {n} ⇒ 把修掉的划掉，别留旧数额")

    top = ", ".join(f"{f}:{n}" for f, n in sorted(live.items(), key=lambda x: (-x[1], x[0]))[:6])
    print(
        f"产码交回 (nil, nil) 的函数键（类型.方法名，按包目录）＝"
        f"{sum(len(v) for v in producers.values())} · "
        f"测试确证站点＝{sites}（{len(live)} 个文件）· 存疑站点＝{unknown}"
    )
    if top:
        print(f"最密：{top}")
    if red:
        print("\n".join(red))
        print(f"红 {len(red)} 条 ⇒ 见脚本 docstring 的判据 1/2")
        return 1
    print(f"绿：确证站点 {sites} 与基线一致，无新增")
    return 0


if __name__ == "__main__":
    sys.exit(main())
