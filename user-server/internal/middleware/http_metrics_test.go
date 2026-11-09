package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/metrics"
)

// G-4：MetricsMiddleware 早有实现但 router 链里零接线（有实现无暴露）——本文件钉住
// 接线后的采集语义：计数（method/path/status 三标签）、时延直方图、/metrics 自排除、
// 未匹配路由固定 "unmatched" 占位（扫描器随机路径不得成为 label，防基数爆炸）。
func TestMetricsMiddleware_CollectsRequestMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	counterCell := func(method, path, status string) uint64 {
		return metrics.MustGetCounter("http_requests_total").WithLabel(method, path, status).Value()
	}
	histCount := func(method, path string) uint64 {
		for _, s := range metrics.CollectSamples() {
			if s.Name != "http_request_duration_ms" || s.Agg != "count" {
				continue
			}
			if len(s.Labels) == 2 && s.Labels[0] == method && s.Labels[1] == path {
				return uint64(s.Value)
			}
		}
		return 0
	}

	before := counterCell(http.MethodGet, "/ping", "200")
	beforeHist := histCount(http.MethodGet, "/ping")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/ping 应 200，实际 %d", w.Code)
	}
	if got := counterCell(http.MethodGet, "/ping", "200"); got != before+1 {
		t.Errorf("http_requests_total{GET,/ping,200} 应 +1，前=%d 后=%d", before, got)
	}
	if got := histCount(http.MethodGet, "/ping"); got != beforeHist+1 {
		t.Errorf("http_request_duration_ms{GET,/ping} count 应 +1，前=%d 后=%d", beforeHist, got)
	}
}

func TestMetricsMiddleware_UnmatchedPathFixedLabel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/known", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/scan/me/abc123", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("未匹配路由应 404，实际 %d", w.Code)
	}

	if got := metrics.MustGetCounter("http_requests_total").WithLabel(http.MethodGet, "unmatched", "404").Value(); got == 0 {
		t.Error("未匹配路由应累计到 http_requests_total{...,unmatched,404}")
	}
	// 读侧 WithLabel 会建零值 cell，断言「原始路径 cell 不存在」等价于其值为 0
	if got := metrics.MustGetCounter("http_requests_total").WithLabel(http.MethodGet, "/scan/me/abc123", "404").Value(); got != 0 {
		t.Errorf("原始路径不得成为 label，实际存在非零 cell 值=%d", got)
	}
}

func TestMetricsMiddleware_ExcludesMetricsItself(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/metrics", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.ServeHTTP(w, req)

	if got := metrics.MustGetCounter("http_requests_total").WithLabel(http.MethodGet, "/metrics", "200").Value(); got != 0 {
		t.Errorf("/metrics 自身应在排除清单，计数应保持 0，实际 %d", got)
	}
}
