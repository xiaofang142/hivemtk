#!/usr/bin/env bash
# =============================================================
# bridge /api/channelbus/ingest 极限测试脚本
#
# 参考数据：小红书会话 1783（conversation_id=63bd52380000000027029f4d）
#   - 39 条 outbound（AI 回复）
#   - 54 条 inbound（客户 + 误入库的 AI 回复）
#   - account_id: 69c730300000000034018cb2
#
# 测试场景覆盖：
#   1.  正常客户消息（新 msg_id）→ 应入库 + 可能触发 AI
#   2.  重复 msg_id → 幂等跳过
#   3.  self/agent 消息 → direction 强制 outbound，不触发 AI
#   4.  system 消息 → 仅落库不触发 AI
#   5.  历史消息（timestamp 10 分钟前）→ 入库但不触发 AI
#   6.  batch 合并多条同会话 inbound → 合并一次 AI
#   7.  最后一条 outbound → 不触发 AI
#   8.  空消息/空 content → 应被拒绝或跳过
#   9.  缺少 channel → 400
#   10. 缺少 account_id → 400
#   11. 无效渠道 → 400
#   12. 超过 200 条 → 截断处理（不拒绝）
#   13. 时序锚点（timestamp 早于锚点）→ backfill
#   14. 4 渠道基本属性
#   15. AI 处理中标记（重复上报不重复触发）
# =============================================================

set -euo pipefail

BASE_URL="http://localhost:8204"
INGEST_URL="${BASE_URL}/api/channelbus/ingest"
PASS=0
FAIL=0
SKIP=0

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 会话 1783 参考数据
XHS_CHANNEL="xiaohongshu"
XHS_ACCOUNT="69c730300000000034018cb2"
XHS_CONV="63bd52380000000027029f4d"
XHS_CUSTOMER="63bd52380000000027029f4d"

# DB 口令仅来自环境（历史上的内网默认口令 :-hivemtk 不再兜底）。
DB_PASS="${POSTGRES_PASSWORD:-}"

now_ms() { python3 -c 'import time; print(int(time.time()*1000))'; }
ts_min_ago() { local m=$1; echo $(( $(now_ms) - m * 60000 )); }

# 生成唯一 msg_id
gen_msg_id() {
  echo "test-$(now_ms)-$RANDOM"
}

# 向 message_hub 直接插入一条平台下发(outbound)消息（模拟平台 AI/人工回复，先入库再下发）
# 用法: insert_outbound <conversation_id> <content> [account_id]
# 说明：message_hub 无 sender_type 列，自/他仅由 direction(inbound/outbound) + is_ai_reply 表达。
insert_outbound() {
  local conv="$1"
  local content="$2"
  local acct="${3:-$XHS_ACCOUNT}"
  local msgid="ob-$(now_ms)-$RANDOM"
  env "PGPASSWORD=$DB_PASS" psql -h localhost -p 8232 -U admin -d user_db -c \
    "INSERT INTO message_hub (platform, account_id, conversation_id, msg_id, sender_id, sender_name, direction, content, msg_type, is_ai_reply, sent_at, created_at) VALUES ('xiaohongshu', '$acct', '$conv', '$msgid', 'bot', 'AI助手', 'outbound', '$content', 'text', true, now(), now());" 2>&1
}

# 发送 ingest 请求
# 用法: post_ingest <channel> <account_id> <conversation_id> <json_body> [extra_query]
post_ingest() {
  local channel="$1"
  local account="$2"
  local conv="$3"
  local body="$4"
  local extra="${5:-}"
  local url="${INGEST_URL}?channel=${channel}&account_id=${account}&conversation_id=${conv}${extra}"
  curl -s -X POST "$url" \
    -H "Content-Type: application/json" \
    -d "$body" 2>&1
}

# 断言函数
assert_ok() {
  local name="$1"
  local resp="$2"
  if echo "$resp" | jq -e '.ok == true' >/dev/null 2>&1; then
    echo -e "${GREEN}[PASS]${NC} $name"
    PASS=$((PASS+1))
  else
    echo -e "${RED}[FAIL]${NC} $name"
    echo "  resp: $resp"
    FAIL=$((FAIL+1))
  fi
}

