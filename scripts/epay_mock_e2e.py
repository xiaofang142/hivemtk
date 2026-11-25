#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
epay_mock_e2e.py — 003 计费二期 T028：mock 收银台端到端回归（可重复执行）

覆盖链路（对本地「双栈」可反复执行，每轮生成唯一租户/订单号，互不撞）：
  S0 前置探测（双栈可达 + 平台 EPAY_MOCK 路由已注册 + edition=saas）
  S1 平台管理员登录 → 支付配置页 API 确认/设置 epay 通道 enabled(mock)
  S2 SaaS 用户端注册租户（POST /api/tenant/register）→ 同步 → 订阅状态 trial
  S3 模块市场下单 browser-automation → 断言 payment 段 pay_url 指向 mock 收银台
  S4 平台 mock confirm 支付 → 轮询 pay-status 至 paid/fulfilled → sync → /api/modules/my 已激活
  S5 SQL 把订阅 period 调过期（测试后门）→ 业务接口 4305 / 白名单接口正常
  S6 平台侧订阅续费下单 → mock confirm → 用户端 sync → 订阅 active、业务接口恢复
  S7 T029 三端点复核（edition/cloud_url · 订阅状态 · pay-status）

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
前置条件（本脚本**不负责**拉起服务；双栈须先按 dev 惯例起好）
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
0) 数据层（本机已起）：
     docker start mtk-postgres mtk-platform-postgres mtk-redis
     # 用户端库 127.0.0.1:8202 ｜ 平台端库 127.0.0.1:8201

1) 平台端 8205，EPAY_MOCK=true（**必须 GIN_MODE≠release**：
   platform-server/router.go 里 EPAY_MOCK=true 且 release 会 log.Fatal 拒绝启动）：
     cd hivemtk-platform/platform-server
     set -a && { [ -f ../.env ] && . ../.env; [ -f ./.env ] && . ./.env; }; set +a
     export PORT=8205 EPAY_MOCK=true GIN_MODE=debug
     go run ./cmd/api
   注意：`make dev`(air) 的 full_bin 会先 source ../.env，再 source 之后的
   环境变量对 .env 里**已存在**的键（如 GIN_MODE=release）无效 —— 所以 mock 模式
   要么直接 go run 上面这条，要么把 ../.env 里的 GIN_MODE 改成 debug。

2) 用户端 8204，TENANT_MODE=on + PLATFORM_ENABLED=true：
     cd hivemtk/user-server
     set -a && { [ -f ../.env ] && . ../.env; [ -f ./.env ] && . ./.env; }; set +a
     export PORT=8204 TENANT_MODE=on PLATFORM_ENABLED=true \
            PLATFORM_API_HOST=http://127.0.0.1:8205
     go run ./cmd/api
   （cmd/api 的 LoadDotEnv 不覆盖已存在的环境变量，所以 export 在 .env 之后仍然生效；
     用 make dev/air 则会被 ../.env 里的 TENANT_MODE=off / PLATFORM_ENABLED=false 盖掉。）
   平台侧 MERCHANT_API_SECRET 与用户端必须一致（两个 .env 同源即可）。

3) 平台管理员口令：取 hivemtk-platform/.env 的 PLATFORM_ADMIN_PASSWORD。

用法：
  # 真跑（需要双栈已按上面起好）
  EPAY_E2E_PLATFORM_PASS=<平台管理员口令> python3 scripts/epay_mock_e2e.py

  # 无双栈时：静态验证请求构造 + 断言逻辑（不发任何网络请求，用代码同源的响应夹具）
  python3 scripts/epay_mock_e2e.py --dry-run

  # 只做前置探测（服务可达性 / mock 路由注册状态 / 公开配置）
  python3 scripts/epay_mock_e2e.py --preflight

  # 子集与善后
  ... --skip-expire          # 跳过 S5（不动订阅 period）
  ... --skip-renew           # 跳过 S6
  ... --restore-only         # 只把订阅 period 恢复成未来（S5 之后中途失败的救急）
  ... --cleanup              # 删除本轮注册的测试租户（SQL 测试后门）

凭据一律从环境变量/命令行读，脚本内**没有任何明文口令**：
  EPAY_E2E_PLATFORM_PASS   平台管理员口令（live 必填；可用 --platform-pass 传入）
  EPAY_E2E_PLATFORM_USER   平台管理员账号（默认 admin）
  EPAY_E2E_USER_BASE       用户端 base（默认 http://127.0.0.1:8204）
  EPAY_E2E_PLAT_BASE       平台端 base（默认 http://127.0.0.1:8205）
  EPAY_E2E_PSQL            psql 前缀命令（默认 docker exec -i mtk-postgres psql）
  EPAY_E2E_PG_USER/DB      psql -U/-d（默认 admin / user_db）

退出码：0 = 全部断言通过；1 = 有断言失败；3 = 前置/凭据缺失（用法错误）。

风格：沿用 scripts/api_verify_full.py 的逐跳取证口径，但按 003 的四段式输出
      【步骤】【请求】【响应】【断言】。
