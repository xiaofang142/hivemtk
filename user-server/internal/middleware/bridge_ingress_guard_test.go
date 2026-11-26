package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBridgeIngressGuardPreservesVerifiedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("BRIDGE_INGEST_AUTH", "on")
	const fixture = "synthetic-identify-fixture"
	for _, tc := range []struct {
		name   string
		query  string
		header string
		want   int
	}{
		{name: "query_only", query: fixture, want: http.StatusNoContent},
		{name: "header_only", header: fixture, want: http.StatusNoContent},
		{name: "header_priority", query: "untrusted-query", header: fixture, want: http.StatusNoContent},
		{name: "invalid_header_over_valid_query", query: fixture, header: "untrusted-header", want: http.StatusUnauthorized},
		{name: "invalid_query", query: "untrusted-query", want: http.StatusUnauthorized},
		{name: "missing", want: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			r := gin.New()
			r.Use(bridgeIngressGuard(func() []string { return []string{fixture} }))
			r.POST("/api/bridge/gateway", func(c *gin.Context) {
				calls++
				// bridgeIdentifyHintFromRequest 下游优先读取这个 header。
				if c.GetHeader(identifyCodeHeader) != fixture {
					t.Error("verified identity missing from downstream request header")
				}
				if c.Request.URL.Query().Get(identifyCodeQuery) != "" {
					t.Error("credential remains in downstream URL")
				}
				if c.Query("keep") != "fixture-value" {
					t.Error("unrelated query field lost")
				}
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodPost, "/api/bridge/gateway?keep=fixture-value", nil)
			query := req.URL.Query()
			if tc.query != "" {
				query.Set(identifyCodeQuery, tc.query)
			}
			req.URL.RawQuery = query.Encode()
			if tc.header != "" {
				req.Header.Set(identifyCodeHeader, tc.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("status=%d, want %d", w.Code, tc.want)
			}
			wantCalls := 0
			if tc.want == http.StatusNoContent {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Errorf("downstream calls=%d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestBridgeIngressGuardNoCandidatesKeepsExplicitPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"on", "off"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("BRIDGE_INGEST_AUTH", mode)
			calls := 0
			r := gin.New()
			r.Use(bridgeIngressGuard(func() []string { return nil }))
			r.POST("/api/bridge/gateway", func(c *gin.Context) {
				calls++
				if c.GetHeader(identifyCodeHeader) != "" {
					t.Error("unverified credential was promoted to header")
				}
				c.Status(http.StatusNoContent)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/bridge/gateway?identify_code=unverified-fixture", nil))
			wantStatus, wantCalls := http.StatusServiceUnavailable, 0
			if mode == "off" {
				wantStatus, wantCalls = http.StatusNoContent, 1
			}
			if w.Code != wantStatus || calls != wantCalls {
				t.Errorf("status=%d downstream calls=%d, want %d/%d", w.Code, calls, wantStatus, wantCalls)
			}
		})
	}
}
