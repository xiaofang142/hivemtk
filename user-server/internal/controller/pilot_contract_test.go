package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/system/install"

	"github.com/gin-gonic/gin"
)

type fakePilotHTTPStore struct{}

func (fakePilotHTTPStore) Load(context.Context) (string, error)       { return "", nil }
func (fakePilotHTTPStore) Save(context.Context, string, string) error { return nil }

// 控制器只读本地 fake 安装身份；缺租户、坏 JSON、越界 body 必须明确失败。
func TestPilotContractController(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewPilotContractService(fakePilotHTTPStore{}, func() (*install.Lock, error) {
		return &install.Lock{InstallID: "ins-http-fake", Initialized: true}, nil
	})
	ctrl := NewPilotContractController(svc)
	engine := gin.New()
	engine.GET("/api/pilot/contract", ctrl.Get)
	engine.PUT("/api/pilot/contract", ctrl.Save)
	engine.POST("/api/pilot/contract/validate", ctrl.Validate)
	for _, tc := range []struct {
		name, method, path, body string
		tenantID                 uint
		status                   int
		fragment                 string
	}{
		{"unconfigured", "GET", "/api/pilot/contract", "", 42, 200, `"configured":false`},
		{"identity", "GET", "/api/pilot/contract", "", 42, 200, `"install_id":"ins-http-fake"`},
		{"no_tenant", "GET", "/api/pilot/contract", "", 0, 403, ""},
		{"malformed", "POST", "/api/pilot/contract/validate", "broken", 42, 400, ""},
		{"unknown_field", "POST", "/api/pilot/contract/validate", `{"pretend_live":true}`, 42, 400, ""},
		{"oversized", "PUT", "/api/pilot/contract", strings.Repeat("x", 65537), 42, 400, ""},
		{"no_actor", "PUT", "/api/pilot/contract", `{}`, 42, 400, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(tenant.OnlyTenant(req.Context(), tc.tenantID))
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.fragment) {
				t.Fatalf("响应=%d %s，期望=%d %s", w.Code, w.Body.String(), tc.status, tc.fragment)
			}
			if tc.status == http.StatusOK && !strings.Contains(w.Body.String(), `"code":0`) {
				t.Fatalf("信封错误: %s", w.Body.String())
			}
		})
	}
}
