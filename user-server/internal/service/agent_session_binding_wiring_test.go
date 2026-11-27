package service

import (
	"testing"

	agentcontext "hivemtk-user/internal/aiagent/context"
)

func TestValidatedLLMSessionRef(t *testing.T) {
	view := &agentcontext.AgentConversationContext{
		Scope: agentcontext.ConversationScope{
			TenantID: 7, Platform: "telegram", ChannelAccountID: 42,
			ConversationID: "conv-1", BusLLMSessionID: "llmsess_bound",
		},
	}
	if got := validatedLLMSessionRef(view, "llmsess_bound"); got != "llmsess_bound" {
		t.Fatalf("同 scope 的 canonical key 应放行, got %q", got)
	}
	if got := validatedLLMSessionRef(view, "llmsess_other"); got != "" {
		t.Fatalf("跨 scope/不一致的 key 必须拒绝, got %q", got)
	}
	if got := validatedLLMSessionRef(nil, "llmsess_bound"); got != "" {
		t.Fatalf("无视图时不得放行 key, got %q", got)
	}
}
