#!/usr/bin/env python3
"""测试脚手架 testkit：封装 登录/API调用/DB查询，供各业务域写测试复用。"""
import json, urllib.request, urllib.error, time, os

BASE = "http://localhost:8204"
AUDIT = os.path.dirname(os.path.abspath(__file__))

# DB 连接（按工作记忆）
import subprocess as _sp
def _pgpw():
    out = _sp.run(["grep", "^POSTGRES_PASSWORD=", "/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/.env"],
                  capture_output=True, text=True).stdout
    return out.strip().split("=",1)[1] if out.strip() else ""

PGPW = _pgpw()
DSN = f"postgresql://admin:{PGPW}@localhost:8232/user_db"

_token = {"t": "", "ts": 0}
def token(force=False):
    now = time.time()
    if not force and _token["t"] and now - _token["ts"] < 60:
        return _token["t"]
    data = json.dumps({"username":"admin","password":"Admin@123456"}).encode()
    req = urllib.request.Request(BASE+"/api/auth/login", data=data, headers={"Content-Type":"application/json"})
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
    url = BASE + path
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
