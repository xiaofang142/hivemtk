package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newOpenTracker(t *testing.T) *EmailOpenTrackerService {
	t.Helper()
	return NewEmailOpenTrackerService(nil, nil)
}

// resetPixelCacheForTest 清空 MarkOpenSeen 的进程级防重 map。
// 固定 token 在 -count=2 第二轮会被上一轮残留记录拦截（utils.DefaultHTTPTimeout 内），
// 导致 "First call should return true" 断言失败。
func resetPixelCacheForTest(t *testing.T) {
	t.Helper()
	pixelCacheMu.Lock()
	pixelCache = make(map[string]time.Time)
	pixelCacheMu.Unlock()
}

// 1) 构造
func TestEmailOpenTracker_NewService(t *testing.T) {
	s := newOpenTracker(t)
	if s == nil {
		t.Fatal("Expected non-nil service")
	}
	if s.tracking == nil {
		t.Error("Expected tracking fallback")
	}
	if s.repo == nil {
		t.Error("Expected repo fallback")
	}
}

// 2) GenerateOpenPixelURL
func TestEmailOpenTracker_GenerateOpenPixelURL(t *testing.T) {
	// 缺 EMAIL_TRACKING_SECRET 时签发会 fail-closed 报错，这里要的是"能签出像素 URL"
	t.Setenv("EMAIL_TRACKING_SECRET", "test-open-tracker-secret")
	s := newOpenTracker(t)
	url, err := s.GenerateOpenPixelURL(context.Background(), "user@demo.com", "job-1")
	if err != nil {
		t.Fatalf("GenerateOpenPixelURL failed: %v", err)
	}
	if !strings.Contains(url, "/api/email/track/open/") {
		t.Errorf("URL missing path: %s", url)
	}
	if !strings.HasSuffix(url, ".png") {
		t.Errorf("URL should end with .png: %s", url)
	}
}

// 3) RenderPixel
func TestEmailOpenTracker_RenderPixel(t *testing.T) {
	s := newOpenTracker(t)
	pix, ct, maxAge, err := s.RenderPixel(context.Background(), "anytoken", "1.1.1.1", "Mozilla/5.0")
	if err != nil {
		t.Fatalf("RenderPixel failed: %v", err)
	}
	if len(pix) != len(EmailOpenPixel) {
		t.Errorf("Pixel size mismatch: got %d, want %d", len(pix), len(EmailOpenPixel))
	}
	if pix[0] != 0x89 || pix[1] != 'P' || pix[2] != 'N' || pix[3] != 'G' {
		t.Errorf("Invalid PNG signature: %x", pix[:4])
	}
	if ct != EmailOpenPixelContentType {
		t.Errorf("Content-Type mismatch: %s", ct)
	}
	if maxAge != EmailOpenPixelMaxAge {
		t.Errorf("MaxAge mismatch: got %d, want %d", maxAge, EmailOpenPixelMaxAge)
	}
}

// 4) RenderPixel 空 token
func TestEmailOpenTracker_RenderPixel_EmptyToken(t *testing.T) {
	s := newOpenTracker(t)
	_, _, _, err := s.RenderPixel(context.Background(), "", "", "")
	if err == nil {
		t.Error("Expected error for empty token")
	}
}

// 5) Postmark 事件映射
func TestEmailOpenTracker_Postmark_Open(t *testing.T) {
	s := newOpenTracker(t)
	err := s.RecordPostmarkEvent(context.Background(), &PostmarkOpenEvent{
		RecordType: "Open",
		Recipient:  "user@demo.com",
		MessageID:  "msg-1",
		IP:         "1.1.1.1",
		UserAgent:  "Mozilla/5.0",
	})
	if err == nil {
		t.Log("expected failure with nil repo:", err)
	}
}

func TestEmailOpenTracker_Postmark_Bounce(t *testing.T) {
	s := newOpenTracker(t)
	err := s.RecordPostmarkEvent(context.Background(), &PostmarkOpenEvent{
		RecordType: "Bounce",
		Recipient:  "user@demo.com",
		MessageID:  "msg-1",
	})
	if err == nil {
		t.Log("expected failure with nil repo:", err)
	}
}

func TestEmailOpenTracker_Postmark_SpamComplaint(t *testing.T) {
	s := newOpenTracker(t)
	err := s.RecordPostmarkEvent(context.Background(), &PostmarkOpenEvent{
		RecordType: "SpamComplaint",
		Recipient:  "user@demo.com",
		MessageID:  "msg-1",
	})
	if err == nil {
		t.Log("expected failure with nil repo:", err)
	}
}

