#!/usr/bin/env bash
#
# start-all.sh —— 一键拉起 LLM + Embedding + Rerank 三服务 + Laya 决策服务
#
# 顺序：先 LLM（最重），再 embedding/rerank（轻量），最后 Laya（torch），等待所有 health
#
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

bash "$SCRIPT_DIR/start-llm.sh"
echo
bash "$SCRIPT_DIR/start-embedding.sh"
echo
bash "$SCRIPT_DIR/start-rerank.sh"
echo
bash "$SCRIPT_DIR/start-laya.sh"
echo

echo "============================================================"
echo "✅ HiveMtk 宿主机推理栈已全部启动"
echo "  LLM       : http://127.0.0.1:${LLM_PORT:-8207}/v1"
echo "  Embedding : http://127.0.0.1:${EMBEDDING_PORT:-8208}/v1"
echo "  Rerank    : http://127.0.0.1:${RERANK_PORT:-8209}/v1/rerank"
echo "  Laya      : http://127.0.0.1:${LAYA_PORT:-8210}/v1/decide"
echo
echo "下一步："
echo "  bash $SCRIPT_DIR/warmup.sh         # 预热（避免首请求慢）"
echo "  bash $SCRIPT_DIR/smoke-test.sh     # 三端点连通性测试"
echo "============================================================"