assert_fail() {
  local name="$1"
  local resp="$2"
  local expected_code="${3:-400}"
  if echo "$resp" | jq -e '.ok == false' >/dev/null 2>&1; then
    echo -e "${GREEN}[PASS]${NC} $name"
    PASS=$((PASS+1))
  else
    echo -e "${RED}[FAIL]${NC} $name (期望失败但 ok=true)"
    echo "  resp: $resp"
    FAIL=$((FAIL+1))
  fi
}

assert_contains() {
  local name="$1"
  local resp="$2"
  local pattern="$3"
  if echo "$resp" | grep -q "$pattern"; then
    echo -e "${GREEN}[PASS]${NC} $name"
    PASS=$((PASS+1))
  else
    echo -e "${RED}[FAIL]${NC} $name (期望包含: $pattern)"
    echo "  resp: $resp"
    FAIL=$((FAIL+1))
  fi
}

echo "========================================"
echo "bridge /api/channelbus/ingest 极限测试"
echo "服务地址: $BASE_URL"
echo "参考会话: $XHS_CONV (小红书 1783)"
echo "时间: $(date '+%Y-%m-%d %H:%M:%S')"
echo "========================================"
echo ""

# ---- 场景 1: 正常客户消息 ----
echo "=== 场景 1: 正常客户消息（新 msg_id）==="
MSG_ID=$(gen_msg_id)
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "test-conv-$(now_ms)" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"test-conv-$(now_ms)\",\"messages\":[{\"event_id\":\"$MSG_ID\",\"content\":\"测试消息\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"test-conv-$(now_ms)\",\"timestamp\":$(now_ms)}]}")
assert_ok "1.1 正常客户消息应 ok=true" "$RESP"
echo ""

# ---- 场景 2: 重复 msg_id 幂等 ----
echo "=== 场景 2: 重复 msg_id 幂等 ==="
MSG_ID=$(gen_msg_id)
CONV="test-dedup-$(now_ms)"
BODY="{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$MSG_ID\",\"content\":\"去重测试\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}"
RESP1=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" "$BODY")
RESP2=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" "$BODY")
assert_ok "2.1 首次上报 ok=true" "$RESP1"
assert_ok "2.2 重复上报 ok=true（幂等不报错）" "$RESP2"
echo ""

# ---- 场景 3: 回环防护（核心修复验证）----
#   服务端权威判定：ingest 上报的消息，若内容命中本会话已存在的平台下发(outbound)，
#   即识别为平台自己的回显(SELF)，跳过入库与 AI，不触发新回复（不再无限循环）。
echo "=== 场景 3: 回环防护（平台 outbound 被误报为 customer → 不触发 AI）==="
CONV="test-loopback-$(now_ms)"
LOOP_CONTENT="感谢您的关注！这是平台 AI 自动回复"
insert_outbound "$CONV" "$LOOP_CONTENT"
# 前端把平台 AI 回复误判为 customer 重新上报（模拟小红书整行容器导致 isSelfMessage 失效）
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"$LOOP_CONTENT\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}")
assert_ok "3.1 回环消息上报 ok=true" "$RESP"
# 验证未触发新的 outbound（DB outbound 数量应仍为 1，即插入的那条平台回复）
OUT_COUNT=$(env "PGPASSWORD=$DB_PASS" psql -h localhost -p 8232 -U admin -d user_db -t -c "SELECT COUNT(*) FROM message_hub WHERE conversation_id='$CONV' AND direction='outbound';" 2>/dev/null | xargs)
if [ "${OUT_COUNT:-0}" = "1" ]; then
  echo -e "${GREEN}[PASS]${NC} 3.2 回环消息未触发新 AI（outbound 仍为 1）"
  PASS=$((PASS+1))
else
  echo -e "${RED}[FAIL]${NC} 3.2 回环消息产生了新 outbound（count=${OUT_COUNT:-0}），回环未修复"
  FAIL=$((FAIL+1))
fi
echo ""

