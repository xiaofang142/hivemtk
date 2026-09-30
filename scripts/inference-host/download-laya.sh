#!/usr/bin/env bash
# download-laya.sh —— 顶层入口（对齐 download-models.sh），实际逻辑在 laya/download-model.sh
set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bash "$SCRIPT_DIR/laya/download-model.sh" "$@"
