package service

import (
	"testing"

	agentcontext "hivemtk-user/internal/aiagent/context"
)

func TestAllowRawSessionHistory(t *testing.T) {
	tests := []struct {
		name string
		req  *SalesRequest
		want bool
	}{
		{name: "nil request", req: nil, want: false},
		{name: "legacy without view", req: &SalesRequest{SessionID: "s1"}, want: true},
		{
			name: "dm with view must not fall back",
			req: &SalesRequest{ContextView: &agentcontext.AgentConversationContext{
				Scope: agentcontext.ConversationScope{IsGroup: false},
			}},
			want: false,
		},
		{
			name: "group with view must not fall back",
			req: &SalesRequest{ContextView: &agentcontext.AgentConversationContext{
				Scope: agentcontext.ConversationScope{IsGroup: true, GroupID: "g1"},
			}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allowRawSessionHistory(tt.req); got != tt.want {
				t.Fatalf("allowRawSessionHistory() = %v, want %v", got, tt.want)
			}
		})
	}
}
