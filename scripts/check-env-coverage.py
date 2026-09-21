#!/usr/bin/env python3
"""配置面可发现性门：生产代码读取的环境变量，必须在运维看得到的地方出现。

背景（2026-09-21 · 第三十五轮）：本门第一次能数全之后实测 —— user-server 生产代码读取 **179** 个
环境变量，其中 **71** 个出现在文档面上、**16** 个只被 `cmd/` 下的工具进程读（自动豁免）、
**92** 个进基线登记为债。没进文档面的那些键只能靠读源码才知道存在，其中包括会放宽安全姿态的开关
（`BRUTE_FORCE_DISABLED`、`ALLOW_SELF_RESTART`、`MARKETING_WEBHOOK_ALLOW_INSECURE`）和改了会让
存量数据错位的身份口径（`ONEID_SALT`）。本卡把其中最危险的一批补进了 `docs/DEPLOYMENT_GUIDE.md`
§6.1（该节"此前只存在于代码里"的两张表就是本卡产物），其余先入基线豁免表登记为债，
**新增一个没人知道的键就红**。上面三个数字是同一轮在改名克隆里跑出来的口径，代码一改就会漂。

判据（三条，任一不成立即 rc=1）：
  1. 每个键要么出现在文档面（`.env-example` / `docs/DEPLOYMENT_GUIDE.md` / 旗子登记表的 `FF_*`，
     整词匹配），要么命中自动豁免（只在 `user-server/cmd/` 下、且不在 `cmd/api` 里被读 ⇒ 工具/mock
     进程专属），要么在 `scripts/env-coverage.baseline` 里带理由登记；
  2. 基线里登记的键若已不再被读取或已被补进文档 ⇒ 判 STALE 红（防基线烂掉）；
  3. 基线不允许无理由条目。

口径边界（按「门禁口径盲区」的规矩写明，别把"没数据"印成"没问题"）：
  - 枚举源是**字面量**三条路径：直读 `os.Getenv("X")` / `os.LookupEnv("X")`、把键名以字面量传给
    「形参直通 os.Getenv」的 helper（见 find_env_helpers 自动识别）、以及同包内 `NAME = "X"` 再
    `os.Getenv(NAME)`（本卡为"文案与读取处不漂移"引入的常量写法，见 CONST_ASSIGN）；
    用变量拼装或运行时算出的键名不在本门视野内；
  - 只扫 `*_test.go` 之外的文件（测试夹具不算运维配置面）；
  - "出现在文档面"只做整词存在性检查，不核对文档描述与代码语义是否一致（那是人工审查面）；
    旗子登记表按 `FF_` 前缀收紧，否则文档里一句"看 MODE"就能替一个叫 `MODE` 的必配项作证。
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
SCAN_ROOT = REPO_ROOT / "user-server"
# 文档面 = 运维/部署视角读得到的那份东西。三元组第二项是**该面可为哪些键作证**的前缀约束：
# 旗子登记表只替它登记的 `FF_*` 家族作证。整词匹配下"文档里出现过 MODE 这个词"不能算
# "运维知道有个叫 MODE 的必配项"——实测正是它把基线里的 MODE 判成 STALE，故按家族收紧。
DOC_SURFACES: list[tuple[Path, str | None]] = [
    (REPO_ROOT / ".env-example", None),
    (REPO_ROOT / "docs" / "DEPLOYMENT_GUIDE.md", None),
    (REPO_ROOT / "docs" / "architecture" / "AI_CORE_FEATURE_INVENTORY.md", "FF_"),
]
BASELINE = REPO_ROOT / "scripts" / "env-coverage.baseline"

ENV_READ = re.compile(r'os\.(?:Getenv|LookupEnv)\("([A-Z0-9_]+)"\)')
TEST_FILE_SUFFIX = "_test.go"

# 只经 helper 传键名的读取（`envInt("WEBHOOK_QUEUE_SIZE", …)` 这类）。
# 不认这一类的话，本门会漏掉 17 个真在用的运维旋钮（ORDER_WEBHOOK_MAX_BODY_BYTES、
# WEBHOOK_WORKER_COUNT、POSTGRES_TEST_* 等），把"没枚举到"当成"没问题"。
FN_HEAD = re.compile(r'func\s+(?:\(\s*\w+\s+\*?\w+\s*\)\s+)?([A-Za-z_]\w*)\s*\(([^)]*)\)[^{]*\{')
KEY_SHAPE = re.compile(r'^[A-Z][A-Z0-9_]{2,}$')

# 键名收进常量再交给 os.Getenv（`const ingressAPIKeyEnv = "INGRESS_API_KEY"` + `os.Getenv(ingressAPIKeyEnv)`）
# 是本卡为"文案与读取处不再漂移"刻意引入的写法；门若只认字面量就会把它当"没有这个键"，
# 反向验证时果然漏了它 ⇒ 一并枚举：同包内 `NAME = "LITERAL"` 且 NAME 出现在 os.Getenv(NAME) 里。
CONST_ASSIGN = re.compile(r'\b([A-Za-z_]\w*)\s*=\s*"([A-Z][A-Z0-9_]{2,})"')
GETENV_IDENT = re.compile(r'os\.(?:Getenv|LookupEnv)\((\w+)\)')


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8", errors="ignore")


def find_env_helpers(sources: dict[Path, str]) -> set[str]:
    """返回「把某个形参直接交给 os.Getenv/LookupEnv」的函数名。"""
    helpers: set[str] = set()
    for text in sources.values():
        for m in FN_HEAD.finditer(text):
            name, params = m.group(1), m.group(2)
            pnames = {p.strip().split()[0] for p in params.split(",") if p.strip()}
            start = m.end()
            nxt = text.find("\nfunc ", start)
            body = text[start: nxt if nxt > 0 else len(text)]
            if any(arg in pnames for arg in re.findall(r'os\.(?:Getenv|LookupEnv)\((\w+)\)', body)):
                helpers.add(name)
    return helpers


def documented_surfaces() -> list[tuple[str, str | None, set[str]]]:
    """每个文档面能替哪些键作证：(面名, 键前缀约束, 该面出现过的整词集合)。"""
    return [
        (str(path.relative_to(REPO_ROOT)), prefix, set(re.findall(r"[A-Z0-9_]{2,}", read(path))))
        for path, prefix in DOC_SURFACES
    ]


def is_documented(surfaces: list[tuple[str, str | None, set[str]]], key: str) -> bool:
    return any(
        key in tokens and (prefix is None or key.startswith(prefix)) for _, prefix, tokens in surfaces
    )


def collect_reads() -> tuple[dict[str, list[str]], int]:
    files = [p for p in sorted(SCAN_ROOT.rglob("*.go")) if not p.name.endswith(TEST_FILE_SUFFIX)]
    sources = {p: read(p) for p in files}
    helpers = find_env_helpers(sources)
    # Go 的 const 是包级的，跨文件引用同包常量很常见 ⇒ 常量表按目录（=包）建，不按单文件
    pkg_consts: dict[Path, dict[str, str]] = {}
    for path, text in sources.items():
        table = pkg_consts.setdefault(path.parent, {})
        for name, literal in CONST_ASSIGN.findall(text):
            table.setdefault(name, literal)

    reads: dict[str, set[str]] = {}
    via_helper: set[str] = set()

    def add(key: str, rel: str, through_helper: bool = False) -> None:
        if not KEY_SHAPE.match(key):
            return
        reads.setdefault(key, set()).add(rel)
        if through_helper:
            via_helper.add(key)

    for path, text in sources.items():
        rel = str(path.relative_to(REPO_ROOT))
        table = pkg_consts.get(path.parent, {})
        for key in ENV_READ.findall(text):
            add(key, rel)
        for ident in GETENV_IDENT.findall(text):
            if ident in table:
                add(table[ident], rel)
        for helper in helpers:
            for key in re.findall(r'\b' + re.escape(helper) + r'\("([A-Z0-9_]+)"', text):
                add(key, rel, through_helper=True)
            for ident in re.findall(r'\b' + re.escape(helper) + r'\((\w+)[,)]', text):
                if ident in table:
                    add(table[ident], rel, through_helper=True)
    direct = set()
    for text in sources.values():
        direct.update(ENV_READ.findall(text))
    return ({k: sorted(v) for k, v in reads.items()}, len(via_helper - direct))


def is_tooling_only(files: list[str]) -> bool:
    """只在 cmd/ 下、且没有一处落在 cmd/api（常驻服务进程）⇒ 工具/mock 专属，不参与运维必配面。"""
    inside_cmd = [f for f in files if f.startswith("user-server/cmd/")]
    return bool(inside_cmd) and len(inside_cmd) == len(files) and not any(
        f.startswith("user-server/cmd/api/") for f in files
    )


def load_baseline() -> dict[str, str]:
    entries: dict[str, str] = {}
    if not BASELINE.exists():
        return entries
    for line in read(BASELINE).splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, sep, reason = line.partition("\t")
        key = key.strip()
        reason = reason.strip() if sep else ""
        if not reason:
            print(f"❌ 基线条目没有理由：{key}")
            entries[key] = ""
            continue
        entries[key] = reason
    return entries


def main() -> int:
    missing = [
        str(p.relative_to(REPO_ROOT)) for p in [SCAN_ROOT, *(p for p, _ in DOC_SURFACES)] if not p.exists()
    ]
    if missing:
        print(f"ENV-COVERAGE: ENV-BROKEN 缺少 {', '.join(missing)}（应在工作区根执行）")
        return 2

    reads, helper_keys = collect_reads()
    surfaces = documented_surfaces()
    baseline = load_baseline()

    holes: list[tuple[str, str]] = []
    exempt_tooling = 0
    exempt_baseline = 0
    documented = 0
    for key, files in sorted(reads.items()):
        if is_documented(surfaces, key):
            documented += 1
        elif is_tooling_only(files):
            exempt_tooling += 1
        elif key in baseline:
            if not baseline[key]:
                holes.append((key, "基线条目缺理由"))
            else:
                exempt_baseline += 1
        else:
            holes.append((key, files[0]))

    stale = sorted(k for k in baseline if k not in reads or is_documented(surfaces, k))

    print(
        f"生产代码读取键 {len(reads)} · 已文档化 {documented} · "
        f"工具进程自动豁免 {exempt_tooling} · 基线登记 {exempt_baseline} · 红 {len(holes)}"
    )
    # 计数自证：helper 侧枚举到的键数单独印出来，否则"识别到 0 个 helper"和"没有键经 helper"看不出差别
    print(f"（其中经 env helper 字面量枚举到 {helper_keys} 个键名）")
    for key, where in holes:
        print(f"  UNDOCUMENTED {key}  <- {where}")
    for key in stale:
        print(f"  STALE 基线条目已不成立（键不再被读取或已补进文档）：{key}")

    if holes or stale:
        print("❌ 配置面可发现性门未过")
        return 1
    print("✅ 配置面可发现性门通过")
    return 0


if __name__ == "__main__":
    sys.exit(main())