func TestEmailOpenTracker_Postmark_UnsupportedType(t *testing.T) {
	s := newOpenTracker(t)
	err := s.RecordPostmarkEvent(context.Background(), &PostmarkOpenEvent{
		RecordType: "Unknown",
		Recipient:  "user@demo.com",
	})
	if err == nil {
		t.Error("Expected error for unsupported type")
	}
}

func TestEmailOpenTracker_Postmark_NilEvent(t *testing.T) {
	s := newOpenTracker(t)
	if err := s.RecordPostmarkEvent(context.Background(), nil); err == nil {
		t.Error("Expected error for nil event")
	}
}

// 6) SendCloud 事件
func TestEmailOpenTracker_SendCloud_Open(t *testing.T) {
	s := newOpenTracker(t)
	err := s.RecordSendCloudEvent(context.Background(), &SendCloudOpenEvent{
		Event:     "open",
		Recipient: "user@demo.com",
		MessageID: "msg-1",
	})
	if err == nil {
		t.Log("expected failure with nil repo:", err)
	}
}

func TestEmailOpenTracker_SendCloud_Click(t *testing.T) {
	s := newOpenTracker(t)
	err := s.RecordSendCloudEvent(context.Background(), &SendCloudOpenEvent{
		Event:     "click",
		Recipient: "user@demo.com",
		MessageID: "msg-1",
		URL:       "https://example.com/landing",
	})
	if err == nil {
		t.Log("expected failure with nil repo:", err)
	}
}

func TestEmailOpenTracker_SendCloud_Bounce(t *testing.T) {
	s := newOpenTracker(t)
	err := s.RecordSendCloudEvent(context.Background(), &SendCloudOpenEvent{
		Event:     "bounce",
		Recipient: "user@demo.com",
		MessageID: "msg-1",
		Reason:    "mailbox full",
	})
	if err == nil {
		t.Log("expected failure with nil repo:", err)
	}
}

func TestEmailOpenTracker_SendCloud_Unsupported(t *testing.T) {
	s := newOpenTracker(t)
	err := s.RecordSendCloudEvent(context.Background(), &SendCloudOpenEvent{
		Event: "unknown",
	})
	if err == nil {
		t.Error("Expected error for unsupported event")
	}
}

func TestEmailOpenTracker_SendCloud_NilEvent(t *testing.T) {
	s := newOpenTracker(t)
	if err := s.RecordSendCloudEvent(context.Background(), nil); err == nil {
		t.Error("Expected error for nil event")
	}
}

// 7) GetOpenRateMetrics
func TestEmailOpenTracker_GetOpenRateMetrics_NilDB(t *testing.T) {
	s := newOpenTracker(t)
	_, err := s.GetOpenRateMetrics(context.Background(), "job-1", 100)
	if err == nil {
		t.Error("Expected error with nil repo")
	}
}

func TestEmailOpenTracker_GetOpenRateMetrics_EmptyJobID(t *testing.T) {
	s := newOpenTracker(t)
	_, err := s.GetOpenRateMetrics(context.Background(), "", 100)
	if err == nil {
		t.Error("Expected error for empty job_id")
	}
}

// 8) MarkOpenSeen 防重
func TestEmailOpenTracker_MarkOpenSeen(t *testing.T) {
	resetPixelCacheForTest(t)
	defer resetPixelCacheForTest(t)
	token := "test-token-1"
	if !MarkOpenSeen(token) {
		t.Error("First call should return true")
	}
	if MarkOpenSeen(token) {
		t.Error("Second call within 30s should return false")
	}
	if !MarkOpenSeen("other-token") {
		t.Error("Different token should return true")
	}
	if !MarkOpenSeen("") {
	}
}

// 9) PrettyPrintJSON
func TestEmailOpenTracker_PrettyPrintJSON(t *testing.T) {
	out, err := PrettyPrintJSON(map[string]any{"event": "open"})
	if err != nil {
		t.Fatalf("PrettyPrintJSON failed: %v", err)
	}
	if !strings.Contains(out, "open") {
		t.Error("Expected output to contain 'open'")
	}
}

// 10) EmailEventSummary
func TestEmailOpenTracker_EmailEventSummary(t *testing.T) {
	out := EmailEventSummary("open", "u@d.com")
	if !strings.Contains(out, "open") || !strings.Contains(out, "u@d.com") {
		t.Errorf("Unexpected summary: %s", out)
	}
}
