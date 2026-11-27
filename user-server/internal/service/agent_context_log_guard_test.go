package service

import (
	"strings"
	"testing"
)

func TestRedactSharedNotesForLogRemovesPII(t *testing.T) {
	notes := []string{"客户手机 13800138000", "ORD-2026-11-0001 需要跟进", "普通备注"}
	redacted := redactSharedNotesForLog(notes)
	joined := strings.Join(redacted, "|")
	for _, secret := range []string{"13800138000", "ORD-2026-11-0001"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("日志仍含明文 %q：%s", secret, joined)
		}
	}
	if !strings.Contains(joined, "[REDACTED:") {
		t.Fatalf("缺少脱敏标记：%s", joined)
	}
}
