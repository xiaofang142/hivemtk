package bridge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 未由上游绑定租户时，所有模式都必须带合法识别码；on 模式绑定租户，off 模式只解析身份。
// 旧 bridge_accounts 行自证、tenant_code 与 bartoken 双 authority 均不再成立。

func bindingCtx(t *testing.T, rawQuery string, headers map[string]string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodGet, "/api/bridge/outbox?"+rawQuery, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.Request = req
	return c
}

func modeOn(t *testing.T) {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
}

func TestBindBridgeTenant_IdentifyCodeRequiredInAllModes(t *testing.T) {
	for _, mode := range []tenant.Mode{tenant.ModeOff, tenant.ModeOn} {
		t.Run(string(mode), func(t *testing.T) {
			testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
			tenant.SetMode(mode)
			t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
			c := bindingCtx(t, "channel=douyin&account_id=1", nil)
			if _, err := bindBridgeTenant(c, "douyin", "1"); !errors.Is(err, ErrBridgeTenantUnresolved) {
				t.Fatalf("缺 identify_code 必须拒绝: %v", err)
			}
		})
	}
}

func TestBridgeTenant_PreboundContextWithoutIdentifyCode(t *testing.T) {
	bound := tenant.WithTenantID(context.Background(), 11)
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		allowed bool
	}{
		{name: "bound-tenant", ctx: bound, allowed: true},
		{name: "unbound", ctx: context.Background()},
		{name: "zero-tenant", ctx: tenant.WithTenantID(context.Background(), 0)},
		{name: "system", ctx: tenant.WithSystemScope(context.Background())},
		{name: "system-with-tenant", ctx: tenant.WithSystemScope(bound)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := bindingCtx(t, "channel=douyin&account_id=1", nil)
			c.Request = c.Request.WithContext(tc.ctx)
			before := c.Request
			httpCtx, httpErr := bindBridgeTenant(c, "douyin", "1")
			wsCtx, wsErr := BridgeTenantForRequest(before, "douyin", "1")
			for _, result := range []struct {
				entry string
				ctx   context.Context
				err   error
			}{
				{entry: "http", ctx: httpCtx, err: httpErr},
				{entry: "ws", ctx: wsCtx, err: wsErr},
			} {
				if tc.allowed && result.err != nil {
					t.Errorf("%s 已绑定租户应放行: %v", result.entry, result.err)
				}
				if !tc.allowed && !errors.Is(result.err, ErrBridgeTenantUnresolved) {
					t.Errorf("%s 不安全上下文应拒绝: %v", result.entry, result.err)
				}
				if result.ctx != tc.ctx {
					t.Errorf("%s 不应改写调用方上下文", result.entry)
				}
			}
			if c.Request != before {
				t.Fatal("不应改写原请求")
			}
		})
	}
}

func TestBindBridgeTenant_ValidIdentifyCodeBindsTenant(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "owned")
	c := bindingCtxWithIdentifyCode(t, "channel=douyin&account_id="+fixture.AccountID, fixture.Code)

	ctx, err := bindBridgeTenant(c, "douyin", fixture.AccountID)
	if err != nil {
		t.Fatalf("合法 identify_code 应解析成功: %v", err)
	}
	if id, bound := tenant.FromContext(ctx); !bound || id != 11 {
		t.Fatalf("返回值 ctx 应绑到 11: bound=%v id=%d", bound, id)
	}
	if id, bound := tenant.FromContext(c.Request.Context()); !bound || id != 11 {
		t.Fatalf("c.Request 应同步绑到 11: bound=%v id=%d", bound, id)
	}
	identity, ok := BridgeIdentityFromContext(c)
	if !ok || identity.TenantID != 11 || identity.AccountID != fixture.AccountID || identity.IdentifyCode != fixture.AccountCode {
		t.Fatalf("统一身份不完整: %+v ok=%v", identity, ok)
	}
}

