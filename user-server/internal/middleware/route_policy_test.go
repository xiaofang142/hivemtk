package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 这一段守的是"匹配语义"本身：规则表是一条条字符串，判据一旦偏宽（`**` 多吃一段、
// `*` 变成前缀匹配）就会把无关路由一起关进去；判据偏窄则表里那行是死规则，
// 看起来"已经收紧了"而实际没人被拦。两种失效都不会体现在业务用例上，只能在这里断死。

func TestMatchSegments_ExactAndWildcards(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/api/system/restart", "/api/system/restart", true},
		{"/api/system/restart", "/api/system/backups", false},
		// `*` 恰好一段：既不能少一段，也不能跨段
		{"/api/users/*/password", "/api/users/42/password", true},
		{"/api/users/*/password", "/api/users/password", false},
		{"/api/users/*/password", "/api/users/42/password/extra", false},
		{"/api/users/*/password", "/api/users/42/1.0/password", false},
		// `**` 只在末尾，吃零或多段
		{"/api/kb-releases/**", "/api/kb-releases", true},
		{"/api/kb-releases/**", "/api/kb-releases/gate", true},
		{"/api/kb-releases/**", "/api/kb-releases/pkg/rollback", true},
		{"/api/kb-releases/**", "/api/kb-releases-extra", false},
		// 参数段与真实段不同名不算命中，防止把注册模板当路径去比
		{"/api/system/configs/**", "/api/system/config", false},
		{"/api/system/configs/**", "/api/system/configs", true},
		{"/api/system/configs/**", "/api/system/configs/enable_register", true},
	}
	for _, c := range cases {
		if got := matchSegments(c.pattern, c.path); got != c.want {
			t.Errorf("matchSegments(%q, %q) = %v，期望 %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestIsAdminOnlyRoute_MethodRestricted(t *testing.T) {
	// 只收紧写侧、读侧保持登录档：这条界线如果被抹平，注册页读口令规则会被 403。
	if !IsAdminOnlyRoute(http.MethodPut, "/api/auth/password-policy") {
		t.Error("PUT /api/auth/password-policy 应在仅超管表内")
	}
	if IsAdminOnlyRoute(http.MethodGet, "/api/auth/password-policy") {
		t.Error("GET /api/auth/password-policy 不该在仅超管表内（口令规则要给未登录的注册页读）")
	}
	if !IsAdminOnlyRoute(http.MethodPost, "/api/system/restart") {
		t.Error("POST /api/system/restart 应在仅超管表内")
	}
	// 表里没有的路径必须放行：守卫不是白名单，默认档是"登录即可"
	if IsAdminOnlyRoute(http.MethodGet, "/api/customer-sessions") {
		t.Error("/api/customer-sessions 不在表内，不该被判成仅超管")
	}
}

// SageX 的 upload/bootstrap 用 identify_code 在控制器/服务层完成外部身份绑定，
// 只应从宽泛 integrations 规则中排除这两个端点，不能把后台集成账号管理一并放开。
func TestIsAdminOnlyRoute_SageXExternalCredentialCarveOut(t *testing.T) {
	for _, path := range []string{
		"/api/integrations/sagex/upload",
		"/api/integrations/sagex/bootstrap",
	} {
		if IsAdminOnlyRoute(http.MethodPost, path) {
			t.Errorf("POST %s 不应命中仅超管表：SageX 由 identify_code 自证身份", path)
		}
	}

	for _, path := range []string{
		"/api/integrations",
		"/api/integrations/42/test",
		"/api/integrations/42/sync-customers",
	} {
		if !IsAdminOnlyRoute(http.MethodPost, path) {
			t.Errorf("POST %s 仍应命中仅超管表：后台集成账号管理不能因 SageX carve-out 放开", path)
		}
	}
}

func runGuard(t *testing.T, method, path, role string) (int, string, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	engine := gin.New()
	handled := false
	engine.Use(func(c *gin.Context) {
		if role != "" {
			c.Set("role", role)
		}
		c.Next()
	})
	engine.Use(RoutePolicyGuard())
	// 表里的路径都注册在 /api 组上；这里给被测 method+path 挂一个"到达即记名"的最小
	// handler，用它区分"守卫放行"与"守卫拦截"，不需要真的业务路由。
	switch method {
	case http.MethodGet:
		engine.GET(path, func(c *gin.Context) { handled = true; c.Status(http.StatusTeapot) })
	case http.MethodPut:
		engine.PUT(path, func(c *gin.Context) { handled = true; c.Status(http.StatusTeapot) })
	case http.MethodPost:
		engine.POST(path, func(c *gin.Context) { handled = true; c.Status(http.StatusTeapot) })
	case http.MethodDelete:
		engine.DELETE(path, func(c *gin.Context) { handled = true; c.Status(http.StatusTeapot) })
	default:
		t.Fatalf("未支持的 method: %s", method)
	}
	engine.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w.Code, w.Body.String(), handled
}

func TestRoutePolicyGuard_BlocksNonAdminOnListedRoute(t *testing.T) {
	code, body, handled := runGuard(t, http.MethodPut, "/api/auth/password-policy", "staff")
	if code != http.StatusForbidden {
		t.Fatalf("staff 打仅超管口应 403，实得 %d / %s", code, body)
	}
	if handled {
		t.Error("守卫必须在进 handler 之前拦住：handler 被到达了")
	}
	if !strings.Contains(body, "需要超管权限") {
		t.Errorf("403 应给出与 RequireAdminMiddleware 同一口径的文案，实得 %s", body)
	}
	// 规范信封字段是 message（不是历史守卫的 msg）
	if !strings.Contains(body, `"message"`) {
		t.Errorf("403 响应体应带 message 字段，实得 %s", body)
	}
}

func TestRoutePolicyGuard_AllowsAdminOnListedRoute(t *testing.T) {
	code, body, handled := runGuard(t, http.MethodPut, "/api/auth/password-policy", "admin")
	if !handled || code != http.StatusTeapot {
		t.Fatalf("admin 打同一条口必须放行到 handler，实得 code=%d handled=%v body=%s", code, handled, body)
	}
}

func TestRoutePolicyGuard_PassesUnlistedRouteForAnyRole(t *testing.T) {
	for _, role := range []string{"staff", "customer_service", "admin"} {
		code, body, handled := runGuard(t, http.MethodGet, "/api/customer-sessions", role)
		if !handled || code != http.StatusTeapot {
			t.Errorf("role=%s 打未列入表的口应直达 handler，实得 code=%d handled=%v body=%s", role, code, handled, body)
		}
	}
}

// TestRoutePolicyGuard_MissingRoleFailsClosed 命中表却没有身份 ⇒ 装配错误，向拒的方向收。
// 这条是"守卫被挂到没跑 JWT 的组上"的探针：漏挂不能变成静默放行。
func TestRoutePolicyGuard_MissingRoleFailsClosed(t *testing.T) {
	code, body, handled := runGuard(t, http.MethodPost, "/api/system/restart", "")
	if handled {
		t.Error("无身份时不能放行到 handler")
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("命中表但无身份应 401，实得 %d / %s", code, body)
	}
}

// TestAdminOnlyRoutePatterns_WellFormed 表本身的形状检查：
// `**` 只允许出现在末尾，路径必须绝对、无空段、无尾斜杠。
// 写歪的模式（比如中间放 `**`）在 matchSegments 里会提前 return true，
// 等于一条吞掉后面所有段的规则——这种风险不能只靠人眼评审。
func TestAdminOnlyRoutePatterns_WellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range AdminOnlyRoutePatterns() {
		p := r[1][0]
		if seen[p] {
			t.Errorf("规则表里 %s 重复出现，多出来的一行只会让覆盖率读数失真", p)
		}
		seen[p] = true
		if !strings.HasPrefix(p, "/api/") {
			t.Errorf("规则 %s 必须以 /api/ 开头（守卫挂在 /api 认证组上）", p)
		}
		if strings.HasSuffix(p, "/") {
			t.Errorf("规则 %s 结尾有多余斜杠，匹配不上任何 live 路径", p)
		}
		segs := strings.Split(strings.Trim(p, "/"), "/")
		for i, s := range segs {
			if s == "" {
				t.Errorf("规则 %s 有空段（连续斜杠）", p)
			}
			if s == "**" && i != len(segs)-1 {
				t.Errorf("规则 %s 的 ** 不在末尾，会吞掉它后面的所有段", p)
			}
			if strings.Contains(s, "*") && s != "*" && s != "**" {
				t.Errorf("规则 %s 段内出现部分通配 %q，匹配语义未定义", p, s)
			}
		}
		for _, m := range r[0] {
			if m != strings.ToUpper(m) {
				t.Errorf("规则 %s 的方法 %q 不是大写，EqualFold 之外不要留大小写歧义", p, m)
			}
		}
	}
}

// TestIsAdminOnlyRoute_ObservabilityDashboard 钉住 007 看板路由的收紧决定。
//
// 选型理由（specs/007/delivery.md §4.2）：看板回的是整个租户的 LLM 成本、送达率、
// 渠道健康与封禁域名，属「全局共享态」而不是调用者自己的数据；前端消费点只有
// admin 菜单节点 observabilityDashboard。两个判据都指向仅超管档，是这张表里
// 权限最小且不破坏界面的取值——退到 public/登录档会让任何坐席读到全租户经营成本。
//
// 这里同时断「精确命中」而不只是「被判成 adminOnly」：宽泛的 `**` 规则也会让
// IsAdminOnlyRoute 回 true，那种情况下这行路径写错了也查不出来。
func TestIsAdminOnlyRoute_ObservabilityDashboard(t *testing.T) {
	const path = "/api/observability/dashboard"
	if !IsAdminOnlyRoute(http.MethodGet, path) {
		t.Fatalf("GET %s 必须命中仅超管表（当前任何登录角色都能读全租户成本/渠道健康）", path)
	}

	exact := false
	for _, r := range adminOnlyRoutes {
		if r.Pattern == path {
			exact = true
			if len(r.Methods) != 0 {
				t.Errorf("规则 %s 的方法应留空（该路由只有 GET，写成方法白名单反而给了后人"+
					"「再加一个 method 就绕过」的错觉），实际 %v", path, r.Methods)
			}
		}
	}
	if !exact {
		t.Errorf("仅超管表里没有 %s 这一行：当前的 true 只可能来自别的通配规则误命中，"+
			"路径一旦改名这条收紧就静默失效", path)
	}
}
