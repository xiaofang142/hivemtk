package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// TestAnonymousAccess_AuthProtectedEndpointsReturn401 验证鉴权组端点在匿名请求下
// 统一返回 401（未提供认证令牌），而不是 404/500/200（T-12）。
func TestAnonymousAccess_AuthProtectedEndpointsReturn401(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	targets := []string{
		"/api/users",
		"/api/tenant/info",
		"/api/module-market/orders",
		"/api/clue/list",
	}
	for _, path := range targets {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s returned %d, expected 401 (body=%s)", path, w.Code, w.Body.String())
		}
	}
}

// TestAnonymousAccess_PublicEndpointsReachable 验证公开档端点匿名可访问（不因 nil 依赖 panic）。
func TestAnonymousAccess_PublicEndpointsReachable(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	targets := []string{
		"/api/module-market/list",
		"/api/module-market/payment-guide",
		"/api/tenant/regulatory-notice",
	}
	for _, path := range targets {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusInternalServerError || w.Code == http.StatusNotFound {
			t.Errorf("anonymous GET %s returned %d (body=%s)", path, w.Code, w.Body.String())
		}
	}
}