func TestBindBridgeTenant_OffModeResolvesWithoutBinding(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "offline")
	c := bindingCtxWithIdentifyCode(t, "channel=douyin&account_id="+fixture.AccountID, fixture.Code)
	before := c.Request

	ctx, err := bindBridgeTenant(c, "douyin", fixture.AccountID)
	if err != nil {
		t.Fatalf("off 模式合法识别码不应失败: %v", err)
	}
	if ctx != before.Context() || c.Request != before {
		t.Fatal("off 模式不应改写请求 ctx")
	}
	if _, bound := tenant.FromContext(ctx); bound {
		t.Fatal("off 模式不应绑定租户")
	}
	if identity, ok := BridgeIdentityFromContext(c); !ok || identity.TenantID != 11 {
		t.Fatalf("off 模式仍应解析统一身份: %+v ok=%v", identity, ok)
	}
}

func TestBindBridgeTenant_AccountMismatchRejected(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "owned")
	c := bindingCtxWithIdentifyCode(t, "channel=douyin&account_id=999999", fixture.Code)
	if _, err := bindBridgeTenant(c, "douyin", "999999"); !errors.Is(err, ErrBridgeTenantAmbiguous) {
		t.Fatalf("识别码与显式账号不一致必须拒绝: %v", err)
	}
}

func TestBindBridgeTenant_ExternalAccountHintDoesNotOverrideIdentifyCode(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "owned")
	// 桥接扩展上报的是第三方平台账号 ID，不是 channel_accounts 行 ID。
	c := bindingCtxWithIdentifyCode(t, "channel=douyin&account_id=69c62722000000003203b0fe", fixture.Code)
	ctx, err := bindBridgeTenant(c, "douyin", "69c62722000000003203b0fe")
	if err != nil {
		t.Fatalf("第三方账号标识不应触发租户歧义: %v", err)
	}
	identity, ok := BridgeIdentityFromContext(c)
	if !ok || identity.TenantID != 11 || identity.AccountID != fixture.AccountID {
		t.Fatalf("内部账号必须仍取识别码解析结果: identity=%+v ok=%v", identity, ok)
	}
	if tid, bound := tenant.FromContext(ctx); !bound || tid != 11 {
		t.Fatalf("必须绑定识别码租户: tid=%d bound=%v", tid, bound)
	}
}

func TestBindBridgeTenant_ChannelHintDoesNotOverrideIdentifyCode(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "owned")
	c := bindingCtxWithIdentifyCode(t, "channel=weibo&account_id="+fixture.AccountID, fixture.Code)
	ctx, err := bindBridgeTenant(c, "weibo", fixture.AccountID)
	if err != nil {
		t.Fatalf("合法识别码不应被客户端 channel hint 覆盖: %v", err)
	}
	identity, ok := BridgeIdentityFromContext(c)
	if !ok || identity.TenantID != 11 || identity.Channel != "douyin" || identity.AccountID != fixture.AccountID {
		t.Fatalf("统一身份必须采用识别码的 canonical 渠道与账号: %+v ok=%v", identity, ok)
	}
	if tid, bound := tenant.FromContext(ctx); !bound || tid != 11 {
		t.Fatalf("on 模式必须绑定识别码租户: tid=%d bound=%v", tid, bound)
	}
}

func TestBindBridgeTenant_SageXServiceBindingUsesMessagePlatform(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	fixture := seedCanonicalBridgeFixture(t, db, 18, "sagex-t18", "service-binding")
	for _, platform := range []string{"qq", "personal_wx"} {
		c := bindingCtxWithIdentifyCode(t, "channel="+platform+"&account_id="+fixture.AccountID, fixture.Code)
		ctx, err := bindBridgeTenant(c, platform, fixture.AccountID)
		identity, ok := BridgeIdentityFromContext(c)
		if err != nil || !ok || identity.Channel != platform || identity.AccountID != fixture.AccountID {
			t.Fatalf("SageX 绑定仅证明归属，消息渠道应为 %s：identity=%+v err=%v", platform, identity, err)
		}
		if tid, bound := tenant.FromContext(ctx); !bound || tid != 18 {
			t.Fatalf("消息渠道不得改变凭据租户：%d/%v", tid, bound)
		}
	}
	for _, platform := range []string{"", "sagex-t18", "douyin"} {
		c := bindingCtxWithIdentifyCode(t, "account_id="+fixture.AccountID, fixture.Code)
		if _, err := bindBridgeTenant(c, platform, fixture.AccountID); !errors.Is(err, ErrBridgeTenantUnresolved) {
			t.Fatalf("SageX 缺少真实平台或平台不受支持时必须拒绝：%q err=%v", platform, err)
		}
	}
}

