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
