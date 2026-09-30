#!/usr/bin/env bash
# stop-laya.sh —— 停止 Laya 决策服务（对齐 stop-all.sh 的 stop_role laya）
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"
print_inference_host_banner
stop_role laya
log_ok "Laya 已停止"
