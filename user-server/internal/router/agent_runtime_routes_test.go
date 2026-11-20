package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 迁移后所有既有入口仍在调用方提供的认证组内，并由控制器处理。
func TestAgentRuntimeRoutes_KeepPathsAndAuthGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api", func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	setupInferenceRoutes(auth)
	setupToolPermissionRoutes(auth)
	setupAgentLifecycleRoutes(auth)
	want := map[string]bool{
		"POST /api/agent/inference/run":                 false,
		"GET /api/agent/inference/stats":                false,
		"POST /api/agent/lifecycle/run":                 false,
		"GET /api/tools/permission/default":             false,
		"PUT /api/tools/permission/default":             false,
		"GET /api/tools/permission/global":              false,
		"POST /api/tools/permission/global":             false,
		"GET /api/tools/permission/agents":              false,
		"GET /api/tools/permission/agents/:agent_id":    false,
		"POST /api/tools/permission/agents/:agent_id":   false,
		"DELETE /api/tools/permission/agents/:agent_id": false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; !ok {
			t.Errorf("出现意外路由：%s", key)
			continue
		}
		want[key] = true
		if !strings.Contains(route.Handler, "internal/controller.") {
			t.Errorf("路由未交由控制器处理：%s %s", key, route.Handler)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(route.Method, strings.ReplaceAll(route.Path, ":agent_id", "7"), nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("认证中间件未覆盖 %s：HTTP %d", key, w.Code)
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("既有路由丢失：%s", route)
		}
	}
}
