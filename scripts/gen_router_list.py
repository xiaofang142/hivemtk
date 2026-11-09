#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""gen_router_list.py — 由 user-web/src/router/* 生成 router-list.md 索引（任务四 第一步）。

只做「索引生成」：把前端页面路由逐条列成清单，供后续浏览器逐页点击验证使用。
本脚本**不开浏览器**、不发请求、不改代码。

抽取口径：
  - src/router/index.js 里的 initRoutes（固定路由：setup / login / 帮助中心 / 聊天挂件）
  - src/router/modules/*.js 里的模块路由（业务页面）
  - 记录 path / name / 组件文件 / 是否需登录 / 标题
"""
import os
import re
from collections import OrderedDict

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ROUTER_DIR = os.path.join(ROOT, "user-web", "src", "router")
MOD_DIR = os.path.join(ROUTER_DIR, "modules")
OUT = os.path.join(ROOT, "router-list.md")

# 路由对象字段之间常夹注释行（`// ...`），先剥掉再匹配，否则 path/name 之间的
# 注释会把正则打断、整条路由静默漏掉。
COMMENT_RE = re.compile(r"^\s*//.*$", re.M)

# 一个路由对象：{ path: '...', name: '...', component: () => import('@/views/X.vue'), meta: {...} }
ROUTE_RE = re.compile(
    r"\{\s*path:\s*'([^']*)'\s*,\s*name:\s*'([^']*)'\s*,\s*"
    r"component:\s*\(\)\s*=>\s*import\('([^']*)'\)"
    r"(?:\s*,\s*meta:\s*\{([^}]*)\})?",
    re.S,
)
# 重定向路由：{ path: '...', name: '...', redirect: '...', meta: {...} }
# 与页面路由一样可能跨行，且 name/meta 顺序不固定。
REDIRECT_RE = re.compile(
    r"path:\s*'([^']*)'\s*,\s*name:\s*'([^']*)'\s*,\s*redirect:\s*'([^']*)'"
    r"(?:\s*,\s*meta:\s*\{([^}]*)\})?",
    re.S,
)
# 具名 layout 根路由：{ path: '/', name: 'Layout', component: Layout, redirect: '...' }
LAYOUT_RE = re.compile(
    r"path:\s*'([^']*)'\s*,\s*name:\s*'([^']*)'\s*,\s*component:\s*Layout"
    r"(?:\s*,\s*redirect:\s*'([^']*)')?",
    re.S,
)
# 匿名重定向：{ path: '...', redirect: '...' }（无 name）
ANON_REDIRECT_RE = re.compile(
    r"path:\s*'([^']*)'\s*,\s*redirect:\s*'([^']*)'", re.S
)
TITLE_RE = re.compile(r"title:\s*'([^']*)'")
AUTH_RE = re.compile(r"requiresAuth:\s*(true|false)")
# 父路由的 children 段：{ path: '/browser-automation', ..., children: [ ... ] }
# 子路由的 path 是**相对**的（'tasks'），实际路径 = 父 path + '/' + 子 path。
# 不解析就会把 'tasks' 当成根级路径记进清单 ⇒ 访问时 404「页面不存在」。
CHILDREN_RE = re.compile(
    r"path:\s*'(/[^']*)'\s*,\s*name:\s*'[^']*'\s*,\s*"
    r"(?:component:\s*\(\)\s*=>\s*import\('([^']*)'\)\s*,\s*)?"
    r"(?:redirect:\s*'([^']*)'\s*,\s*)?"
    r"meta:\s*\{([^}]*)\}\s*,\s*children:\s*\[",
    re.S,
)