func TestBindBridgeTenant_InvalidIdentifyCodeRejected(t *testing.T) {
	testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	c := bindingCtxWithIdentifyCode(t, "channel=douyin&account_id=1", "not-a-valid-code")
	if _, err := bindBridgeTenant(c, "douyin", "1"); !errors.Is(err, ErrBridgeTenantUnresolved) {
		t.Fatalf("无效 identify_code 必须拒绝: %v", err)
	}
}

func TestBindBridgeTenant_AlreadyBoundSameTenantNoop(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "owned")
	c := bindingCtxWithIdentifyCode(t, "channel=douyin&account_id="+fixture.AccountID, fixture.Code)
	c.Request = c.Request.WithContext(tenant.WithTenantID(c.Request.Context(), 11))
	before := c.Request
	ctx, err := bindBridgeTenant(c, "douyin", fixture.AccountID)
	if err != nil || ctx != before.Context() || c.Request != before {
		t.Fatalf("同租户已绑定应 noop: ctx_changed=%v request_changed=%v err=%v", ctx != before.Context(), c.Request != before, err)
	}
}

func TestBindBridgeTenant_AlreadyBoundDifferentTenantRejected(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "owned")
	c := bindingCtxWithIdentifyCode(t, "channel=douyin&account_id="+fixture.AccountID, fixture.Code)
	c.Request = c.Request.WithContext(tenant.WithTenantID(c.Request.Context(), 99))
	if _, err := bindBridgeTenant(c, "douyin", fixture.AccountID); !errors.Is(err, ErrBridgeTenantAmbiguous) {
		t.Fatalf("跨租户 ctx 与识别码冲突必须拒绝: %v", err)
	}
}

func TestBindBridgeTenant_AliasChannelMatchesCanonical(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "alias")
	c := bindingCtxWithIdentifyCode(t, "channel=douyin_web&account_id="+fixture.AccountID, fixture.Code)
	ctx, err := bindBridgeTenant(c, "douyin_web", fixture.AccountID)
	if err != nil {
		t.Fatalf("规范别名应匹配 canonical 渠道: %v", err)
	}
	if id, _ := tenant.FromContext(ctx); id != 11 {
		t.Fatalf("别名请求应绑到 11: %d", id)
	}
}

func TestBindBridgeTenant_ChannelFallbackQueryErrorRejected(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "fallback-error")
	queryErr := errors.New("fallback channel query failed")
	const callback = "test:bridge-channel-fallback-query-error"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			tx.AddError(queryErr)
		}
	}); err != nil {
		t.Fatalf("注册渠道查询故障失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	c := bindingCtxWithIdentifyCode(t, "channel=douyin&account_id="+fixture.AccountID, fixture.Code)
	if _, err := bindBridgeTenant(c, "douyin", fixture.AccountID); !errors.Is(err, queryErr) {
		t.Fatalf("channel fallback 运行错误必须可见: %v", err)
	}
}

// 账号或权威识别码查询故障必须可见，不能继续回退成另一条身份路径。
func TestBindBridgeTenant_IdentityLookupQueryErrorRejected(t *testing.T) {
	for _, table := range []string{"channel_accounts", "channel_identify_codes"} {
		t.Run(table, func(t *testing.T) {
			db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
			modeOn(t)
			fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "lookup-error")
			queryErr := errors.New("identity lookup query failed")
			const callback = "test:bridge-identity-lookup-query-error"
			if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					tx.AddError(queryErr)
				}
			}); err != nil {
				t.Fatalf("注册身份查询故障失败: %v", err)
			}
			t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
			c := bindingCtxWithIdentifyCode(t, "channel=douyin&account_id="+fixture.AccountID, fixture.Code)
			if _, err := bindBridgeTenant(c, "douyin", fixture.AccountID); !errors.Is(err, queryErr) {
				t.Fatalf("身份查询运行错误必须可见: %v", err)
			}
			if _, ok := BridgeIdentityFromContext(c); ok {
				t.Fatal("查询故障不得产生已解析身份")
			}
		})
	}
}

