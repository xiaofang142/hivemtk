#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
epay_live_e2e.py — 易支付**真实网关**全链路验收（三条产品线各跑一遍）

与 epay_mock_e2e.py 的分工（两者互补，不互相替代）：
  · mock 脚本验「状态机与订阅闸」——收银台是本地 mock confirm，网关那一段是假的；
  · 本脚本验 mock 覆盖不到的三段真实差异：
      ① 下单口真的打到 mapi.php 并拿回网关 payurl（不是本地 mock、也不是死掉的 submit.php）
      ② 订单页「去支付」重开收银台走同一条腿（关掉收银台后订单仍可在线支付）
      ③ 回调：MD5 验签（直接拼 key）→ 金额比对 → MarkPaid → 权益落盘 → 重放不重复入账
  产品线：功能模块（module）、订阅续费（subscription）、LLM 套餐（llm）。

回调的诚实边界（务必读）：
  本脚本按网关报文格式、用商户密钥**自己签一条**投给 /public/payments/notify/epay。
  验签/比对/入账代码路径与真实回调**完全一致**，但**不证明**真实网关会把通知打到这里
  （那需要真实付款 + 公网可达回调，见 docs/operations/EPAY_LIVE_CHECKLIST.md §3/§4）。
  线上放行仍以该清单为准；本脚本是它的自动化前置。

用法：
  python3 scripts/epay_live_e2e.py                          # 本地双栈（默认 base）
  python3 scripts/epay_live_e2e.py --skip llm               # 只跑模块+续费
  python3 scripts/epay_live_e2e.py --no-sql                 # 纯 HTTP 断言（线上无本地 psql）
  python3 scripts/epay_live_e2e.py --user-base https://u --plat-base https://p --no-sql

  密钥来源：EPAY_LIVE_PLATFORM_ENV=<hivemtk-platform/.env>（默认取本仓同级目录）
  该文件里的 EPAY_API_KEY 只进内存用于签名，**不打印、不落盘**。

