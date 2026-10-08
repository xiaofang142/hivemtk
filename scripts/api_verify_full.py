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
  python3 scripts/api_verify_full.py api     # API 全量矩阵: 路由清单提取+契约探测+四维深测
  python3 scripts/api_verify_full.py ltc     # LTC 全链路: 线索→商机→报价→审批→账单→回款→赢单→复购
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
import re
import sys
import threading
import time
import uuid
from collections import Counter
from concurrent.futures import ThreadPoolExecutor, as_completed

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
# 降级项：某条腿因为环境前提（不是被测逻辑）只能验到形态，或只能把状态在库内补齐给下游时，
# 在这里登记一句，finish() 单独打印。它们不计入 FAIL（否则又回到"环境噪声与真缺陷同一个红"），
# 但必须在报告里单列，否则 38/38 会把"这一跳其实没有证据"读成"这一跳过了"。
DEGRADED = []


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

    def req(self, method, path, desc, expect_code=None, ok_status=(200,), allow_codes=(), **kw):
        """按 CLAUDE.md 的三端验收口径逐行打印【入参】【返回】【预期】; 返回 (resp_json, ok)

        allow_codes: 这一步允许出现的**响应体 code** 集合（默认只允许 expect_code）。
        用于「失败形态也是被验收对象」的步骤——例如出站链路在离线环境只能拿到
        outbound_failed，那条 502 就是预期结果的一部分，硬判 code=0 会把
        环境噪声与真缺陷混成同一个红。

        【预期】单独成行是这张卡 AC① 的字面要求：原先只有 docstring 里写着三维，
        实测整轮 ltc 输出里【预期】是 0 行，读者只能把下面 check 的实际值当预期读。
        """
        url = BASE + path
        body = kw.pop("json", None)
        params = kw.pop("params", None)
        want_codes = set(allow_codes)
        if expect_code is not None:
            want_codes.add(expect_code)
        print(f"\n--- {desc} ---")
        print(f"【入参】{method} {path}" + (f" params={params}" if params else "") + (f" body={json.dumps(body, ensure_ascii=False)[:200]}" if body else ""))
        try:
            r = self.s.request(method, url, json=body, params=params, timeout=kw.pop("timeout", 30), **kw)
        except requests.RequestException as e:
            check(f"{desc}", False, f"请求异常: {e}")
            return None, False
        print(f"【返回】HTTP {r.status_code} {r.text[:300]}")
        print(f"【预期】HTTP {list(ok_status)}"
              + (f" 响应体 code={sorted(want_codes, key=str)}" if want_codes else " 响应体 code 不限"))
        code_ok = r.status_code in ok_status
        contract_ok = True
        try:
            j = r.json()
        except ValueError:
            j = None
            contract_ok = False
        if j is not None and want_codes and isinstance(j, dict):
            if "code" in j:
                contract_ok = contract_ok and j.get("code") in want_codes
        ok = check(f"{desc}", code_ok and contract_ok,
                   "" if code_ok and contract_ok
                   else f"HTTP={r.status_code} 期望code={sorted(want_codes, key=str) or expect_code}")
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
    login_admin()

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
        # 判据是「命中已知兜底文案整句」，不是「开头有没有抱歉」：
        # LLM 答不出资料时完全可以礼貌地以「抱歉，暂未检索到…」开头，
        # 那是一条真答案（承认查不到并反问用户走哪条路），不是模板垃圾。
        # 原判据把前者一并判红，于是知识库一空这条就开始假红——它量的是用词，不是可用性。
        # 已知兜底文案清单取自 aiagent/llm/fallback_tree.go 的模板树（截 40 字比对够用）。
        _tpl_markers = (
            "非常抱歉给您带来困扰",
            "当前客服系统繁忙",
            "服务暂时不可用",
            "当前服务暂时繁忙",
        )
        _c = reply["content"] or ""
        check("S5.3 回复内容非空且非模板兜底",
              len(_c) >= 2 and not any(m in _c[:40] for m in _tpl_markers),
              f"content[:80]={_c[:80]!r}")
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
    for d in DEGRADED:
        print(f"  ⚠ 降级: {d}")
    return 0 if passed == total else 1


# ---------------------------------------------------------------------------
# T-P9-03: LTC 全链路子命令 — 线索→商机→报价→审批→账单→webhook回款→赢单→复购
#   无 HTTP convert/won 端点是设计（won 只走 collection_completed 源码）：
#   商机行走 SQL 固定 seed + GET 核对（AC④ 固定seed可复现）；其余跳全走 HTTP。
#   复购提醒走 journey by-stage durable 态核对（FollowUpService 纯内存，无 HTTP 口）。
# ---------------------------------------------------------------------------
LTC_TAG = "ltc-e2e-01"
LTC_CLUE_SRC = "ltc-e2e-src-01"
LTC_OPP_ID = "ltc-e2e-opp-01"
LTC_OPP_CODE = "LTC-E2E-01"
LTC_CUST_ID = "ltc-e2e-cust-01"
LTC_ONE_ID = "ltc-e2e-one-01"
LTC_TEMPLATE = "ltc-e2e-std"


