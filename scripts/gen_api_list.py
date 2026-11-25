#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""gen_api_list.py — 由 user-web/src/api/* 生成 api-list.md 索引（任务三 第一步）。

只做「索引生成」：把前端 API 出口逐条抽成清单，供后续逐路由 curl 验证使用。
本脚本**不做任何请求**，也不批量改代码。

抽取口径：
  - 扫描 user-web/src/api/*.js 的 `http.<method>(<url>[, ...])` 调用
  - 解析同一文件内的 `const base = '...'` 常量并做替换
  - 模板串 `${...}` 段一律归一为 `{param}` 占位符（保留参数名）
  - 记录调用点行号、导出名、http 方法、URL 形态、参数来源
"""
import os
import re
import sys
from collections import OrderedDict

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from path_guard import safe_resolve  # noqa: E402  路径闸：输出文件不得越出仓库

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
API_DIR = os.path.join(ROOT, "user-web", "src", "api")
OUT = os.path.join(ROOT, "docs", "verification", "api-list.md")

# http.<method>( "url"  或  `url`  或  ${base}/x  或  base（裸标识符）
CALL_RE = re.compile(
    r"http\.(get|post|put|patch|delete|upload)\s*\(\s*(?:(['\"`])([^'\"`]*)\2|([A-Za-z_$][\w$]*))",
    re.M,
)
# 本仓 api 层有 postForm('/api/batch/export', {...}) 这类薄封装：真正的 URL 在封装
# 实参里，http.post 那一行拿到的是形参 `url`（解析不出地址）。单独扫一层，
# 否则清单里会出现一条路径为空的假行。
POST_FORM_RE = re.compile(r"postForm\(\s*(['\"`])([^'\"`]+)\1", re.M)
BASE_CONST_RE = re.compile(r"^\s*(?:export\s+)?const\s+(\w+)\s*=\s*['\"]([^'\"]+)['\"]", re.M)

# 反引号里的 ${expr} → {expr}（expr 里的 . 换成 _，去掉 encodeURIComponent 包裹）
def norm_template(u):
    def rep(m):
        expr = m.group(1)
        expr = re.sub(r"encodeURIComponent\(([^)]*)\)", r"\1", expr)
        expr = re.sub(r"\bseg\(([^)]*)\)", r"\1", expr)   # seg(id) → id
        expr = expr.replace(".", "_")
        expr = expr.replace(" ", "")
        return "{" + expr + "}"
    return re.sub(r"\$\{([^}]*)\}", rep, u)


FUNC_RE = re.compile(r"\n  (?:async\s+)?(\w+)\s*\(([^)]*)\)\s*\{")


def parse_call_args(src, open_paren):
    """返回 http.xxx(...) 的实参原文列表（按顶层逗号切分）。"""
    depth = 0
    quote = None
    cur = []
    out = []
    i = open_paren
    while i < len(src):
        ch = src[i]
        if quote:
            cur.append(ch)
            if ch == quote and src[i - 1] != "\\":
                quote = None
        elif ch in "'\"`":
            quote = ch
            cur.append(ch)
        elif ch in "([{":
            depth += 1
            if depth > 1:
                cur.append(ch)
        elif ch in ")]}":
            depth -= 1
            if depth == 0:
                tail = "".join(cur).strip()
                if tail:
                    out.append(tail)
                return out
            cur.append(ch)
        elif ch == "," and depth == 1:
            out.append("".join(cur).strip())
            cur = []
        else:
            cur.append(ch)
        i += 1
    return out


def inline_keys(expr):
    """从 `{ a, b: 1, params: { c } }` 里抽出一层键名。"""
    m = re.match(r"^\{\s*([^{}]*)\}\s*$", expr.strip())
    if not m:
        return None
    keys = []
    for part in m.group(1).split(","):
        part = part.strip()
        if not part:
            continue
        key = part.split(":", 1)[0].strip()
        if re.fullmatch(r"[A-Za-z_$][\w$]*", key):
            keys.append(key)
    return keys


def describe_params(method, url, args, sig):
    """把实参归类成 path/query/body 三段，供 curl 构造。"""
    path = re.findall(r"\{([^{}]+)\}", url)
    query = []
    body = []
    rest = args[1:]
    config = None
    for idx, a in enumerate(rest):
        if a.strip().startswith("{") and "params" in a:
            config = a
            break
    for idx, a in enumerate(rest):
        if idx == 0 and a.strip().startswith("{") and "params" in a:
            # get/delete 的 { params: {...} } / { params }
            m = re.search(r"params\s*:\s*(\{[^{}]*\}|[A-Za-z_$][\w$]*)", a)
            if m:
                node = m.group(1)
                if node.startswith("{"):
                    query.extend(inline_keys(node) or [])
                else:
                    query.append(node)
            elif re.search(r"\bparams\b", a):
                query.append("params")
            continue
        if a.strip() == "null":
            continue
        if a.strip().startswith("{") and ("headers" in a or "responseType" in a or "paramsSerializer" in a or "_silent" in a):
            continue
        if method in ("GET", "DELETE"):
            if a.strip() == "params":
                query.append("params")
            else:
                body.append(a)
        elif method == "POST" and "params" in (config or "") and a.strip().startswith("{"):
            body.append(a)
        else:
            body.append(a)
    if config:
        m = re.search(r"params\s*:\s*(\{[^{}]*\}|[A-Za-z_$][\w$]*)", config)
        if m:
            node = m.group(1)
            if node.startswith("{"):
                query.extend(k for k in (inline_keys(node) or []) if k not in query)
            elif node not in query:
                query.append(node)
        elif re.search(r"\bparams\b", config):
            if "params" not in query:
                query.append("params")
    parts = []
    if path:
        parts.append("path: " + ", ".join(path))
    if query:
        parts.append("query: " + ", ".join(query))
    if body:
        parts.append("body: " + "; ".join(body))
    if not parts:
        return "-"
    # 单元格必须是单行：前端实参里带换行/竖线（多字段对象）会把 markdown 表格拆散。
    out = " ".join(" / ".join(parts).split())
    return out.replace("|", "\\|")


def classify_auth(path):
    """按 URL 前缀判档位（前端出口档位，后端实际鉴权以路由装配为准）。"""
    pub = (
        "/api/auth/login", "/api/auth/mfa", "/api/auth/refresh-token",
        "/api/module-market/", "/api/tenant/regulatory-notice",
        "/api/chat/public", "/api/health", "/healthz",
    )
    if any(path.startswith(p) for p in pub):
        return "公开"
    if path.startswith("/api/manage/") or path.startswith("/api/platform/"):
        return "管理端"
    return "用户端"


def load_prior_status():
    """读既有 api-list.md 的勾选状态，建索引。

    为什么要有这一步：清单是「逐条 curl 验证」的进度台账，重新生成时若把状态
    一律写成 [ ]，几百条已验证的结论会被静默清零——而且没人会发现（表还在，
    只是全是未勾选）。所以生成器必须**认得**自己上一版的勾选。

    索引键用（方法, 路径, 源文件）——**不含行号**：行号会随前端文件改动整体
    平移，拿它当键等于「改一行前端就丢一批勾选」。同一个文件里同一对
    （方法, 路径）重复出现时（确有这种：chatChannel 的 create/get 各一次等），
    再按出现次序配对（同一键的第 k 次出现 ↔ 上一版的第 k 次出现），
    这样重复项也不会互相错位。
    """
    if not os.path.exists(OUT):
        return {}
    pat = re.compile(
        r"^\|\s*\d+\s*\|\s*\[([ xX~!-])\]\s*\|\s*(\w+)\s*\|\s*`([^`]*)`\s*\|(?:\\.|[^|])*\|(?:\\.|[^|])*\|(?:\\.|[^|])*\|\s*"
        r"`src/api/([^`]*)`\s*\|\s*(\d+)\s*\|"
    )
    out = {}
    seq = {}
    with open(OUT, encoding="utf-8") as fh:
        for ln in fh:
            m = pat.match(ln)
            if not m:
                continue
            mark, method, url, fname, line = m.groups()
            key = (method.upper(), url, fname)
            k = seq.get(key, 0)
            seq[key] = k + 1
            out[(key, k)] = mark
    return out


def load_prior_responses():
    """读既有 api-list.md 的「响应」列（键口径与状态列一致）。

    响应列是逐条 curl 的取证结论，重新生成索引时不能丢——否则重跑一次生成器
    就要重打几百条路由。空列（`-`）表示尚未验证。
    """
    if not os.path.exists(OUT):
        return {}
    pat = re.compile(
        r"^\|\s*\d+\s*\|\s*\[[ xX~!-]\]\s*\|\s*(\w+)\s*\|\s*`([^`]*)`\s*\|(?:\\.|[^|])*\|(?:\\.|[^|])*\|\s*((?:\\.|[^|])*?)\s*\|\s*"
        r"`src/api/([^`]*)`\s*\|\s*(\d+)\s*\|"
    )
    out = {}
    seq = {}
    with open(OUT, encoding="utf-8") as fh:
        for ln in fh:
            m = pat.match(ln)
            if not m:
                continue
            method, url, resp, fname, line = m.groups()
            key = (method.upper(), url, fname)
            k = seq.get(key, 0)
            seq[key] = k + 1
            if resp and resp != "-":
                out[(key, k)] = resp
    return out


def main():
    # --reset：显式清零旧勾选（本轮要求全量重验时使用）；默认沿用既有结论。
    reset = "--reset" in sys.argv
    prior_status = {} if reset else load_prior_status()
    prior_resp = {} if reset else load_prior_responses()
    rows = []
    files = sorted(f for f in os.listdir(API_DIR) if f.endswith(".js"))
    for fname in files:
        path = os.path.join(API_DIR, fname)
        with open(path, encoding="utf-8") as fh:
            src = fh.read()
        consts = {m.group(1): m.group(2) for m in BASE_CONST_RE.finditer(src)}
        for m in CALL_RE.finditer(src):
            method = m.group(1).upper()
            raw = m.group(3)
            if raw is None:
                # 裸标识符（http.get(base, params)）→ 取同文件常量
                raw = consts.get(m.group(4), "")
                if not raw:
                    # 形参/运行时变量：解析不出地址，不能生成一条路径为空的假行
                    continue
            line = src[: m.start()].count("\n") + 1
            url = raw
            for k, v in consts.items():
                if k in url:
                    url = url.replace("${" + k + "}", v).replace(k, v)
            url = norm_template(url)
            paren = src.find("(", m.start())
            args = parse_call_args(src, paren) if paren >= 0 else []
            fn = None
            for fm in FUNC_RE.finditer(src, 0, m.start()):
                fn = fm
            sig = fn.group(2).strip() if fn else ""
            rows.append(
                OrderedDict(
                    file=fname, line=line, method=method, url=url,
                    params=describe_params(method, url, args, sig),
                    fn=fn.group(1) if fn else "-",
                    auth=classify_auth(url),
                )
            )
        # postForm(...) 薄封装：补上真实 URL（form-urlencoded 的实操口径）
        for m in POST_FORM_RE.finditer(src):
            url = norm_template(m.group(2))
            if not url.startswith("/"):
                continue
            line = src[: m.start()].count("\n") + 1
            rows.append(
                OrderedDict(
                    file=fname, line=line, method="POST", url=url,
                    params="body: form-urlencoded",
                    fn="postForm", auth=classify_auth(url),
                )
            )
        # 同一文件内按行号还原调用顺序（postForm 行是后补的）
        file_rows = [r for r in rows if r["file"] == fname]
        other = [r for r in rows if r["file"] != fname]
        file_rows.sort(key=lambda r: r["line"])
        rows = other + file_rows

    # 输出
    by_method = {}
    for r in rows:
        by_method.setdefault(r["method"], []).append(r)

    lines = []
    lines.append("# API 路由清单（SaaS 多租户 · 私域独立部署 双模式逐路由 curl 验证）")
    lines.append("")
    lines.append(f"- 来源：`user-web/src/api/*.js`（{len(files)} 个文件）")
    lines.append(f"- 规模：**{len(rows)} 条前端 API 出口**")
    lines.append("- 生成方式：`scripts/gen_api_list.py`（只做索引，不发请求）")
    lines.append("- 参数列：`path` 路径占位符 / `query` 查询参数 / `body` 请求体（从前端调用点实参归类）")
    lines.append("- 响应列：逐条 curl 实测后回填（HTTP 状态 + 业务 `code` + 关键字段），未验为 `-`")
    lines.append("- 校验口径：每条路由**逐条手工 curl**，分析 `code` 与 `data`；")
    lines.append("  `code=0` 通过；`code!=0` 判定是调用错误还是业务错误，即时修复")
    lines.append("- **本轮为全量重测**：旧轮勾选不再沿用（历史见 git），所有路由从 `[ ]` 重新验证")
    lines.append("")
    lines.append("## 状态标记")
    lines.append("")
    lines.append("| 标记 | 含义 |")
    lines.append("|---|---|")
    lines.append("| `[ ]` | 未验证 |")
    lines.append("| `[x]` | 已验证通过（HTTP 200 且 `code=0`，或已确认是**正确**的业务错误响应） |")
    lines.append("| `[~]` | 已验证但**存在待修问题**（见任务三结论与本轮修复记录） |")
    lines.append("")
    lines.append("> 本次生成显式清零旧勾选：上一轮结论对应的是旧代码快照，")
    lines.append("> 本轮要求每条路由重新 curl 取证后才置 `[x]`。")
    lines.append("## 档位说明")
    lines.append("")
    lines.append("| 档位 | 含义 |")
    lines.append("|---|---|")
    lines.append("| 公开 | 无需登录（登录、模块市场、公开聊天） |")
    lines.append("| 用户端 | 需登录的普通业务接口 `/api/*` |")
    lines.append("| 管理端 | 管理面接口 `/api/manage/*`、`/api/platform/*` |")
    lines.append("")
    lines.append("## 清单")
    lines.append("")
    lines.append("| # | 状态 | 方法 | 路径 | 档位 | 参数 | 响应 | 源文件 | 行 |")
    lines.append("|---|---|---|---|---|---|---|---|---|")
    seq = {}
    for i, r in enumerate(rows, 1):
        # 状态沿用既有清单里同一条路由的标记（键 = 方法+路径+源文件，同键按出现次序配对）：
        # 重新生成本文件**不得**把已逐条 curl 验证过的 [x] 抹回 [ ] ——
        # 那会把上一轮几百条路由的验证结论静默清零。
        key = (r["method"], r["url"], r["file"])
        k = seq.get(key, 0)
        seq[key] = k + 1
        mark = prior_status.get((key, k), " ")
        resp = prior_resp.get((key, k), "-")
        lines.append(
            f"| {i} | [{mark}] | {r['method']} | `{r['url']}` | {r['auth']} | "
            f"{r['params']} | {resp} | `src/api/{r['file']}` | {r['line']} |"
        )
    lines.append("")

    # 输出路径过闸：realpath+commonpath 必须留在仓库根内。
    with open(safe_resolve(OUT, ROOT), "w", encoding="utf-8") as fh:
        fh.write("\n".join(lines))
    print(f"写入 {OUT}：{len(rows)} 条（{len(files)} 个文件）")
    for m in sorted(by_method):
        print(f"  {m}: {len(by_method[m])}")


if __name__ == "__main__":
    main()
