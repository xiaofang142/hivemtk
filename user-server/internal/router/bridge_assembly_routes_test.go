package router

import (
	"strings"
	"testing"

	"hivemtk-user/internal/app"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// MD-04 冻结基线：选定渠道桥接/收件箱路由的 method+path 注册形状。
// 改前改后必须同绿——S1/S3 只移装配，不动路由表（映射/鉴权/模块闸不变）。
func TestSetup_SelectedBridgeRoutesRegisteredExactlyOnce(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	want := []struct {
		method string
		path   string
	}{
		{"POST", "/api/bridge/ingest"},
		{"GET", "/api/bridge/outbox"},
		{"POST", "/api/bridge/outbox/ack"},
		{"GET", "/api/bridge/outbox/sse"},
		{"GET", "/api/ws/channel"},
		{"POST", "/api/inbox/lock-human"},
		{"POST", "/api/inbox/unlock-human/:session_id"},
		{"POST", "/api/bridge/selector-health"},
		{"GET", "/api/bridge/selector-health"},
		// canonical 线路：与旧组逐条对应，共用 handler；旧路径仅作兼容别名。
		{"POST", "/api/channelbus/ingest"},
		{"GET", "/api/channelbus/outbox"},
		{"POST", "/api/channelbus/outbox/ack"},
		{"POST", "/api/channelbus/gateway"},
		{"GET", "/api/channelbus/gateway"},
		{"GET", "/api/channelbus/outbox/sse"},
		{"GET", "/api/channelbus/outbox/wake"},
		{"GET", "/api/channelbus/capabilities"},
		{"POST", "/api/channelbus/selector-health"},
		{"GET", "/api/channelbus/selector-health"},
		{"GET", "/api/channelbus/ws"},
	}
	for _, w := range want {
		n := 0
		var handler string
		for _, route := range r.Routes() {
			if route.Method == w.method && route.Path == w.path {
				n++
				handler = route.Handler
			}
		}
		if n != 1 {
			t.Errorf("%s %s 注册 %d 次，期望恰好 1 次", w.method, w.path, n)
		}
		if handler == "" {
			t.Errorf("%s %s handler 为空", w.method, w.path)
		}
	}
	// 桥接摄入口的 handler 身份不变（门链由桥接中间件与 CS-06 电池另钉，
	// 既有 token 特例不改成一律 JWT）。
	for _, route := range r.Routes() {
		if route.Method != "POST" {
			continue
		}
		if route.Path == "/api/bridge/ingest" || route.Path == "/api/channelbus/ingest" {
			if !strings.Contains(route.Handler, "HandleHTTPIngest") {
				t.Errorf("%s handler 身份漂移: %s", route.Path, route.Handler)
			}
		}
	}
	// canonical WS 与旧 WS 必须共用同一个 HandleWS 入口。
	for _, route := range r.Routes() {
		if route.Method == "GET" && (route.Path == "/api/ws/channel" || route.Path == "/api/channelbus/ws") {
			if !strings.Contains(route.Handler, "HandleWS") {
				t.Errorf("%s handler 身份漂移: %s", route.Path, route.Handler)
			}
		}
	}
}

// 两个独立测试 runtime：各自 Setup 不覆盖对方的 ingress/checker 全局注册、
// 同一 runtime 内选定路由不重复注册。
func TestSetup_TwoIndependentRuntimesDoNotCrossOverwrite(t *testing.T) {
	for i, name := range []string{"runtime-a", "runtime-b"} {
		t.Run(name, func(t *testing.T) {
			database := testutil.NewTestDB(t)
			dbutil.SetTestDB(database)
			defer dbutil.SetTestDB(nil)

			gin.SetMode(gin.TestMode)
			r := gin.New()
			Setup(r, database)

			count := 0
			for _, route := range r.Routes() {
				if route.Path == "/api/bridge/ingest" {
					count++
				}
			}
			if count != 1 {
				t.Errorf("runtime %d /api/bridge/ingest 注册 %d 次，期望 1", i, count)
			}
			if app.GetBridgeIngressSvc() == nil {
				t.Errorf("runtime %d 全局桥接入站实例未注册", i)
			}
		})
	}
}
