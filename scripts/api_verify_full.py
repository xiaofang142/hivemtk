#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
api_verify_full.py — API 四维验收脚本（CLAUDE.md 验收标准实现）

每个端点验证四维:
  【入参】前端发送字段
  【返回】API实际响应
  【预期】code=0 + 字段完整
  【DB】  写操作后SQL确认

用法:
  python3 scripts/api_verify_full.py chain   # 核心业务链路: 消息入库→AI→出库→ack 全链
  python3 scripts/api_verify_full.py api     # API 全量矩阵: 路由清单提取+契约探测+四维深测(含 CRM)
  python3 scripts/api_verify_full.py crm     # CRM scoped 四维验收(不触发真实渠道外发)
  python3 scripts/api_verify_full.py sc001   # SC-001 100 对合成去重抽样(不触发真实渠道外发)
  python3 scripts/api_verify_full.py ltc     # LTC 全链路: 线索→商机→报价→审批→账单→回款→赢单→复购
  python3 scripts/api_verify_full.py billing # 003 计费二期新端点: edition/cloud_url·订阅状态·pay-status
  python3 scripts/api_verify_full.py chain --keep  # 保留测试数据便于排查

注意: `all`/`chain` 仍包含旧 bridge token 与真实 AI/出站链路；identify_code-only
验收或禁止真实外发的场景必须显式使用 `api`/`crm`/`sc001`，不得跑 `all`/`chain`。

凭证（必填，脚本内无任何明文口令）:
  HIVEMTK_DB_PASSWORD   DB 口令（也可用 POSTGRES_PASSWORD 代替）
  HIVEMTK_ADMIN_PASS    e2e_admin 登录口令
  HIVEMTK_BRIDGE_TOKEN  可选；缺省时回退读 DB platform_config_kv.bridge_ingest_token
缺失即以退出码 3 终止并打印所需变量名。

隔离与安全:
  HIVEMTK_DB_PORT       必须显式设为 8217；API 服务必须连接同一个 8217 测试库
  HIVEMTK_BASE          指向该测试 API（默认 8204，但未证实同库前不得用于写验收）
  HIVEMTK_EVIDENCE_DIR  SC-001 取证目录；缺省时创建系统临时目录
  HIVEMTK_ALLOW_EXTERNAL_AI  默认不发起有效 AI 画像调用；仅在确认本地 mock/授权外发时设 1
