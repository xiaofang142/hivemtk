package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
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
	testutil.NewTestDB(t, &model.PlatformConfigKV{})
	t.Setenv("BRIDGE_INGEST_TOKEN", "synthetic-wake-test-token")
	t.Setenv("BRIDGE_INGEST_TOKEN_PREV", "")
	t.Setenv("BRIDGE_INGEST_AUTH", "")
	for _, tc := range []struct {
		name, token string
		status      int
	}{
		{name: "缺少凭证不得开流", status: http.StatusUnauthorized},
		{name: "错误凭证不得开流", token: "invalid", status: http.StatusUnauthorized},
		{name: "有效凭证仍须明确租户", token: "synthetic-wake-test-token", status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestCtx, cancel := context.WithCancel(tenant.OnlyTenant(context.Background(), 18))
			defer cancel()
			writer := &wakeCancelWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
			router := gin.New()
			router.Use(middleware.BridgeIngressGuard())
			router.GET("/api/bridge/outbox/wake", NewBridgeOutboxWakeController(service.NewBridgeOutboxWakeBroker()).Stream)
			request := httptest.NewRequest(http.MethodGet, "/api/bridge/outbox/wake?channel=douyin&account_id=account-a", nil).WithContext(requestCtx)
			request.Header.Set("X-Bridge-Token", tc.token)
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
