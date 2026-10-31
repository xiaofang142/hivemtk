#!/usr/bin/env bash
# 本地 API 回归用的双形态重启脚本（独立部署 off:8204 / SaaS on:8206）
# 用法：bash scripts/local-api-restart.sh
# 说明：口令一律从容器实际环境取，不读 .env 里的占位符（.env 内占位符是部署期替换的）
set -euo pipefail
cd "$(dirname "$0")/.."

DB_PW="$(docker exec mtk-postgres printenv POSTGRES_PASSWORD)"
MASTER_KEY="${MASTER_KEY:?需要导出 MASTER_KEY（32 字节以上）}"
REDIS_PW="${REDIS_PASSWORD:-}"

go build -o bin/user-server ./cmd/api
pkill -f "bin/user-server" 2>/dev/null || true
sleep 2

start() {
  local mode="$1" port="$2" log="$3"
  env POSTGRES_PASSWORD="$DB_PW" MASTER_KEY="$MASTER_KEY" REDIS_PASSWORD="$REDIS_PW" \
      DB_HOST=127.0.0.1 DB_PORT=8202 REDIS_HOST=127.0.0.1 REDIS_PORT=8203 \
      TENANT_MODE="$mode" PORT="$port" APP_ENV=development \
      nohup ./bin/user-server >"$log" 2>&1 &
  echo "started TENANT_MODE=$mode port=$port log=$log"
}

start off 8204 /tmp/us-off.log
start on  8206 /tmp/us-on.log

for i in $(seq 1 40); do
  if curl -sf -m 2 "http://127.0.0.1:8204/healthz" >/dev/null && curl -sf -m 2 "http://127.0.0.1:8206/healthz" >/dev/null; then
    echo "both up"
    exit 0
  fi
  sleep 2
done
echo "startup timeout, check /tmp/us-off.log /tmp/us-on.log" >&2
exit 1
