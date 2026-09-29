#!/usr/bin/env bash
# deep_mcp_sse.sh - 端到端验证 MCP server + SSE 下行
# 解决：单元测试通过但路由未注册，curl 端到端才发现。
#
# 本脚本此前**一个凭证都不带**，而 /api/mcp 与 /api/bridge/outbox/sse 都在鉴权组里
# （桥接凭证自 system_config_kv 起就是必填）。于是每条断言读到的都是 401 响应体：
# MCP 那五条恒红，SSE 那几条恒红，第 9 条（拿 wc -c 当「Last-Event-ID 被接受」）
# 反倒恒绿 —— 一份既报红又报绿的报告里，绿的那条恰恰是最没意义的那条。
# 现在：先解析凭证，拿不到就按环境问题退出 2（不产出任何 PASS/FAIL 结论）。
set -u
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
BASE="${BASE:-http://127.0.0.1:8204}"

PASS=0
FAIL=0
pass() { echo "  [PASS] $1"; PASS=$((PASS+1)); }
fail() { echo "  [FAIL] $1"; FAIL=$((FAIL+1)); }

# 先探一口：连不上就直接按环境问题退出，别让下面的判据把"端口被别的东西占了"
# 报成"功能坏了"（实测本机 127.0.0.1:8204 会被一个 crash-loop 的 user-server 容器
# 抢走，而热重载的开发进程只在 ::1/localhost 上接得到）。
preflight=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$BASE/healthz" 2>/dev/null)
if [ "$preflight" = "000" ] || [ -z "$preflight" ]; then
  echo "ENV-BROKEN: ${BASE} 连不上（http=${preflight}）。先跑 lsof -nP -iTCP:8204 看谁在听这个端口" >&2
  exit 2
fi

# ---- 凭证解析：专用 MCP_TOKEN 优先，其次桥接凭证（env，再退到开发库 KV） ----
MCP_HDR=()
BRIDGE_HDR=()
if [ -n "${MCP_TOKEN:-}" ]; then
  MCP_HDR=(-H "X-MCP-Token: ${MCP_TOKEN}")
fi
BRIDGE="${BRIDGE_INGEST_TOKEN:-}"
envfile="$(cd "$(dirname "$0")/../../.." && pwd)/.env"
if [ -z "$BRIDGE" ]; then
  # 开发态取值来源与中间件一致（system_config_kv.bridge_ingest_token）；口令只进变量，
  # 不回显、不写盘、不进任何日志。
  pgpass="$(awk -F= '/^POSTGRES_PASSWORD=/{print $2; exit}' "$envfile" 2>/dev/null)"
  pgport="$(awk -F= '/^POSTGRES_PORT=/{print $2; exit}' "$envfile" 2>/dev/null)"
  BRIDGE=$(PGPASSWORD="$pgpass" psql -h "${POSTGRES_HOST:-127.0.0.1}" -p "${pgport:-8232}" \
    -U "${POSTGRES_USER:-admin}" -d "${POSTGRES_DB:-user_db}" -Atc \
    "select value from system_config_kv where key='bridge_ingest_token' limit 1" 2>/dev/null || true)