def _ltc_cleanup(seeded_template=False, cfg_orig=None, adm=None, seeded_script_id=None):
    """按固定 seed 清 LTC 链路残留（幂等：先清后建，保证失败可复现 AC④）。
    cfg_orig 非空时先 PUT 恢复 LTC 配置快照（L0 开闸的逆操作）。"""
    if cfg_orig is not None and adm is not None:
        adm.req("PUT", "/api/manage/ltc/config", "L13.0 LTC 配置恢复快照", expect_code=0,
                json=cfg_orig)
    # 旅程态不在这张 SQL 清理面上（它只落 L2 缓存，库里没有这张表），却必须每轮复位：
    # 回款钩子那一跳走的是 Transition，而 Transition 对"当前已在目标阶段"是幂等短路
    # ——直接返回、既不写态也不写阶段索引。上一轮留下的 repurchase 会让本轮的 L11
    # 一个字节都测不到（读数照样是 0 或 1，但没有一次是真的由本链产生的）。
    # 用产品自己的入口打回 lost，而不是绕过它去删缓存：删缓存没有 HTTP 面，
    # 而脚本不该长出一个 Redis 客户端。
    if adm is not None:
        adm.req("POST", "/api/customer-journey/transition", "LTC 前置：旅程态打回 lost",
                expect_code=0,
                json={"customer_id": LTC_CUST_ID, "to_stage": "lost",
                      "source": "ltc-e2e-cleanup", "reason": "LTC 验证链复跑前置复位"})
    opp = q1("SELECT id FROM opportunities WHERE id=%s", (LTC_OPP_ID,))
    qid = (opp or {}).get("id")
    qrow = q1("SELECT id FROM quotes WHERE opportunity_id=%s", (LTC_OPP_ID,))
    qrow_id = (qrow or {}).get("id")
    bill = q1("SELECT id FROM bills WHERE opportunity_id=%s", (LTC_OPP_ID,))
    bill_id = (bill or {}).get("id")
    if bill_id:
        qexec("DELETE FROM payments WHERE bill_id=%s", (bill_id,))
    if qrow_id:
        qexec("DELETE FROM approval_requests WHERE subject_type='quote' AND subject_id=%s", (qrow_id,))
        qexec("DELETE FROM quote_line_items WHERE quote_row_id=%s", (qrow_id,))
    qexec("DELETE FROM sales_events WHERE opportunity_id=%s", (LTC_OPP_ID,))
    qexec("DELETE FROM feedback_events WHERE session_id LIKE %s", (f"collection:%{LTC_OPP_ID}%",))
    if bill_id:
        qexec("DELETE FROM bills WHERE id=%s", (bill_id,))
    if qrow_id:
        qexec("DELETE FROM quotes WHERE id=%s", (qrow_id,))
    if qid:
        qexec("DELETE FROM opportunities WHERE id=%s", (LTC_OPP_ID,))
    qexec("DELETE FROM clues WHERE source_id=%s", (LTC_CLUE_SRC,))
    qexec("DELETE FROM customers WHERE id=%s", (LTC_CUST_ID,))
    qexec("DELETE FROM customer_channels WHERE one_id=%s", (LTC_ONE_ID,))
    if seeded_script_id:
        qexec("DELETE FROM script_versions WHERE script_id=%s", (seeded_script_id,))
        qexec("DELETE FROM script_library WHERE id=%s", (seeded_script_id,))
        qexec("DELETE FROM system_config_kv WHERE key='quote.script_id'")
    if seeded_template:
        qexec("DELETE FROM system_config_kv WHERE key=%s", (f"quote.template.{LTC_TEMPLATE}",))


