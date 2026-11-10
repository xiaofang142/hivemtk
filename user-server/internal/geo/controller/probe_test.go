// probe_test.go 探针端点错误分档。
//
// 回归背景（2026-11-10 双模式 API 验证 #523 POST /api/geo/probe/test）：
// 上游模型回 429「Daily token quota exceeded. Retry after 54 seconds」时，
// handler 把 service 返回的任何 error 一律回 400 INVALID_PARAM_1001 —— 于是
//   · 前端把它当「用户输入写错了」，不让用户重试；
//   · 运维在 400 里找不到上流故障，retry_after 这类可操作信息也丢了。
// 现在的分档：engine 名不存在 → 400（真的是参数错）；其余 → 502/503（上游）。
package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/geo/service"

	"github.com/gin-gonic/gin"
)

// stubProbe 是可控失败的 SearchProbe。
type stubProbe struct{ err error }

func (p stubProbe) Name() string { return "stub" }

func (p stubProbe) Probe(ctx context.Context, query string) (*service.ProbeResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &service.ProbeResult{Engine: "stub", Query: query, Response: "ok"}, nil
}

func newProbeTestRouter(t *testing.T, probeErr error) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := service.NewProbeService([]service.SearchProbe{stubProbe{err: probeErr}},
		repository.NewGeoProbeRunRepository())
	ctrl := NewProbeController(svc)
	r := gin.New()
	r.POST("/api/geo/probe/test", ctrl.TestSingle)
	return r
}

func TestProbeTestSingle_UpstreamFailureIsNot400(t *testing.T) {
	// 上游 429 是服务端可重试问题：必须是 502（UPSTREAM_FAILURE_5005），
	// 而不是让调用方以为自己参数写错了。
	r := newProbeTestRouter(t, errors.New(`stub: status 429: {"code":"quota_exceeded","retry_after":54}`))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/geo/probe/test",
		strings.NewReader(`{"engine":"stub","query":"HiveMTK 是什么"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("上游 429 应回 502 UPSTREAM_FAILURE，实际 %d body=%s", w.Code, w.Body.String())
	}
}

func TestProbeTestSingle_UnknownEngineIs400(t *testing.T) {
	// engine 名写错是真正的参数错误，必须仍是 400（不能一并抬成 502）。
	r := newProbeTestRouter(t, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/geo/probe/test",
		strings.NewReader(`{"engine":"no-such-engine","query":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("未知 engine 应回 400，实际 %d body=%s", w.Code, w.Body.String())
	}
}

func TestProbeTestSingle_MissingQueryIs400(t *testing.T) {
	r := newProbeTestRouter(t, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/geo/probe/test",
		strings.NewReader(`{"engine":"stub"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("缺 query 应回 400，实际 %d", w.Code)
	}
}
