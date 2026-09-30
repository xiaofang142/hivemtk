#!/usr/bin/env bash
#
# stop-all.sh —— 一键停止三服务 + Laya 决策服务
#
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

print_inference_host_banner
stop_role llm
stop_role embedding
stop_role rerank
stop_role laya
log_ok "全部推理栈已停止"