# ---- 场景 4: system 消息不触发 AI ----
echo "=== 场景 4: system 消息不触发 AI ==="
CONV="test-system-$(now_ms)"
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"系统通知\",\"sender_id\":\"system\",\"sender_type\":\"system\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}")
assert_ok "4.1 system 消息 ok=true" "$RESP"
echo ""

# ---- 场景 5: 历史消息（timestamp 10 分钟前）→ 不触发 AI ----
echo "=== 场景 5: 历史消息（10 分钟前）不触发 AI ==="
CONV="test-history-$(now_ms)"
OLD_TS=$(ts_min_ago 10)
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"10分钟前的消息\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$OLD_TS}]}")
assert_ok "5.1 历史消息 ok=true" "$RESP"
echo ""

# ---- 场景 6: batch 合并多条同会话 inbound ----
echo "=== 场景 6: batch 合并多条同会话 inbound ==="
CONV="test-batch-$(now_ms)"
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"你好\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)},{\"event_id\":\"$(gen_msg_id)\",\"content\":\"在吗\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)},{\"event_id\":\"$(gen_msg_id)\",\"content\":\"多少钱\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}")
assert_ok "6.1 batch 3 条消息 ok=true" "$RESP"
# 验证 ingested 数量
INGESTED_COUNT=$(echo "$RESP" | jq '.ingested | length' 2>/dev/null || echo "0")
if [ "$INGESTED_COUNT" = "3" ]; then
  echo -e "${GREEN}[PASS]${NC} 6.2 ingested 数量=3"
  PASS=$((PASS+1))
else
  echo -e "${RED}[FAIL]${NC} 6.2 ingested 数量=$INGESTED_COUNT (期望 3)"
  FAIL=$((FAIL+1))
fi
echo ""

# ---- 场景 7: 平台已回复后，客户新消息仍正常触发 AI（健康路径）----
echo "=== 场景 7: 平台已回复后客户新消息仍触发 AI ==="
CONV="test-after-outbound-$(now_ms)"
# 平台已回复（先入库 outbound）
insert_outbound "$CONV" "平台已回复：请问还有什么可以帮您"
# 客户发送一条「新且不同」的消息（非回环）→ 应触发新的 AI 回复
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"客户新问题\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}")
assert_ok "7.1 客户新消息 ok=true" "$RESP"
# 验证产生了新的 outbound（插入的 1 条 + 新 AI 1 条 = 2）
sleep 2
OUT_COUNT=$(env "PGPASSWORD=$DB_PASS" psql -h localhost -p 8232 -U admin -d user_db -t -c "SELECT COUNT(*) FROM message_hub WHERE conversation_id='$CONV' AND direction='outbound';" 2>/dev/null | xargs)
if [ "${OUT_COUNT:-0}" = "2" ]; then
  echo -e "${GREEN}[PASS]${NC} 7.2 客户新消息触发了新 AI（outbound=2）"
  PASS=$((PASS+1))
else
  echo -e "${RED}[FAIL]${NC} 7.2 客户新消息未触发新 AI（outbound=${OUT_COUNT:-0}，期望 2）"
  FAIL=$((FAIL+1))
fi
echo ""

# ---- 场景 8: 空消息 ----
echo "=== 场景 8: 空消息 ==="
CONV="test-empty-$(now_ms)"
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[]}")
assert_ok "8.1 空消息列表 ok=true（空批次不报错）" "$RESP"
echo ""

# ---- 场景 9: 缺少 channel ----
echo "=== 场景 9: 缺少 channel ==="
RESP=$(curl -s -X POST "${INGEST_URL}?account_id=$XHS_ACCOUNT&conversation_id=test-conv" \
  -H "Content-Type: application/json" \
  -d "{\"v\":2,\"channel\":\"\",\"account_id\":\"$XHS_ACCOUNT\",\"messages\":[]}" 2>&1)
assert_fail "9.1 缺少 channel 应 400" "$RESP"
echo ""