"""
from __future__ import annotations

import argparse
import json
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

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from url_guard import allowed_target_hosts, check_target_url  # noqa: E402  URL 闸：防 SSRF

# ---------------------------------------------------------------------------
# 配置（全部来自环境变量/命令行，不落明文）
# ---------------------------------------------------------------------------
USER_BASE = os.environ.get("EPAY_E2E_USER_BASE", "http://127.0.0.1:8204").rstrip("/")
PLAT_BASE = os.environ.get("EPAY_E2E_PLAT_BASE", "http://127.0.0.1:8205").rstrip("/")
PLAT_USER = os.environ.get("EPAY_E2E_PLATFORM_USER", "admin")
PLAT_PASS = os.environ.get("EPAY_E2E_PLATFORM_PASS", "")
PSQL = shlex.split(os.environ.get(
    # 注意：mtk-postgres 容器内 postgres 监听 8202（socket=.s.PGSQL.8202），不是默认 5432。
    "EPAY_E2E_PSQL", "docker exec -i mtk-postgres psql -p 8202"))
PG_USER = os.environ.get("EPAY_E2E_PG_USER", os.environ.get("POSTGRES_USER", "admin"))
PG_DB = os.environ.get("EPAY_E2E_PG_DB", os.environ.get("USER_DB_NAME", "user_db"))
MODULE_KEY = os.environ.get("EPAY_E2E_MODULE_KEY", "browser-automation")
# 业务面探针：必须是「有模块归属」的非白名单路径（否则订阅闸放行 + WARN，测不出 4305）。
BIZ_PROBES = ["/api/feature-flags", "/api/analytics/ai-performance"]
# 白名单面（订阅闸显式放行，锁态下必须仍可用）
USER_STATUS_OK = "trial,active,expired,unknown"

MASK_KEYS = {"password", "passwd", "pwd", "token", "access_token", "authorization",
             "api_key", "apikey", "client_secret", "secret"}


def mask(v, depth=0):
    if depth > 8:
        return "***"
    if isinstance(v, dict):
        out = {}
        for k, val in v.items():
            if isinstance(k, str) and k.strip().lower() in MASK_KEYS:
                n = len(val) if isinstance(val, str) else 0
                out[k] = "***" if not n else f"***[{n} chars]"
            else:
                out[k] = mask(val, depth + 1)
        return out
    if isinstance(v, list):
        return [mask(x, depth + 1) for x in v]
    return v


class Report:
    """四段式取证：每步打印 【步骤】【请求】【响应】【断言】。"""

    def __init__(self):
        self.rows = []
        self.degraded = []
        self.sent = []          # (method, url) —— dry-run 自检用
        self.sim = None         # dry-run 夹具（psql 也要同步改状态）
        self._step = ""

    def step(self, title):
        self._step = title
        print(f"\n{'=' * 70}\n【步骤】{title}\n{'=' * 70}")

    def note(self, text):
        print(f"  · {text}")

    def request(self, method, url, body=None, extra=""):
        self.sent.append((method.upper(), url))
        line = f"【请求】{method.upper()} {url}"
        if extra:
            line += f"  {extra}"
        if body is not None:
            line += f"\n        body={json.dumps(mask(body), ensure_ascii=False)[:400]}"
        print(line)

    def response(self, status, payload, text=""):
        if isinstance(payload, (dict, list)):
            echo = json.dumps(mask(payload), ensure_ascii=False)
        else:
            echo = (text or str(payload))[:300]
        print(f"【响应】HTTP {status} {echo[:400]}")

    def assert_(self, name, ok, detail=""):
        mark = "PASS" if ok else "FAIL"
        print(f"【断言】[{mark}] {name}" + (f" — {detail}" if detail else ""))
        self.rows.append((self._step, name, bool(ok), detail))
        return bool(ok)

    def degrade(self, why):
        print(f"【断言】[DEGRADED] {why}")
        self.degraded.append(why)

    def finish(self):
        print(f"\n{'=' * 70}\n【报告】epay_mock_e2e\n{'=' * 70}")
        passed = sum(1 for r in self.rows if r[2])
        total = len(self.rows)
        for step, name, ok, detail in self.rows:
            if not ok:
                print(f"  ✗ [{step}] {name} — {detail}")
        print(f"总计: {passed}/{total} PASS" + (f"，DEGRADED {len(self.degraded)} 条" if self.degraded else ""))
        for d in self.degraded:
            print(f"  ⚠ {d}")
        return 0 if total and passed == total else 1


# ---------------------------------------------------------------------------
# dry-run 夹具：形状取自两仓 response 封装与控制器源码（不是随手编的）
#   用户端成功 {code:0,message,data}；业务错误 HTTP200 + code=4305/4xxx
#   平台端成功 {code:200,msg,data}；业务错误 HTTP200 + code=400/401...
#   平台 mock confirm 成功返回 **纯文本 "success"**（processNotify ctx.String）
# ---------------------------------------------------------------------------
class Sim:
    def __init__(self, run):
        self.run = run
        self.edition = "saas"
        self.cloud_url = "https://saas.example.invalid"
        self.agreement = "v3.6"
        self.sub_status = "trial"
        self.sub_locked = False
        self.order_status = "pending_payment"
        self.owned = False
        self.channel_enabled = True
        self.renew_pending = False
        self.tenant_code = "t_" + run
        self.user_token = "DRY-USER-JWT"
        self.plat_token = "DRY-PLATFORM-JWT"
        self.order_no = None

    # --- 用户端 envelope ---
    def u(self, data, msg="ok"):
        return 200, {"code": 0, "message": msg, "data": data}

    def ubiz(self, code, msg):
        return 200, {"code": code, "message": msg, "data": {}}

    # --- 平台端 envelope ---
    def p(self, data, msg="ok"):
        return 200, {"code": 200, "msg": msg, "data": data}

    def fixture(self, method, path, body, token=None):
        m, p = method.upper(), path
        # ---------- 平台端 ----------
        if p == "/api/auth/login":
            return self.p({"token": self.plat_token, "user": {"id": 1, "username": PLAT_USER, "role": "admin"}}, "登录成功")
        if p == "/platform/payments/channels" and m == "GET":
            return self.p({"list": [{
                "channel": "epay", "name": "易支付", "enabled": self.channel_enabled,
                "notify_url": "", "gateway_url": "", "pid": "mock-pid", "trade_types": "alipay",
                "remark": "mock", "api_key_set": False, "enc_key_ready": True, "mock": True,
            }]})
        if p.startswith("/platform/payments/channels/") and m == "PUT":
            self.channel_enabled = bool((body or {}).get("enabled", True))
            return self.p({"channel": "epay", "enabled": self.channel_enabled, "mock": True}, "支付渠道配置已保存")
        if p.startswith("/platform/payments/mock/confirm/"):
            # 与真实回调同一条 processNotify：成功是纯文本 success
            if not self._mock_route_registered:
                return 404, {"error": "not found", "path": p}
            if not token:
                # admin+审计组：无 JWT 一律 401（preflight 就是靠它判路由已注册）
                return 401, {"code": 401, "msg": "未登录"}
            self.order_status = "fulfilled"
            return 200, "success"
        if p.startswith("/platform/orders/"):
            return self.p({"order_no": p.rsplit("/", 1)[-1], "status": self.order_status,
                           "order_type": "module", "total_amount_cents": 19900})
        # ---------- 用户端 ----------
        if p == "/api/public/config":
            return self.u({"edition": self.edition, "cloud_url": self.cloud_url,
                           "site_name": "HiveMtk", "reg_mode": "tenant"})
        if p == "/api/tenant/regulatory-notice":
            return self.u({"notice": "平台监管责任告知", "agreement_version": self.agreement,
                           "required_fields": ["company_name", "legal_person", "phone", "address"]})
        if p == "/api/tenant/register":
            return self.u({"tenant_id": 1, "tenant_code": self.tenant_code, "token": self.user_token}, "注册成功")
        if p == "/api/modules/sync" and m == "POST":
            # sync 会把平台最新订单态与订阅镜像写回本地
            if self.order_status == "fulfilled":
                self.owned = True
            # 续费单已 mock 支付 → 平台下发的 period 推到未来，镜像重算为 active
            if self.renew_pending and self.order_status in ("paid", "fulfilled"):
                self.sub_status = "active"
                self.sub_locked = False
                self.renew_pending = False
            return self.u({"active_count": 1, "source": "platform", "degraded": False,
                           "message": "同步完成"}, "同步完成")
        if p == "/api/subscription/status":
            return self.u({"status": self.sub_status, "locked": self.sub_locked,
                           "no_data": False, "stale": False, "price_cents": 19900,
                           "current_period_end": None, "trial_expires_at": None})
        if p == "/api/subscription/renew" and m == "POST":
            self.pending_sub_order = "SUB" + self.run
            self.renew_pending = True
            return self.u({"order_no": self.pending_sub_order, "status": "pending_payment",
                           "total_amount_cents": 19900, "items": [],
                           "payment": {"gateway_url": "", "pay_url": "/platform/payments/mock/confirm/" + self.pending_sub_order,
                                       "params": {"out_trade_no": self.pending_sub_order},
                                       "method": "POST", "mock": True},
                           "payment_guide": "", "duplicate": False, "platform_enabled": True},
                          "续费订单已提交，请完成支付")
        if p == "/api/module-market/list" and m == "GET":
            return self.u({"list": [{"module_key": MODULE_KEY, "name": "浏览器自动化", "category": "paid",
                                     "price_cents": 19900, "billing_cycle": "monthly", "owned": self.owned,
                                     "free": False, "free_to_enable": False, "menu_count": 3}],
                           "total": 1, "platform_enabled": True})
        if p == "/api/module-market/order" and m == "POST":
            self.order_no = (body or {}).get("client_order_no") or ("EB" + self.run)
            return self.u({"order_no": self.order_no, "status": "pending_payment",
                           "total_amount_cents": 19900,
                           "items": [{"module_key": MODULE_KEY}],
                           "payment": {"gateway_url": "", "pay_url": "/platform/payments/mock/confirm/" + self.order_no,
                                       "params": {"out_trade_no": self.order_no},
                                       "method": "POST", "mock": True},
                           "payment_guide": "", "duplicate": False, "platform_enabled": True},
                          "订单已提交，请完成支付")
        if "/pay-status" in p and m == "GET":
            paid = self.order_status in ("paid", "fulfilled")
            st = self.order_status if paid else "pending_payment"
            no = p.split("/orders/")[1].split("/")[0]
            return self.u({"order_no": no, "status": st, "paid": paid})
        if p == "/api/modules/my":
            return self.u({"list": [{"module_key": MODULE_KEY, "owned": self.owned,
                                     "status": "active" if self.owned else "inactive"}],
                           "total": 1, "owned": 1 if self.owned else 0})
        if p in BIZ_PROBES:
            if self.sub_locked:
                return self.ubiz(4305, "订阅已到期，业务功能已暂停，请续费后继续使用")
            return self.u({"list": [], "total": 0})
        return 404, {"error": "not found", "path": p}

    # mock 路由是否注册（dry-run 默认已注册；--no-mock-route 可反向验证 404 分支）
    _mock_route_registered = True


# ---------------------------------------------------------------------------
# HTTP 层：live 走 requests；dry-run 走 Sim 夹具，请求照样打印（构造正确性可见）
# ---------------------------------------------------------------------------
class Client:
    def __init__(self, base, rep: Report, dry: bool, sim: Sim | None = None):
        self.base = base
        self.rep = rep
        self.dry = dry
        self.sim = sim
        self.s = requests.Session()

    def call(self, method, path, desc="", body=None, token=None,
             want_status=(200,), timeout=30):
        url = self.base + path
        # base 来自 --user-base / --plat-base 或环境变量：过 host 白名单闸，
        # 默认仅回环，拒 userinfo/非 http(s)/私网/保留/链路本地（防 SSRF）。
        check_target_url(url, allowed_hosts=allowed_target_hosts())
        headers = {}
        if token:
            headers["Authorization"] = f"Bearer {token}"
        self.rep.request(method, url, body,
                         extra=(f"({desc})" if desc else "") +
                               ("" if not token else "  Authorization=Bearer ***"))
        if self.dry:
            status, payload = self.sim.fixture(method, path, body, token)
            self.rep.response(status, payload)
            return status, payload, ""
        try:
            r = self.s.request(method, url, json=body, headers=headers, timeout=timeout)
        except requests.RequestException as e:
            self.rep.response(0, None, f"请求异常: {e}")
            return 0, None, str(e)
        payload = None
        text = r.text
        try:
            payload = r.json()
        except ValueError:
            payload = None
        self.rep.response(r.status_code, payload if payload is not None else text)
        return r.status_code, payload, text

    def post(self, path, **kw):
        return self.call("POST", path, **kw)

    def get(self, path, **kw):
        return self.call("GET", path, **kw)

    def put(self, path, **kw):
        return self.call("PUT", path, **kw)


def code_of(payload):
    return payload.get("code") if isinstance(payload, dict) else None


def data_of(payload):
    return (payload.get("data") or {}) if isinstance(payload, dict) else {}


def uniq(prefix, n=6):
    alphabet = string.ascii_lowercase + string.digits
    # 唯一串改用 CSPRNG（secrets.SystemRandom），测试/模拟也不能用可预测随机源。
    return prefix + "".join(secrets.choice(alphabet) for _ in range(n))


# ---------------------------------------------------------------------------
# SQL 测试后门（生产无此路径；仅本机回归用）
# ---------------------------------------------------------------------------
def psql(sql, dry, rep: Report):
    cmd = PSQL + ["-U", PG_USER, "-d", PG_DB, "-v", "ON_ERROR_STOP=1", "-tAc", sql]
    rep.request("SQL", " ".join(shlex.quote(c) for c in cmd), None, extra="(测试后门)")
    if dry:
        rep.response(0, {"dry_run": True, "sql": sql})
        sim = getattr(rep, "sim", None)
        if sim is not None:
            # SQL 测试后门在 dry-run 下同步推进夹具状态，让后续断言走同一条路径
            if "'expired'" in sql:
                sim.sub_status, sim.sub_locked = "expired", True
            elif "'active'" in sql:
                sim.sub_status, sim.sub_locked = "active", False
        return "DRY"
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.SubprocessError) as e:
        rep.response(0, None, f"psql 执行失败: {e}")
        return ""
    if proc.returncode != 0:
        rep.response(proc.returncode, None, (proc.stderr or "").strip()[:300])
        return ""
    rep.response(0, {"stdout": (proc.stdout or "").strip()[:300]})
    return (proc.stdout or "").strip()


# 订阅镜像 SQL 后门：tenant_subscription_mirror 已是**每租户一行**（tenant_id 列），
# 不能再写死 WHERE id = 1（那是单行时代的假设，多租户库里会改错别人的行，
# 表现为本轮租户 status 恒 trial/active、S5.2/3/4 三连红）。按本轮注册的
# eb_<run> 管理员反查 tenant_id 定位镜像行。
EXPIRE_SQL_TPL = (
    "UPDATE tenant_subscription_mirror SET "
    "status='expired', "
    "current_period_end = now() - interval '2 days', "
    "trial_expires_at = now() - interval '3 days', "
    "fetched_at = now(), updated_at = now() "
    "WHERE tenant_id = (SELECT tenant_id FROM system_users WHERE username = '%s')"
    " RETURNING id;"
)
RESTORE_SQL_TPL = (
    "UPDATE tenant_subscription_mirror SET "
    "status='active', "
    "current_period_end = now() + interval '30 days', "
    "fetched_at = now(), updated_at = now() "
    "WHERE tenant_id = (SELECT tenant_id FROM system_users WHERE username = '%s')"
    " RETURNING id;"
)


# ---------------------------------------------------------------------------
# 各步骤
# ---------------------------------------------------------------------------
def s0_preflight(user: Client, plat: Client, rep: Report, dry: bool):
    rep.step("S0 前置探测：双栈可达 / EPAY_MOCK 路由已注册 / edition=saas")
    st, payload, _ = user.get("/api/public/config", desc="公开配置（003 T003）")
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S0.1 用户端可达且 /api/public/config code=0", ok, f"HTTP={st} code={code_of(payload)}")
    if ok:
        d = data_of(payload)
        rep.assert_("S0.2 data.edition == saas（TENANT_MODE=on 实例）",
                    d.get("edition") == "saas", f"edition={d.get('edition')!r}")
        rep.assert_("S0.3 data 带 cloud_url 键", "cloud_url" in d, f"keys={sorted(d)}")

    # 平台 mock 路由探测：未登录打它，401=路由已注册(EPAY_MOCK=true)、404=未注册
    st2, payload2, text2 = plat.post("/platform/payments/mock/confirm/E2EPROBE",
                                     desc="mock 收银台探测（无 JWT）", want_status=(401, 404))
    if st2 == 404:
        rep.assert_("S0.4 平台 EPAY_MOCK 收银台路由已注册", False,
                    "404 = 路由未注册：平台端未开 EPAY_MOCK=true（或仍以 GIN_MODE=release 启动被 fatal 拒绝）")
    else:
        rep.assert_("S0.4 平台 EPAY_MOCK 收银台路由已注册",
                    st2 == 401, f"HTTP={st2}（401=路由在、等 JWT）")
    return ok


def s1_channel(plat: Client, rep: Report, plat_pass: str):
    rep.step("S1 平台管理员登录 → 支付配置：epay 通道 enabled（mock 模式）")
    st, payload, _ = plat.post("/api/auth/login", desc="平台管理员登录",
                               body={"username": PLAT_USER, "password": plat_pass})
    ok = st == 200 and code_of(payload) == 200 and data_of(payload).get("token")
    rep.assert_("S1.1 登录成功并拿到 JWT", bool(ok), f"HTTP={st} code={code_of(payload)}")
    if not ok:
        return None
    token = data_of(payload)["token"]

    st, payload, _ = plat.get("/platform/payments/channels", desc="支付渠道列表", token=token)
    ok = st == 200 and code_of(payload) == 200
    rep.assert_("S1.2 GET /platform/payments/channels code=200", ok, f"HTTP={st} code={code_of(payload)}")
    epay = None
    if ok:
        for c in data_of(payload).get("list", []):
            if c.get("channel") == "epay":
                epay = c
        rep.assert_("S1.3 列表含 epay 通道", epay is not None,
                    f"channels={[c.get('channel') for c in data_of(payload).get('list', [])]}")
        if epay is not None:
            rep.assert_("S1.4 epay.mock == true（平台处于 EPAY_MOCK 模式）",
                        epay.get("mock") is True, f"mock={epay.get('mock')}")
            if epay.get("enabled") is not True:
                rep.note("epay.enabled=false → 下发 PUT 打开（mock 模式同样要 enabled 才走在线支付）")
                st, payload, _ = plat.put("/platform/payments/channels/epay", token=token,
                                          desc="启用 epay 通道",
                                          body={"name": epay.get("name") or "易支付", "enabled": True,
                                                "remark": "epay_mock_e2e 启用"})
                rep.assert_("S1.5 PUT 启用 epay 通道 code=200",
                            st == 200 and code_of(payload) == 200, f"HTTP={st} code={code_of(payload)}")
            else:
                rep.assert_("S1.5 epay 通道已 enabled（无需下发）", True, "enabled=true")
    return token


def s2_register(user: Client, rep: Report, run: str):
    rep.step("S2 SaaS 注册租户（POST /api/tenant/register）→ 订阅状态 = trial")
    st, payload, _ = user.get("/api/tenant/regulatory-notice", desc="监管告知（取协议版本）")
    agreement = ""
    if st == 200 and code_of(payload) == 0:
        agreement = data_of(payload).get("agreement_version") or ""

    username = "eb_" + run
    body = {
        "name": "E2E计费" + run,
        "username": username,
        "email": f"{username}@example.com",
        "password": secrets.token_hex(7) + "7a",
        "company_name": "E2E计费" + run,
        "legal_person": "E2E负责人",
        "phone": "13800138000",
        "license_no": "E2E-LIC-" + run,
        "address": "E2E测试地址",
        "regulatory_ack": True,
        "agreement_version": agreement,
    }
    st, payload, _ = user.post("/api/tenant/register", desc="自助开户（SaaS）", body=body)
    if code_of(payload) == 4105:
        # 服务层注册限流（registerLimiter）：等一档再试一次
        rep.note("命中注册限流 4105，30s 后重试一次")
        time.sleep(30)
        st, payload, _ = user.post("/api/tenant/register", desc="自助开户（重试）", body=body)
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S2.1 注册成功 code=0 且返回 token", ok and bool(data_of(payload).get("token")),
                f"HTTP={st} code={code_of(payload)} msg={(payload or {}).get('message') if isinstance(payload, dict) else ''}")
    if not ok:
        return None, None
    token = data_of(payload)["token"]
    rep.note(f"tenant_code={data_of(payload).get('tenant_code')} username={username}")

    # 注册后先 sync 一次：订阅镜像是 sync 落库的（003 T022 · 修订 #4）
    st, payload, _ = user.post("/api/modules/sync", desc="同步（写订阅镜像）", token=token)
    rep.assert_("S2.2 POST /api/modules/sync code=0",
                st == 200 and code_of(payload) == 0, f"HTTP={st} code={code_of(payload)}")

    st, payload, _ = user.get("/api/subscription/status", desc="订阅状态", token=token)
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S2.3 GET /api/subscription/status code=0", ok, f"HTTP={st} code={code_of(payload)}")
    status = data_of(payload).get("status") if ok else None
    if ok and status == "trial":
        rep.assert_("S2.4 订阅状态 == trial", True, f"status={status}")
    elif ok and status in ("active", "expired"):
        rep.assert_("S2.4 订阅状态 == trial（非首次运行降级）", True,
                    f"status={status}：本部署商户已注册过，trial 只在首次开户出现；流程继续")
        rep.degrade(f"S2 trial 断言降级：本轮 status={status}（重复执行同一双栈时的预期形态）")
    else:
        rep.assert_("S2.4 订阅状态 == trial", False, f"status={status!r} data={data_of(payload)}")
    return token, username


def s3_order(user: Client, rep: Report, token: str, run: str):
    rep.step(f"S3 模块市场下单 {MODULE_KEY} → payment 段 pay_url(mock)")
    st, payload, _ = user.get("/api/module-market/list", desc="市场列表（公开面）")
    price = None
    if st == 200 and code_of(payload) == 0:
        for it in data_of(payload).get("list", []):
            if it.get("module_key") == MODULE_KEY:
                price = it.get("price_cents")
    rep.assert_("S3.1 市场列表含目标模块", price is not None, f"price_cents={price}")
    if price is None:
        return None
    rep.assert_("S3.2 目标模块定价 > 0（0 元单直通 fulfilled，测不到 mock 收银台）",
                bool(price and price > 0), f"price_cents={price}")

    body = {
        "module_keys": [MODULE_KEY],
        "contact_name": "E2E计费",
        "contact_phone": "13800138000",
        "contact_email": f"eb_{run}@example.com",
        "remark": "epay_mock_e2e 下单",
        "client_order_no": "EB" + run,
    }
    st, payload, _ = user.post("/api/module-market/order", desc="下单", token=token, body=body)
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S3.3 下单 code=0", ok, f"HTTP={st} code={code_of(payload)}")
    if not ok:
        return None
    d = data_of(payload)
    order_no = d.get("order_no")
    rep.note(f"order_no={order_no} status={d.get('status')} total={d.get('total_amount_cents')}")
    pay = d.get("payment") or {}
    mock_url = f"{PLAT_BASE}/platform/payments/mock/confirm/{order_no}"
    rep.assert_("S3.4 payment 段非空且 mock=true", bool(pay) and pay.get("mock") is True,
                f"payment={json.dumps(mask(pay), ensure_ascii=False)[:200]}")
    rep.assert_("S3.5 payment.pay_url 指向 mock 收银台",
                bool(pay.get("pay_url")) and (
                    pay.get("pay_url").endswith(f"/platform/payments/mock/confirm/{order_no}")
                    or pay.get("pay_url") == mock_url),
                f"pay_url={pay.get('pay_url')!r} 期望后缀=/platform/payments/mock/confirm/{order_no}")
    rep.assert_("S3.6 payment.method == POST", pay.get("method") == "POST", f"method={pay.get('method')!r}")
    return order_no


def s4_pay_and_activate(user: Client, plat: Client, rep: Report, token, plat_token, order_no):
    rep.step("S4 平台 mock confirm → 轮询 pay-status → sync → /api/modules/my 已激活")
    st, payload, text = plat.post(f"/platform/payments/mock/confirm/{order_no}",
                                  desc="mock 收银台确认支付", token=plat_token)
    ok = st == 200 and ((text or "").strip() == "success" or payload == "success" or
                        (isinstance(payload, dict) and code_of(payload) in (0, 200)))
    rep.assert_("S4.1 mock confirm 成功（纯文本 success，与真实回调同一条 processNotify）",
                ok, f"HTTP={st} resp={str(payload or text)[:120]}")

    status, paid = None, None
    for i in range(30):
        st, payload, _ = user.get(f"/api/module-market/orders/{order_no}/pay-status",
                                  desc="支付状态直查（轮询）", token=token)
        if st == 200 and code_of(payload) == 0:
            d = data_of(payload)
            status, paid = d.get("status"), d.get("paid")
            if paid is True or status in ("paid", "fulfilled"):
                break
        time.sleep(0 if _dry_mode else 1)
    rep.assert_("S4.2 pay-status 轮询到 paid/fulfilled",
                status in ("paid", "fulfilled"), f"status={status!r} paid={paid!r}")
    rep.assert_("S4.3 pay-status.paid 与 status 同向",
                paid is (status in ("paid", "fulfilled")), f"status={status!r} paid={paid!r} — T029 端点口径")

    st, payload, _ = user.post("/api/modules/sync", desc="sync（拉模块+订单态+订阅镜像）", token=token)
    rep.assert_("S4.4 POST /api/modules/sync code=0",
                st == 200 and code_of(payload) == 0, f"HTTP={st} code={code_of(payload)}")

    st, payload, _ = user.get("/api/modules/my", desc="我的模块", token=token)
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S4.5 GET /api/modules/my code=0", ok, f"HTTP={st} code={code_of(payload)}")
    owned = None
    if ok:
        for it in data_of(payload).get("list", []):
            if it.get("module_key") == MODULE_KEY:
                owned = it.get("owned")
    rep.assert_("S4.6 browser-automation 已激活（owned=true）", owned is True, f"owned={owned!r}")


def s5_expire(user: Client, rep: Report, token, dry: bool, username: str = ""):
    rep.step("S5 SQL 把订阅 period 调过期 → 业务接口 4305 / 白名单正常")
    out = psql(EXPIRE_SQL_TPL % (username or "eb_unknown"), dry, rep)
    rep.assert_("S5.1 镜像行已更新（UPDATE ... RETURNING id 有输出）",
                out not in ("", None), f"stdout={out!r}")
    if out in ("", None):
        # 行不存在 = 从未 sync 到订阅段：按本轮租户插一行（同样属测试后门）
        ins = ("INSERT INTO tenant_subscription_mirror (status, current_period_end, trial_expires_at,"
               " price_cents, fetched_at, created_at, updated_at, tenant_id)"
               " SELECT 'expired', now() - interval '2 days', now() - interval '3 days',"
               " 19900, now(), now(), now(), tenant_id FROM system_users WHERE username = '%s'"
               " ON CONFLICT DO NOTHING RETURNING id;") % (username or "eb_unknown")
        out2 = psql(ins, dry, rep)
        rep.assert_("S5.1b 镜像行不存在时插入过期行", out2 not in ("", None), f"stdout={out2!r}")

    st, payload, _ = user.get("/api/subscription/status", desc="订阅状态（锁态）", token=token)
    d = data_of(payload) if st == 200 and code_of(payload) == 0 else {}
    rep.assert_("S5.2 status == expired", d.get("status") == "expired", f"status={d.get('status')!r}")
    rep.assert_("S5.3 locked == true", d.get("locked") is True, f"locked={d.get('locked')!r}")

    # 业务面探针：命中 4305 才算闸生效
    hit = False
    for path in BIZ_PROBES:
        st, payload, _ = user.get(path, desc="业务面探针（应被订阅闸拦）", token=token)
        code = code_of(payload)
        if code == 4305:
            hit = True
            rep.assert_(f"S5.4 {path} → HTTP {st} + code=4305（订阅闸拦截）", True, f"msg={payload.get('message')!r}")
            break
        rep.note(f"{path} → HTTP {st} code={code}（非 4305，换下一候选）")
    if not hit:
        rep.assert_("S5.4 业务面探针至少一条返回 4305", False,
                    f"candidates={BIZ_PROBES} —— 若全部 403/401，说明该账号角色在 RoutePolicy 层先被拒（换探针路径）")

    # 白名单面：订阅状态 / 市场列表 / 支付状态 / sync / 我的模块 在锁态下必须正常
    for path, desc in [("/api/subscription/status", "订阅状态（墙页依赖）"),
                       ("/api/module-market/list", "市场列表（公开橱窗）")]:
        st, payload, _ = user.get(path, desc=desc + "（白名单）",
                                  token=token if "subscription" in path else None)
        rep.assert_(f"S5.5 白名单 {path} 仍 code=0",
                    st == 200 and code_of(payload) == 0, f"HTTP={st} code={code_of(payload)}")


def s6_renew(user: Client, plat: Client, rep: Report, token, plat_token, dry: bool):
    rep.step("S6 订阅续费下单 → mock confirm → 用户端 sync → 订阅 active、业务接口恢复")
    st, payload, _ = user.post("/api/subscription/renew", desc="续费下单（代理平台 subscription 单）",
                               token=token, body={
                                   "client_order_no": "SR" + uniq("", 8),
                                   "contact_name": "E2E计费", "contact_phone": "13800138000",
                                   "contact_email": "eb@example.com", "remark": "epay_mock_e2e 续费"})
    ok = st == 200 and code_of(payload) == 0
    rep.assert_("S6.1 续费下单 code=0（expired 态放行：续费是恢复通道）", ok,
                f"HTTP={st} code={code_of(payload)}")
    if not ok:
        return
    d = data_of(payload)
    sub_no = d.get("order_no")
    pay = d.get("payment") or {}
    rep.note(f"subscription order_no={sub_no} status={d.get('status')} total={d.get('total_amount_cents')}")

    if not pay:
        rep.assert_("S6.2 续费单 payment 段（0 元单可为空=免支付直通）", d.get("status") == "fulfilled",
                    f"payment 为空且 status={d.get('status')!r}；若非 fulfilled 说明通道未配置")
    else:
        rep.assert_("S6.2 续费单 payment.pay_url 指向 mock 收银台",
                    bool(pay.get("pay_url")) and pay.get("pay_url").endswith(
                        f"/platform/payments/mock/confirm/{sub_no}"),
                    f"pay_url={pay.get('pay_url')!r}")
        st, payload, text = plat.post(f"/platform/payments/mock/confirm/{sub_no}",
                                      desc="mock 确认续费单", token=plat_token)
        rep.assert_("S6.3 mock confirm 续费单成功",
                    st == 200 and ((text or "").strip() == "success" or payload == "success" or
                                   (isinstance(payload, dict) and code_of(payload) in (0, 200))),
                    f"HTTP={st} resp={str(payload or text)[:120]}")
        st, payload, _ = plat.get(f"/platform/orders/{sub_no}", desc="平台订单终态", token=plat_token)
        # 平台端响应形状为 {"code":200,"data":{"order":{...}}}，status 在 data.order 下
        ord_status = data_of(payload).get("status")
        if ord_status is None and isinstance(data_of(payload).get("order"), dict):
            ord_status = data_of(payload)["order"].get("status")
        rep.assert_("S6.4 平台订单 paid/fulfilled",
                    st == 200 and code_of(payload) == 200 and ord_status in ("paid", "fulfilled"),
                    f"HTTP={st} status={ord_status!r}")

    st, payload, _ = user.post("/api/modules/sync", desc="sync（刷新订阅镜像）", token=token)
    rep.assert_("S6.5 POST /api/modules/sync code=0",
                st == 200 and code_of(payload) == 0, f"HTTP={st} code={code_of(payload)}")

    st, payload, _ = user.get("/api/subscription/status", desc="订阅状态（恢复后）", token=token)
    d = data_of(payload) if st == 200 and code_of(payload) == 0 else {}
    rep.assert_("S6.6 status == active", d.get("status") == "active", f"status={d.get('status')!r} data={d}")
    rep.assert_("S6.7 locked == false", d.get("locked") is False, f"locked={d.get('locked')!r}")

    # 业务面恢复
    st, payload, _ = user.get(BIZ_PROBES[0], desc="业务面探针（恢复）", token=token)
    code = code_of(payload)
    rep.assert_("S6.8 业务接口不再 4305（恢复）", code not in (4305,) and st == 200,
                f"{BIZ_PROBES[0]} → HTTP={st} code={code}")
    if code in (403, 401):
        rep.degrade(f"S6.8 业务探针 {BIZ_PROBES[0]} 返回 {code}（角色能力面先于订阅闸），"
                    "订阅恢复已由 status/locked 证实，业务面探针沿用 S5 的同一批候选")


def s7_t029_trio(user: Client, rep: Report, token, order_no):
    rep.step("S7 T029 三端点复核（api_verify 口径：edition/cloud_url · 订阅状态 · pay-status）")
    # ① GET /api/public/config
    st, payload, _ = user.get("/api/public/config", desc="公开配置")
    d = data_of(payload) if st == 200 and code_of(payload) == 0 else {}
    rep.assert_("S7.1 /api/public/config code=0", st == 200 and code_of(payload) == 0, f"HTTP={st}")
    rep.assert_("S7.2 data.edition ∈ {opensource,saas}",
                d.get("edition") in ("opensource", "saas"), f"edition={d.get('edition')!r}")
    rep.assert_("S7.3 data 带 cloud_url（003 T004 云端入口常量）",
                "cloud_url" in d, f"cloud_url={d.get('cloud_url')!r}")
    # ② GET /api/subscription/status
    st, payload, _ = user.get("/api/subscription/status", desc="订阅状态", token=token)
    d = data_of(payload) if st == 200 and code_of(payload) == 0 else {}
    rep.assert_("S7.4 /api/subscription/status code=0", st == 200 and code_of(payload) == 0, f"HTTP={st}")
    rep.assert_("S7.5 status ∈ {trial,active,expired,unknown}",
                d.get("status") in USER_STATUS_OK.split(","), f"status={d.get('status')!r}")
    rep.assert_("S7.6 locked 与 status=expired 同向",
                bool(d.get("locked")) is (d.get("status") == "expired"),
                f"status={d.get('status')!r} locked={d.get('locked')!r}")
    rep.assert_("S7.7 no_data / stale 为布尔", isinstance(d.get("no_data"), bool) and isinstance(d.get("stale"), bool),
                f"no_data={d.get('no_data')!r} stale={d.get('stale')!r}")
    # ③ GET /api/module-market/orders/:no/pay-status
    if not order_no:
        rep.assert_("S7.8 pay-status（需要 S3/S4 的 order_no）", False, "本轮未拿到 order_no")
        return
    st, payload, _ = user.get(f"/api/module-market/orders/{order_no}/pay-status",
                              desc="支付状态直查", token=token)
    d = data_of(payload) if st == 200 and code_of(payload) == 0 else {}
    rep.assert_("S7.9 pay-status code=0 且回显 order_no",
                st == 200 and code_of(payload) == 0 and d.get("order_no") == order_no,
                f"HTTP={st} order_no={d.get('order_no')!r} 期望 {order_no!r}")
    rep.assert_("S7.10 status ∈ 订单状态枚举", d.get("status") in
                ("pending_payment", "paid", "fulfilled", "cancelled", "rejected"),
                f"status={d.get('status')!r}")
    rep.assert_("S7.11 paid 与 status 同向",
                bool(d.get("paid")) is (d.get("status") in ("paid", "fulfilled")),
                f"status={d.get('status')!r} paid={d.get('paid')!r}")


def restore_only(user: Client, rep: Report, token, dry: bool, username: str = ""):
    rep.step("--restore-only：把订阅 period 恢复成未来（S5 之后失败的救急）")
    out = psql(RESTORE_SQL_TPL % (username or "eb_unknown"), dry, rep)
    rep.assert_("恢复 UPDATE 有输出", out not in ("", None), f"stdout={out!r}")
    if token:
        st, payload, _ = user.get("/api/subscription/status", desc="恢复后状态", token=token)
        d = data_of(payload) if st == 200 and code_of(payload) == 0 else {}
        rep.assert_("status 恢复为 active", d.get("status") == "active", f"status={d.get('status')!r}")


def cleanup_user(rep: Report, run: str, dry: bool):
    rep.step("--cleanup：删除本轮注册的测试租户（SQL 测试后门；平台侧 merchant/订单保留）")
    sql = ("DELETE FROM system_users WHERE username = 'eb_" + run + "'; "
           "DELETE FROM tenants WHERE code = (SELECT code FROM tenants WHERE name = 'E2E计费" + run + "' "
           "ORDER BY id DESC LIMIT 1);")
    out = psql(sql, dry, rep)
    rep.assert_("清理 SQL 执行", out is not None, f"stdout={str(out)[:120]!r}")


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------
_dry_mode = False


def main():
    global _dry_mode
    ap = argparse.ArgumentParser(description="003 计费二期 mock 收银台端到端回归（可重复执行）")
    ap.add_argument("--dry-run", action="store_true",
                    help="不发网络请求：用代码同源的响应夹具走完整流程，校验请求构造与断言逻辑")
    ap.add_argument("--preflight", action="store_true", help="只做 S0 前置探测后退出")
    ap.add_argument("--skip-expire", action="store_true", help="跳过 S5（不改订阅 period）")
    ap.add_argument("--skip-renew", action="store_true", help="跳过 S6")
    ap.add_argument("--restore-only", action="store_true", help="只执行订阅 period 恢复")
    ap.add_argument("--cleanup", action="store_true", help="清理本轮注册的测试租户")
    ap.add_argument("--platform-pass", default=os.environ.get("EPAY_E2E_PLATFORM_PASS", ""),
                    help="平台管理员口令（建议用环境变量 EPAY_E2E_PLATFORM_PASS）")
    args = ap.parse_args()

    _dry_mode = args.dry_run
    run = time.strftime("%m%d%H%M") + "".join(secrets.choice(string.digits) for _ in range(3))
    rep = Report()
    sim = Sim(run)
    rep.sim = sim
    user = Client(USER_BASE, rep, args.dry_run, sim)
    plat = Client(PLAT_BASE, rep, args.dry_run, sim)

    print("=" * 70)
    print(f"epay_mock_e2e  run={run}  mode={'DRY-RUN' if args.dry_run else 'LIVE'}")
    print(f"  用户端 {USER_BASE}   平台端 {PLAT_BASE}   模块 {MODULE_KEY}")
    print("=" * 70)

    # 凭据只在真的要以管理员身份打平台时才必填（preflight 探测是匿名的）
    need_admin = (not args.dry_run and not args.preflight
                  and not args.restore_only and not args.cleanup)
    if need_admin and not args.platform_pass:
        if not args.platform_pass:
            sys.stderr.write(
                "❌ 缺少平台管理员口令：export EPAY_E2E_PLATFORM_PASS=<hivemtk-platform/.env 的 PLATFORM_ADMIN_PASSWORD>\n"
                "   （脚本不落任何明文口令；只想验证脚本可用 --dry-run）\n")
            sys.exit(3)

    if not s0_preflight(user, plat, rep, args.dry_run):
        rep.finish()
        print("前置探测未通过：先按脚本头部说明起好双栈（含 EPAY_MOCK=true）。")
        sys.exit(1)
    if args.preflight:
        sys.exit(rep.finish())

    order_no = None
    user_token = None
    username = None

    if not args.restore_only:
        plat_token = s1_channel(plat, rep, args.platform_pass or "DRY")
        if not plat_token:
            sys.exit(rep.finish())
        user_token, username = s2_register(user, rep, run)
        if not user_token:
            sys.exit(rep.finish())
        order_no = s3_order(user, rep, user_token, run)
        if order_no:
            s4_pay_and_activate(user, plat, rep, user_token, plat_token, order_no)
        if not args.skip_expire:
            s5_expire(user, rep, user_token, args.dry_run, username)
            if not args.skip_renew:
                s6_renew(user, plat, rep, user_token, plat_token, args.dry_run)
            else:
                rep.note("已跳过 S6：本轮把订阅留在 expired，善后请跑 --restore-only"
                         "（或在页面上走完续费），否则下次访问业务面仍是 4305")
        s7_t029_trio(user, rep, user_token, order_no)

    if args.restore_only:
        restore_only(user, rep, None, args.dry_run, username)
    if args.cleanup:
        cleanup_user(rep, run, args.dry_run)

    rc = rep.finish()

    # ---- dry-run 自检：请求构造的硬口径 ----
    if args.dry_run:
        print("\n【dry-run 自检】请求构造")
        bad = [f"{m} {u}" for m, u in rep.sent
               if m != "SQL" and not (u.startswith(USER_BASE) or u.startswith(PLAT_BASE))]
        print(f"  · 共构造 {len(rep.sent)} 个请求，越界 base 0 个" if not bad else f"  · 越界: {bad}")
        leaks = [f"{m} {u}" for m, u in rep.sent if "password=" in u or "pass" in u.lower()]
        print(f"  · URL 中无口令泄漏" if not leaks else f"  · ⚠ URL 含口令字段: {leaks}")
        if bad or leaks:
            rc = 1
        print("  · 说明：dry-run 用的响应夹具形状取自两仓 response 封装与控制器源码；"
              "真实验收仍以 live 跑法为准。")
    sys.exit(rc)


if __name__ == "__main__":
    main()
