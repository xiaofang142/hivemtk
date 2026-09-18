#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
api_verify_full.py — API 三端验收脚本（CLAUDE.md 验收标准实现）

每个端点验证四维:
  【入参】前端发送字段
  【返回】API实际响应
  【预期】code=0 + 字段完整
  【DB】  写操作后SQL确认

用法:
  python3 scripts/api_verify_full.py chain   # 核心业务链路: 消息入库→AI→出库→ack 全链
  python3 scripts/api_verify_full.py api     # API 全量矩阵(未实现时打印TODO)
  python3 scripts/api_verify_full.py chain --keep  # 保留测试数据便于排查

凭证（必填，脚本内无任何明文口令）:
  HIVEMTK_DB_PASSWORD   DB 口令（也可用 POSTGRES_PASSWORD 代替）
  HIVEMTK_ADMIN_PASS    e2e_admin 登录口令
  HIVEMTK_BRIDGE_TOKEN  可选；缺省时回退读 DB system_config_kv.bridge_ingest_token
缺失即以退出码 3 终止并打印所需变量名。
"""
import argparse
import json
import os
import sys
import time
import uuid

import psycopg2
import psycopg2.extras
import requests

# 主机/端口/库名等非敏感项保留默认值；口令一律仅从环境变量取，脚本内不落任何明文。
BASE = os.environ.get("HIVEMTK_BASE", "http://localhost:8204")

_MISSING_ENV = []


def _secret(name, *aliases):
    """读取敏感变量；缺失则登记并在导入末尾统一退出（不回落到任何内置默认值）。"""
    for n in (name,) + aliases:
        v = os.environ.get(n)
        if v:
            return v
    _MISSING_ENV.append((name, aliases))
    return ""


DB = dict(
    host=os.environ.get("HIVEMTK_DB_HOST", "127.0.0.1"),
    port=int(os.environ.get("HIVEMTK_DB_PORT", "8232")),
    user=os.environ.get("HIVEMTK_DB_USER", "admin"),
    password=_secret("HIVEMTK_DB_PASSWORD", "POSTGRES_PASSWORD"),
    dbname=os.environ.get("HIVEMTK_DB_NAME", "user_db"),
)
ADMIN_USER = os.environ.get("HIVEMTK_ADMIN", "e2e_admin")
ADMIN_PASS = _secret("HIVEMTK_ADMIN_PASS")
# 规则2（问题根治）：默认使用专用测试账号 e2e_admin，与人工登录账号解耦；
# 测试既不得依赖、也不得修改人工账号，其口令只经环境变量注入。
# bridge 入口守卫凭证：优先环境变量，回退 DB system_config_kv（管理端轮换源）
BRIDGE_TOKEN = os.environ.get("HIVEMTK_BRIDGE_TOKEN", "")

if _MISSING_ENV:
    _need = ", ".join(n + ("（或 " + " / ".join(a) + "）" if a else "") for n, a in _MISSING_ENV)
    sys.stderr.write(
        "❌ 缺少必需环境变量：%s\n"
        "本脚本不落任何明文凭证（口令仅从环境变量注入）。请先导出再运行，例如：\n"
        "  set -a && . hivemtk/.env && set +a\n"
        '  export HIVEMTK_DB_PASSWORD="$POSTGRES_PASSWORD"\n'
        "  export HIVEMTK_ADMIN_PASS=<e2e_admin 的登录口令>\n"
        "（由 scripts/auto_detect.py 调用时，父进程会自动注入上述变量。）\n" % _need
    )
    sys.exit(3)

REPORT = []


def section(title):
    print(f"\n{'=' * 62}\n【{title}】\n{'=' * 62}")


def check(name, ok, detail=""):
    mark = "PASS" if ok else "FAIL"
    line = f"[{mark}] {name}" + (f" — {detail}" if detail else "")
    print(line)
    REPORT.append((name, ok, detail))
    return ok


def db():
    return psycopg2.connect(**DB)


def q1(sql, params=()):
    conn = db()
    try:
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute(sql, params)
            return cur.fetchone()
    finally:
        conn.close()


def qall(sql, params=()):
    conn = db()
    try:
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute(sql, params)
            return cur.fetchall()
    finally:
        conn.close()


def qexec(sql, params=()):
    conn = db()
    try:
        with conn.cursor() as cur:
            cur.execute(sql, params)
            conn.commit()
    finally:
        conn.close()


class APIClient:
    def __init__(self, token=None, bridge_token=None):
        self.s = requests.Session()
        self.token = token
        if token:
            self.s.headers["Authorization"] = f"Bearer {token}"
        if bridge_token:
            self.s.headers["X-Bridge-Token"] = bridge_token

    def req(self, method, path, desc, expect_code=None, ok_status=(200,), **kw):
        """【入参】【返回】【预期】 三维打印; 返回 (resp_json, ok)"""
        url = BASE + path
        body = kw.pop("json", None)
        params = kw.pop("params", None)
        print(f"\n--- {desc} ---")
        print(f"【入参】{method} {path}" + (f" params={params}" if params else "") + (f" body={json.dumps(body, ensure_ascii=False)[:200]}" if body else ""))
        try:
            r = self.s.request(method, url, json=body, params=params, timeout=kw.pop("timeout", 30), **kw)
        except requests.RequestException as e:
            check(f"{desc}", False, f"请求异常: {e}")
            return None, False
        print(f"【返回】HTTP {r.status_code} {r.text[:300]}")
        code_ok = r.status_code in ok_status
        contract_ok = True
        try:
            j = r.json()
        except ValueError:
            j = None
            contract_ok = False
        if j is not None and expect_code is not None and isinstance(j, dict):
            if "code" in j:
                contract_ok = contract_ok and j.get("code") == expect_code
        ok = check(f"{desc}", code_ok and contract_ok,
                   "" if code_ok and contract_ok else f"HTTP={r.status_code} 期望code={expect_code}")
        return j, ok


def login_admin():
    c = APIClient()
    j, ok = c.req("POST", "/api/auth/login", "管理员登录", expect_code=0,
                  json={"username": ADMIN_USER, "password": ADMIN_PASS})
    if not ok or not j:
        sys.exit(2)
    return j["data"]["token"]


# ---------------------------------------------------------------------------
# 核心业务链路: 消息入库 → 幂等/回声闸门 → message_hub → AI处理 → outbox → ack 送达
# ---------------------------------------------------------------------------
def run_chain(keep=False):
    section("核心业务链路 E2E：消息入库→AI处理→出库→ack 全流程")
    run_id = uuid.uuid4().hex[:8]
    channel = "douyin"
    account_id = f"acct-e2e-{run_id}"
    conversation_id = f"conv-e2e-{run_id}"
    sender_id = f"cust-e2e-{run_id}"
    msg_content = f"你好，我想了解一下HiveMtk的渠道接入方案 e2e-{run_id}"
    event_id = f"evt-e2e-{run_id}-1"

    # bridge 通道守卫凭证：DB system_config_kv 为主源（与 guard 中间件一致）
    global BRIDGE_TOKEN
    if not BRIDGE_TOKEN:
        row = q1("SELECT value FROM system_config_kv WHERE key='bridge_ingest_token'")
        BRIDGE_TOKEN = (row or {}).get("value", "") if row else ""
    if not BRIDGE_TOKEN:
        check("S0 bridge token 可用", False, "system_config_kv 无 bridge_ingest_token 且未设 HIVEMTK_BRIDGE_TOKEN")
        return finish(keep)
    bridge = APIClient(bridge_token=BRIDGE_TOKEN)  # bridge 通道无需 JWT
    admin_token = login_admin()
    adm = APIClient(admin_token)

    # ---- Step1 消息入库 (ingest) ----
    section("Step1 消息入库: POST /api/bridge/ingest")
    body = {
        "v": 1, "channel": channel, "account_id": account_id,
        "conversation_id": conversation_id,
        "messages": [{
            "event_id": event_id, "conversation_id": conversation_id,
            "sender_id": sender_id, "sender_name": "E2E客户",
            "sender_type": "customer", "msg_type": "text",
            "content": msg_content, "timestamp": int(time.time() * 1000),
        }],
        "expect_reply": True, "timeout_ms": 5000,
    }
    j, ok = bridge.req("POST", "/api/bridge/ingest", "S1 客户消息上报", expect_code=200, json=body)
    if not ok or not j:
        return finish(keep)
    ing = (j.get("ingested") or [{}])[0]
    check("S1.1 accepted=true", ing.get("accepted") is True, f"result={ing}")
    check("S1.2 duplicate=false", ing.get("duplicate") is False, f"result={ing}")

    # ---- Step2 DB断言: message_hub 入库 ----
    section("Step2 DB断言: message_hub 入站行")
    hub = q1("SELECT * FROM message_hub WHERE msg_id=%s AND platform=%s", (event_id, channel))
    check("S2.1 inbound行存在", hub is not None, f"msg_id={event_id}")
    if hub:
        check("S2.2 direction=inbound", hub["direction"] == "inbound", f"got={hub['direction']}")
        check("S2.3 content完整保留", hub["content"] == msg_content, f"len={len(hub['content'] or '')}")
        check("S2.4 conversation_id正确", hub["conversation_id"] == conversation_id,
              f"got={hub['conversation_id']}")
        check("S2.5 account_id正确", hub["account_id"] == account_id, f"got={hub['account_id']}")

    # ---- Step3 DB断言: 客户与会话创建 ----
    section("Step3 DB断言: 客户档案 + 会话创建")
    cust = q1("SELECT * FROM customers WHERE douyin_open_id=%s", (sender_id,))
    check("S3.1 客户档案创建(OneID归一)", cust is not None, f"douyin_open_id={sender_id}")
    sess = q1("SELECT * FROM customer_sessions WHERE session_id=%s", (f"sess_{channel}_{account_id}_{sender_id}",))
    check("S3.2 会话创建", sess is not None, f"session_id=sess_{channel}_{account_id}_{sender_id}")

    # ---- Step4 幂等闸门: 重复上报同 event_id ----
    section("Step4 幂等闸门: 重复 event_id 上报")
    j2, _ = bridge.req("POST", "/api/bridge/ingest", "S4 重复上报", expect_code=200, json=body)
    ing2 = (j2.get("ingested") or [{}])[0] if j2 else {}
    check("S4.1 duplicate=true", ing2.get("duplicate") is True, f"result={ing2}")
    cnt = q1("SELECT count(*) c FROM message_hub WHERE msg_id=%s AND platform=%s", (event_id, channel))["c"]
    check("S4.2 无重复入库", cnt == 1, f"rows={cnt}")

    # ---- Step5 AI 链路: 等待回复产生 ----
    section("Step5 AI 链路: 轮询等待 AI 回复落 outbox (最多90s)")
    ai_handled = ing.get("ai_handled")
    print(f"【预期】ingest 返回 ai_handled={ai_handled}; 等待 outbound 回复落库...")
    # 23:00-07:00 免打扰时段（webhook_outbound.go:192 isAIReplyQuietHours）按设计不把 AI
    # 回复写进 message_hub，而是入 reach_delayed_outbound 等次日到期。此处在轮询里同时探测
    # 延迟队列，命中则把 send_at 提前到期，交给服务自身的 30s ticker 走**真实**重放路径，
    # 使 S5.2~S7 在任意时刻都执行同一条落库断言（不跳过、不放宽、不 mock）。
    print("【预期】免打扰时段(23-07)命中时：回复先入 reach_delayed_outbound，再到期重放入 message_hub")
    reply = None
    deferred = None
    deadline = time.time() + 90
    while time.time() < deadline:
        rows = qall(
            "SELECT * FROM message_hub WHERE platform=%s AND account_id=%s AND conversation_id=%s "
            "AND direction='outbound' ORDER BY id DESC LIMIT 5", (channel, account_id, conversation_id))
        if rows:
            reply = rows[0]
            break
        if deferred is None:
            d = q1("SELECT id, send_at FROM reach_delayed_outbound "
                   "WHERE platform=%s AND account_id=%s AND conversation_id=%s AND status='pending' "
                   "ORDER BY id DESC LIMIT 1", (channel, account_id, conversation_id))
            if d:
                deferred = d
                qexec("UPDATE reach_delayed_outbound SET send_at = now() - interval '1 second', "
                      "updated_at = now() WHERE id=%s AND status='pending'", (d["id"],))
                print(f"【分支】命中免打扰：回复已入延迟队列 id={d['id']} 原 send_at={d['send_at']}，"
                      "已提前到期，等待服务重放")
        time.sleep(3)
    if reply is None:
        s51_detail = ("90s内未产生回复"
                      + (f"（已入延迟队列 id={deferred['id']}，但到期重放未落 message_hub → 重放消费者缺陷）"
                         if deferred else "（且未进延迟队列 → AI 链路真断）"))
    else:
        s51_detail = (f"msg_id={reply['msg_id']} content={reply['content'][:60]!r}"
                      + ("[经延迟队列重放]" if deferred else "[直发]"))
    check("S5.1 AI回复产生(outbound落库)", reply is not None, s51_detail)
    if reply:
        check("S5.2 is_ai_reply标记", reply["is_ai_reply"] is True, f"got={reply['is_ai_reply']}")
        check("S5.3 回复内容非空且非模板垃圾", len(reply["content"] or "") >= 2 and "抱歉" not in (reply["content"] or "")[:6],
              f"content[:80]={reply['content'][:80]!r}")
        check("S5.4 回复会话绑定正确", reply["conversation_id"] == conversation_id)
        check("S5.5 trace_id可观测", bool(reply["trace_id"]), f"trace_id={reply['trace_id']}")

    # ---- Step6 出库: 扩展端拉取 outbox ----
    section("Step6 出库: GET /api/bridge/outbox")
    j6, ok6 = bridge.req("GET", "/api/bridge/outbox", "S6 拉取待发消息", expect_code=200,
                         params={"channel": channel, "account_id": account_id, "limit": 50})
    msgs = (j6 or {}).get("messages") or []
    check("S6.1 outbox包含AI回复", any(m["msg_id"] == reply["msg_id"] for m in msgs) if reply else False,
          f"outbox_count={len(msgs)}")
    if reply:
        target = next((m for m in msgs if m["msg_id"] == reply["msg_id"]), None)
        check("S6.2 outbox消息字段完整",
              target is not None and target.get("content") and target.get("conversation_id") == conversation_id,
              f"item={json.dumps(target, ensure_ascii=False)[:150] if target else None}")

    # ---- Step7 ack 送达确认 ----
    section("Step7 ack: POST /api/bridge/outbox/ack")
    if reply:
        ack_body = {"msg_ids": [reply["msg_id"]], "status": "delivered"}
        j7, _ = bridge.req("POST", "/api/bridge/outbox/ack", "S7 送达确认", expect_code=200,
                           params={"channel": channel, "account_id": account_id}, json=ack_body)
        check("S7.1 acked_items_count=1", (j7 or {}).get("acked_items_count") == 1, f"resp={j7}")
        # msg_id=内容hash：相同兜底内容跨运行复用同一 msg_id（多行共存是设计内，
        # 唯一索引含 conversation_id），断言必须 scope 到本运行的行
        hub2 = None
        for _ in range(6):
            hub2 = q1("SELECT status FROM message_hub WHERE msg_id=%s AND platform=%s AND account_id=%s AND conversation_id=%s",
                      (reply["msg_id"], channel, account_id, conversation_id))
            if hub2 and hub2["status"] == "delivered":
                break
            time.sleep(0.5)
        check("S7.2 DB状态翻转delivered", hub2 is not None and hub2["status"] == "delivered", f"status={hub2['status'] if hub2 else None}")

    # ---- Step8 回声拦截: 客户侧回传AI自己的回复 ----
    section("Step8 回声拦截: outbound回文不重触发AI")
    if reply:
        echo_body = {
            "v": 1, "channel": channel, "account_id": account_id,
            "conversation_id": conversation_id,
            "messages": [{
                "event_id": f"evt-e2e-{run_id}-echo",
                "conversation_id": conversation_id,
                "sender_id": sender_id, "sender_type": "customer",
                "msg_type": "text", "content": reply["content"],
                "timestamp": int(time.time() * 1000),
            }],
        }
        j8, _ = bridge.req("POST", "/api/bridge/ingest", "S8 回文上报", expect_code=200, json=echo_body)
        ing8 = (j8.get("ingested") or [{}])[0] if j8 else {}
        echo_row = q1("SELECT * FROM message_hub WHERE msg_id=%s AND platform=%s",
                      (f"evt-e2e-{run_id}-echo", channel))
        check("S8.1 回文未触发AI", ing8.get("ai_handled") is not True,
              f"ai_handled={ing8.get('ai_handled')} reason={ing8.get('reason')}")

    # ---- Step9 观测链路完整性 ----
    section("Step9 观测: trace_id 贯穿 / intent / 会话同步")
    intents = qall("SELECT * FROM intent_records WHERE session_id=%s LIMIT 3", (f"sess_{channel}_{account_id}_{sender_id}",))
    print(f"【DB】intent_records={len(intents)} (可空: 意图识别按需触发)")

    # ---- Step10 清理 ----
    section("Step10 清理测试数据")
    if not keep:
        qexec("DELETE FROM message_hub WHERE account_id=%s", (account_id,))
        # 免打扰延迟队列也须清：否则残留 pending 行会在次日 07:00 重放出无人认领的回复
        qexec("DELETE FROM reach_delayed_outbound WHERE account_id=%s", (account_id,))
        qexec("DELETE FROM customer_sessions WHERE session_id=%s", (f"sess_{channel}_{account_id}_{sender_id}",))
        if cust:
            qexec("DELETE FROM customers WHERE id=%s", (cust["id"],))
        print("已清理 ingest 链路测试数据")
    else:
        print(f"保留测试数据: account_id={account_id} conversation_id={conversation_id}")

    return finish(keep)


def finish(keep):
    section("链路验收报告")
    passed = sum(1 for _, ok, _ in REPORT if ok)
    total = len(REPORT)
    for name, ok, detail in REPORT:
        if not ok:
            print(f"  ✗ {name} — {detail}")
    print(f"\n总计: {passed}/{total} PASS ({passed * 100 // total if total else 0}%)")
    return 0 if passed == total else 1


def run_api():
    section("API 全量矩阵")
    print("TODO: 由任务5(api矩阵)填充 — 路由清单提取 + 四维验证")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cmd", choices=["chain", "api", "all"])
    ap.add_argument("--keep", action="store_true")
    args = ap.parse_args()
    rc = 0
    if args.cmd in ("chain", "all"):
        rc |= run_chain(keep=args.keep)
    if args.cmd in ("api", "all"):
        run_api()
    sys.exit(rc)


if __name__ == "__main__":
    main()
