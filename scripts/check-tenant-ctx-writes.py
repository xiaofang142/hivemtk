#!/usr/bin/env python3
"""后台任务租户归属门：每个注册的 cron 任务必须显式声明「在哪扇出」或「为什么与租户无关」。

为什么需要这道门（2026-10-22 · 桥接链路线上故障后建门）：

线上 SaaS 部署（TENANT_MODE=on）出现一条只报数、不报错的静默空转：

    [BridgeReplay] 渠道快照读取失败: [tenant] 表 bridge_accounts 需要租户上下文但 ctx 未绑定（query）
    [lead-mining] 读取配置失败: [tenant] 表 lead_mining_configs 需要租户上下文但 ctx 未绑定（query）
    [SSE] 出站认领报错，放弃本次推送（行仍待办，超时后重投）

三处同源：**后台路径要写租户表，但拿到的 ctx 里没有租户**。`internal/pkg/tenancyidx`
的隔离 callback 在 on 模式下一律 fail-closed（绝不回落默认租户），于是「任务照跑、什么都不干」。
同文件里另外四个任务是走 `fanoutTask` 的 —— 说明这是**既定约定，只是漏了一处**。

为什么不做「静态推断是否扇出」：
  fanout 可能发生在**被调用的 service 方法内部**（本轮的 `RunOnceAllTenants` 就是），
  浅层 grep 会把它误判为红；深一层就要跨包解析调用图，正则做不到、AST 也不稳。
  猜实现的闸门必然要么误报（没人看）要么漏报（等于没有）。

本门的判据换成**完整性与防腐烂**，不猜实现：
  1. 扫出后台任务注册点：`user-server/internal/pkg/cron/` 里的
     `goTask("<name>", …)` / `AddTask("<spec>", func(){…})`，
     以及**其它自带调度器的包**（`SCHEDULER_DIRS`：`internal/geo/service`、
     `internal/browser_automation/service`）里的 `.AddTask(…, func(){…})`
     —— 这些包各有一份自己的调度装配，2026-10-23 之前**完全不在扫描面内**
     （登记表全绿 ≠ 全仓有门，只是"被扫的那部分"有门）；
  2. 每个注册点必须在 `scripts/tenant-ctx-cron.registry` 里有一行登记：
       - `fanout<TAB><相对路径>::<锚点>` —— 扇出发生在别处，锚点必须是该文件里**真实存在的字面量**
         （通常是那行扇出调用）；锚点消失 ⇒ STALE 红（防「登记表写着扇出、代码里早没了」）；
       - `tenant-free<TAB><理由>` —— 确实不碰租户表，理由必须非空（一句话写清为什么）；
  3. 注册点无登记 ⇒ **NEW 红**（新增后台任务必须做一次有意识的归属决策）；
     登记项无对应注册点 ⇒ **STALE 红**（任务删了/改名了，登记表要跟着划掉）；
  4. 登记表缺失 ⇒ **rc=2**（区分「没扫」与「零命中」，同 test-nil-deref 门口径）。

第二条判据（注入式 seam 回调）：
  `internal/service` / `internal/bridge` 里包级 `var Global*` / `var *Sink` 的**函数类型变量**
  是 service↔bridge 的解耦缝（`service` 不能 import `bridge`）。这类缝会被请求路径与后台路径
  同时调用，租户只活在 ctx 里 ⇒ **首参必须是 `context.Context`**。
  舰上实证：`GlobalSSEPublisher` 的签名原本没有 ctx，4 个调用点只能把请求 ctx 整个丢掉，
  于是认领那步拿不到租户（正是上面第三条报错）。签名里没有 ctx 的缝，调用方**无法**传租户。

执行入口：本地 `python3 scripts/check-tenant-ctx-writes.py`；
接 CI 见 `.github/workflows/user-server-ci.yml` 的 static-gates 作业（脚本与登记表都必须进 paths）。
牙齿：`bash scripts/check-tenant-ctx-writes.test.sh`。
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
CRON_DIR = "user-server/internal/pkg/cron"
REGISTRY = "scripts/tenant-ctx-cron.registry"

# R3 扫描面：cron 包之外**自带调度器**的包。判据与 cron 包一致（注册点必须有登记），
# 唯一差别是任务键取法：这些地方的注册调用首参常常是变量（`spec`），没有字面量名字，
# 所以键改用 `@<相对路径>::<所在函数名>` —— 函数名是这些注册点唯一稳定的锚。
# 新增一个"自己 new 调度器"的包时，把它的目录加进来，否则新包里的任务对这道门不可见。
SCHEDULER_DIRS = (
    "user-server/internal/geo/service",
    "user-server/internal/browser_automation/service",
)

# R2 作用域：这两个包里的注入式 seam
SEAM_DIRS = ("user-server/internal/service", "user-server/internal/bridge")

GO_TASK_RE = re.compile(r'mgr\.goTask\(\s*"([^"]+)"\s*,')
ADD_TASK_RE = re.compile(r'mgr\.AddTask\(\s*"[^"]*"\s*,\s*func\s*\(\s*\)\s*\{')
# 其它调度器包：注册调用首参常是变量，靠"所在函数名"当锚（见 SCHEDULER_DIRS 注释）
ANY_REGISTER_RE = re.compile(r"\.(?:AddTask|goTask)\(")
ENCLOSING_FUNC_RE = re.compile(r"^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*\(", re.M)
SAME_PKG_CALL_RE = re.compile(r"\b([A-Z][A-Za-z0-9_]*)\s*\(")
SEAM_VAR_RE = re.compile(r"^var\s+((?:Global[A-Z]\w*)|(?:\w+Sink))\s+func\s*\((.*?)\)", re.S)


def read(p: str) -> str:
    return (REPO / p).read_text(encoding="utf-8")


def iter_go(dirpath: str):
    root = REPO / dirpath
    if not root.is_dir():
        return
    for p in sorted(root.rglob("*.go")):
        if p.name.endswith("_test.go"):
            continue
        if any(part.startswith(".") or part in ("vendor", "node_modules") for part in p.relative_to(REPO).parts):
            continue
        yield p.relative_to(REPO).as_posix(), p.read_text(encoding="utf-8", errors="replace")


def closure_body(src: str, open_brace: int) -> str:
    """从 `{` 起按大括号配平取出闭包体（跳过字符串与行注释，够用即止）。"""
    depth = 0
    i = open_brace
    n = len(src)
    in_str = False
    in_raw = False
    while i < n:
        ch = src[i]
        if in_str:
            if ch == "\\":
                i += 2
                continue
            if ch == '"':
                in_str = False
        elif in_raw:
            if ch == "`":
                in_raw = False
        else:
            if ch == '"':
                in_str = True
            elif ch == "`":
                in_raw = True
            elif ch == "/" and i + 1 < n and src[i + 1] == "/":
                j = src.find("\n", i)
                i = n if j < 0 else j
                continue
            elif ch == "{":
                depth += 1
            elif ch == "}":
                depth -= 1
                if depth == 0:
                    return src[open_brace : i + 1]
        i += 1
    return src[open_brace:]


def enclosing_func_name(src: str, pos: int) -> str:
    """返回 pos 之前最近的函数声明名（方法取方法名）；找不到返回 `?`。"""
    name = "?"
    for m in ENCLOSING_FUNC_RE.finditer(src, 0, pos):
        name = m.group(1)
    return name


def collect_registrations() -> dict[str, dict]:
    """{task_key: {file, line, body}} —— 任务注册点。

    body 是**任务真正执行的那段代码**：
      - `goTask("<name>", func(ctx){…})` ⇒ 闭包体本身；
      - `AddTask("<spec>", func(){ XxxCron() })` ⇒ 到同包里解析 `func XxxCron(…)` 的函数体。
        只拿闭包体（就是一行 `XxxCron()`）判"有没有扇出"会误判：扇出在函数里。
    """
    regs: dict[str, dict] = {}
    # 先建同包函数体索引，供 AddTask 键解析
    func_bodies: dict[str, str] = {}
    for rel, src in iter_go(CRON_DIR) or []:
        for m in re.finditer(r"^func\s+([A-Z]\w*)\s*\(", src, re.M):
            brace = src.find("{", m.end())
            if brace >= 0:
                func_bodies.setdefault(m.group(1), closure_body(src, brace))

    for rel, src in iter_go(CRON_DIR) or []:
        # goTask("<name>", func(ctx context.Context) { ... })
        for m in GO_TASK_RE.finditer(src):
            name = m.group(1).strip()
            if not name:
                continue
            brace = src.find("{", m.end())
            body = closure_body(src, brace) if brace >= 0 else ""
            regs[name] = {"file": rel, "line": src[: m.start()].count("\n") + 1, "body": body}
        # AddTask("<spec>", func() { XxxCron() })
        for m in ADD_TASK_RE.finditer(src):
            body = closure_body(src, m.end() - 1)
            called = SAME_PKG_CALL_RE.search(body)
            if called:
                key = called.group(1)
                # 解析到同包函数体；解析不到就退回闭包体（至少能看见那行调用）
                body = func_bodies.get(key, body)
            else:
                key = "@%s:%d" % (rel, src[: m.start()].count("\n") + 1)
            regs[key] = {"file": rel, "line": src[: m.start()].count("\n") + 1, "body": body}

    # 其它自带调度器的包：键 = `@<相对路径>::<所在函数>`
    for dirpath in SCHEDULER_DIRS:
        for rel, src in iter_go(dirpath) or []:
            for m in ANY_REGISTER_RE.finditer(src):
                key = "@%s::%s" % (rel, enclosing_func_name(src, m.start()))
                regs.setdefault(
                    key,
                    {"file": rel, "line": src[: m.start()].count("\n") + 1, "body": src[m.start() :]},
                )
    return regs


def load_registry() -> tuple[dict[str, tuple[str, str]], list[str]]:
    """返回 ({task: (kind, payload)}, errors)。"""
    path = REPO / REGISTRY
    if not path.is_file():
        return {}, []
    out: dict[str, tuple[str, str]] = {}
    errs: list[str] = []
    for lineno, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        parts = raw.split("\t")
        if len(parts) != 3:
            errs.append("%s:%d 列数应为 3（task<TAB>kind<TAB>payload），实际 %d" % (REGISTRY, lineno, len(parts)))
            continue
        task, kind, payload = (p.strip() for p in parts)
        if not task:
            errs.append("%s:%d 任务名为空" % (REGISTRY, lineno))
            continue
        if kind not in ("fanout", "tenant-free"):
            errs.append("%s:%d kind 只认 fanout / tenant-free，实际 %r" % (REGISTRY, lineno, kind))
            continue
        if not payload:
            errs.append("%s:%d payload 为空（fanout 要写 路径::锚点，tenant-free 要写理由）" % (REGISTRY, lineno))
            continue
        if task in out:
            errs.append("%s:%d 任务 %r 重复登记" % (REGISTRY, lineno, task))
            continue
        out[task] = (kind, payload)
    return out, errs


def check_registry_completeness() -> list[str]:
    problems: list[str] = []
    regs = collect_registrations()
    registry, errs = load_registry()
    problems.extend(errs)

    for task in sorted(regs):
        if task not in registry:
            problems.append(
                "NEW  未登记的后台任务 %r（%s:%d）：新增后台任务必须做一次租户归属决策 —— "
                "写入 %s，选 fanout（写明扇出锚点）或 tenant-free（写明理由）"
                % (task, regs[task]["file"], regs[task]["line"], REGISTRY)
            )

    for task in sorted(registry):
        if task not in regs:
            problems.append("STALE 登记表里的任务 %r 在 cron 注册点里找不到（删了/改名了？划掉这行）" % task)

    for task, (kind, payload) in sorted(registry.items()):
        if kind != "fanout":
            continue
        if "::" not in payload:
            problems.append("STALE 任务 %r 的 fanout 锚点格式应为 <相对路径>::<锚点>，实际 %r" % (task, payload))
            continue
        rel, anchor = payload.split("::", 1)
        rel = rel.strip()
        anchor = anchor.strip()
        target = REPO / rel
        if not target.is_file():
            problems.append("STALE 任务 %r 的锚点文件不存在：%s" % (task, rel))
            continue
        if anchor not in target.read_text(encoding="utf-8", errors="replace"):
            problems.append(
                "STALE 任务 %r 声明的扇出锚点在 %s 里找不到：%r —— "
                "扇出被删/改写了？改回来，或同步改登记表" % (task, rel, anchor)
            )
            continue

        # 锚点存在还不够：**这个任务还得真在调它**。
        # 反例（本门补这条的直接原因）：有人把 bridge_offline_replay 的调用改回
        # `RunOnce(ctx)`，而 `RunOnceAllTenants` 仍留在 service 里 —— 光看锚点会误绿。
        if task in regs:
            problems.extend(_check_task_reaches_fanout(task, regs[task]["body"], anchor))
    return problems


# 判定"这一处就是扇出"的调用形状（本仓既定口径，见 pkg/tenant/fanout.go）
FANOUT_HELPERS = ("fanoutTask(", "tenant.Fanout(", "BackgroundWithTenant(", "OnlyTenant(", "WithSystemScope(")
# 锚点里不当标识符的通用词（避免"人人都有 ctx"这种假命中）
_ANCHOR_STOPWORDS = {
    "func", "context", "error", "string", "bool", "int64", "uint", "time", "Status",
    "return", "nil", "true", "false", "make", "chan", "struct", "interface",
}


def _anchor_symbols(anchor: str) -> set[str]:
    """锚点里长度 ≥4 的标识符（含方法名），排除通用词。"""
    out = set()
    for tok in re.findall(r"[A-Za-z_]\w{3,}", anchor):
        if tok.lower() in {w.lower() for w in _ANCHOR_STOPWORDS}:
            continue
        out.add(tok)
    return out


def _check_task_reaches_fanout(task: str, body: str, anchor: str) -> list[str]:
    """任务体必须真能走到扇出：或直接调扇出 helper，或引用锚点里的那个符号。"""
    if not body.strip():
        return []  # 解析不到体时不下判断（宁可漏一次，也不凭猜报红）
    if any(h in body for h in FANOUT_HELPERS):
        return []
    syms = _anchor_symbols(anchor)
    hit = [s for s in syms if re.search(r"\b" + re.escape(s) + r"\b", body)]
    if hit:
        return []
    return [
        "BROKEN 任务 %r 的任务体既没调扇出 helper（%s），也没引用登记锚点里的任何符号（%s）—— "
        "扇出实现还在、但这个任务已经不走它了（典型形态：把调用改回不带租户的旧方法）"
        % (task, " / ".join(FANOUT_HELPERS[:2]) + " …", ", ".join(sorted(syms)[:6]) or "<无>")
    ]


def check_seam_ctx() -> list[str]:
    problems: list[str] = []
    for d in SEAM_DIRS:
        for rel, src in iter_go(d) or []:
            # 逐条包级 var 声明（可能跨行，用括号配平收口）
            for m in re.finditer(r"^var\s+((?:Global[A-Z]\w*)|(?:\w+Sink))\s+func\s*\(", src, re.M):
                name = m.group(1)
                start = m.end() - 1
                depth = 0
                i = start
                params_end = None
                while i < len(src):
                    if src[i] == "(":
                        depth += 1
                    elif src[i] == ")":
                        depth -= 1
                        if depth == 0:
                            params_end = i
                            break
                    i += 1
                if params_end is None:
                    continue
                params = src[start + 1 : params_end].strip()
                first = params.split(",")[0].strip() if params else ""
                # 首参可能是「名字 类型」（`ctx context.Context`）、「仅类型」（`context.Context`）、
                # 或「带包名的别名」（`c context.Context` / `context.Context`）。
                # 取**类型**那一侧判定，不能拿整个首参比。
                first_type = first.split()[-1] if first.split() else ""
                if first_type not in ("context.Context", "context.Context,"):
                    line = src[: m.start()].count("\n") + 1
                    problems.append(
                        "SEAM %s:%d 注入式 seam %s 的首参不是 context.Context（实际 %r）："
                        "这类缝被请求路径与后台路径同时调用，租户只活在 ctx 里，签名里没有 ctx "
                        "⇒ 调用方无法传租户（线上实证：SSE 出站认领被隔离守卫拒绝）"
                        % (rel, line, name, first or "<空>")
                    )
    return problems


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", default=str(REPO))
    args = ap.parse_args()
    globals()["REPO"] = Path(args.repo).resolve()

    if not (REPO / REGISTRY).is_file():
        print(
            "ENV-BROKEN：缺登记表 %s —— 未判定（缺基线时「零命中」与「没扫」分不开）" % REGISTRY,
            file=sys.stderr,
        )
        return 2

    regs = collect_registrations()
    problems = check_registry_completeness() + check_seam_ctx()

    if problems:
        print("── 后台任务租户归属门（FAILED）──", file=sys.stderr)
        for p in problems:
            print("  " + p, file=sys.stderr)
        print(
            "\n判据说明：脚本头注释；登记表 %s；新增任务先在登记表做归属决策。" % REGISTRY,
            file=sys.stderr,
        )
        return 1

    print(
        "OK tenant-ctx：后台任务注册点 %d 个全部登记（fanout/tenant-free 各有据可查），seam 回调均带 ctx"
        % len(regs)
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