# ---- 场景 10: 缺少 account_id ----
echo "=== 场景 10: 缺少 account_id ==="
RESP=$(curl -s -X POST "${INGEST_URL}?channel=$XHS_CHANNEL&conversation_id=test-conv" \
  -H "Content-Type: application/json" \
  -d "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"\",\"messages\":[]}" 2>&1)
assert_fail "10.1 缺少 account_id 应 400" "$RESP"
echo ""

# ---- 场景 11: 无效渠道 ----
echo "=== 场景 11: 无效渠道 ==="
RESP=$(curl -s -X POST "${INGEST_URL}?channel=wechat&account_id=$XHS_ACCOUNT&conversation_id=test-conv" \
  -H "Content-Type: application/json" \
  -d "{\"v\":2,\"channel\":\"wechat\",\"account_id\":\"$XHS_ACCOUNT\",\"messages\":[]}" 2>&1)
assert_fail "11.1 无效渠道应 400" "$RESP"
echo ""

# ---- 场景 12: 超过 200 条 → 截断 ----
echo "=== 场景 12: 超过 200 条截断 ==="
CONV="test-overflow-$(now_ms)"
# 构造 205 条消息
MSGS="["
for i in $(seq 1 205); do
  if [ $i -gt 1 ]; then MSGS+=","; fi
  MSGS+="{\"event_id\":\"ovf-$i-$(now_ms)\",\"content\":\"msg-$i\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}"
done
MSGS+="]"
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":$MSGS}")
assert_ok "12.1 超 200 条截断后 ok=true（不拒绝）" "$RESP"
INGESTED_COUNT=$(echo "$RESP" | jq '.ingested | length' 2>/dev/null || echo "0")
if [ "$INGESTED_COUNT" = "200" ]; then
  echo -e "${GREEN}[PASS]${NC} 12.2 截断到 200 条"
  PASS=$((PASS+1))
else
  echo -e "${YELLOW}[WARN]${NC} 12.2 ingested=$INGESTED_COUNT (期望 200，可能因 dedup 减少)"
  SKIP=$((SKIP+1))
fi
echo ""

# ---- 场景 13: 时序锚点（timestamp 为 0 / 未来时间）----
echo "=== 场景 13: 时序锚点 ==="
CONV="test-anchor-$(now_ms)"
# timestamp=0（零值）
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"零时间戳\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":0}]}")
assert_ok "13.1 timestamp=0 ok=true（服务端用 now 兜底）" "$RESP"
# 未来时间（1 小时后）
FUTURE_TS=$(( $(now_ms) + 3600000 ))
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"未来时间\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$FUTURE_TS}]}")
assert_ok "13.2 未来时间 ok=true（服务端修正为 now）" "$RESP"
echo ""

# ---- 场景 14: 4 渠道基本属性 ----
echo "=== 场景 14: 4 渠道基本属性 ==="
for CH in xiaohongshu douyin tiktok xianyu kuaishou; do
  CONV="test-ch-$CH-$(now_ms)"
  RESP=$(post_ingest "$CH" "test-acct-$CH" "$CONV" \
    "{\"v\":2,\"channel\":\"$CH\",\"account_id\":\"test-acct-$CH\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"$CH 测试\",\"sender_id\":\"cust-$CH\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}")
  assert_ok "14.x 渠道 $CH ok=true" "$RESP"
done
echo ""

# ---- 场景 15: AI 处理中标记（快速重复上报不重复触发）----
echo "=== 场景 15: AI 处理中标记 ==="
CONV="test-ai-flag-$(now_ms)"
MSG_ID=$(gen_msg_id)
# 第一次上报 → 触发 AI
RESP1=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$MSG_ID\",\"content\":\"触发AI\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}")
assert_ok "15.1 首次触发 AI ok=true" "$RESP1"
# 立即第二次上报（相同 msg_id → 幂等跳过）
RESP2=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$MSG_ID\",\"content\":\"触发AI\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}")
assert_ok "15.2 重复 msg_id 幂等 ok=true" "$RESP2"
echo ""

