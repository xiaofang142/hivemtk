#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""cs01_post_upgrade_recheck.py — CS-01「升级后再验」黑盒复测脚本（CS-P1-② 本机先行执行包）。

用途（CS-P1 发布窗口纪律）：user-server 升级/发布后，对运行中实例复跑 CS-01 可观测不变量
（三样本安全面/去重面/身份面的 HTTP 可判定子集），与升级前基线对账。

口径（对齐 CS-D2 测量规格，docs/未完成任务清单.md §一 CS-R1 行「测量规格（冻结不变）」）：
  · 缺回执/不可判定 = UNDETERMINED，不写 0 不冒充通过；
  · 每项检查给出 依据（期望↔实际）；失败=FAIL 立列；rc 语义：0=无 FAIL（UNDETERMINED 允许并逐条列出）、
    1=存在 FAIL、2=使用错误；--strict 时 UNDETERMINED 亦 rc=1；
  · 本脚本只读探测（GET/带签名的 POST 探针），不发真实客户消息、不写业务数据；
  · 口令/token 只从参数/环境读取，不落盘、不进输出。

检查面（每项标注 CS-01 规格出处，§3.10.2 规格块）：
  A  存活基线            GET /health → 200（升级后第一闸）
  B  身份面·缺 token     受保护端点无 JWT → 401（身份安全测试：缺 token 拒绝、无绑定副作用）
  C  身份面·坏 token     伪造 JWT → 401（恶意 token 拒绝）
  D  安全面·回调缺签     POST /api/integration/webhook/order/<platform> 无签名头 → 400/401
                         （webhook_signature.go 分档；缺头/坏 nonce/坏 ts=400、错签=401）
  E  安全面·回调伪签     同上但签名错误 → 401（需 --webhook-secret 才可构造；缺参=UNDETERMINED）
  F  安全面·回调重放     合法帧重投第二次 → 409（checkReplay；需 --webhook-secret；UNDETERMINED 同上）
  G  去重面·ingress 重放 需访客会话 fixture（CS-R1 授权窗口），本脚本不内置 → UNDETERMINED（如实登记）
     —— G 的可执行形态在 CS-R1 窗口由 G-UI/ego 按三样本执行，此处只占位声明不冒充。

用法：
  python3 cs01_post_upgrade_recheck.py --self-test            # 离线自检（内置 canned 响应，含负例）
  python3 cs01_post_upgrade_recheck.py --base-url https://host \
      [--token JWT] [--probe-auth-path /api/llm/platform-usage] \
      [--webhook-platform wechat] [--webhook-secret SECRET] [--webhook-body '{}'] [--strict] [--json]

