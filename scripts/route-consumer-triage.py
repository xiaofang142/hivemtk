#!/usr/bin/env python3
"""路由消费面分诊器（可重跑版）：把 Setup() 的活路由表 × 三面消费证据分四档。

## 它补的是哪个洞
2026-10-11 那轮僵尸接口分诊（工作区根的 `ZOMBIE_API_TRIAGE.md`）算出
「命中 client / 只命中 ops·e2e·self-page / 只有弱证据 / 三面全不命中」四档，
但用的是一次性脚本：那份脚本没入库，事后既复算不出那四个数，
也拿不到"三面全不命中"那批口的**名册**——而"补前端接线还是降级删除"这个待决的产品决策
需要的正是名册，不是计数。本脚本把同一件事变成仓库里能重跑的出口。

## 事实源
路由表 = `go test -run '^TestDumpLiveRouteTable$' ./internal/router`（需测试库）导出的 TSV，
逐行自带 `METHOD  PATH  HANDLER`。这张表来自生产装配入口 `router.Setup()`，
所以 `frontend_aliases.go` 里 `doReg/doRegAdmin` 这类"静态正则拼不出全路径"的注册也在内——
那正是旧的 `API_PAGE_INVENTORY.md`（1114 条）比现测（1721 条 `/api/`）少算 607 行的成因。

## 三面（消费证据的扫描面，口径写死在这里，改口径就是改结论）
| 档 | 目录 | 说明 |
|---|---|---|
| client | `user-web/src`、`user-web/bridge`、`user-web/browser_automation`、`embed-sdk/src`、`packages`、`website` | 业务前端与 SDK：真有人在页面上调 |
| ops | `scripts`、`tests`、`deploy`、`user-server/tests`、`user-web/tests` | 运维脚本与端到端/冒烟用例：调用方是人或 CI，不是终端用户 |
| self-page | `user-server/internal/reach`（只取 `.html` 模板） | 服务端自己生成的落地页：链接由本仓代码写死 |

`.md` 不进任何一面：文档里出现一个路径不证明有人调它（那恰好是本分诊要防的"注解在说谎"）。

## 匹配口径
按**路径形状**匹配，不看方法：注解/前端里写的方法与注册方法可以合法地不一致，
而"这条口有没有人调"问的是路径。参数段（gin 的 `:id`、OpenAPI 的 `{id}`、
前端的 `${id}` / `{{.ID}}` / 真实数字 id）一律折成单段通配 `*`，一段对一段。
弱证据 = 完整形状不命中，但它的某个**祖先前缀**（≥2 段的严格前缀，如 `/api/agent`）
在某一面上出现过——这只能证明"有人在调这一族"，不足以判"这条口有人调"，
所以单独一档、不计入"有消费方"。

## 三面全不命中 ≠ 可以删（本工具最重要的一条输出限制）
静态扫描天然看不见三类消费方，2026-10-11 实测过它们存在：
① 浏览器地址栏直开的页面/分享口；② 渠道与 ESP 回调（邮件像素、webhook、短信状态回执）；
③ 版本不可知的老前端构建（`frontend_aliases.go` 的设计前提就是"线上还装着叫不出名字的老构建"）。
所以本工具只产**候选名册**，删除动作必须另有运行时证据（访问日志）。

## 自检
`--self-check` 用内存里的控制组验提取器与口径本身：五枚正向（quoted / bare /
真实数字 id / Go 模板占位 / 落地页）+ 四枚负向——① 无证据的串必须判入"三面全不命中"，
② 只有真进了扫描面的串才算证据，③ 三面都不吃 `.json`（路由转储当证据＝自证循环），
④ 门的注码夹具（`mut_*`）、取证器自身、探针结果表必须被排除，而冒烟输入清单与落地页模板照常算证据。
任何一枚不成立就退非零：工具的读数先要能被它自己否掉，才允许拿去当别人的输入。
"""

import argparse
import hashlib
import os
import re
import subprocess
import sys
import tempfile
import time

