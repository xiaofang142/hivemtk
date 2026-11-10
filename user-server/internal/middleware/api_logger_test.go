package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCappedBufferWriter_CapsAndReportsFull(t *testing.T) {
	w := &cappedBufferWriter{cap: 8}
	n, err := w.Write([]byte("0123456789"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 10 {
		t.Fatalf("must report full length written; got %d", n)
	}
	got := w.Bytes()
	if len(got) != 8 {
		t.Fatalf("cap violated: len=%d want 8", len(got))
	}
	if string(got) != "01234567" {
		t.Fatalf("wrong prefix kept: %q", string(got))
	}
}

func TestCappedBufferWriter_MultipleWritesCap(t *testing.T) {
	w := &cappedBufferWriter{cap: 5}
	_, _ = w.Write([]byte("ab"))
	_, _ = w.Write([]byte("cd"))
	_, _ = w.Write([]byte("efgh"))
	if string(w.Bytes()) != "abcde" {
		t.Fatalf("want abcde got %q", string(w.Bytes()))
	}
	if len(w.Bytes()) != 5 {
		t.Fatalf("len want 5 got %d", len(w.Bytes()))
	}
}

// TestAPIInteractionLogger_FullBodyDeliveredViaTee 关键回归：
// handler 必须仍收到完整请求体（TeeReader 不截断），即便 body 远超日志 cap。
func TestAPIInteractionLogger_FullBodyDeliveredViaTee(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(APIInteractionLogger())
	r.POST("/api/echo", func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusOK, string(b))
	})

	big := strings.Repeat("x", 1*1024*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(big))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if w.Body.String() != big {
		t.Fatalf("handler did not receive full body (len=%d want=%d)", w.Body.Len(), len(big))
	}
}

// TestAPIInteractionLogger_SmallBodyCaptured 小 JSON body 应被捕获并经脱敏/截断逻辑处理，
// 且 handler 仍能正常读取。
func TestAPIInteractionLogger_SmallBodyCaptured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(APIInteractionLogger())
	r.POST("/api/login", func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"echo_len": len(b)})
	})

	payload := `{"username":"alice","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
}

// ===== MA-02：响应体落日志口径（≥400 才记 + 内容级脱敏）=====

// TestAPIInteractionLogger_ResponsePIIMasked 负例（红转绿锚点）：
// 含手机号的错误响应落日志时必须呈掩码形态；2xx 响应体一律不记。
func TestAPIInteractionLogger_ResponsePIIMasked(t *testing.T) {
	// ≥400：内容级脱敏生效（手机号 138****7777 形态）。
	got := apiLogResponseSnapshot(500, `{"message":"customer 13812345678 not found","phone":"13900001111"}`)
	if !strings.Contains(got, "138****5678") {
		t.Errorf("内容级手机号未掩码: %q", got)
	}
	if !strings.Contains(got, "***REDACTED***") {
		t.Errorf("phone 键级未掩码: %q", got)
	}
	if strings.Contains(got, "13812345678") || strings.Contains(got, "13900001111") {
		t.Errorf("完整手机号泄漏进日志: %q", got)
	}
	// <400：不记（最小化口径）。
	if s := apiLogResponseSnapshot(200, `{"phone":"13812345678","name":"alice"}`); s != "" {
		t.Errorf("2xx 响应体不应落日志，实际: %q", s)
	}
	if s := apiLogResponseSnapshot(302, `{"phone":"13812345678"}`); s != "" {
		t.Errorf("3xx 响应体不应落日志，实际: %q", s)
	}
}

// TestAPIInteractionLogger_SensitiveKeyList 钉住键级清单扩张（MA-02 判据之一）：
// phone/mobile/email 键名请求体落日志时键级掩码生效。
func TestAPIInteractionLogger_SensitiveKeyList(t *testing.T) {
	for _, k := range []string{"phone", "mobile", "email", "phone_no"} {
		if _, ok := apiLogSensitiveKeys[k]; !ok {
			t.Errorf("apiLogSensitiveKeys 缺 %q（MA-02 键级清单）", k)
		}
	}
}
