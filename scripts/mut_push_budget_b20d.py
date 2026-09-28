#!/usr/bin/env python3
"""批20d 变异电池：B 链路出站重推上界（A3）+ 本地已发缓存时间界（A4），逐消费点验牙。

为什么要有这条电池（而不是"测试全绿"就算完）：
- A3 的上界有**三处消费点**（轮询认领 / SSE 单行认领 / SSE 补拉列表）+ 一个共享的收口写。
  只测一处，"另两处忘了加界"这种半只眼睛的改法是绿的——而本批立项时的事实恰恰是
  「三条路径都能取到同一行」，任何一条没关住都等于没关。所以 R1/R2/R3 逐路径一刀。
- A3 的计数（+1）、预算不被回收抹掉、到界落终态、终态不写 sent_at、只碰出站欠交付行，
  是五个互相独立的判据，一个都不许靠"另一个也红了"蒙过去。
- 迁移侧（M1~M7）盯的是"这条迁移到底做了什么"：摘掉任一 ALTER、改默认值、放开 NOT NULL、
  不注册、去掉 HasTable 前置、Down 真去 DROP——每一种都必须点名一条腿。
  尤其 M7：declineColumnDrop 是一句"拒绝执行"的调用，没有对应注码它就是一行永远绿的注释。
- A4 的 TTL 有三个可分开的失效面（判据本身、装载即回收、超界淘汰取哪一端）+ 老格式兼容 +
  常量下界。J3/J8 就是为"evict 的过期前缀循环其实没测试守着"和"不重排也没事"补的刀——
  第一版夹具让这两刀都活着，改夹具（插入序与时间序错位）之后才判得出来。

口径（沿用批16/17/18/19x/20c 电池）：
- 控制组必须 rc==0、被结算用例数与全绿轮的通过数一致、skip==0、且不许有任何红名；
- 每格断言 PASS+FAIL==控制组数（一条用例 panic 会带走整个二进制，"FAIL=1"看着像杀其实没跑完）；
- BUILD FAILED / panic / 红而没点名 一律判 BROKEN，**不计入杀掉**（编译红不是牙）；
- 锚点命中必须恰好一次，注码先过语法检查（gofmt -e / node --check 式前置），
  还原后逐文件比 md5；只在私有 --shared 克隆 / 私有副本里注码，绝不碰共享工作树。

用法：python3 scripts/mut_push_budget_b20d.py [--js-only|--go-only] [--keep] [--clone DIR]
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from mut_dispose import dispose, workdir

# 脚本在 <repo>/scripts/ 下 ⇒ 根 = 上一级。**不硬编码仓名**（改名克隆必须照样能跑）。
ROOT = Path(__file__).resolve().parent.parent
BRIDGE = ROOT / "user-web" / "bridge"
REPO_REL = Path("user-server/internal/repository/message_hub_inbox_outbound.go")
MIG_REL = Path("user-server/internal/migration/migrations/v3_45_0_message_hub_outbound_push_budget_migration.go")
REG_REL = Path("user-server/internal/migration/migrations/initial_schema.go")
# 本泳道脏 .go 的枚举范围：A3 的仓储、模型两列、迁移三件套都在这里面。
# 手写清单必然漏（漏一个用旧签名的文件 = 克隆里 [build failed]，电池整个失声）。
LANE_PATHS = ["user-server/internal/repository",
              "user-server/internal/model",
              "user-server/internal/migration",
              # migrations 包的测试 import 了 browser_automation/model（v3.38~v3.44 建表判据），
              # 不带上它 = 克隆里拿旧 *bool 之前的形状编新测试，[build failed] 式失声。
              "user-server/internal/browser_automation"]

JS_PRIM_REL = Path("src/core/downlink.js")
JS_CONST_REL = Path("src/core/constants.js")
JS_TESTS = ["test/downlink-b20d-sentcache-ttl.test.js"]
GO_PKGS = ["./internal/repository/", "./internal/migration/migrations/"]
# 只跑本批 10 条腿（仓储 5 + 迁移 5）。跑整包会把无关用例的红混进「杀掉」名单，
# 看着像牙其实是被别人撞红的。
GO_RUN = "|".join([
    "TestClaimPendingOutboundCapsPushAttemptsAndTerminalizes",
    "TestInflightRecycleKeepsPushAttemptBudget",
    "TestClaimOutboundForPushHonorsCap",
    "TestFetchOutboundUndeliveredExcludesAndEscalatesExhausted",
    "TestPushCapSweepTouchesOnlyOwedOutbound",
    "TestOutboundPushBudgetMigration_Meta",
    "TestOutboundPushBudgetMigration_NilDB",
    "TestOutboundPushBudgetMigration_UpAndShape",
    "TestOutboundPushBudgetMigration_NoTable",
    "TestOutboundPushBudgetMigration_IsRegistered",
])

CAP_POLL = "TestClaimPendingOutboundCapsPushAttemptsAndTerminalizes"
CAP_RECYCLE = "TestInflightRecycleKeepsPushAttemptBudget"
CAP_SSE = "TestClaimOutboundForPushHonorsCap"
CAP_FETCH = "TestFetchOutboundUndeliveredExcludesAndEscalatesExhausted"
CAP_SWEEP = "TestPushCapSweepTouchesOnlyOwedOutbound"
MIG_SHAPE = "TestOutboundPushBudgetMigration_UpAndShape"
MIG_NOTABLE = "TestOutboundPushBudgetMigration_NoTable"
MIG_REG = "TestOutboundPushBudgetMigration_IsRegistered"

ANSI = re.compile(r"\x1b\[[0-9;]*m")
CONTROL = {"js": 0, "go": 0}


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    """恰好命中一次，否则当场死——静默的"没改到"是最便宜的假绿。"""
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1 次）：{old[:90]!r}")
    return text.replace(old, new, 1)


def lane_overlays() -> list[str]:
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到本泳道脏文件清单：" + r.stderr[-200:])
    out = []
    for line in r.stdout.splitlines():
        p = line[3:].split(" -> ")[-1].strip().strip('"')
        if p.endswith(".go"):
            out.append(p)
    if not out:
        raise SystemExit("脏文件清单为空——克隆里跑的是 HEAD，测不到本批改动（宁可停机也别假绿）")
    return out


def verdict(r: dict, expect_total: int) -> str:
    """三态判定：杀掉 / 存活=洞 / BROKEN（红了但判不了）。"""
    if r["rc"] == 0 and not r["killed"]:
        return "存活=洞"
    if r.get("panicked") or r.get("buildfailed") or not r["killed"]:
        return "BROKEN=判不了"
    if r["total"] != expect_total or r["failed"] < 1 or r["skipped"] > 0:
        return "BROKEN=判不了"
    return "杀掉"


def dup_report(side: str, kills: dict) -> None:
    seen = {}
    for code, names in kills.items():
        key = tuple(sorted(names))
        seen.setdefault(key, []).append(code)
    for key, codes in seen.items():
        if len(codes) > 1 and key:
            print(f"[{side}] 同族（同一批用例被多格杀掉，判据可能重叠）：" + "≈".join(codes)
                  + f" → {' '.join(sorted(key))[:120]}")


# ------------------------------------------------------------------ JS 侧
def js_prepare(dst: Path) -> Path:
    work = dst / "web"
    work.mkdir(parents=True)
    for item in ("src", "test", "package.json", "vite.config.js"):
        s = BRIDGE / item
        if not s.exists():
            raise SystemExit(f"缺少 {s}")
        (shutil.copytree if s.is_dir() else shutil.copy2)(s, work / item)
    nm = BRIDGE / "node_modules"
    if not nm.is_dir():
        raise SystemExit(f"{nm} 不存在——先在 user-web/bridge 里 npm install")
    (work / "node_modules").symlink_to(nm.resolve())
    return work


def js_run(work: Path) -> dict:
    p = subprocess.run(["npx", "vitest", "run"] + JS_TESTS, cwd=work,
                       capture_output=True, text=True, timeout=900, env=os.environ)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = [re.sub(r"\s+\d+ms$", "", ln.split("×", 1)[1].strip())
              for ln in out.splitlines() if "×" in ln]
    m = re.search(r"Tests\s+(?:\d+ failed \| )?(?:\d+ skipped \| )?(\d+) passed.*?\((\d+)\)", out)
    passed = int(m.group(1)) if m else 0
    total = int(m.group(2)) if m else 0
    sm = re.search(r"(\d+) skipped", out)
    skipped = int(sm.group(1)) if sm else 0
    return {"rc": p.returncode, "killed": killed, "total": total,
            "failed": total - passed - skipped, "skipped": skipped, "out": out,
            "panicked": "Unhandled" in out and total == 0,
            "buildfailed": "Transform failed" in out or "SyntaxError" in out}


LEG_A = "A 界内（23h）命中：不重发，只补一次 delivered"
LEG_B = "B 界外（25h）不再拦：重发一次，且记录以新时间戳续期"
LEG_C = "C 上限淘汰按时间：插入序与时间序错位时，丢的是最旧的而不是最先写入的"
LEG_F = "F 未超上限也回收过期条目：时间界不是条数上限的副产品"
LEG_D = "D 旧格式（纯字符串）可读：不重发，并被改写为带时间戳的新格式"
LEG_E = "E TTL 下界锁：≥24h（同行 5min 去重窗不可照抄，界必须盖住服务端重推窗）"
LEG_G = "G 会话内跨过界：不经装载也必须放开重发"


HAS_LINE = "    return ts !== undefined && Date.now() - ts <= BRIDGE_THREE_CHANNEL.sentCacheTtlMs;"
EVICT_LOOP = ("    for (const [k, ts] of this.mem) {\n"
              "      if (now - ts <= ttl) break;\n"
              "      this.mem.delete(k);\n"
              "    }\n")
BREAK_LINE = "      if (now - ts <= ttl) break;"
TRIM_LINE = "      this.mem.delete(this.mem.keys().next().value);"
SORT_LINE = "        this.mem = new Map([...this.mem.entries()].sort((a, b) => a[1] - b[1]));"
LEGACY_LINE = "            this.mem.set(String(e), now);"
TTL_CONST_LINE = "  sentCacheTtlMs: 24 * 3600 * 1000,"


def js_mutants():
    return [
        ("J1", "has() 的时间界整条失效（会话内跨过界仍判「已发过」→ 该重发的永远不重发）", LEG_G),
        ("J2", "has() 的界折成 0（界内也当过期 → 把还在重推窗内的记录放开重发）", LEG_A),
        ("J3", "evict 的过期前缀循环整段摘掉（未超界时永不回收＝A4 立项理由①复发）", LEG_F),
        ("J4", "evict 的过期判据反向（<= 写成 >=，先删界内的、留下界外的）", LEG_F),
        ("J5", "超界淘汰掐的是尾部（最新那条先丢）", LEG_C),
        ("J8", "装载不重排（保留写入序 → 「头部即最旧」的前提塌了）", LEG_C),
        ("J6", "老格式记录判成「早已过期」（升级瞬间把在途重复放出去）", LEG_D),
        ("J7", "老格式记录整条丢弃（读都读不到）", LEG_D),
        ("J9", "TTL 常量改成 5min（照抄同行去重窗，短于上游重推窗）", LEG_E),
    ]


def apply_js(src: str, const_src: str, code: str) -> tuple[str, str]:
    """返回 (downlink.js 新内容, constants.js 新内容)；只有一者会变。"""
    if code == "J1":
        return sub_once(src, HAS_LINE, "    return ts !== undefined;", code), const_src
    if code == "J2":
        return sub_once(src, HAS_LINE,
                        "    return ts !== undefined && Date.now() - ts <= 0;", code), const_src
    if code == "J3":
        return sub_once(src, EVICT_LOOP, "", code), const_src
    if code == "J4":
        return sub_once(src, BREAK_LINE, "      if (now - ts >= ttl) break;", code), const_src
    if code == "J5":
        return sub_once(src, TRIM_LINE,
                        "      const __ks = [...this.mem.keys()];\n"
                        "      this.mem.delete(__ks[__ks.length - 1]);", code), const_src
    if code == "J8":
        return sub_once(src, SORT_LINE,
                        "        this.mem = new Map([...this.mem.entries()].sort(() => 0));",
                        code), const_src
    if code == "J6":
        return sub_once(src, LEGACY_LINE, "            this.mem.set(String(e), 0);", code), const_src
    if code == "J7":
        return sub_once(src, LEGACY_LINE, "            void 0;", code), const_src
    if code == "J9":
        return src, sub_once(const_src, TTL_CONST_LINE, "  sentCacheTtlMs: 5 * 60 * 1000,", code)
    raise SystemExit(f"未知 JS 格 {code}")


# ------------------------------------------------------------------ Go 侧
def syntax_ok_go(src: str) -> tuple[bool, str]:
    p = subprocess.run(["gofmt", "-e"], input=src, capture_output=True, text=True, timeout=120)
    return p.returncode == 0, p.stderr.strip()


def syntax_ok_js(src: str) -> tuple[bool, str]:
    p = subprocess.run(["node", "--input-type=module", "-e", src],
                       capture_output=True, text=True, timeout=120)
    # 只判**语法**坏：模块解析期报错（import 找不到、顶层抛）与本电池的注码无关，
    # node --check 走不了 ESM 字符串，这里用 stderr 里是不是 SyntaxError 来分。
    err = (p.stderr or "").strip()
    return "SyntaxError" not in err, err[:300]


def go_prepare(dst: Path) -> Path:
    clone = dst / "clone"
    if clone.exists():
        raise SystemExit(f"{clone} 已存在（换 --clone 目录或先删）")
    r = subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(clone)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("克隆失败：" + (r.stdout + r.stderr)[-400:])
    # 分支从**工作树**读，不从克隆读：--no-checkout 的克隆里 HEAD 是未 born 的符号引用，
    # `branch --show-current` 可能给空串 ⇒ 退回 master，而本仓当前分支未必是 master。
    b = subprocess.run(["git", "-C", str(ROOT), "branch", "--show-current"],
                       capture_output=True, text=True, timeout=60)
    branch = b.stdout.strip() or "master"
    c = subprocess.run(["git", "checkout", "-f", branch], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if c.returncode != 0:
        raise SystemExit("checkout 失败：" + (c.stdout + c.stderr)[-400:])
    for rel in lane_overlays():
        src = ROOT / rel
        if not src.exists():
            raise SystemExit(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
        if md5_bytes(src) != md5_bytes(tgt):
            raise SystemExit(f"覆盖后 md5 不一致（装错树/写盘失败）：{rel}")
    # .env 不进 git ⇒ 克隆里没有则依赖 DB 的用例会 skip，控制组就不干净（skip==0 是硬门）。
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path) -> dict:
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-b20dmut")
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    p = subprocess.run(["go", "test", "-p", "1", "-count=1", "-v"] + GO_PKGS + ["-run", GO_RUN],
                       cwd=root, capture_output=True, text=True, timeout=1800, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = sorted(set(re.findall(r"^    --- FAIL: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    passed = len(re.findall(r"^--- PASS: ", out, re.M))
    failed = len(re.findall(r"^--- FAIL: ", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: ", out, re.M))
    return {"rc": p.returncode, "killed": killed, "total": passed + failed + skipped,
            "passed": passed, "failed": failed, "skipped": skipped, "out": out,
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "cannot use" in out or "undefined:" in out}


# 锚点全部取**完整语句**（含缩进），且每条锚点在两个函数里的形状互不相同。
POLL_CLAIM_SQL = ("UPDATE message_hub SET status = 'inflight', claimed_at = now(), "
                  "push_attempts = push_attempts + 1\n\t\tWHERE id IN")
POLL_BOUND = "\t\t\t  AND push_attempts < ?\n\t\t\tORDER BY id ASC LIMIT ?"
POLL_SWEEP = ('\tif err := r.exhaustOutbound(ctx, channel, accountID, cutoff); err != nil {\n'
              '\t\tfmt.Printf("[ClaimPendingOutbound] 到界行升级终态失败（继续认领）: %v\\n", err)\n\t}')
POLL_RECYCLE = 'Updates(map[string]any{"status": "pending", "claimed_at": nil})'
SSE_CLAIM_SQL = ("UPDATE message_hub SET status = 'inflight', claimed_at = now(), "
                 "push_attempts = push_attempts + 1\n\t\tWHERE id = ?")
SSE_BOUND = "AND direction = 'outbound' AND push_attempts < ?\n\t\t  AND (status = 'pending'"
FETCH_BOUND = '\t\tWhere("push_attempts < ?", MaxOutboundPushAttempts).'
FETCH_SWEEP = ('\tif err := r.exhaustOutbound(ctx, channel, accountID, cutoff); err != nil {\n'
               '\t\tfmt.Printf("[FetchOutboundUndelivered] 到界行升级终态失败（继续取待推）: %v\\n", err)\n\t}')
SWEEP_SET = "\t\tSET status = 'failed', push_error = ?, claimed_at = NULL"
SWEEP_DIR = ("WHERE platform = ? AND account_id = ? AND direction = 'outbound'\n"
             "\t\t  AND push_attempts >= ?")
SWEEP_OWED = ("AND (status = 'pending' OR (status = 'inflight' AND claimed_at IS NOT NULL "
              "AND claimed_at < ?))`,\n\t\tpushExhaustedError")
SWEEP_REASON = 'const pushExhaustedError = "outbound_push_exhausted"'

MIG_ADD_ATTEMPTS = ("`ALTER TABLE message_hub\n"
                    "\t\t\tADD COLUMN IF NOT EXISTS push_attempts INTEGER NOT NULL DEFAULT 0`")
MIG_ADD_ERROR = ("`ALTER TABLE message_hub\n"
                 "\t\t\tADD COLUMN IF NOT EXISTS push_error VARCHAR(200) NOT NULL DEFAULT ''`")
MIG_NOTABLE_GUARD = ('\tif !m.db.Migrator().HasTable("message_hub") {\n'
                     "\t\treturn nil // 表还没建（全新库由模型标签负责），此处无物可改\n\t}")
MIG_DOWN = ('\tdeclineColumnDrop(m.Version(), "message_hub.push_attempts", "message_hub.push_error")\n'
            "\treturn nil")
REG_LINE = "\tregister(NewMessageHubOutboundPushBudgetMigration(db))"


def go_mutants():
    """(格, 说明, [(文件槽, 原文, 注码), ...], 该红的腿)

    支持一格叠加多处注码（内存里叠完一次写盘），因为 A3 的「界」与「收口」互为冗余：
      - 轮询/补拉两条路径先跑 exhaustOutbound 把到界行判成 failed，之后 `push_attempts < ?`
        自然取不到它 → 单摘界不红（R1/R3 因此必须是复合格）；
      - 反过来单摘收口（R7/R8）也不红于「永推」这一条，因为界还兜着——但它必须红于
        「到界行没落终态/没写原因」，那正是收口独立承担的那一半。
    SSE 单行认领路径没有收口（它是热路径、且只针对一行），所以界在那儿是**唯一**防线，
    R2 单摘即红。三格合起来说的是同一句话：每条取行路径至少留一道防线，一条都没留才算洞。
    """
    return [
        ("R1", "轮询路径两道防线同时摘掉（界失效＋收口失效＝第 21 次照样重推）",
         [("repo", POLL_BOUND, POLL_BOUND.replace("AND push_attempts < ?",
                                                  "AND (push_attempts < ? OR true)")),
          ("repo", POLL_SWEEP, POLL_SWEEP.replace("accountID, cutoff", 'accountID+"-none", cutoff'))],
         CAP_POLL),
        ("R2", "SSE 单行认领的界摘掉（该路径没有收口，界是唯一防线）",
         [("repo", SSE_BOUND, SSE_BOUND.replace("AND push_attempts < ?",
                                                "AND (push_attempts < ? OR true)"))], CAP_SSE),
        ("R3", "补拉路径两道防线同时摘掉（第三条路径）",
         [("repo", FETCH_BOUND,
           FETCH_BOUND.replace('Where("push_attempts < ?"', 'Where("(push_attempts < ? OR true)"')),
          ("repo", FETCH_SWEEP, FETCH_SWEEP.replace("accountID, cutoff", 'accountID+"-none", cutoff'))],
         CAP_FETCH),
        ("R4", "轮询认领不计数（预算永远花不完，上界形同虚设）",
         [("repo", POLL_CLAIM_SQL, POLL_CLAIM_SQL.replace("push_attempts = push_attempts + 1",
                                                         "push_attempts = push_attempts"))], CAP_POLL),
        ("R5", "SSE 认领不计数",
         [("repo", SSE_CLAIM_SQL, SSE_CLAIM_SQL.replace("push_attempts = push_attempts + 1",
                                                        "push_attempts = push_attempts"))], CAP_SSE),
        ("R6", "超时回收把预算抹回 0（每 30s 白送一轮）",
         [("repo", POLL_RECYCLE,
           'Updates(map[string]any{"status": "pending", "claimed_at": nil, "push_attempts": 0})')],
         CAP_RECYCLE),
        ("R7", "轮询路径忘了收口（界仍兜住永推，但到界行停在 inflight、无人落终态写原因）",
         [("repo", POLL_SWEEP, POLL_SWEEP.replace("accountID, cutoff", 'accountID+"-none", cutoff'))],
         CAP_POLL),
        ("R8", "补拉路径忘了收口（纯 SSE 部署永远等不到落终态那一步）",
         [("repo", FETCH_SWEEP, FETCH_SWEEP.replace("accountID, cutoff", 'accountID+"-none", cutoff'))],
         CAP_FETCH),
        ("R9", "收口漏了 direction 过滤（入站行被出站预算判死）",
         [("repo", SWEEP_DIR, "WHERE platform = ? AND account_id = ?\n\t\t  AND push_attempts >= ?")],
         CAP_SWEEP),
        ("R10", "收口把终态行也重写（delivered 被改判 failed，审计事实被覆盖）",
         [("repo", SWEEP_OWED, SWEEP_OWED.replace("AND claimed_at < ?))",
                                                  "AND claimed_at < ?) OR status = 'delivered'))"))],
         CAP_SWEEP),
        ("R11", "到界不落终态（离开 owed 集合但没人知道它为什么不见了）",
         [("repo", SWEEP_SET, "\t\tSET status = 'pending', push_error = ?, claimed_at = NULL")],
         CAP_POLL),
        ("R12", "到界不写原因（行落终态却没说为什么）",
         [("repo", SWEEP_REASON, 'const pushExhaustedError = ""')], CAP_POLL),
        ("R13", "收口给从未交付的行盖上 sent_at（回显检测会吞掉后续回复）",
         [("repo", SWEEP_SET,
           "\t\tSET status = 'failed', push_error = ?, claimed_at = NULL, sent_at = now()")],
         CAP_SWEEP),
        ("M1", "迁移不加 push_attempts（新代码第一条 SQL 就报错）",
         [("mig", MIG_ADD_ATTEMPTS, "`SELECT 1`")], MIG_SHAPE),
        ("M2", "迁移不加 push_error", [("mig", MIG_ADD_ERROR, "`SELECT 1`")], MIG_SHAPE),
        ("M3", "push_attempts 默认值写成 7（列缺省≠代码缺省，存量行一升级就背着 7 次）",
         [("mig", MIG_ADD_ATTEMPTS, MIG_ADD_ATTEMPTS.replace("DEFAULT 0", "DEFAULT 7"))], MIG_SHAPE),
        ("M4", "放开 NOT NULL（新代码可写 NULL，计数语义退化成三态）",
         [("mig", MIG_ADD_ATTEMPTS, MIG_ADD_ATTEMPTS.replace("INTEGER NOT NULL", "INTEGER"))],
         MIG_SHAPE),
        ("M5", "去掉 HasTable 前置（全新库里 ALTER 不存在的表 = 启动链当场断）",
         [("mig", MIG_NOTABLE_GUARD, "")], MIG_NOTABLE),
        ("M6", "迁移没挂进注册链（写了等于没写）", [("reg", REG_LINE, "")], MIG_REG),
        ("M7", "Down 真去 DROP 这两列（降级销毁在用列）",
         [("mig", MIG_DOWN,
           '\tm.db.WithContext(ctx).Exec(`ALTER TABLE message_hub DROP COLUMN IF EXISTS '
           'push_attempts, DROP COLUMN IF EXISTS push_error`)\n\treturn nil')], MIG_SHAPE),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--js-only", action="store_true")
    ap.add_argument("--go-only", action="store_true")
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    tmp, owned = workdir(args.clone or None, prefix="b20dmut-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}", flush=True)
    problems = []

    if not args.go_only:
        work = js_prepare(tmp)
        prim = work / JS_PRIM_REL
        const = work / JS_CONST_REL
        orig, c_orig = read(prim), read(const)
        base_md5, c_base_md5 = md5_bytes(prim), md5_bytes(const)
        rc = js_run(work)
        CONTROL["js"] = rc["total"]
        print(f"\n[JS] 控制组 rc={rc['rc']} total={rc['total']} failed={rc['failed']} "
              f"skipped={rc['skipped']} 红名={rc['killed']}")
        if rc["rc"] != 0 or rc["total"] == 0 or rc["skipped"] > 0 or rc["killed"]:
            print(rc["out"][-3000:])
            raise SystemExit("[JS] 控制组不干净——后面所有红/绿都不可信")
        jskill = {}
        for code, desc, expect in js_mutants():
            try:
                new_p, new_c = apply_js(orig, c_orig, code)
            except SystemExit as e:
                problems.append(str(e))
                continue
            ok, err = syntax_ok_js(new_p if new_p != orig else new_c)
            if not ok:
                problems.append(f"[JS] {code} 注码语法坏（跑出来只会是编译红）：{err[:120]}")
                continue
            prim.write_text(new_p, encoding="utf-8")
            const.write_text(new_c, encoding="utf-8")
            r = js_run(work)
            v = verdict(r, CONTROL["js"])
            if expect not in r["killed"]:
                v = v + f"（未点出 {expect[:28]}）" if v == "杀掉" else v
            print(f"{code:<4} {desc[:56]:<58} {v:<7} total={r['total']} fail={r['failed']} "
                  f"skip={r['skipped']} ｜ " + " | ".join(k[:64] for k in r["killed"][:2]))
            if not v.startswith("杀掉"):
                problems.append(f"[JS] {code} {v}：{desc}")
                print(r["out"][-2500:])
            jskill[code] = set(r["killed"])
            prim.write_text(orig, encoding="utf-8")
            const.write_text(c_orig, encoding="utf-8")
            if md5_bytes(prim) != base_md5 or md5_bytes(const) != c_base_md5:
                raise SystemExit(f"[JS] {code} 还原后 md5 不一致，停机")
        dup_report("JS", jskill)
        print("[JS] 已全量还原（md5 一致）")

    if not args.js_only:
        clone = go_prepare(tmp)
        files = {"repo": clone / REPO_REL, "mig": clone / MIG_REL, "reg": clone / REG_REL}
        for name, p in files.items():
            if not p.exists():
                raise SystemExit(f"注码目标文件不在克隆里：{p}")
        originals = {name: read(p) for name, p in files.items()}
        basemd5 = {name: md5_bytes(p) for name, p in files.items()}
        cells = []
        for code, desc, edits, expect in go_mutants():
            # 复合格：一条取行路径上「界」与「收口」互为冗余，必须同时摘掉才看得见永推。
            # 多处注码先在**内存里累加**、最后一次落盘；锚点唯一性按累加后的文本算，
            # 否则同文件两刀各对原文命中一次、合起来却互相吃掉锚点也查不出来。
            acc: dict[str, str] = {}
            for slot, old, new in edits:
                cur = acc.get(slot, originals[slot])
                hit = cur.count(old)
                if hit != 1:
                    raise SystemExit(f"{code} 锚点在 {slot} 里命中 {hit} 次（要求恰好 1）：{old[:70]!r}")
                if old == new:
                    raise SystemExit(f"{code} 注码无效（原文与注码后一致）")
                acc[slot] = cur.replace(old, new, 1)
            for slot, src in acc.items():
                if src == originals[slot]:
                    raise SystemExit(f"{code} 注码无效（{slot} 替换后与原文件一致）")
                ok, err = syntax_ok_go(src)
                if not ok:
                    raise SystemExit(f"{code} 注码语法坏，跑出来只会是 build failed：{err[:160]}")
            cells.append((code, desc, acc, expect))
        print(f"\n[Go] 注码前置：{len(cells)} 格锚点各命中一次 + 注码后语法可解析")
        r = go_run(clone)
        CONTROL["go"] = r["total"]
        print(f"[Go] 控制组 rc={r['rc']} total={r['total']} passed={r['passed']} "
              f"skip={r['skipped']} FAIL={r['killed']}")
        if r["rc"] != 0 or r["total"] == 0 or r["skipped"] > 0 or r["killed"]:
            print(r["out"][-4000:])
            raise SystemExit("[Go] 控制组不干净")
        gkill = {}
        for code, desc, acc, expect in cells:
            for slot, src in acc.items():
                files[slot].write_text(src, encoding="utf-8")
            for slot in acc:
                if md5_bytes(files[slot]) == basemd5[slot]:
                    raise SystemExit(f"{code} 注码未生效（{slot} 与原内容一致）")
            r = go_run(clone)
            v = verdict(r, CONTROL["go"])
            if v == "杀掉" and expect not in r["killed"]:
                v = f"红了但没点出 {expect}"
            print(f"{code:<4} {desc[:56]:<58} {v:<7} total={r['total']} fail={r['failed']} "
                  f"skip={r['skipped']} ｜ " + " | ".join(k[:60] for k in r["killed"][:2]))
            if not v.startswith("杀掉"):
                problems.append(f"[Go] {code} {v}：{desc}")
                print(r["out"][-3000:])
            gkill[code] = set(r["killed"])
            for slot in acc:
                files[slot].write_text(originals[slot], encoding="utf-8")
            for slot in acc:
                if md5_bytes(files[slot]) != basemd5[slot]:
                    raise SystemExit(f"{code} 还原后 md5 不一致，停机")
        dup_report("Go", gkill)
        print("[Go] 已全量还原（md5 一致）")

    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    if problems:
        print("\n===== 电池判定：有洞 =====")
        for x in problems:
            print("  ✗", x)
        return 1
    parts = []
    if not args.go_only:
        parts.append(f"JS {len(js_mutants())} 格")
    if not args.js_only:
        parts.append(f"Go {len(go_mutants())} 格")
    print(f"\n===== 电池判定：{' + '.join(parts)} 逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
