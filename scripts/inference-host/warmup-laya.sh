#!/usr/bin/env bash
# warmup-laya.sh —— 预热 Laya 决策端点（对齐 warmup.sh：短/长两轮）
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"
: "${LAYA_PORT:=8210}"

print_inference_host_banner
echo "[warmup-laya] 等待 /health ..."
wait_health "$LAYA_PORT" 180 || { log_err "[laya] 健康检查失败"; exit 1; }
log_ok "127.0.0.1:${LAYA_PORT} 就绪"

echo "[warmup-laya] 第 1 轮（短 state + choice 单题）..."
code1=$(curl -s -o /dev/null -w "%{http_code}" -X POST "http://127.0.0.1:${LAYA_PORT}/v1/decide" \
  -H 'Content-Type: application/json' \
  -d '{"state":"hi","questions":{"q1":{"type":"choice","instructions":"pick","criteria":{"a":"greeting","b":"farewell"}}}}' \
  --max-time 120 || echo "000")
[ "$code1" = "200" ] || { log_err "预热第 1 轮失败 (HTTP $code1)"; exit 1; }

echo "[warmup-laya] 第 2 轮（客服 state + choice/score 混合）..."
code2=$(curl -s -o /dev/null -w "%{http_code}" -X POST "http://127.0.0.1:${LAYA_PORT}/v1/decide" \
  -H 'Content-Type: application/json' \
  -d '{"state":"客户询问如何退款，订单7天内未发货","questions":{"intent":{"type":"choice","instructions":"识别意图","criteria":{"refund":"退款","greet":"打招呼","other":"其他"}},"urgency":{"type":"score","instructions":"紧急程度打分","criteria":["低","中","高"]}}}' \
  --max-time 120 || echo "000")
[ "$code2" = "200" ] || { log_err "预热第 2 轮失败 (HTTP $code2)"; exit 1; }

log_ok "Laya 预热完成（2 轮），业务首请求可享受热模型响应"
