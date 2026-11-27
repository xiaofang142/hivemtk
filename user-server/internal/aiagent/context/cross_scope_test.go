package context

import "testing"

func TestCrossScopeIdentityIsolation(t *testing.T) {
	dm := ConversationScope{
		TenantID: 7, Platform: "douyin", ChannelAccountID: 11, StablePeerID: "oneid-1",
	}
	otherAccount := dm
	otherAccount.ChannelAccountID = 12
	otherTenant := dm
	otherTenant.TenantID = 8
	otherPlatform := dm
	otherPlatform.Platform = "xiaohongshu"

	a, err := DeriveScopeIdentity(dm)
	if err != nil {
		t.Fatalf("主 scope 身份派生失败: %v", err)
	}
	tests := []struct {
		name  string
		scope ConversationScope
	}{
		{"同客户跨账号", otherAccount},
		{"同账号跨租户", otherTenant},
		{"同账号跨渠道", otherPlatform},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DeriveScopeIdentity(tt.scope)
			if err != nil {
				t.Fatalf("scope 身份派生失败: %v", err)
			}
			if got.ScopeHash == a.ScopeHash || got.LLMConversationID == a.LLMConversationID {
				t.Fatalf("跨 scope 不得复用会话身份: %s vs %s", a.ScopeHash, got.ScopeHash)
			}
			if got.BusLLMSessionID == a.BusLLMSessionID {
				t.Fatalf("跨 scope 不得复用续接 key: %s", got.BusLLMSessionID)
			}
		})
	}
}

func TestCrossGroupIdentityUsesGroupRoot(t *testing.T) {
	base := ConversationScope{
		TenantID: 7, Platform: "feishu", ChannelAccountID: 21,
		IsGroup: true, GroupID: "group-1", ConversationID: "thread-a",
	}
	a, err := DeriveScopeIdentity(base)
	if err != nil {
		t.Fatalf("群 scope 身份派生失败: %v", err)
	}
	sameGroupOtherThread := base
	sameGroupOtherThread.ConversationID = "thread-b"
	b, err := DeriveScopeIdentity(sameGroupOtherThread)
	if err != nil {
		t.Fatalf("同群不同线程身份派生失败: %v", err)
	}
	if a.ScopeHash != b.ScopeHash || a.BusLLMSessionID != b.BusLLMSessionID {
		t.Fatalf("同群不同线程必须共用会话根: %s vs %s", a.ScopeHash, b.ScopeHash)
	}

	otherGroup := base
	otherGroup.GroupID = "group-2"
	c, err := DeriveScopeIdentity(otherGroup)
	if err != nil {
		t.Fatalf("跨群身份派生失败: %v", err)
	}
	if c.ScopeHash == a.ScopeHash || c.BusLLMSessionID == a.BusLLMSessionID {
		t.Fatalf("不同群不得串会话: %s vs %s", a.ScopeHash, c.ScopeHash)
	}
}

func TestCrossScopeSessionBindingRejected(t *testing.T) {
	scope := ConversationScope{
		TenantID: 7, Platform: "douyin", ChannelAccountID: 11, StablePeerID: "peer-1",
	}
	identity, err := DeriveScopeIdentity(scope)
	if err != nil {
		t.Fatalf("身份派生失败: %v", err)
	}
	binding := SessionBinding{ScopeHash: identity.ScopeHash, BusLLMSessionID: identity.BusLLMSessionID}

	if err := ValidateSessionBinding(binding, scope, identity.BusLLMSessionID); err != nil {
		t.Fatalf("同 scope 绑定应通过: %v", err)
	}
	otherTenant := scope
	otherTenant.TenantID = 8
	if err := ValidateSessionBinding(binding, otherTenant, identity.BusLLMSessionID); err != ErrSessionScopeConflict {
		t.Fatalf("跨租户 key 复用必须拒绝, got %v", err)
	}
	otherAccount := scope
	otherAccount.ChannelAccountID = 12
	if err := ValidateSessionBinding(binding, otherAccount, identity.BusLLMSessionID); err != ErrSessionScopeConflict {
		t.Fatalf("跨账号 key 复用必须拒绝, got %v", err)
	}
	if err := ValidateSessionBinding(binding, scope, "llmsess_forged"); err != ErrSessionScopeConflict {
		t.Fatalf("伪造 key 必须拒绝, got %v", err)
	}
}