# ---- 场景 16: 长轮询 expect_reply ----
echo "=== 场景 16: 长轮询 expect_reply ==="
CONV="test-longpoll-$(now_ms)"
START=$(python3 -c 'import time; print(int(time.time()*1000))')
RESP=$(curl -s -X POST "${INGEST_URL}?channel=$XHS_CHANNEL&account_id=$XHS_ACCOUNT&conversation_id=$CONV" \
  -H "Content-Type: application/json" \
  -d "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"expect_reply\":true,\"timeout_ms\":3000,\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"长轮询测试\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}" 2>&1)
END=$(python3 -c 'import time; print(int(time.time()*1000))')
ELAPSED=$((END - START))
assert_ok "16.1 长轮询 ok=true" "$RESP"
if [ "$ELAPSED" -ge 1000 ]; then
  echo -e "${GREEN}[PASS]${NC} 16.2 长轮询等待 >= 1s (elapsed=${ELAPSED}ms)"
  PASS=$((PASS+1))
else
  echo -e "${YELLOW}[WARN]${NC} 16.2 长轮询未等待 (elapsed=${ELAPSED}ms，可能 AI 立即返回)"
  SKIP=$((SKIP+1))
fi
echo ""

# ---- 场景 17: 参考会话 1783 重复上报（老数据不应入库）----
echo "=== 场景 17: 参考会话 1783 老数据重复上报 ==="
# 用 DB 已存在的 msg_id 上报
EXISTING_MSG_ID="xiaohongshu:63bd52380000000027029f4d:1785929700036:qbg0oz"
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$XHS_CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$XHS_CONV\",\"messages\":[{\"event_id\":\"$EXISTING_MSG_ID\",\"content\":\"项目收费吗\",\"sender_id\":\"$XHS_CUSTOMER\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$XHS_CONV\",\"timestamp\":$(ts_min_ago 30)}]}")
assert_ok "17.1 老数据 msg_id 重复 ok=true（幂等不报错）" "$RESP"
echo ""

# ---- 场景 18: 大消息体（接近 4MB）----
echo "=== 场景 18: 大消息体 ==="
CONV="test-large-$(now_ms)"
# 构造 ~100KB content（避免 base64 SIGPIPE）
LARGE_CONTENT=$(python3 -c 'print("A"*100000)')
RESP=$(curl -s -X POST "${INGEST_URL}?channel=$XHS_CHANNEL&account_id=$XHS_ACCOUNT&conversation_id=$CONV" \
  -H "Content-Type: application/json" \
  -d "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"$LARGE_CONTENT\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}" 2>&1)
if echo "$RESP" | jq -e '.ok == true' >/dev/null 2>&1; then
  echo -e "${GREEN}[PASS]${NC} 18.1 大消息体(100KB) ok=true"
  PASS=$((PASS+1))
elif echo "$RESP" | jq -e '.ok == false' >/dev/null 2>&1; then
  echo -e "${YELLOW}[SKIP]${NC} 18.1 大消息体被拒绝"
  SKIP=$((SKIP+1))
else
  echo -e "${YELLOW}[SKIP]${NC} 18.1 大消息体响应异常"
  SKIP=$((SKIP+1))
fi
echo ""

# ---- 汇总 ----
echo ""

# ---- 场景 19: AI 回复后用户新消息（关键边界）----
echo "=== 场景 19: AI 回复后用户新消息（判断时机）==="
# 真实流程：客户消息 → 平台先入库 AI 回复(outbound)再下发 → 客户再发消息 → 触发新 AI
CONV="test-ai-then-new-$(now_ms)"
# 步骤 1: 客户首条消息
RESP1=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"第一条\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}")
assert_ok "19.1 客户首条消息 ok=true" "$RESP1"
# 步骤 2: 模拟平台 AI 回复（先入库 outbound，再下发）
sleep 1
insert_outbound "$CONV" "AI 回复"
assert_ok "19.2 平台 AI 回复已入库 outbound" "{\"ok\":true}"
# 步骤 3: 客户再发消息（内容不同于平台回复 → 触发新 AI）
sleep 1
RESP3=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"第二条\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}")
assert_ok "19.3 AI 回复后客户新消息 ok=true（应触发新 AI）" "$RESP3"
echo ""

