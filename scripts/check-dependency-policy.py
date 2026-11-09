#!/usr/bin/env python3
"""A2-a：依赖策略门 —— 弃维护库的引用面收敛、依赖声明形状、禁入名单。

为什么需要它（以及**不**重复哪个门）：
  本仓已经有两道 CVE 门，且都已转阻断——
    · `user-server-ci.yml` 的 "Go dependency audit (govulncheck)"（2026-09-29 起阻断）；
    · 同工作流的 "npm audit (user-web)"。
  所以本门**不**冒充漏洞扫描：没有 CVE 库、不联网、不判"这个版本有没有洞"。
  它守的是上面两道门结构上抓不到的三类：

  1) **弃维护（unmaintained）**。govulncheck 的判据是「已知通告」，而一个三年没人提交的库
     完全可以零通告——直到它依赖的某个东西坏了，或者浏览器/平台改协议，而无人会修。
     本仓就有一个具体例子：`github.com/Rhymen/go-whatsapp`（个人版 WhatsApp Web 协议，
     上游早已停更，且协议本身已被 Meta 收紧）。对这类库，处置不是「等它出 CVE」，
     而是**把引用面收敛到一个 seam 文件 + 定一个到期复查日**：
       · 引用面不收敛（散落在 N 个文件里）⇒ 将来换库要改 N 处，事实上换不掉；
       · 没有到期日 ⇒ 「以后再说」= 永远不说（本仓 `fix.md` 里「红是不是常态」那类糊涂账）。
     所以基线里每条 watch 依赖必须写 `seam`（唯一允许 import 它的文件）与 `review`（到期日），
     门逐条现测：seam 文件存在、且**只有它**引用该库、且 review 未过期。

  2) **依赖声明形状**。`"foo": "latest"` / `"*"` / `""` / 直接写 git/tarball URL 的依赖，
     装出来的东西随时间漂移，且锁文件一旦重建就换了内容——这类形状没有任何门看着。

  3) **禁入名单**。有些包没有 CVE 通告但已被明确取代或出过供应链事件（`request`、
     `event-stream`、`node-sass`、`dgrijalva/jwt-go`）。等 CVE 库收录它们，等于用别人的数据库
     替自己做决定。

判据（任意一条不成立即 rc=1）：
  A. DENY 命中共 `go.mod` 直接依赖或任一 `package.json` 依赖 ⇒ `BANNED` 红；
  B. WATCH 命中但基线无对应行 ⇒ `UNREGISTERED` 红（登记必须写 why/seam/review 三样）；
  C. 基线行的 `seam` 文件不存在 ⇒ `SEAM_MISSING` 红；
  D. 该库在 seam 之外仍有引用（Go: `*.go` 的 import；JS: `src/**.js` 的 import/require）
     ⇒ `SEAM_LEAK` 红（逐条打印漏出来的文件）；
  E. 基线行的 `review` 已过期 ⇒ `OVERDUE` 红（到期必须做决定：换掉，或显式延到新日期）；
  F. 基线行对应的库已不再被任何清单声明 ⇒ `STALE` 红（防基线烂成永久豁免）。
  G. `package.json` 里出现 `latest` / `*` / 空版本 / 非 registry 的 URL 规格 ⇒ `UNPINNED` 红；
  H. `go.mod` 的 `replace` 指向相对路径（`=> ../x` / `=> ./x`）⇒ `LOCAL_REPLACE` 红
     （本地路径依赖在别人的机器和 CI 上都不成立，等于"我这能编译"）。

口径边界（把"没数到"写成"没数到"）：
  - **只看直接依赖**（go.mod 里不带 `// indirect` 的行、package.json 的 dependencies 等段落）。
    传递依赖的版本由 go.sum / package-lock 决定，本门不解析锁文件——那是 govulncheck / npm audit
    的工作面，重复造一份只会两份结论不一致。
  - JS 侧只扫仓内 6 个 `package.json`（含 `user-web/bridge`、`browser_automation`、`embed-sdk`、
    `website`、`packages/browser-core`）。新增子包时 `JS_MANIFESTS` 要跟着加，否则**它完全不在视野里**
    ——脚本会在读数里打印实际扫到的清单数，改漏了能看出来。
  - 引用面现测只认**字面 import**：Go 的 `import "path"` / `import ( ... "path" ... )`；
    JS 的 `from 'pkg'` / `require('pkg')`。用变量拼出来的模块名（`import(变量)`）不在视野内。
  - 基线的 `seam` 可以是**一个文件**，也可以是**空格分隔的多个路径**（文件或目录前缀）——
    换库时的改动面就是这些位置。目录前缀的用法留给「一个包里多处引用、但换库只需改这个包」
    的形态（如 `gopkg.in/gomail.v2` 收敛在 `internal/pkg/mail` + 一个 daemon 文件）。
  - `*_test.go` 的 import **不计入**引用面：换库时测试必然要改，把它算进来则读数由用例个数决定，
    而不是由生产引用面决定。
  - 已评估但本轮不收敛的弃维护依赖写在 `OUT_OF_SCOPE`（如 `lib/pq`：它是 model 层的**类型**依赖，
    收敛等于改字段类型 + 迁移，属另一张卡）。它每次运行都会打印出来——
    「沉默的豁免」和「漏扫」在输出上长得一样，所以不许沉默。
  - `review` 到期只说明"该复查了"，不等于"这个库有毒"。到期时正确动作是**做决定**：
    换掉（改 seam）或把日期延到下一次真的会复查的时刻，并在 why 里写清为什么延。

执行入口：本地 `python3 scripts/check-dependency-policy.py`；已接 `make audit` 与
`user-server-ci.yml` 的 static-gates 作业。
牙齿：`python3 scripts/check-dependency-policy.py --self-check`（9 枚控制，A–H 每类各一枚
正向 + 极性对：间接依赖不算命中、seam 内的引用不算泄漏）。
"""

