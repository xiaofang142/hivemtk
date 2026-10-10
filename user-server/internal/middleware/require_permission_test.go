package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/pkg/utils"

	"github.com/gin-gonic/gin"
)

// require_permission_test.go 权限点门的角色映射与 fail-closed 语义（不连库，本地即跑）。
func TestRequireRBACPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	probe := func(permission string) *gin.Engine {
		r := gin.New()
		r.GET("/p", JWTAuthMiddleware(), RequireRBACPermission(permission), func(c *gin.Context) {
			c.Status(http.StatusOK)
		})
		return r
	}
	do := func(r *gin.Engine, token string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/p", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// IsTestMode 旁路会直接放行——本用例要真判角色映射，关掉并还原。
	old := IsTestMode
	IsTestMode = false
	t.Cleanup(func() { IsTestMode = old })

	// 用真实 JWT 打通 JWTAuthMiddleware → RequireRBACPermission 链。
	// jwt.go 在测试进程会用固定测试密钥（未配置 SECRET 时的既定口径）。
	mint := func(role string) string {
		tok, err := utils.NewJWTUtils(utils.DefaultJWTConfig).GenerateToken(9, "tester", role)
		if err != nil {
			t.Fatalf("签测试令牌失败: %v", err)
		}
		return tok
	}

	// ① team_admin 持有 api_key.reveal（矩阵：team_admin ✅）
	if code := do(probe("api_key.reveal"), mint("team_admin")); code != http.StatusOK {
		t.Errorf("team_admin 应通过 api_key.reveal，got %d", code)
	}
	// ② team_admin 不持有 team.manage（跨团队仅 super_admin）
	if code := do(probe("team.manage"), mint("team_admin")); code != http.StatusForbidden {
		t.Errorf("team_admin 应被 team.manage 拦下（403），got %d", code)
	}
	// ③ 普通成员不持有 api_key.reveal
	if code := do(probe("api_key.reveal"), mint("user")); code != http.StatusForbidden {
		t.Errorf("member 应被 api_key.reveal 拦下，got %d", code)
	}
	// ④ 未知角色 fail-closed
	if code := do(probe("customer.read"), mint("ghost")); code != http.StatusForbidden {
		t.Errorf("未知角色应 fail-closed，got %d", code)
	}
	// ⑤ admin（super_admin）全量
	if code := do(probe("team.manage"), mint("admin")); code != http.StatusOK {
		t.Errorf("admin 应通过 team.manage，got %d", code)
	}
}
