package tooluse

import (
	"fmt"
	"testing"

	"hivemtk-user/internal/pkg/tracing"
)

func TestRedactToolTraceEventRemovesSensitiveValues(t *testing.T) {
	ev := tracing.ToolTraceEvent{
		Input:  map[string]any{"phone": "13800138000", "note": "ORD-2026-11-0001"},
		Output: "contact 13800138000 via a@example.com",
		Error:  "failed for ORD-2026-11-0001",
	}
	got := redactToolTraceEvent(ev)
	all := fmt.Sprint(got.Input) + fmt.Sprint(got.Output) + got.Error
	for _, secret := range []string{"13800138000", "ORD-2026-11-0001", "a@example.com"} {
		if containsLiteral(all, secret) {
			t.Fatalf("trace 仍包含明文 %q：%s", secret, all)
		}
	}
}

func containsLiteral(s, sub string) bool {
	if len(sub) == 0 {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