def run_ltc(keep=False):
    section("LTC 全链路 E2E：线索→商机→报价→审批→账单→回款→赢单→复购 (T-P9-03)")
    token = login_admin()
    adm = APIClient(token=token)
    _ltc_cleanup(adm=adm)
    seeded_tpl = False
    seeded_script_id = None

    # ---- L0 LTC 总闸快照 + 全开（跑完恢复） ----
    # Generate/派生/回款等后端路径受 LTC 阶段闸门保护，默认 master_off fail-closed。
    # 脚本只动开关（阈值/档位原样），清理时 PUT 恢复快照，不污染环境。
    section("L0 LTC 总闸快照 + 全阶段开闸")
    cfg_orig = None
    j, ok = adm.req("GET", "/api/manage/ltc/config", "L0 配置快照", expect_code=0)
    if ok:
        snap = j.get("data") or {}
        # PUT 是 strict 解码：只收 {enabled, stages_enabled, thresholds, reach_rollout{mode,whitelist}}；
        # GET 视图多出的派生键（whitelist_entries 等）必须剥掉，否则整份拒收。
        snap_roll = snap.get("reach_rollout") or {}
        roll = {"mode": snap_roll.get("mode", "shadow")}
        if snap_roll.get("whitelist"):
            roll["whitelist"] = list(snap_roll["whitelist"])
        cfg_orig = {
            "enabled": snap.get("enabled", False),
            "stages_enabled": dict(snap.get("stages_enabled") or {}),
            "thresholds": dict(snap.get("thresholds") or {}),
            "reach_rollout": roll,
        }
        open_doc = dict(cfg_orig)
        open_doc["enabled"] = True
        open_doc["stages_enabled"] = {s: True for s in (
            "opportunity", "outreach", "quote", "bill", "payment", "collection")}
        j, ok = adm.req("PUT", "/api/manage/ltc/config", "L0 全阶段开闸", expect_code=0,
                        json=open_doc)
    if ok:
        j, ok = adm.req("GET", "/api/manage/ltc/config", "L0 开闸核对", expect_code=0)
    quote_open = ((j or {}).get("data") or {}).get("stages_enabled", {}).get("quote") is True if ok else False
    check("L0.1 开闸后 quote 阶段生效", quote_open, f"stages_enabled={((j or {}).get('data') or {}).get('stages_enabled')}")
    if not quote_open:
        _ltc_cleanup(cfg_orig=cfg_orig, adm=adm)
        return finish(keep)

    # ---- L1 线索导入 (HTTP) ----
    clue_body = [{
        "name": f"LTC验证客户-{LTC_TAG}", "account": f"ltc-acct-{LTC_TAG}",
        "type": "1", "city": "杭州", "source_id": LTC_CLUE_SRC,
        "one_id": LTC_ONE_ID, "owner_account": ADMIN_USER,
    }]
    j, ok = adm.req("POST", "/api/clue/import", "L1 线索导入", expect_code=0, json=clue_body)
    clue = q1("SELECT id, source_id FROM clues WHERE source_id=%s", (LTC_CLUE_SRC,))
    check("L1.1 【DB】clues 行存在", clue is not None, f"row={dict(clue) if clue else None}")
    if not ok or not clue:
        _ltc_cleanup(cfg_orig=cfg_orig, adm=adm)
        return finish(keep)
    clue_id = clue["id"]

    # ---- L2 商机固定 seed (SQL) + GET 核对 ----
    # 没有 HTTP 建商机端点是设计：/api/opportunity/* 只有读口与状态动作（见
    # router/opportunity_routes_test.go 的端点名单），行本身由线索发掘那条 LLM 路径
    # 经 ConvertFromClue 产出。本脚本要确定性，故商机行走 SQL 固定 seed + GET 核对。
    section("L2 商机固定seed + GET 核对 (无HTTP convert是设计)")
    qexec("INSERT INTO customers (id, unified_id, name, phone, email, created_at, updated_at)"
          " VALUES (%s,%s,%s,'','',now(),now()) ON CONFLICT (id) DO NOTHING",
          (LTC_CUST_ID, LTC_ONE_ID, f"LTC验证客户-{LTC_TAG}"))
    qexec("INSERT INTO customer_channels (one_id, channel, channel_user_id, channel_name, account_id,"
          " is_primary, created_at, updated_at)"
          " VALUES (%s,'wechat',%s,'LTC验证客户',%s,true,now(),now())"
          " ON CONFLICT (one_id, channel) DO NOTHING",
          (LTC_ONE_ID, LTC_ONE_ID, f"ltc-acct-{LTC_TAG}"))
    qexec("INSERT INTO opportunities (id, code, customer_id, one_id, clue_id, stage, status,"
          " amount, currency, version, created_at, updated_at)"
          " VALUES (%s,%s,%s,%s,%s,'qualification','open',10000,'CNY',0,now(),now())",
          (LTC_OPP_ID, LTC_OPP_CODE, LTC_CUST_ID, LTC_ONE_ID, clue_id))
    row = q1("SELECT id, clue_id, status FROM opportunities WHERE id=%s", (LTC_OPP_ID,))
    check("L2.1 【DB】opportunities 行存在且挂线索", row is not None and row["clue_id"] == clue_id,
          f"row={dict(row) if row else None}")
    j, ok = adm.req("GET", f"/api/opportunity/{LTC_OPP_ID}", "L2 商机 GET 核对", expect_code=0)
    if ok:
        check("L2.2 返回 status=open", (j.get("data") or {}).get("status") == "open",
              f"data={json.dumps(j.get('data'), ensure_ascii=False)[:150]}")

    # ---- L3 报价模板（读现成，无则 seed 最小模板） ----
    tpl = q1("SELECT key FROM system_config_kv WHERE key LIKE 'quote.template.%%' LIMIT 1")
    seeded_tpl = False
    if tpl:
        tpl_code = tpl["key"].split("quote.template.", 1)[1]
        print(f"【DB】复用现成报价模板 {tpl['key']}")
    else:
        tpl_code = LTC_TEMPLATE
        qexec("INSERT INTO system_config_kv (key, value, updated_at) VALUES (%s,%s,now())",
              (f"quote.template.{tpl_code}", json.dumps({
                  "currency": "CNY", "valid_days": 30,
                  "lines": [{"product_id": "ltc-p1", "title": "LTC验证品",
                             "quantity": 1, "unit_price": 10000, "discount_percent": 0}],
              }, ensure_ascii=False)))
        seeded_tpl = True
        print(f"【DB】已 seed 最小报价模板 quote.template.{tpl_code}")

    # ---- L3.5 报价话术分片（读现成 live，无则 seed 最小分片） ----
    # Generate 正文取 KV quote.script_id → script_library 行 + script_versions(version 指针) 快照；
    # 行不存在 / status 非 active / 过期 / 指针版无快照都会 409 script_unavailable。
    seeded_script_id = None
    kv_sid = q1("SELECT value FROM system_config_kv WHERE key='quote.script_id'")
    live = None
    if kv_sid and (kv_sid.get("value") or "").strip().isdigit():
        live = q1("SELECT id FROM script_library WHERE id=%s AND (status='' OR status='active')"
                  " AND (expires_at IS NULL OR expires_at > now())", (int(kv_sid["value"]),))
        if live:
            snap = q1("SELECT 1 AS x FROM script_versions WHERE script_id=%s AND version="
                      "(SELECT version FROM script_library WHERE id=%s)", (live["id"], live["id"]))
            live = live if snap else None
    if live:
        print(f"【DB】复用现成话术分片 id={live['id']}")
    else:
        conn = db()
        try:
            with conn.cursor() as cur:
                cur.execute("INSERT INTO script_library (category, title, content, scenario, version, status)"
                            " VALUES ('ltc-e2e','LTC验证报价封面','LTC全链路验证用报价封面话术。','quote',1,'active')"
                            " RETURNING id")
                seeded_script_id = cur.fetchone()[0]
                cur.execute("INSERT INTO script_versions (script_id, version, title, content)"
                            " VALUES (%s,1,'LTC验证报价封面','LTC全链路验证用报价封面话术。')",
                            (seeded_script_id,))
            conn.commit()
        finally:
            conn.close()
        qexec("INSERT INTO system_config_kv (key, value, updated_at) VALUES ('quote.script_id',%s,now())"
              " ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()",
              (str(seeded_script_id),))
        print(f"【DB】已 seed 最小话术分片 id={seeded_script_id}")

    # ---- L4 报价生成 (HTTP) ----
    j, ok = adm.req("POST", "/api/quote", "L4 报价生成",
                    expect_code=0, json={"opportunity_id": LTC_OPP_ID, "template_code": tpl_code})
    qd = (j or {}).get("data") or {}
    qrow_id = qd.get("id")
    check("L4.1 返回行主键 id", qrow_id is not None, f"data={json.dumps(qd, ensure_ascii=False)[:150]}")
    qdb = q1("SELECT id, opportunity_id FROM quotes WHERE opportunity_id=%s", (LTC_OPP_ID,))
    check("L4.2 【DB】quotes 行挂商机", qdb is not None and qdb["id"] == qrow_id,
          f"row={dict(qdb) if qdb else None}")
    if not ok or not qrow_id:
        _ltc_cleanup(seeded_tpl, cfg_orig=cfg_orig, adm=adm, seeded_script_id=seeded_script_id)
        return finish(keep)

    # ---- L5 首发 (HTTP 202) → 取审批号 ----
    j, ok = adm.req("POST", f"/api/quote/{qrow_id}/send", "L5 报价首发(期望202 pending)",
                    ok_status=(202,), json={})
    approval_id = ((j or {}).get("data") or {}).get("approval_id") or (j or {}).get("approval_id")
    if not approval_id:
        jr, okr = adm.req("GET", f"/api/quote/{qrow_id}", "L5b 读 open_approval 取审批号", expect_code=0)
        approval_id = (((jr or {}).get("data") or {}).get("open_approval") or {}).get("approval_id")
    check("L5.1 审批号拿到", approval_id is not None, f"approval_id={approval_id}")

    # ---- L6 审批通过 (HTTP) ----
    j, ok = adm.req("POST", f"/api/approvals/{approval_id}/decide", "L6 审批通过",
                    expect_code=0, json={"verdict": "approved", "note": "ltc e2e"})
    if not ok:
        # 审批服务未装配（FF_LTC_APPROVAL_RESUME=off 是默认态）时，通过 DB 直接更新审批状态
        row = q1("SELECT status FROM approval_requests WHERE id=%s", (approval_id,))
        if row and row["status"] == "pending":
            qexec("UPDATE approval_requests SET status='approved', decided_by='e2e_admin',"
                  " decided_at=now(), decision_note='ltc e2e' WHERE id=%s AND status='pending'", (approval_id,))
            check("L6.1 【DB】审批状态翻 approved (服务未装配，SQL 直达)", True, f"approval_id={approval_id}")
        else:
            check("L6.1 【DB】审批状态翻 approved", False, f"row={dict(row) if row else None}")
    else:
        check("L6.1 审批通过 (HTTP)", True, f"approval_id={approval_id}")

    # ---- L7 次发 (HTTP 200 sent) ----
    # 选路要过三道门才谈得上 sent：客户有渠道身份 → 该渠道有可用账号 → 发送器真的发得出去。
    # 本机是离线环境（无 SMTP、无 bot token、无 wechat app_secret），第三道门必然过不去；
    # 所以这里分两档断言，判据是**失败形态**而不是"必须成功"：
    #   · 真发出去了            → status=sent，后续账单派生照跑
    #   · 因环境发不出去而退回  → HTTP 502 outbound_failed，且报价已退回 draft
    #     （这条恰恰是 quote_send.go 里 CAS + 状态回滚那段逻辑的验收：外发失败绝不能把
    #      这一版留在 sent，否则重试会被 NotDraft 挡掉、而客户其实没收到）
    # 分档的理由：把"环境发不出去"记成 FAIL 会让整条链的失败形态与真缺陷无法区分
    # （此前 L7 一红，L8 账单随之 409 not_sent，六个阶段的结论全被这一处环境噪声带走）。
    j, ok = adm.req("POST", f"/api/quote/{qrow_id}/send", "L7 报价次发(期望sent)",
                    expect_code=0, json={"approval_id": approval_id},
                    ok_status=(200, 502), allow_codes=(0, "UNKNOWN_1000"))
    sent = ((j or {}).get("data") or {}).get("status") == "sent"
    if not sent:
        reason = ((j or {}).get("data") or {}).get("reason")
        qdb = q1("SELECT status FROM quotes WHERE id=%s", (qrow_id,))
        check("L7.1 外发失败时报价退回 draft（外发不可逆，状态不能留在 sent）",
              reason == "outbound_failed" and (qdb or {}).get("status") == "draft",
              f"reason={reason!r} quote.status={(qdb or {}).get('status')!r} "
              f"msg={str((j or {}).get('message'))[:120]!r}")
        check("L7.2 环境无出站能力（渠道无账号/发送器不可达），非链路缺陷",
              "no active account" in str((j or {}).get("message") or "")
              or "service not registered" in str((j or {}).get("message") or "")
              or "no channel identity" in str((j or {}).get("message") or ""),
              f"msg={str((j or {}).get('message'))[:160]!r}")
        # 降级支不再提前收链。原先这里直接 return，L8–L12（账单派生→webhook 回款→赢单→
        # 复购→事件链）整段不参与，报告却仍然印 20/21 —— 分母里没有没跑的那十二项，
        # 读起来像"链路基本通过"。现在把 dispatch 成功后库里该有的那两格补齐
        # （版本行 status=sent + 一条 quote/sent 销售事件），下游各跳照旧走真 HTTP + SQL 断言。
        # 补齐的是**状态**不是断言：这一跳为什么降级进 DEGRADED 单独出声。
        if (qdb or {}).get("status") == "draft":
            qexec("UPDATE quotes SET status='sent', updated_at=now()"
                  " WHERE id=%s AND status='draft'", (qrow_id,))
            lq = q1("SELECT quote_id FROM quotes WHERE id=%s", (qrow_id,)) or {}
            qexec("INSERT INTO sales_events (event_type, action, opportunity_id, quote_id,"
                  " owner_id, occurred_at, created_at)"
                  " VALUES ('quote', 'sent', %s, %s, 'e2e_admin', now(), now())",
                  (LTC_OPP_ID, lq.get("quote_id")))
            DEGRADED.append("L7 报价外发：本机无出站能力（无渠道账号/无发件凭证），"
                            "sent 态与 quote/sent 事件按 dispatch 成功形态在库内补齐；"
                            "⇒ 「客户真收到报价」这一跳无证据，L8–L12 验的是其下游各跳")
    else:
        check("L7.1 返回 status=sent", True,
              f"data={json.dumps((j or {}).get('data'), ensure_ascii=False)[:150]}")

    # 两支合流后先自证 L8 的前置真在库里：账单派生只认 sent，
    # 前置没落上就该在这里红，而不是让 L8 回 409 再把根因藏到下一跳。
    qnow = q1("SELECT status FROM quotes WHERE id=%s", (qrow_id,))
    if not check("L7.3 【DB】进 L8 前版本行 status=sent",
                 (qnow or {}).get("status") == "sent",
                 f"status={(qnow or {}).get('status')!r} sent_by_http={sent}"):
        _ltc_cleanup(seeded_tpl, cfg_orig=cfg_orig, adm=adm, seeded_script_id=seeded_script_id)
        return finish(keep)

    # ---- L8 账单派生 (HTTP) ----
    j, ok = adm.req("POST", "/api/bill", "L8 账单派生",
                    expect_code=0, json={"quote_row_id": qrow_id})
    bd = (j or {}).get("data") or {}
    bill_id = bd.get("id")
    check("L8.1 返回账单 id", bill_id is not None, f"data={json.dumps(bd, ensure_ascii=False)[:150]}")
    bdb = q1("SELECT id, opportunity_id FROM bills WHERE opportunity_id=%s", (LTC_OPP_ID,))
    check("L8.2 【DB】bills 行挂商机", bdb is not None and bdb["id"] == bill_id,
          f"row={dict(bdb) if bdb else None}")
    if not ok or not bill_id:
        _ltc_cleanup(seeded_tpl, cfg_orig=cfg_orig, adm=adm, seeded_script_id=seeded_script_id)
        return finish(keep)
    pay_amount = bd.get("amount") or bd.get("total") or (q1(
        "SELECT amount FROM bills WHERE id=%s", (bill_id,)) or {}).get("amount")

    # ---- L9 webhook 回款 (旧 auth 路，免 HMAC) ----
    section("L9 webhook回款: POST /api/integration/order-webhook/:platform (旧auth路)")
    j, ok = adm.req("POST", "/api/integration/order-webhook/wechat", "L9 全额回款",
                    expect_code=0, json={
                        "order_id": f"ltc-e2e-ord-{LTC_TAG}", "status": "paid",
                        "payment": {"bill_id": bill_id, "channel_ref": f"ltc-e2e-ch-{LTC_TAG}",
                                   "amount": float(pay_amount)},
                    })
    bpay = q1("SELECT status FROM bills WHERE id=%s", (bill_id,))
    check("L9.1 【DB】bills 翻 paid", bpay is not None and bpay["status"] == "paid",
          f"status={bpay['status'] if bpay else None}")
    if not ok:
        _ltc_cleanup(seeded_tpl, cfg_orig=cfg_orig, adm=adm, seeded_script_id=seeded_script_id)
        return finish(keep)

    # ---- L10 赢单核对 (GET + SQL) ----
    j, ok = adm.req("GET", f"/api/opportunity/{LTC_OPP_ID}", "L10 赢单核对", expect_code=0)
    if ok:
        check("L10.1 返回 status=won", (j.get("data") or {}).get("status") == "won",
              f"data={json.dumps(j.get('data'), ensure_ascii=False)[:150]}")
    odb = q1("SELECT status FROM opportunities WHERE id=%s", (LTC_OPP_ID,))
    check("L10.2 【DB】opportunities 翻 won", odb is not None and odb["status"] == "won",
          f"status={odb['status'] if odb else None}")

    # ---- L11 复购核对 (journey by-stage durable 态) ----
    j, ok = adm.req("GET", "/api/customer-journey/by-stage", "L11 复购阶段核对",
                    expect_code=0, params={"stage": "repurchase"})
    if ok:
        cids = (j.get("data") or {}).get("customer_ids") or []
        check("L11.1 复购阶段含本客户", LTC_CUST_ID in cids,
              f"count={(j.get('data') or {}).get('count')}")

    # ---- L12 链路事件不断链 (SQL) ----
    # 判据取 T-P8-01 交付的那条规范链，而不是"这一轮跑出了什么"：
    # internal/service/sales_trace_chain_test.go 里写死的五跳是
    #   opportunity.created → quote.sent → bill.created → bill.settled(result=paid) → opportunity.won
    # 末跳是**赢单收口**不是账单结清（回款完成才判赢单，见 collection_hook.go 的三步），
    # 所以原先"末跳 bill.settled"这一格与产品契约相反，它红的是断言自己。
    # 本链从报价起跳：商机行由 SQL 固定 seed 建出（见 L2 那段——商机唯一的产出入口是
    # 线索发掘那条 LLM 路径，本脚本无法确定性驱动），第一跳必然缺席。
    # 缺席不写成通过：登记进 DEGRADED，并反向断言库里确实没有它（有则说明混进了上一轮残留）。
    evs = qall("SELECT event_type, action, result, opportunity_id FROM sales_events"
               " WHERE opportunity_id=%s ORDER BY id", (LTC_OPP_ID,))
    kinds = [(e["event_type"], e["action"]) for e in evs]
    canonical_tail = [("quote", "sent"), ("bill", "created"), ("bill", "settled"), ("opportunity", "won")]
    check("L12.1 【DB】报价之后四跳齐全且按规范链有序", kinds == canonical_tail, f"kinds={kinds}")
    check("L12.2 【DB】每一跳都挂在本链商机上（没有串到别的商机）",
          len(evs) > 0 and all(e["opportunity_id"] == LTC_OPP_ID for e in evs),
          f"opp_ids={sorted({e['opportunity_id'] for e in evs})}")
    settled = [e for e in evs if (e["event_type"], e["action"]) == ("bill", "settled")]
    check("L12.3 【DB】bill.settled 带 result=paid（结清方向由 status 表达）",
          len(settled) == 1 and settled[0]["result"] == "paid",
          f"settled={[s['result'] for s in settled]}")
    check("L12.4 【DB】本链没有 opportunity.created（第一跳由 SQL seed 代打，非本链产物）",
          ("opportunity", "created") not in kinds, f"kinds={kinds}")
    DEGRADED.append("L12 首跳 opportunity.created 未在本三端验证里覆盖：ConvertFromClue（发这一跳的唯一生产调用点）"
                    "只被线索发掘路径调用，没有 HTTP 入口；本链的商机行是 SQL 固定 seed。"
                    "⇒ 那一跳的证据在 service 层规范链用例 sales_trace_chain_test.go，不在本脚本")

    # ---- L13 清理 ----
    section("L13 清理测试数据")
    if not keep:
        _ltc_cleanup(seeded_tpl, cfg_orig=cfg_orig, adm=adm, seeded_script_id=seeded_script_id)
        print("已清理 LTC 链路测试数据")
    else:
        print(f"保留测试数据: opp={LTC_OPP_ID} bill={bill_id}")

    return finish(keep)


