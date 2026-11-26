#!/usr/bin/env python3
"""提取 user-web 全部页面清单：路由模块 -> (path, name, component, title)。"""
import os, re, json, glob
from pathlib import Path

WEB = Path(__file__).resolve().parents[2] / "src"
MOD_DIR = os.path.join(WEB, "router/modules")

def to_fs(path):
    # @/views/... -> user-web/src/views/...
    if path.startswith("@/"):
        return os.path.join(WEB, path[2:])
    return path

pages = []
for f in sorted(glob.glob(os.path.join(MOD_DIR, "*.js"))):
    mod = os.path.basename(f)[:-3]
    src = open(f, encoding="utf-8").read()
    # 匹配 component: () => import('@/views/xxx.vue')
    for m in re.finditer(r"path:\s*'([^']+)'.*?name:\s*'([^']+)'.*?component:\s*\(\)\s*=>\s*import\('([^']+)'\)", src, re.S):
        path, name, comp = m.group(1), m.group(2), m.group(3)
        # title
        tm = re.search(r"name:\s*'" + re.escape(name) + r"'.*?title:\s*'([^']+)'", src, re.S)
        title = tm.group(1) if tm else ""
        pages.append({
            "module": mod,
            "path": path,
            "name": name,
            "component": comp,
            "component_fs": to_fs(comp),
            "title": title,
        })

# 去重（同 path+name）
seen = set()
uniq = []
for p in pages:
    key = (p["path"], p["name"])
    if key in seen: continue
    seen.add(key); uniq.append(p)

# 输出按模块分组
out = {}
for p in uniq:
    out.setdefault(p["module"], []).append(p)

with open(os.path.join(os.path.dirname(__file__), "pages.json"), "w", encoding="utf-8") as fo:
    json.dump(uniq, fo, ensure_ascii=False, indent=2)

# 汇总
print(f"总页面数: {len(uniq)}")
print(f"涉及模块数: {len(out)}")
for mod in sorted(out):
    print(f"\n=== {mod} ({len(out[mod])}) ===")
    for p in out[mod]:
        print(f"  /{p['path']:40s} <- {os.path.basename(p['component_fs']):30s} [{p['title']}]")
