package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 全局日志器必须保留路径和状态，不得捕获完整、破损或超限的授权请求。
func TestMerchantConnectionLogging_MetadataOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &captureAuditEntries{}
	SetAuditSink(sink)
	t.Cleanup(func() { SetAuditSink(nil) })
	paths := []string{"/api" + MerchantConnectionPath + "/complete", "/api" + MerchantConnectionPath + "/refresh", "/api" + MerchantConnectionPath + "/revoke"}
	logPath := filepath.Join(t.TempDir(), "api.log")
	logger.InitLogger(logger.LoggingConfig{Format: "json", Output: "file", File: logPath})
	t.Cleanup(func() { logger.InitLogger(logger.DefaultConfig()) })
	r := gin.New()
	r.Use(APIInteractionLogger(), AuditMiddleware())
	handler := func(c *gin.Context) {
		c.Set("user_id", uint(27))
		_, _ = io.Copy(io.Discard, c.Request.Body)
		_ = c.Error(io.ErrUnexpectedEOF)
		c.JSON(http.StatusBadRequest, gin.H{"message": "provider_error_marker"})
	}
	for _, path := range paths {
		r.POST(path, handler)
		r.POST(path+"/", handler)
	}
	payloads := []string{
		`{"code":"code_marker","state":"state_marker"}`,
		`{"code":"malformed_marker`,
		`{"code":"large_marker` + strings.Repeat("a", apiLogReqBodyCap+1),
		`{"refresh_token":"refresh_token_marker"}`,
	}
	for _, path := range paths {
		for i := range 50 {
			requestPath := path
			if (i/len(payloads))%2 != 0 {
				requestPath += "/"
			}
			req := httptest.NewRequest(http.MethodPost, requestPath+"?refresh_token=query_marker", strings.NewReader(payloads[i%len(payloads)]))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code)
		}
	}
	require.Eventually(t, func() bool {
		for _, path := range paths {
			found := false
			for _, entry := range sink.all() {
				if strings.Contains(entry.Detail, path) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}, 3*time.Second, 10*time.Millisecond)
	events, err := os.ReadFile(logPath)
	require.NoError(t, err)
	for _, path := range paths {
		require.Contains(t, string(events), path)
	}
	require.Contains(t, string(events), `"status":400`)
	for _, marker := range []string{"code_marker", "state_marker", "malformed_marker", "large_marker", "query_marker", "provider_error_marker", "refresh_token_marker", "unexpected EOF"} {
		require.NotContains(t, string(events), marker)
		for _, entry := range sink.all() {
			if strings.Contains(entry.Detail, MerchantConnectionPath) {
				require.Contains(t, entry.Detail, `"status_code":400`)
				require.NotContains(t, entry.Detail, marker)
			}
		}
	}
	for _, key := range []string{`"request_body"`, `"response_body"`, `"query"`, `"gin_errors"`} {
		require.NotContains(t, string(events), key)
	}
}

func TestMerchantConnectionLogging_RecoveryMetadataOnly(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	sink := &captureAuditEntries{}
	SetAuditSink(sink)
	t.Cleanup(func() { SetAuditSink(nil) })
	logPath := filepath.Join(t.TempDir(), "api.log")
	logger.InitLogger(logger.LoggingConfig{Format: "json", Output: "file", File: logPath})
	t.Cleanup(func() { logger.InitLogger(logger.DefaultConfig()) })
	previousWriter := gin.DefaultErrorWriter
	var recoveryLog strings.Builder
	gin.DefaultErrorWriter = &recoveryLog
	t.Cleanup(func() { gin.DefaultErrorWriter = previousWriter })
	r := gin.New()
	r.Use(Recovery(), APIInteractionLogger(), AuditMiddleware(), MerchantConnectionRecovery())
	const path = "/api" + MerchantConnectionPath + "/refresh/"
	r.POST(path, func(c *gin.Context) {
		c.Set("user_id", uint(27))
		panic("provider_panic_marker")
	})
	for range 50 { // Trigger the audit worker's batch flush.
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path+"?state=panic_query_marker", nil))
		require.Equal(t, http.StatusInternalServerError, w.Code)
	}
	require.NotContains(t, recoveryLog.String(), "panic_query_marker")
	require.NotContains(t, recoveryLog.String(), "provider_panic_marker")
	require.Eventually(t, func() bool {
		for _, entry := range sink.all() {
			if strings.Contains(entry.Detail, path) && strings.Contains(entry.Detail, `"status_code":500`) {
				return !strings.Contains(entry.Detail, "panic_query_marker")
			}
		}
		return false
	}, 3*time.Second, 10*time.Millisecond)
	events, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(events), `"status":500`)
	require.NotContains(t, string(events), "panic_query_marker")
	require.NotContains(t, string(events), "provider_panic_marker")
}
