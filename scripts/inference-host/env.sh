# ============================================================
# scripts/inference-host/env.sh —— 宿主机推理栈共享环境变量
# ------------------------------------------------------------
# 设计（单一源）：
#   - 所有推理栈脚本 source 本文件
#   - 优先读取项目根目录 .env（与 user-server / docker-compose 共享）
#   - 本文件仅提供 .env 未定义时的 fallback 默认值
#   - 调参只改 .env，不改本文件
# ============================================================

# ---- 项目根目录 ----
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# ---- 从 .env 加载（单一源）----
# 容错：.env 中可能存在非 shell 语法行（如 "KEY = value" 等号两侧带空格），
# 若调用方开启 set -e（如 mlx/download-model.sh），source 报错会直接中断脚本，
# 故临时关闭 errexit 再加载；个别行解析失败不影响其余变量。
if [[ -f "$PROJECT_ROOT/.env" ]]; then
  _env_sh_had_e=0
  if [[ "$-" == *e* ]]; then _env_sh_had_e=1; set +e; fi
  set -a
  # shellcheck source=/dev/null
  source "$PROJECT_ROOT/.env" 2>/dev/null
  set +a
  if [[ $_env_sh_had_e -eq 1 ]]; then set -e; fi
  unset _env_sh_had_e
fi

# ---- 路径（仅 .env 未定义时 fallback）----
: "${HIVEMTK_MODELS_DIR:=$PROJECT_ROOT/models}"
: "${HIVEMTK_RUNTIME_DIR:=$HOME/.hivemtk/runtime}"
HIVEMTK_SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---- 模型 profile ----
: "${HIVEMTK_PROFILE:=dev}"

# ---- llama.cpp 二进制探测 ----
detect_llamacpp_bin() {
  if [[ -n "${LLAMACPP_BIN:-}" && -x "$LLAMACPP_BIN" ]]; then
    echo "$LLAMACPP_BIN"; return
  fi
  local candidates=(
    "/opt/homebrew/bin/llama-server"
    "/usr/local/bin/llama-server"
    "/usr/bin/llama-server"
  )
  for c in "${candidates[@]}"; do
    if [[ -x "$c" ]]; then echo "$c"; return; fi
  done
  if command -v llama-server >/dev/null 2>&1; then
    command -v llama-server; return
  fi
  echo ""
}
export LLAMACPP_BIN
LLAMACPP_BIN="$(detect_llamacpp_bin)"

# ---- 端口 ----
: "${LLM_PORT:=8207}"
: "${EMBEDDING_PORT:=8208}"
: "${RERANK_PORT:=8209}"

# ---- LLM 引擎：统一 llama.cpp（GGUF）——
# 2026-09-07 移除 MLX 引擎（SmolLM3-3B MLX safetensors 已废弃）
# Apple Silicon 上 llama-server + GGUF 质量/兼容性更优
: "${LLM_ENGINE:=llamacpp}"
# MLX 引擎已废弃，保留变量名避免 .env 解析报错，但设为空
MLX_MODEL=""
MLX_MAX_TOKENS=1024
MLX_ENABLE_THINKING=false
MLX_HOST=0.0.0.0
MLX_STATS_DIR:=$HIVEMTK_RUNTIME_DIR/mlx-stats
MLX_PYTHON=""
export MLX_MODEL MLX_MAX_TOKENS MLX_ENABLE_THINKING MLX_HOST MLX_STATS_DIR MLX_PYTHON

# ---- 推理参数 ----
: "${CTX_SIZE:=8192}"
: "${THREADS:=0}"
if [[ "$(uname -s)" == "Darwin" && "$(uname -m)" == "arm64" ]]; then
  : "${NGL:=999}"
else
  : "${NGL:=0}"
fi
: "${BATCH_SIZE:=512}"
: "${UBATCH_SIZE:=512}"
: "${LLAMACPP_EXTRA_ARGS:=}"

# ---- 性能优化 ----
: "${FLASH_ATTN:=on}"
# mlock：模型锁定在 RAM 防止 macOS swap 抖动（2026-07-30 用户反馈性能问题后开启）
: "${USE_MLOCK:=true}"
: "${CACHE_TYPE_K:=q4_0}"
: "${CACHE_TYPE_V:=q4_0}"
# prompt cache 复用阈值：相同 prefix >= N token 时复用 KV cache（2026-07-31 调到 128，更激进）
: "${CACHE_REUSE:=128}"

# ---- 推测解码 ----
# 2026-09-07 禁用：16GB Mac 上 7B Q4 已占 4.4G，加 draft 模型争内存得不偿失
# 将来 32GB+ prod 工作站可重新启用（需先下载 LLM_DRAFT_FILE）
LLM_DRAFT_REPO=""
SPEC_TYPE="none"
LLM_DRAFT_FILE=""
: "${SPEC_DRAFT_N_MAX:=5}"
: "${SPEC_NGRAM_SIMPLE_N:=64}"
: "${SPEC_NGRAM_SIMPLE_M:=4}"
: "${SPEC_NGRAM_MIN_HITS:=1}"

# ---- 并发 & 批处理 ----
: "${LLM_PARALLEL:=4}"
: "${EMBEDDING_PARALLEL:=4}"
: "${RERANK_PARALLEL:=1}"
: "${LLM_CONT_BATCHING:=true}"
: "${SERVER_TIMEOUT:=300}"
# 私域部署: 默认禁用 llama.cpp /metrics 端点
# 如需开启, 显式设置 ENABLE_METRICS=true
: "${ENABLE_METRICS:=false}"
: "${USE_ALIAS:=true}"

# ---- 下载源 ----
: "${DOWNLOAD_SOURCE:=modelscope,hf-mirror,hf}"

# ---- 子目录 ----
LLM_MODEL_DIR="$HIVEMTK_MODELS_DIR/llm"
EMBEDDING_MODEL_DIR="$HIVEMTK_MODELS_DIR/embedding"
RERANK_MODEL_DIR="$HIVEMTK_MODELS_DIR/rerank"

mkdir -p "$HIVEMTK_RUNTIME_DIR" \
         "$LLM_MODEL_DIR" \
         "$EMBEDDING_MODEL_DIR" \
         "$RERANK_MODEL_DIR"

# ---- 提示信息 ----
print_inference_host_banner() {
  cat <<EOF
============================================================
HiveMtk 宿主机推理栈
  profile     = $HIVEMTK_PROFILE
  llm engine  = ${LLM_ENGINE:-llamacpp}
  llama.cpp   = ${LLAMACPP_BIN:-未安装}
  project     = $PROJECT_ROOT
  models dir  = $HIVEMTK_MODELS_DIR  (项目内，.gitignore 已忽略)
  runtime dir = $HIVEMTK_RUNTIME_DIR
  LLM port    = $LLM_PORT
  Embed port  = $EMBEDDING_PORT
  Rerank port = $RERANK_PORT
  download    = $DOWNLOAD_SOURCE
============================================================
EOF
}
