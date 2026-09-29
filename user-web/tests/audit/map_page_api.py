#!/usr/bin/env python3
"""构建 页面(vue组件) -> 调用的 API 端点 映射。

分析:
1. api/*.js 中 http.get/post/put/delete('url') -> (method, urltpl)
2. views/*.vue 中 import 的 api 模块 + 调用的方法名 -> 端点
输出 pages_api.json: [{page, component, module, apis:[{method,url,fn,api_file}]}]
"""
import os, re, json, glob

WEB = "/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web/src"
API_DIR = os.path.join(WEB, "api")
VIEW_DIR = os.path.join(WEB, "views")

# 1) 解析 api/*.js -> {file: {fn: (method, urltpl)}}
api_index = {}
for f in glob.glob(os.path.join(API_DIR, "*.js")):
    fname = os.path.basename(f)[:-3]
    src = open(f, encoding="utf-8").read()
    api_index[fname] = {}
    # fnName(a,b) { return http.METHOD('url', ...) }
    for m in re.finditer(r"(\w+)\s*\([^)]*\)\s*\{[^}]*?return\s+http\.(get|post|put|delete|patch)\(\s*'([^']+)'", src, re.S):
        fn, method, url = m.group(1), m.group(2).upper(), m.group(3)
        api_index[fname][fn] = (method, url)
    # 也支持 export function fn(...) { return http.METHOD('url'...) }
    for m in re.finditer(r"(?:export\s+function\s+|\bfunction\s+)(\w+)\s*\([^)]*\)\s*\{[^}]*?return\s+http\.(get|post|put|delete|patch)\(\s*'([^']+)'", src, re.S):
        fn, method, url = m.group(1), m.group(2).upper(), m.group(3)
        api_index[fname][fn] = (method, url)

# 2) 遍历所有 vue 组件
pages = json.load(open(os.path.join(os.path.dirname(__file__), "pages.json"), encoding="utf-8"))
results = []
for p in pages:
    comp = p["component_fs"]
    if not os.path.exists(comp):
        results.append({**p, "apis": [], "missing_component": True})
        continue
    src = open(comp, encoding="utf-8").read()
    # import api 模块: import X from '@/api/yyy' 或 import {a,b} from '@/api/yyy' 或 import * as X from '@/api/yyy'
    ns_map = {}  # 命名空间 -> api file
    for m in re.finditer(r"import\s+(?:(\w+)\s*,?\s*)?(?:\{([^}]*)\})?\s*from\s*'@/api/(\w+)'", src):
        default_ns, named, apifile = m.group(1), m.group(2), m.group(3)
        if default_ns:
            ns_map[default_ns] = apifile
        if named:
            # 命名导入直接用方法名（局部名=方法名）
            for nm in re.findall(r"(\w+)", named):
                ns_map[nm] = apifile
        # 无 default 也无 named 的情况跳过
    # 调用: NS.method( 或 method(
    apis = []
    seen = set()
    def add_api(apifile, fn):
        if apifile in api_index and fn in api_index[apifile]:
            method, url = api_index[apifile][fn]
            key = (method, url)
            if key not in seen:
                seen.add(key)
                apis.append({"method": method, "url": url, "fn": fn, "api_file": apifile})
    for ns, apifile in ns_map.items():
        # NS.method(  (非默认导入的 named 方法名本身就是 fn)
        # 若 ns 是 default 导入 -> 调用形如 ns.fn(
        for m in re.finditer(re.escape(ns) + r"\.(\w+)\s*\(", src):
            add_api(apifile, m.group(1))
        # 若 ns 是 named 方法名本身 -> 直接 fn(
        add_api(apifile, ns)  # 仅当该 ns 实为方法名
    results.append({**p, "apis": apis, "missing_component": False})

json.dump(results, open(os.path.join(os.path.dirname(__file__), "pages_api.json"), "w", encoding="utf-8"), ensure_ascii=False, indent=2)

# 统计
total_apis = sum(len(p["apis"]) for p in results)
print(f"页面数 {len(results)}, 解析到的 API 调用总数 {total_apis}")
missing = [p for p in results if p.get("missing_component")]
print(f"组件缺失 {len(missing)}")
noapi = [p for p in results if not p.get("missing_component") and not p["apis"]]
print(f"无 API 调用的页面 {len(noapi)}:")
for p in noapi:
    print(f"   /{p['path']:38s} {os.path.basename(p['component_fs'])}")
# 保存 unique url 清单
allurls = {}
for p in results:
    for a in p["apis"]:
        allurls.setdefault((a["method"], a["url"]), []).append(p["path"])
print(f"\n唯一 API 端点 {len(allurls)}")
