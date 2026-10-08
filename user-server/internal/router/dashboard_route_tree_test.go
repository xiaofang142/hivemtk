package router

// 契约锁：SSE 三条新路由必须与既有 /dashboards 通配面共存，且不能把通配面吃掉。
//
// 立项依据：本轮把 DashboardSSEController 的三个曾 404 的 handler 接到
// /dashboards/stream|snapshot|metrics 上，而 /dashboards 下面已经有
// 「静态段 + :id 通配段」混排（/dashboards/data、/dashboards/activities 与
// /dashboards/:id 并存，另有 /dashboards/:id/data）。gin 的路由树对
// 同层「静态 vs 通配」的共存有实现差异，一旦这条线将来换 gin 大版本或改前缀，
// 冲突的表现是 **启动期 panic**：整个服务起不来，而不是某一个 404。
// 那种失效形态在部署那一刻才炸，所以判据必须在路由注册那一刻给出。
import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestDashboardsRouteTreeNoConflict 注册与生产同形的路径集，断言不 panic 且解析正确。
//
// 刻意不调用 setupDashboardRoutes（它会拉真实 DB 依赖）：这里要判的是
// 「同一棵 gin 路由树上这些路径能否共存 + 谁优先匹配」，与 handler 实现无关。
func TestDashboardsRouteTreeNoConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/api")

	// 既有面（business_routes.go setupDashboardRoutes + frontend_aliases.go）
	g.GET("/dashboards", func(c *gin.Context) { c.String(200, "list") })
	g.GET("/dashboards/data", func(c *gin.Context) { c.String(200, "data") })
	g.GET("/dashboards/activities", func(c *gin.Context) { c.String(200, "activities") })
	g.GET("/dashboards/list", func(c *gin.Context) { c.String(200, "list-alias") })
	g.GET("/dashboards/:id", func(c *gin.Context) { c.String(200, "byid:"+c.Param("id")) })
	g.GET("/dashboards/:id/data", func(c *gin.Context) { c.String(200, "byid-data:"+c.Param("id")) })

	// 本轮新接的面（service_routes.go setupSSEDashboardRoutes）
	g.GET("/dashboards/stream", func(c *gin.Context) { c.String(200, "stream") })
	g.GET("/dashboards/snapshot", func(c *gin.Context) { c.String(200, "snapshot") })
	g.GET("/dashboards/metrics", func(c *gin.Context) { c.String(200, "metrics") })

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/api/dashboards", "list"},
		{"/api/dashboards/data", "data"},
		{"/api/dashboards/activities", "activities"},
		{"/api/dashboards/list", "list-alias"},
		{"/api/dashboards/stream", "stream"},
		{"/api/dashboards/snapshot", "snapshot"},
		{"/api/dashboards/metrics", "metrics"},
		{"/api/dashboards/abc", "byid:abc"},
		{"/api/dashboards/abc/data", "byid-data:abc"},
	} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s 状态码=%d want 200（路由树冲突时这里是 404/panic）", tc.path, w.Code)
			continue
		}
		if got := w.Body.String(); got != tc.want {
			t.Errorf("%s 命中 %q，want %q（静态段必须优先于 :id 通配段）", tc.path, got, tc.want)
		}
	}
}
