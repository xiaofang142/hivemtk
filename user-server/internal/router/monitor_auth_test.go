package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"

	"github.com/gin-gonic/gin"
)

// TestMonitorRoutes_RequireAuth 守护 /api/monitor/* 的登录态要求。
//
// 背景（2026-09-16 审计发现）：`monitor.RegisterRoutes(auth)` 曾注册在
// `auth.Use(middleware.JWTAuthMiddleware())` **之前**。gin 的 RouterGroup.Use
// 只在**路由注册时**把当时的 handler 链快照进路由，对之后 `Use` 追加的中间件
// 不生效 —— 于是这 7 个接口长期匿名可访问。
//
// 实证：启动日志中 `/api/monitor/health` 的 handler 数为 11，而同样需要登录的
// `/api/users`、`/api/auth/notifications` 为 12，差值恰为 JWTAuthMiddleware；
// 且生产日志里出现过公网 IP 直接 GET /api/monitor/health 返回 200。
//
// 这些接口会返回会话链路明细（MessageTrace：按 conversation_id 查 trace 节点），
// 属敏感数据，必须保持登录态可见。此测试即用于防止该缺陷复发。
func TestMonitorRoutes_RequireAuth(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	monitorPaths := []string{
		"/api/monitor/health",
		"/api/monitor/anomalies",
		"/api/monitor/node-health",
		"/api/monitor/latency",
		"/api/monitor/lifecycle",
		"/api/monitor/traces",
		"/api/monitor/trace-tree",
	}

	registered := map[string]bool{}
	for _, ri := range r.Routes() {
		registered[ri.Path] = true
	}
	for _, p := range monitorPaths {
		if !registered[p] {
			t.Fatalf("路由未注册：%s（monitor.RegisterRoutes 是否仍被调用？）", p)
		}
	}

	for _, p := range monitorPaths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s 未携带令牌应返回 401，实际 %d（body=%s）", p, w.Code, w.Body.String())
		}
	}
}

// TestMonitorRoutes_ReachableWithToken 对照组：带合法令牌时不应再被 401 拦截。
// 仅断言「不是 401」，不校验业务结果（依赖 DB 中是否有 trace 数据）。
func TestMonitorRoutes_ReachableWithToken(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	token, err := utils.NewJWTUtils(utils.DefaultJWTConfig).GenerateToken(1, "admin", "admin")
	if err != nil {
		t.Fatalf("生成测试令牌失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/monitor/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Fatalf("/api/monitor/health 携带合法令牌仍返回 401（body=%s）", w.Body.String())
	}
}