def parse_routes(src):
    out = []
    seen = set()
    # 剥注释：字段之间夹的 `// ...` 会打断 path/name 的相邻匹配
    clean = COMMENT_RE.sub("", src)
    # 子路由（父路由带 children）：把相对 path 拼成绝对路径再记，
    # 否则清单里留下的是 'tasks' 这种相对片段，访问时必然 404。
    #
    # 先扫子路由：父路由的 children 段里的 path 是**相对**的（'tasks'），
    # ROUTE_RE 那轮会把它们当成根级路径记下来，于是清单里同时出现
    # 'tasks' 和 '/browser-automation/tasks' 两条 —— 前者根本不是一条真实路由
    # （按它访问必然落到 NotFound）。所以这里先收集相对子路径，
    # 主循环里遇到这些 path 时直接跳过。
    child_rel = set()
    for m in CHILDREN_RE.finditer(clean):
        parent, parent_comp, parent_redirect, parent_meta = m.groups()
        start = m.end()
        depth = 1
        i = start
        while i < len(clean) and depth > 0:
            if clean[i] == "[":
                depth += 1
            elif clean[i] == "]":
                depth -= 1
            i += 1
        block = clean[start : i - 1]
        for cm in ROUTE_RE.finditer(block):
            child_rel.add(cm.group(1))
    for m in ROUTE_RE.finditer(clean):
        path, name, comp, meta = m.groups()
        # 相对子路径不是独立路由：真实路径由父路由拼出（下面 children 那轮处理）
        if path in child_rel:
            continue
        title = ""
        auth = ""
        if meta:
            t = TITLE_RE.search(meta)
            a = AUTH_RE.search(meta)
            title = t.group(1) if t else ""
            auth = a.group(1) if a else ""
        seen.add(path)
        out.append(
            OrderedDict(
                path=path, name=name, comp=comp.replace("@/", ""),
                title=title, auth=auth, kind="页面",
            )
        )
    for m in REDIRECT_RE.finditer(clean):
        path, name, target, meta = m.groups()
        if path in seen:
            continue
        seen.add(path)
        title = ""
        auth = ""
        if meta:
            t = TITLE_RE.search(meta)
            a = AUTH_RE.search(meta)
            title = t.group(1) if t else ""
            auth = a.group(1) if a else ""
        out.append(
            OrderedDict(
                path=path, name=name, comp="-", title=title, auth=auth,
                kind="重定向→" + target,
            )
        )
    for m in LAYOUT_RE.finditer(clean):
        path, name, target = m.groups()
        if path in seen:
            continue
        seen.add(path)
        out.append(
            OrderedDict(
                path=path, name=name, comp="layout/Layout.vue", title="", auth="",
                kind="重定向→" + (target or "-"),
            )
        )
    for m in ANON_REDIRECT_RE.finditer(clean):
        path, target = m.groups()
        if path in seen:
            continue
        seen.add(path)
        out.append(
            OrderedDict(
                path=path, name="-", comp="-", title="", auth="",
                kind="重定向→" + target,
            )
        )
    # 子路由（父路由带 children）：把相对 path 拼成绝对路径再记，
    # 否则清单里留下的是 'tasks' 这种相对片段，访问时必然 404。
    for m in CHILDREN_RE.finditer(clean):
        parent, parent_comp, parent_redirect, parent_meta = m.groups()
        start = m.end()
        depth = 1
        i = start
        while i < len(clean) and depth > 0:
            if clean[i] == "[":
                depth += 1
            elif clean[i] == "]":
                depth -= 1
            i += 1
        block = clean[start : i - 1]
        for cm in ROUTE_RE.finditer(block):
            cpath, cname, ccomp, cmeta = cm.groups()
            title = ""
            auth = ""
            if cmeta:
                t = TITLE_RE.search(cmeta)
                a = AUTH_RE.search(cmeta)
                title = t.group(1) if t else ""
                auth = a.group(1) if a else ""
            full = parent.rstrip("/") + "/" + cpath.lstrip("/")
            if full in seen:
                continue
            seen.add(full)
            out.append(
                OrderedDict(
                    path=full, name=cname, comp=ccomp.replace("@/", ""),
                    title=title, auth=auth, kind="页面",
                )
            )
    return out


