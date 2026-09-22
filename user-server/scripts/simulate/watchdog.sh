#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# 推理栈看门狗：探测 8207/8208/8209 健康，下行则自动重启，确保模拟测试不坠 502。
# 用法：bash watchdog.sh            # 默认检查并重启所有失败服务
#       bash watchdog.sh --report   # 仅报告不重启
# 依赖：scripts/inference-host/start-*.sh（阻塞至 /health 通过）
# ---------------------------------------------------------------------------
set -u
HIVE=/Users/xiaofang/Documents/www/go/hivemtk/hivemtk
INFRA=$HIVE/scripts/inference-host
REPORT_ONLY=0
[[ "${1:-}" == "--report" ]] && REPORT_ONLY=1

ports=(8207 8208 8209)
names=(llm embedding rerank)
starts=(start-llm.sh start-embedding.sh start-rerank.sh)

green=0; down=()
for i in "${!ports[@]}"; do
  code=$(curl -s -m 6 -o /dev/null -w '%{http_code}' "http://localhost:${ports[$i]}/health" 2>/dev/null || echo 000)
  if [[ "$code" == "200" ]]; then
    green=$((green+1))
  else
    down+=("$i")
  fi
  echo "[watchdog] ${names[$i]}(${ports[$i]}) health=$code"
done

echo "[watchdog] 健康 ${green}/${#ports[@]}"

if [[ $REPORT_ONLY -eq 1 ]]; then
  [[ ${#down[@]} -eq 0 ]] && echo "[watchdog] 全部正常" || echo "[watchdog] 下行: ${down[*]}"
  exit ${#down[@]}
fi

if [[ ${#down[@]} -eq 0 ]]; then
  echo "[watchdog] 全部正常，无需重启"
  exit 0
fi

for i in "${down[@]}"; do
  name=${names[$i]}; port=${ports[$i]}; start=${starts[$i]}
  echo "[watchdog] 重启 $name ($port) ..."
  ( cd "$INFRA" && bash "$start" ) >/dev/null 2>&1
  # 验证
  sleep 2
  code=$(curl -s -m 8 -o /dev/null -w '%{http_code}' "http://localhost:${port}/health" 2>/dev/null || echo 000)
  if [[ "$code" == "200" ]]; then
    echo "[watchdog] $name 重启成功 (health=200)"
  else
    echo "[watchdog] 警告: $name 重启后仍异常 health=${code}，需人工排查"
  fi
done
exit 0