退出码：0 = 全部断言通过；1 = 有 FAIL；3 = 前置/凭据缺失。
"""
from __future__ import annotations

import argparse
import hashlib
import os
import secrets
import shlex
import string
import subprocess
import sys
import time

try:
    import requests
except ImportError:  # pragma: no cover
    sys.stderr.write("缺少 requests：pip3 install requests\n")
    sys.exit(3)

# 复用同仓 mock 脚本的四段式取证与 HTTP 客户端（模块级无副作用，main 有 __main__ 护栏）
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from epay_mock_e2e import Client, Report, code_of, data_of  # noqa: E402
from url_guard import allowed_target_hosts, check_target_url  # noqa: E402  URL 闸：防 SSRF


def safe_post(url, **kwargs):
    """requests.post 的唯一入口：URL 必过 host 白名单闸（防 SSRF）。

    默认仅放行回环；真网关验收时由 HIVEMTK_SCRIPT_ALLOWED_HOSTS 显式追加
    网关/用户/平台域名（私网/保留/链路本地 IP 与 userinfo/非 http(s) 一律拒绝）。
    """
    check_target_url(url, allowed_hosts=allowed_target_hosts())
    return requests.post(url, **kwargs)

USER_BASE = os.environ.get("EPAY_E2E_USER_BASE", "http://127.0.0.1:8204").rstrip("/")
PLAT_BASE = os.environ.get("EPAY_E2E_PLAT_BASE", "http://127.0.0.1:8205").rstrip("/")
MODULE_KEY = os.environ.get("EPAY_E2E_MODULE_KEY", "browser-automation")
# 平台 .env：EPAY_API_KEY（验签密钥）/ EPAY_GATEWAY_URL / 平台库口令。
# 本脚本在 <workspace>/hivemtk/scripts/，平台仓是同级的 <workspace>/hivemtk-platform/。
_HERE = os.path.dirname(os.path.abspath(__file__))
_WORKSPACE = os.path.dirname(os.path.dirname(_HERE))
PLATFORM_ENV = os.environ.get("EPAY_LIVE_PLATFORM_ENV") or os.path.join(
    _WORKSPACE, "hivemtk-platform", ".env")
# 本地平台库（容器内 postgres 监听 8201）；线上用 --no-sql 跳过 SQL 断言
PSQL = shlex.split(os.environ.get(
    "EPAY_LIVE_PSQL", "docker exec -i mtk-platform-postgres psql -p 8201"))
PG_USER = os.environ.get("EPAY_LIVE_PG_USER", "admin")
PG_DB = os.environ.get("EPAY_LIVE_PG_DB", "platform_db")


class LiveReport(Report):
    """四段式取证，汇总标题用本脚本名（其余接口沿用 epay_mock_e2e.Report）。"""

    def finish(self):
        print(f"\n{'=' * 70}\n【报告】epay_live_e2e\n{'=' * 70}")
        passed = sum(1 for r in self.rows if r[2])
        total = len(self.rows)
        for step, name, ok, detail in self.rows:
            if not ok:
                print(f"  ✗ [{step}] {name} — {detail}")
        print(f"总计: {passed}/{total} PASS" +
              (f"，DEGRADED {len(self.degraded)} 条" if self.degraded else ""))
        for d in self.degraded:
            print(f"  ⚠ {d}")
        return 0 if total and passed == total else 1


def load_platform_env(path: str) -> dict:
    """读平台 .env 的键值（只取本脚本用得到的键；不打印任何值）。"""
    want = {"EPAY_API_KEY", "EPAY_GATEWAY_URL", "EPAY_PID", "EPAY_TRADE_TYPES",
            "POSTGRES_PASSWORD"}
    out = {}
    try:
        with open(path, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                k, v = line.split("=", 1)
                if k in want:
                    out[k] = v.strip().strip('"').strip("'")
    except OSError:
        return out
    return out


_pg_password: str | None = None


def _sql_env() -> dict:
    """psql 连接环境：口令从容器现取（只进子进程环境，不打印）。"""
    global _pg_password
    env = dict(os.environ)
    if "PGPASSWORD" in env:
        return env
    if _pg_password is None:
        try:
            _pg_password = subprocess.run(
                ["docker", "exec", "mtk-platform-postgres", "printenv",
                 "POSTGRES_PASSWORD"],
                capture_output=True, text=True, timeout=15,
            ).stdout.strip()
        except (OSError, subprocess.SubprocessError):
            _pg_password = ""
    if _pg_password:
        env["PGPASSWORD"] = _pg_password
    return env


def psql(sql: str, rep: Report) -> str:
    """平台库查询（本机容器）。失败返回空串，由调用方决定是否降级。"""
    cmd = PSQL + ["-U", PG_USER, "-d", PG_DB, "-v", "ON_ERROR_STOP=1", "-tAc", sql]
    rep.request("SQL", " ".join(shlex.quote(c) for c in cmd), None,
                extra="(对账取证)")
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, timeout=30,
                              env=_sql_env())
    except (OSError, subprocess.SubprocessError) as e:  # pragma: no cover
        rep.response(0, None, f"psql 执行失败: {e}")
        return ""
    if proc.returncode != 0:
        rep.response(proc.returncode, None, (proc.stderr or "").strip()[:300])
        return ""
    out = (proc.stdout or "").strip()
    rep.response(0, {"stdout": out[:300]})
    return out


def epay_sign(params: dict, key: str) -> str:
    """官方 SDK 口径：排除 sign/sign_type/空值 → ksort → a=b&c=d → **直接拼 key** → md5。

    不是 `&key=KEY`。与 platform-server 的 EpaySign 同源，回归向量见
    payment_service_test.go::TestEpaySignMatchesOfficialSDKVector。
    """
    pairs = sorted(f"{k}={v}" for k, v in params.items()
                   if k not in ("sign", "sign_type") and v not in (None, ""))
    return hashlib.md5(("&".join(pairs) + key).encode("utf-8")).hexdigest()


def send_notify(plat: Client, rep: Report, key: str, params: dict, desc: str):
    """按网关 POST 表单形态投一条异步通知（回调口是公开组，无鉴权）。"""
    body = dict(params)
    body["sign"] = epay_sign(body, key)
    body["sign_type"] = "MD5"
    url = plat.base + "/public/payments/notify/epay"
    rep.request("POST", url + "  (form)", body, extra=f"({desc})")
    try:
        r = safe_post(url, data=body, timeout=30)
    except requests.RequestException as e:
        rep.response(0, None, f"请求异常: {e}")
        return 0, ""
    text = (r.text or "").strip()
    try:
        rep.response(r.status_code, r.json())
    except ValueError:
        rep.response(r.status_code, None, text)
    return r.status_code, text


def cents_to_yuan(cents: int) -> str:
    return f"{cents / 100:.2f}"


def pick_pay_url(data: dict) -> tuple[str, str]:
    pay = data.get("payment") or {}
    return pay.get("pay_url") or "", pay.get("method") or ""


def assert_real_cashier(rep: Report, name: str, pay_url: str, method: str,
                        gateway: str) -> bool:
    """收银台必须是真实网关的 mapi 结果，不是本地 mock、也不是死掉的 submit.php。"""
    ok = bool(pay_url) and method == "GET" and gateway.rstrip("/") in pay_url \
        and not pay_url.rstrip("/").endswith("submit.php")
    return rep.assert_(name, ok,
                       f"pay_url={pay_url!r} method={method!r} 期望: 含网关域名、"
                       f"method=GET、非 submit.php（该口对本商户实测 404）")


def wait_pay_status(user: Client, rep: Report, token: str, order_no: str,
                    tries: int = 20) -> str | None:
    status = None
    for _ in range(tries):
        st, payload, _ = user.get(f"/api/module-market/orders/{order_no}/pay-status",
                                  desc="支付状态直查（轮询）", token=token)
        if st == 200 and code_of(payload) == 0:
            status = data_of(payload).get("status")
            if status in ("paid", "fulfilled"):
                return status
        time.sleep(1)
    return status


# ---------------------------------------------------------------------------
# S0 前置：双栈可达 + 真实模式（mock 路由必须 404）+ 通道配置就绪
# ---------------------------------------------------------------------------
def s0_preflight(user: Client, plat: Client, rep: Report, env: dict,
                 sql_ok: bool) -> str:
    rep.step("S0 前置探测：双栈可达 / 真实模式（EPAY_MOCK 关）/ 通道与密钥就绪")

    st, payload, _ = user.get("/api/public/config", desc="用户端公开配置")
    rep.assert_("S0.1 用户端可达（code=0）", st == 200 and code_of(payload) == 0,
                f"HTTP={st} code={code_of(payload)}")
    st, payload, _ = plat.get("/health", desc="平台端健康检查")
    rep.assert_("S0.2 平台端可达", st == 200, f"HTTP={st}")

    # mock 收银台是条件路由：EPAY_MOCK=true 才注册。真人验收必须 404。
    st, payload, _ = plat.post("/platform/payments/mock/confirm/PROBE",
                               desc="mock 路由探测（期望 404=已关）")
    rep.assert_("S0.3 真实模式：mock confirm 路由不存在（404）", st == 404,
                f"HTTP={st}（若 401/200 说明 EPAY_MOCK 仍开着，必须先关掉）")

    key = env.get("EPAY_API_KEY", "")
    rep.assert_("S0.4 平台 .env 里有 EPAY_API_KEY（验签密钥，值不回显）",
                bool(key), f"EPAY_LIVE_PLATFORM_ENV={PLATFORM_ENV}")
    gateway = env.get("EPAY_GATEWAY_URL") or ""
    if gateway:
        rep.note(f"EPAY_GATEWAY_URL={gateway}")

    if sql_ok:
        row = psql("SELECT enabled, gateway_url, pid, left(api_key,7), trade_types "
                   "FROM payment_channels WHERE channel='epay';", rep)
        parts = row.split("|") if row else []
        ok = len(parts) == 5 and parts[0] == "t" and bool(parts[2]) \
            and parts[3] == "enc:v1:"
        rep.assert_("S0.5 通道 epay 已启用、pid 有值、api_key 密文落库",
                    ok, f"row={row!r}（api_key 只看前缀，不出明文）")
    else:
        rep.degrade("S0.5 跳过 SQL 断言（--no-sql）")
    return key


# ---------------------------------------------------------------------------
# S1 注册租户（沿用 mock 脚本口径：每轮唯一账号，trial 起步）
# ---------------------------------------------------------------------------
def s1_register(user: Client, rep: Report, run: str, sql_ok: bool = True) -> str | None:
    rep.step("S1 注册 SaaS 租户 → sync → 订阅状态 → 商户镜像")
    st, payload, _ = user.get("/api/tenant/regulatory-notice", desc="监管告知")
    agreement = ""
    if st == 200 and code_of(payload) == 0:
        agreement = data_of(payload).get("agreement_version") or ""

    username = "ev_" + run
    body = {
        "name": "真网关验收" + run,
        "username": username,
        "email": f"{username}@example.com",
        # 测试用随机口令改为 CSPRNG（secrets），满足密码复杂度并消除不安全随机性。
        "password": secrets.token_hex(7) + "7a",
        "company_name": "真网关验收" + run,
        "legal_person": "验收负责人",
        "phone": "13800138000",
        "license_no": "EV-LIC-" + run,
        "address": "验收测试地址",
        "regulatory_ack": True,
        "agreement_version": agreement,
    }
    st, payload, _ = user.post("/api/tenant/register", desc="自助开户", body=body)
    if code_of(payload) == 4105:
        rep.note("命中注册限流 4105，30s 后重试一次")
        time.sleep(30)
        st, payload, _ = user.post("/api/tenant/register", desc="自助开户（重试）",
                                   body=body)
    ok = st == 200 and code_of(payload) == 0 and bool(data_of(payload).get("token"))
    rep.assert_("S1.1 注册成功 code=0 且拿到 token", ok,
                f"HTTP={st} code={code_of(payload)}")
    if not ok:
        return None
    token = data_of(payload)["token"]
    rep.note(f"tenant_code={data_of(payload).get('tenant_code')} username={username}")

    st, payload, _ = user.post("/api/modules/sync", desc="同步（写订阅镜像）",
                               token=token)
    rep.assert_("S1.2 sync code=0", st == 200 and code_of(payload) == 0,
                f"HTTP={st} code={code_of(payload)}")

    # 需求 2026-10-21 #2：开户登记必须镜像进 merchants —— 否则平台端
    # "租户登记有数据、商户管理空白"。
    if sql_ok:
        company = body["company_name"]
        row = psql("SELECT count(*) FROM merchants WHERE name='%s';" % company, rep)
        rep.assert_("S1.3 开户登记已镜像进 merchants（商户管理有数据）",
                    bool(row) and int(row) >= 1, f"merchants 匹配行={row!r}")
    else:
        rep.degrade("S1.3 跳过 SQL 断言（--no-sql）")
    return token


# ---------------------------------------------------------------------------
# S2 功能模块：下单 → 真实收银台 → 去支付 → 回调入账 → 权益
# ---------------------------------------------------------------------------
def s2_module(user: Client, plat: Client, rep: Report, token: str, run: str,
               key: str, gateway: str, sql_ok: bool) -> str | None:
    rep.step(f"S2 功能模块 {MODULE_KEY}：下单 → mapi 真实收银台 → 去支付 → 回调")
    st, payload, _ = user.get("/api/module-market/list", desc="市场列表")
    price = None
    if st == 200 and code_of(payload) == 0:
        for it in data_of(payload).get("list", []):
            if it.get("module_key") == MODULE_KEY:
                price = it.get("price_cents")
    rep.assert_("S2.1 市场列表含目标模块且定价 > 0",
                bool(price and price > 0), f"price_cents={price}")
    if not price:
        return None

    body = {
        "module_keys": [MODULE_KEY],
        "contact_name": "真网关验收",
        "contact_phone": "13800138000",
        "contact_email": f"ev_{run}@example.com",
        "remark": "epay_live_e2e 下单",
        "client_order_no": "EV" + run,
    }
    st, payload, _ = user.post("/api/module-market/order", desc="模块下单",
                               token=token, body=body)
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S2.2 下单 code=0", ok, f"HTTP={st} code={code_of(payload)}")
    if not ok:
        return None
    d = data_of(payload)
    order_no = d.get("order_no")
    total = int(d.get("total_amount_cents") or 0)
    rep.note(f"order_no={order_no} total={total}分")

    pay_url, method = pick_pay_url(d)
    assert_real_cashier(rep, "S2.3 payment 段是真实网关 mapi 收银台", pay_url,
                        method, gateway)
    rep.assert_("S2.4 payment.mock 不为 true（真网关）",
                (d.get("payment") or {}).get("mock") is not True,
                f"mock={(d.get('payment') or {}).get('mock')!r}")

    # 「去支付」：关掉收银台后订单页重开
    st, payload, _ = user.post(f"/api/module-market/orders/{order_no}/pay",
                               desc="订单页去支付（重开收银台）", token=token)
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S2.5 去支付 code=0", ok, f"HTTP={st} code={code_of(payload)}")
    if ok:
        pay_url2, method2 = pick_pay_url(data_of(payload))
        assert_real_cashier(rep, "S2.6 去支付返回同款真实收银台", pay_url2, method2,
                            gateway)

    base = {
        "pid": env_pid, "type": "alipay", "out_trade_no": order_no,
        "trade_no": "EV" + time.strftime("%Y%m%d%H%M%S") +
                    "".join(secrets.choice(string.digits) for _ in range(6)),
        "trade_status": "TRADE_SUCCESS", "money": cents_to_yuan(total),
        "name": "HiveMTK module " + order_no,
    }

    # 反向 1：金额被改（签是对的）→ 必须 400 且不入账
    tampered = dict(base, money=cents_to_yuan(total + 1))
    st, text = send_notify(plat, rep, key, tampered, "金额篡改（签名正确）")
    rep.assert_("S2.7 金额不符的回调被拒（HTTP 400）", st == 400,
                f"HTTP={st} resp={text[:120]}")

    # 正向：正确金额
    st, text = send_notify(plat, rep, key, base, "真实报文形态回调")
    rep.assert_("S2.8 正确回调 HTTP 200 且 body=success",
                st == 200 and text == "success", f"HTTP={st} resp={text[:120]}")

    status = wait_pay_status(user, rep, token, order_no)
    rep.assert_("S2.9 pay-status 轮询到 paid/fulfilled",
                status in ("paid", "fulfilled"), f"status={status!r}")

    # 重放：同一 trade_no 再投一次 → 200，但不重复入账
    st, text = send_notify(plat, rep, key, base, "重放同一笔")
    rep.assert_("S2.10 重放回调 200 success", st == 200 and text == "success",
                f"HTTP={st} resp={text[:120]}")

    st, payload, _ = user.post("/api/modules/sync", desc="sync", token=token)
    rep.assert_("S2.11 sync code=0", st == 200 and code_of(payload) == 0,
                f"HTTP={st} code={code_of(payload)}")
    st, payload, _ = user.get("/api/modules/my", desc="我的模块", token=token)
    owned = None
    if st == 200 and code_of(payload) == 0:
        for it in data_of(payload).get("list", []):
            if it.get("module_key") == MODULE_KEY:
                owned = it.get("owned")
    rep.assert_("S2.12 模块已激活 owned=true", owned is True, f"owned={owned!r}")

    if sql_ok:
        row = psql(
            "SELECT o.status, (SELECT count(*) FROM payment_records p "
            "WHERE p.order_no=o.order_no), "
            "(SELECT count(*) FROM payment_callbacks c WHERE c.order_no=o.order_no "
            "AND c.verified AND c.processed_at IS NOT NULL) "
            f"FROM orders o WHERE o.order_no='{order_no}';", rep)
        parts = row.split("|") if row else []
        rep.assert_("S2.13 DB：订单 fulfilled + 流水恰 1 行 + 回调已验签入账",
                    len(parts) == 3 and parts[0] == "fulfilled"
                    and parts[1] == "1" and parts[2] == "1", f"row={row!r}")
    else:
        rep.degrade("S2.13 跳过 SQL 断言（--no-sql）")
    return order_no


# ---------------------------------------------------------------------------
# S3 订阅续费：下单 → 收银台 → 回调 → 顺延
# ---------------------------------------------------------------------------
def s3_subscription(user: Client, plat: Client, rep: Report, token: str, run: str,
                    key: str, gateway: str, sql_ok: bool) -> str | None:
    rep.step("S3 订阅续费：下单 → 真实收银台 → 回调 → 有效期顺延")
    before_st, payload, _ = user.get("/api/subscription/status", desc="续费前状态")
    before_end = ""
    if before_st == 200 and code_of(payload) == 0:
        before_end = data_of(payload).get("current_period_end") or ""
        rep.note(f"续费前 status={data_of(payload).get('status')} "
                 f"current_period_end={before_end or '（trial，无 period）'}")

    st, payload, _ = user.post("/api/subscription/renew", desc="订阅续费下单",
                               token=token, body={
                                   "client_order_no": "SR" + run,
                                   "contact_name": "真网关验收",
                                   "contact_phone": "13800138000",
                                   "remark": "epay_live_e2e 续费",
                               })
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S3.1 续费下单 code=0", ok, f"HTTP={st} code={code_of(payload)}")
    if not ok:
        return None
    d = data_of(payload)
    order_no = d.get("order_no")
    total = int(d.get("total_amount_cents") or 0)
    rep.note(f"order_no={order_no} total={total}分")
    if total <= 0:
        rep.degrade("S3 续费单金额为 0（免支付直通），测不到真实收银台")
        return order_no

    pay_url, method = pick_pay_url(d)
    assert_real_cashier(rep, "S3.2 续费单收银台是真实网关", pay_url, method,
                        gateway)

    params = {
        "pid": env_pid, "type": "alipay", "out_trade_no": order_no,
        "trade_no": "EV" + time.strftime("%Y%m%d%H%M%S") +
                    "".join(secrets.choice(string.digits) for _ in range(6)),
        "trade_status": "TRADE_SUCCESS", "money": cents_to_yuan(total),
        "name": "HiveMTK subscription " + order_no,
    }
    st, text = send_notify(plat, rep, key, params, "续费回调")
    rep.assert_("S3.3 回调 200 success", st == 200 and text == "success",
                f"HTTP={st} resp={text[:120]}")

    status = wait_pay_status(user, rep, token, order_no)
    rep.assert_("S3.4 pay-status 到 paid/fulfilled",
                status in ("paid", "fulfilled"), f"status={status!r}")

    # 订阅镜像是 sync 落库的（pay-status 只刷订单态）：不 sync 读到的还是续费前那段
    st, payload, _ = user.post("/api/modules/sync", desc="sync（刷订阅镜像）",
                               token=token)
    rep.assert_("S3.4b sync code=0", st == 200 and code_of(payload) == 0,
                f"HTTP={st} code={code_of(payload)}")

    st, payload, _ = user.get("/api/subscription/status", desc="续费后状态",
                              token=token)
    d2 = data_of(payload) if st == 200 and code_of(payload) == 0 else {}
    after_end = d2.get("current_period_end") or ""
    locked = d2.get("locked")
    rep.assert_("S3.5 续费后未锁（locked != true）", locked is not True,
                f"status={d2.get('status')!r} locked={locked!r}")
    if before_end:
        rep.assert_("S3.6 current_period_end 顺延",
                    bool(after_end) and after_end > before_end,
                    f"before={before_end} after={after_end}")
    else:
        rep.degrade("S3.6 续费前无 current_period_end（trial 期），只校验有值")
        rep.assert_("S3.6b 续费后有 current_period_end", bool(after_end),
                    f"after={after_end!r}")

    if sql_ok:
        row = psql("SELECT count(*) FROM subscription_grants "
                   f"WHERE order_no='{order_no}';", rep)
        rep.assert_("S3.7 幂等锚 subscription_grants 恰 1 行", row == "1",
                    f"row={row!r}")
        row = psql("SELECT count(*) FROM payment_records "
                   f"WHERE order_no='{order_no}';", rep)
        rep.assert_("S3.8 续费流水恰 1 行", row == "1", f"row={row!r}")
    else:
        rep.degrade("S3.7/8 跳过 SQL 断言（--no-sql）")
    return order_no


# ---------------------------------------------------------------------------
# S4 LLM 套餐：下单 → 收银台 → 回调 → 余额入账
# ---------------------------------------------------------------------------
def ensure_llm_module(user: Client, plat: Client, rep: Report, token: str,
                      run: str, key: str) -> None:
    """套餐列表被模块闸门挡住（4301）时，先 1 元买下 llm-cloud 再放行。

    付费模块标价后新租户不再自动持有 llm-cloud，套餐页必须先开通模块。
    这条腿顺带把"付费模块购买"也纳入真网关验收。
    """
    st, payload, _ = user.get("/api/llm/plans", desc="套餐列表（闸门探测）",
                              token=token)
    if st == 200 and code_of(payload) == 0:
        return
    st, payload, _ = user.post("/api/module-market/order",
                               desc="模块闸门 4301 → 1 元开通 llm-cloud",
                               token=token, body={
                                   "module_keys": ["llm-cloud"],
                                   "client_order_no": "EM" + run,
                                   "contact_phone": "13800138000",
                               })
    if st != 200 or code_of(payload) != 0:
        return  # 交由 S4.1 断言如实报告
    d = data_of(payload)
    order_no = d.get("order_no")
    total = int(d.get("total_amount_cents") or 0)
    params = {
        "pid": env_pid, "type": "alipay", "out_trade_no": order_no,
        "trade_no": "EV" + time.strftime("%Y%m%d%H%M%S") +
                    "".join(secrets.choice(string.digits) for _ in range(6)),
        "trade_status": "TRADE_SUCCESS", "money": cents_to_yuan(total),
        "name": "HiveMTK llm-cloud " + str(order_no),
    }
    send_notify(plat, rep, key, params, "llm-cloud 模块回调")
    wait_pay_status(user, rep, token, order_no)
    user.post("/api/modules/sync", desc="sync（激活 llm-cloud）", token=token)
    rep.note(f"llm-cloud 已开通 order_no={order_no} total={total}分")


def s4_llm(user: Client, plat: Client, rep: Report, token: str, run: str,
            key: str, gateway: str, sql_ok: bool, plan_code: str) -> str | None:
    rep.step("S4 LLM 套餐：下单 → 真实收银台 → 回调 → 余额入账")
    ensure_llm_module(user, plat, rep, token, run, key)
    st, payload, _ = user.get("/api/llm/plans", desc="套餐列表", token=token)
    plans = data_of(payload).get("list", []) if st == 200 and code_of(payload) == 0 else []
    chosen = None
    for p in plans:
        if plan_code and p.get("plan_code") == plan_code:
            chosen = p
            break
        if not plan_code and int(p.get("price_cents") or 0) > 0 and chosen is None:
            chosen = p
    rep.assert_("S4.1 找到可购套餐（price_cents>0）",
                bool(chosen) and int(chosen.get("price_cents") or 0) > 0,
                f"plan={(chosen or {}).get('plan_code')!r} "
                f"price={(chosen or {}).get('price_cents')!r}")
    if not chosen:
        return None

    st, payload, _ = user.get("/api/llm/wallet", desc="充值前余额", token=token)
    before_in = data_of(payload).get("input_tokens_remaining") \
        if st == 200 and code_of(payload) == 0 else None
    rep.note(f"充值前 input_tokens_remaining={before_in}")

    st, payload, _ = user.post("/api/llm/recharge", desc="LLM 套餐下单",
                               token=token, body={
                                   "plan_code": chosen.get("plan_code"),
                                   "client_order_no": "EL" + run,
                                   "contact_phone": "13800138000",
                                   "remark": "epay_live_e2e LLM",
                               })
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S4.2 LLM 下单 code=0", ok, f"HTTP={st} code={code_of(payload)}")
    if not ok:
        return None
    d = data_of(payload)
    order_no = d.get("order_no")
    total = int(d.get("total_amount_cents") or 0)
    rep.note(f"order_no={order_no} total={total}分")

    pay_url, method = pick_pay_url(d)
    assert_real_cashier(rep, "S4.3 LLM 单收银台是真实网关", pay_url, method,
                        gateway)

    params = {
        "pid": env_pid, "type": "alipay", "out_trade_no": order_no,
        "trade_no": "EV" + time.strftime("%Y%m%d%H%M%S") +
                    "".join(secrets.choice(string.digits) for _ in range(6)),
        "trade_status": "TRADE_SUCCESS", "money": cents_to_yuan(total),
        "name": "HiveMTK llm " + order_no,
    }
    st, text = send_notify(plat, rep, key, params, "LLM 回调")
    rep.assert_("S4.4 回调 200 success", st == 200 and text == "success",
                f"HTTP={st} resp={text[:120]}")

    status = wait_pay_status(user, rep, token, order_no)
    rep.assert_("S4.5 pay-status 到 paid/fulfilled",
                status in ("paid", "fulfilled"), f"status={status!r}")

    after_in = None
    for _ in range(10):
        user.post("/api/modules/sync", desc="sync（刷钱包镜像）", token=token)
        st, payload, _ = user.get("/api/llm/wallet", desc="充值后余额", token=token)
        if st == 200 and code_of(payload) == 0:
            after_in = data_of(payload).get("input_tokens_remaining")
            if before_in is None or (after_in or 0) > before_in:
                break
        time.sleep(1)
    rep.assert_("S4.6 余额已入账（input_tokens_remaining 增加）",
                before_in is None or (after_in or 0) > before_in,
                f"before={before_in} after={after_in}")

    if sql_ok:
        row = psql("SELECT count(*) FROM payment_records "
                   f"WHERE order_no='{order_no}';", rep)
        rep.assert_("S4.7 LLM 流水恰 1 行", row == "1", f"row={row!r}")
        row = psql("SELECT status FROM orders WHERE order_no='%s';" % order_no, rep)
        rep.assert_("S4.8 订单 fulfilled", row == "fulfilled", f"row={row!r}")
    else:
        rep.degrade("S4.7/8 跳过 SQL 断言（--no-sql）")
    return order_no


# ---------------------------------------------------------------------------
# S5 回调反向用例：未签名 / 未知订单 / 未知通道
# ---------------------------------------------------------------------------
def s5_negatives(plat: Client, rep: Report, key: str) -> None:
    rep.step("S5 回调反向用例：无签名 / 金额改签 / 未知订单")
    url = plat.base + "/public/payments/notify/epay"

    # 无 sign
    raw = {"pid": env_pid, "out_trade_no": "EVPROBE", "trade_no": "EVPROBENO",
           "trade_status": "TRADE_SUCCESS", "money": "1.00"}
    rep.request("POST", url + "  (form)", raw, extra="(无签名)")
    r = safe_post(url, data=raw, timeout=30)
    rep.response(r.status_code, None, (r.text or "")[:200])
    rep.assert_("S5.1 无签名回调 HTTP 401", r.status_code == 401,
                f"HTTP={r.status_code}")

    # 改钱但不改签
    bad = {"pid": env_pid, "out_trade_no": "EVPROBE", "trade_no": "EVPROBET",
           "trade_status": "TRADE_SUCCESS", "money": "999.00",
           "sign": epay_sign({"pid": env_pid, "out_trade_no": "EVPROBE",
                              "trade_no": "EVPROBET",
                              "trade_status": "TRADE_SUCCESS", "money": "1.00"},
                             key),
           "sign_type": "MD5"}
    rep.request("POST", url + "  (form)", bad, extra="(改钱不改签)")
    r = safe_post(url, data=bad, timeout=30)
    rep.response(r.status_code, None, (r.text or "")[:200])
    rep.assert_("S5.2 改钱不改签 → 401", r.status_code == 401,
                f"HTTP={r.status_code}")

    # 未知订单（签名正确）
    ghost = {"pid": env_pid, "out_trade_no": "EVNOSUCHORDER",
             "trade_no": "EVGHOST" + time.strftime("%H%M%S"),
             "trade_status": "TRADE_SUCCESS", "money": "1.00",
             "name": "ghost"}
    st, text = send_notify(plat, rep, key, ghost, "未知订单（签名正确）")
    rep.assert_("S5.3 未知订单 → 404", st == 404, f"HTTP={st} resp={text[:120]}")


# ---------------------------------------------------------------------------
# S6 三向对账（清单 §5 的本机子集）
# ---------------------------------------------------------------------------
def s6_reconcile(rep: Report, order_nos: list[str], sql_ok: bool,
                 paid_skipped: bool = False) -> None:
    rep.step("S6 三向对账：订单 ↔ 支付流水 ↔ 回调留痕（本轮订单）")
    if not sql_ok:
        rep.degrade("S6 跳过 SQL 断言（--no-sql）")
        return
    if paid_skipped:
        # 付费场景全跳过时本轮没有可对账的订单（免费单不走支付流水/回调）
        rep.degrade("S6 跳过：module/subscription/llm 全被 --skip，无付费订单")
        return
    if not order_nos:
        rep.assert_("S6.0 本轮至少有一张订单", False, "没有可对账的订单号")
        return
    in_list = ",".join(f"'{o}'" for o in order_nos)
    row = psql(
        "SELECT count(*) FROM orders o WHERE o.order_no IN (" + in_list + ") "
        "AND o.status IN ('paid','fulfilled') "
        "AND (SELECT count(*) FROM payment_records p WHERE p.order_no=o.order_no)=1 "
        "AND (SELECT coalesce(sum(p.amount_cents),-1) FROM payment_records p "
        "     WHERE p.order_no=o.order_no)=o.total_amount_cents "
        "AND (SELECT count(*) FROM payment_callbacks c WHERE c.order_no=o.order_no "
        "     AND c.verified AND c.processed_at IS NOT NULL)>=1;", rep)
    rep.assert_("S6.1 每张订单：状态终态 + 流水 1 行金额相符 + 回调已入账",
                row == str(len(order_nos)),
                f"满足条数={row} / 本轮订单数={len(order_nos)}")

    row = psql("SELECT count(*) FROM payment_records p WHERE p.order_no IN ("
               + in_list + ") GROUP BY p.order_no HAVING count(*) > 1;", rep)
    rep.assert_("S6.2 无同一订单多条流水", row == "", f"row={row!r}")

    row = psql("SELECT count(*) FROM subscription_grants g WHERE g.order_no IN ("
               + in_list + ") AND NOT EXISTS (SELECT 1 FROM orders o "
               "WHERE o.order_no = g.order_no);", rep)
    rep.assert_("S6.3 无孤儿 grant", row == "0", f"row={row!r}")


# ---------------------------------------------------------------------------
# S7 免费/0 元功能订阅：下单即开通，不拦联系方式、不弹线下指引
# （需求 2026-10-21 #1：免费功能订阅直接完成开通，不需要联系微信）
# ---------------------------------------------------------------------------
def s7_free(user: Client, rep: Report, token: str, run: str, sql_ok: bool) -> None:
    rep.step("S7 免费模块：下单即开通（无联系方式、无 payment 段也不拦）")
    free_key = None
    st, payload, _ = user.get("/api/module-market/list", desc="市场列表（找免费模块）")
    if st == 200 and code_of(payload) == 0:
        for it in data_of(payload).get("list", []):
            if (it.get("free") or int(it.get("price_cents") or 0) == 0) \
                    and not it.get("owned"):
                free_key = it.get("module_key")
                break
    rep.assert_("S7.1 市场存在未开通的免费模块", bool(free_key), f"free_key={free_key!r}")
    if not free_key:
        return

    # 故意不带任何联系方式：免费单必须直接开通
    body = {"module_keys": [free_key], "client_order_no": "EV" + run + "F"}
    st, payload, _ = user.post("/api/module-market/order",
                               desc="免费模块下单（不填联系方式）",
                               token=token, body=body)
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S7.2 免费下单 code=0（不因缺联系方式被拒）", ok,
                f"HTTP={st} code={code_of(payload)} "
                f"msg={payload.get('msg') or payload.get('message')}")
    if not ok:
        return
    d = data_of(payload)
    order_no = d.get("order_no")
    rep.assert_("S7.3 下单响应 status=fulfilled（0 元直通）",
                d.get("status") == "fulfilled", f"status={d.get('status')!r}")
    rep.assert_("S7.4 无 payment 段且 payment_guide 已清空（不引导线下付款）",
                not d.get("payment") and not (d.get("payment_guide") or "").strip(),
                f"payment={d.get('payment')!r} guide={d.get('payment_guide')!r}")

    st, payload, _ = user.post("/api/modules/sync", desc="sync", token=token)
    rep.assert_("S7.5 sync code=0", st == 200 and code_of(payload) == 0,
                f"HTTP={st} code={code_of(payload)}")
    st, payload, _ = user.get("/api/modules/my", desc="我的模块", token=token)
    owned = None
    if st == 200 and code_of(payload) == 0:
        for it in data_of(payload).get("list", []):
            if it.get("module_key") == free_key:
                owned = it.get("owned")
    rep.assert_("S7.6 免费模块已激活 owned=true", owned is True, f"owned={owned!r}")

    if sql_ok:
        row = psql("SELECT status, coalesce(contact_phone,''), "
                   "coalesce(contact_email,'') "
                   f"FROM orders WHERE order_no='{order_no}';", rep)
        rep.assert_("S7.7 DB：订单 fulfilled 且未强塞联系方式",
                    row == "fulfilled||", f"row={row!r}")
    else:
        rep.degrade("S7.7 跳过 SQL 断言（--no-sql）")


env_pid = ""


def main():
    global env_pid
    ap = argparse.ArgumentParser(description="易支付真实网关全链路验收")
    ap.add_argument("--user-base", default=USER_BASE)
    ap.add_argument("--plat-base", default=PLAT_BASE)
    ap.add_argument("--skip", default="",
                    help="逗号分隔，跳过步骤：module,subscription,llm,free,negatives")
    ap.add_argument("--plan-code", default="", help="LLM 套餐编码（默认取第一个正价套餐）")
    ap.add_argument("--no-sql", action="store_true", help="跳过全部 SQL 断言（线上）")
    ap.add_argument("--client-ip", default="203.0.113.10",
                    help="模拟买家公网 IP（X-Forwarded-For）。"
                         "本地回环 127.0.0.1 会被网关拒收 clientip；"
                         "传空串改用真实来源")
    args = ap.parse_args()

    skip = {s.strip() for s in args.skip.split(",") if s.strip()}
    env = load_platform_env(PLATFORM_ENV)
    key = env.get("EPAY_API_KEY", "")
    gateway = env.get("EPAY_GATEWAY_URL", "https://pay.gggua.com")
    env_pid = env.get("EPAY_PID", "") or os.environ.get("EPAY_PID", "")

    rep = LiveReport()
    run = time.strftime("%m%d%H%M") + "".join(secrets.choice(string.digits) for _ in range(3))
    user = Client(args.user_base, rep, False)
    plat = Client(args.plat_base, rep, False)
    if args.client_ip:
        user.s.headers["X-Forwarded-For"] = args.client_ip

    print("=" * 70)
    print(f"epay_live_e2e  run={run}  mode=LIVE（真实网关）")
    print(f"  用户端 {args.user_base}   平台端 {args.plat_base}")
    print(f"  网关 {gateway}  pid={env_pid or '(待读)'}  "
          f"clientip={args.client_ip or '(真实来源)'}")
    print(f"  SQL 断言 {'关闭(--no-sql)' if args.no_sql else '开启'}  "
          f"跳过: {sorted(skip) or '无'}")
    print("=" * 70)

    # pid 优先取通道行（配置页写入的那份才是回调验签口径）
    if not args.no_sql:
        row = psql("SELECT pid, gateway_url FROM payment_channels "
                   "WHERE channel='epay';", rep)
        if row and "|" in row:
            db_pid, db_gw = row.split("|", 1)
            if db_pid:
                env_pid = db_pid
            if db_gw:
                gateway = db_gw
            rep.note(f"通道行 pid={env_pid} gateway={gateway}")

    if not s0_preflight(user, plat, rep, env, not args.no_sql):
        rep.finish()
        sys.exit(3)
    if not key or not env_pid:
        rep.finish()
        sys.stderr.write("❌ 缺少 EPAY_API_KEY 或 pid（通道未配置）\n")
        sys.exit(3)

    token = s1_register(user, rep, run, not args.no_sql)
    if not token:
        sys.exit(rep.finish())

    order_nos = []
    if "module" not in skip:
        no = s2_module(user, plat, rep, token, run, key, gateway, not args.no_sql)
        if no:
            order_nos.append(no)
    if "subscription" not in skip:
        no = s3_subscription(user, plat, rep, token, run, key, gateway,
                             not args.no_sql)
        if no:
            order_nos.append(no)
    if "llm" not in skip:
        no = s4_llm(user, plat, rep, token, run, key, gateway, not args.no_sql,
                    args.plan_code)
        if no:
            order_nos.append(no)
    if "free" not in skip:
        s7_free(user, rep, token, run, not args.no_sql)
    if "negatives" not in skip:
        s5_negatives(plat, rep, key)
    s6_reconcile(rep, order_nos, not args.no_sql,
                 paid_skipped={"module", "subscription", "llm"} <= skip)

    sys.exit(rep.finish())


if __name__ == "__main__":
    main()