func TestBindBridgeTenant_ChannelCodeDisabledAccountCannotSend(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	channel := &model.Channel{TenantID: 11, Name: "douyin", Code: "douyin", Type: "dm", Enabled: true}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(channel).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	account := &model.ChannelAccount{TenantID: 11, ChannelID: channel.ID, Name: "disabled-only", IdentifyCode: "disabled-account-code", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(account).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Model(&model.ChannelAccount{}).Where("id = ?", account.ID).Update("status", 0).Error; err != nil {
		t.Fatalf("disable account: %v", err)
	}
	const identifyCode = "disabled-channel-code"
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(&model.ChannelIdentifyCode{TenantID: 11, ChannelID: channel.ID, Code: identifyCode, Version: 1}).Error; err != nil {
		t.Fatalf("seed identify code: %v", err)
	}
	c := bindingCtxWithIdentifyCode(t, "channel=douyin", identifyCode)
	ctx, err := bindBridgeTenant(c, "douyin", "")
	if err != nil {
		t.Fatalf("channel-level code 应解析身份: %v", err)
	}
	identity, ok := BridgeIdentityFromContext(c)
	if !ok || identity.AccountID == "" {
		t.Fatalf("channel-level code 应补全账号: %+v", identity)
	}
	if _, err := service.NewChannelDispatchService().ResolveExplicitOutboundAccount(ctx, 11, "douyin", identity.AccountID); err == nil {
		t.Fatal("disabled-only 账号不得通过显式出站校验")
	}
}

func TestBridgeTenantForRequest_IdentifyCode(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "ws")
	req := httptest.NewRequest(http.MethodGet, "/api/ws/channel", nil)
	req = req.WithContext(edition.WithContext(req.Context(), edition.TypeCustom))
	req.Header.Set("X-Channel-Identify-Code", fixture.Code)
	ctx, err := BridgeTenantForRequest(req, "douyin", fixture.AccountID)
	if err != nil {
		t.Fatalf("WS 合法识别码应成功: %v", err)
	}
	if id, bound := tenant.FromContext(ctx); !bound || id != 11 {
		t.Fatalf("WS ctx 应绑到 11: bound=%v id=%d", bound, id)
	}
}

func TestBridgeTenantForRequest_MissingCodeRejected(t *testing.T) {
	testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	modeOn(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ws/channel", nil)
	if _, err := BridgeTenantForRequest(req, "douyin", "1"); !errors.Is(err, ErrBridgeTenantUnresolved) {
		t.Fatalf("WS 缺识别码必须拒绝: %v", err)
	}
}

func TestBridgeTenantForRequest_OffModePassesThroughWithoutBinding(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	fixture := seedCanonicalBridgeFixture(t, db, 11, "douyin", "ws-off")
	req := httptest.NewRequest(http.MethodGet, "/api/ws/channel", nil)
	req = req.WithContext(edition.WithContext(req.Context(), edition.TypeCustom))
	req.Header.Set("X-Channel-Identify-Code", fixture.Code)
	ctx, err := BridgeTenantForRequest(req, "douyin", fixture.AccountID)
	if err != nil || ctx != req.Context() {
		t.Fatalf("off 模式应原样 ctx 且不失败: changed=%v err=%v", ctx != req.Context(), err)
	}
	if _, bound := tenant.FromContext(ctx); bound {
		t.Fatal("off 模式不应绑定租户")
	}
}