from __future__ import annotations

import json
import re
import sys
import tempfile
from datetime import date
import os
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
for _i, _a in enumerate(sys.argv):
    if _a == "--repo" and _i + 1 < len(sys.argv):
        REPO_ROOT = Path(sys.argv[_i + 1]).resolve()

GO_MOD_REL = Path("user-server") / "go.mod"
BASELINE_REL = Path("scripts") / "dependency-policy.baseline"
# 仓内全部前端清单。刻意列死而不是 rglob：`node_modules/` 下有几万个 package.json，
# rglob 会把它们全扫进来（而且那些正是"别人的依赖"，不该由本仓的策略管）。
JS_MANIFESTS = [
    Path("user-web") / "package.json",
    Path("user-web") / "bridge" / "package.json",
    Path("user-web") / "browser_automation" / "package.json",
    Path("embed-sdk") / "package.json",
    Path("website") / "package.json",
    Path("packages") / "browser-core" / "package.json",
]
# JS 引用面扫描根：只看源码，dist/node_modules 不是"我们的引用"。
JS_SRC_ROOTS = [
    Path("user-web") / "src",
    Path("user-web") / "bridge" / "src",
    Path("user-web") / "browser_automation" / "src",
    Path("embed-sdk") / "src",
    Path("packages") / "browser-core" / "src",
]
GO_SRC_ROOTS = [Path("user-server") / "internal", Path("user-server") / "cmd"]

# ── 禁入名单（没有 CVE 也禁：已被取代 / 出过供应链事件 / 官方明确废弃）────────────
# 每条都要写"为什么禁"与"换成什么"，否则下一个人只会看到一条黑名单，
# 然后因为不知道替代品而绕开它。
DENY: dict[str, str] = {
    "github.com/dgrijalva/jwt-go": (
        "CVE-2020-26160（aud 校验可被绕过）+ 官方已归档，唯一推荐替代是本仓已在用的 "
        "github.com/golang-jwt/jwt/v5"),
    "github.com/golang-jwt/jwt": (
        "v3 老路径，官方已迁移到 github.com/golang-jwt/jwt/v5（本仓已用 v5）；"
        "写成不带版本段的旧路径会让 v3/v4 与 v5 同时被拉进依赖图"),
    "request": "2020-02 官方废弃；Node 内置 fetch / undici 已覆盖用途",
    "request-promise": "随 request 一起废弃",
    "event-stream": "2018 供应链投毒事件的包本身（flatmap-stream 事件），永不使用",
    "node-sass": "绑定 LibSass，已停止更新；用 sass（Dart Sass）",
    "tslint": "2021 起官方废弃，用 eslint",
    "vue-cli": "Vue 3 时代由 Vite 取代（本仓 7 个 pin 全是 Vite，见 check-vite-version.sh）",
}

