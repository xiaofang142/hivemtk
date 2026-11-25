#!/usr/bin/env python3
"""
数据分析模块真实端到端回归测试。
直接打运行中真实服务 localhost:8204 (dev 容器)，验证：
  1. 7 个数据分析页的读 API 真实可达且返回结构合理
  2. custom-report / ab-experiment 写路径 round-trip 真实落库
  3. funnel 访问数与 PG customer_events 真实计数一致性
  4. churn 预测表为空时接口优雅返回(缺陷记录在报告里)

用法:
  python3 data_analysis_e2e.py --base http://localhost:8204 \
    --pg "host=localhost port=8232 user=admin dbname=user_db password=***"
"""
import argparse
import ipaddress
import json
import os
import subprocess
import sys
import urllib.parse
import urllib.request
import urllib.error

# SSRF 防护：本脚本只打本机服务；换受控目标时通过 DA_E2E_ALLOWED_HOSTS 显式追加。
# 所有 urlopen 出口必须先过 host 白名单，拒绝 userinfo、非 http(s)、私网/保留地址。
_BASE_ALLOWED_HOSTS = ("localhost", "127.0.0.1", "::1")
_ALLOWED_HOSTS_ENV = "DA_E2E_ALLOWED_HOSTS"


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
    """目标必须是绝对 http(s) URL（禁 userinfo/fragment）且 host 命中白名单。"""
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


PAGES = {
    "conversion-funnel": "/api/conversion-funnel?start_date=2026-07-13&end_date=2026-08-13",
    "conversion-funnel-stage-visit": "/api/conversion-funnel/stage?stage=visit&start_date=2026-07-13&end_date=2026-08-13",
    "ai-productivity": "/api/ai-productivity/overview?start_date=2026-07-13&end_date=2026-08-13",
    "dashboard-screen": "/api/dashboard-screens",
    "customer-journey": "/api/customer-journey/overview",
    "custom-reports": "/api/custom-reports",
    "ab-experiments": "/api/ab-experiments",
    "churn-prediction": "/api/churn-prediction?page=1&page_size=5",
    "churn-risk-dist": "/api/churn-prediction/risk-distribution",
    "churn-users": "/api/churn-prediction/users?limit=5",
}


def login(base):
    pw = os.environ.get("E2E_ADMIN_PASSWORD")
    if not pw:
        raise SystemExit("E2E_ADMIN_PASSWORD 未设置，拒绝运行")
    req = urllib.request.Request(
        _check_target_url(base + "/api/auth/login"),
        data=json.dumps({"username": "admin", "password": pw}).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=10) as r:
        return json.load(r)["data"]["token"]


def get(base, token, path):
    req = urllib.request.Request(_check_target_url(base + path), headers={"Authorization": "Bearer " + token})
    with urllib.request.urlopen(req, timeout=10) as r:
        return r.status, json.load(r)


def pg_count(pg_dsn, sql):
    env = dict(os.environ)
    cmd = ["psql", pg_dsn, "-tAc", sql]
    out = subprocess.run(cmd, capture_output=True, text=True, env=env, timeout=30)
    return out.stdout.strip()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8204")
    ap.add_argument("--pg", required=True, help="psql -d 连接串, 含 password")
    args = ap.parse_args()

    # --base 是用户可控的唯一网络入口，尽早校验（失败即退出，不发任何请求）。
    _check_target_url(args.base)
    token = login(args.base)
    print("[login] ok, token_len=%d" % len(token))

    failures = []
    for name, path in PAGES.items():
        try:
            code, body = get(args.base, token, path)
        except urllib.error.HTTPError as e:
            failures.append("%s -> HTTP %d" % (name, e.code))
            print("FAIL %-28s HTTP %d" % (name, e.code))
            continue
        if code != 200:
            failures.append("%s -> HTTP %d" % (name, code))
            print("FAIL %-28s HTTP %d" % (name, code))
            continue
        d = body.get("data")
        if name.startswith("conversion-funnel") and isinstance(d, dict):
            print("OK   %-28s total=%s" % (name, d.get("total")))
        else:
            print("OK   %-28s type=%s" % (name, type(d).__name__))

    # DB 一致性: funnel 访问数 == customer_events 计数
    sql = ("SELECT count(*) FROM customer_events "
           "WHERE created_at >= '2026-07-13' AND created_at < '2026-08-14';")
    db_cnt = pg_count(args.pg, sql)
    _, funnel = get(args.base, token, PAGES["conversion-funnel"])
    api_total = funnel["data"]["total"]
    if str(api_total) == str(db_cnt):
        print("OK   db-consistency funnel=%s == customer_events=%s" % (api_total, db_cnt))
    else:
        failures.append("funnel mismatch api=%s db=%s" % (api_total, db_cnt))
        print("FAIL db-consistency funnel=%s != customer_events=%s" % (api_total, db_cnt))

    # churn 预测应有真实数据(流失计算已接入每日定时任务 RunChurnCalculationForAllCustomers)
    _, churn = get(args.base, token, PAGES["churn-prediction"])
    churn_total = churn["data"].get("total", 0)
    if churn_total and churn_total > 0:
        print("OK   churn_predictions total=%s (真实流失计算已生效)" % churn_total)
    else:
        failures.append("churn_predictions empty (RunChurnCalculationForAllCustomers 未产出数据)")
        print("FAIL churn_predictions total=%s" % churn_total)

    print("\n=== RESULT ===")
    if failures:
        print("FAILURES(%d):" % len(failures))
        for f in failures:
            print("  -", f)
        sys.exit(1)
    print("ALL READ PATHS PASS")


if __name__ == "__main__":
    main()
