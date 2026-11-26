#!/usr/bin/env python3
"""测试脚手架 testkit：封装 登录/API调用/DB查询，供各业务域写测试复用。"""
import json, urllib.request, urllib.error, urllib.parse, ipaddress, time, os
from pathlib import Path

# SSRF 防护：本脚手架只打本机服务；换受控目标时通过 AUDIT_BASE_URL_ALLOWED_HOSTS 显式追加。
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
    """请求目标必须是绝对 http(s) URL（禁 userinfo/fragment）且 host 命中白名单。"""
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
AUDIT = os.path.dirname(os.path.abspath(__file__))
_check_target_url(BASE)  # BASE 可经环境变量注入，模块加载即校验，尽早失败

# DB 连接（按工作记忆）
import subprocess as _sp
def _pgpw():
    env_file = Path(__file__).resolve().parents[3] / ".env"
    out = _sp.run(["grep", "^POSTGRES_PASSWORD=", str(env_file)],
                  capture_output=True, text=True).stdout
    return out.strip().split("=",1)[1] if out.strip() else ""

PGPW = _pgpw()
DSN = f"postgresql://admin:{PGPW}@localhost:8232/user_db"

_token = {"t": "", "ts": 0}
def token(force=False):
    now = time.time()
    if not force and _token["t"] and now - _token["ts"] < 60:
        return _token["t"]
    pw = os.environ.get("E2E_ADMIN_PASSWORD")
    if not pw:
        raise SystemExit("E2E_ADMIN_PASSWORD 未设置，拒绝运行")
    data = json.dumps({"username":"admin","password":pw}).encode()
    req = urllib.request.Request(_check_target_url(BASE+"/api/auth/login"), data=data, headers={"Content-Type":"application/json"})
    j = json.loads(urllib.request.urlopen(req, timeout=20).read())
    _token["t"] = j.get("data",{}).get("token","")
    _token["ts"] = time.time()
    return _token["t"]

_rate = {"last":0.0}
def _throttle():
    gap = 1/8.0
    now = time.time()
    w = gap - (now - _rate["last"])
    if w > 0: time.sleep(w)
    _rate["last"] = time.time()

def api(method, path, body=None, raw=False, retries=3):
    """返回 (status, json_or_text)。body 为 dict 时以 JSON 发送。"""
    _throttle()
    url = _check_target_url(BASE + path)
    headers = {"Authorization":"Bearer "+token(), "Accept":"application/json"}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, headers=headers, method=method.upper())
    for att in range(retries+1):
        try:
            resp = urllib.request.urlopen(req, timeout=40)
            b = resp.read()
            if raw: return resp.status, b
            try: return resp.status, json.loads(b)
            except: return resp.status, b.decode("utf-8","replace")
        except urllib.error.HTTPError as e:
            b = e.read()
            if e.code == 429 and att < retries:
                time.sleep(0.6*(att+1)); continue
            try: return e.code, json.loads(b)
            except: return e.code, b.decode("utf-8","replace")
        except Exception as e:
            if att < retries:
                time.sleep(0.6); continue
            return 0, str(e)[:200]
    return 0, "max retries"

def db_q(sql, params=None):
    """查询，返回 list[dict]。"""
    cmd = ["psql", "-h","localhost","-p","8232","-U","admin","-d","user_db","-tA","-F","\x01","-c",sql]
    env = dict(os.environ); env["PGPASSWORD"]=PGPW
    r = _sp.run(cmd, capture_output=True, text=True, env=env)
    if r.returncode != 0:
        return {"__error__": r.stderr.strip()}
    rows = []
    for line in r.stdout.splitlines():
        if not line.strip(): continue
        # 首列作为标记
        rows.append(line)
    return rows

def db_rows(sql):
    """返回 list[dict]，首行用列名（需 -P 不现实，简化：返回原始行供计数/存在性判断）。"""
    cmd = ["psql", "-h","localhost","-p","8232","-U","admin","-d","user_db","-tA","-c",sql]
    env = dict(os.environ); env["PGPASSWORD"]=PGPW
    r = _sp.run(cmd, capture_output=True, text=True, env=env)
    if r.returncode != 0:
        return {"__error__": r.stderr.strip()}
    out = []
    for line in r.stdout.splitlines():
        if line.strip()=="" : continue
        out.append(line.split("\x01"))
    return out

def db_count(sql):
    rows = db_rows(sql)
    if isinstance(rows, dict): return rows
    return len(rows)

def log(*a):
    print(*a, flush=True)

if __name__ == "__main__":
    s,j = api("GET","/api/knowledge/products")
    log("products status", s, "sample:", str(j)[:120])