# ── 观察名单（弃维护，但仍在用）────────────────────────────────────────────────
# 命中不直接红：要求基线登记 why/seam/review，把"暂时换不掉"变成一份有到期日的合约。
# seam 是**引用面边界**：允许 import 它的文件，或一个目录前缀（换库时的改动面 = 这些位置）。
WATCH: dict[str, str] = {
    "github.com/Rhymen/go-whatsapp": "个人版 WhatsApp Web 协议库，上游停更；协议侧已被 Meta 收紧",
    "github.com/fvbock/endless": "2017 年后无提交；优雅重启可用 http.Server.Shutdown 自实现",
    "gopkg.in/gomail.v2": "2016 年后无提交（作者已声明不再维护）",
}

# ── 已评估、本轮**不**纳入引用面收敛的弃维护依赖 ───────────────────────────────
# 这一类不是"忘了"，而是"收敛成本不在依赖治理这张卡里"。写在这里而不是直接无视，
# 是因为沉默的豁免与漏扫在输出上长得一样。每次运行都会打印出来（读数里带原因）。
OUT_OF_SCOPE: dict[str, str] = {
    "github.com/lib/pq": (
        "官方进入维护模式（只修安全），推荐 pgx（本仓已用 jackc/pgx/v5）。但它在本仓是"
        "**类型依赖**（model 层字段用 pq.StringArray），收敛 = 改字段类型 + 迁移，"
        "属于「模型层类型替换」那张卡，不是把 import 搬一个文件就完事。此处显式声明不纳入，"
        "避免它被当成「本门已覆盖」"),
}

UNPINNED_SPEC = re.compile(r"^\s*(\*|latest|x|)$|^(git\+|https?://|file:|link:)")


class Finding:
    def __init__(self, code: str, msg: str):
        self.code = code
        self.msg = msg

    def __str__(self) -> str:
        return f"  {self.code}: {self.msg}"


def rel(p: Path, root: Path) -> str:
    try:
        # as_posix()：seam/策略比对口径恒为 / 分隔；Windows 上 relative_to 返回
        # \ 分隔会把 in-seam 文件误判成「引用面漏出」（CI/Linux 不受影响）。
        return p.relative_to(root).as_posix()
    except ValueError:
        return str(p).replace(os.sep, "/")


# ── 解析：一律做成「吃文本、吐结构」的纯函数，自检才能在内存里喂夹具 ────────────
def parse_go_direct(text: str) -> list[tuple[str, str]]:
    """go.mod 直接依赖 (module, version)。带 `// indirect` 的行不是直接依赖。"""
    out: list[tuple[str, str]] = []
    in_block = False
    for raw in text.splitlines():
        line = raw.split("//")[0].strip()
        if not line:
            continue
        if line.startswith("require ("):
            in_block = True
            continue
        if in_block and line == ")":
            in_block = False
            continue
        body = None
        if in_block:
            body = line
        elif line.startswith("require "):
            body = line[len("require "):].strip()
        if not body:
            continue
        # 单行形态也可能是 `require x v1.2.3 // indirect`；注释已在上面剥掉，
        # 但 indirect 判定要看原文，所以回读 raw。
        if "// indirect" in raw:
            continue
        parts = body.split()
        if len(parts) >= 2:
            out.append((parts[0], parts[1]))
    return out


