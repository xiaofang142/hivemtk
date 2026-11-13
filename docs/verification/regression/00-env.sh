#!/usr/bin/env bash
# 00-env.sh — 回归用例公共环境。不落任何密钥明文。
# K4_BASE：被测实例地址（8401=TENANT_MODE=off 独立部署 / 8402=TENANT_MODE=on SaaS）
# K4_TOKEN：Bearer token（调用方自行登录获取后 export K4_TOKEN=...）
#   登录示例：
#   curl -s -X POST "$K4_BASE/api/auth/login" -H 'Content-Type: application/json' \
#     -d '{"username":"admin","password":"<SEED_PASSWORD>"}'
export K4_BASE="${K4_BASE:-http://127.0.0.1:8401}"
if [ -z "${K4_TOKEN:-}" ]; then
  echo "[00-env] K4_TOKEN 未设置：请先登录并 export K4_TOKEN=<token> 后重跑本用例" >&2
  exit 2
fi
# 统一带鉴权的快捷封装（仍是一发一判，不做循环）
auth() { printf 'Authorization: Bearer %s' "$K4_TOKEN"; }