PARAM_SEG_RE = re.compile(r"^(:[\w.]+|\{[^}/]*\}|\$\{[^}/]*\})$")
NUM_SEG_RE = re.compile(r"^\d+$")
GO_TPL_RE = re.compile(r"\{\{[^{}]*\}\}")
# 从任意文本里抽出看起来像路径的串：以 / 开头、字符集不含引号与空白
PATH_TOKEN_RE = re.compile(r"/(?:[A-Za-z0-9_\-{}.:$<>/+*])+")
CLEAN_SEG_RE = re.compile(r"^[A-Za-z0-9_\-.]+$")

SCAN_EXTS = {".js", ".ts", ".vue", ".mjs", ".cjs", ".html", ".htm", ".py", ".sh", ".go",
             ".tsv", ".yml", ".yaml"}
# `.json` 刻意不在任何一面里：JSON 不会"调用"接口，它只会**描述**接口。
# 本仓里就有现成的自证循环陷阱——`user-web/tests/audit/backend_routes.json` 是后端路由表的转储，
# 把它当消费面会让每条路由都被判成"有人调"（1740/1740 全命中），门当场变成橡皮章。
# 同类风险还有 `user-server/docs/swagger.{json,yaml}`（注解面的生成物）。
SKIP_DIRS = {".git", "node_modules", "dist", "build", "vendor", "__pycache__", "logs",
             "pg_data", ".next", "coverage", ".tmp_files"}

# 写了路径字面量、但**不是调用方**的文件——把它们算进消费面＝拿门的夹具或取证器自己的输出给路由作证。
# 用 `--why "POST /api/livecode/:id/click"` 实测出这三族（该路由的精确形状证据原本有四条，其中三条属此类）：
#   1. `scripts/mut_*.py`：常驻反向注码电池。串是被注进被测代码的锚点，例如
#      `mut_route_surface.py:117` 的 `A_PAGE_BEACON = "fetch('/api/livecode/{{.ID}}/click'"`——它是"这条腿该红"的夹具，不是消费者。
#   2. 本取证器自己：文档串与自检夹具里写的示例路径（实测给出一条 `/api/livecode/*` 的前缀证据）。
#   3. `probe_result.tsv`：冒烟探针**跑完写出的结果表**，输入清单已是同目录的 `routes_user.tsv`；
#      再收一遍＝同一证据记两次。注意 `routes_user.tsv` 本身不排除——`probe_endpoints.sh:90` 真按它逐条发请求，是正当消费方。
NON_CONSUMER_NAMES = {"route-consumer-triage.py", "probe_result.tsv"}
NON_CONSUMER_PREFIXES = ("mut_",)


def is_non_consumer_file(name):
    return name in NON_CONSUMER_NAMES or name.startswith(NON_CONSUMER_PREFIXES)

FACES = {
    "client": {
        "dirs": ["user-web/src", "user-web/bridge", "user-web/browser_automation",
                 "embed-sdk/src", "packages", "website"],
        "exts": SCAN_EXTS,
    },
    "ops": {
        "dirs": ["scripts", "tests", "deploy", "user-server/tests", "user-web/tests"],
        "exts": SCAN_EXTS,
    },
    "self-page": {
        "dirs": ["user-server/internal/reach"],
        "exts": {".html", ".htm"},
    },
}


def repo_root():
    # scripts/ 的上一级即仓库根：这里刻意写成"跟着本文件走"而不是硬编码仓名，
    # 否则改名克隆里会退成"零消费方"的假读数
    return os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def seg_wildcard(seg):
    """段是不是一个占位（参数段 / 数字 id / 模板槽）。"""
    if PARAM_SEG_RE.match(seg) or NUM_SEG_RE.match(seg):
        return True
    core = re.sub(r"[{}$:]", "", seg)
    if core == "" or NUM_SEG_RE.match(core):
        return True
    return False


def shape_of(path):
    segs = [s for s in path.split("/") if s]
    return "/" + "/".join("*" if seg_wildcard(s) else s for s in segs)


