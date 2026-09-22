package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// 平台集成关闭时，"要打到平台侧的写操作"必须挡在 handler 门口：
// 403 + 一句照着做就能开通的话，而不是往下走 service 换来一条
// "平台未配置"的失败与 Error 日志（那是把"没启用"伪装成"坏了"）。
//
// service 一律传 nil 就是这条判据的一部分：守卫若在 service 调用之后，
// 这里会先 panic 而不是拿到 403。

func newPlatformGuardRouter(t *testing.T) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	market := NewAssetMarketController(nil, nil, nil)
	bundle := NewAssetBundleController(nil)
	r := gin.New()
	r.POST("/api/v1/asset-market/purchase", market.Purchase)
	r.POST("/api/v1/asset-market/sync", market.Sync)
	r.POST("/api/asset-market/report-usage", market.ReportUsage)
	r.POST("/api/asset-bundle/:id/submit-platform", bundle.SubmitToPlatform)
	return r
}

func TestPlatformDisabledWritesReturn403(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "")
	router := newPlatformGuardRouter(t)

	for _, route := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/asset-market/purchase", `{"asset_id":"a1"}`},
		{http.MethodPost, "/api/v1/asset-market/sync", `{"asset_id":"a1"}`},
		{http.MethodPost, "/api/asset-market/report-usage", `{"asset_id":"a1"}`},
		{http.MethodPost, "/api/asset-bundle/b1/submit-platform", ``},
	} {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("%s 关态应返回 403，实际 %d，body=%s", route.path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "平台集成未启用") {
			t.Errorf("%s 关态应说明\"平台集成未启用\"，实际 body=%s", route.path, w.Body.String())
		}
	}
}

// TestPlatformDisabledReadsStayEmpty 读面必须保持"空数据 200"，不能跟着写面一起 403——
// 前端市场页/通知中心就是靠空列表渲染"暂无资产"的。
func TestPlatformDisabledReadsStayEmpty(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "")
	if config.PlatformEnabled() {
		t.Fatal("前置条件：本用例必须是关态")
	}
	// service 层客户端已由 platform.disabledClient 保证空值（见 internal/platform 的同名契约测试），
	// 这里只锁 controller 不再吞掉形状：marketSvc 用关态工厂装配，ListMarket 必须 200 + 空 list。
	market := NewAssetMarketController(nil, service.NewAssetMarketService(platform.NewPlatformAPIClient()), nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/asset-market/list", market.ListMarket)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/asset-market/list", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("关态市场列表必须 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			List  []map[string]any `json:"list"`
			Total int64            `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON：%v body=%s", err, w.Body.String())
	}
	if body.Code != 0 || body.Data.Total != 0 || body.Data.List == nil || len(body.Data.List) != 0 {
		t.Fatalf("关态市场列表必须是 code=0 + 空 list + total=0，实际 %s", w.Body.String())
	}
}
