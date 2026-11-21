package router

import (
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 路由只能走 tenantAdmin；普通角色在触达安装身份之前被拒绝。
func TestPilotContractRoutes_AdminGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	admin := engine.Group("/api", middleware.AdminAuthMiddleware())
	setupPilotContractRoutes(admin, nil)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/pilot/contract"}, {"PUT", "/api/pilot/contract"}, {"POST", "/api/pilot/contract/validate"},
	} {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("未授权身份访问 %s 得到 %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}