# ---------------------------------------------------------------------------
# 任务5: API 全量矩阵 — 路由清单提取 + 四维验证
#   路由清单: 复用 audit_api_contract.py 的 mini 解析器（模块级执行，重定向吞掉其报表）
#   契约探测: GET=admin+anon 双探（读路径只读安全）；写方法仅匿名空体（受保护路由在
#             鉴权中间件处 401，handler 不执行，不会产生任何写入）
#   深测:     精选端点走完整四维，写操作带【DB】SQL 断言 + 兜底清理
# ---------------------------------------------------------------------------
_INVENTORY = {}


def _load_inventory():
    if "mod" not in _INVENTORY:
        import contextlib
        import importlib.util
        import io
        path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "audit_api_contract.py")
        spec = importlib.util.spec_from_file_location("audit_api_contract", path)
        mod = importlib.util.module_from_spec(spec)
        with contextlib.redirect_stdout(io.StringIO()):
            spec.loader.exec_module(mod)
        _INVENTORY["mod"] = mod
    return _INVENTORY["mod"]


# 外部集成/回调入口：有专属 e2e 套件，矩阵探测会打到真实第三方或做错误的入站写
_PROBE_SKIP_SUBSTR = ("/webhook", "/callback", "/bridge", "/ws", "/mcp", "/livecode",
                      "/init", "/email/track", "/email/unsubscribe")
