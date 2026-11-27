package context

import (
	"errors"
	"testing"
)

func TestDeriveScopeIdentityDM(t *testing.T) {
	tests := []struct {
		name    string
		scope   ConversationScope
		wantErr error
	}{
		{
			name: "conversation_id",
			scope: ConversationScope{
				TenantID:         1,
				Platform:         "telegram",
				ChannelAccountID: 2,
				ConversationID:   "dm:alice",
			},
		},
		{
			name: "stable_peer_fallback",
			scope: ConversationScope{
				TenantID:         1,
				Platform:         "telegram",
				ChannelAccountID: 2,
				StablePeerID:     "user:alice",
			},
		},
		{
			name: "missing_stable_peer",
			scope: ConversationScope{
				TenantID:         1,
				Platform:         "telegram",
				ChannelAccountID: 2,
				BusSessionID:     "telegram:2:Alice",
			},
			wantErr: ErrMissingStablePeer,
		},
		{
			name: "missing_tenant",
			scope: ConversationScope{
				Platform:         "telegram",
				ChannelAccountID: 2,
				ConversationID:   "dm:alice",
			},
			wantErr: ErrInvalidScope,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DeriveScopeIdentity(tt.scope)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("期望错误 %v，实际 %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("派生 scope identity 失败: %v", err)
			}
			if got.ScopeHash == "" || got.LLMConversationID == "" || got.BusLLMSessionID == "" {
				t.Fatalf("派生结果不完整: %+v", got)
			}
			if len(got.LLMConversationID) <= len("llmconv_") {
				t.Fatalf("LLMConversationID 格式错误: %q", got.LLMConversationID)
			}
			if len(got.BusLLMSessionID) <= len("llmsess_") {
				t.Fatalf("BusLLMSessionID 格式错误: %q", got.BusLLMSessionID)
			}
		})
	}
}

func TestDeriveScopeIdentityGroupUsesGroupRoot(t *testing.T) {
	base := ConversationScope{
		TenantID:         1,
		Platform:         "telegram",
		ChannelAccountID: 2,
		IsGroup:          true,
		GroupID:          "group:42",
	}
	withThread := base
	withThread.ConversationID = "thread:99"
	withOtherSender := base
	withOtherSender.StablePeerID = "user:bob"

	first, err := DeriveScopeIdentity(base)
	if err != nil {
		t.Fatalf("派生群 scope identity 失败: %v", err)
	}
	second, err := DeriveScopeIdentity(withThread)
	if err != nil {
		t.Fatalf("派生群线程 scope identity 失败: %v", err)
	}
	third, err := DeriveScopeIdentity(withOtherSender)
	if err != nil {
		t.Fatalf("派生群成员 scope identity 失败: %v", err)
	}
	if first.ScopeHash != second.ScopeHash || first.ScopeHash != third.ScopeHash {
		t.Fatalf("同群不同线程/成员不得拆分根身份: %+v %+v %+v", first, second, third)
	}
	if first.BusLLMSessionID != second.BusLLMSessionID || first.BusLLMSessionID != third.BusLLMSessionID {
		t.Fatalf("同群应复用同一 fallback session key: %+v %+v %+v", first, second, third)
	}
}

func TestDeriveScopeIdentityRejectsGroupWithoutGroupID(t *testing.T) {
	_, err := DeriveScopeIdentity(ConversationScope{
		TenantID:         1,
		Platform:         "telegram",
		ChannelAccountID: 2,
		IsGroup:          true,
		ConversationID:   "thread:99",
	})
	if !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("群缺 GroupID 应 fail-closed，实际 %v", err)
	}
}

func TestDeriveScopeIdentityIgnoresNicknameSessionKey(t *testing.T) {
	alice := ConversationScope{
		TenantID:         1,
		Platform:         "telegram",
		ChannelAccountID: 2,
		BusSessionID:     "telegram:2:Alice",
		StablePeerID:     "user:alice",
	}
	bob := ConversationScope{
		TenantID:         1,
		Platform:         "telegram",
		ChannelAccountID: 2,
		BusSessionID:     "telegram:2:Alice",
		StablePeerID:     "user:bob",
	}
	aliceID, err := DeriveScopeIdentity(alice)
	if err != nil {
		t.Fatalf("派生 Alice identity 失败: %v", err)
	}
	bobID, err := DeriveScopeIdentity(bob)
	if err != nil {
		t.Fatalf("派生 Bob identity 失败: %v", err)
	}
	if aliceID.ScopeHash == bobID.ScopeHash || aliceID.BusLLMSessionID == bobID.BusLLMSessionID {
		t.Fatalf("昵称碰撞不得合并身份: %+v %+v", aliceID, bobID)
	}
}

func TestValidateSessionBindingRejectsCrossScopeReuse(t *testing.T) {
	scopeA := ConversationScope{
		TenantID:         1,
		Platform:         "telegram",
		ChannelAccountID: 2,
		ConversationID:   "dm:alice",
		BusLLMSessionID:  "bus-session-opaque",
	}
	scopeB := scopeA
	scopeB.ConversationID = "dm:bob"

	identityA, err := DeriveScopeIdentity(scopeA)
	if err != nil {
		t.Fatalf("派生 scope A 失败: %v", err)
	}
	binding := SessionBinding{
		BusLLMSessionID: identityA.BusLLMSessionID,
		ScopeHash:       identityA.ScopeHash,
	}
	if err := ValidateSessionBinding(binding, scopeA, identityA.BusLLMSessionID); err != nil {
		t.Fatalf("同 scope 绑定应通过: %v", err)
	}
	if err := ValidateSessionBinding(binding, scopeB, identityA.BusLLMSessionID); !errors.Is(err, ErrSessionScopeConflict) {
		t.Fatalf("跨 scope 复用应拒绝，实际 %v", err)
	}
	if err := ValidateSessionBinding(binding, scopeB, "another-session"); !errors.Is(err, ErrSessionScopeConflict) {
		t.Fatalf("session key 不一致应拒绝，实际 %v", err)
	}
}

func TestDeriveScopeIdentityIsDeterministic(t *testing.T) {
	scope := ConversationScope{
		TenantID:         7,
		Platform:         "wecom",
		ChannelAccountID: 9,
		IsGroup:          true,
		GroupID:          "group:100",
	}
	first, err := DeriveScopeIdentity(scope)
	if err != nil {
		t.Fatalf("首次派生失败: %v", err)
	}
	second, err := DeriveScopeIdentity(scope)
	if err != nil {
		t.Fatalf("二次派生失败: %v", err)
	}
	if first != second {
		t.Fatalf("相同 scope 应得到完全相同的 identity: %+v %+v", first, second)
	}
}