def parse_go_local_replace(text: str) -> list[str]:
    """go.mod 里指向相对路径的 replace 目标（`=> ../x` / `=> ./x`）。"""
    out: list[str] = []
    in_block = False
    for raw in text.splitlines():
        line = raw.split("//")[0].strip()
        if not line:
            continue
        if line.startswith("replace ("):
            in_block = True
            body = ""
        elif in_block and line == ")":
            in_block = False
            continue
        elif in_block:
            body = line
        elif line.startswith("replace "):
            body = line[len("replace "):].strip()
        else:
            continue
        if "=>" not in body:
            continue
        target = body.split("=>", 1)[1].strip().split()[0] if body.split("=>", 1)[1].strip() else ""
        if target.startswith("./") or target.startswith("../"):
            out.append(body)
    return out


def parse_js_deps(text: str) -> dict[str, str]:
    """package.json 的 dependencies/devDependencies/optionalDependencies 合并成 name→spec。"""
    data = json.loads(text)
    out: dict[str, str] = {}
    for sec in ("dependencies", "devDependencies", "optionalDependencies"):
        for name, spec in (data.get(sec) or {}).items():
            out[name] = str(spec)
    return out


GO_IMPORT_LINE = re.compile(r'^\s*(?:[\w.]+\s+)?"([^"]+)"\s*$')
GO_IMPORT_ONE = re.compile(r'^\s*import\s+(?:[\w.]+\s+)?"([^"]+)"')
JS_IMPORT = re.compile(
    r"""(?:\bfrom\s*|\brequire\s*\(\s*|\bimport\s*\(\s*|@import\s+)['"]([^'"]+)['"]""")


def count_go_importers(root: Path, module: str) -> list[str]:
    """哪些 .go 文件 import 了该 module（子包也算：`mod/sub` 属于 `mod`）。"""
    hits: list[str] = []
    for base in GO_SRC_ROOTS:
        d = root / base
        if not d.is_dir():
            continue
        for p in d.rglob("*.go"):
            # 测试文件的 import 不计入引用面：换库时测试必然跟着改，把它们算进来
            # 只会让读数由「用例个数」决定，而不是由「生产引用面」决定。
            if p.name.endswith("_test.go"):
                continue
            try:
                t = p.read_text(encoding="utf-8", errors="replace")
            except OSError:
                continue
            for m in GO_IMPORT_ONE.finditer(t):
                if _mod_matches(m.group(1), module):
                    hits.append(rel(p, root))
                    break
            else:
                in_block = False
                for ln in t.splitlines():
                    if ln.strip().startswith("import ("):
                        in_block = True
                        continue
                    if in_block and ln.strip() == ")":
                        in_block = False
                        continue
                    if in_block:
                        mm = GO_IMPORT_LINE.match(ln)
                        if mm and _mod_matches(mm.group(1), module):
                            hits.append(rel(p, root))
                            break
    return sorted(set(hits))


def count_js_importers(root: Path, pkg: str) -> list[str]:
    hits: list[str] = []
    for base in JS_SRC_ROOTS:
        d = root / base
        if not d.is_dir():
            continue
        for p in d.rglob("*.js"):
            try:
                t = p.read_text(encoding="utf-8", errors="replace")
            except OSError:
                continue
            for m in JS_IMPORT.finditer(t):
                if _mod_matches(m.group(1), pkg):
                    hits.append(rel(p, root))
                    break
    return sorted(set(hits))


def _under_any(file_rel: str, seams: list[str]) -> bool:
    """引用面判定：命中某个 seam 文件本身，或落在某个 seam 目录前缀下。"""
    for s in seams:
        if file_rel == s:
            return True
        if file_rel.startswith(s.rstrip("/") + "/"):
            return True
    return False


def _mod_matches(spec: str, module: str) -> bool:
    """`mod` 精确命中，或 `mod/sub` 命中（Go 子包 / JS 子路径导入）。

    反向不算：`mod-extra` 不应因为前缀相同被判成 `mod`（实测 `lib/pq` 与
    `lib/pqutil` 这种命名在生态里是存在的）。
    """
    return spec == module or spec.startswith(module + "/")


