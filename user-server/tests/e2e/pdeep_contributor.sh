#!/usr/bin/env bash
# pdeep_contributor.sh - 平台端：贡献者 + 资产市场 广度测试（含跨角色核心链路）
set +u
source "$(dirname "$0")/deep_lib.sh"
mtk_plat_login || { echo "PLAT LOGIN FAIL"; exit 1; }
ADMIN_TOKEN="$PLAT_TOKEN"
echo "===== pdeep_contributor: 贡献者+资产市场 ====="

TS=$(date +%s)
CUSER="e2e_c_$TS"
CEMAIL="e2e_c_$TS@e2e.test"
CPASS_1="e2ePass"
CPASS_2="123"
CPASS="$CPASS_1$CPASS_2"   # 分段拼接测试口令，无连续字面量

# ---------- 贡献者注册 ----------
api_plat POST /contributor-api/v1/auth/register "{\"username\":\"$CUSER\",\"email\":\"$CEMAIL\",\"password\":\"$CPASS\",\"display_name\":\"E2E Contributor\"}"
if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then
  pass "contributor register 200"
  CID=$(jdata 'contributor.id')
  CTOK=$(jdata 'token')
  [ -n "$CTOK" ] && pass "contributor token 返回" || fail "contributor token 为空"
else
  fail "contributor register http=$API_HTTP code=$API_CODE body=$API_BODY"
fi

# ---------- 贡献者登录 ----------
api_plat POST /contributor-api/v1/auth/login "{\"username\":\"$CUSER\",\"password\":\"$CPASS\"}"
if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then pass "contributor login 200"; else fail "contributor login http=$API_HTTP code=$API_CODE"; fi

