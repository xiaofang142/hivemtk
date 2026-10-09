// sales_workbench_test.go I7：事件流报表读口的控制器层形状。
//
// 只测三条决定可用性的出口（与 order-draft 控制器用例同思路）：
//   - 服务没传进来（装配层还没挂）⇒ 503，不是 200+空壳；
//   - 服务在但 stats 没注入（运行时装配了却没数据源）⇒ 同样 503；
//   - days 参数非数/越界 ⇒ 400，垃圾参数不进读侧。
//
// 200 的数据形状在 service 层测（需要真库），router 层测注册与端到端。
package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestSalesWorkbenchDashboard_Endpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name     string
		svc      *service.SalesWorkbenchService
		path     string
		wantCode int
	}{
		{"服务未传入-503", nil, "/team-dashboard", http.StatusServiceUnavailable},
		{"服务未传入-503-champion", nil, "/champion", http.StatusServiceUnavailable},
		{"stats未注入-503", service.NewSalesWorkbenchService(), "/team-dashboard", http.StatusServiceUnavailable},
		{"stats未注入-503-champion", service.NewSalesWorkbenchService(), "/champion", http.StatusServiceUnavailable},
		{"days非数-400", service.NewSalesWorkbenchService(), "/team-dashboard?days=abc", http.StatusBadRequest},
		{"days为0-400", service.NewSalesWorkbenchService(), "/team-dashboard?days=0", http.StatusBadRequest},
		{"days越界-400", service.NewSalesWorkbenchService(), "/champion?days=4000", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			c := NewSalesWorkbenchController(tc.svc)
			engine.GET("/team-dashboard", c.GetTeamDashboard)
			engine.GET("/champion", c.GetChampionProfile)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
			if rec.Code != tc.wantCode {
				t.Errorf("%s 期望 %d，实际 %d：%s", tc.path, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestSalesWorkbenchQuickActions 快链端点（A10：服务方法零暴露多轮）。
//
// 两条出口 + URL 死链回归锁：静态列表无参、stats 未注入也可 200（与
// team-dashboard 不同——它不需要事件流数据源）；4 条死链修复后的 URL
// 不得回退（/dashboard/drafts/new 落 :id=new 404 的旧值）。
func TestSalesWorkbenchQuickActions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 服务未传入 ⇒ 503
	engine := gin.New()
	c := NewSalesWorkbenchController(nil)
	engine.GET("/quick-actions", c.GetQuickActions)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("GET", "/quick-actions", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("未装配期望 503，实际 %d：%s", rec.Code, rec.Body.String())
	}

	// 服务在（stats 未注入）⇒ 200 且 5 条、URL 全为真实落点
	engine = gin.New()
	c = NewSalesWorkbenchController(service.NewSalesWorkbenchService())
	engine.GET("/quick-actions", c.GetQuickActions)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("GET", "/quick-actions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, dead := range []string{"/dashboard/drafts/new", "/dashboard/ai/transfer", "/dashboard/leads", "/dashboard/sales/"} {
		if contains := strings.Contains(body, dead); contains {
			t.Errorf("死链回退：%s 仍在响应中", dead)
		}
	}
	for _, alive := range []string{"/dashboard/drafts", "/inbox/list", "/followups/today", "/clue/list", "/sales-workbench"} {
		if !strings.Contains(body, alive) {
			t.Errorf("真实落点缺失：%s", alive)
		}
	}
	if !strings.Contains(body, `"list"`) {
		t.Errorf("响应应为 {list:[...]} 包装，实际 %s", body)
	}
}