def load_existing_status():
    """读取既有 router-list.md 的勾选状态，按 path 记住。

    重新生成清单**不能**把已验证的路由打回未验证——那样每跑一次脚本
    就等于把全部人工点击结论清零。键只用 path：路由改名（如
    FaqList→FAQList）不应把已点验状态清零；同一 path 在路由表里是唯一的，
    name 只是展示名。
    """
    status = {}
    if not os.path.exists(OUT):
        return status
    with open(OUT, encoding="utf-8") as fh:
        for line in fh:
            if not line.startswith("|"):
                continue
            cells = [c.strip() for c in line.strip().strip("|").split("|")]
            if len(cells) < 5:
                continue
            idx_cell = cells[0]
            if not idx_cell.isdigit():
                continue
            st = cells[1]
            # 用 path 做键：路由改名（如 FaqList→FAQList）不应把已点验状态清零。
            path = cells[2].strip("`")
            status[path] = st
    return status


def load_existing_guide():
    """读取既有 router-list.md 的「引导」列状态，按 path 记住。

    任务六：这一列记的是「这一页的新手导航写没写」。跟点验状态一样，
    重跑脚本不能把已经写好的导览打回未写——否则每次重新生成清单就等于
    把全部人工结论清零。键与 load_existing_status 一致，用稳定的 path。
    """
    guide = {}
    if not os.path.exists(OUT):
        return guide
    with open(OUT, encoding="utf-8") as fh:
        for line in fh:
            if not line.startswith("|"):
                continue
            cells = [c.strip() for c in line.strip().strip("|").split("|")]
            if len(cells) < 9:
                continue
            idx_cell = cells[0]
            if not idx_cell.isdigit():
                continue
            # 同 status：path 是稳定键，name 可能随命名规范调整。
            path = cells[2].strip("`")
            guide[path] = cells[8]
    return guide


