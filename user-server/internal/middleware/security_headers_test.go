package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders_GlobalBaseline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/ping", nil)

	SecurityHeaders()(c)

	h := w.Header()
	if got := h.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Errorf("全局 CSP 缺失或未以 default-src 'self' 兜底: %q", got)
	}
	if v := h.Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("nosniff=%q", v)
	}
	if v := h.Get("Referrer-Policy"); v != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy=%q", v)
	}
	if v := h.Get("X-Frame-Options"); v != "" {
		t.Errorf("全局不应设 X-Frame-Options（embed 跨域嵌入会被误伤）: %q", v)
	}
}

func TestSecurityHeaders_HandlerCSPOverridesGlobal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())
	r.GET("/files", func(c *gin.Context) {
		c.Header("Content-Security-Policy", "sandbox")
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Errorf("handler 覆盖语义被破坏（/files 守卫 sandbox 级 CSP 必须胜出）: %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff 被覆盖丢失: %q", got)
	}
}