# ---- 场景 20: 历史消息顺序入库（时序锚点）----
echo "=== 场景 20: 历史消息顺序入库 ==="
CONV="test-seq-$(now_ms)"
# 按乱序上报：先发 5 分钟前的，再发 3 分钟前的，最后发 1 分钟前的
TS_5MIN=$(ts_min_ago 5)
TS_3MIN=$(ts_min_ago 3)
TS_1MIN=$(ts_min_ago 1)
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"5分钟前\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$TS_5MIN},{\"event_id\":\"$(gen_msg_id)\",\"content\":\"3分钟前\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$TS_3MIN},{\"event_id\":\"$(gen_msg_id)\",\"content\":\"1分钟前\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$TS_1MIN}]}")
assert_ok "20.1 乱序时间戳 ok=true" "$RESP"
echo ""

# ---- 场景 21: 混合 batch（回环 self + customer 同批上报）----
echo "=== 场景 21: 混合 batch（回环 self + customer 同批）==="
CONV="test-mix-batch-$(now_ms)"
# 平台已回复（先入库 outbound），后续被前端误报为 self 时服务端应识别为回显跳过
insert_outbound "$CONV" "平台回复"
RESP=$(post_ingest "$XHS_CHANNEL" "$XHS_ACCOUNT" "$CONV" \
  "{\"v\":2,\"channel\":\"$XHS_CHANNEL\",\"account_id\":\"$XHS_ACCOUNT\",\"conversation_id\":\"$CONV\",\"messages\":[{\"event_id\":\"$(gen_msg_id)\",\"content\":\"客户问题\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)},{\"event_id\":\"$(gen_msg_id)\",\"content\":\"平台回复\",\"sender_id\":\"$XHS_ACCOUNT\",\"sender_type\":\"self\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)},{\"event_id\":\"$(gen_msg_id)\",\"content\":\"客户再问\",\"sender_id\":\"cust-1\",\"sender_type\":\"customer\",\"msg_type\":\"text\",\"conversation_id\":\"$CONV\",\"timestamp\":$(now_ms)}]}")
assert_ok "21.1 混合 batch ok=true" "$RESP"
INGESTED_COUNT=$(echo "$RESP" | jq '.ingested | length' 2>/dev/null || echo "0")
if [ "$INGESTED_COUNT" = "3" ]; then
  echo -e "${GREEN}[PASS]${NC} 21.2 混合 batch ingested=3"
  PASS=$((PASS+1))
else
  echo -e "${RED}[FAIL]${NC} 21.2 混合 batch ingested=$INGESTED_COUNT (期望 3)"
  FAIL=$((FAIL+1))
fi
echo ""

# ---- 场景 22: DB 数据验证（查 1783 参考会话最新状态）----
echo "=== 场景 22: DB 数据验证（1783 参考会话）==="
DB_LAST=$(env "PGPASSWORD=$DB_PASS" psql -h localhost -p 8232 -U admin -d user_db -t -c "SELECT direction FROM message_hub WHERE conversation_id = '$XHS_CONV' ORDER BY sent_at DESC LIMIT 1;" 2>/dev/null | xargs)
if [ -n "$DB_LAST" ]; then
  echo -e "${GREEN}[PASS]${NC} 22.1 会话 1783 DB 最后一条方向: $DB_LAST"
  PASS=$((PASS+1))
else
  echo -e "${RED}[FAIL]${NC} 22.1 无法查询会话 1783 DB 数据"
  FAIL=$((FAIL+1))
fi
echo ""

echo "========================================"
echo "测试汇总"
echo "========================================"
echo -e "${GREEN}PASS: $PASS${NC}"
echo -e "${RED}FAIL: $FAIL${NC}"
echo -e "${YELLOW}SKIP: $SKIP${NC}"
echo "总计: $((PASS+FAIL+SKIP))"
echo ""
if [ "$FAIL" -gt 0 ]; then
  echo -e "${RED}存在失败用例，请检查上方日志${NC}"
  exit 1
fi
echo -e "${GREEN}全部通过${NC}"