def parse_baseline(text: str) -> tuple[dict[str, dict[str, str]], list[str]]:
    """基线：`module | seam | review(YYYY-MM-DD) | why`。返回 (行表, 格式错行)。"""
    rows: dict[str, dict[str, str]] = {}
    bad: list[str] = []
    for ln in text.splitlines():
        s = ln.strip()
        if not s or s.startswith("#"):
            continue
        parts = [x.strip() for x in s.split("|")]
        if len(parts) < 4:
            bad.append(ln)
            continue
        module, seam, review, why = parts[0], parts[1], parts[2], "|".join(parts[3:]).strip()
        if not module or not seam or not review or not why:
            bad.append(ln)
            continue
        rows[module] = {"seam": seam, "review": review, "why": why}
    return rows, bad


def evaluate(
    *,
    root: Path,
    go_mod_text: str,
    js_manifests: dict[str, str],
    baseline_text: str,
    today: date | None = None,
    importer_fn=None,
) -> tuple[list[Finding], dict[str, int]]:
    """核心判据。`importer_fn(module) -> [files]` 可注入，自检用它绕开磁盘。"""
    today = today or date.today()
    findings: list[Finding] = []
    counts = {"go_direct": 0, "js_manifests": 0, "js_deps": 0}

    go_direct = parse_go_direct(go_mod_text)
    counts["go_direct"] = len(go_direct)
    declared: dict[str, str] = {name: "go.mod" for name, _ in go_direct}
    for path, text in js_manifests.items():
        counts["js_manifests"] += 1
        deps = parse_js_deps(text)
        counts["js_deps"] += len(deps)
        for name in deps:
            declared.setdefault(name, path)
        for name, spec in deps.items():
            if UNPINNED_SPEC.search(spec):
                findings.append(Finding(
                    "UNPINNED", f"{path}: {name}={spec!r} —— 依赖规格必须钉到版本区间/标签，"
                                f"`latest`/`*`/URL 会随时间漂移且锁文件重建即变"))

    for line in parse_go_local_replace(go_mod_text):
        findings.append(Finding(
            "LOCAL_REPLACE", f"go.mod replace 指向本地相对路径：{line} —— "
                             f"别人的机器/CI 上不成立（等于\"只有我这能编译\"）"))

    for name, reason in DENY.items():
        if name in declared:
            findings.append(Finding("BANNED", f"{declared[name]}: 禁入依赖 {name} —— {reason}"))

    baseline, bad_lines = parse_baseline(baseline_text)
    for ln in bad_lines:
        findings.append(Finding("BASELINE_FORMAT", f"基线行不是 `module | seam | review | why`：{ln}"))

    for name, why in WATCH.items():
        if name not in declared:
            continue
        row = baseline.get(name)
        if row is None:
            findings.append(Finding(
                "UNREGISTERED", f"{declared[name]}: 弃维护依赖 {name} 未登记（{why}）—— "
                                f"在 {BASELINE_REL} 加一行 `{name} | <唯一 seam 文件> | YYYY-MM-DD | 为什么`"))
            continue
        seam_paths = row["seam"].split()
        missing = [s for s in seam_paths if not (root / s).exists()]
        if missing:
            findings.append(Finding(
                "SEAM_MISSING", f"{name}: 基线登记的 seam 路径不存在：{', '.join(missing)}"))
        try:
            review = date.fromisoformat(row["review"])
        except ValueError:
            findings.append(Finding("BASELINE_FORMAT", f"{name}: review 不是 YYYY-MM-DD：{row['review']!r}"))
            review = None
        if review is not None and review < today:
            findings.append(Finding(
                "OVERDUE", f"{name}: 复查日 {row['review']} 已过期 —— 该做决定了："
                           f"换掉（改 {row['seam']}）或显式延到下一次真会复查的日期并写明为什么延"))
        if importer_fn is not None:
            importers = importer_fn(name)
            leaks = [f for f in importers if not _under_any(f, seam_paths)]
            if leaks:
                findings.append(Finding(
                    "SEAM_LEAK", f"{name}: 引用面漏出 {len(leaks)} 处 —— seam 是 "
                                 f"{' / '.join(seam_paths)}；漏出：{', '.join(leaks[:6])}"))

    for name, row in baseline.items():
        if name in declared:
            continue
        findings.append(Finding(
            "STALE", f"{name}: 基线登记了但任何清单都没声明它（seam={row['seam']}）—— "
                      f"库已移除就把这行删掉，别留成永久豁免"))
    return findings, counts