def main():
    rows = []

    # 固定路由
    with open(os.path.join(ROUTER_DIR, "index.js"), encoding="utf-8") as fh:
        idx = fh.read()
    for r in parse_routes(idx):
        r["src"] = "src/router/index.js"
        rows.append(r)

    # 模块路由
    mod_files = sorted(f for f in os.listdir(MOD_DIR) if f.endswith(".js"))
    n_mod = len(mod_files)
    for fname in mod_files:
        with open(os.path.join(MOD_DIR, fname), encoding="utf-8") as fh:
            src = fh.read()
        for r in parse_routes(src):
            r["src"] = f"src/router/modules/{fname}"
            rows.append(r)

    lines = []
    lines.append("# 前端页面路由清单（任务四逐页点验 + 任务六新手引导文案）")
    lines.append("")
    lines.append(f"- 来源：`user-web/src/router/index.js` + `src/router/modules/*.js`（{n_mod} 个模块文件）")
    lines.append(f"- 规模：**{len(rows)} 条页面路由**")
    lines.append("- 生成方式：`scripts/gen_router_list.py`（只做索引，不开浏览器；重跑保留既有勾选状态）")
    lines.append("- 校验口径：逐条路由**拟人点击**页面元素/表单/交互（每个元素显式指定，禁止枚举循环），")
    lines.append("  比对**浏览器控制台报错**与 **Network 里的非 2xx API 响应**；发现错误即本地修复并回归")
    lines.append("")
    lines.append("## 测试环境（2026-11-06 全量复测轮）")
    lines.append("")
    lines.append("| 形态 | 访问入口 | 后端 | 账号 | 标识 |")
    lines.append("|---|---|---|---|---|")
    lines.append("| SaaS 云端（线上） | `https://work.xapptool.cn/` | 线上租户 18（tz3rmdj） | `jideilvluoqun@gmail.com`（口令不入库） | `tenant_mode=true` / `edition=saas` |")
    lines.append("| SaaS 本地 | `127.0.0.1:8321` | `127.0.0.1:8315`（`TENANT_MODE=on`） | `uit_admin`（tenant_id=1；口令不入库） | `tenant_mode=true` / `edition=saas` |")
    lines.append("| 私域独立部署 | `127.0.0.1:8320` | `127.0.0.1:8314`（`TENANT_MODE=off`） | `uit_admin`（口令不入库） | `tenant_mode=false` / `edition=opensource` |")
    lines.append("")
    lines.append("- 浏览器：ego lite（`ego-browser`，复用登录态；控制台 + Network 全量采集）")
    lines.append("- 两种形态**都要过**：SaaS 页出现的租户/商务属性，独立部署页必须不出现且不报错")
    lines.append("")
    lines.append("## 状态图例")
    lines.append("")
    lines.append("| 标记 | 含义 |")
    lines.append("|---|---|")
    lines.append("| `[x]` | 已逐页打开并点击验证：控制台无报错、API 无非 2xx |")
    lines.append("| `[~]` | 已开页，响应属设计内拒绝（如参数必填校验、无权限），非缺陷 |")
    lines.append("| `[!]` | 已点出缺陷，待修复 |")
    lines.append("| `[ ]` | 未测 |")
    lines.append("")
    lines.append("## 任务六 · 新手引导（页面导览）状态图例")
    lines.append("")
    lines.append("「引导」列记的是**这一页的新手引导文案与步骤**写没写、写没写对：")
    lines.append("读懂该页的功能/业务/使用/配置逻辑 → 写这一页的定位说明与关键操作说明 →")
    lines.append("把说明挂到该页**真实存在**的 DOM 元素上 → 浏览器里点开确认高亮正确、× 能关掉。")
    lines.append("")
    lines.append("| 标记 | 含义 |")
    lines.append("|---|---|")
    lines.append("| `G` | 已写导览：文案讲的是这一页的真实功能/操作/配置，高亮元素在浏览器里点过，关闭按钮生效 |")
    lines.append("| `-` | 本页不需要导览（公共页/重定向/无业务语义），或尚未开写 |")
    lines.append("")
    lines.append("## 清单")
    lines.append("")
    lines.append("| # | 状态 | 路径 | 名称 | 标题 | 需登录 | 组件 | 源文件 | 引导 |")
    lines.append("|---|---|---|---|---|---|---|---|---|")
    existing = load_existing_status()
    existing_guide = load_existing_guide()
    for i, r in enumerate(rows, 1):
        auth = "是" if r["auth"] == "true" else ("否" if r["auth"] == "false" else "-")
        st = existing.get(r["path"], "[ ]")
        guide = existing_guide.get(r["path"], "-")
        lines.append(
            f"| {i} | {st} | `{r['path']}` | {r['name']} | {r['title']} | {auth} | "
            f"`{r['comp']}` | `{r['src']}` | {guide} |"
        )
    lines.append("")

    done = sum(1 for line in lines if "| [x] |" in line)
    partial = sum(1 for line in lines if "| [~] |" in line)
    todo = sum(1 for line in lines if "| [ ] |" in line)
    bug = sum(1 for line in lines if "| [!] |" in line)
    guide_done = sum(1 for line in lines if line.rstrip().endswith("| G |"))
    lines.append("## 进度")
    lines.append("")
    lines.append(f"- 已验证无错误 `[x]`：**{done}** 条")
    lines.append(f"- 已验证·设计内拒绝 `[~]`：**{partial}** 条")
    lines.append(f"- 待点验 `[ ]`：**{todo}** 条")
    lines.append(f"- 已点出缺陷 `[!]`：**{bug}** 条")
    lines.append(f"- 任务六已写页面导览 `G`：**{guide_done}** 条")
    lines.append("")

    with open(OUT, "w", encoding="utf-8") as fh:
        fh.write("\n".join(lines))
    print(
        f"写入 {OUT}：{len(rows)} 条页面路由 "
        f"(保留状态 x={done} ~={partial} 空={todo} !={bug})"
    )


if __name__ == "__main__":
    main()