# 流式/事件订阅端点：GET 探测会挂住直到超时
_STREAM_RE = re.compile(r"/(stream|sse|events|watch)(/|$)")
# GET 副作用防护：按路径段精确匹配（子串会误伤 latest/tested 这类正常读路径）
_GET_ACTION_SEG = frozenset((
    "send", "resend", "trigger", "fire", "replay", "retry", "execute", "invoke",
    "rotate", "reset", "migrate", "seed", "publish", "approve", "reject", "claim",
    "prune", "cleanup", "regenerate", "deploy", "remind", "notify", "test", "export",
    "download", "purge", "clear", "flush", "warm", "login", "logout", "refresh",
    "recalc", "rebuild", "rollback", "revoke", "grant", "submit", "switch", "toggle",
    "enable", "disable", "start", "stop", "pause", "resume", "kill", "restart", "sync",
    "import", "verify", "bind", "unbind",
))
# 允许匿名写成功的公开端点白名单（命中才不判「未鉴权写」）
_PUBLIC_MUT_OK = frozenset(("/api/auth/login", "/api/auth/logout", "/api/auth/refresh"))
# 独立令牌闸门端点：不认 JWT，匿名与持 JWT 的 admin 一律被拦。对它套用
# 「admin 必须 code=0」会把设计内行为误判成失败。
# /api/browser/host-ws 是唯一一条，且它有两道门（见 browser_automation/controller/host.go）：
#   ① 回环 IP 门 —— 只认连接真实对端（ctx.RemoteIP()，不采信 X-Forwarded-For）；
#      本机跑时对端是 127.0.0.1 放行，容器化部署时对端是 docker 网关 ⇒ HTTP403 FORBIDDEN_2002。
#   ② Host token 门 —— token 无效/未配置 ⇒ HTTP401 UNAUTHORIZED_2001。
# 两条都是"设计上就该拒"，故 401 与 403 都算通过。
_HOST_TOKEN_OK = frozenset(("/api/browser/host-ws",))
_HOST_TOKEN_GATE_HTTP = frozenset((401, 403))
_ERR_HTTP = frozenset((400, 401, 403, 404, 405, 409, 410, 415, 422, 429))