def extract_tokens(text):
    """把一段源码里的路径串抽出来并归一成形状集合；清洗不掉的段直接弃整个 token（宁漏不误判成有人调）。"""
    shapes = set()
    for m in PATH_TOKEN_RE.finditer(text):
        raw = m.group(0)
        for cut in ("?", "#", "&"):
            raw = raw.split(cut)[0]
        raw = GO_TPL_RE.sub("{}", raw)
        raw = raw.replace("%s", "{}").replace("%d", "{}")
        segs = [s for s in raw.split("/") if s]
        if len(segs) < 2:
            continue
        out = []
        bad = False
        for s in segs:
            if seg_wildcard(s):
                out.append("*")
                continue
            if CLEAN_SEG_RE.match(s):
                out.append(s)
                continue
            bad = True
            break
        if bad:
            continue
        shapes.add("/" + "/".join(out))
    return shapes


def ancestor_prefixes(shape):
    """≥2 段的严格前缀；用于"弱证据"档（这一族被调过，不代表这条口被调过）。"""
    segs = [s for s in shape.split("/") if s]
    return ["/" + "/".join(segs[:n]) for n in range(2, len(segs))]


def scan_face(root, face):
    hits = set()
    files = 0
    occurrences = 0
    src = {}
    for d in face["dirs"]:
        base = os.path.join(root, d)
        if not os.path.isdir(base):
            print(f"警告：面 {face['dirs']} 里的 {d} 不存在，这一条没进扫描", file=sys.stderr)
            continue
        for dirpath, dirnames, filenames in os.walk(base):
            dirnames[:] = [x for x in dirnames if x not in SKIP_DIRS]
            for fn in filenames:
                if os.path.splitext(fn)[1] not in face["exts"]:
                    continue
                if is_non_consumer_file(fn):
                    continue
                p = os.path.join(dirpath, fn)
                try:
                    with open(p, "r", encoding="utf-8", errors="ignore") as f:
                        text = f.read()
                except OSError as e:
                    print(f"警告：读 {p} 失败：{e}", file=sys.stderr)
                    continue
                files += 1
                found = extract_tokens(text)
                if found:
                    occurrences += len(found)
                    hits |= found
                    rel = os.path.relpath(p, root)
                    for shape in found:
                        src.setdefault(shape, set()).add(rel)
    return hits, occurrences, files, src


def load_routes(tsv_path):
    rows = []
    with open(tsv_path, "r", encoding="utf-8") as f:
        header = f.readline().rstrip("\n")
        if header != "METHOD\tPATH\tHANDLER":
            raise SystemExit(f"{tsv_path} 的表头不是 METHOD\\tPATH\\tHANDLER：读到 {header!r}")
        for line in f:
            parts = line.rstrip("\n").split("\t")
            if len(parts) != 3:
                raise SystemExit(f"路由表行形状不对（应为 3 列）：{line!r}")
            rows.append(parts)
    if not rows:
        raise SystemExit(f"{tsv_path} 是空的：空事实源上做出的分档没有意义")
    return rows


def classify(routes, faces):
    buckets = {"client": [], "ops": [], "weak": [], "none": []}
    for method, path, handler in routes:
        sh = shape_of(path)
        if sh in faces["client"]:
            buckets["client"].append((method, path, handler, sh))
        elif sh in faces["ops"] or sh in faces["self-page"]:
            buckets["ops"].append((method, path, handler, sh))
        elif any(p in set().union(*faces.values()) for p in ancestor_prefixes(sh)):
            buckets["weak"].append((method, path, handler, sh))
        else:
            buckets["none"].append((method, path, handler, sh))
    return buckets


