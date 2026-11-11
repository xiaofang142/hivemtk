package middleware

// audit_redact_test.go —— N-1：审计中间件 query 参数脱敏落库（与 api_logger 同口径）。
// 红转绿锚点：修前 audit.go detail 构造里 "query": c.Request.URL.Query() 直接落明文。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// captureAuditEntries 记录送达 sink 的全部审计条目。
// processAuditLogs 协程写、测试协程读，互斥锁保护。
type captureAuditEntries struct {
	mu      sync.Mutex
	entries []*AuditEntry
}

func (s *captureAuditEntries) Save(ctx context.Context, entry *AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	return nil
}

func (s *captureAuditEntries) all() []*AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*AuditEntry(nil), s.entries...)
}

// TestRedactQueryValues_SharedWithApiLogger 钉住共享原语的行为与两侧口径一致性：
// 值形态（audit detail.query 落库用）与字符串形态（api_logger query 字段落日志用）
// 同源——redactQuery 恒等于 redactQueryValues(q).Encode()，键表同为 apiLogSensitiveKeys，
// 掩码同为 apiLogRedactedMask，两处不可能再次分化。
func TestRedactQueryValues_SharedWithApiLogger(t *testing.T) {
	q := url.Values{
		"token":        {"abc123"},
		"Token":        {"case-insensitive"},
		"bridge_token": {"bt"},
		"secret":       {"s3cr3t"},
		"phone":        {"13812345678"},
		"email":        {"a@b.com"},
		"page":         {"2"},
		"name":         {"alice"},
	}

	got := redactQueryValues(q)
	for _, k := range []string{"token", "Token", "bridge_token", "secret", "phone", "email"} {
		require.Equal(t, []string{apiLogRedactedMask}, got[k], "键 %q 应脱敏", k)
	}
	require.Equal(t, []string{"2"}, got["page"], "非敏感键原样保留")
	require.Equal(t, []string{"alice"}, got["name"], "非敏感键原样保留")

	// 口径对照：api_logger 的字符串形态 = 共享值形态的 Encode（同一实现）。
	encoded := redactQuery(q)
	require.Equal(t, got.Encode(), encoded)
	require.Contains(t, encoded, "token=%2A%2A%2AREDACTED%2A%2A%2A")
	require.NotContains(t, encoded, "abc123")
	require.NotContains(t, encoded, "s3cr3t")
	require.NotContains(t, encoded, "13812345678")

	// 空 query 不 panic、空输出。
	require.Equal(t, "", redactQuery(nil))
	require.Empty(t, redactQueryValues(nil))
}

// TestAuditMiddleware_LoggedInWriteQueryRedacted —— N-1 验收锚点：
// 带 ?token=x 的登录态写请求，落 audit_logs 的 detail.query 中 token 值为
// ***REDACTED***，非敏感键明文保留，原始 token 不得出现在 detail 任何位置。
func TestAuditMiddleware_LoggedInWriteQueryRedacted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sink := &captureAuditEntries{}
	SetAuditSink(sink)
	t.Cleanup(func() { SetAuditSink(nil) })

	r := gin.New()
	r.Use(AuditMiddleware())
	r.POST("/api/redact-probe", func(c *gin.Context) {
		c.Set("user_id", uint(1)) // 模拟登录态（JWT 中间件写入的键）
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// processAuditLogs 的 batchSize=50（audit.go 常量）：打满一批即触发同步刷盘，
	// 不依赖 5s ticker，测试全同步可复现。
	const n = 50
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodPost,
			"/api/redact-probe?token=topsecret&page=2", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}

	require.Eventually(t, func() bool {
		return len(sink.all()) >= n
	}, 3*time.Second, 10*time.Millisecond,
		"审计条目应经批处理刷盘送达 sink（batchSize=%d）", n)

	entries := sink.all()
	require.NotEmpty(t, entries)
	for _, e := range entries {
		var detail struct {
			Query map[string][]string `json:"query"`
		}
		require.NoError(t, json.Unmarshal([]byte(e.Detail), &detail))
		require.Equal(t, []string{"***REDACTED***"}, detail.Query["token"],
			"audit_logs query.token 必须为 ***REDACTED***（N-1 验收）")
		require.Equal(t, []string{"2"}, detail.Query["page"],
			"非敏感键原样保留，排障可用")
		require.NotContains(t, e.Detail, "topsecret",
			"明文 token 不得进 audit_logs")
	}
}

// TestAuditMiddleware_QueryWithoutSensitiveKeys 反向锚点：无敏感键的 query 原样落库，
// 脱敏不得误伤排障信息。
func TestAuditMiddleware_QueryWithoutSensitiveKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sink := &captureAuditEntries{}
	SetAuditSink(sink)
	t.Cleanup(func() { SetAuditSink(nil) })

	r := gin.New()
	r.Use(AuditMiddleware())
	r.POST("/api/redact-probe", func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodPost,
			"/api/redact-probe?page=3&keyword=hello", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}

	require.Eventually(t, func() bool {
		return len(sink.all()) >= 50
	}, 3*time.Second, 10*time.Millisecond, "审计条目应送达 sink")

	for _, e := range sink.all() {
		var detail struct {
			Query map[string][]string `json:"query"`
		}
		require.NoError(t, json.Unmarshal([]byte(e.Detail), &detail))
		require.Equal(t, []string{"3"}, detail.Query["page"])
		require.Equal(t, []string{"hello"}, detail.Query["keyword"])
	}
}
