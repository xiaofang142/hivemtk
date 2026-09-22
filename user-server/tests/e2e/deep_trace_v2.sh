#!/usr/bin/env bash
# deep_trace_v2.sh - 端到端验证 trace 全链路
# 验证：HTTP middleware → trace_events 表 → /api/trace/:id 查询 → /api/traces/recent 列表
# 解决：单元测试通过但 ListRecentTraces 不填 kind_counts（实跑发现 null bug）
set +u
export PATH="/opt/homebrew/bin:/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
BASE="http://127.0.0.1:8204"
PGHOST=127.0.0.1
PGPORT=8232
PGUSER=admin
# 口令仅从环境注入：set -a && . ../../.env && set +a
: "${POSTGRES_PASSWORD:?缺少 POSTGRES_PASSWORD（user 库口令，见 hivemtk/.env）}"
PGPASSWORD="$POSTGRES_PASSWORD"
PGDB=user_db

PASS=0
FAIL=0
pass() { echo "  [PASS] $1"; PASS=$((PASS+1)); }
fail() { echo "  [FAIL] $1"; FAIL=$((FAIL+1)); }

# 登录（口令：SEED_PASSWORD 覆盖 > 仓库公开的演示默认值）
ADMIN_PW="${SEED_PASSWORD:-Seed@123456}"
TOKEN=$(curl -s -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}" | jq -r '.data.token')
if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
  echo "LOGIN_FAIL"; exit 1
fi

echo "==== W3C traceparent E2E ===="

# 1. 入站带 traceparent：响应头 X-Trace-Id 应等于 traceparent 内的 trace_id
PARENT_TRACE_ID="0123456789abcdef0123456789abcdef"
PARENT_SPAN_ID="0123456789abcdef"
resp_headers=$(curl -s -D - -o /dev/null "$BASE/api/health" \
  -H "traceparent: 00-$PARENT_TRACE_ID-$PARENT_SPAN_ID-01")
echoed_trace_id=$(printf '%s' "$resp_headers" | grep -i "x-trace-id:" | head -1 | awk '{print $2}' | tr -d '\r')
if [ "$echoed_trace_id" = "$PARENT_TRACE_ID" ]; then
  pass "W3C traceparent 透传：X-Trace-Id=$echoed_trace_id"
else
  fail "W3C traceparent 异常：got=$echoed_trace_id expected=$PARENT_TRACE_ID"
fi

# 2. 响应头含 W3C traceparent（标准格式）
echoed_tp=$(printf '%s' "$resp_headers" | grep -i "^traceparent:" | head -1 | awk '{print $2}' | tr -d '\r')
if [ "$(printf '%s' "$echoed_tp" | cut -d- -f1)" = "00" ] && \
   [ "$(printf '%s' "$echoed_tp" | cut -d- -f2)" = "$PARENT_TRACE_ID" ]; then
  pass "W3C traceparent 标准格式在响应头：trace_id 部分匹配"
else
  fail "W3C traceparent 响应头异常: $echoed_tp"
fi

echo ""
echo "==== trace_events 表落库 ===="

# 先触发一次带认证的 API 调用（保证 trace 事件入总线），并等待 DBTraceSink flush（500ms 周期）
curl -s "$BASE/api/traces/recent?limit=1" -H "Authorization: Bearer $TOKEN" > /dev/null
sleep 1

# 3. DB 验证：最近的 trace_events 有非空 kind（PG DISTINCT + ORDER BY 需用子查询规避）
RECENT_KIND=$(PGPASSWORD=$PGPASSWORD psql -h $PGHOST -p $PGPORT -U $PGUSER -d $PGDB -tA -c "SELECT DISTINCT kind FROM (SELECT kind, id FROM trace_events ORDER BY id DESC LIMIT 100) AS t WHERE kind IS NOT NULL AND kind <> '' LIMIT 5" 2>/dev/null | grep -v "^$" | head -1)
if [ -n "$RECENT_KIND" ] && [ "$RECENT_KIND" != "" ]; then
  pass "trace_events 表有数据落库（kind=$RECENT_KIND）"