def dump_routes_via_go(root, out_path):
    env = dict(os.environ)
    env["ROUTE_DUMP_FILE"] = out_path
    env.setdefault("CGO_ENABLED", "0")
    cmd = ["go", "test", "-count=1", "-timeout", "300s",
           "-run", "^TestDumpLiveRouteTable$", "./internal/router"]
    proc = subprocess.run(cmd, cwd=os.path.join(root, "user-server"), env=env,
                          capture_output=True, text=True)
    if proc.returncode != 0 or not os.path.exists(out_path):
        sys.stderr.write("红因（go test 输出尾部）：\n")
        sys.stderr.write("\n".join(proc.stdout.splitlines()[-15:]) + "\n")
        sys.stderr.write("\n".join(proc.stderr.splitlines()[-15:]) + "\n")
        raise SystemExit("路由表导出失败：缺产物就退非零，不能读成'零消费方'")


def self_check():
    """九枚控制：五枚正向必须命中期望形状，四枚负向必须证明"不该算证据的东西没被算进来"。

    返回 (退码, 控制总数, 失败数)——读数要能被写进取证产物的身份行里，
    只回一个 0/1 就没人知道这轮究竟验了几枚。
    """
    checks = 0
    cases = [
        ("quoted", "http.get('/api/customer/{id}/tags')", "/api/customer/*/tags"),
        ("bare", 'const u = "/api/livecode/42";', "/api/livecode/*"),
        ("real numeric id", 'fetch("/api/order/10086/items?page=2")', "/api/order/*/items"),
        ("go template placeholder", "url: '{{.Base}}/api/sop/state/{{.SID}}'", "/api/sop/state/*"),
        ("landing page", '<img src="/api/track/open?campaign=abc">', "/api/track/open"),
    ]
    failed = 0
    for name, src, expect in cases:
        checks += 1
        got = extract_tokens(src)
        if expect in got:
            print(f"  ✓ {name}：{src} ⇒ {expect}")
        else:
            print(f"  ✗ {name}：{src} ⇒ 期望 {expect}，实得 {sorted(got)}")
            failed += 1
    faces = {"client": set(), "ops": set(), "self-page": set()}
    # 负向之一：一条不存在的路由必须落进 none
    checks += 1
    b = classify([("GET", "/api/zz-not-a-route", "handler.X")], faces)
    if len(b["none"]) == 1 and not any(b[k] for k in ("client", "ops", "weak")):
        print("  ✓ 负向控制之一：无证据的串判入 none，不被读成有消费方")
    else:
        print("  ✗ 负向控制之一：无证据的串没有干净地判入 none")
        failed += 1
    # 负向之二：文档面（.md）不参与判定——只把串写进 faces 才改变档位，写进仓库的 .md 不会
    checks += 1
    b2 = classify([("GET", "/api/zz-not-a-route", "handler.X")],
                  {"client": {"/api/zz-not-a-route"}, "ops": set(), "self-page": set()})
    if len(b2["client"]) == 1:
        print("  ✓ 负向控制之二：只有真的进了扫描面才算证据（文档面不参与）")
    else:
        print("  ✗ 负向控制之二：面证据没被采信，说明匹配腿坏了")
        failed += 1
    # 负向之三（结构闸）：路由表自身的转储不许成为消费面——`.json` 不能出现在任何一面的后缀集合里
    checks += 1
    leaked = {n: sorted(f["exts"] & {".json"}) for n, f in FACES.items() if f["exts"] & {".json"}}
    if leaked:
        print(f"  ✗ 负向控制之三：面 {list(leaked)} 收了 .json，路由转储会自证成'有人调'")
        failed += 1
    else:
        print("  ✓ 负向控制之三：三面都不吃 .json（`backend_routes.json` 这类路由转储不构成证据）")
    # 负向之四：门的夹具与取证器自己的输出不构成消费方——排除表两半都要有牙
    checks += 1
    must_skip = ["mut_route_surface.py", "mut_tg_gate_policy.py", "route-consumer-triage.py",
                 "probe_result.tsv"]
    must_keep = ["routes_user.tsv", "live_code.html", "api_verify_full.py", "probe_endpoints.sh"]
    bad_skip = [n for n in must_skip if not is_non_consumer_file(n)]
    bad_keep = [n for n in must_keep if is_non_consumer_file(n)]
    if bad_skip or bad_keep:
        print(f"  ✗ 负向控制之四：非消费方排除表坏了（该排没排 {bad_skip} · 误伤 {bad_keep}）")
        failed += 1
    else:
        print("  ✓ 负向控制之四：`mut_*` 锚点、取证器自身、探针结果表被排除，"
              "而冒烟输入清单与落地页模板照常算证据")
    print(f"自检：{checks} 枚控制，失败 {failed} 枚")
    return (1 if failed else 0), checks, failed


