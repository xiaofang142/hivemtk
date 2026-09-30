#!/usr/bin/env bash
#
# laya/download-model.sh —— Laya 决策模型下载（ModelScope 优先）
#
# 设计（对齐 download-models.sh / mlx/download-model.sh）：
#   - 模型：convaiinnovations/laya（ModernBERT-large 421M，非 GGUF，不能走 llama.cpp）
#   - 目标：$HIVEMTK_MODELS_DIR/laya（默认 hivemtk/models/laya）
#   - 优先 ModelScope（国内可达），回退 hf-mirror → hf
#   - 幂等：已有 .safetensors 即跳过
#
# 用法：
#   bash scripts/inference-host/laya/download-model.sh
#
set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../env.sh
source "$SCRIPT_DIR/../env.sh"

: "${LAYA_REPO:=convaiinnovations/laya}"
LAYA_DIR="${LAYA_MODEL_DIR:-$HIVEMTK_MODELS_DIR/laya}"

echo "============================================================"
echo "HiveMtk Laya 模型下载"
echo "  repo   = $LAYA_REPO"
echo "  target = $LAYA_DIR"
echo "  source = $DOWNLOAD_SOURCE"
echo "============================================================"

if compgen -G "$LAYA_DIR/*.safetensors" >/dev/null 2>&1; then
  echo "[laya-download] ✅ 已存在权重，跳过 ($(du -sh "$LAYA_DIR" | cut -f1))"
  exit 0
fi
mkdir -p "$LAYA_DIR"

# ---- 方式1：modelscope SDK（优先，断点续传）----
if python3 -c "import modelscope" >/dev/null 2>&1; then
  echo "[laya-download] 尝试 modelscope snapshot_download ..."
  if python3 - "$LAYA_REPO" "$LAYA_DIR" <<'PYEOF'; then
import sys
from modelscope import snapshot_download
repo, target = sys.argv[1], sys.argv[2]
snapshot_download(repo, local_dir=target)
print("[laya-download] modelscope SDK 下载成功")
PYEOF
    if compgen -G "$LAYA_DIR/*.safetensors" >/dev/null 2>&1; then
      echo "[laya-download] ✅ 成功 ($(du -sh "$LAYA_DIR" | cut -f1))"
      exit 0
    fi
  fi
  echo "[laya-download] ⚠️  modelscope SDK 方式未拿到权重，改走直链 ..." >&2
fi

# ---- 方式2：直链（对齐 download-models.sh 的源顺序）----
# 先拿 repo 文件清单？Laya 仓库文件固定为 config/权重/tokenizer，
# 直链逐个取核心文件（config.json 必需 + 权重 + tokenizer）。
CORE_FILES=(config.json model.safetensors tokenizer.json tokenizer_config.json)
IFS=',' read -ra SRCS <<< "$DOWNLOAD_SOURCE"
ok=0
for f in "${CORE_FILES[@]}"; do
  out="$LAYA_DIR/$f"
  [[ -s "$out" ]] && { echo "[laya-download] [$f] 已存在，跳过"; continue; }
  got=0
  for s in "${SRCS[@]}"; do
    case "$s" in
      modelscope) url="https://modelscope.cn/models/${LAYA_REPO}/resolve/master/${f}" ;;
      hf-mirror)  url="https://hf-mirror.com/${LAYA_REPO}/resolve/main/${f}" ;;
      hf)         url="https://huggingface.co/${LAYA_REPO}/resolve/main/${f}" ;;
      *) continue ;;
    esac
    echo "[laya-download] [$f] 尝试: $url"
    if curl -fL --retry 2 --retry-delay 2 --max-time 600 -o "$out.part" "$url" 2>/dev/null \
       && [[ -s "$out.part" ]]; then
      mv -f "$out.part" "$out"
      echo "[laya-download] [$f] ✅ 成功"
      got=1
      break
    fi
    rm -f "$out.part"
  done
  [[ "$got" == "1" ]] && ok=$((ok+1))
done

if compgen -G "$LAYA_DIR/*.safetensors" >/dev/null 2>&1; then
  echo "============================================================"
  echo "[laya-download] ✅ 模型已就绪 ($(du -sh "$LAYA_DIR" | cut -f1)): $LAYA_DIR"
  exit 0
fi
echo "[laya-download] ❌ 未拿到权重文件，请检查网络或手动放置到：$LAYA_DIR" >&2
exit 1
