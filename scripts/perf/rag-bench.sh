#!/usr/bin/env bash
# RAG 检索延迟基准（OPT-SEC-06）
#
# 覆盖三档语料规模：100 / 500 / 1000 chunks；
# 每档构造代表性 query 集，统计 P50 / P95 / P99 延迟。
#
# 前置：需要 jq、curl；可选 wrk（更准的分位需要 wrk + --latency）
#
# 用法：
#   ./scripts/perf/rag-bench.sh http://localhost:8204
#   CHUNK_SIZES="100 500" ./scripts/perf/rag-bench.sh http://localhost:8204
#
# 输出：
#   /tmp/rag-bench-{chunks}.log    原始逐次延迟（毫秒）
#   /tmp/rag-bench-report.txt      P50/P95/P99 汇总

set -euo pipefail

TARGET="${1:-http://localhost:8204}"
CHUNK_SIZES="${CHUNK_SIZES:-100 500 1000}"
QUERIES_PER_SIZE="${QUERIES_PER_SIZE:-30}"
REPORT="/tmp/rag-bench-report.txt"

# 内置 query 模板：覆盖价格异议 / 信任异议 / 售后 / 产品咨询 / 知识库常见问
QUERIES=(
  "多少钱"
  "太贵了"
  "能便宜点吗"
  "这个产品怎么用"
  "售后多久"
  "质量怎么样"
  "靠谱吗"
  "会不会跑路"
  "怎么发货"
  "支持定制吗"
)

: > "$REPORT"
echo "RAG 检索延迟基准" >> "$REPORT"
echo "Target: $TARGET" >> "$REPORT"
echo "时间:   $(date -Iseconds)" >> "$REPORT"
echo "==========================================" >> "$REPORT"

# 检查依赖
command -v jq >/dev/null 2>&1 || { echo "[ERR] 缺少 jq，请 brew install jq"; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "[ERR] 缺少 curl"; exit 1; }

# 通过环境变量注入 admin token（可选）
TOKEN="${RAG_ADMIN_TOKEN:-${HIVEMTK_TOKEN:-}}"
AUTH_HEADER=()
if [[ -n "$TOKEN" ]]; then
  AUTH_HEADER=(-H "Authorization: Bearer $TOKEN")
fi

for CHUNKS in $CHUNK_SIZES; do
  echo
  echo "=== chunks=$CHUNKS ==="
  RAW="/tmp/rag-bench-${CHUNKS}.log"
  : > "$RAW"

  # 1) 若知识库语料数不足，先批量注入
  CURRENT=$(curl -sf "${AUTH_HEADER[@]}" "$TARGET/api/rag/health" | jq -r '.data.chunk_count // 0' 2>/dev/null || echo 0)
  if [[ "$CURRENT" -lt "$CHUNKS" ]]; then
    echo "  [setup] 注入 $((CHUNKS - CURRENT)) 条测试 chunks..."
    for ((i = CURRENT + 1; i <= CHUNKS; i++)); do
      curl -sf -X POST "${AUTH_HEADER[@]}" -H "Content-Type: application/json" \
        -d "{\"id\":\"bench-$i\",\"content\":\"测试语料 #${i}，用于压测检索延迟。\"}" \
        "$TARGET/api/rag/chunks" >/dev/null 2>&1 || true
    done
  fi

  # 2) 跑 N 次 query，统计每次检索耗时
  for ((q = 0; q < QUERIES_PER_SIZE; q++)); do
    QUERY="${QUERIES[$((q % ${#QUERIES[@]}))]}"
    MS=$(curl -sf -o /dev/null -w "%{time_total}\n" "${AUTH_HEADER[@]}" \
      -H "Content-Type: application/json" \
      -d "{\"q\":\"$QUERY\",\"top_k\":5}" \
      "$TARGET/api/rag/search" 2>/dev/null || echo "0")
    # 转换为毫秒（保留 3 位小数）
    MS_NUM=$(awk -v t="$MS" 'BEGIN { printf "%.3f", t * 1000 }')
    echo "$MS_NUM" >> "$RAW"
  done

  # 3) 计算 P50 / P95 / P99
  SORTED=$(sort -n "$RAW")
  N=$(wc -l < "$RAW" | tr -d ' ')
  P50=$(echo "$SORTED" | awk -v n="$N" 'NR == int(n * 0.50 + 0.5)')
  P95=$(echo "$SORTED" | awk -v n="$N" 'NR == int(n * 0.95 + 0.5)')
  P99=$(echo "$SORTED" | awk -v n="$N" 'NR == int(n * 0.99 + 0.5)')
  AVG=$(awk '{ sum += $1 } END { if (NR>0) printf "%.3f", sum / NR; else print "0" }' "$RAW")
  MAX=$(tail -1 <<< "$SORTED")
  MIN=$(head -1 <<< "$SORTED")

  LINE=$(printf "chunks=%-5s N=%-4s min=%-8s avg=%-8s p50=%-8s p95=%-8s p99=%-8s max=%s" \
    "$CHUNKS" "$N" "$MIN" "$AVG" "$P50" "$P95" "$P99" "$MAX")
  echo "  $LINE"
  echo "$LINE" >> "$REPORT"
done

echo
echo "报告已生成：$REPORT"
echo "==========================================="
cat "$REPORT"
