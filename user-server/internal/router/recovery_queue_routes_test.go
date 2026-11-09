package router

// 挽回队列写口的接线条：四条 POST 必须真的注册在 /recovery-queue 下，且统一被
// AdminAuthMiddleware 拦住。三条 GET 保持登录态可读（它们本来就在）。
//
// 这里只搭一条最小链（上游身份桩 + 真实 AdminAuthMiddleware + 真实 setup 函数），
// 不装配整个 Setup()：行为腿要的只是"守卫在链上且真的拒绝"，全量装配会把这些
// 断言埋进几百条路由里，还得依赖测试库。合法调用改状态的那条腿在
// internal/controller/recovery_queue_test.go（用内存仓储，不碰 PG）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// recoveryQueueEngine 用生产注册函数搭引擎。role 为空模拟"未登录"（上下文没有 role），
// 非空模拟上游 JWT 已经写入的角色。
func recoveryQueueEngine(role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api", func(c *gin.Context) {
		if role != "" {
			c.Set("role", role)
		}
		c.Next()
	})
	// 接线条测试：handler 不会被触达（写口先被 AdminAuthMiddleware 拦），
	// 这里不需要真库，只要一份真实服务实例过构造器即可。
	setupRecoveryQueueRoutes(api, service.NewRecoveryQueueService())
	return r
}

func TestRecoveryQueueWriteRoutesRegistered(t *testing.T) {
	engine := recoveryQueueEngine("admin")

	have := map[string]bool{}
	for _, ri := range engine.Routes() {
		have[ri.Method+" "+ri.Path] = true
	}
	for _, want := range []string{
		"GET /api/recovery-queue/list",
		"GET /api/recovery-queue/distribution",
		"GET /api/recovery-queue/ready",
		"POST /api/recovery-queue/enqueue",
		"POST /api/recovery-queue/:id/attempt",
		"POST /api/recovery-queue/:id/recovered",
		"POST /api/recovery-queue/:id/cancel",
	} {
		if !have[want] {
			t.Errorf("路由表缺 %s（写口没收进来或路径改了）", want)
		}
	}
}

// 未带 admin 凭据调写口必须被拒：匿名 401、已登录非管理员 403，
// 且拒绝文案来自 AdminAuthMiddleware，而不是 handler 自己回的。
func TestRecoveryQueueWriteRoutesRejectNonAdmin(t *testing.T) {
	writes := []struct{ path, body string }{
		{"/api/recovery-queue/enqueue", `{"customer_id":"c1"}`},
		{"/api/recovery-queue/1/attempt", `{"channel":"sms","stage":"failed"}`},
		{"/api/recovery-queue/1/recovered", `{"recovery_value":100}`},
		{"/api/recovery-queue/1/cancel", ""},
	}
	cases := []struct {
		name, role string
		want       int
		wantMsg    string
	}{
		{"匿名", "", http.StatusUnauthorized, "未找到用户角色信息"},
		{"已登录非管理员", "agent", http.StatusForbidden, "权限不足，需要管理员权限"},
	}
	for _, tc := range cases {
		engine := recoveryQueueEngine(tc.role)
		for _, w := range writes {
			t.Run(tc.name+"/"+w.path, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, w.path, strings.NewReader(w.body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				if rec.Code != tc.want {
					t.Errorf("%s 期望 %d，实得 %d（body=%s）", w.path, tc.want, rec.Code, rec.Body.String())
				}
				if !strings.Contains(rec.Body.String(), tc.wantMsg) {
					t.Errorf("%s 拒绝文案应含 %q，实得 %s", w.path, tc.wantMsg, rec.Body.String())
				}
			})
		}
	}
}
