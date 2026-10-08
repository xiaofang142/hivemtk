#!/usr/bin/env bash
#
# start-laya.sh —— 启动 Laya 决策服务（端口 8210）
#
# 对齐 start-llm.sh / _common.sh 契约：
#   - PID 文件：$HIVEMTK_RUNTIME_DIR/laya.pid
#   - 日志   ：$HIVEMTK_RUNTIME_DIR/laya.log
#   - 健康   ：GET /health（最多 180s，首 load 需读 421M 权重）
#
# 用法：
#   bash scripts/inference-host/start-laya.sh
#
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

: "${LAYA_PORT:=8210}"
LAYA_DIR="${LAYA_MODEL_DIR:-$HIVEMTK_MODELS_DIR/laya}"
PID_FILE="$PID_DIR/laya.pid"
LOG_FILE="$PID_DIR/laya.log"

print_inference_host_banner
echo "  Laya port   = $LAYA_PORT"
echo "  Laya model  = $LAYA_DIR"
echo

if is_running "laya"; then
  log_warn "[laya] 已在运行 (pid=$(cat "$PID_FILE"))，跳过启动"
  exit 0
fi

if port_in_use "$LAYA_PORT"; then
  log_warn "[laya] 端口 $LAYA_PORT 已被其他进程占用（不是本服务）"
  log_warn "  如需替换，请先：kill 该进程 或 bash scripts/inference-host/stop-laya.sh"
  exit 1
fi

# 模型缺失则自动下载（对齐 ensure_model_file 行为）
if ! compgen -G "$LAYA_DIR/*.safetensors" >/dev/null 2>&1; then
  log_warn "[laya] 模型产物不存在: ${LAYA_DIR}，正在下载 ..."
  bash "$SCRIPT_DIR/laya/download-model.sh" || {
    log_err "[laya] 模型准备失败"
    exit 1
  }
fi

PY_BIN="${LAYA_PYTHON:-python3}"
if ! "$PY_BIN" -c "import laya, fastapi, uvicorn, pydantic" >/dev/null 2>&1; then
  log_err "[laya] Python 依赖缺失，请执行：$PY_BIN -m pip install laya fastapi uvicorn pydantic"
  exit 1
fi

log_info "[laya] 启动: $PY_BIN $SCRIPT_DIR/laya/server.py (port=$LAYA_PORT)"
LAYA_PORT="$LAYA_PORT" nohup "$PY_BIN" "$SCRIPT_DIR/laya/server.py" >"$LOG_FILE" 2>&1 &
pid=$!
echo "$pid" > "$PID_FILE"
sleep 0.5

if ! kill -0 "$pid" >/dev/null 2>&1; then
  log_err "[laya] 启动后立即退出，请查看日志：$LOG_FILE"
  tail -n 30 "$LOG_FILE" >&2 || true
  rm -f "$PID_FILE"
  exit 1
fi
log_ok "[laya] 已启动 (pid=$pid, port=$LAYA_PORT, log=$LOG_FILE)"

log_info "等待 /health（最多 180s，首 load 读权重较慢）..."
if wait_health "$LAYA_PORT" 180; then
  log_ok "[laya] 健康检查通过：http://127.0.0.1:${LAYA_PORT}"
else
  log_err "[laya] 健康检查失败，请查看日志：$LOG_FILE"
  exit 1
fi