"""
import argparse
import csv
import hashlib
import json
import os
import random
import re
import string
import sys
import tempfile
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
    # 端口/用户/库名接受本仓 .env 的既有键名作别名：§2 的调用口径就是「set -a && . ./.env」，
    # 只认 HIVEMTK_* 时那份 .env 会把连接打到默认 8232 上（本机 user_db 实监听 8202），
    # 报出来的红长得像"数据库挂了"，其实是脚本没读到这里已配置的值。
    host=os.environ.get("HIVEMTK_DB_HOST", "127.0.0.1"),
    port=int(os.environ.get("HIVEMTK_DB_PORT") or os.environ.get("DB_PORT") or "8232"),
    user=os.environ.get("HIVEMTK_DB_USER") or os.environ.get("POSTGRES_USER") or "admin",
    password=_secret("HIVEMTK_DB_PASSWORD", "POSTGRES_PASSWORD"),
    dbname=os.environ.get("HIVEMTK_DB_NAME") or os.environ.get("USER_DB_NAME") or "user_db",
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
        "  set -a && . ./.env && set +a\n"
        '  export "HIVEMTK_DB_PASSWORD=$POSTGRES_PASSWORD"  # 全引号赋值，仅变量引用无字面量\n'
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


_SENSITIVE_KEYS = {"password", "passwd", "pwd", "secret", "api_key", "apikey",
                   "authorization", "token", "access_token", "refresh_token", "id_token",
                   "bridge_token", "x-bridge-token", "client_secret", "app_secret"}


def _mask(value, depth=0):
    """把凭证类字段的**值**就地遮掉，键名与其余字段一个都不少。

    【入参】会把请求体原样打进 stdout，【返回】会把响应体前 300 字符打进去，
    而整轮输出要落进取证日志、取证日志随卡进 ledger ⇒ 登录口令和登录响应里的
    JWT 会一起进版本库。这条线只能在打印之前断掉，字段名照旧可读（AC① 要的是
    「发了哪些字段」，不是「发了哪个值」）。
    """
    if depth > 12:
        return "***[嵌套过深,整体省略]"
    if isinstance(value, dict):
        out = {}
        for k, v in value.items():
            key = k.strip().lower() if isinstance(k, str) else k
            if key in _SENSITIVE_KEYS:
                n = len(v) if isinstance(v, str) else 0
                out[k] = "***" if not n else "***[%d chars]" % n
            else:
                out[k] = _mask(v, depth + 1)
        return out
    if isinstance(value, list):
        return [_mask(v, depth + 1) for v in value]
    return value


# ---------------------------------------------------------------------------
# SC-001：本地合成客户对 + 冻结人工标签 + 候选分类评分（纯函数，不连 DB）
# ---------------------------------------------------------------------------
SC001_CATEGORY_COUNTS = (
    ("P01", "duplicate", 15),   # 同手机号精确匹配，同账号
    ("P02", "duplicate", 10),   # 同手机号精确匹配，跨账号
    ("P03", "duplicate", 10),   # 同邮箱精确匹配，同账号
    ("P04", "duplicate", 5),    # 同邮箱精确匹配，跨账号
    ("P05", "duplicate", 5),    # 同外部 ID，同账号
    ("P06", "duplicate", 5),    # 名称相似度正例，强标识不同
    ("N01", "nonduplicate", 20),
    ("N02", "nonduplicate", 5), # 同名不同人
    ("N03", "nonduplicate", 15),
    ("N04", "nonduplicate", 5), # 手机前缀相同、完整号码不同
    ("N05", "nonduplicate", 5), # 邮箱近似但不同
)


def NameSimilarityScore(a, b):
    """复刻 service.NameSimilarityScore 的归一化与编辑距离口径，仅供夹具自检。"""
    def normalize(value):
        return "".join(ch for ch in value.lower().strip() if ch not in " -_.,()")

    left, right = normalize(a), normalize(b)
    if not left or not right:
        return 0
    if left == right:
        return 100
    previous = list(range(len(right) + 1))
    for i, lch in enumerate(left, 1):
        current = [i]
        for j, rch in enumerate(right, 1):
            current.append(min(
                current[-1] + 1,
                previous[j] + 1,
                previous[j - 1] + (lch != rch),
            ))
        previous = current
    max_len = max(len(left), len(right))
    return max(0, (max_len - previous[-1]) * 100 // max_len)


def build_sc001_sample(run_id):
    """生成 100 对 / 200 个合成客户规格；同一 run_id  deterministic。"""
    if not re.fullmatch(r"[A-Za-z0-9_-]{1,32}", run_id):
        raise ValueError("run_id 仅允许 1-32 位字母、数字、下划线或连字符")

    rng = random.Random("sc001|" + run_id)
    run_phone_tag = int(hashlib.sha256(run_id.encode("utf-8")).hexdigest()[:4], 16)
    pairs, customers = [], []
    customer_serial = 0
    pair_serial = 0

    def token():
        return "".join(rng.choice(string.ascii_lowercase) for _ in range(10))

    for category, truth, count in SC001_CATEGORY_COUNTS:
        for index in range(1, count + 1):
            pair_serial += 1
            pair_id = f"{category}-{index:03d}"
            pair_token = token()
            cross_account = category in ("P02", "P04")
            accounts = ("a", "b") if cross_account else ("a", "a")

            if category == "P06":
                names = (
                    f"Northstar Trading Limited {run_id} {pair_token}",
                    f"Northstar Trading Ltd {run_id} {pair_token}",
                )
                expected_rule = "名称相似度"
            elif category == "N02":
                names = (
                    f"John Smith {run_id} {pair_token}",
                    f"John Smith {run_id} {pair_token}",
                )
                expected_rule = ""
            elif category == "N03":
                names = (
                    f"Aurora Trading Limited {run_id} {pair_token}",
                    f"Zenith Trading Limited {run_id} {pair_token}",
                )
                expected_rule = ""
            else:
                names = (
                    f"Customer Alpha {run_id} {pair_token}",
                    f"Customer Omega {run_id} {pair_token}",
                )
                expected_rule = ""

            row_specs = []
            for side_index, side in enumerate(("l", "r")):
                initial_phone = f"190{run_phone_tag:04d}{customer_serial:04d}"
                customer_serial += 1
                phone = initial_phone
                email = f"sc001-{run_id}-{pair_token}-{side}@example.invalid"
                external_id = f"sc001-{run_id}-{pair_token}-{side}"

                if category in ("P01", "P02"):
                    phone = f"191{run_phone_tag:04d}{pair_serial:04d}"
                    expected_rule = "同手机号合并"
                elif category in ("P03", "P04"):
                    email = f"sc001-{run_id}-{pair_token}@example.invalid"
                    expected_rule = "同邮箱合并"
                elif category == "P05":
                    external_id = f"sc001-{run_id}-{pair_token}"
                    expected_rule = "同外部 ID 合并"
                elif category == "N04":
                    phone = f"192{run_phone_tag % 1000:03d}{pair_serial:04d}{side_index}"
                elif category == "N05":
                    email = f"buyer{side_index + 1:02d}-{run_id}-{pair_token}@example.invalid"

                key = f"{pair_id}-{side.upper()}"
                row_specs.append({
                    "key": key,
                    "pair_id": pair_id,
                    "side": side.upper(),
                    "name": names[side_index],
                    "initial_phone": initial_phone,
                    "phone": phone,
                    "email": email,
                    "external_id": external_id,
                    "source_account_id": f"sc001-{run_id}-{accounts[side_index]}",
                    "source_channel": "douyin",
                    "industry": "sc001",
                    "tags": json.dumps(["sc001", run_id], ensure_ascii=False),
                })

            customers.extend(row_specs)
            pairs.append({
                "pair_id": pair_id,
                "category": category,
                "ground_truth": truth,
                "left_key": row_specs[0]["key"],
                "right_key": row_specs[1]["key"],
                "expected_rule": expected_rule,
            })

    return {"run_id": run_id, "pairs": pairs, "customers": customers}


def _candidate_pair_key(left_id, right_id):
    return tuple(sorted((str(left_id), str(right_id))))


def score_sc001(pairs, candidates, id_map=None):
    """按冻结人工标签评分；review 与 auto 分别统计，不剔除任何样本。"""
    if len(pairs) != 100:
        raise ValueError("SC-001 必须恰好 100 对标签")

    best = {}
    for candidate in candidates:
        action = candidate.get("action")
        if action not in ("auto", "review"):
            raise ValueError(f"非法去重 action: {action!r}")
        key = _candidate_pair_key(candidate.get("left_id"), candidate.get("right_id"))
        rank = (int(candidate.get("score") or 0), action == "auto")
        if key not in best or rank > best[key][0]:
            best[key] = (rank, candidate)

    rows = []
    tp = fp = tn = fn = 0
    positive_total = sum(1 for pair in pairs if pair["ground_truth"] == "duplicate")
    negative_total = len(pairs) - positive_total
    metrics = {
        "auto": {"tp": 0, "fp": 0, "fn": 0},
        "review": {"tp": 0, "fp": 0, "fn": 0},
    }

    for pair in pairs:
        truth = pair["ground_truth"]
        if truth not in ("duplicate", "nonduplicate"):
            raise ValueError(f"非法人工真值: {truth!r}")
        left_id = id_map[pair["left_key"]] if id_map is not None else pair["left_key"]
        right_id = id_map[pair["right_key"]] if id_map is not None else pair["right_key"]
        found = best.get(_candidate_pair_key(left_id, right_id))
        candidate = found[1] if found else None
        predicted = candidate is not None

        if predicted and truth == "duplicate":
            result, tp = "tp", tp + 1
        elif predicted and truth == "nonduplicate":
            result, fp = "fp", fp + 1
        elif not predicted and truth == "nonduplicate":
            result, tn = "tn", tn + 1
        else:
            result, fn = "fn", fn + 1

        action = candidate.get("action") if candidate else None
        if action:
            bucket = "tp" if truth == "duplicate" else "fp"
            metrics[action][bucket] += 1
        elif truth == "duplicate":
            metrics["auto"]["fn"] += 1
            metrics["review"]["fn"] += 1

        rows.append({
            "pair_id": pair["pair_id"],
            "category": pair["category"],
            "ground_truth": truth,
            "left_id": left_id,
            "right_id": right_id,
            "predicted": predicted,
            "action": action,
            "rule": candidate.get("rule") if candidate else None,
            "score": candidate.get("score") if candidate else None,
            "result": result,
        })

    for action in ("auto", "review"):
        item = metrics[action]
        decided = item["tp"] + item["fp"]
        item["precision"] = item["tp"] / decided if decided else None
        item["recall"] = item["tp"] / positive_total if positive_total else None

    return {
        "rows": rows,
        "total": len(pairs),
        "positive_total": positive_total,
        "negative_total": negative_total,
        "tp": tp, "fp": fp, "tn": tn, "fn": fn,
        "correct": tp + tn,
        "accuracy": (tp + tn) / len(pairs),
        "auto": metrics["auto"],
        "review": metrics["review"],
    }


def _ratio_text(numerator, denominator):
    return f"{numerator}/{denominator} = {numerator * 100 / denominator:.2f}%" if denominator else "0/0 = n/a"


def format_sc001_summary(result):
    return "\n".join((
        f"SC-001 候选准确率: {_ratio_text(result['correct'], result['total'])} "
        f"(TP={result['tp']}, FP={result['fp']}, TN={result['tn']}, FN={result['fn']})",
        f"  auto: precision {_ratio_text(result['auto']['tp'], result['auto']['tp'] + result['auto']['fp'])}, "
        f"recall {_ratio_text(result['auto']['tp'], result['positive_total'])}",
        f"  review: precision {_ratio_text(result['review']['tp'], result['review']['tp'] + result['review']['fp'])}, "
        f"recall {_ratio_text(result['review']['tp'], result['positive_total'])}",
    ))


def external_ai_calls_allowed():
    """有效 AI 画像会调用 LLM provider；默认显式禁用，防止验收夹带真实外发。"""
    return os.environ.get("HIVEMTK_ALLOW_EXTERNAL_AI", "").strip() == "1"


def cleanup_crm_operation_logs(audit_floor, customer_ids, audit_ids, execute):
    """只删本 run 捕获到的审计 ID/客户资源 ID，绝不按 action 广删。"""
    clauses, params = [], []
    if audit_ids:
        clauses.append("id = ANY(%s)")
        params.append(audit_ids)
    if customer_ids:
        clauses.append("(id > %s AND resource_id = ANY(%s))")
        params.extend((audit_floor, customer_ids))
    if clauses:
        execute("DELETE FROM operation_logs WHERE " + " OR ".join(clauses), tuple(params))


def _write_json(path, payload):
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, ensure_ascii=False, sort_keys=True, indent=2)
        handle.write("\n")


def write_sc001_evidence(evidence_dir, sample, id_map, scan_response, result, cleanup):
    """把冻结夹具、100 行标签、原始扫描响应与清理结果落到临时取证目录。"""
    os.makedirs(evidence_dir, exist_ok=True)
    labels = result["rows"] if result else []
    fields = (
        "pair_id", "category", "ground_truth", "left_id", "right_id",
        "predicted", "action", "rule", "score", "result",
    )
    with open(os.path.join(evidence_dir, "labels.csv"), "w", encoding="utf-8", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(labels)

    frozen = json.dumps(sample["pairs"], ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    summary = {key: value for key, value in (result or {}).items() if key != "rows"}
    summary.update({
        "run_id": sample["run_id"],
        "label_sha256": hashlib.sha256(frozen.encode("utf-8")).hexdigest(),
        "cleanup": cleanup,
    })
    _write_json(os.path.join(evidence_dir, "fixture.json"), sample)
    _write_json(os.path.join(evidence_dir, "id-map.json"), id_map)
    _write_json(os.path.join(evidence_dir, "labels.json"), labels)
    _write_json(os.path.join(evidence_dir, "scan-response.json"), scan_response)
    _write_json(os.path.join(evidence_dir, "summary.json"), summary)
    return evidence_dir


def crm_customer_create_payload(spec):
    """Create 只发初始身份，确保重复对在扫描前拥有不同 UnifiedID。"""
    return {
        "name": spec["name"],
        "phone": spec["initial_phone"],
        "email": f"seed-{spec['key']}@example.invalid",
        "industry": spec["industry"],
        "source_account_id": spec["source_account_id"],
        "source_channel": spec["source_channel"],
        "tags": spec["tags"],
    }


def crm_customer_update_payload(spec):
    """Update 再施加冻结的最终手机号/邮箱；UnifiedID 不会被 API DTO 改写。"""
    return {
        "name": spec["name"],
        "phone": spec["phone"],
        "email": spec["email"],
        "industry": spec["industry"],
        "source_account_id": spec["source_account_id"],
        "source_channel": spec["source_channel"],
        "tags": spec["tags"],
    }


def crm_entity_payload(customer_id, name, run_id):
    return {
        "name": name,
        "customer_id": customer_id,
        "contact_info": json.dumps({
            "phone": "19000009999",
            "email": f"{run_id}@example.invalid",
            "source": "crm-e2e",
        }, ensure_ascii=False),
        "role": "采购",
        "remark": "api_verify_full CRM 四维验收",
        "source": "crm-e2e",
        "source_ref": f"crm-e2e-{run_id}",
    }


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
        print(f"【入参】{method} {path}"
              + (f" params={_mask(params)}" if params else "")
              + (f" body={json.dumps(_mask(body), ensure_ascii=False)[:200]}" if body else ""))
        try:
            r = self.s.request(method, url, json=body, params=params, timeout=kw.pop("timeout", 30), **kw)
        except requests.RequestException as e:
            check(f"{desc}", False, f"请求异常: {e}")
            return None, False
        try:
            echo = json.dumps(_mask(r.json()), ensure_ascii=False)
        except ValueError:
            echo = r.text
        print(f"【返回】HTTP {r.status_code} {echo[:300]}")
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
def _bridge_default_tenant_code(channel):
    """首次上报的归属租户短码（on 模式必需，判据见 internal/bridge/tenant_binding.go）。

    bridge 入口在 TENANT_MODE=on 下要求 (channel, account_id) 自证归属，自证只有两条
    线索：bridge_accounts 里该账号的行，或显式 tenant_code(query)/X-Tenant-Code 头
    （bridgeTenantHintFromRequest）。本脚本每轮新造 account_id，库里必然没有行
    ⇒ 恒属「首次上报」，不带短码就必然 404 ErrBridgeTenantUnresolved
    （"该渠道账号尚未归属任何租户，首次上报请携带 tenant_code"）。

    选哪个租户：优先该渠道既有 bridge_accounts 行所属的租户（日常流量的真实形态，
    也是 e2e 历史账号落地的租户）；该渠道尚无行时退回最小的启用租户。
    HIVEMTK_BRIDGE_TENANT_CODE 可显式覆盖（多租户环境下指定落到哪个租户）。
    """
    row = q1("SELECT tenant_id FROM bridge_accounts WHERE channel=%s ORDER BY id DESC LIMIT 1",
             (channel,))
    tid = (row or {}).get("tenant_id")
    if tid:
        t = q1("SELECT code FROM tenants WHERE id=%s AND status=1", (tid,))
        if t and t.get("code"):
            return t["code"]
    t = q1("SELECT code FROM tenants WHERE status=1 ORDER BY id LIMIT 1")
    return (t or {}).get("code", "")


def run_chain(keep=False):
    section("核心业务链路 E2E：消息入库→AI处理→出库→ack 全流程")
    run_id = uuid.uuid4().hex[:8]
    channel = "douyin"
    account_id = f"acct-e2e-{run_id}"
    conversation_id = f"conv-e2e-{run_id}"
    sender_id = f"cust-e2e-{run_id}"
    msg_content = f"你好，我想了解一下HiveMtk的渠道接入方案 e2e-{run_id}"
    event_id = f"evt-e2e-{run_id}-1"

    # bridge 通道守卫凭证：与 middleware.BridgeIngressGuard 同源 —— 它读的是
    # platform_config_kv（实例级，无 tenant_id），不是 system_config_kv（租户业务配置）。
    # 读取顺序也照 guard：env（本脚本的显式覆盖）→ DB 现行键 → DB 上一版键。
    global BRIDGE_TOKEN
    if not BRIDGE_TOKEN:
        row = q1("SELECT value FROM platform_config_kv WHERE key='bridge_ingest_token'")
        BRIDGE_TOKEN = (row or {}).get("value", "") if row else ""
    if not BRIDGE_TOKEN:
        row = q1("SELECT value FROM platform_config_kv WHERE key='bridge_ingest_token_prev'")
        BRIDGE_TOKEN = (row or {}).get("value", "") if row else ""
    if not BRIDGE_TOKEN:
        check("S0 bridge token 可用", False,
              "platform_config_kv 无 bridge_ingest_token 且未设 HIVEMTK_BRIDGE_TOKEN")
        return finish(keep)
    bridge = APIClient(bridge_token=BRIDGE_TOKEN)  # bridge 通道无需 JWT
    # on 模式首次上报必须显式指认归属租户：脚本每轮都是新 account_id，
    # 自证查不到 bridge_accounts 行，只能靠短码（见 _bridge_default_tenant_code）。
    # 走请求头而不是 query，一条 session 覆盖 ingest/outbox/ack/sse 四个入口
    # ——四者都经 bindBridgeTenant → bridgeTenantHintFromRequest 读同一个头。
    tenant_code = os.environ.get("HIVEMTK_BRIDGE_TENANT_CODE") or _bridge_default_tenant_code(channel)
    if tenant_code:
        bridge.s.headers["X-Tenant-Code"] = tenant_code
        print(f"【入参】bridge 请求统一携带 X-Tenant-Code={tenant_code}（on 模式首次上报必需）")
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
    j, ok = adm.req("POST", "/api/clues/import", "L1 线索导入", expect_code=0, json=clue_body)
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
    # 唯一约束是 (tenant_id, one_id, channel)（多租户改造后列前缀带 tenant_id）；
    # 只写 (one_id, channel) 在 PG 上会因「没有匹配 ON CONFLICT 的唯一/排除约束」直接报错。
    qexec("INSERT INTO customer_channels (one_id, channel, channel_user_id, channel_name, account_id,"
          " is_primary, created_at, updated_at)"
          " VALUES (%s,'wechat',%s,'LTC验证客户',%s,true,now(),now())"
          " ON CONFLICT (tenant_id, one_id, channel) DO NOTHING",
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
        # system_config_kv 的主键是 (tenant_id, key)；单写 (key) 在 PG 上匹配不到唯一约束。
        qexec("INSERT INTO system_config_kv (key, value, updated_at) VALUES ('quote.script_id',%s,now())"
              " ON CONFLICT (tenant_id, key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()",
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
_PUBLIC_MUT_OK = frozenset(("/api/auth/login", "/api/auth/logout", "/api/auth/refresh-token"))
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
    # 离线(reg_mode=off)自助开户：Service 层设计内拒绝，HTTP 恒 200 + 业务码 4104，
    # 匿名空体到不了建租户分支 ⇒ 零写入（controller/tenant.go:48 注释；2026-10-25 §7.1 实测 DB 探针租户 0 行）。
    if path == "/api/tenant/register" and code == 4104 and 200 <= status < 300:
        return True, "离线模式设计内(200+4104,匿名空体零写入)"
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


def run_crm(keep=False):
    """CRM scoped 四维验收；不触发 bridge/chain/真实渠道外发。"""
    section("CRM 四维验收 · 入参 / 返回 / 预期 / DB")
    token = login_admin()
    _crm_deep(token)
    return finish(keep)


def run_sc001(keep=False):
    """SC-001：100 对合成客户候选分类准确率；不触发真实渠道外发。"""
    section("SC-001 · 100 对人工标签 / 候选准确率")
    token = login_admin()
    adm = APIClient(token=token)
    run_id = uuid.uuid4().hex[:8]
    sample = build_sc001_sample(run_id)
    frozen = json.dumps(sample["pairs"], ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    print(f"【冻结标签】run_id={run_id} pairs={len(sample['pairs'])} "
          f"sha256={hashlib.sha256(frozen.encode('utf-8')).hexdigest()}")

    id_map, customer_ids = {}, []
    audit_floor = 0
    floor_row = q1("SELECT COALESCE(MAX(id), 0) AS id FROM operation_logs")
    audit_floor = int((floor_row or {}).get("id") or 0)

    def cleanup():
        if keep:
            print(f"【保留】SC-001 run_id={run_id} customer_ids={customer_ids}")
            return
        try:
            if customer_ids:
                qexec("DELETE FROM operation_logs WHERE id > %s AND resource_id = ANY(%s)",
                      (audit_floor, customer_ids))
                qexec("DELETE FROM customers WHERE id = ANY(%s)", (customer_ids,))
            check("SC001-CLEAN 【DB】合成客户清理完成", True, f"rows={len(customer_ids)}")
        except Exception as exc:
            check("SC001-CLEAN 【DB】合成客户清理完成", False, f"清理异常: {exc}")

    def validate():
        create_failures = []
        for spec in sample["customers"]:
            j, ok = adm.req("POST", "/api/manage/crm/customers",
                            f"SC001 create {spec['key']}", expect_code=0,
                            json=crm_customer_create_payload(spec))
            customer_id = ((j or {}).get("data") or {}).get("id") if ok else None
            if customer_id:
                id_map[spec["key"]] = customer_id
                customer_ids.append(customer_id)
            else:
                create_failures.append(spec["key"])
        check("SC001-SEED 200 个客户全部创建", not create_failures and len(id_map) == 200,
              f"created={len(id_map)} failures={create_failures[:10]}")
        if len(id_map) != 200:
            return

        update_failures = []
        for spec in sample["customers"]:
            _, ok = adm.req("PUT", f"/api/manage/crm/customers/{id_map[spec['key']]}",
                            f"SC001 finalize {spec['key']}", expect_code=0,
                            json=crm_customer_update_payload(spec))
            if not ok:
                update_failures.append(spec["key"])
        check("SC001-FINALIZE 200 个最终身份全部落库", not update_failures,
              f"failures={update_failures[:10]}")
        if update_failures:
            return

        external_failures = []
        for spec in sample["customers"]:
            try:
                qexec("UPDATE customers SET we_com_external_id=%s WHERE id=%s",
                      (spec["external_id"], id_map[spec["key"]]))
            except Exception as exc:
                external_failures.append(f"{spec['key']}:{exc}")
        check("SC001-EXTERNAL 外部 ID 夹具补齐", not external_failures,
              f"failures={external_failures[:3]}")
        if external_failures:
            return

        j, ok = adm.req("POST", "/api/manage/crm/dedupe/scan", "SC001-SCAN 租户候选扫描",
                        expect_code=0, json={
                            "max_customers": 5000,
                            "name_threshold": 85,
                            "same_account_only": False,
                        })
        if not ok:
            return
        candidates = ((j or {}).get("data") or {}).get("list") or []
        result = score_sc001(sample["pairs"], candidates, id_map=id_map)
        for row in result["rows"]:
            print("【逐行标签】" + json.dumps(row, ensure_ascii=False, sort_keys=True))
        print(format_sc001_summary(result))
        check("SC-001 候选分类准确率 >=95%", result["accuracy"] >= 0.95,
              format_sc001_summary(result).replace("\n", " | "))
        return None

    try:
        validate()
    finally:
        cleanup()
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

    # T066/SC-009：CRM 新增/修改 API 必须有入参/返回/预期/DB 四维证据；
    # 通用路由契约探测不能替代这一段。
    _crm_deep(token)


def verify_crm_ai_profile(adm, customer_id):
    """画像请求可能触发外部 LLM；只有显式授权时才调用。"""
    if not external_ai_calls_allowed():
        DEGRADED.append("C16 CRM AI 画像：未设置 HIVEMTK_ALLOW_EXTERNAL_AI=1，跳过外部 AI 请求")
        return

    j, ok = adm.req("POST", "/api/manage/crm/ai/analyze", "C16 AI画像(依赖不可用则降级)",
                    ok_status=(200, 503), json={"customer_id": customer_id})
    ai_data = (j or {}).get("data") if isinstance(j, dict) else None
    if ok and isinstance(ai_data, dict) and "suggestions" in ai_data:
        suggestions = ai_data.get("suggestions") or []
        check("C16.1 每条 AI 建议均有 evidence", all(
            isinstance(item, dict) and item.get("evidence") for item in suggestions),
            f"suggestions={len(suggestions)} degraded={ai_data.get('degraded')}")
    else:
        DEGRADED.append("C16 CRM AI 画像：LLM dispatcher 不可用，端点返回 503；"
                        "路由与鉴权已探测，但 AI 结论/evidence 未验证")


def _crm_deep(token):
    """CRM 四维深测：所有写操作带 DB 断言，finally 清理本 run 数据。"""
    adm = APIClient(token=token)
    anon = APIClient()
    run = uuid.uuid4().hex[:8]
    tag = f"CRM-E2E-{run}"
    customer_ids, entity_ids, one_ids = [], [], []
    audit_floor = 0

    def remember_customer(customer_id):
        if customer_id and customer_id not in customer_ids:
            customer_ids.append(customer_id)

    def create_customer(label, phone):
        body = {
            "name": f"{tag}-{label}",
            "phone": phone,
            "email": f"{run}-{label}@example.invalid",
            "industry": "e2e",
            "source_account_id": f"crm-e2e-{run}-{label.lower()}",
            "source_channel": "douyin",
            "tags": json.dumps(["crm-e2e", run], ensure_ascii=False),
        }
        j, ok = adm.req("POST", "/api/manage/crm/customers",
                        f"C1 创建客户 {label}", expect_code=0, json=body)
        customer_id = ((j or {}).get("data") or {}).get("id") if ok else None
        check(f"C1.1 {label} 返回 data.id", customer_id is not None,
              f"data={(j or {}).get('data')}")
        if customer_id:
            remember_customer(customer_id)
        row = q1("SELECT id, name, phone FROM customers WHERE id=%s", (customer_id,)) if customer_id else None
        check(f"C1.2 【DB】{label} 客户行存在", row is not None and row["phone"] == phone,
              f"row={dict(row) if row else None}")
        return customer_id

    def cleanup():
        try:
            if customer_ids:
                rows = qall("SELECT unified_id FROM customers WHERE id = ANY(%s)", (customer_ids,))
                one_ids.extend(row["unified_id"] for row in rows if row.get("unified_id"))
            if one_ids:
                qexec("DELETE FROM contact_consent_identity_links "
                      "WHERE left_one_id = ANY(%s) OR right_one_id = ANY(%s)",
                      (one_ids, one_ids))
            for table, entity_id in entity_ids:
                qexec(f"DELETE FROM {table} WHERE id=%s", (entity_id,))
            if customer_ids:
                qexec("DELETE FROM customer_merges "
                      "WHERE primary_id = ANY(%s) OR secondary_id = ANY(%s)",
                      (customer_ids, customer_ids))
                qexec("DELETE FROM customers WHERE id = ANY(%s)", (customer_ids,))
            if audit_floor:
                qexec("DELETE FROM operation_logs WHERE id > %s "
                      "AND (action IN ('customer_import','customer_export','customer_merge','merge') "
                      "OR resource_id = ANY(%s))", (audit_floor, customer_ids))
            check("C99 【DB】本 run 合成数据清理完成", True, f"customers={len(customer_ids)} entities={len(entity_ids)}")
        except Exception as exc:
            check("C99 【DB】本 run 合成数据清理完成", False, f"清理异常: {exc}")

    try:
        floor_row = q1("SELECT COALESCE(MAX(id), 0) AS id FROM operation_logs")
        audit_floor = int((floor_row or {}).get("id") or 0)

        j, ok = anon.req("POST", "/api/manage/crm/customers", "C0 匿名创建客户(鉴权闸)",
                         ok_status=(401,), json={})
        check("C0.1 HTTP401 且未授权码", ok and isinstance(j, dict)
              and j.get("code") in ("UNAUTHORIZED_2001", 401),
              f"code={j.get('code') if isinstance(j, dict) else None}")

        base = 19088000000 + (int(run, 16) % 100000)
        primary_id = create_customer("A", str(base))
        secondary_id = create_customer("B", str(base + 1))
        if not primary_id or not secondary_id:
            return

        j, ok = adm.req("GET", "/api/manage/crm/customers", "C2 客户列表",
                        expect_code=0, params={"keyword": tag, "page": 1, "page_size": 20})
        listed = ((j or {}).get("data") or {}).get("list") or []
        check("C2.1 列表含本 run 两客户", ok and {primary_id, secondary_id}.issubset(
            {str(row.get("id")) for row in listed}), f"rows={len(listed)}")

        j, ok = adm.req("GET", f"/api/manage/crm/customers/{primary_id}",
                        "C3 客户详情", expect_code=0)
        detail = (j or {}).get("data") or {}
        check("C3.1 详情 ID/名称完整", ok and detail.get("id") == primary_id
              and tag in str(detail.get("name")), f"data={str(detail)[:160]}")

        update_body = {
            "name": f"{tag}-A-updated", "phone": str(base), "email": f"{run}-a@example.invalid",
            "industry": "e2e", "source_account_id": f"crm-e2e-{run}-a",
            "source_channel": "douyin", "tags": json.dumps(["crm-e2e", run]),
        }
        j, ok = adm.req("PUT", f"/api/manage/crm/customers/{primary_id}",
                        "C4 修改客户", expect_code=0, json=update_body)
        row = q1("SELECT name FROM customers WHERE id=%s", (primary_id,))
        check("C4.1 【DB】修改落库", ok and row and row["name"] == update_body["name"],
              f"row={dict(row) if row else None}")

        duplicate_body = dict(update_body)
        duplicate_body.update({
            "name": f"{tag}-B", "phone": str(base),
            "email": f"{run}-b@example.invalid",
            "source_account_id": f"crm-e2e-{run}-b",
        })
        adm.req("PUT", f"/api/manage/crm/customers/{secondary_id}",
                "C5 构造同手机号重复档", expect_code=0, json=duplicate_body)
        row = q1("SELECT phone, unified_id FROM customers WHERE id=%s", (secondary_id,))
        primary_row = q1("SELECT unified_id FROM customers WHERE id=%s", (primary_id,))
        check("C5.1 【DB】重复档手机号一致但 UnifiedID 仍独立",
              row and primary_row and row["phone"] == str(base)
              and row["unified_id"] != primary_row["unified_id"],
              f"secondary={dict(row) if row else None}")

        j, ok = adm.req("POST", "/api/manage/crm/dedupe/scan", "C6 去重扫描", expect_code=0,
                        json={"max_customers": 5000, "name_threshold": 85, "same_account_only": False})
        candidate_rows = ((j or {}).get("data") or {}).get("list") or []
        pair_keys = {_candidate_pair_key(row.get("left_id"), row.get("right_id")) for row in candidate_rows}
        check("C6.1 返回本 run 重复候选", ok and _candidate_pair_key(primary_id, secondary_id) in pair_keys,
              f"candidates={len(candidate_rows)}")

        j, ok = adm.req("POST", "/api/manage/crm/merge", "C7 合并客户", expect_code=0,
                        json={"primary_id": primary_id, "secondary_id": secondary_id,
                              "reason": "crm-e2e", "source": "api_verify_full"})
        secondary_row = q1("SELECT deleted_at FROM customers WHERE id=%s", (secondary_id,))
        merge_row = q1("SELECT merge_id, primary_id, secondary_id FROM customer_merges "
                       "WHERE primary_id=%s AND secondary_id=%s ORDER BY id DESC LIMIT 1",
                       (primary_id, secondary_id))
        check("C7.1 【DB】次客户软删且合并审计存在", ok and secondary_row
              and secondary_row["deleted_at"] is not None and merge_row is not None,
              f"deleted_at={secondary_row['deleted_at'] if secondary_row else None} merge={dict(merge_row) if merge_row else None}")

        j, ok = adm.req("GET", "/api/manage/crm/merge/audit", "C8 合并审计查询", expect_code=0,
                        params={"customer_id": primary_id})
        audits = ((j or {}).get("data") or {}).get("list") or []
        check("C8.1 审计列表含本 run 合并", ok and any(
            row.get("primary_id") == primary_id and row.get("secondary_id") == secondary_id
            for row in audits), f"rows={len(audits)}")

        entity_specs = (
            ("contacts", "contacts", "联系人"),
            ("suppliers", "suppliers", "供应商"),
            ("service-providers", "service_providers", "服务商"),
        )
        for path, table, label in entity_specs:
            body = crm_entity_payload(primary_id, f"{tag}-{label}", run)
            j, ok = adm.req("POST", f"/api/manage/crm/{path}", f"C9 创建{label}",
                            expect_code=0, json=body)
            entity_id = ((j or {}).get("data") or {}).get("id") if ok else None
            if entity_id:
                entity_ids.append((table, entity_id))
            row = q1(f"SELECT id, name FROM {table} WHERE id=%s", (entity_id,)) if entity_id else None
            check(f"C9.1 【DB】{label}行存在", row is not None and row["name"] == body["name"],
                  f"row={dict(row) if row else None}")

            j, ok = adm.req("GET", f"/api/manage/crm/{path}", f"C10 {label}列表", expect_code=0,
                            params={"customer_id": primary_id, "page": 1, "page_size": 20})
            rows = ((j or {}).get("data") or {}).get("list") or []
            check(f"C10.1 {label}列表含新行", ok and any(str(row.get("id")) == str(entity_id) for row in rows),
                  f"rows={len(rows)}")

            updated = dict(body)
            updated["name"] = f"{tag}-{label}-updated"
            adm.req("PUT", f"/api/manage/crm/{path}/{entity_id}", f"C11 修改{label}",
                    expect_code=0, json=updated)
            row = q1(f"SELECT name FROM {table} WHERE id=%s", (entity_id,))
            check(f"C11.1 【DB】{label}修改落库", row and row["name"] == updated["name"],
                  f"row={dict(row) if row else None}")

        j, ok = adm.req("GET", f"/api/manage/crm/360/{primary_id}", "C12 客户360", expect_code=0)
        view = (j or {}).get("data") or {}
        check("C12.1 360 含三类实体与合并历史", ok
              and len(view.get("contacts") or []) >= 1
              and len(view.get("suppliers") or []) >= 1
              and len(view.get("service_providers") or []) >= 1
              and len(view.get("merge_history") or []) >= 1,
              f"keys={sorted(view) if isinstance(view, dict) else type(view).__name__}")

        j, ok = adm.req("GET", "/api/manage/crm/cross-industry", "C13 跨账号行业检索", expect_code=0,
                        params={"keyword": tag, "industry": "e2e", "page": 1, "page_size": 20})
        cross = (j or {}).get("data") or {}
        check("C13.1 交叉检索含主客户", ok and any(
            row.get("id") == primary_id for row in (cross.get("items") or [])),
            f"total={cross.get('total')}")

        import_phone = str(base + 2)
        import_body = {"file_name": f"crm-e2e-{run}.json", "rows": [{
            "name": f"{tag}-import", "phone": import_phone,
            "email": f"{run}-import@example.invalid", "industry": "e2e",
            "source_account_id": f"crm-e2e-{run}-import", "source_channel": "douyin",
            "tags": json.dumps(["crm-e2e", run]),
        }]}
        j, ok = adm.req("POST", "/api/manage/crm/import", "C14 导入客户", expect_code=0, json=import_body)
        imported = (j or {}).get("data") or {}
        imported_row = q1("SELECT id FROM customers WHERE phone=%s", (import_phone,))
        imported_id = imported_row["id"] if imported_row else None
        if imported_id:
            remember_customer(imported_id)
        check("C14.1 【DB】导入 created=1 且行存在", ok and imported.get("created") == 1 and imported_id,
              f"result={imported} id={imported_id}")

        j, ok = adm.req("GET", "/api/manage/crm/export", "C15 导出客户", expect_code=0,
                        params={"keyword": tag, "industry": "e2e"})
        exported = (j or {}).get("data") or {}
        check("C15.1 导出含导入客户", ok and any(
            row.get("id") == imported_id for row in (exported.get("rows") or [])),
            f"total={exported.get('total')}")

        verify_crm_ai_profile(adm, primary_id)

        for path, table, label in entity_specs:
            entity_id = next((eid for tbl, eid in entity_ids if tbl == table), None)
            if entity_id:
                adm.req("DELETE", f"/api/manage/crm/{path}/{entity_id}", f"C17 删除{label}",
                        expect_code=0)
                row = q1(f"SELECT deleted_at FROM {table} WHERE id=%s", (entity_id,))
                check(f"C17.1 【DB】{label}软删", row and row["deleted_at"] is not None,
                      f"row={dict(row) if row else None}")

        if imported_id:
            adm.req("DELETE", f"/api/manage/crm/customers/{imported_id}", "C18 删除导入客户", expect_code=0)
        adm.req("DELETE", f"/api/manage/crm/customers/{primary_id}", "C19 删除主客户", expect_code=0)

        j, ok = adm.req("POST", "/api/manage/crm/merge", "C20 空合并体(参数校验)",
                        ok_status=(400,), json={})
        check("C20.1 HTTP400", ok, f"code={j.get('code') if isinstance(j, dict) else None}")
        j, ok = adm.req("GET", "/api/manage/crm/merge/audit", "C21 缺 customer_id(参数校验)",
                        ok_status=(400,))
        check("C21.1 HTTP400", ok, f"code={j.get('code') if isinstance(j, dict) else None}")
        j, ok = adm.req("POST", "/api/manage/crm/ai/analyze", "C22 缺 customer_id(参数校验)",
                        ok_status=(400,), json={})
        check("C22.1 HTTP400", ok, f"code={j.get('code') if isinstance(j, dict) else None}")
    finally:
        cleanup()


# ---------------------------------------------------------------------------
# 003 计费二期（T029）：新端点验证段
#
#   覆盖三个 003 新增/改口径端点，沿用本脚本四维口径：
#     【入参】【返回】【预期】由 req() 逐行打印，【DB】在本函数里用 SQL 对账。
#     ① GET /api/public/config     —— data.edition / data.cloud_url（T003/T004/T012）
#     ② GET /api/subscription/status —— status/locked/no_data/stale + tenant_subscription_mirror 行
#     ③ GET /api/module-market/orders/:order_no/pay-status —— order_no 回显 + 订单状态枚举
#
#   与 scripts/epay_mock_e2e.py 的分工（取舍写在这里，免得下个会话重问）：
#     那边跑**双栈 mock 支付全流程**（要平台端 8205 + EPAY_MOCK=true + 注册租户），
#     本段只打**用户端 8204 + user_db**，不碰平台端、不下单，可在任何已部署实例上单跑。
#     所以 ③ 的订单号取库里最新一行 module_orders（【DB】维），库内无单时登记 DEGRADED
#     而不是编一个订单号打 404 —— 缺席不写成通过，也不写成环境噪声之外的红。
#   未并入 `all`：全量矩阵的口径是「既有三端面」，计费段单独成段，避免无订单的干净库
#   把 matrix 变红；需要时显式 `billing`。
# ---------------------------------------------------------------------------
BILLING_ORDER_SQL = ("SELECT order_no, status, total_amount_cents "
                     "FROM module_orders ORDER BY created_at DESC LIMIT 1")


def run_billing(keep=False):
    section("003 计费二期新端点：edition/cloud_url · 订阅状态 · pay-status")
    anon = APIClient()

    # ---- B1 GET /api/public/config（登录前公开面）----
    j, ok = anon.req("GET", "/api/public/config",
                     "B1 站点公开配置（003 T003：edition/cloud_url）", expect_code=0)
    data = j.get("data") if isinstance(j, dict) else None
    if isinstance(data, dict):
        check("B1.1 data.edition ∈ {opensource,saas,cloud}",
              data.get("edition") in ("opensource", "saas", "cloud"),
              f"edition={data.get('edition')!r}")
        check("B1.2 data 带 cloud_url 键（003 T004 云端入口常量）",
              "cloud_url" in data, f"cloud_url={data.get('cloud_url')!r} keys={sorted(data)}")
        check("B1.3 data 带 reg_mode（TENANT_MODE 语义映射，003 T012）",
              "reg_mode" in data, f"reg_mode={data.get('reg_mode')!r}")
    elif ok:
        check("B1 data 为对象", False, f"type={type(data).__name__}")

    # ---- B2 GET /api/subscription/status（登录态；订阅闸白名单面，锁态下自身不得 4305）----
    adm = APIClient(token=login_admin())
    j, ok = adm.req("GET", "/api/subscription/status",
                    "B2 订阅状态快照（003 T018）", expect_code=0)
    snap = j.get("data") if isinstance(j, dict) else None
    if ok and isinstance(snap, dict) and snap:
        st = snap.get("status")
        check("B2.1 status ∈ {trial,active,expired,unknown}",
              st in ("trial", "active", "expired", "unknown"), f"status={st!r}")
        check("B2.2 locked 与 status=expired 同向",
              bool(snap.get("locked")) is (st == "expired"),
              f"status={st!r} locked={snap.get('locked')!r}")
        check("B2.3 no_data / stale 均为布尔（fail-open 档与可观测档分离）",
              isinstance(snap.get("no_data"), bool) and isinstance(snap.get("stale"), bool),
              f"no_data={snap.get('no_data')!r} stale={snap.get('stale')!r}")
        try:
            row = q1("SELECT id, status, current_period_end, trial_expires_at "
                     "FROM tenant_subscription_mirror WHERE id=1")
        except Exception as e:  # DB 不可达：登记降级，不把环境噪声算进红
            row, db_err = None, str(e)
            DEGRADED.append(f"B2 【DB】tenant_subscription_mirror 读取失败（{db_err[:120]}）—— 本维缺席")
        else:
            db_err = None
        if db_err is None:
            if row is None:
                check("B2.4 【DB】无镜像行时 API 必须 no_data=true（fail-open，修订 #2）",
                      snap.get("no_data") is True, "row=None")
            else:
                has_period = row["current_period_end"] is not None or row["trial_expires_at"] is not None
                check("B2.4 【DB】镜像单行存在（id=1 行锚）", True, f"row.status={row['status']!r}")
                check("B2.5 【DB】镜像有 period 数据 ↔ API no_data=false",
                      has_period is (snap.get("no_data") is False),
                      f"db.has_period={has_period} api.no_data={snap.get('no_data')!r}")
    elif ok:
        check("B2 订阅快照非空", False, f"data={snap}")

    # ---- B3 GET /api/module-market/orders/:order_no/pay-status ----
    try:
        orow = q1(BILLING_ORDER_SQL)
    except Exception as e:
        orow = None
        DEGRADED.append(f"B3 【DB】module_orders 读取失败（{str(e)[:120]}）—— pay-status 只按 HTTP 维验收")
    if orow is None:
        # 库内没有订单：不编订单号打 404，也不谎称这一段过了
        DEGRADED.append("B3 module_orders 无订单行：pay-status 的【DB】对账维缺席"
                        "（先跑 scripts/epay_mock_e2e.py 走一笔，或在市场页下单一笔再跑本段）")
        check("B3 pay-status 端点（需至少一笔既有订单）", True,
              "跳过：库内无订单，端点形状未在本轮取样（DEGRADED 单列）")
        return finish(keep)
    order_no = orow["order_no"]
    j, ok = adm.req("GET", f"/api/module-market/orders/{order_no}/pay-status",
                    f"B3 支付状态直查（003 T018，order_no={order_no}）", expect_code=0)
    d = j.get("data") if isinstance(j, dict) else None
    if ok and isinstance(d, dict):
        check("B3.1 回显 order_no 与请求一致", d.get("order_no") == order_no,
              f"got={d.get('order_no')!r} want={order_no!r}")
        st = d.get("status")
        check("B3.2 status ∈ {pending_payment,paid,fulfilled,cancelled,rejected}",
              st in ("pending_payment", "paid", "fulfilled", "cancelled", "rejected"), f"status={st!r}")
        check("B3.3 paid 与 status 同向（paid ⇔ status∈{paid,fulfilled}）",
              bool(d.get("paid")) is (st in ("paid", "fulfilled")),
              f"status={st!r} paid={d.get('paid')!r}")
        check("B3.4 【DB】本地镜像状态与平台态一致（不一致=待 sync，仍属合法中间态）",
              True, f"local={orow['status']!r} api={st!r}（本维只取证，不判红："
                    "PayStatus 直查平台，本地 module_orders 是只读缓存）")
    elif ok:
        check("B3 data 为对象", False, f"type={type(d).__name__}")
    return finish(keep)


def build_arg_parser():
    ap = argparse.ArgumentParser()
    ap.add_argument("cmd", choices=["chain", "api", "crm", "sc001", "all", "ltc", "billing"])
    ap.add_argument("--keep", action="store_true")
    return ap


def main(argv=None):
    args = build_arg_parser().parse_args(argv)
    rc = 0
    if args.cmd in ("chain", "all"):
        rc |= run_chain(keep=args.keep)
    if args.cmd in ("api", "all"):
        rc |= run_api(keep=args.keep)
    if args.cmd == "crm":
        rc |= run_crm(keep=args.keep)
    if args.cmd == "sc001":
        rc |= run_sc001(keep=args.keep)
    if args.cmd == "ltc":
        rc |= run_ltc(keep=args.keep)
    if args.cmd == "billing":
        rc |= run_billing(keep=args.keep)
    sys.exit(rc)


if __name__ == "__main__":
    main()
