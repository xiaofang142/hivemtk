#!/usr/bin/env bash
#
# status.sh —— 查看三服务（llm / embedding / rerank）+ Laya 决策服务运行状态
#
# 用法：
#   bash scripts/inference-host/status.sh
#
set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

print_inference_host_banner
echo "[status] 服务状态："
describe_role llm       "$LLM_PORT"
describe_role embedding "$EMBEDDING_PORT"
describe_role rerank    "$RERANK_PORT"
describe_role laya      "${LAYA_PORT:-8210}"
echo

# MLX 引擎附加信息：统计摘要
if [[ "${LLM_ENGINE:-llamacpp}" == "mlx" ]] && is_running llm; then
  if stats=$(curl -fsS --max-time 3 "http://127.0.0.1:${LLM_PORT}/v1/stats" 2>/dev/null); then
    echo "[status] MLX 统计（/v1/stats）："
    echo "$stats" | python3 -c "
import json,sys
d=json.load(sys.stdin)
print(f\"  请求总数      = {d['requests_total']} (成功 {d['requests_ok']} / 失败 {d['requests_failed']})\")
print(f\"  流式请求      = {d['stream_requests']}, 工具降级 = {d['tool_fallback_requests']}\")
print(f\"  token 累计    = prompt {d['prompt_tokens']} + completion {d['completion_tokens']} = {d['total_tokens']}\")
print(f\"  平均延迟      = {d['avg_latency_ms']} ms\")
print(f\"  今日          = {d['today']['requests']} 请求 / {d['today']['tokens']} tokens\")
print(f\"  运行时长      = {d['uptime_seconds']} s\")
" 2>/dev/null || echo "  (统计解析失败)"
  fi
fi