fi
if [ -n "$BRIDGE" ]; then
  BRIDGE_HDR=(-H "X-Bridge-Token: ${BRIDGE}")
  [ ${#MCP_HDR[@]} -eq 0 ] && MCP_HDR=("${BRIDGE_HDR[@]}")
fi
if [ ${#MCP_HDR[@]} -eq 0 ]; then
  echo "ENV-BROKEN: 拿不到 MCP/桥接凭证（export MCP_TOKEN 或 BRIDGE_INGEST_TOKEN，或让 psql 能读到开发库 system_config_kv）" >&2
  echo "本入口已改为必填鉴权，无凭证跑不出任何结论，不产出 PASS/FAIL。" >&2
  exit 2
fi
HAS_BRIDGE=1
if [ ${#BRIDGE_HDR[@]} -eq 0 ]; then
  # 只配了 MCP 专用凭证、拿不到桥接凭证：MCP 那几条照跑，SSE 那几条没有判据可言。
  HAS_BRIDGE=0
fi

mcp_post() { curl -s --max-time 8 -X POST "$BASE/api/mcp" -H 'Content-Type: application/json' \
  "${MCP_HDR[@]}" -d "$1"; }

echo "==== MCP server E2E ===="

# 0. 门禁本身：不带凭证必须被拦（否则「鉴权在」这件事只是传说）
noauth_code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 8 -X POST "$BASE/api/mcp" \
  -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":"0","method":"ping"}')
if [ "$noauth_code" = "401" ] || [ "$noauth_code" = "503" ]; then
  pass "无凭证 POST /api/mcp 被拦（http=${noauth_code}）"
else
  fail "无凭证 POST /api/mcp 竟然返回 http=${noauth_code}（门禁失效）"
fi

# 1. initialize
resp=$(mcp_post '{"jsonrpc":"2.0","id":"1","method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"e2e","version":"1.0"}}}')
protocol=$(printf '%s' "$resp" | jq -r '.result.protocolVersion' 2>/dev/null)
servername=$(printf '%s' "$resp" | jq -r '.result.serverInfo.name' 2>/dev/null)
if [ "$protocol" = "2025-06-18" ] && [ "$servername" = "hivemtk-tooluse-mcp" ]; then
  pass "MCP initialize 返回正确 protocol/serverName"
else
  fail "MCP initialize 异常: $(printf '%s' "$resp" | head -c 200)"
fi

# 2. ping
resp=$(mcp_post '{"jsonrpc":"2.0","id":"2","method":"ping"}')
if [ "$(printf '%s' "$resp" | jq -r '.result' 2>/dev/null)" = "{}" ]; then
  pass "MCP ping 返回空对象"
else
  fail "MCP ping 异常: $(printf '%s' "$resp" | head -c 200)"
fi

# 3. tools/list
tools_len=$(mcp_post '{"jsonrpc":"2.0","id":"3","method":"tools/list"}' | jq -r '.result.tools | length' 2>/dev/null)
case "$tools_len" in
'' | null) fail "MCP tools/list 没读到工具数（响应不是预期形状）" ;;
*) pass "MCP tools/list 返回 ${tools_len} 个工具（只判读得到条数，不判具体数：工具注册表面归别的用例管）" ;;
esac

# 4. 未知方法
errcode=$(mcp_post '{"jsonrpc":"2.0","id":"4","method":"foo/bar"}' | jq -r '.error.code' 2>/dev/null)
if [ "$errcode" = "-32601" ]; then
  pass "MCP 未知方法返回 -32601 (Method not found)"
else
  fail "MCP 未知方法错误码异常: $errcode"
fi

# 5. 非法 JSON
errcode=$(mcp_post 'not json' | jq -r '.error.code' 2>/dev/null)
if [ "$errcode" = "-32700" ]; then
  pass "MCP 非法 JSON 返回 -32700 (Parse error)"
else
  fail "MCP parse error 异常: $errcode"
fi

echo ""
echo "==== SSE 下行 E2E ===="

sse_headers() {
  curl -s -N --max-time 2 -D - -o /dev/null "${BRIDGE_HDR[@]}" \
    "$BASE/api/bridge/outbox/sse?channel=douyin&account_id=default" 2>&1
}

# 下面五条 SSE 判据必须有桥接凭证才谈得上握手成功；只有 MCP 专用凭证时整段跳过并说明，
# 而不是用空凭证跑出五个红（那会把环境缺凭证读成功能坏了）。
if [ "$HAS_BRIDGE" -eq 0 ]; then
  echo "  [SKIP] 无桥接凭证（本次只有 MCP_TOKEN）：SSE 那五条判据不在射程内"
else

# 6. 握手状态码：先证明这条 SSE 真的开了（后面三条头字段才有意义）
sse_code=$(curl -s -N --max-time 2 -o /dev/null -w '%{http_code}' "${BRIDGE_HDR[@]}" \
  "$BASE/api/bridge/outbox/sse?channel=douyin&account_id=default" 2>/dev/null)
if [ "$sse_code" = "200" ]; then
  pass "SSE 建连 http=200"
else
  fail "SSE 建连 http=${sse_code}（头字段断言在下面几条里会跟着失真）"
fi

# 7. SSE 响应头
ct=$(sse_headers | grep -i "content-type:" | head -1 | tr -d '\r')
if echo "$ct" | grep -q "text/event-stream"; then
  pass "SSE Content-Type: text/event-stream"
else
  fail "SSE Content-Type 异常: $ct"
fi

# 8. SSE 缓存头
cache=$(sse_headers | grep -i "cache-control:" | head -1 | tr -d '\r')
if echo "$cache" | grep -q "no-cache"; then
  pass "SSE Cache-Control: no-cache"
else
  fail "SSE Cache-Control 异常: $cache"
fi

# 9. SSE X-Accel-Buffering（防 反向代理层 缓冲）
xab=$(sse_headers | grep -i "x-accel-buffering:" | head -1 | tr -d '\r')
if echo "$xab" | grep -q "no"; then
  pass "SSE X-Accel-Buffering: no"
else
  fail "SSE X-Accel-Buffering 异常: $xab"
fi

# 10. 带 Last-Event-ID 重连不破坏握手（旧版拿 wc -c>0 当判据，401 响应体也能过）
resume_code=$(curl -s -N --max-time 2 -o /dev/null -w '%{http_code}' -H "Last-Event-ID: evt-12345" \
  "${BRIDGE_HDR[@]}" "$BASE/api/bridge/outbox/sse?channel=douyin&account_id=default" 2>/dev/null)
if [ "$resume_code" = "200" ]; then
  pass "带 Last-Event-ID 的请求仍然 200（续订语义本身不在本脚本射程内）"
else
  fail "带 Last-Event-ID 的请求 http=$resume_code"
fi

fi

echo ""
echo "==== 综合 ===="
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
