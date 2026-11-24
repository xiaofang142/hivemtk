package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestMerchantCallbackRoutes_UnconfiguredNeverAcknowledgesOrRedirects(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	previousWriter := gin.DefaultWriter
	var debug strings.Builder
	gin.DefaultWriter = &debug
	t.Cleanup(func() { gin.DefaultWriter = previousWriter })
	r := gin.New()
	r.Use(middleware.MerchantCallbackAccessLog())
	setupMerchantCallbackRoutes(r.Group("/api"), nil)
	for _, tc := range []struct {
		suffix string
		status int
	}{
		{"/path-fixture", 503}, {"/path-fixture/", 503}, {"/path-fixture/?proof=query-fixture", 400},
		{"/path-fixture/private-fixture?proof=query-fixture", 400}, {"/", 400}, {"", 400},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/merchant-callbacks"+tc.suffix, strings.NewReader("body-fixture"))
		req.Header.Set("X-Forwarded-Prefix", "/prefix-fixture")
		r.ServeHTTP(w, req)
		if w.Code != tc.status || w.Header().Get("Location") != "" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("预期失败状态 %d 且禁止缓存和跳转，实际 %d: %s", tc.status, w.Code, w.Body.String())
		}
	}
	for _, secret := range []string{"path-fixture", "private-fixture", "query-fixture", "prefix-fixture", "body-fixture"} {
		if strings.Contains(debug.String(), secret) {
			t.Fatalf("Gin 调试日志泄露标记 %s", secret)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodOptions, http.MethodHead} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/integrations/merchant-callbacks/path-fixture/?proof=query-fixture", nil))
		if w.Code != 405 || w.Header().Get("Location") != "" || w.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("方法 %s 应直接拒绝且不跳转: %d", method, w.Code)
		}
	}
}

type fixedMerchantCallbackService struct {
	result service.MerchantCallbackResult
}

func (s fixedMerchantCallbackService) Ingest(_ context.Context, _ string, _ []byte, _ http.Header) service.MerchantCallbackResult {
	return s.result
}

func (s fixedMerchantCallbackService) AcknowledgeCompensationPage(_ context.Context, _ repository.MerchantCallbackCompensationPage, _ []uint) error {
	return nil
}

func (s fixedMerchantCallbackService) ReadCompensationPage(_ context.Context, _ uint, _ int) (repository.MerchantCallbackCompensationPage, error) {
	return repository.MerchantCallbackCompensationPage{}, nil
}

func TestMerchantCallbackRoutes_TrustedEmptyAckIsExact(t *testing.T) {
	for _, tc := range []struct {
		name               string
		ack                service.MerchantCallbackAck
		wantType, wantBody string
	}{
		{name: "empty", ack: service.MerchantCallbackAck{Status: 204}, wantType: "", wantBody: ""},
		{name: "body", ack: service.MerchantCallbackAck{Status: 202, ContentType: "text/plain", Body: "fixture-ack"}, wantType: "text/plain", wantBody: "fixture-ack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(middleware.MerchantCallbackAccessLog())
			mountMerchantCallbackRoutes(r.Group("/api"), fixedMerchantCallbackService{result: service.MerchantCallbackResult{Ack: tc.ack, TrustedAck: true}})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/integrations/merchant-callbacks/fixture-provider", strings.NewReader("fixture")))
			if w.Code != tc.ack.Status || w.Body.String() != tc.wantBody || w.Header().Get("Content-Type") != tc.wantType {
				t.Fatalf("trusted ack changed: status=%d content-type=%q body=%q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
			}
		})
	}
}
