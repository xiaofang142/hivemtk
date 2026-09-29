#!/usr/bin/env python3
"""从 user-server Go 源码提取全部路由 (method, fullpath)。
方法：finditer 找所有 .Group 闭包区间(括号配平)建树，再找 .METHOD 落在最深层 group 内累积前缀。
"""
import os, re, json

SRV = "/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-server"
files = []
for root, _, fs in os.walk(os.path.join(SRV, "internal/router")):
    for fn in fs:
        if fn.endswith(".go") and not fn.endswith("_test.go"):
            files.append(os.path.join(root, fn))

METHODS = ["GET", "POST", "PUT", "DELETE", "PATCH"]
routes = set()

def norm(prefix, p):
    if not p.startswith("/"):
        p = "/" + p
    return (prefix.rstrip("/") + p) if prefix else p

def find_groups(text):
    """返回 list of (body_start, body_end, prefix)，并处理嵌套（按包含关系递归）。"""
    groups = []
    for m in re.finditer(r'\.Group\(\s*"([^"]*)"', text):
        j = m.end()
        fm = re.search(r'func\([^)]*\)\s*\{', text[j:])
        if not fm:
            continue
        body_start = j + fm.end()
        depth = 0
        k = body_start
        while k < len(text):
            if text[k] == '{': depth += 1
            elif text[k] == '}':
                depth -= 1
                if depth == 0:
                    break
            k += 1
        body_end = k  # 指向闭合 }
        groups.append([body_start, body_end, m.group(1)])
    return groups

def method_at(text, pos):
    mm = re.match(r'\.(' + '|'.join(METHODS) + r')\(\s*"([^"]*)"', text[pos:])
    return mm

for f in files:
    text = open(f, encoding="utf-8").read()
    groups = find_groups(text)
    # 构建嵌套：按 (start,end) 包含排序，parent 是包含它且最紧的
    groups_sorted = sorted(groups, key=lambda g: g[0])
    # 为每个 group 找父前缀
    def parent_prefix(idx):
        s, e, pref = groups[idx]
        # 找包含它的最内层父
        best = None
        for j, (ps, pe, pp) in enumerate(groups):
            if j == idx: continue
            if ps <= s and e <= pe:
                if best is None or (ps >= groups[best][0]):
                    best = j
        if best is None:
            return ""
        return norm(parent_prefix(best), groups[best][2])
    # 提取路由：遍历所有 .METHOD，判断所属最深层 group
    for mm in re.finditer(r'\.(' + '|'.join(METHODS) + r')\(\s*"([^"]*)"', text):
        pos = mm.start()
        # 找包含 pos 的最深层 group
        best = None
        for j, (gs, ge, gp) in enumerate(groups):
            if gs <= pos < ge:
                if best is None or (gs >= groups[best][0]):
                    best = j
        prefix = parent_prefix(best) if best is not None else ""
        if best is not None:
            prefix = norm(prefix, groups[best][2])
        full = norm(prefix, mm.group(2))
        routes.add((mm.group(1), full))

out = [{"method": m, "path": p} for m, p in sorted(routes)]
json.dump(out, open(os.path.join(os.path.dirname(__file__), "backend_routes.json"), "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print(f"后端路由总数 {len(out)}")
for r in out[:8]:
    print(r)
# 统计空前缀（无 /api）
naked = [r for r in out if not r["path"].startswith("/api")]
print(f"非 /api 前缀路由: {len(naked)}")
for r in naked[:10]:
    print("  ", r)
