package service

import (
	"context"
	"errors"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/pkg/tenant"
)

type fakeAgentContextReader struct {
	messages []UnifiedMessage
	err      error
	calls    int
	query    UnifiedMessageQuery
}

func (f *fakeAgentContextReader) List(_ context.Context, query UnifiedMessageQuery) ([]UnifiedMessage, error) {
	f.calls++
	f.query = query
	if f.err != nil {
		return nil, f.err
	}
	return append([]UnifiedMessage{}, f.messages...), nil
}

func builderTestMessage(id, sender, content string, at time.Time, visibility agentcontext.Visibility) UnifiedMessage {
	return UnifiedMessage{ContextMessage: agentcontext.ContextMessage{
		MessageID: id, SenderID: sender, Content: content, SentAt: at,
		Visibility: visibility, Origin: agentcontext.OriginMessageHub,
		HistorySource: agentcontext.HistorySourceMessageHub,
	}}
}

func TestConversationContextBuilderCrossSessionDM(t *testing.T) {
	base := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	reader := &fakeAgentContextReader{messages: []UnifiedMessage{
		builderTestMessage("hub:30", "cust", "上一会话的追问", base.Add(-time.Minute), agentcontext.VisibilityPublic),
		builderTestMessage("hub:20", "bot", "更早的答复", base.Add(-2*time.Minute), agentcontext.VisibilityPublic),
	}}
	builder := NewConversationContextBuilder(reader)
	scope := agentcontext.ConversationScope{
		TenantID: 7, Platform: "telegram", ChannelAccountID: 3,
		ConversationID: "dm-1", SessionID: "session-A", BusLLMSessionID: "llmsess-a",
	}
	ctx := tenant.WithTenantID(context.Background(), 7)
	result, err := builder.Build(ctx, AgentContextBuilderInput{
		Scope: scope,
		Current: agentcontext.ContextMessage{
			MessageID: "hub:40", SenderID: "cust", Content: "那第二个方案呢", SentAt: base,
			Visibility: agentcontext.VisibilityPublic,
		},
		Now: base,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Recent) != 2 || result.Recent[0].MessageID != "hub:20" || result.Recent[1].MessageID != "hub:30" {
		t.Fatalf("跨业务会话历史应按稳定 cursor 合并：%+v", result.Recent)
	}
	if reader.query.SessionID != "" || reader.query.ConversationID != "dm-1" {
		t.Fatalf("查询不得绑定单个业务 SessionID：%+v", reader.query)
	}
	if result.Provenance.VisibilityProof != "scope_verified" || result.SnapshotID == "" {
		t.Fatalf("provenance 缺失：%+v", result.Provenance)
	}
}

func TestConversationContextBuilderGroupFiltersPrivateHistoryAndFacts(t *testing.T) {
	base := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	reader := &fakeAgentContextReader{messages: []UnifiedMessage{
		builderTestMessage("hub:11", "u2", "群里公开问题", base.Add(-time.Minute), agentcontext.VisibilityPublic),
		builderTestMessage("hub:12", "u2", "私下报价秘密", base.Add(-30*time.Second), agentcontext.VisibilityMember),
	}}
	reader.messages[1].Audience = []string{"u2"}
	builder := NewConversationContextBuilder(reader)
	scope := agentcontext.ConversationScope{
		TenantID: 7, Platform: "telegram", ChannelAccountID: 3,
		IsGroup: true, GroupID: "group-1", ConversationID: "thread-9", SessionID: "session-B",
	}
	result, err := builder.Build(tenant.WithTenantID(context.Background(), 7), AgentContextBuilderInput{
		Scope: scope,
		Current: agentcontext.ContextMessage{
			MessageID: "hub:13", SenderID: "u1", Content: "谁有报价", SentAt: base,
			Visibility: agentcontext.VisibilityPublic,
		},
		Customer: &agentcontext.CustomerContext{SubjectType: "group_member", SubjectID: "u2", Facts: []agentcontext.BusinessFact{
			{Key: "secret_budget", Value: "预算私密", Grade: agentcontext.FactConfirmed, SubjectType: "group_member", SubjectID: "u2"},
		}},
		Now: base,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Recent) != 1 || result.Recent[0].MessageID != "hub:11" {
		t.Fatalf("私有群历史必须过滤：%+v", result.Recent)
	}
	if result.Customer != nil && len(result.Customer.Facts) != 0 {
		t.Fatalf("其他成员私有事实不得进群上下文：%+v", result.Customer)
	}
	if result.Provenance.VisibilityProof != "group_audienceVerified" {
		t.Fatalf("群可见性证明标记错误：%q", result.Provenance.VisibilityProof)
	}
	if reader.query.GroupID != "group-1" || reader.query.ConversationID != "" {
		t.Fatalf("群查询必须以 group_id 为根：%+v", reader.query)
	}
}

func TestConversationContextBuilderFailClosed(t *testing.T) {
	reader := &fakeAgentContextReader{}
	builder := NewConversationContextBuilder(reader)
	scope := agentcontext.ConversationScope{TenantID: 7, Platform: "telegram", ChannelAccountID: 3, ConversationID: "dm-1"}
	if _, err := builder.Build(context.Background(), AgentContextBuilderInput{
		Scope: scope, Current: agentcontext.ContextMessage{MessageID: "hub:1"},
	}); err == nil {
		t.Fatal("缺租户上下文必须 fail-closed")
	}
	if reader.calls != 0 {
		t.Fatal("校验失败不得读取历史")
	}
	if _, err := builder.Build(tenant.WithTenantID(context.Background(), 8), AgentContextBuilderInput{
		Scope: scope, Current: agentcontext.ContextMessage{MessageID: "hub:1"},
	}); err == nil {
		t.Fatal("ctx 租户与 scope 不一致必须拒绝")
	}
	reader.err = errors.New("store down")
	if _, err := builder.Build(tenant.WithTenantID(context.Background(), 7), AgentContextBuilderInput{
		Scope: scope, Current: agentcontext.ContextMessage{MessageID: "hub:1"},
	}); err == nil {
		t.Fatal("读取失败不得静默降级为空上下文")
	}
}
