#!/usr/bin/env python3
"""L1 全量 GET 冒烟（带 429 退避重试 + 限速 + 真实 id 回填）。
对 backend_routes.json 所有 GET 路由打真实服务 8204，输出 smoke_get.json + 汇总。
5xx 视为缺陷候选；429 退避重试后记为 pass(限流非缺陷)。
"""
import json, urllib.request, urllib.error, urllib.parse, ipaddress, time, os
from collections import Counter

# SSRF 防护：本脚本只打本机服务；换受控目标时通过 AUDIT_BASE_URL_ALLOWED_HOSTS 显式追加。
# 所有 urlopen 出口必须先过 host 白名单，拒绝 userinfo、非 http(s)、私网/保留地址。
_BASE_ALLOWED_HOSTS = ("localhost", "127.0.0.1", "::1")
_ALLOWED_HOSTS_ENV = "AUDIT_BASE_URL_ALLOWED_HOSTS"


def _allowed_target_hosts():
    extra = os.environ.get(_ALLOWED_HOSTS_ENV, "")
    return set(_BASE_ALLOWED_HOSTS) | {
        h.strip().lower().rstrip(".") for h in extra.split(",") if h.strip()
    }


def _host_in_allowlist(hostname, allowed):
    """精确匹配 host；同一 IP 的不同字面写法（如 ::1 展开形式）视为等价。"""
    hostname = hostname.lower().rstrip(".")
    if hostname in allowed:
        return True
    try:
        addr = ipaddress.ip_address(hostname)
    except ValueError:
        return False
    for candidate in allowed:
        try:
            if addr == ipaddress.ip_address(candidate):
                return True
        except ValueError:
            continue
    return False


def _check_target_url(url):
    """冒烟目标必须是绝对 http(s) URL（禁 userinfo/fragment）且 host 命中白名单。"""
    parts = urllib.parse.urlsplit(url)
    if parts.scheme not in ("http", "https") or not parts.netloc or parts.username or parts.password or parts.fragment:
        raise ValueError("非法目标 URL: %r" % url)
    hostname = parts.hostname
    if not hostname or not _host_in_allowlist(hostname, _allowed_target_hosts()):
        raise ValueError("目标 host 不在白名单: %r（可用 %s 追加）" % (hostname, _ALLOWED_HOSTS_ENV))
    try:
        addr = ipaddress.ip_address(hostname.lower().rstrip("."))
    except ValueError:
        # 域名只能靠精确白名单放行，不做 DNS 解析（避免 DNS rebinding 绕过）。
        return url
    if not addr.is_loopback and (addr.is_private or addr.is_link_local or addr.is_reserved
                                or addr.is_multicast or addr.is_unspecified):
        raise ValueError("禁止请求内网/保留地址: %r" % hostname)
    return url


BASE = os.environ.get("AUDIT_BASE_URL", "http://localhost:8204")
# 路径穿越防护：结果文件写入根目录固定为脚本所在目录；realpath 解析后必须仍在该目录内。
_AUDIT_ROOT = os.path.realpath(os.path.dirname(os.path.abspath(__file__)))
AUDIT = _AUDIT_ROOT


def _safe_audit_path(name):
    """把输出文件名约束在脚本目录内，拒绝 .. / 绝对路径 / 符号链接逃逸。"""
    if not name or os.path.isabs(name) or ".." in name.split("/") or ".." in name.split("\\"):
        raise ValueError("输出文件必须是脚本目录下的文件名，禁止路径穿越")
    path = os.path.realpath(os.path.join(_AUDIT_ROOT, name))
    if path == _AUDIT_ROOT or os.path.commonpath([_AUDIT_ROOT, path]) != _AUDIT_ROOT:
        raise ValueError("输出文件越出脚本目录")
    return path


routes = json.load(open(os.path.join(AUDIT, "backend_routes.json")))

def login():
    pw = os.environ.get("E2E_ADMIN_PASSWORD")
    if not pw:
        raise SystemExit("E2E_ADMIN_PASSWORD 未设置，拒绝运行")
    data = json.dumps({"username":"admin","password":pw}).encode()
    req = urllib.request.Request(_check_target_url(BASE+"/api/auth/login"), data=data, headers={"Content-Type":"application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=20).read()).get("data",{}).get("token","")
_check_target_url(BASE)  # 用户可经 AUDIT_BASE_URL 注入目标，模块加载即校验，尽早失败
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
    url = _check_target_url(BASE + path)
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

smoke_out = _safe_audit_path("smoke_get.json")
if os.path.commonpath([_AUDIT_ROOT, os.path.realpath(smoke_out)]) != _AUDIT_ROOT:
    raise ValueError("输出文件越出脚本目录")
with open(smoke_out, "w") as _f:
    json.dump(results, _f, indent=2)
err5 = [x for x in results if x["status"]>=500]
err0 = [x for x in results if x["status"]==0]
c = Counter(x["status"] for x in results)
print(f"总 {len(results)}, 5xx={len(err5)}, 连接错={len(err0)}")
print("分布:", dict(sorted(c.items())))
for x in err5: print("  5xx:", x["path"], "->", x["filled"])