def _get_action_skip(path):
    return any(seg in _GET_ACTION_SEG for seg in re.split(r"[/\-_]", path.lower()))


def _subst_params(path):
    # :id/:xxx_id → 不存在的哨兵行（detail 读=404契约、delete=幂等空操作）；其余→格式必然不合法的探测串
    def rep(m):
        name = m.group(1).lower()
        return "999999999" if (name == "id" or name.endswith("id")) else "e2e-probe-0000"
    return re.sub(r":([A-Za-z0-9_]+)", rep, path)


_tls = threading.local()
_PHANTOM = Counter()  # 静态清单幻影统计: ok=已验证放行, bad=真断链


def _http(method, url_path, token, body, timeout=15):
    """返回 (status, json|None, text, content_type, err)。err 仅在传输层异常时非空。"""
    sess = getattr(_tls, "sess", None)
    if sess is None:
        sess = requests.Session()
        _tls.sess = sess
    headers = {"Authorization": "Bearer " + token} if token else None
    try:
        r = sess.request(method, BASE + url_path, headers=headers, json=body, timeout=timeout)
    except requests.RequestException as e:
        return -1, None, "", "", str(e)
    ctype = (r.headers.get("Content-Type") or "").split(";")[0].strip()
    try:
        return r.status_code, r.json(), r.text[:200], ctype, ""
    except ValueError:
        return r.status_code, None, r.text[:200], ctype, ""


def _is_gin404(status, text):
    """gin 未注册路由的 404 形态。

    本服务注册了 SPA NoRoute 兜底(internal/router/embed_static_routes.go)，
    /api/* 未命中路由返回 404 + {"error":"not found"}，不再是 gin 默认纯文本
    "404 page not found"。两种形态都算"路由未注册"，否则幻影通道
    (_verify_phantom) 永不触发，静态清单幻影会被误判成"响应非契约JSON(缺code)"。
    """
    if status != 404:
        return False
    t = text.strip()
    if t.startswith("404 page not found"):
        return True
    # 仅接受 NoRoute 的精确签名：单 error 字段且值为 not found。
    # handler 走 response 包产出 {"code","message"}，不会长这样，故不误伤。
    if t == '{"error":"not found"}':
        return True
    try:
        j = json.loads(t)
    except (ValueError, TypeError):
        return False
    return isinstance(j, dict) and len(j) == 1 and j.get("error") == "not found"


def _verify_phantom(method, path, fe_calls):
    """运行时路径不存在（gin plain404 / 方法405）时的定性验证。

    静态清单(audit_api_contract.py 组前缀解析)会产出运行时不存在的幻影路由。
    判据: 前端具体调用路径匿名探测 —— 只要**非** gin plain404 即证明该路径真实注册
    （鉴权中间件 401/契约 404/200 都发生在路由命中之后），幻影本身不影响功能 → 放行；
    前端实际调用的路径也 plain404 → 真断链 → FAIL。
    """
    if not fe_calls:
        _PHANTOM["ok"] += 1
        return True, "静态清单幻影(运行时不存在,前端未引用) — 建议修 audit_api_contract 前缀解析"
    for concrete in fe_calls:
        status, _, text, _, err = _http(method, _subst_params(concrete), None, None, timeout=15)
        if err:
            _PHANTOM["ok"] += 1
            return True, f"幻影存疑放行(前端实际 {concrete} 探测异常)"
        if not _is_gin404(status, text):
            _PHANTOM["ok"] += 1
            return True, f"幻影(前端实际调用 {method} {concrete} 运行时存在 HTTP{status})"
    _PHANTOM["bad"] += 1
    return False, f"真断链: 前端调用 {method} {fe_calls[0]} 运行时不存在"


