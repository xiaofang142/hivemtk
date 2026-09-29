#!/usr/bin/env python3
"""L1 全量 GET 冒烟（带 429 退避重试 + 限速 + 真实 id 回填）。
对 backend_routes.json 所有 GET 路由打真实服务 8204，输出 smoke_get.json + 汇总。
5xx 视为缺陷候选；429 退避重试后记为 pass(限流非缺陷)。
"""
import json, urllib.request, urllib.error, time, os
from collections import Counter

BASE = "http://localhost:8204"
AUDIT = "/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web/tests/audit"
routes = json.load(open(os.path.join(AUDIT, "backend_routes.json")))

def login():
    data = json.dumps({"username":"admin","password":"Admin@123456"}).encode()
    req = urllib.request.Request(BASE+"/api/auth/login", data=data, headers={"Content-Type":"application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=20).read()).get("data",{}).get("token","")
TOKEN = login()

rate = {"last":0.0}
def throttle():
    # 限速到 ~8 req/s 以避免触发全局 RPS=10 限流
    gap = 1/8.0
    now = time.time()
    wait = gap - (now - rate["last"])
    if wait > 0:
        time.sleep(wait)
    rate["last"] = time.time()

def do_get(path, attempt=0):
    url = BASE + path
    req = urllib.request.Request(url, headers={"Authorization":"Bearer "+TOKEN, "Accept":"application/json"})
    try:
        resp = urllib.request.urlopen(req, timeout=30)
        body = resp.read()
        try: j = json.loads(body)
        except: j = None
        return resp.status, j
    except urllib.error.HTTPError as e:
        if e.code == 429 and attempt < 5:
            time.sleep(0.5 * (attempt+1))
            return do_get(path, attempt+1)
        return e.code, None
    except Exception:
        if attempt < 3:
            time.sleep(0.5)
            return do_get(path, attempt+1)
        return 0, None

# 先收集一些真实 id（供回填）
def collect_ids():
    ids = {}
    # 通用 list 取首条 id
    probes = {
        "agent": "/api/ai-agents",
        "channel": "/api/chat-channels",
        "product": "/api/knowledge/products",
        "customer": "/api/customers",
        "session": "/api/customer-sessions",
    }
    for key, path in probes.items():
        st, j = do_get(path)
        if st == 200 and isinstance(j, dict):
            arr = j.get("data") or j.get("items") or j.get("list")
            if isinstance(arr, list) and arr:
                first = arr[0]
                for fk in ("id","agent_id","channel_id","product_id","customer_id","session_id"):
                    if fk in first:
                        ids[key] = str(first[fk]); break
    return ids

IDS = collect_ids()
print("真实 id:", IDS)

# 占位 id 规则
PLACEHOLDERS = [":id",":code",":channel_id",":account_id",":session_id",":customer_id",":product_id",":doc_id",":agent_id",":name",":aid",":traceId",":rule_id",":template_id",":msg_id"]
IDKEY_FOR = {
    ":agent_id":"agent", ":channel_id":"channel", ":product_id":"product",
    ":customer_id":"customer", ":session_id":"session",
}

get_routes = [r for r in routes if r["method"]=="GET"]
skip_substr = ("/ws", "websocket", "/share/", "/download", "/export", "/upload")
results = []
for r in get_routes:
    p = r["path"]
    if p in ("/",) or any(s in p for s in skip_substr):
        continue
    # 回填占位
    filled = p
    for ph in PLACEHOLDERS:
        if ph in filled:
            key = IDKEY_FOR.get(ph)
            val = IDS.get(key) if key else "1"
            if val is None: val = "1"
            filled = filled.replace(ph, val)
    st, j = do_get(filled)
    results.append({"path":p, "filled":filled, "status":st})

json.dump(results, open(os.path.join(AUDIT,"smoke_get.json"),"w"), indent=2)
err5 = [x for x in results if x["status"]>=500]
err0 = [x for x in results if x["status"]==0]
c = Counter(x["status"] for x in results)
print(f"总 {len(results)}, 5xx={len(err5)}, 连接错={len(err0)}")
print("分布:", dict(sorted(c.items())))
for x in err5: print("  5xx:", x["path"], "->", x["filled"])
