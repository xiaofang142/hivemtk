#!/usr/bin/env bash
#
# download-models.sh —— 宿主机推理栈模型下载（ModelScope 优先）
#
# 设计：
#   - 通过 HIVEMTK_PROFILE=dev|prod 选档（默认 dev）
#   - 三个 role（llm / embedding / rerank）独立下载，缺哪个下哪个
#   - 优先 ModelScope（国内可达），回退 hf-mirror → hf
#   - 断点续传、校验非空、失败可读
#   - 用户可在 source 前用同名环境变量覆盖任意仓库或文件名
#
# 用法：
#   bash scripts/inference-host/download-models.sh          # 下载 HIVEMTK_PROFILE 指定的所有 role
#   bash scripts/inference-host/download-models.sh llm      # 只下载 LLM
#   HIVEMTK_PROFILE=prod bash scripts/inference-host/download-models.sh
#
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "$SCRIPT_DIR/env.sh"
# shellcheck source=models.env
source "$SCRIPT_DIR/models.env"

ONLY_ROLE="${1:-}"

# ---- 工具函数 ----
# build_urls <repo> <branch> <file> [ms_file]
# ms_file: ModelScope 上的实际文件名（个别仓库首尾带空格），不传则用 file
build_urls() {
  local repo="$1" branch="$2" f="$3" msf="${4:-$3}"
  # ModelScope URL-encode 空格
  local msf_encoded="${msf// /%20}"
  IFS=',' read -ra SRCS <<< "$DOWNLOAD_SOURCE"
  for s in "${SRCS[@]}"; do
    case "$s" in
      modelscope) echo "https://modelscope.cn/models/${repo}/resolve/${branch}/${msf_encoded}" ;;
      hf-mirror)  echo "https://hf-mirror.com/${repo}/resolve/main/${f}" ;;
      hf)         echo "https://huggingface.co/${repo}/resolve/main/${f}" ;;
      *)          echo "[download] 未知源：${s}（跳过）" >&2 ;;
    esac
  done
}

download_one() {
  local role="$1" repo="$2" branch="$3" file="$4" target_dir="$5" msf="${6:-}"
  local out="$target_dir/$file"
  if [[ -s "$out" ]]; then
    echo "[download] [$role] 已存在且非空，跳过: $out ($(du -h "$out" | cut -f1))"
    return 0
  fi
  mkdir -p "$target_dir"
  local urls=()
  while IFS= read -r u; do urls+=("$u"); done < <(build_urls "$repo" "$branch" "$file" "$msf")
  for u in "${urls[@]}"; do
    echo "[download] [$role] 尝试: $u"
    local auth=()
    if [[ "$u" == https://huggingface.co/* && -n "${HF_TOKEN:-}" ]]; then
      auth=(-H "Authorization: Bearer $HF_TOKEN")
    fi
    local tmp="$out.part"
    # ${auth[@]+...} 兼容 bash 3.2 (macOS 默认)：空数组在 set -u 下不报 unbound
    if curl -fL --retry 2 --retry-delay 2 --max-time 600 -C - ${auth[@]+"${auth[@]}"} -o "$tmp" "$u" 2>/dev/null; then
      if [[ -s "$tmp" ]]; then
        mv -f "$tmp" "$out"
        echo "[download] [$role] ✅ 成功 ($file): $out ($(du -h "$out" | cut -f1))"
        return 0
      fi
    fi
    rm -f "$tmp"
  done
  echo "[download] [$role] ❌ 所有源均失败: $repo/$file" >&2
  return 1
}

# ---- 入口 ----
print_inference_host_banner
echo "[download] profile=$HIVEMTK_PROFILE, source=$DOWNLOAD_SOURCE"
echo

roles_done=0
roles_failed=0

download_role() {
  local role="$1" repo="$2" file="$3" dir="$4" branch="${5:-master}" msf="${6:-}"
  if [[ -n "$ONLY_ROLE" && "$ONLY_ROLE" != "$role" ]]; then return 0; fi
  echo "[download] [$role] 仓库=$repo, 文件=$file, 目标=$dir"
  if download_one "$role" "$repo" "$branch" "$file" "$dir" "$msf"; then
    roles_done=$((roles_done + 1))
  else
    roles_failed=$((roles_failed + 1))
  fi
  echo
}

# ModelScope 大多 master；HF main
download_role llm        "$LLM_REPO"       "$LLM_FILE"       "$LLM_MODEL_DIR"       master
download_role embedding  "$EMBEDDING_REPO" "$EMBEDDING_FILE" "$EMBEDDING_MODEL_DIR" master
download_role rerank     "$RERANK_REPO"    "$RERANK_FILE"    "$RERANK_MODEL_DIR"    master
# draft-simple 草稿模型：仅当 LLM_DRAFT_FILE 已配置（prod 档）才下载
if [[ -n "${LLM_DRAFT_FILE:-}" ]]; then
  download_role llm-draft "$LLM_DRAFT_REPO" "$LLM_DRAFT_FILE" "$LLM_MODEL_DIR" master
fi

echo "============================================================"
echo "[download] 汇总：成功 $roles_done / 失败 $roles_failed"
if [[ $roles_failed -gt 0 ]]; then
  echo "  失败排查："
  echo "  1) 检查网络是否可访问 ModelScope / HuggingFace"
  echo "  2) 仓库 ID 与文件名是否正确（注意大小写、量化后缀）"
  echo "  3) gated 仓库需设置 HF_TOKEN"
  echo "  4) 可手动下载后放入对应目录："
  echo "     LLM:       $LLM_MODEL_DIR"
  echo "     Embedding: $EMBEDDING_MODEL_DIR"
  echo "     Rerank:    $RERANK_MODEL_DIR"
  exit 1
fi
echo "✅ 所有模型已就绪"
