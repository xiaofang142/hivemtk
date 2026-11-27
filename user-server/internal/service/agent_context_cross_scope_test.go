package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/pkg/tenant"
)

type crossScopeReader struct {
	wantAccount string
	wantGroup   string
	rows        []UnifiedMessage
	queries     []UnifiedMessageQuery
}

func (r *crossScopeReader) List(_ context.Context, query UnifiedMessageQuery) ([]UnifiedMessage, error) {
	r.queries = append(r.queries, query)
	if len(query.AccountIDs) != 1 || query.AccountIDs[0] != r.wantAccount {
		return nil, fmt.Errorf("读取器收到越权账号: %v", query.AccountIDs)
	}
	if r.wantGroup != "" && query.GroupID != r.wantGroup {
		return nil, fmt.Errorf("读取器收到越权群: %q", query.GroupID)
	}
	out := make([]UnifiedMessage, 0, len(r.rows))
	for _, row := range r.rows {
		if row.AccountID != r.wantAccount {
			continue
		}
		if r.wantGroup != "" && row.GroupID != r.wantGroup {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func crossScopeMessage(id, account, group, sender, content string) UnifiedMessage {
	return UnifiedMessage{
		ContextMessage: agentcontext.ContextMessage{
			MessageID:  id,
			SenderID:   sender,
			Content:    content,
			SentAt:     time.Unix(1, 0),
			Visibility: agentcontext.VisibilityPublic,
		},
		AccountID: account,
		GroupID:   group,
	}
}

func buildCrossScopeContext(t *testing.T, scope agentcontext.ConversationScope, reader *crossScopeReader, senderID string, customer *agentcontext.CustomerContext) *agentcontext.AgentConversationContext {
	t.Helper()
	ctx := tenant.WithTenantID(context.Background(), scope.TenantID)
	view, err := NewConversationContextBuilder(reader).Build(ctx, AgentContextBuilderInput{
		Scope:         scope,
		Current:       agentcontext.ContextMessage{MessageID: "current-1", SenderID: senderID, Content: "现在呢"},
		Customer:      customer,
		PromptVersion: "cross-scope-v1",
	})
	if err != nil {
		t.Fatalf("上下文装配失败: %v", err)
	}
	return view
}

func hasMessageID(messages []agentcontext.ContextMessage, id string) bool {
	for _, msg := range messages {
		if msg.MessageID == id {
			return true
		}
	}
	return false
}

func TestCrossAccountSameCustomerContextIsolation(t *testing.T) {
	base := agentcontext.ConversationScope{
		TenantID: 7, Platform: "douyin", StablePeerID: "oneid-1",
	}
	scopeA := base
	scopeA.ChannelAccountID = 11
	scopeB := base
	scopeB.ChannelAccountID = 12
	customer := &agentcontext.CustomerContext{
		SubjectType: "dm_person", SubjectID: "oneid-1", DisplayName: "同一客户",
		Facts: []agentcontext.BusinessFact{{
			Key: "意向", Value: "高价套餐", Grade: agentcontext.FactConfirmed, SubjectType: "dm_person", SubjectID: "oneid-1",
		}},
	}

	rows := []UnifiedMessage{
		crossScopeMessage("hub:1", "11", "", "oneid-1", "账号A历史"),
		crossScopeMessage("hub:2", "12", "", "oneid-1", "账号B历史"),
	}
	readerA := &crossScopeReader{wantAccount: "11", rows: rows}
	readerB := &crossScopeReader{wantAccount: "12", rows: rows}
	viewA := buildCrossScopeContext(t, scopeA, readerA, "oneid-1", customer)
	viewB := buildCrossScopeContext(t, scopeB, readerB, "oneid-1", customer)

	if viewA.LLMConversation.ID == viewB.LLMConversation.ID || viewA.LLMConversation.ScopeHash == viewB.LLMConversation.ScopeHash {
		t.Fatalf("同一客户跨账号不得合并会话身份: %s vs %s", viewA.LLMConversation.ID, viewB.LLMConversation.ID)
	}
	if !hasMessageID(viewA.Recent, "hub:1") || hasMessageID(viewA.Recent, "hub:2") {
		t.Fatalf("账号A上下文混入其他账号历史: %+v", viewA.Recent)
	}
	if !hasMessageID(viewB.Recent, "hub:2") || hasMessageID(viewB.Recent, "hub:1") {
		t.Fatalf("账号B上下文混入其他账号历史: %+v", viewB.Recent)
	}
	if len(readerA.queries) != 1 || readerA.queries[0].AccountIDs[0] != "11" {
		t.Fatalf("账号A查询范围错误: %+v", readerA.queries)
	}
	if len(readerB.queries) != 1 || readerB.queries[0].AccountIDs[0] != "12" {
		t.Fatalf("账号B查询范围错误: %+v", readerB.queries)
	}
	for _, view := range []*agentcontext.AgentConversationContext{viewA, viewB} {
		if view.Customer == nil || len(view.Customer.Facts) != 1 {
			t.Fatalf("同账号确认客户事实应保留: %+v", view.Customer)
		}
	}
}

func TestCrossGroupContextIsolation(t *testing.T) {
	base := agentcontext.ConversationScope{
		TenantID: 7, Platform: "feishu", ChannelAccountID: 21,
		IsGroup: true, GroupID: "group-1", ConversationID: "thread-9",
	}
	scopeB := base
	scopeB.GroupID = "group-2"

	rows := []UnifiedMessage{
		crossScopeMessage("hub:g1", "21", "group-1", "u1", "群1消息"),
		crossScopeMessage("hub:g2", "21", "group-2", "u1", "群2消息"),
	}
	reader := &crossScopeReader{wantAccount: "21", wantGroup: "group-1", rows: rows}
	view := buildCrossScopeContext(t, base, reader, "u1", nil)

	if len(reader.queries) != 1 || reader.queries[0].GroupID != "group-1" || reader.queries[0].ConversationID != "" {
		t.Fatalf("群查询必须以 group_id 为根: %+v", reader.queries)
	}
	if !hasMessageID(view.Recent, "hub:g1") || hasMessageID(view.Recent, "hub:g2") {
		t.Fatalf("群上下文混入其他群历史: %+v", view.Recent)
	}
	if view.LLMConversation.ScopeHash == "" {
		t.Fatal("群上下文缺少 scope 身份")
	}
}

func TestCrossGroupPrivateFactsAreFiltered(t *testing.T) {
	scope := agentcontext.ConversationScope{
		TenantID: 7, Platform: "feishu", ChannelAccountID: 21,
		IsGroup: true, GroupID: "group-1", ConversationID: "thread-9",
	}
	reader := &crossScopeReader{wantAccount: "21", wantGroup: "group-1"}
	ctx := tenant.WithTenantID(context.Background(), scope.TenantID)
	shared := []agentcontext.BusinessFact{
		{Key: "mine", Value: "个人报价", Grade: agentcontext.FactConfirmed, SubjectType: "group_member", SubjectID: "u1"},
		{Key: "other", Value: "他人隐私", Grade: agentcontext.FactConfirmed, SubjectType: "group_member", SubjectID: "u2"},
	}
	view, err := NewConversationContextBuilder(reader).Build(ctx, AgentContextBuilderInput{
		Scope:          scope,
		Current:        agentcontext.ContextMessage{MessageID: "current-2", SenderID: "u1", Content: "在吗"},
		SharedBusiness: shared,
		PromptVersion:  "cross-scope-v1",
	})
	if err != nil {
		t.Fatalf("上下文装配失败: %v", err)
	}
	if len(view.SharedBusiness) != 1 || view.SharedBusiness[0].Key != "mine" {
		t.Fatalf("其他成员私有事实必须过滤: %+v", view.SharedBusiness)
	}
}

func TestCrossTenantContextBuilderFailClosed(t *testing.T) {
	scope := agentcontext.ConversationScope{
		TenantID: 7, Platform: "douyin", ChannelAccountID: 11, StablePeerID: "peer-1",
	}
	reader := &crossScopeReader{wantAccount: "11"}
	ctx := tenant.WithTenantID(context.Background(), 8)
	_, err := NewConversationContextBuilder(reader).Build(ctx, AgentContextBuilderInput{
		Scope:   scope,
		Current: agentcontext.ContextMessage{MessageID: "current-3", SenderID: "peer-1", Content: "你好"},
	})
	if err == nil {
		t.Fatal("跨租户构建上下文必须失败")
	}
	if len(reader.queries) != 0 {
		t.Fatalf("越权请求不得触达历史读取器: %+v", reader.queries)
	}
}
