package middleware

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMerchantCallbackLogging_AllFailurePaths(t *testing.T) {
	previous := gin.Mode()
	gin.SetMode(gin.DebugMode)
	t.Cleanup(func() { gin.SetMode(previous) })
	var ginLog strings.Builder
	previousOut, previousErr := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = &ginLog, &ginLog
	t.Cleanup(func() { gin.DefaultWriter, gin.DefaultErrorWriter = previousOut, previousErr })
	sink := &captureAuditEntries{}
	SetAuditSink(sink)
	t.Cleanup(func() { SetAuditSink(nil) })
	logPath := filepath.Join(t.TempDir(), "callback.log")
	logger.InitLogger(logger.LoggingConfig{Format: "json", Output: "file", File: logPath})
	t.Cleanup(func() { logger.InitLogger(logger.DefaultConfig()) })
	r := gin.New()
	r.Use(Recovery()) // Same outer recovery and proxy guard ordering as the executable.
	guard := newClientIPGuard(nil, func(_, _ string) { t.Error("callback forwarded headers reached proxy warning") })
	r.Use(guard.handler(), MerchantCallbackAccessLog(), Recovery(), SecurityHeaders(), BodyLimit(128), TraceMiddleware(), APIInteractionLogger(), AuditMiddleware(), MerchantConnectionRecovery())
	const root = "/api" + MerchantCallbackPath
	r.POST(root+"/*delivery", func(c *gin.Context) {
		c.Set("user_id", uint(17))
		_, _ = io.Copy(io.Discard, c.Request.Body)
		_ = c.Error(io.ErrUnexpectedEOF)
		if strings.Contains(c.Param("delivery"), "panic") {
			panic("private-panic-marker")
		}
		c.String(403, "private-response-marker")
	})
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"unverified", "POST", "/private-path-marker", `{"proof":"private-body-marker"}`, 403},
		{"malformed", "POST", "/private-path-marker/", `{"private-malformed-marker`, 403},
		{"nested", "POST", "/private-path-marker/private-nested-marker", "private-body-marker", 403},
		{"panic", "POST", "/panic-private-path-marker", "private-body-marker", 500},
		{"oversize", "POST", "/private-path-marker", strings.Repeat("private-large-marker", 128), 413},
		{"wrong method", "GET", "/private-path-marker/", "private-body-marker", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, root+tc.path+"?proof=private-query-marker", strings.NewReader(tc.body))
			req.Header.Set("X-Fixture-Proof", "private-header-marker")
			req.Header.Set("User-Agent", "private-agent-marker")
			req.Header.Set("X-Request-ID", "private-trace-marker")
			req.Header.Set("X-Forwarded-For", "private-forwarded-marker")
			req.Header.Set("X-Forwarded-Prefix", "/private-prefix-marker")
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code)
			require.Empty(t, w.Header().Get("Location"))
			require.Empty(t, w.Header().Get("Set-Cookie"))
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		})
	}
	events, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Equal(t, 6, strings.Count(string(events), `"event":"api_interaction"`))
	require.Contains(t, string(events), merchantCallbackLogPath)
	for _, status := range []string{`"status":403`, `"status":413`, `"status":500`, `"status":405`} {
		require.Contains(t, string(events), status)
	}
	for _, key := range []string{"private-", "unexpected EOF", `"client_ip"`, `"query"`, `"request_body"`, `"response_body"`, `"gin_errors"`} {
		require.NotContains(t, string(events), key)
		require.NotContains(t, ginLog.String(), key)
	}
	for _, entry := range sink.all() {
		require.NotContains(t, entry.Detail, MerchantCallbackPath)
	}
}
