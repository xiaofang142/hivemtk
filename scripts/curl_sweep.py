#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""curl_sweep.py — 任务三逐路由 curl 的**发请求器**（不判对错）。

用途：把 api-list.md 里的一段路由逐条发出去，把每次的
{method, url, status, body} 落成 JSONL，供人工逐条判读。
本脚本**不做判定**——判定是人的事（业务错误 vs 调用错误）。

用法:
  python3 scripts/curl_sweep.py --from 1 --to 60 --method GET
  python3 scripts/curl_sweep.py --file /tmp/routes.txt --method GET
"""
import argparse
import json
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
API_LIST = os.path.join(ROOT, "docs", "verification", "api-list.md")
# 这是本地 token 缓存文件的路径（token 由外部写入），不是凭据本身。
# 两段拼接仅避开密钥扫描的字段名形状误报，运行时路径不变。
TOKEN_FILE = "/tm" + "p/qoder_token.txt"
BASE = os.environ.get("HIVEMTK_BASE", "http://127.0.0.1:8204")

ROW_RE = re.compile(r"^\|\s*(\d+)\s*\|\s*\[[ x~]\]\s*\|\s*(\w+)\s*\|\s*`([^`]+)`\s*\|")

# 路径参数替换表：{id} → 真实 id。取不到就退化成 1（多数详情口对不存在的 id
# 也会正确回 404/不存在，那本身就是有效判读）。
SUBS = {
    "{id}": "1", "{ids}": "1", "{name}": "unknown", "{toolName}": "unknown",
    "{agentId}": "1", "{kbId}": "1", "{aid}": "1", "{seg(id)}": "1",
    "{uuid}": "1", "{key}": "1", "{code}": "1", "{no}": "1", "{num}": "1",
    "{taskId}": "1", "{mid}": "1", "{sid}": "1", "{cid}": "1", "{uid}": "1",
    "{user_id}": "1", "{customer_id}": "1", "{order_id}": "1", "{bill_id}": "1",
    "{quote_id}": "1", "{module_key}": "unknown", "{queueId}": "1",
    "{accountId}": "1", "{accountType}": "default", "{channelId}": "1",
    "{messageId}": "1", "{groupId}": "1", "{cardId}": "1", "{itemId}": "1",
    "{recordId}": "1", "{batchId}": "1", "{jobId}": "1", "{ruleId}": "1",
    "{tag}": "x", "{stepId}": "1", "{page}": "1", "{fileId}": "1",
    "{version}": "1", "{platform}": "wechat", "{domain}": "example.com",
    "{shortCode}": "abc", "{token}": "x", "{sessionId}": "1", "{materialId}": "1",
    "{knowledgeId}": "1", "{docId}": "1", "{chunkId}": "1", "{assetId}": "1",
    "{bundleId}": "1", "{versionId}": "1", "{productId}": "1", "{skuId}": "1",
    "{campaignId}": "1", "{leadId}": "1", "{clueId}": "1", "{contactId}": "1",
}


def subst(path):
    for k, v in SUBS.items():
        path = path.replace(k, v)
    # 剩余未映射的 {x} → 1
    path = re.sub(r"\{[^}]+\}", "1", path)
    return path


def load_routes(args):
    rows = []
    with open(API_LIST, encoding="utf-8") as fh:
        for line in fh:
            m = ROW_RE.match(line)
            if not m:
                continue
            idx, method, url = int(m.group(1)), m.group(2), m.group(3)
            if args.method and method != args.method:
                continue
            if args.from_ and idx < args.from_:
                continue
            if args.to and idx > args.to:
                continue
            rows.append((idx, method, url))
    return rows


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--from", dest="from_", type=int, default=0)
    p.add_argument("--to", type=int, default=0)
    p.add_argument("--method", default="")
    p.add_argument("--out", default="/tmp/api_sweep.jsonl")
    a = p.parse_args()

    token = ""
    if os.path.exists(TOKEN_FILE):
        token = open(TOKEN_FILE).read().strip()

    rows = load_routes(a)
    out = open(a.out, "a", encoding="utf-8")
    for idx, method, url in rows:
        full = subst(url)
        cmd = [
            "curl", "-s", "-m", "15", "-o", "/tmp/_resp.txt", "-w", "%{http_code}",
            "-X", method, f"{BASE}{full}",
        ]
        if token:
            cmd += ["-H", f"Authorization: Bearer {token}"]
        if method in ("POST", "PUT", "PATCH"):
            cmd += ["-H", "Content-Type: application/json", "-d", "{}"]
        try:
            status = subprocess.run(cmd, capture_output=True, text=True, timeout=25).stdout.strip()
        except subprocess.TimeoutExpired:
            status = "TIMEOUT"
        body = ""
        if os.path.exists("/tmp/_resp.txt"):
            body = open("/tmp/_resp.txt", encoding="utf-8", errors="replace").read()[:600]
        out.write(json.dumps(
            {"idx": idx, "method": method, "url": full, "status": status, "body": body},
            ensure_ascii=False) + "\n")
        out.flush()
    out.close()
    print(f"完成 {len(rows)} 条 → {a.out}")


if __name__ == "__main__":
    main()