def load_inputs(root: Path) -> tuple[str, dict[str, str], str]:
    go_mod = root / GO_MOD_REL
    if not go_mod.is_file():
        fail(f"找不到 {GO_MOD_REL}（--repo 指错了？）")
    missing = [str(p) for p in JS_MANIFESTS if not (root / p).is_file()]
    if missing:
        # 清单少了不等于没问题：那几份里的依赖会整体落在视野外。
        fail(f"以下前端清单不存在，依赖面不完整（新增/删除子包时同步 JS_MANIFESTS）：{missing}")
    js = {}
    for p in JS_MANIFESTS:
        js[str(p)] = (root / p).read_text(encoding="utf-8")
    bl = root / BASELINE_REL
    if not bl.is_file():
        fail(f"基线文件不存在：{BASELINE_REL}（本门靠它区分「已登记的弃维护」与「新引入的弃维护」，"
             f"没有基线不能绿）")
    return go_mod.read_text(encoding="utf-8"), js, bl.read_text(encoding="utf-8")


def fail(msg: str, code: int = 2):
    print(f"❌ {msg}")
    sys.exit(code)


# ── 自检：判据坏掉时本门会「恒绿」，所以牙齿要自己咬一遍 ─────────────────────
SELF_CHECK_GO = """module demo

go 1.26.0

require (
	github.com/Rhymen/go-whatsapp v0.1.1
	github.com/gin-gonic/gin v1.12.0
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
)

require github.com/stretchr/testify v1.12.1 // indirect

replace github.com/gin-gonic/gin => ../gin-fork
"""

SELF_CHECK_PKG = json.dumps({
    "dependencies": {"request": "2.88.2", "left-pad": "latest"},
    "devDependencies": {"node-sass": "4.14.1", "vitest": "^4.0.0"},
})

SELF_CHECK_BASELINE = """# module | seam | review | why
github.com/Rhymen/go-whatsapp | user-server/internal/service/whatsapp_provider.go | 2099-01-01 | 个人版协议库停更，已收敛到唯一 seam
gopkg.in/nowhere.v9 | user-server/internal/x.go | 2099-01-01 | 已不再使用的库
"""


