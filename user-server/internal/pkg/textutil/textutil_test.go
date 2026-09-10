package textutil

import (
	"strings"
	"testing"
)

func TestTruncateText_ShortCircuit(t *testing.T) {
	if got := TruncateText("hello", 100); got != "hello" {
		t.Fatalf("short text unchanged, got %q", got)
	}
}

func TestTruncateText_ByteLimitAndMarker(t *testing.T) {
	s := strings.Repeat("a", 30)
	got := TruncateText(s, 10)
	if !strings.HasPrefix(got, strings.Repeat("a", 10)) {
		t.Fatalf("head should be preserved, got %q", got)
	}
	if !strings.Contains(got, "truncated 20 bytes") {
		t.Fatalf("truncation marker should report cut size, got %q", got)
	}
}

func TestTruncateText_DoesNotSplitRune(t *testing.T) {
	// 3 字节/汉字；limit 落在多字节字符中间时应回退而不是产出乱码
	got := TruncateText("中文中文", 4)
	if strings.Contains(got, "\uFFFD") {
		t.Fatalf("should not contain replacement rune, got %q", got)
	}
	if !strings.HasPrefix(got, "中") {
		t.Fatalf("first rune should survive, got %q", got)
	}
}

func TestTruncateText_DefaultMaxBytes(t *testing.T) {
	s := strings.Repeat("b", DefaultMaxBytes+5)
	got := TruncateText(s, 0)
	if len(got) <= DefaultMaxBytes {
		t.Fatalf("maxBytes<=0 should apply DefaultMaxBytes and truncate, len=%d", len(got))
	}
}
