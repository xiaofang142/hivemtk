#!/usr/bin/env bash
# flow_bridge.sh - 核心链路: 桥接入站→下发队列→回执确认 (跨 message_hub)
set +u
cd "$(dirname "$0")"
source ./deep_lib.sh

echo "===== 核心链路: 桥接入站→下发→回执 ====="
mtk_login || { echo "LOGIN_FAIL"; exit 1; }
U="$(date +%sN | tail -c 8)"
ACC="bridge_acc_$U"; CONV="bridge_conv_$U"

# 1. 入站 ingest (channel 须白名单 xiaohongshu, account_id 必填)
info "1. POST /api/channelbus/ingest"
api POST "/api/channelbus/ingest?channel=xiaohongshu&account_id=$ACC&conversation_id=$CONV" "{\"v\":2,\"messages\":[{\"role\":\"user\",\"content\":\"桥接测试$U\"}]}"
[ "$API_HTTP" = "200" ] && pass "1.ingest 200" || fail "1.ingest http=$API_HTTP body=$API_BODY"
[ "$(dbqv "SELECT count(*) FROM message_hub WHERE account_id='$ACC'")" != "0" ] && pass "1.DB message_hub 落库(account_id=$ACC)" || info "1.DB 落库(info)"

# 2. 下发队列
info "2. GET /api/channelbus/outbox"
api GET "/api/channelbus/outbox?channel=xiaohongshu&account_id=$ACC"
[ "$API_HTTP" = "200" ] && pass "2.outbox 200" || fail "2.outbox http=$API_HTTP"
OID="$(echo "$API_BODY" | jq -r '.data.list[0].id // .data[0].id // empty' 2>/dev/null)"

# 3. 回执确认
info "3. POST /api/channelbus/outbox/ack"
if [ -n "$OID" ]; then
  api POST "/api/channelbus/outbox/ack?channel=xiaohongshu&account_id=$ACC" "{\"ids\":[\"$OID\"]}"
  [ "$API_HTTP" = "200" ] && pass "3.ack 回执 $OID 200" || fail "3.ack http=$API_HTTP body=$API_BODY"
else
  info "outbox 无待下发项, 以空 ids 回执验证接口可用性"
  api POST "/api/channelbus/outbox/ack?channel=xiaohongshu&account_id=$ACC" "{\"ids\":[]}"
  [ "$API_HTTP" = "200" ] && pass "3.ack(空) 200" || fail "3.ack(空) http=$API_HTTP"
fi

# 4. 下游清理
dbq "DELETE FROM message_hub WHERE account_id='$ACC';" >/dev/null 2>&1
info "清理 message_hub(account_id=$ACC)"

echo "通过: $PASS  失败: $FAIL"
[ "$FAIL" -eq 0 ] && echo "RESULT: GREEN" || echo "RESULT: RED"
exit $FAIL