def explain(why, routes, buckets, sources, faces):
    """打印一条路由的判档依据：它落在哪一档、该档证据来自哪些文件。

    分档读数只能整体看，会掩盖"某一条是被谁算成有消费方"的错——门的锚点串、
    别的路由转储这类**不是调用方**的证据，只有把单条的出处摊开才看得见。
    """
    method, _, path = why.partition(" ")
    path = path.strip()
    if not method or not path:
        print(f"--why 需要 'METHOD PATH' 两段（方法大写），实得 {why!r}")
        return 2
    if not any(m == method.upper() and p == path for m, p, _ in routes):
        print(f"❌ 事实源里没有 {method} {path}：方法要写大写，路径按路由表原样（参数段写 :id）")
        return 2
    shape = shape_of(path)
    print(f"{method} {path}  →  形状 {shape}")
    for name in ("client", "ops", "weak", "none"):
        if any(r[0] == method.upper() and r[1] == path for r in buckets[name]):
            print(f"档位：{name}")
            break
    evidence = sorted(sources.get(shape, ()))
    if evidence:
        print("精确形状证据：")
        for line in evidence:
            print(f"  {line}")
    else:
        print("精确形状证据：无")
    prefix = [p for p in ancestor_prefixes(shape) if any(p in v for v in faces.values())]
    for p in prefix:
        print(f"前缀证据 {p}：")
        for line in sorted(sources.get(p, ())):
            print(f"  {line}")
    return 0


def git_head(root):
    proc = subprocess.run(["git", "-C", root, "rev-parse", "HEAD"],
                          capture_output=True, text=True)
    if proc.returncode != 0:
        return f"UNKNOWN（git -C {root} rev-parse HEAD 退 {proc.returncode}：本树不是 git 仓库）"
    return proc.stdout.strip()


def file_md5(path):
    digest = hashlib.md5()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            digest.update(chunk)
    return digest.hexdigest()


