#!/usr/bin/env bash
# pdeep_admin.sh - 平台端：运营/管理面广度测试（admin token）
# 覆盖：health、公开联系信息、dashboard、stats、monitoring、message、installs/heartbeats、site contact
set +u
source "$(dirname "$0")/deep_lib.sh"
mtk_plat_login || { echo "PLAT LOGIN FAIL"; exit 1; }
ADMIN_TOKEN="$PLAT_TOKEN"   # 仅变量引用，token 来自 deep_lib.sh 环境注入登录结果

echo "===== pdeep_admin: 平台管理面 ====="

# ---------- 健康检查 ----------
api_plat GET /health
[ "$API_HTTP" = "200" ] && pass "health 200" || fail "health http=$API_HTTP"
api_plat GET /api/health
[ "$API_HTTP" = "200" ] && pass "api/health 200" || fail "api/health http=$API_HTTP"

# ---------- 公开联系信息（无鉴权） ----------
api_plat GET /public/site/contact
[ "$API_HTTP" = "200" ] && pass "public/site/contact 200" || fail "public/site/contact http=$API_HTTP"

# ---------- dashboard ----------
api_plat GET /platform/dashboard
if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then pass "dashboard 200"; else fail "dashboard http=$API_HTTP code=$API_CODE body=$API_BODY"; fi

# ---------- stats ----------
api_plat GET /platform/stats/system
[ "$API_HTTP" = "200" ] && pass "stats/system 200" || fail "stats/system http=$API_HTTP"
api_plat GET /platform/stats/overview
[ "$API_HTTP" = "200" ] && pass "stats/overview 200" || fail "stats/overview http=$API_HTTP"
api_plat GET /platform/stats/merchant
[ "$API_HTTP" = "200" ] && pass "stats/merchant 200" || fail "stats/merchant http=$API_HTTP"

# ---------- monitoring ----------
api_plat GET /platform/monitoring/health
[ "$API_HTTP" = "200" ] && pass "monitoring/health 200" || fail "monitoring/health http=$API_HTTP"
api_plat GET /platform/monitoring/api-metrics
[ "$API_HTTP" = "200" ] && pass "monitoring/api-metrics 200" || fail "monitoring/api-metrics http=$API_HTTP"
api_plat GET /platform/monitoring/performance
[ "$API_HTTP" = "200" ] && pass "monitoring/performance 200" || fail "monitoring/performance http=$API_HTTP"

# ---------- message ----------
api_plat GET "/platform/message/list?page=1&page_size=20"
if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then pass "message/list 200"; else fail "message/list http=$API_HTTP code=$API_CODE"; fi
MTITLE="deep_test_$(date +%s)"
api_plat POST /platform/message/send "{\"title\":\"$MTITLE\",\"content\":\"hello platform\",\"type\":\"announcement\"}"
if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then
  pass "message/send 200"
  MID=$(jdata 'id')
  # DB 校验
  DBT=$(dbqv_plat "select title from platform_messages where id=$MID")
  if [ "$DBT" = "$MTITLE" ]; then pass "message DB 落库 title 一致"; else fail "message DB title=$DBT want=$MTITLE"; fi
  # 标记已读
  api_plat POST "/platform/message/$MID/read"
  if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then pass "message/$MID/read 200"; else fail "message read http=$API_HTTP code=$API_CODE"; fi
  DBR=$(dbqv_plat "select is_read from platform_messages where id=$MID")
  if [ "$DBR" = "t" ] || [ "$DBR" = "true" ]; then pass "message DB is_read=true"; else fail "message DB is_read=$DBR"; fi
else
  fail "message/send http=$API_HTTP code=$API_CODE body=$API_BODY"
fi
api_plat GET /platform/message/latest
[ "$API_HTTP" = "200" ] && pass "message/latest 200" || fail "message/latest http=$API_HTTP"

# ---------- installs / heartbeats ----------
api_plat GET /platform/installs/list
[ "$API_HTTP" = "200" ] && pass "installs/list 200" || fail "installs/list http=$API_HTTP"
api_plat GET /platform/heartbeats/list
[ "$API_HTTP" = "200" ] && pass "heartbeats/list 200" || fail "heartbeats/list http=$API_HTTP"
api_plat GET /platform/heartbeats/stats
[ "$API_HTTP" = "200" ] && pass "heartbeats/stats 200" || fail "heartbeats/stats http=$API_HTTP"

# ---------- site contact（管理端 get/put，测后还原） ----------
api_plat GET /platform/site/contact
if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then pass "site/contact get 200"; else fail "site/contact get http=$API_HTTP code=$API_CODE"; fi
OWECHAT=$(jdata 'wechat_id'); OEMAIL=$(jdata 'email'); OPHONE=$(jdata 'phone'); OBW=$(jdata 'business_wechat_id'); OSH=$(jdata 'service_hours'); OOWNER=$(jdata 'owner')
TESTWX="e2e_test_wx_$(date +%s)"
api_plat PUT /platform/site/contact "{\"wechat_id\":\"$TESTWX\",\"email\":\"$OEMAIL\",\"phone\":\"$OPHONE\",\"business_wechat_id\":\"$OBW\",\"service_hours\":\"$OSH\",\"owner\":\"$OOWNER\"}"
if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then
  pass "site/contact put 200"
  DBW=$(dbqv_plat "select wechat_id from site_contact_config where id='default'")
  if [ "$DBW" = "$TESTWX" ]; then pass "site contact DB wechat_id 更新"; else fail "site contact DB wechat_id=$DBW want=$TESTWX"; fi
else
  fail "site/contact put http=$API_HTTP code=$API_CODE body=$API_BODY"
fi
# 还原
api_plat PUT /platform/site/contact "{\"wechat_id\":\"$OWECHAT\",\"email\":\"$OEMAIL\",\"phone\":\"$OPHONE\",\"business_wechat_id\":\"$OBW\",\"service_hours\":\"$OSH\",\"owner\":\"$OOWNER\"}" >/dev/null 2>&1

echo "===== pdeep_admin 结果: PASS=$PASS FAIL=$FAIL ====="
[ "$FAIL" = "0" ] && exit 0 || exit 1
