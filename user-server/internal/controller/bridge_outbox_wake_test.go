package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

type wakeCancelWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *wakeCancelWriter) Flush() { w.ResponseRecorder.Flush(); w.cancel() }

func TestBridgeOutboxWakeController_InitialSignalHasNoPayloadOrClaim(t *testing.T) {
	requestCtx, cancel := context.WithCancel(tenant.OnlyTenant(context.Background(), 18))
	defer cancel()
	w := &wakeCancelWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	g, _ := gin.CreateTestContext(w)
	g.Request = httptest.NewRequest(http.MethodGet, "/api/bridge/outbox/wake?channel=douyin_web&account_id=account-a", nil).WithContext(requestCtx)
	NewBridgeOutboxWakeController(service.NewBridgeOutboxWakeBroker()).Stream(g)
	if w.Code != http.StatusOK {
		t.Fatalf("明确租户及规范别名应连接，实际=%d", w.Code)
	}
	if w.Body.String() != "event: outbox_ready\ndata: {}\n\n" {
		t.Fatalf("只能包含固定无正文信号，实际=%q", w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatal("必须返回事件流")
	}
}

func TestBridgeOutboxWakeController_RejectsScopeBefore200(t *testing.T) {
	old := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(old) })
	for _, query := range []string{"channel=douyin", "channel=unsupported&account_id=a", "channel=douyin&account_id=default"} {
		w := httptest.NewRecorder()
		g, _ := gin.CreateTestContext(w)
		g.Request = httptest.NewRequest(http.MethodGet, "/api/bridge/outbox/wake?"+query, nil)
		NewBridgeOutboxWakeController(service.NewBridgeOutboxWakeBroker()).Stream(g)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("缺范围必须在流200前拒绝，实际=%d", w.Code)
		}
	}
}

func TestBridgeOutboxWakeController_RealBridgeAuthentication(t *testing.T) {
	database := testutil.NewTestDB(t, &model.PlatformConfigKV{}, &model.ChannelIdentifyCode{}, &model.Channel{}, &model.ChannelAccount{})
	priorMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(priorMode) })
	// T058 后 BridgeIngressGuard 只认渠道识别码（头=X-Channel-Identify-Code，
	// 候选源=channel_identify_codes 表），不再读旧 BRIDGE_INGEST_TOKEN。
	const wakeIdentifyCode = "synthetic-wake-identify-code"
	seedCtx := tenant.WithSystemScope(context.Background())
	channel := &model.Channel{TenantID: 18, Code: "douyin", Name: "Wake fixture", Type: "dm", Enabled: true}
	if err := database.WithContext(seedCtx).Create(channel).Error; err != nil {
		t.Fatal(err)
	}
	account := &model.ChannelAccount{TenantID: 18, ChannelID: channel.ID, Name: "Wake fixture",
		IdentifyCode: "synthetic-wake-account-code", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
	if err := database.WithContext(seedCtx).Create(account).Error; err != nil {
		t.Fatal(err)
	}
	accountID := strconv.FormatUint(uint64(account.ID), 10)
	if err := repository.NewChannelIdentifyCodeRepository().Upsert(seedCtx, &model.ChannelIdentifyCode{
		TenantID: 18, ChannelID: channel.ID, Code: wakeIdentifyCode, Version: 1,
	}); err != nil {
		t.Fatalf("种子识别码写入失败: %v", err)
	}
	t.Setenv("BRIDGE_INGEST_AUTH", "")
	for _, tc := range []struct {
		name, token, account string
		tenantID             uint
		status               int
	}{
		{name: "缺少凭证不得开流", tenantID: 18, account: accountID, status: http.StatusUnauthorized},
		{name: "错误凭证不得开流", token: "invalid-identify-code", tenantID: 18, account: accountID, status: http.StatusUnauthorized},
		{name: "有效凭证且租户账号一致", token: wakeIdentifyCode, tenantID: 18, account: accountID, status: http.StatusOK},
		{name: "凭证租户不符不得开流", token: wakeIdentifyCode, tenantID: 19, account: accountID, status: http.StatusBadRequest},
		{name: "凭证账号不符不得开流", token: wakeIdentifyCode, tenantID: 18, account: "other-account", status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestCtx, cancel := context.WithCancel(edition.WithContext(tenant.OnlyTenant(context.Background(), tc.tenantID), edition.TypeCustom))
			defer cancel()
			writer := &wakeCancelWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
			router := gin.New()
			router.Use(middleware.BridgeIngressGuard())
			router.GET("/api/bridge/outbox/wake", NewBridgeOutboxWakeController(service.NewBridgeOutboxWakeBroker()).Stream)
			request := httptest.NewRequest(http.MethodGet, "/api/bridge/outbox/wake?channel=douyin&account_id="+tc.account, nil).WithContext(requestCtx)
			request.Header.Set("X-Channel-Identify-Code", tc.token)
			router.ServeHTTP(writer, request)
			if writer.Code != tc.status {
				t.Fatalf("认证必须在事件流200之前生效：实际=%d，期望=%d", writer.Code, tc.status)
			}
			if tc.status != http.StatusOK && strings.Contains(writer.Body.String(), "outbox_ready") {
				t.Fatal("认证失败不得产生唤醒事件")
			}
		})
	}
}
