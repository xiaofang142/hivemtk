package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/tenant"

	"github.com/gin-gonic/gin"
)

const merchantConnectionTestPath = "/api/v1/integrations/merchant-connections"

// 走真实注册入口和管理员守卫，生产装配没有商家授权适配器。
func TestMerchantConnectionRoutes_GuardAndPending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, method, suffix, body, role string
		tenant                           uint
		want                             int
	}{
		{"anonymous status", "GET", "?provider=shop-api&shop_id=owner-shop", "", "", 0, 401},
		{"anonymous completion", "POST", "/complete", `{}`, "", 0, 401},
		{"ordinary status", "GET", "?provider=shop-api&shop_id=owner-shop", "", "agent", 72, 403},
		{"ordinary start", "POST", "/start", `{}`, "agent", 72, 403},
		{"ordinary completion", "POST", "/complete", `{}`, "agent", 72, 403},
		{"anonymous refresh", "POST", "/refresh", `{}`, "", 0, 401},
		{"ordinary refresh", "POST", "/refresh/", `{}`, "agent", 72, 403},
		{"anonymous revoke", "POST", "/revoke/", `{}`, "", 0, 401},
		{"ordinary revoke", "POST", "/revoke", `{}`, "agent", 72, 403},
		{"refresh query", "POST", "/refresh/?refresh_token=fixture", `{}`, "admin", 72, 400},
		{"revoke query", "POST", "/revoke/?refresh_token=fixture", `{}`, "admin", 72, 400},
		{"refresh missing generation", "POST", "/refresh", `{"provider":"shop-api","shop_id":"owner-shop"}`, "admin", 72, 400},
		{"refresh unavailable", "POST", "/refresh", `{"provider":"shop-api","shop_id":"owner-shop","credential_version":1}`, "admin", 72, 503},
		{"revoke forged authority", "POST", "/revoke", `{"provider":"shop-api","shop_id":"owner-shop","tenant_id":21}`, "admin", 72, 400},
		{"revoke body cannot contain credential", "POST", "/revoke/", `{"provider":"shop-api","shop_id":"owner-shop","refresh_token":"fixture"}`, "admin", 72, 400},
		{"refresh missing tenant", "POST", "/refresh", `{"provider":"shop-api","shop_id":"owner-shop","credential_version":1}`, "admin", 0, 403},
		{"revoke missing tenant", "POST", "/revoke", `{"provider":"shop-api","shop_id":"owner-shop"}`, "admin", 0, 403},
		{"global rejection keeps private headers", "POST", "/refresh/", `{}`, "limited", 0, 429},
		{"missing tenant", "GET", "?provider=shop-api&shop_id=owner-shop&tenant_id=72", "", "admin", 0, 403},
		{"admin pending", "GET", "?provider=shop-api&shop_id=owner-shop", "", "admin", 72, 200},
		{"admin unavailable start", "POST", "/start", `{"provider":"shop-api","shop_id":"owner-shop"}`, "admin", 72, 503},
		{"malformed completion", "POST", "/complete", `{"code":"fixture`, "admin", 72, 400},
		{"oversized completion", "POST", "/complete", `{"code":"` + strings.Repeat("a", 9000) + `"}`, "admin", 72, 400},
		{"query completion", "POST", "/complete?code=fixture", `{}`, "admin", 72, 400},
		{"trailing slash query completion", "POST", "/complete/?code=fixture", `{}`, "admin", 72, 400},
		{"forged authority", "POST", "/complete", `{"provider":"shop-api","shop_id":"owner-shop","state":"nonce","code":"code","tenant_id":72,"scopes":["write"]}`, "admin", 72, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(middleware.Recovery(), middleware.SecurityHeaders(), func(c *gin.Context) {
				if tc.role == "limited" {
					c.AbortWithStatus(http.StatusTooManyRequests)
				}
			}, middleware.MerchantConnectionRecovery())
			admin := engine.Group("/api", func(c *gin.Context) {
				if tc.role != "" {
					c.Set("role", tc.role)
					c.Set("user_id", uint(15))
					c.Request = c.Request.WithContext(tenant.WithTenantID(c.Request.Context(), tc.tenant))
				}
				c.Next()
			}, middleware.AdminAuthMiddleware())
			setupMerchantConnectionRoutes(admin, nil)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, merchantConnectionTestPath+tc.suffix, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: "session", Value: "fixture-session"})
			engine.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("预期状态 %d，实际 %d：%s", tc.want, w.Code, w.Body.String())
			}
			if strings.HasPrefix(tc.suffix, "/refresh") || strings.HasPrefix(tc.suffix, "/revoke") {
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("Set-Cookie") != "" {
					t.Fatal("商家凭证操作必须禁止缓存和引用泄露，且不得改写登录 Cookie")
				}
			}
			if tc.want == http.StatusOK && (!strings.Contains(w.Body.String(), `"pending_live_validation"`) || !strings.Contains(w.Body.String(), `"production_allowed":false`) || w.Header().Get("Cache-Control") != "no-store") {
				t.Fatal("生产默认状态必须明确待验证，并禁止缓存")
			}
		})
	}
}