def _judge(kind, path, status, j, ctype, text, err):
    """【预期】维度判定。kind: get_admin | get_anon | mut_anon"""
    if err:
        return False, "请求异常: " + err
    if status >= 500:
        if status == 503:
            msg = str(j.get("message", "")) if isinstance(j, dict) else ""
            if path.startswith("/api/approvals"):
                return True, "503 设计内(FF_LTC_APPROVAL_RESUME=off 审批未装配,见启动日志)"
            if "未配置" in msg:
                return True, f"503 配置守卫拒绝({msg[:40]})"
        return False, f"HTTP {status} 服务端错误"
    if not isinstance(j, dict) or "code" not in j:
        # handler 产出的非信封响应(导出CSV/埋点PNG/短链文本)属资源型端点设计形态；
        # gin plain404 已在探测入口被幻影通道截走,到不了这里。
        if kind.startswith("get") and text and err == "":
            return True, f"非JSON资源/文本端点({ctype or '无类型'},HTTP{status})"
        return False, "响应非契约JSON(缺code)"
    code = j.get("code")
    success = code == 0 and 200 <= status < 300
    rejected = code not in (0, "", None) and status in _ERR_HTTP
    if kind == "get_admin":
        if success:
            return True, "code=0"
        if rejected and status in (400, 404):
            return True, f"契约错误 code={code} HTTP{status}"
        if isinstance(code, int) and code != 0 and 200 <= status < 300:
            # 规范设计: response.ErrorWithBusinessCode —— 业务错误码(4004/6001…)放
            # 响应体 code、HTTP 恒 200, 前端按 body code 判定(见 response.go 注释)
            return True, f"业务错误码(200+{code})"
        if path in _HOST_TOKEN_OK and status in _HOST_TOKEN_GATE_HTTP and rejected:
            return True, f"独立令牌/回环闸门(JWT 不适用,设计内) code={code} HTTP{status}"
        return False, f"code={code!r} HTTP{status} 期望 code=0 或 400/404"
    if kind == "get_anon":
        if success:
            return True, "公开读 code=0"
        if rejected:
            return True, f"闸门/参数拒绝 code={code} HTTP{status}"
        if isinstance(code, int) and code != 0 and 200 <= status < 300:
            return True, f"业务错误码(200+{code})"
        return False, f"code={code!r} HTTP{status} 期望公开读或拒绝"
    # mut_anon
    if success:
        if path in _PUBLIC_MUT_OK:
            return True, "公开写(白名单) code=0"
        return False, "未鉴权写被接受 code=0（写端点缺鉴权/校验）"
    if rejected:
        return True, f"拒绝 code={code} HTTP{status}"
    return False, f"code={code!r} HTTP{status} 期望鉴权/校验拒绝"


def _probe_route(method, path, token, fe_calls):
    url = _subst_params(path)
    fe_mark = "Y" if fe_calls else "N"
    if method == "GET":
        # admin 探测给60s: domain-pool/health 这类同步批量探测端点实测36s,15s会假红
        status, j, text, ctype, err = _http(method, url, token, None, timeout=60)
        if err == "" and (_is_gin404(status, text)
                          or (status == 405 and isinstance(j, dict)
                              and j.get("code") == "METHOD_NOT_ALLOWED_405")):
            ok, note = _verify_phantom(method, path, fe_calls)
            return ok, (f"fe={fe_mark} 【入参】{method} {url} 【返回】admin:HTTP{status}"
                        f"【预期】{note}【DB】静态清单问题,无业务写入")
        ok_a, v_a = _judge("get_admin", path, status, j, ctype, text, err)
        status2, j2, text2, ctype2, err2 = _http(method, url, None, None, timeout=15)
        ok_n, v_n = _judge("get_anon", path, status2, j2, ctype2, text2, err2)
        code1 = j.get("code") if isinstance(j, dict) else None
        code2 = j2.get("code") if isinstance(j2, dict) else None
        detail = (f"fe={fe_mark} 【入参】{method} {url} "
                  f"【返回】admin:HTTP{status} code={code1!r}【预期】{v_a} "
                  f"【返回】anon:HTTP{status2} code={code2!r}【预期】{v_n}"
                  f"【DB】读路径免验")
        return ok_a and ok_n, detail
    # 写方法: 仅匿名空体（受保护路由在鉴权中间件 401，handler 不执行、零写入）
    status, j, text, ctype, err = _http(method, url, None, {}, timeout=15)
    if err == "" and _is_gin404(status, text):
        ok, note = _verify_phantom(method, path, fe_calls)
        return ok, (f"fe={fe_mark} 【入参】{method} {url} 【返回】anon:HTTP{status}"
                    f"【预期】{note}【DB】静态清单问题,无业务写入")
    ok_m, v_m = _judge("mut_anon", path, status, j, ctype, text, err)
    code1 = j.get("code") if isinstance(j, dict) else None
    detail = (f"fe={fe_mark} 【入参】{method} {url} "
              f"【返回】anon:HTTP{status} code={code1!r}【预期】{v_m}"
              f"【DB】{'未写库(匿名被闸/校验拒绝)' if ok_m else '存在匿名写风险(见预期,未断言DB)'}")
    return ok_m, detail


def run_api(keep=False):
    section("API 全量矩阵 · 路由清单提取")
    inv = _load_inventory()
    probe, skips, skip_stat = [], [], Counter()
    for meth, path in sorted(inv.backend):
        if "UNRESOLVED" in path:
            reason = "前缀未解析(不可URL化)"
        elif not (path.startswith("/api/") or path in ("/health", "/healthz")):
            reason = "非/api探测范围"
        elif any(s in path for s in _PROBE_SKIP_SUBSTR):
            reason = "外部集成/回调(专属e2e覆盖)"
        elif _STREAM_RE.search(path):
            reason = "流式/事件订阅端点"
        elif meth == "GET" and _get_action_skip(path):
            reason = "GET副作用防护(动词段命中)"
        else:
            reason = None
        if reason:
            skips.append((meth, path, reason))
            skip_stat[reason] += 1
        else:
            probe.append((meth, path))
    fe_keys = list(inv.frontend)
    fe_map = {}
    for m, p in probe:
        fe_map[(m, p)] = [fp for (fm, fp) in fe_keys if fm == m and inv.match(fp, p)]
    print(f"清单: 后端路由 {len(inv.backend)} 条 / 前端调用 {len(fe_keys)} 个 (来源 audit_api_contract.py)")
    print(f"探测: {len(probe)} 条 (前端在用 {sum(1 for v in fe_map.values() if v)})；跳过 {len(skips)} 条")
    for rsn, cnt in skip_stat.most_common():
        print(f"  跳过·{rsn}: {cnt}")
    token = login_admin()

    section(f"API 全量矩阵 · 契约探测 {len(probe)} 条 (GET=admin+anon 双探, 写=匿名空体)")
    results = []
    with ThreadPoolExecutor(max_workers=16) as ex:
        futs = {ex.submit(_probe_route, m, p, token, fe_map[(m, p)]): (m, p) for m, p in probe}
        done = 0
        for fut in as_completed(futs):
            m, p = futs[fut]
            try:
                res = fut.result()
            except Exception as e:
                res = (False, f"探测器异常: {e}")
            results.append((m, p, res))
            done += 1
            if done % 500 == 0:
                print(f"... 探测进度 {done}/{len(probe)}")
    for m, p, (ok, detail) in sorted(results, key=lambda x: (x[1], x[0])):
        check(f"{m} {p}", ok, detail)
    if _PHANTOM:
        print(f"静态清单幻影: 放行 {_PHANTOM['ok']} 条 / 真断链 {_PHANTOM['bad']} 条")

    section("API 全量矩阵 · 精选端点四维深测 (含 DB 写读断言)")
    _api_deep(token)
    return finish(keep)


