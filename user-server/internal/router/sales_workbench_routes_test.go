// sales_workbench_routes_test.go I7：事件流报表读口的路由层。
//
// 两件事分开测（与 order_draft_routes_test 同思路）：
//   - 注册：两条新读口必须真的出现在路由表上，而不是只在控制器里写好了；
//   - 端到端：装配在位时 200 + code=0，days 垃圾参数 400 —— 这条走真路由、
//     真控制器、真服务、真库，任一环断了都会红。
package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/app"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

func TestSalesWorkbenchRoutes_Registered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	setupMarketingFlowRoutes(auth)

	found := map[string]bool{}
	for _, r := range engine.Routes() {
		found[r.Method+" "+r.Path] = true
	}
	// 四条一起断言：overview（项11a）、两条 I7 读口与 A10 快链是同一条竖的
	// 入口，挂一半不算挂上。
	for _, want := range []string{
		"GET /api/sales-workbench/overview",
		"GET /api/sales-workbench/team-dashboard",
		"GET /api/sales-workbench/champion",
		"GET /api/sales-workbench/quick-actions",
	} {
		if !found[want] {
			t.Errorf("路由未注册：%s（实际 %v）", want, found)
		}
	}
}

func TestSalesWorkbenchRoutes_DashboardEndToEnd(t *testing.T) {
	database := testutil.NewTestDB(t, &model.SalesEvent{})
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	app.InitSalesWorkbenchRuntime()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	setupMarketingFlowRoutes(auth)

	for _, path := range []string{
		"/api/sales-workbench/team-dashboard?days=30",
		"/api/sales-workbench/champion?days=7",
		"/api/sales-workbench/quick-actions",
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s 期望 200，实际 %d：%s", path, rec.Code, rec.Body.String())
			continue
		}
		var body struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s 响应不是 JSON：%v", path, err)
			continue
		}
		if body.Code != 0 {
			t.Errorf("%s 期望 code=0，实际 %d：%s", path, body.Code, rec.Body.String())
		}
		if len(body.Data) == 0 || string(body.Data) == "null" {
			t.Errorf("%s data 不应为 null：%s", path, rec.Body.String())
		}
	}

	// 垃圾 days 参数：400，不进读侧。
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("GET", "/api/sales-workbench/team-dashboard?days=abc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("days=abc 期望 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
}
