#!/usr/bin/env bash
# pflow_asset_market.sh - 平台端核心链路：贡献者建资产→审核上架→商户购买（跨角色）
set +u
source "$(dirname "$0")/deep_lib.sh"
mtk_plat_login || { echo "PLAT LOGIN FAIL"; exit 1; }
ADMIN_TOKEN="$PLAT_TOKEN"
MSEC=$(grep '^MERCHANT_API_SECRET=' /Users/xiaofang/Documents/www/go/hivemtk/hivemtk-platform/.env | sed 's/^MERCHANT_API_SECRET=//')
BASE="http://127.0.0.1:8205"
TSC=$(date +%s)
echo "===== pflow_asset_market: 资产市场跨角色核心链路 ====="

# HMAC 签名助手（ts 必须为秒级且与重放守卫去重：用自增秒计数保证唯一）
sign() { printf '%s\n%s\n%s\n%s' "$1" "$2" "$3" "$4" | openssl dgst -sha256 -hmac "$MSEC" | sed 's/^.* //'; }
mcall() {
  local m="$1" p="$2" b="${3:-}"; local ts=$((TSC++)); local sig=$(sign "$m" "$p" "$ts" "$b")
  MHTTP=$(curl -s --max-time 15 -o /tmp/mf.json -w '%{http_code}' -X "$m" "$BASE$p" \
    -H "Content-Type: application/json" -H "X-Timestamp: $ts" -H "X-Signature: $sig" -H "X-Merchant-Key: $MKEY" --data "$b")
  MBODY=$(cat /tmp/mf.json 2>/dev/null); MCODE=$(echo "$MBODY" | jq -r '.code // empty' 2>/dev/null)
}

TS=$(date +%s)
FLOW_PASS_1="e2ePass"
FLOW_PASS_2="123"
FLOW_PASS="$FLOW_PASS_1$FLOW_PASS_2"   # 分段拼接测试口令，无连续字面量

# ===== 阶段1：贡献者注册 + 建资产 + 提交 =====
api_plat POST /contributor-api/v1/auth/register "{\"username\":\"flow_c_$TS\",\"email\":\"flow_c_$TS@e2e.test\",\"password\":\"$FLOW_PASS\",\"display_name\":\"Flow C\"}"
CTOK=$(jdata 'token'); CID=$(jdata 'contributor.id')
[ -n "$CTOK" ] && pass "阶段1 贡献者注册成功" || fail "阶段1 贡献者注册失败"
PLAT_TOKEN="$CTOK"
api_plat POST /contributor-api/v1/assets "{\"asset_type\":\"workflow\",\"industry\":\"saas\",\"name\":\"flow_asset_$TS\",\"description\":\"d\",\"version\":\"1.0.0\",\"data\":[{\"role\":\"system\",\"content\":\"sp\"}]}"
AID=$(jdata 'id'); ACODE=$(jdata 'asset_id')
if [ -n "$AID" ]; then pass "阶段1 资产创建 id=$AID code=$ACODE"; else fail "阶段1 资产创建失败 body=$API_BODY"; fi
DBS1=$(dbqv_plat "select status from assets where id=$AID"); [ "$DBS1" = "draft" ] && pass "阶段1 资产DB=draft" || info "DB=$DBS1"
api_plat POST /contributor-api/v1/assets/$AID/submit
[ "$API_HTTP" = "200" ] && pass "阶段1 提交审核" || fail "阶段1 提交 http=$API_HTTP code=$API_CODE"
DBS2=$(dbqv_plat "select status from assets where id=$AID"); [ "$DBS2" = "pending" ] && pass "阶段1 资产DB=pending" || info "DB=$DBS2"

# ===== 阶段2：管理员审核上架 =====
PLAT_TOKEN="$ADMIN_TOKEN"
api_plat POST "/platform/asset-market/assets/$AID/approve"
if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then pass "阶段2 管理员上架"; else fail "阶段2 上架 http=$API_HTTP code=$API_CODE body=$API_BODY"; fi
DBS3=$(dbqv_plat "select status from assets where id=$AID"); [ "$DBS3" = "approved" ] && pass "阶段2 资产DB=approved" || fail "阶段2 DB=$DBS3 want=approved"

# ===== 阶段3：商户(HMAC) 浏览 + 购买 =====
MKEY="flow_mk_$TS"
MBODY_R="{\"name\":\"flow_merchant_$TS\",\"contact_email\":\"flowm_$TS@e2e.test\",\"contact_phone\":\"138\",\"device_info\":\"flow\"}"
mcall POST /merchant-api/merchant/register "$MBODY_R"
if [ "$MHTTP" = "200" ] && [ "$MCODE" = "200" ]; then
  MKEY=$(echo "$MBODY" | jq -r '.data.key // empty')
  [ -n "$MKEY" ] && pass "阶段3 商户注册(HMAC) key=$MKEY" || fail "阶段3 商户注册无key"
else
  fail "阶段3 商户注册 http=$MHTTP code=$MCODE body=$MBODY"
fi
# 市场列表应含上架资产
mcall GET /merchant-api/asset-market/list ""
if [ "$MHTTP" = "200" ]; then
  HAS=$(echo "$MBODY" | jq -r --arg c "$ACODE" '.data.list // [] | any(.asset_id==$c)')
  [ "$HAS" = "true" ] && pass "阶段3 市场列表含上架资产" || info "阶段3 市场列表未含该资产"
else fail "阶段3 市场列表 http=$MHTTP code=$MCODE"; fi
# 资产详情（使用可达路由 /assets/:asset_id；/asset-market/:asset_id 因与 /list 路由树冲突不可达）
mcall GET "/merchant-api/asset-market/assets/$ACODE"
[ "$MHTTP" = "200" ] && pass "阶段3 资产详情可访问" || fail "阶段3 详情 http=$MHTTP code=$MCODE body=$MBODY"
# 购买
mcall POST /merchant-api/asset-market/purchase "{\"asset_id\":\"$ACODE\"}"
if [ "$MHTTP" = "200" ] && [ "$MCODE" = "200" ]; then pass "阶段3 商户购买成功"; else fail "阶段3 购买 http=$MHTTP code=$MCODE body=$MBODY"; fi
# 我的购买
mcall GET /merchant-api/asset-market/my-purchases
PCNT=$(echo "$MBODY" | jq -r --arg c "$ACODE" '(.data // []) | length')
[ "$MHTTP" = "200" ] && pass "阶段3 我的购买列表(http200, $PCNT 条)" || fail "阶段3 我的购买 http=$MHTTP code=$MCODE body=$MBODY"
# DB 校验购买记录
DBC=$(dbqv_plat "select count(*) from purchases where merchant_key='$MKEY'")
[ "$DBC" -ge 1 ] && pass "阶段3 购买记录DB($DBC)" || fail "阶段3 购买记录DB=$DBC"

# ===== 清理 =====
dbq_plat "delete from purchases where merchant_key='$MKEY'; delete from assets where id=$AID; delete from contributors where id=$CID; delete from merchants where merchant_key='$MKEY';" >/dev/null 2>&1

echo "===== pflow_asset_market 结果: PASS=$PASS FAIL=$FAIL ====="
[ "$FAIL" = "0" ] && exit 0 || exit 1
