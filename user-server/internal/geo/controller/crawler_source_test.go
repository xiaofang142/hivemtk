package controller

import (
	"context"
	"net/http"
	"testing"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/tenant"

	"github.com/gin-gonic/gin"
)

// HTTP 202 只表示主动探测已受理；测试注入执行入口，不启动任何真实爬取。
func TestCrawlerSource_RunAcceptedContract(t *testing.T) {
	controller := NewReportController(nil, nil)
	calls := 0
	controller.startCrawler = func(context.Context) error { calls++; return nil }
	router := gin.New()
	router.POST("/crawler/run", controller.RunCrawler)
	status, envelope := do(t, router, http.MethodPost, "/crawler/run", "")
	if status != http.StatusAccepted || envelope.Code != float64(0) || calls != 1 {
		t.Fatalf("受理应为 202/code=0 且仅派发一次，实际 %d %+v，派发 %d 次", status, envelope, calls)
	}
	meta, ok := envelope.Data["data_source"].(map[string]any)
	if !ok || meta["kind"] != "active_ua_probe" || meta["external_crawl_verified"] != false || meta["indexing_verified"] != false || envelope.Data["status"] != "accepted" {
		t.Fatalf("受理响应必须说明主动探测及证据边界，实际 %+v", envelope)
	}
}

// 使用真实服务入口，缺租户时同步拒绝，不能启动后台探测。
func TestCrawlerSource_RunMissingTenant(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	controller := NewReportController(nil, nil)
	controller.startCrawler = service.StartCrawlerProbe
	router := gin.New()
	router.POST("/crawler/run", controller.RunCrawler)
	status, envelope := do(t, router, http.MethodPost, "/crawler/run", "")
	if status != http.StatusUnauthorized || envelope.Code == float64(0) {
		t.Fatalf("缺租户应拒绝而不是受理探测，实际 %d %+v", status, envelope)
	}
}
