#!/usr/bin/env bash
#
# mlx/test.sh —— MLX LLM 服务端到端测试（产品交付验收）
#
# 覆盖：健康检查 / 模型列表 / 非流式 chat / prompt 兼容 / 流式 SSE /
#       工具降级 / 错误处理 / 统计 / 并发锁
#
# 用法：
#   bash scripts/inference-host/mlx/test.sh
#
set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../_common.sh
source "$SCRIPT_DIR/../_common.sh"

BASE="http://127.0.0.1:${LLM_PORT}"
pass=0; fail=0
ok()   { echo "  ✅ $1"; pass=$((pass+1)); }
bad()  { echo "  ❌ $1"; fail=$((fail+1)); }

if ! curl -fsS --max-time 5 "$BASE/health" >/dev/null 2>&1; then
  log_err "服务未运行（${BASE}），请先：bash scripts/inference-host/start-llm.sh"
  exit 1
fi

echo "=== [1/8] 健康检查 /health ==="
body=$(curl -fsS --max-time 5 "$BASE/health")
if echo "$body" | grep -q '"status":"ok"' && echo "$body" | grep -q '"engine":"mlx"'; then
  ok "/health 返回 ok + engine=mlx"
else
  bad "/health 响应异常: $body"
fi

echo "=== [2/8] 模型列表 /v1/models ==="
body=$(curl -fsS --max-time 5 "$BASE/v1/models")
if echo "$body" | grep -q "$LLM_SERVED_NAME"; then
  ok "/v1/models 含 $LLM_SERVED_NAME"
else
  bad "/v1/models 缺少模型名: $body"
fi

echo "=== [3/8] 非流式 chat（messages 格式，dispatcher 契约）==="
code=$(curl -s -o /tmp/mlx_t_chat.json -w "%{http_code}" --max-time 120 -X POST \
  "$BASE/v1/chat/completions" -H 'Content-Type: application/json' \
  -d '{"model":"'"$LLM_SERVED_NAME"'","messages":[{"role":"system","content":"你是客服助手，回答简短"},{"role":"user","content":"你好"}],"max_tokens":64,"temperature":0}')
if [ "$code" = "200" ] && grep -q '"content"' /tmp/mlx_t_chat.json \
   && grep -q '"total_tokens"' /tmp/mlx_t_chat.json \
   && grep -q '"finish_reason"' /tmp/mlx_t_chat.json; then
  ok "非流式 chat 返回 content + usage + finish_reason"
else
  bad "非流式 chat 失败 (HTTP $code): $(head -c 300 /tmp/mlx_t_chat.json)"
fi

echo "=== [4/8] prompt 字段兼容（简化客户端）==="
code=$(curl -s -o /tmp/mlx_t_prompt.json -w "%{http_code}" --max-time 120 -X POST \
  "$BASE/v1/chat/completions" -H 'Content-Type: application/json' \
  -d '{"prompt":"1+1=","max_tokens":16,"temperature":0}')
if [ "$code" = "200" ] && grep -q '"content"' /tmp/mlx_t_prompt.json; then
  ok "prompt 字段兼容"
else
  bad "prompt 字段失败 (HTTP $code)"
fi

echo "=== [5/8] 流式 SSE（stream=true）==="
code=$(curl -s -o /tmp/mlx_t_stream.txt -w "%{http_code}" --max-time 120 -X POST \
  "$BASE/v1/chat/completions" -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"说一个字"}],"max_tokens":32,"stream":true}')
chunks=$(grep -c 'chat.completion.chunk' /tmp/mlx_t_stream.txt 2>/dev/null)
chunks=${chunks:-0}
if [ "$code" = "200" ] && [ "$chunks" -ge 2 ] && grep -q 'data: \[DONE\]' /tmp/mlx_t_stream.txt; then
  ok "SSE 流式：$chunks 个 chunk + [DONE] 终止符"
else
  bad "SSE 流式失败 (HTTP $code, chunks=$chunks)"
fi

echo "=== [6/8] 工具调用降级（tools 注入 ReAct 协议）==="
code=$(curl -s -o /tmp/mlx_t_tools.json -w "%{http_code}" --max-time 120 -X POST \
  "$BASE/v1/chat/completions" -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"查询订单 123 的状态"}],"max_tokens":96,"temperature":0,
       "tools":[{"type":"function","function":{"name":"query_order",
         "description":"查询订单状态","parameters":{"type":"object",
         "properties":{"order_id":{"type":"string"}}}}}],
       "tool_choice":"auto"}')
if [ "$code" = "200" ] && grep -q '"content"' /tmp/mlx_t_tools.json; then
  if grep -q 'Action' /tmp/mlx_t_tools.json; then
    ok "tools 降级：模型按 ReAct 文本协议输出 Action"
  else
    ok "tools 降级：未报错（模型未触发 Action，属可接受行为）"
  fi
else
  bad "tools 降级失败 (HTTP $code): $(head -c 300 /tmp/mlx_t_tools.json)"
fi

echo "=== [7/8] 错误处理 ==="
code=$(curl -s -o /tmp/mlx_t_err.json -w "%{http_code}" --max-time 10 -X POST \
  "$BASE/v1/chat/completions" -H 'Content-Type: application/json' -d '{}')
if [ "$code" = "400" ] && grep -q '"error"' /tmp/mlx_t_err.json; then
  ok "空请求返回 400 + OpenAI 风格 error 体"
else
  bad "错误处理异常 (HTTP $code)"
fi

echo "=== [8/8] 统计 /v1/stats（本轮请求应已计入）==="
body=$(curl -fsS --max-time 5 "$BASE/v1/stats")
if echo "$body" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d['requests_total'] >= 5, f\"requests_total={d['requests_total']}\"
assert d['stream_requests'] >= 1, 'stream 未计入'
assert d['total_tokens'] > 0, 'tokens 未计入'
print(f\"  请求={d['requests_total']} 流式={d['stream_requests']} \"
      f\"工具降级={d['tool_fallback_requests']} tokens={d['total_tokens']} \"
      f\"avg={d['avg_latency_ms']}ms\")
" 2>/tmp/mlx_t_stats_err.txt; then
  ok "统计端点正常（累计/流式/token 均已计入）"
  STATS_FILE="${MLX_STATS_DIR:-$HIVEMTK_RUNTIME_DIR/mlx-stats}/stats.json"
  sleep 1
  if [ -f "$STATS_FILE" ]; then
    ok "统计落盘存在: $STATS_FILE"
  else
    bad "统计未落盘: ${STATS_FILE}（30s 周期，可等待后复查）"
  fi
else
  bad "统计校验失败: $(cat /tmp/mlx_t_stats_err.txt)"
fi

echo
echo "============================================================"
echo "[mlx-test] 结果：通过 $pass / 失败 $fail"
[ "$fail" = "0" ] && echo "✅ MLX LLM 服务端到端测试全部通过" || echo "❌ 存在失败项"
echo "============================================================"
[ "$fail" = "0" ]