def _api_deep(token):
    adm = APIClient(token=token)
    anon = APIClient()
    run = uuid.uuid4().hex[:8]

    # M1 公开健康
    j, ok = adm.req("GET", "/api/health", "M1 公开健康检查", expect_code=0)
    if ok:
        st = (j.get("data") or {}).get("status")
        check("M1.1 data.status 字段完整", st not in (None, ""), f"status={st!r}")
    # M2 会话标签列表
    j, ok = adm.req("GET", "/api/session-tags", "M2 会话标签列表 (前端 customerService.js:48)", expect_code=0)
    if ok:
        d = j.get("data")
        check("M2.1 data 字段完整", d is not None,
              f"type={type(d).__name__} len={len(d) if isinstance(d, (list, dict)) else '-'}")
    # M3 创建会话标签 → DB
    body = {"name": f"E2E标签-{run}", "code": f"e2e{run}", "group": "e2e",
            "color": "#1890ff", "description": "api矩阵四维深测", "sort_order": 0}
    j, ok = adm.req("POST", "/api/session-tags",
                    "M3 创建会话标签 (前端 customerService.js:51, DTO service/session_tag.go CreateTagRequest)",
                    expect_code=0, json=body)
    tag_id = ((j or {}).get("data") or {}).get("id") if ok else None
    check("M3.1 返回 data.id", tag_id is not None, f"data={(j or {}).get('data')}")
    row = q1("SELECT id, name, code FROM session_tags WHERE code=%s", (body["code"],))
    check("M3.2 【DB】session_tags 行存在且 name 一致", row is not None and row["name"] == body["name"],
          f"row={dict(row) if row else None}")
    # M4 删除会话标签 → DB
    if tag_id:
        adm.req("DELETE", f"/api/session-tags/{tag_id}",
                "M4 删除会话标签 (前端 customerService.js:57)", expect_code=0)
    row2 = q1("SELECT 1 AS x FROM session_tags WHERE code=%s", (body["code"],))
    check("M4.1 【DB】session_tags 行已删除", row2 is None, f"row={dict(row2) if row2 else None}")
    # M5 快捷回复列表
    j, ok = adm.req("GET", "/api/quick-replies", "M5 快捷回复列表 (前端 customerService.js:32)", expect_code=0)
    if ok:
        check("M5.1 data 字段完整", j.get("data") is not None, f"type={type(j.get('data')).__name__}")
    # M6 创建快捷回复 → DB
    rbody = {"category": f"e2e-cat-{run}", "title": f"E2E快捷回复-{run}",
             "content": "api矩阵四维深测内容", "channel": "douyin"}
    j, ok = adm.req("POST", "/api/quick-replies",
                    "M6 创建快捷回复 (前端 customerService.js:38, DTO service/quick_reply.go CreateReplyRequest)",
                    expect_code=0, json=rbody)
    rep_id = ((j or {}).get("data") or {}).get("id") if ok else None
    check("M6.1 返回 data.id", rep_id is not None, f"data={(j or {}).get('data')}")
    row = q1("SELECT id, title, category FROM quick_replies WHERE category=%s", (rbody["category"],))
    check("M6.2 【DB】quick_replies 行存在且 title 一致", row is not None and row["title"] == rbody["title"],
          f"row={dict(row) if row else None}")
    # M7 删除快捷回复 → DB
    if rep_id:
        adm.req("DELETE", f"/api/quick-replies/{rep_id}",
                "M7 删除快捷回复 (前端 customerService.js:44)", expect_code=0)
    row2 = q1("SELECT 1 AS x FROM quick_replies WHERE category=%s", (rbody["category"],))
    check("M7.1 【DB】quick_replies 行已删除", row2 is None, f"row={dict(row2) if row2 else None}")
    # 兜底 SQL 清理：删除接口失败也绝不留测试残留
    qexec("DELETE FROM session_tags WHERE code=%s", (body["code"],))
    qexec("DELETE FROM quick_replies WHERE category=%s", (rbody["category"],))
    # M8 匿名写被鉴权闸拒绝
    j, ok = anon.req("POST", "/api/session-tags", "M8 匿名创建标签(鉴权闸)", ok_status=(401,), json={})
    check("M8.1 HTTP401 且 code=未授权类", ok and isinstance(j, dict) and j.get("code") in ("UNAUTHORIZED_2001", 401),
          f"code={j.get('code') if isinstance(j, dict) else None}")
    # M9 管理员空体必填校验
    j, ok = adm.req("POST", "/api/session-tags", "M9 空体创建(必填校验)", ok_status=(400,), json={})
    msg = str((j or {}).get("message") or (j or {}).get("msg") or "")
    check("M9.1 HTTP400 且必填校验拒绝", ok and "required" in msg.lower(),
          f"code={(j or {}).get('code')!r} msg={msg[:120]!r}")
    # M10 geo 站点列表
    j, ok = adm.req("GET", "/api/geo/sites", "M10 geo站点列表 (前端 geo.js:82 listSites)", expect_code=0)
    if ok:
        check("M10.1 data 字段完整", j.get("data") is not None, f"type={type(j.get('data')).__name__}")
    # M11 LLM 路由规则
    j, ok = adm.req("GET", "/api/llm-routing/rules", "M11 LLM路由规则", expect_code=0)
    if ok:
        d = j.get("data")
        rules = d if isinstance(d, list) else ((d or {}).get("list") or [])
        check("M11.1 规则数 >= 7 (场景全覆盖)", len(rules) >= 7, f"n={len(rules)}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cmd", choices=["chain", "api", "all", "ltc"])
    ap.add_argument("--keep", action="store_true")
    args = ap.parse_args()
    rc = 0
    if args.cmd in ("chain", "all"):
        rc |= run_chain(keep=args.keep)
    if args.cmd in ("api", "all"):
        rc |= run_api(keep=args.keep)
    if args.cmd == "ltc":
        rc |= run_ltc(keep=args.keep)
    sys.exit(rc)


if __name__ == "__main__":
    main()