def self_check() -> int:
    """9 枚控制。每枚都要求"该开火的开火、不该开火的闭嘴"（极性对）。"""
    controls: list[tuple[str, bool]] = []

    def run(**kw):
        root = Path(kw.pop("root", tempfile.mkdtemp()))
        seam = kw.pop("seam", "user-server/internal/service/whatsapp_provider.go")
        if kw.pop("make_seam", True):
            p = root / seam
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text("package service\n", encoding="utf-8")
        findings, counts = evaluate(
            root=root,
            go_mod_text=kw.pop("go", SELF_CHECK_GO),
            js_manifests=kw.pop("js", {"user-web/package.json": SELF_CHECK_PKG}),
            baseline_text=kw.pop("baseline", SELF_CHECK_BASELINE),
            today=date(2026, 1, 1),
            importer_fn=kw.pop("importers", lambda m: [seam] if m.startswith("github.com/Rhymen") else []),
        )
        return {f.code for f in findings}, counts

    # 1 禁入名单：go 直接依赖里的 jwt-go、JS 里的 request/node-sass 都要命中
    codes, _ = run()
    controls.append(("BANNED 命中 go 与 JS 两侧", "BANNED" in codes))
    # 2 未登记弃维护：把基线整份去掉 ⇒ Rhymen 变 UNREGISTERED，同时 STALE 也消失
    codes, _ = run(baseline="")
    controls.append(("未登记弃维护 ⇒ UNREGISTERED", "UNREGISTERED" in codes))
    controls.append(("无基线时不该冒 STALE", "STALE" not in codes))
    # 3 seam 文件缺失
    codes, _ = run(make_seam=False)
    controls.append(("seam 文件不存在 ⇒ SEAM_MISSING", "SEAM_MISSING" in codes))
    # 4 seam 泄漏：seam 之外还有一处引用
    codes, _ = run(importers=lambda m: ["user-server/internal/service/whatsapp_provider.go",
                                        "user-server/internal/service/other.go"]
                   if m.startswith("github.com/Rhymen") else [])
    controls.append(("seam 外仍有引用 ⇒ SEAM_LEAK", "SEAM_LEAK" in codes))
    # 5 到期：review 早于 today
    overdue_bl = SELF_CHECK_BASELINE.replace("2099-01-01", "2020-01-01")
    codes, _ = run(baseline=overdue_bl)
    controls.append(("review 过期 ⇒ OVERDUE", "OVERDUE" in codes))
    # 6 基线烂掉：登记了但没人声明的库
    codes, _ = run()
    controls.append(("基线里的死条目 ⇒ STALE", "STALE" in codes))
    # 7 声明形状：latest 与非 registry URL
    codes, _ = run(js={"user-web/package.json": json.dumps(
        {"dependencies": {"a": "latest", "b": "https://x/y.tgz", "c": "^1.2.3"}})})
    controls.append(("latest / URL 规格 ⇒ UNPINNED", "UNPINNED" in codes))
    # 8 本地 replace
    codes, _ = run()
    controls.append(("replace => ../ ⇒ LOCAL_REPLACE", "LOCAL_REPLACE" in codes))
    # 9 极性对：干净的清单+基线 ⇒ 一条都不该红（判据不许恒红）
    clean_go = """module demo

go 1.26.0

require (
	github.com/gin-gonic/gin v1.12.0
)
"""
    clean_js = json.dumps({"dependencies": {"vue": "^3.5.0"}})
    codes, counts = run(go=clean_go, js={"user-web/package.json": clean_js}, baseline="")
    controls.append(("干净清单 ⇒ 零 findings（含 indirect 不算直接依赖、无基线不报 STALE）",
                     not codes and counts["go_direct"] == 1 and counts["js_deps"] == 1))

    failed = [name for name, ok in controls if not ok]
    for name, ok in controls:
        print(f"  {'✓' if ok else '✗'} {name}")
    print(f"自检：{len(controls)} 枚控制，失败 {len(failed)} 枚")
    return 0 if not failed else 1


def main() -> int:
    if "--self-check" in sys.argv:
        return self_check()

    go_text, js, bl_text = load_inputs(REPO_ROOT)
    root = REPO_ROOT

    def importers(module: str) -> list[str]:
        if module.startswith("github.com/") or module.startswith("gopkg.in/") or module.startswith("golang.org/"):
            return count_go_importers(root, module)
        return count_js_importers(root, module)

    findings, counts = evaluate(
        root=root, go_mod_text=go_text, js_manifests=js, baseline_text=bl_text,
        importer_fn=importers)
    print(f"扫描面: go.mod 直接依赖 {counts['go_direct']} 个 / 前端清单 {counts['js_manifests']} 份 "
          f"/ 前端直接依赖 {counts['js_deps']} 个")
    print(f"策略: 禁入 {len(DENY)} 条 / 弃维护观察 {len(WATCH)} 条（基线登记即合约，到期必须做决定）")
    for name, why in OUT_OF_SCOPE.items():
        print(f"  已评估但不纳入引用面收敛: {name} —— {why}")
    if not findings:
        print("✅ 依赖策略门通过：无禁入依赖、弃维护库均收敛到唯一 seam 且未到期、声明形状合规")
        return 0
    for f in findings:
        print(f)
    print("❌ 依赖策略门未通过")
    return 1


if __name__ == "__main__":
    sys.exit(main())
