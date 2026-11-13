#!/usr/bin/env bash
# monitor 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #805｜RAG recall 监控全生命周期：start→stop→二次 stop（不 panic）→stop 后重启→收尾 stop
# 预期：四步全部 200；二次 stop 不再 500（stopCh 重建修复）
case_805_recall_stop_cycle() {
  echo "-- start";  curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/rag/recall/start"; echo
  echo "-- stop#1"; curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/rag/recall/stop";  echo
  echo "-- stop#2（历史缺陷点：二次 close panic）"; curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/rag/recall/stop"; echo
  echo "-- restart after stop（历史缺陷点：goroutine 静默停摆）"; curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/rag/recall/start"; echo
  echo "-- final stop"; curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/rag/recall/stop"; echo
  echo "assert: 五行响应全部 code=0 且无 500"
}

"$@"
