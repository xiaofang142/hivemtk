#!/usr/bin/env bash
#
# start-llm.sh —— 启动 LLM 服务（端口 8207，chat/completions）
#
# 引擎选择（.env 的 LLM_ENGINE）：
#   llamacpp（默认）—— llama-server（GGUF）
#   mlx            —— MLX 服务（Apple Silicon，SmolLM3-3B 4bit，见 mlx/server.py）
#
# 用法：
#   bash scripts/inference-host/start-llm.sh
#
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

print_inference_host_banner

ENGINE="${LLM_ENGINE:-llamacpp}"
case "$ENGINE" in
  mlx)
    start_mlx_llm
    ;;
  llamacpp)
    start_role llm "$LLM_FILE" "$LLM_PORT" "$LLM_MODEL_DIR" llm
    ;;
  *)
    log_err "未知 LLM_ENGINE: ${ENGINE}（可选：llamacpp | mlx）"
    exit 1
    ;;
esac

log_info "等待 /health（最多 120s）..."
if wait_health "$LLM_PORT" 120; then
  log_ok "[llm] 健康检查通过：http://127.0.0.1:${LLM_PORT}"
  log_info "OpenAI 兼容端点：http://127.0.0.1:${LLM_PORT}/v1"
else
  log_err "[llm] 健康检查失败，请查看日志：$HIVEMTK_RUNTIME_DIR/llm.log"
  exit 1
fi