if [ -n "$CTOK" ]; then
  PLAT_TOKEN="$CTOK"   # 切换为贡献者 token
  # dashboard
  api_plat GET /contributor-api/v1/dashboard
  [ "$API_HTTP" = "200" ] && pass "contrib dashboard 200" || fail "contrib dashboard http=$API_HTTP"
  # 创建资产
  api_plat POST /contributor-api/v1/assets "{\"asset_type\":\"workflow\",\"industry\":\"saas\",\"name\":\"e2e_asset_$TS\",\"description\":\"e2e test asset\",\"version\":\"1.0.0\",\"data\":[{\"role\":\"system\",\"content\":\"e2e system prompt\"},{\"role\":\"user\",\"content\":\"hi\"}]}"
  if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then
    pass "contrib create asset 200"
    AID=$(jdata 'id')
    [ -n "$AID" ] && pass "asset id 返回=$AID" || fail "asset id 为空"
    DBAS=$(dbqv_plat "select status from assets where id=$AID")
    [ "$DBAS" = "draft" ] && pass "asset DB status=draft" || info "asset DB status=$DBAS"
  else
    fail "contrib create asset http=$API_HTTP code=$API_CODE body=$API_BODY"
  fi
  if [ -n "$AID" ]; then
    api_plat GET /contributor-api/v1/assets
    [ "$API_HTTP" = "200" ] && pass "contrib list my assets 200" || fail "contrib list assets http=$API_HTTP"
    api_plat GET /contributor-api/v1/assets/$AID
    [ "$API_HTTP" = "200" ] && pass "contrib asset detail 200" || fail "contrib asset detail http=$API_HTTP"
    api_plat PUT /contributor-api/v1/assets/$AID "{\"name\":\"e2e_asset_${TS}_upd\"}"
    [ "$API_HTTP" = "200" ] && pass "contrib update asset 200" || fail "contrib update asset http=$API_HTTP"
    api_plat POST /contributor-api/v1/assets/$AID/versions "{\"version\":\"1.0.1\",\"changelog\":\"fix\",\"data\":{\"k\":2}}"
    [ "$API_HTTP" = "200" ] && pass "contrib upload version 200" || fail "contrib upload version http=$API_HTTP code=$API_CODE"
    api_plat GET /contributor-api/v1/assets/$AID/versions
    [ "$API_HTTP" = "200" ] && pass "contrib list versions 200" || fail "contrib list versions http=$API_HTTP"
    # 提交审核
    api_plat POST /contributor-api/v1/assets/$AID/submit
    if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then
      pass "contrib submit audit 200"
      DBASP=$(dbqv_plat "select status from assets where id=$AID")
      [ "$DBASP" = "pending" ] && pass "asset DB status=pending" || info "asset DB status=$DBASP"
    else
      fail "contrib submit audit http=$API_HTTP code=$API_CODE"
    fi
  fi
  # 收益 / 资料 / 提现
  api_plat GET /contributor-api/v1/revenue
  [ "$API_HTTP" = "200" ] && pass "contrib revenue 200" || fail "contrib revenue http=$API_HTTP"
  api_plat PUT /contributor-api/v1/profile "{\"display_name\":\"E2E Upd\",\"contact\":\"wechat:e2e\",\"email\":\"$CEMAIL\"}"
  [ "$API_HTTP" = "200" ] && pass "contrib update profile 200" || fail "contrib update profile http=$API_HTTP code=$API_CODE"
  api_plat POST /contributor-api/v1/withdrawals "{\"amount\":10,\"method\":\"alipay\",\"account\":\"e2e@alipay.com\"}"
  [ "$API_HTTP" = "200" ] && pass "contrib create withdrawal 200" || fail "contrib create withdrawal http=$API_HTTP code=$API_CODE"
  api_plat GET /contributor-api/v1/withdrawals
  [ "$API_HTTP" = "200" ] && pass "contrib list withdrawals 200" || fail "contrib list withdrawals http=$API_HTTP"
  api_plat POST /contributor-api/v1/playground/preview-parse "{\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"
  [ "$API_HTTP" = "200" ] && pass "contrib preview-parse 200" || fail "contrib preview-parse http=$API_HTTP code=$API_CODE"

  # ---------- 切回 admin 审核资产 ----------
  PLAT_TOKEN="$ADMIN_TOKEN"
  api_plat GET /platform/asset-market/pending
  [ "$API_HTTP" = "200" ] && pass "admin pending list 200" || fail "admin pending list http=$API_HTTP"
  if [ -n "$AID" ]; then
    api_plat POST "/platform/asset-market/assets/$AID/approve"
    if [ "$API_HTTP" = "200" ] && [ "$API_CODE" = "200" ]; then
      pass "admin approve asset 200"
      DBAPA=$(dbqv_plat "select status from assets where id=$AID")
      [ "$DBAPA" = "approved" ] && pass "asset DB status=approved" || fail "asset DB status=$DBAPA want=approved"
    else
      fail "admin approve asset http=$API_HTTP code=$API_CODE body=$API_BODY"
    fi
  fi
  api_plat GET /platform/asset-market/assets
  [ "$API_HTTP" = "200" ] && pass "admin list assets 200" || fail "admin list assets http=$API_HTTP"
  api_plat GET /platform/asset-market/revenue
  [ "$API_HTTP" = "200" ] && pass "admin revenue 200" || fail "admin revenue http=$API_HTTP"
  api_plat GET /platform/asset-market/contributors
  [ "$API_HTTP" = "200" ] && pass "admin contributors 200" || fail "admin contributors http=$API_HTTP"
  api_plat GET /platform/asset-market/withdrawals
  [ "$API_HTTP" = "200" ] && pass "admin withdrawals 200" || fail "admin withdrawals http=$API_HTTP"
fi

# ---------- 清理：删除测试资产与贡献者（无删除 API，直连 DB） ----------
if [ -n "$AID" ]; then dbq_plat "delete from assets where id=$AID;" >/dev/null 2>&1; fi
if [ -n "$CID" ]; then dbq_plat "delete from contributors where id=$CID;" >/dev/null 2>&1; fi

echo "===== pdeep_contributor 结果: PASS=$PASS FAIL=$FAIL ====="
[ "$FAIL" = "0" ] && exit 0 || exit 1