def write_provenance(outdir, root, tsv, buckets, distinct, api_lines,
                     control_total, control_failed):
    """把"这轮读的是哪一笔字节"写进轮次目录，和名册放在一起。

    名册每行都是一句"这条路由没人调"，会被拿去做删除决策；不带字节身份的读数下次复跑对不上账，
    留给后人的就是一份无法证伪的名单。产物轴的归属判据也按这一行认这一族。
    """
    rows = sum(len(v) for v in buckets.values())
    tool = os.path.abspath(__file__)
    lines = [
        f"基线字节：仓库 HEAD {git_head(root)}",
        f"取证器 {os.path.relpath(tool, root)} md5 {file_md5(tool)}",
        f"事实源 {os.path.relpath(os.path.abspath(tsv), root)} md5 {file_md5(tsv)}",
        f"复现命令：python3 {' '.join([os.path.relpath(tool, root)] + sys.argv[1:])}",
        f"读数：路由 {rows} 行 · client {len(buckets['client'])} · ops/self-page {len(buckets['ops'])}"
        f" · weak {len(buckets['weak'])} · none {len(buckets['none'])}"
        f" · distinct (方法,处理器) {len(distinct)} · /api/ {api_lines}",
        f"口径自检：{control_total} 枚控制，失败 {control_failed} 枚",
        f"落盘时间：{time.strftime('%Y-%m-%dT%H:%M:%S%z')}",
    ]
    path = os.path.join(outdir, "00-provenance.log")
    with open(path, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")
    print(f"字节身份行：{path}")


def main():
    ap = argparse.ArgumentParser(description="路由消费面分诊器")
    ap.add_argument("--repo", default=None, help="仓库根（默认取 scripts/ 的上一级）")
    ap.add_argument("--routes", default=None, help="已有的路由表 TSV；不给就现跑导出")
    ap.add_argument("--out", default=None,
                    help="名册输出目录（默认 docs/superpowers/specs/ledger/logs/RouteTriage/<时间戳>）")
    ap.add_argument("--self-check", action="store_true", help="只验提取器，不读仓库")
    ap.add_argument("--why", default=None, metavar="METHOD PATH",
                    help="打印某条路由被判进哪一档、由哪些文件的哪些串供给证据（只出这一条，不写名册）")
    args = ap.parse_args()

    if args.self_check:
        return self_check()[0]

    root = args.repo or repo_root()
    tsv = args.routes
    if not tsv:
        stamp = time.strftime("%Y%m%d-%H%M%S")
        if args.why and not args.out:
            # 单条查询不配在取证树里留一轮目录：事实源导出进临时目录，读完即弃
            outdir = os.path.join(tempfile.mkdtemp(prefix="route-why-"), stamp)
        else:
            outdir = args.out or os.path.join(root, "docs", "superpowers", "specs", "ledger",
                                              "logs", "RouteTriage", stamp)
        os.makedirs(outdir, exist_ok=True)
        tsv = os.path.join(outdir, "live_routes.tsv")
        dump_routes_via_go(root, tsv)
    else:
        outdir = args.out or os.path.dirname(os.path.abspath(tsv))
        os.makedirs(outdir, exist_ok=True)

    routes = load_routes(tsv)
    faces = {}
    sources = {}
    for name, face in FACES.items():
        hits, occ, files, src = scan_face(root, face)
        faces[name] = hits
        for shape, where in src.items():
            sources.setdefault(shape, set()).update(f"{name} <- {w}" for w in where)
        print(f"面 {name}：扫描文件 {files} 份 · 抽出形状 {len(hits)} 个（出现 {occ} 次）")
    if sum(len(v) for v in faces.values()) == 0:
        print("❌ 三面一条证据都没抽到：扫描面或提取器坏了，这份读数不可用")
        return 1

    buckets = classify(routes, faces)
    if args.why:
        return explain(args.why, routes, buckets, sources, faces)
    total = len(routes)
    named = sum(len(v) for v in buckets.values())
    if named != total:
        print(f"❌ 计数器对不上：路由 {total} 行，分档合计 {named} 行 ⇒ 有行没进任何档")
        return 1
    print(f"分档（合计 {named} = 路由表 {total} 行）：")
    print(f"  命中 client             ：{len(buckets['client'])}")
    print(f"  只命中 ops/self-page    ：{len(buckets['ops'])}")
    print(f"  只有弱证据（祖先前缀命中）：{len(buckets['weak'])}")
    print(f"  三面全不命中（候选名册）  ：{len(buckets['none'])}")

    control_rc, control_total, control_failed = self_check()
    if control_rc:
        print("❌ 口径自检未过：提取器或排除表坏了，这份分档不可用，名册不写出")
        return 1

    for name in ("client", "ops", "weak", "none"):
        path = os.path.join(outdir, f"bucket-{name}.tsv")
        with open(path, "w", encoding="utf-8") as f:
            f.write("METHOD\tPATH\tHANDLER\tSHAPE\n")
            for row in sorted(buckets[name]):
                f.write("\t".join(row) + "\n")
    distinct = {(m, h) for m, p, h in routes}
    api_lines = sum(1 for m, p, h in routes if p.startswith("/api/"))
    print(f"另有对账读数：distinct (方法,处理器) {len(distinct)} 个 · "
          f"/api/ 行数 {api_lines}")
    write_provenance(outdir, root, tsv, buckets, distinct, api_lines,
                     control_total, control_failed)
    print(f"名册输出目录：{outdir}")
    print("提醒：三面全不命中只是候选，不是可删——地址栏直开口、渠道/ESP 回调、"
          "版本不可知的老前端构建这三类消费方静态扫不到。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
