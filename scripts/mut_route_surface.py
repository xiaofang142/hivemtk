#!/usr/bin/env python3
"""路由声明面守卫的变异电池：逐刀验"守卫里每句判据真的会开火"。

## 为什么单独立一支

`user-server/internal/router/route_surface_consistency_test.go` 守的是四个面的一致性：
接口注解 ↔ 真实路由表 ↔ 端到端冒烟清单 ↔ 服务端当场生成、由终端用户浏览器执行的落地页模板。
它立项的依据是两类真实事故——
下线的端点注解没跟着删（swagger 对外宣称根本不存在的接口），以及注解在、路径在、
但两者不属于同一个函数（详情路由由 `Customer360Controller.GetCustomerDetail` 服务，
注解却写在另一个控制器的同名方法上）。第二类只判"路径在不在路由表里"的门完全量不到。

守卫写出来只是一半。它的每条判据（幽灵路径、归属错位、参数段写法、清单死行、覆盖下界、
扫描根空转、前端静态面前提、落地页引用未注册的口、豁免失效反向棘轮、键形状空转自检）都是一句
`t.Errorf`/`t.Fatalf`，而它有没有牙只能靠
"人为把它的前置破坏一次，看它是否正好红在那句话上"来证。这一族过去每次都靠一次性临时脚本取证，
跑完就没人能复跑，守卫于是退化成橡皮章。本文件把那趟取证变成常驻可重跑。

## 判据形状

- 每格期望"红名集合恰好等于推演的那条用例 **且** 红因里点名到本格该打的这句判据"。
  只判"红了"会把连带面当证据。
- 控制组**现测**（用例名单与 settled 数都从跑出来的输出里取，不写死）：共享工作树下别人往
  `internal/router` 加用例会让写死的计数漂，漂了就是假红。
- G8 是"拆判据"格，期望**全绿**：它同时打上归属谎言并摘掉 `handlerNames` 的比对。
  绿才说明归属那条腿是这类谎言的唯一拦截面；若它反而红，说明另有腿在管、这格冗余，同样出声。
- G9/G10/G11/G12 打的是第四条腿（服务端当场生成、由终端用户浏览器执行的落地页模板）：
  G9 往活码页的点击上报之后**追加**一个没注册过的口，G10 把闲鱼卡片里**已登记豁免**的那条上报整行摘掉——
  后者验的是豁免表的反向棘轮（口哪天注册上了或上报被删了，登记它的人必须回来删这行，否则门红）。
  G11 反过来打这条腿自己：把拼键正则的前导斜杠挪出捕获组，所有模板引用都会对不上路由表——
  这条腿有过一次真坏法（已注册的上报口被报成 404、豁免表整批失配，门一脸证据地判红了 4 条），
  所以它必须自曝"键的形状对不上、比对已经空转"，而不是留下一串假 404 让人去查接口。
  G12 把四条 /api 上报整面改走 /local/，验"这一面离开了门的面"那支自检要自杀而不是报"零问题"。
  G9 为什么是追加而不是顶掉：顶掉的话，模板里唯一命中路由表的那个正样本就没了，
  空转自检会抢在幽灵判据之前开火（实测第一版就这样 BROKEN：红而不点名）——
  注码必须走得到那句判据，而不是把它的前置一起拆掉。
- 环境前提不满足（盘、库、克隆）退 ENV-BROKEN（rc=4）且**不**印"全杀"。

## 口径（照本仓既有电池的规矩）

- 只在 `git clone --shared` 出来的私有克隆里注码，本泳道的脏/未跟踪文件按 `git status` 现取覆盖进去，
  绝不碰调用方的工作树；
- 一格多处编辑在内存里叠完再一次写盘；
- 每刀还原后逐槽比 md5，不等即停机（现场只活在克隆里，`leave_for_evidence` 让路不回收）；
- BUILD-BROKEN / panic 带走整包（settled 掉）/ 红而不点名 ⇒ 一律 BROKEN，不计入杀掉；
- 逐格产物落到 `docs/superpowers/specs/ledger/logs/RouteSurface/<tag>/`，tag 默认取本地时间戳，
  复跑不覆盖上一轮（该轮次目录要在 `.gitignore` 里成对放行，否则"取证物已入库"
  只是作者机器上的事实）。

用法：
    python3 scripts/mut_route_surface.py --check        # 只做工作树静态自检，不建克隆、不放刀
    python3 scripts/mut_route_surface.py                # 全族
    python3 scripts/mut_route_surface.py --only G2,G8   # 只跑指定格（终态会写明本趟是子集，门不认）
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

from mut_dispose import dispose, dispose_at_exit, leave_for_evidence, workdir
from redact import scrub

ROOT = Path(__file__).resolve().parent.parent      # 脚本住在 <repo>/scripts/，不写死仓名
US = "user-server"
GUARD = f"{US}/internal/router/route_surface_consistency_test.go"
ANN = f"{US}/internal/controller/customer_360.go"
TSV = f"{US}/tests/e2e/routes_user.tsv"
# 落地页那一面（守卫的第四条腿管的就是它）：两份模板分别承载两格——
# live_code.html 用来注入"页面里引用了一个没注册的口"，
# xianyu_card.html 用来把已登记豁免的那条上报摘掉、验"豁免失效"反向棘轮会不会开火。
PAGE = f"{US}/internal/reach/card/template/live_code.html"
CARD = f"{US}/internal/reach/card/template/xianyu_card.html"

# 槽表：`scripts/anchor-preflight.py` 的形状 B 适配器要的是 {槽名: 仓内相对路径}，
# 注码格表里只出现槽名，路径只在这里登记一次。
SLOT_FILES = {"guard": GUARD, "ann": ANN, "tsv": TSV, "page": PAGE, "card": CARD}
SLOT_OF = {v: k for k, v in SLOT_FILES.items()}
LANE_FILES = sorted(SLOT_FILES.values())

PKG = "./internal/router/"
T_ANN = "TestAnnotationsMatchLiveRoutes"
T_INV = "TestE2EInventoryMatchesLiveRoutes"
T_PAGE = "TestPageTemplateLinksMatchLiveRoutes"
FILTER = f"^({T_ANN}|{T_INV}|{T_PAGE})$"
DEFAULT_LOGS = "docs/superpowers/specs/ledger/logs/RouteSurface"
ANSI = re.compile(r"\x1b\[[0-9;]*m")

# 清单锚行：唯一、且确实是 /api/ 行（G4 在它后面追加、G5 把它删掉，两刀共用一个锚点）。
UPLOAD_ROW = "user\tPOST\t/api/upload\n"
# 追加的这条必须真"不在路由表里"：名字带 battery，任何真实端点都不会撞。
DEAD_ROW = "user\tGET\t/api/route-surface-battery-injected-row\n"

A_TAGS_ROUTE = "// @Router /api/customer/{id}/tags [post]\n"
A_TAGS_ROUTE_GHOST = "// @Router /api/customer/{id}/tagsX [post]\n"
A_DETAIL_ROUTE = "// @Router /api/customer/{id} [get]\n"
A_DETAIL_ROUTE_COLON = "// @Router /api/customer/:id [get]\n"
A_UPDATE_SIG = ("// UpdateCustomer 更新客户信息（兼容前端 PUT /api/customer/:id）\n"
                "func (c *Customer360Controller) UpdateCustomer(ctx *gin.Context) {\n")
A_UPDATE_SIG_LIE = ("// UpdateCustomer 更新客户信息（兼容前端 PUT /api/customer/:id）\n"
                    "// @Summary 谎称这个函数服务客户详情\n"
                    "// @Router /api/customer/{id} [get]\n"
                    "func (c *Customer360Controller) UpdateCustomer(ctx *gin.Context) {\n")
A_SCAN_ROOT = 'var annotationScanRoot = filepath.Join("..", "..")\n'
A_SCAN_ROOT_EMPTY = 'var annotationScanRoot = filepath.Join("..", "..", "docs")\n'
A_DIST_SEED = '\tt.Setenv("USER_WEB_DIST", t.TempDir())\n'
A_HANDLER_NAMES = "\t\treturn strings.Contains(ginName, owner)\n"
A_HANDLER_NAMES_BLIND = "\t\treturn true\n"

# 落地页四刀的锚点：都是模板里那一行 fetch 串（在 html 里唯一）。
A_PAGE_BEACON = "fetch('/api/livecode/{{.ID}}/click'"
A_PAGE_BEACON_GHOST = "fetch('/api/route-surface-battery-beacon/{{.ID}}/click'"
# G9 用"追加"而不是"顶掉"：活码页那条上报是这一面**唯一的正样本**（模板里 4 个引用中
# 唯一命中路由表的那个），把它换成幽灵串会让"一条都没命中路由表"的空转自检先开火，
# 幽灵判据反而永远轮不到——注码要走得到那句判据，不能把它的前置一起拆掉。
# 因此在原 fetch 之后补一段完整、语法仍成立的第二个 fetch。
A_PAGE_BEACON_APPEND = (A_PAGE_BEACON + ", {\n"
                        "                method: 'POST',\n"
                        "            });\n"
                        "            " + A_PAGE_BEACON_GHOST)
A_CARD_BEACON = "fetch('/api/xianyu/{{.ID}}/click'"
A_CARD_BEACON_SHARE = "fetch('/api/xianyu/{{.ID}}/share'"
A_CARD_BEACON_VIEW = "fetch('/api/xianyu/{{.ID}}/view'"
A_CARD_BEACON_REMOVED = "fetch('/xianyu-click-report-removed'"
# G12 用的"整面消失"注码：把四条 /api 上报都改写到 /local/ 下，模板里再没有 /api 引用。
NO_API = {a: a.replace("/api/", "/local/") for a in
          (A_PAGE_BEACON, A_CARD_BEACON, A_CARD_BEACON_SHARE, A_CARD_BEACON_VIEW)}
# 第四腿拼键的那处正则：把前导斜杠从捕获组里挪出去，键就成了 "POST api/livecode/:x/click"。
# 这不是假想的坏法，是本腿写出来时真踩过的一次——那时已注册的上报口被报成 404、
# 豁免表整批失配，门一脸证据地判红，测的却是它自己拼键的那行代码。
A_LINK_RE = r'''["'\x60](/api/'''
A_LINK_RE_SLASHOUT = r'''["'\x60]/(api/'''

K_GHOST = "声明了路由表里不存在的端点"
K_MISPLACE = "挂错了函数"
K_FORM = "冒号写法"
K_DEADROW = "不指向任何已注册路由"
K_RATCHET = "低于下界"
K_EMPTYSCAN = "一条接口注解都没扫到"
K_PAGEGHOST = "落地页模板里有"
K_STALEEXEMPT = "豁免表里"
K_KEYSILT = "键的形状对不上"
K_NOREF = "一个 /api 引用都没扫到"

# 每格：(格号, 破坏的是哪句承诺, [(槽, 原文, 注码)], 该红的用例, 该点名的判据, 是否期望全绿)
CELLS = [
    ("G1", "注解指向路由表里不存在的端点（幽灵接口进 swagger）",
     [("ann", A_TAGS_ROUTE, A_TAGS_ROUTE_GHOST)], (T_ANN,), K_GHOST, False),
    ("G2", "注解挂在并不服务该路由的函数上（路径在、归属错）",
     [("ann", A_UPDATE_SIG, A_UPDATE_SIG_LIE)], (T_ANN,), K_MISPLACE, False),
    ("G3", "参数段退回 gin 的冒号写法（OpenAPI 路径模板非法）",
     [("ann", A_DETAIL_ROUTE, A_DETAIL_ROUTE_COLON)], (T_ANN,), K_FORM, False),
    ("G4", "冒烟清单里出现不指向任何路由的死行",
     [("tsv", UPLOAD_ROW, UPLOAD_ROW + DEAD_ROW)], (T_INV,), K_DEADROW, False),
    ("G5", "清单覆盖的 /api/ 条数跌破棘轮下界",
     [("tsv", UPLOAD_ROW, "")], (T_INV,), K_RATCHET, False),
    ("G6", "注解扫描根空转（一条注解都扫不到时门要自杀而不是放行）",
     [("guard", A_SCAN_ROOT, A_SCAN_ROOT_EMPTY)], (T_ANN,), K_EMPTYSCAN, False),
    ("G7", "撤掉前端静态面的 dist 前提（清单里那三条静态行会变死行）",
     [("guard", A_DIST_SEED, "")], (T_INV,), K_DEADROW, False),
    ("G8", "打归属谎言的同时摘掉 handlerNames 比对（期望全绿＝归属腿是谎言的唯一拦截面）",
     [("ann", A_UPDATE_SIG, A_UPDATE_SIG_LIE), ("guard", A_HANDLER_NAMES, A_HANDLER_NAMES_BLIND)],
     (), None, True),
    ("G9", "落地页模板里引用一个没注册的口（终端用户点了只会拿到 404）",
     [("page", A_PAGE_BEACON, A_PAGE_BEACON_APPEND)], (T_PAGE,), K_PAGEGHOST, False),
    ("G10", "把已登记豁免的那条上报从模板里摘掉（豁免表里的条目随之失效，反向棘轮该开火）",
     [("card", A_CARD_BEACON, A_CARD_BEACON_REMOVED)], (T_PAGE,), K_STALEEXEMPT, False),
    ("G11", "把第四个面的键拼错（前导斜杠落到捕获组外）：门要自曝空转，而不是把已注册的口报成一串 404",
     [("guard", A_LINK_RE, A_LINK_RE_SLASHOUT)], (T_PAGE,), K_KEYSILT, False),
    ("G12", "模板里再没有任何 /api 引用（这一面整块离开门的面）：门要自杀，不能读成\"零问题\"",
     [("page", A_PAGE_BEACON, NO_API[A_PAGE_BEACON]),
      ("card", A_CARD_BEACON, NO_API[A_CARD_BEACON]),
      ("card", A_CARD_BEACON_SHARE, NO_API[A_CARD_BEACON_SHARE]),
      ("card", A_CARD_BEACON_VIEW, NO_API[A_CARD_BEACON_VIEW])],
     (T_PAGE,), K_NOREF, False),
]


def cells():
    """`scripts/anchor-preflight.py` 形状 B 的投影：(格号, 说明, [(槽, 原文, 注码)], 该红的腿)。

    判定本体读的是 `CELLS`（多带期望判据字面量与"拆判据格"两列），这里只做投影，
    两张表同源，不会各说各话。
    """
    return [(code, desc, edits, expect) for code, desc, edits, expect, _kw, _green in CELLS]


ENV_SIGNS = ("connection refused", "failed to connect", "too many clients",
             "no such host", "timeout awaiting response", "dial tcp")

# 本泳道的覆盖集：守卫读的那批接口注解，此刻还修在**未提交字节**上的文件全列在这。
# 为什么是一份点名清单、而不是"把 internal/ 下所有脏文件都搬进来"：
# 共享工作树里别的泳道随时可能压着一个编译不过的文件（`git status` 现数），
# 那会让这枚门红在别人的语法错误上，而红因写得像"守卫坏了"。
# 提交进 HEAD 之后这份清单会自然收缩成空操作（下面按"相对 HEAD 有差异"过滤）。
LANE_OVERLAYS = (
    GUARD, ANN, TSV, PAGE, CARD,
    f"{US}/docs/docs.go", f"{US}/docs/swagger.json", f"{US}/docs/swagger.yaml",
    f"{US}/internal/controller/alert_rule.go",
    f"{US}/internal/controller/customer.go",
    f"{US}/internal/controller/customer_event.go",
    f"{US}/internal/controller/customer_oneid.go",
    f"{US}/internal/controller/recovery_queue.go",
    f"{US}/internal/controller/workflow_orchestrator.go",
    f"{US}/internal/geo/controller/alert.go",
    f"{US}/internal/service/oneid_merge_rule.go",
    # 第二批六份不属于本泳道：守卫的第一条腿要把 `internal/controller` 下每条注解和真实路由表对，
    # 而 HEAD 上这六份文件里有 14 条注解宣称的端点根本不在路由表里（`/public/register`、
    # `/api/auth/refresh`、`/api/sops` 那一族），修正后的字节只存在于并行泳道的未提交改动里。
    # 不覆盖进来的话控制组红在别人的在途修复上，红因写得像"守卫在基线上就坏"（实测 2026-10-10
    # 23:27 一轮：`14/145 条` ⇒ ENV-BROKEN、rc=4、八格一刀未放）。这一族条目由笔尖上那句
    # "相对 HEAD 有差异才覆盖"自动清账：他们把那笔提交进来后这里就不搬任何东西。
    f"{US}/internal/controller/auth.go",
    f"{US}/internal/controller/clue.go",
    f"{US}/internal/controller/intent.go",
    f"{US}/internal/controller/sales_persona.go",
    f"{US}/internal/controller/short_link.go",
    f"{US}/internal/controller/sop.go",
)


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


# ---------------------------------------------------------------- 静态自检


def preflight() -> int:
    """工作树字节上能核的四件事：锚点命中一次、注码与原文不同、判据字面量还在守卫里、
    期望用例真有 func 定义。

    边界要写明：这一档只挡"锚点/字面量根本不存在"（抄错字、判据被改写、用例改名），
    挡不住"注了码而门不开火"——那一半只有真跑能照出来，`--check` 绿**不是**电池有牙的证据。
    """
    texts = {}
    for rel in LANE_FILES:
        p = ROOT / rel
        if not p.exists():
            print(f"BROKEN=文件不在位：{rel}")
            return 2
        texts[rel] = read(p)
    guard = texts[GUARD]
    bad = []
    for code, _desc, edits, expect, keyword, expect_green in CELLS:
        acc = {}
        for slot, old, new in edits:
            rel = SLOT_FILES[slot]
            cur = acc.get(rel, texts[rel])
            n = cur.count(old)
            if n != 1:
                bad.append(f"{code} BROKEN=锚点在 {rel} 命中 {n} != 1：{old[:60]!r}")
                continue
            if old == new:
                bad.append(f"{code} BROKEN=锚点与注码相同（无效变异）")
                continue
            acc[rel] = cur.replace(old, new, 1)
        for leg in expect:
            if f"func {leg}(t *testing.T)" not in guard:
                bad.append(f"{code} BROKEN=用例 func {leg} 在守卫里查无（改名或删掉了）")
        if not expect_green:
            if keyword is None:
                bad.append(f"{code} BROKEN=没写期望判据字面量")
            elif keyword not in guard:
                bad.append(f"{code} BROKEN=判据字面量 {keyword!r} 不在守卫源码里"
                           "（那句话被改写过，期望要跟着订正）")
    if bad:
        print("\n".join(bad))
        print(f"锚点前置有 {len(bad)} 项 BROKEN，先修锚点，不许改期望")
        return 1
    print(f"锚点前置：{len(CELLS)} 格、每格锚点在 {ROOT.name} 的工作树里各命中一次，"
          "判据字面量与用例名都在守卫源码里查得见")
    print("注：这一档不装架、不跑 go test ⇒ 它绿不等于电池有牙。")
    return 0


# ---------------------------------------------------------------- 装架


def lane_overlays() -> list[str]:
    """LANE_OVERLAYS 里此刻相对 HEAD 有差异的文件（含未跟踪的新文件）。"""
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"]
                       + list(LANE_OVERLAYS),
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise RuntimeError("git status 失败，拿不到脏文件清单：" + r.stderr[-200:])
    dirty = []
    for line in r.stdout.splitlines():
        p = line[3:].split(" -> ")[-1].strip().strip('"')
        if p in LANE_OVERLAYS and (ROOT / p).exists():
            dirty.append(p)
    return sorted(dirty)


def prepare(dst: Path, owned: bool) -> Path:
    def bail(msg: str) -> None:
        """克隆已建起来之后的中止路：先回收私有克隆再出声（一轮几十 MB，磁盘常态 97% 满）。"""
        if (dst / "clone").exists():
            dispose(dst, owned=owned, keep=False, repo_root=ROOT)
        raise SystemExit(msg)

    clone = dst / "clone"
    if clone.exists():
        raise SystemExit(f"{clone} 已存在（换 --clone 目录或先删）")
    r = subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(clone)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("克隆失败：" + (r.stdout + r.stderr)[-400:])
    branch = subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--abbrev-ref", "HEAD"],
                            capture_output=True, text=True).stdout.strip() or "master"
    b = subprocess.run(["git", "checkout", "-f", branch], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if b.returncode != 0:
        bail("checkout 失败：" + (b.stdout + b.stderr)[-400:])
    try:
        overlays = lane_overlays()
    except RuntimeError as exc:
        bail(str(exc))
    for rel in overlays:
        src = ROOT / rel
        if not src.exists():
            bail(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
    if overlays:
        print(f"装架：克隆 {branch} + 覆盖 {len(overlays)} 个本泳道脏/未跟踪文件"
              "（其余面走 HEAD 字节）：" + ", ".join(overlays))
    else:
        print(f"装架：克隆 checkout 到的就是守卫与注码文件的当前字节（工作树对这三个槽无差异）")
    for env in (ROOT / ".env", ROOT / US / ".env"):
        if env.exists():
            shutil.copy2(env, clone / US / ".env")
            break
    return clone


def db_password() -> str:
    if os.environ.get("POSTGRES_TEST_PASSWORD"):
        return os.environ["POSTGRES_TEST_PASSWORD"]
    for env in (ROOT / ".env", ROOT / US / ".env"):
        if env.exists():
            for line in read(env).splitlines():
                if line.startswith("POSTGRES_PASSWORD="):
                    return line.split("=", 1)[1].strip()
    return ""


def test_env() -> dict:
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", os.environ.get("R45_MUT_GOCACHE", "/tmp/gocache-r45mut"))
    # 端口刻意不设：internal/pkg/testutil 自己按 POSTGRES_TEST_PORT → USER_POSTGRES_HOST_PORT
    # → DB_PORT → 8232 → 8202 的顺序探测可建连的宿主机端点。写死一个端口就把探测能力关掉了，
    # 换一台机器只会得到"连不上 ⇒ 控制组不干净"的假环境红。
    if not env.get("POSTGRES_TEST_PASSWORD") and not env.get("POSTGRES_PASSWORD"):
        pw = db_password()
        if not pw:
            raise SystemExit("ENV-BROKEN：拿不到测试库口令（.env 不在位、环境也没给）")
        env["POSTGRES_TEST_PASSWORD"] = pw
    env.setdefault("EMBEDDING_ALLOW_FALLBACK", "true")
    return env


# ---------------------------------------------------------------- 跑与判


def run_guard(clone: Path, env: dict, timeout: int = 2400) -> dict:
    argv = ["go", "test", PKG, "-run", FILTER, "-count", "1", "-v", "-timeout", "20m"]
    p = subprocess.run(argv, cwd=clone / US, capture_output=True, text=True,
                       timeout=timeout, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    top = lambda kind: len(re.findall(rf"^--- {kind}: ", out, re.M))
    return {"rc": p.returncode, "out": out,
            "settled": top("PASS") + top("FAIL") + top("SKIP"),
            "skipped": top("SKIP"),
            "ran": sorted(re.findall(r"^--- (?:PASS|FAIL): (\S+)", out, re.M)),
            "red": sorted({m.split("/")[0] for m in re.findall(r"^--- FAIL: (\S+)", out, re.M)}),
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "undefined:" in out
                           or "declared and not used" in out}


def causes(out: str) -> list[str]:
    keep = [l.strip()[:220] for l in out.splitlines()
            if re.match(r"^\s{2,}\S+\.go:\d+:", l) or "panic:" in l]
    return keep[:10]


def env_broken(out: str) -> bool:
    low = out.lower()
    return any(s in low for s in ENV_SIGNS)


def apply_edits(files: dict, originals: dict, edits) -> tuple[bool, str, list[str]]:
    """同一格的多处编辑在内存里叠完再一次写盘；任一锚点命中数 != 1 就不落盘。"""
    staged: dict[str, str] = {}
    for slot, old, new in edits:
        base = staged.get(slot, originals[slot])
        if base.count(old) != 1:
            return False, f"BROKEN=锚点命中 {base.count(old)} != 1（{SLOT_FILES[slot]}）", []
        staged[slot] = base.replace(old, new, 1)
    for slot, text in staged.items():
        files[slot].write_text(text, encoding="utf-8")
    return True, "", sorted(staged)


# ---------------------------------------------------------------- main


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true",
                    help="只做工作树静态自检，不建克隆、不放刀、不跑 go test")
    ap.add_argument("--only", default="", help="逗号分隔格名；终态会写明本趟是子集")
    ap.add_argument("--clone", default="")
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--logs", default=DEFAULT_LOGS)
    ap.add_argument("--tag", default=time.strftime("%Y%m%d-%H%M%S"))
    args = ap.parse_args()

    if args.check:
        return preflight()

    only = {c.strip() for c in args.only.split(",") if c.strip()}
    allcodes = {c[0] for c in CELLS}
    if only and not only <= allcodes:
        raise SystemExit(f"--only 里有不存在的格：{sorted(only - allcodes)}")

    st = os.statvfs("/tmp")
    free = st.f_bavail * st.f_frsize
    if free < 8 * 1024 ** 3:
        print(f"ENV-BROKEN：/tmp 只剩 {free // 1024 ** 2} MB，装不下私有克隆与构建缓存（盘满的红全是假红）")
        return 4

    logs = ROOT / args.logs / args.tag
    logs.mkdir(parents=True, exist_ok=True)
    from battlog import tee_to
    tee_to(logs / "00-run.log")
    tmp, owned = workdir(args.clone, prefix="routesurfacemut-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)

    head = subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--short", "HEAD"],
                          capture_output=True, text=True).stdout.strip()
    load = subprocess.run(["uptime"], capture_output=True, text=True).stdout.strip()
    print(f"私有作业目录：{tmp}\n逐格日志目录：{logs}")
    print(f"取证基线：HEAD={head} 工作树={ROOT} load={load} 日志 tag={args.tag}")

    clone = prepare(tmp, owned)
    dispose_at_exit(tmp, owned=owned, keep=args.keep, repo_root=ROOT)

    files = {slot: clone / rel for slot, rel in SLOT_FILES.items()}
    missing = [slot for slot, p in files.items() if not p.exists()]
    if missing:
        print("ENV-BROKEN：克隆里缺槽文件（装架没生效？）：" + ", ".join(missing))
        return 4
    originals = {slot: read(p) for slot, p in files.items()}
    base = {slot: md5_bytes(p) for slot, p in files.items()}
    print("本轮基线字节（注码前 == 还原后 才算干净）：")
    for slot in sorted(SLOT_FILES):
        print(f"  {base[slot]}  {SLOT_FILES[slot]}")

    env = test_env()
    problems: list[str] = []

    c = run_guard(clone, env)
    (logs / "control.log").write_text(scrub(c["out"]))
    print(f"[控制组] rc={c['rc']} settled={c['settled']} skip={c['skipped']} 红名={c['red'] or '—'}")
    if c["rc"] != 0 or c["settled"] == 0 or c["skipped"] or c["red"]:
        for line in causes(c["out"]):
            print("   红因: " + line)
        if c["buildfailed"]:
            print("ENV-BROKEN：克隆编译不过——覆盖集没兜住的那份脏字节在别人手上（本门不代修，"
                  "也别把编译红读成守卫红），停机")
        elif env_broken(c["out"]):
            print("ENV-BROKEN：控制组连不上测试库/超时——后面所有红与绿都不可信，停机")
        else:
            print("ENV-BROKEN：守卫在基线上就红——先修守卫，再谈它有没有牙（停机）")
        return 4
    control_settled, control_ran = c["settled"], c["ran"]
    want_names = sorted({leg for _, _, _, expect, _, _ in CELLS for leg in expect})
    missing_names = [n for n in want_names if n not in control_ran]
    if missing_names:
        print("!! 期望红名不在控制组名单里（用例被改名/删掉/漏跑）—— 停机")
        for m in missing_names:
            print("   缺: " + m)
        return 2

    for code, desc, edits, expect, keyword, expect_green in CELLS:
        if only and code not in only:
            print(f"{code:<4} {desc[:56]:<60} 跳过（--only 未选）")
            continue
        ok, why, edited = apply_edits(files, originals, edits)
        if not ok:
            problems.append(f"{code} {why}")
            print(f"{code:<4} {desc[:56]:<60} BROKEN=锚点")
            continue
        if all(md5_bytes(files[slot]) == base[slot] for slot in edited):
            problems.append(f"{code} BROKEN=注码后字节未变（无效变异）")
            print(f"{code:<4} {desc[:56]:<60} BROKEN=无效变异")
            continue
        r = None
        try:
            r = run_guard(clone, env)
            (logs / f"{code}.log").write_text(scrub(r["out"]))
        finally:
            for slot in edited:
                files[slot].write_text(originals[slot], encoding="utf-8")
        drifted = [slot for slot in edited if md5_bytes(files[slot]) != base[slot]]
        if drifted:
            print(f"!! {code} 还原后 md5 不一致：{[SLOT_FILES[s] for s in drifted]} —— "
                  "现场留在克隆里不回收，停机")
            leave_for_evidence(f"{code} 还原后 md5 不一致")
            return 2
        if r["buildfailed"]:
            v = "BROKEN=编译红"
        elif r["panicked"]:
            v = "BROKEN=panic 带走整包"
        elif r["settled"] != control_settled or r["skipped"]:
            v = f"BROKEN=没跑完(settled={r['settled']}/{control_settled})"
        elif expect_green:
            v = "拆判据后谎言漏网（归属腿独有牙，符合预期）" if r["rc"] == 0 and not r["red"] \
                else "BROKEN=摘了归属比对仍红（另有腿在管，本格判据要重推）"
        elif not r["red"]:
            v = "存活=洞" if r["rc"] == 0 else "BROKEN=红了却读不到用例名"
        elif r["red"] != sorted(expect):
            v = "BROKEN=红集合不符"
        elif keyword not in r["out"]:
            v = "BROKEN=红而不点名"
        else:
            v = "杀掉"
        print(f"{code:<4} {desc[:56]:<60} {v:<26} rc={r['rc']} 红名={r['red'] or '—'}")
        killed = v == "杀掉" or (expect_green and v.startswith("拆判据"))
        if not killed:
            problems.append(f"{code} {v}：{desc}")
            for line in causes(r["out"])[:3]:
                print("     红因: " + line)
            if v == "BROKEN=红集合不符":
                print(f"     期望={sorted(expect)} 实际={r['red']}")

    for slot, p in files.items():
        if md5_bytes(p) != base[slot]:
            print(f"!! 收尾 md5 校验失败：{SLOT_FILES[slot]} 没回到注码前")
            return 2

    done = len(only) if only else len(CELLS)
    print("\n===== 判定：" + (f"{done} 格逐刀被杀，无存活" if not problems
                            else f"{len(problems)} 格未杀/BROKEN：" + "; ".join(problems)))
    print("OK：源文件逐字节还原（" + " ".join(f"{s}={base[s][:8]}" for s in sorted(SLOT_FILES))
          + f"），日志在 {logs}")
    if only:
        print("   注：本次按 --only 只跑了窄口子，**门禁不认这一行**，认的是不带 --only 的全量。")
    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