else
  fail "trace_events 表无数据"
fi

# 4. DB 验证：service 字段非空
RECENT_SERVICE=$(PGPASSWORD=$PGPASSWORD psql -h $PGHOST -p $PGPORT -U $PGUSER -d $PGDB -tA -c "SELECT DISTINCT service FROM (SELECT service, id FROM trace_events ORDER BY id DESC LIMIT 100) AS t WHERE service IS NOT NULL AND service <> '' LIMIT 5" 2>/dev/null | grep -v "^$" | head -1)
if [ -n "$RECENT_SERVICE" ]; then
  pass "trace_events 表 service 字段有值（$RECENT_SERVICE）"
else
  fail "trace_events service 字段空"
fi

echo ""
echo "==== /api/traces/recent 端点 ===="

# 5. 列表接口正常返回
RECENT_RESP=$(curl -s "$BASE/api/traces/recent?limit=3" -H "Authorization: Bearer $TOKEN")
TOTAL=$(printf '%s' "$RECENT_RESP" | jq -r '.count')
if [ "$TOTAL" -ge 1 ] 2>/dev/null; then
  pass "/api/traces/recent 返回 $TOTAL 条"
else
  fail "/api/traces/recent 异常: $RECENT_RESP"
fi

# 6. v3 审计发现的关键 bug：ListRecentTraces 之前不填 kind_counts
FIRST_KIND_COUNTS=$(printf '%s' "$RECENT_RESP" | jq -r '.data[0].kind_counts')
FIRST_SERVICE_COUNT=$(printf '%s' "$RECENT_RESP" | jq -r '.data[0].service_count')
if [ "$FIRST_KIND_COUNTS" != "null" ] && [ "$FIRST_KIND_COUNTS" != "" ]; then
  pass "kind_counts 已填充（v3 修复）：$FIRST_KIND_COUNTS"
else
  fail "kind_counts 仍为 null（v3 bug 复发）"
fi
if [ "$FIRST_SERVICE_COUNT" != "null" ] && [ "$FIRST_SERVICE_COUNT" != "0" ]; then
  pass "service_count 已填充：$FIRST_SERVICE_COUNT"
else
  fail "service_count 异常：$FIRST_SERVICE_COUNT"
fi

echo ""
echo "==== /api/trace/:id 端点 ===="

# 7. 查最新 trace 详情
LATEST_TRACE=$(printf '%s' "$RECENT_RESP" | jq -r '.data[0].trace_id')
DETAIL_RESP=$(curl -s "$BASE/api/trace/$LATEST_TRACE" -H "Authorization: Bearer $TOKEN")
SPAN_COUNT=$(printf '%s' "$DETAIL_RESP" | jq -r '.data.summary.span_count')
if [ "$SPAN_COUNT" -ge 1 ] 2>/dev/null; then
  pass "/api/trace/$LATEST_TRACE 返回 span_count=$SPAN_COUNT"
else
  fail "/api/trace 异常: $DETAIL_RESP"
fi

# 8. 不存在的 trace_id 返回 404
NOT_FOUND=$(curl -s "$BASE/api/trace/no-such-trace-$(date +%s)" -H "Authorization: Bearer $TOKEN")
CODE=$(printf '%s' "$NOT_FOUND" | jq -r '.code')
if [ "$CODE" = "NOT_FOUND" ]; then
  pass "不存在的 trace_id 返回 404 NOT_FOUND"
else
  fail "不存在的 trace_id 异常: $NOT_FOUND"
fi

# 9. trace_id 空字符串返回 400
EMPTY_RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/trace/" -H "Authorization: Bearer $TOKEN")
EMPTY_HTTP=$(printf '%s' "$EMPTY_RESP" | tail -1)
if [ "$EMPTY_HTTP" = "404" ] || [ "$EMPTY_HTTP" = "400" ]; then
  pass "空 trace_id 返回 $EMPTY_HTTP"
else
  echo "  [INFO] 空 trace_id 返回 $EMPTY_HTTP (gin 路由 404)"
fi

echo ""
echo "==== 综合 ===="
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
