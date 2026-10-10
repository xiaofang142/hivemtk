#!/usr/bin/env bash
# 生产机 WS Origin 白名单幂等修复：向 /opt/hivemtk/.env 追加 ALLOWED_WS_ORIGINS 并重启握手验证
# 背景：未配置时 WS 只放行 localhost，线上 wss://api.xjznpt.com/api/ws/agent 每次握手 403
set -e
echo "=== 1) 现状：.env 里有没有 ALLOWED_WS_ORIGINS ==="
grep -c '^ALLOWED_WS_ORIGINS=' /opt/hivemtk/.env || true
grep -c 'allowed_ws_origins' /opt/hivemtk/config.yaml || true

echo "=== 2) 追加配置（幂等）==="
if ! grep -q '^ALLOWED_WS_ORIGINS=' /opt/hivemtk/.env; then
  cp -a /opt/hivemtk/.env /opt/hivemtk/.env.bak.$(date +%Y%m%d_%H%M%S)
  printf '\n# ---- WebSocket Origin 白名单（2026-10-10 修：未配置时只放行 localhost，\n# 导致线上 wss://api.xjznpt.com/api/ws/agent 每次握手都是 403 Forbidden）----\nALLOWED_WS_ORIGINS=https://api.xjznpt.com\n' >> /opt/hivemtk/.env
  echo "  已追加"
else
  echo "  已存在，跳过"
fi
grep '^ALLOWED_WS_ORIGINS=' /opt/hivemtk/.env

echo "=== 3) 重启 ==="
systemctl restart hivemtk
for i in $(seq 1 30); do
  ct=$(curl -fsS -o /dev/null -w "%{content_type}" http://127.0.0.1:8204/api/health 2>/dev/null || true)
  case "$ct" in application/json*) echo "  health OK ${i}x2s"; break ;; esac
  sleep 2
done

echo "=== 4) 取一个真 token（不落盘、不回显）==="
TOKEN=$(curl -s -X POST http://127.0.0.1:8204/api/auth/login \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"Alan\",\"password\":\"$(grep -m1 '^SEED_PASSWORD=' /opt/hivemtk/.env | cut -d= -f2-)\"}" \
  | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
if [ -z "$TOKEN" ]; then
  echo "  SEED_PASSWORD 登录失败，改用命令行参数传入口令"
  TOKEN=$(curl -s -X POST http://127.0.0.1:8204/api/auth/login \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"Alan\",\"password\":\"$ADMIN_PASS\"}" \
    | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
fi
if [ -z "$TOKEN" ]; then echo "  ❌ 拿不到 token，无法做握手验证"; exit 1; fi
echo "  token 长度=${#TOKEN}（不回显）"

hs() {
  local origin="$1"
  if [ -n "$origin" ]; then
    curl -s -i -N --max-time 4 \
      -H "Connection: Upgrade" -H "Upgrade: websocket" \
      -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==" \
      -H "Origin: $origin" \
      "http://127.0.0.1:8204/api/ws/agent?agent_id=1&token=$TOKEN" | head -1
  else
    curl -s -i -N --max-time 4 \
      -H "Connection: Upgrade" -H "Upgrade: websocket" \
      -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==" \
      "http://127.0.0.1:8204/api/ws/agent?agent_id=1&token=$TOKEN" | head -1
  fi
}

echo "=== 5) 握手验证 ==="
printf "  线上 Origin（期望 101）      : "; hs "https://api.xjznpt.com"
printf "  无 Origin（期望 101）        : "; hs ""
printf "  恶意 Origin（期望 403）      : "; hs "https://evil.example.com"