退出码：0=无 FAIL；1=存在 FAIL（--strict 时含 UNDETERMINED）；2=用法/参数错误。
"""

import argparse
import hashlib
import hmac
import json
import sys
import time
import urllib.error
import urllib.request

UA = "cs01-post-upgrade-recheck/1.0"
TIMEOUT = 15


# ---------------------------------------------------------------- 传输层（可注入，供 --self-test 替换）
class Transport:
    def request(self, method, url, headers=None, body=None):
        """返回 (status:int|None, body_text:str|None, error:str|None)。网络异常时 status=None。"""
        req = urllib.request.Request(url, data=(body.encode() if body else None), method=method)
        for k, v in (headers or {}).items():
            req.add_header(k, v)
        req.add_header("User-Agent", UA)
        try:
            with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
                return r.status, r.read(65536).decode("utf-8", "replace"), None
        except urllib.error.HTTPError as e:
            try:
                return e.code, e.read(65536).decode("utf-8", "replace"), None
            except Exception:
                return e.code, None, None
        except Exception as e:  # 网络不可达/DNS/超时 → UNDETERMINED，不写 0
            return None, None, "%s: %s" % (type(e).__name__, e)


def http_get(t, url, headers=None):
    status, _body, err = t.request("GET", url, headers=headers)
    return status, err


def http_post(t, url, headers=None, body=""):
    status, _body, err = t.request("POST", url, headers=headers, body=body)
    return status, err


# ---------------------------------------------------------------- 判定层（纯函数，--self-test 直测）
def judge(check_id, expect, actual, note=""):
    """expect: (ok_codes:set, fail_codes:set)；actual: (status, err)。返回结果 dict。
    网络错误/状态缺失=UNDETERMINED（不写 0）；命中 ok=PASS；命中 fail 或其余 2xx/4xx=FAIL。"""
    status, err = actual
    ok_codes, fail_codes = expect
    if err is not None or status is None:
        return {"id": check_id, "verdict": "UNDETERMINED", "status": None, "note": (note + " " if note else "") + "传输失败=" + str(err)}
    if status in ok_codes:
        return {"id": check_id, "verdict": "PASS", "status": status, "note": note}
    if status in fail_codes:
        return {"id": check_id, "verdict": "FAIL", "status": status, "note": note}
    return {"id": check_id, "verdict": "FAIL", "status": status, "note": (note + " " if note else "") + "状态码落在期望集外"}


def check_health(t, base):
    return judge("A-health", ({200}, {502, 503}), http_get(t, base.rstrip("/") + "/health"),
                 "升级后存活基线（CS-01 关闭基架前提）")


def check_auth_missing(t, base, path):
    return judge("B-auth-missing", ({401, 403}, {200}), http_get(t, base.rstrip("/") + path),
                 "受保护端点缺 JWT 必须 401/403（CS-01 身份安全：缺 token 拒绝）；200=鉴权失效")


def check_auth_bad_token(t, base, path):
    bad = {"Authorization": "Bearer cs01-recheck-invalid.token.value"}
    return judge("C-auth-bad-token", ({401, 403}, {200}), http_get(t, base.rstrip("/") + path, headers=bad),
                 "伪造 JWT 必须 401/403（CS-01 身份安全：恶意 token 拒绝、失败无绑定副作用）")


def _sig_headers(secret, body, ts=None, nonce=None, good=True):
    ts = ts or str(int(time.time()))
    nonce = nonce or "cs01recheck-nonce-0001"
    mac = hmac.new(secret.encode(), (ts + "." + nonce + "." + body).encode(), hashlib.sha256).hexdigest()
    if not good:
        mac = ("0" * 64 if mac[0] != "0" else "1" * 64)
    return {"Content-Type": "application/json", "X-Webhook-Timestamp": ts, "X-Webhook-Nonce": nonce,
            "X-Webhook-Signature": mac}


def check_webhook_missing_sig(t, base, platform):
    url = base.rstrip("/") + "/api/integration/webhook/order/" + platform
    return judge("D-webhook-missing-sig", ({400, 401}, {200}), http_post(t, url, body="{}"),
                 "回调公开入口缺签名头必须 400/401（webhook_signature.go 分档；200=fail-open 事故）")


def check_webhook_bad_sig(t, base, platform, secret, body):
    url = base.rstrip("/") + "/api/integration/webhook/order/" + platform
    return judge("E-webhook-bad-sig", ({401, 400}, {200}), http_post(t, url, headers=_sig_headers(secret, body, good=False), body=body),
                 "伪签必须 401/400 拒绝且不回显期望签名（CS-01 安全负例）")


def check_webhook_replay(t, base, platform, secret, body):
    url = base.rstrip("/") + "/api/integration/webhook/order/" + platform
    h = _sig_headers(secret, body)
    first = http_post(t, url, headers=h, body=body)
    if first[0] not in (200, 409):
        return {"id": "F-webhook-replay", "verdict": "UNDETERMINED", "status": first[0],
                "note": "首投未获 200/409（可能缺 fixture 单据），重放语义不可判定——不写 0"}
    return judge("F-webhook-replay", ({409}, {200}), http_post(t, url, headers=h, body=body),
                 "同 nonce+ts+签名重投必须 409（checkReplay；CS-01 去重矩阵：原帧重放不增副作用）")


# ---------------------------------------------------------------- 自检（canned 响应，双极性）
class CannedTransport(Transport):
    def __init__(self, table):
        self.table = table  # (method,url) -> (status, body)

    def request(self, method, url, headers=None, body=None):
        hit = self.table.get((method, url))
        if hit is None:
            return None, None, "canned-miss"
        status, b = hit
        return status, b, None


def self_test():
    """离线自检：判定层双极性 + 汇总 rc 语义。全过 rc=0；任一断言不成立 rc=1（校验器有牙）。"""
    base = "https://selftest.invalid"
    t = CannedTransport({
        ("GET", base + "/health"): (200, "ok"),
        ("GET", base + "/api/llm/platform-usage"): (401, "unauthorized"),
        ("POST", base + "/api/integration/webhook/order/wechat"): (400, "missing signature"),
    })
    results = [
        check_health(t, base),
        check_auth_missing(t, base, "/api/llm/platform-usage"),
        check_auth_bad_token(t, base, "/api/llm/platform-usage"),
        check_webhook_missing_sig(t, base, "wechat"),
    ]
    asserts = [
        ("A-health PASS", results[0]["verdict"] == "PASS"),
        ("B-auth-missing PASS(401)", results[1]["verdict"] == "PASS"),
        ("C-auth-bad-token PASS(401/403)", results[2]["verdict"] == "PASS"),
        ("D-webhook-missing-sig PASS(400/401)", results[3]["verdict"] == "PASS"),
    ]
    # 负例一：受保护端点返回 200（鉴权失效）必须判 FAIL
    t2 = CannedTransport({("GET", base + "/api/llm/platform-usage"): (200, "{}")})
    asserts.append(("负例：auth 200 → FAIL 有牙", judge("B", ({401, 403}, {200}), http_get(t2, base + "/api/llm/platform-usage"))["verdict"] == "FAIL"))
    # 负例二：网络错误必须 UNDETERMINED（不写 0）
    t3 = CannedTransport({})
    asserts.append(("负例：传输失败 → UNDETERMINED 不写 0", judge("X", ({200}, {500}), http_get(t3, base + "/x"))["verdict"] == "UNDETERMINED"))
    # 负例三：E 伪签构造器确实产出与好签不同的头
    good = _sig_headers("s", "{}", good=True)["X-Webhook-Signature"]
    bad = _sig_headers("s", "{}", good=False)["X-Webhook-Signature"]
    asserts.append(("负例：伪签≠好签", good != bad))

    failed = [name for name, ok in asserts if not ok]
    for name, ok in asserts:
        print(("PASS " if ok else "FAIL ") + name)
    if failed:
        print("self-test FAILED: %d/%d" % (len(failed), len(asserts)))
        return 1
    print("self-test OK: %d/%d（判定层双极性+UNDETERMINED 语义+伪签构造器全过）" % (len(asserts), len(asserts)))
    return 0


# ---------------------------------------------------------------- 主流程
def main():
    ap = argparse.ArgumentParser(description="CS-01 升级后再验黑盒复测（只读探针，不发真实客户消息）")
    ap.add_argument("--self-test", action="store_true", help="离线自检判定层（不连网）")
    ap.add_argument("--base-url", help="user-server 基址，如 https://workapi.example.cn")
    ap.add_argument("--token", default="", help="受保护端点探针用 JWT（可选；仅用于对照 B/C 的 401 面）")
    ap.add_argument("--probe-auth-path", default="/api/llm/platform-usage", help="受保护 GET 探针路径（默认 JWT+模块闸双闸端点）")
    ap.add_argument("--webhook-platform", default="wechat", help="回调探针平台词（默认 wechat）")
    ap.add_argument("--webhook-secret", default="", help="order_webhook_secret_<platform>（可选；缺省则 E/F=UNDETERMINED 如实登记）")
    ap.add_argument("--webhook-body", default="{}", help="回调探针 body（默认 {} 空格子；真实单据级重放对账归 CS-R1 窗口）")
    ap.add_argument("--strict", action="store_true", help="UNDETERMINED 亦判 rc=1")
    ap.add_argument("--json", action="store_true", help="JSON 报告输出")
    args = ap.parse_args()

    if args.self_test:
        sys.exit(self_test())
    if not args.base_url:
        ap.error("--base-url 必填（或使用 --self-test）")

    t = Transport()
    results = [
        check_health(t, args.base_url),
        check_auth_missing(t, args.base_url, args.probe_auth_path),
        check_auth_bad_token(t, args.base_url, args.probe_auth_path),
        check_webhook_missing_sig(t, args.base_url, args.webhook_platform),
    ]
    if args.webhook_secret:
        results.append(check_webhook_bad_sig(t, args.base_url, args.webhook_platform, args.webhook_secret, args.webhook_body))
        results.append(check_webhook_replay(t, args.base_url, args.webhook_platform, args.webhook_secret, args.webhook_body))
    else:
        for cid, why in (("E-webhook-bad-sig", "缺 --webhook-secret（不读盘不猜密钥）"),
                         ("F-webhook-replay", "缺 --webhook-secret；且单据级重放对账归 CS-R1 授权窗口")):
            results.append({"id": cid, "verdict": "UNDETERMINED", "status": None, "note": why})
    results.append({"id": "G-dedup-ingress", "verdict": "UNDETERMINED", "status": None,
                    "note": "需访客会话 fixture（CS-R1 授权窗口，G-UI/ego 三样本执行）——占位声明不冒充"})

    fails = [r for r in results if r["verdict"] == "FAIL"]
    und = [r for r in results if r["verdict"] == "UNDETERMINED"]
    rc = 1 if fails or (args.strict and und) else 0
    report = {"base_url": args.base_url, "strict": args.strict, "rc": rc,
              "summary": {"pass": len(results) - len(fails) - len(und), "fail": len(fails), "undetermined": len(und)},
              "results": results}
    if args.json:
        print(json.dumps(report, ensure_ascii=False, indent=1))
    else:
        for r in results:
            print("[%s] %s status=%s %s" % (r["verdict"], r["id"], r["status"], r["note"]))
        print("汇总：PASS=%d FAIL=%d UNDETERMINED=%d → rc=%d（UNDETERMINED=不可判断不写 0%s）" %
              (report["summary"]["pass"], len(fails), len(und), rc, "，--strict 下计入失败" if args.strict else ""))
    sys.exit(rc)


if __name__ == "__main__":
    main()
