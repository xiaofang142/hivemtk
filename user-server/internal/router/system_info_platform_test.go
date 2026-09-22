package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// TestSystemInfoExposesPlatformEnabled 走真实路由表打 GET /api/system/info，
// 断言响应体带 platform_enabled，且**不带任何 Authorization 头**就能拿到。
//
// 为什么钉在路由层而不是 controller：前端拿这个字段是为了决定要不要渲染"上架到官方商城"
// 入口，它依赖两件事同时成立——字段名对、这个端点在 public 组里（Playground 可能在
// 令牌刷新完成前就发这个请求）。少任何一件，入口就会永久隐藏或请求 401。
func TestSystemInfoExposesPlatformEnabled(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	for _, tc := range []struct {
		env  string
		want string
	}{
		{"false", `"platform_enabled":false`},
		{"true", `"platform_enabled":true`},
	} {
		t.Setenv("PLATFORM_ENABLED", tc.env)
		req := httptest.NewRequest(http.MethodGet, "/api/system/info", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("PLATFORM_ENABLED=%s 时 GET /api/system/info 应匿名 200，实际 %d body=%s",
				tc.env, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("PLATFORM_ENABLED=%s 响应体应含 %s，实际=%s", tc.env, tc.want, w.Body.String())
		}
	}
}
