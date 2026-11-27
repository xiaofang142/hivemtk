package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// TestLeadNotifyHistoryRoutesAreTenantScoped 独立历史与汇总端点必须挂到生产 controller，并返回统一响应。
func TestLeadNotifyHistoryRoutesAreTenantScoped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t, &model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{})
	ctrl := &LeadMiningNotifyRouteController{
		svc:    service.NewLeadMiningNotifyRouteService(),
		notify: service.NewLeadNotifyService(),
	}
	router := gin.New()
	group := router.Group("/api")
	ctrl.RegisterRoutes(group, group)

	for _, path := range []string{"/api/lead-mining/notify-deliveries?clue_id=clue-none", "/api/lead-mining/notify-deliveries/summary?clue_id=clue-none"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(tenant.WithTenantID(req.Context(), 11))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var body struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != 0 {
			t.Fatalf("%s 响应不符合统一出口：body=%s err=%v", path, rec.Body.String(), err)
		}
	}
}
