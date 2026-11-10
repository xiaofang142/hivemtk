package middleware

// audit_ma03_test.go —— MA-03 审计中间件有界化：请求体 Tee 预览不截断 handler、
// 响应缓冲 cap。红转绿锚点：修前 audit 对 multipart/大响应整读整缓冲（无界）。

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAuditMiddleware_LargeRequestBodyFullToHandlerPreviewBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuditMiddleware())

	const full = 3 * auditBodyPreviewCap // 3MB：超预览 cap 但属正常 JSON 请求
	var handlerGot int
	r.POST("/api/audit-me", func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		handlerGot = len(b)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/audit-me",
		strings.NewReader(strings.Repeat("x", full)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if handlerGot != full {
		t.Fatalf("handler 收到 body 长度 = %d want %d（Tee 预览不得截断 handler 可读体）", handlerGot, full)
	}
}

func TestAuditResponseWriter_CapsBuffer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := &testResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	aw := &auditResponseWriter{ResponseWriter: w, body: &cappedBufferWriter{cap: auditRespBodyCap}}

	big := strings.Repeat("y", 3*auditRespBodyCap)
	if _, err := aw.Write([]byte(big)); err != nil {
		t.Fatal(err)
	}
	if n := len(aw.body.Bytes()); n > auditRespBodyCap {
		t.Fatalf("响应缓冲 = %d 字节，超 cap %d（MA-03 无界缓冲未修）", n, auditRespBodyCap)
	}
	if w.Body.Len() != len(big) {
		t.Fatalf("下游 Write 被截断: %d want %d", w.Body.Len(), len(big))
	}
	if _, err := aw.WriteString(big); err != nil {
		t.Fatal(err)
	}
	if n := len(aw.body.Bytes()); n > auditRespBodyCap {
		t.Fatalf("WriteString 后缓冲 = %d 字节，超 cap %d", n, auditRespBodyCap)
	}
}

// sizedReader 流式产出 n 个 'x' 字节（测试侧不整驻 50MB 请求体，
// 内存增量有界性由被侧的 cap 结构保证）。
type sizedReader struct {
	remaining int64
}

func (r *sizedReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > r.remaining {
		n = r.remaining
	}
	for i := int64(0); i < n; i++ {
		p[i] = 'x'
	}
	r.remaining -= n
	return int(n), nil
}

// TestAuditMiddleware_HugeMultipartPreviewBounded —— MA-03 负例（判据原话：
// 「50MB multipart 内存增量有界」）：multipart 大文件导入场景下审计面只留
// 首 1MB 预览（TeeReader+cappedBufferWriter），handler 仍可读完整流；
// 修前 io.ReadAll 会把整份 50MB 读进审计内存（audit+handler 双份）。
func TestAuditMiddleware_HugeMultipartPreviewBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuditMiddleware())

	const full = 50 << 20 // 50MB 知识库导入场景（BodyLimit 对 multipart 豁免）
	var handlerGot int64
	r.POST("/api/audit-multipart", func(c *gin.Context) {
		n, _ := io.Copy(io.Discard, c.Request.Body)
		handlerGot = n
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/audit-multipart", &sizedReader{remaining: full})
	req.Header.Set("Content-Type", "multipart/form-data; boundary=ma03probe")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if handlerGot != full {
		t.Fatalf("handler 消费 body = %d want %d（审计预览不得截断 handler 可读体）", handlerGot, full)
	}
}

// TestCappedBufferWriter_HugeInputBounded —— 50MB 流量灌入 cappedBufferWriter
// 的直接断言：缓冲封顶、Write 不报错（超 cap 丢弃预览超出部分是有意口径，
// 不影响下游 handler 面——handler 读的是原始流而非该缓冲）。
func TestCappedBufferWriter_HugeInputBounded(t *testing.T) {
	cw := &cappedBufferWriter{cap: auditBodyPreviewCap}
	chunk := []byte(strings.Repeat("x", 64<<10))
	written := 0
	for written < 50<<20 {
		n, err := cw.Write(chunk)
		if err != nil {
			t.Fatalf("Write 报错: %v", err)
		}
		written += n
	}
	if got := len(cw.Bytes()); got > auditBodyPreviewCap {
		t.Fatalf("50MB 灌入后缓冲 = %d 字节，超 cap %d（MA-03 无界缓冲未修）", got, auditBodyPreviewCap)
	}
}
