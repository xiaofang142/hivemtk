package service

import (
	"errors"
	"testing"
)

func TestAIGenerationFallbackDispositionReason(t *testing.T) {
	tests := []struct {
		name   string
		closed bool
		err    error
		result *HandleResult
		want   string
	}{
		{name: "already_closed", closed: true, err: errors.New("late failure"), want: ""},
		{name: "orchestrator_failed", err: errors.New("provider down"), want: "ai_failed"},
		{name: "nil_result", result: nil, want: "ai_nil_result"},
		{name: "successful_result_handled_by_delivery", result: &HandleResult{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := aiGenerationFallbackDispositionReason(tt.closed, tt.err, tt.result)
			if got != tt.want {
				t.Fatalf("fallback reason 错误: got %q want %q", got, tt.want)
			}
		})
	}
}
