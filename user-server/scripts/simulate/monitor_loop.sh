#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# 无人值守监控循环（全自动质量飞轮）：
#   1. 看门狗：推理栈下行自动重启（避免 502 致测试全挂）
#   2. 质量评估：读取 interactions.jsonl，输出 AI 回答内容质量报告
#   3. 结论打印
# 设计：本脚本只做「巡检+自愈+评估汇报」，持续发请求由 simulate.py --daemon 负责。
# 用法：
#   bash monitor_loop.sh            # 跑一轮即退出（适合 automation/cron）
#   bash monitor_loop.sh --daemon   # 每 300s 一轮常驻
# ---------------------------------------------------------------------------
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"

INTERVAL=300
if [[ "${1:-}" == "--daemon" ]]; then
  echo "[monitor_loop] 常驻模式，每 ${INTERVAL}s 一轮 (Ctrl+C 退出)"
  while true; do
    bash "$HERE/watchdog.sh"
    python3 ai_quality.py --once --last 20
    echo ""
    sleep "$INTERVAL"
  done
else
  bash "$HERE/watchdog.sh"
  python3 ai_quality.py --once --last 20
fi
