package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
)

func groupFixtureScope() agentcontext.ConversationScope {
	return agentcontext.ConversationScope{
		TenantID: 7, Platform: "feishu", ChannelAccountID: 31,
		IsGroup: true, GroupID: "group-quality", ConversationID: "thread-1",
	}
}

func groupFixtureView(t *testing.T, recent []agentcontext.ContextMessage, current agentcontext.ContextMessage, policyKind string) *agentcontext.AgentConversationContext {
	t.Helper()
	identity, err := agentcontext.DeriveScopeIdentity(groupFixtureScope())
	if err != nil {
		t.Fatalf("群 scope 身份失败: %v", err)
	}
	kind := agentcontext.SourcePolicyKind(policyKind)
	if kind == "" {
		kind = agentcontext.SourcePolicyGroup
	}
	view, err := agentcontext.AssembleAgentContext(agentcontext.ContextAssemblerInput{
		Scope:   groupFixtureScope(),
		Current: current,
		Recent:  recent,
		LLMConversation: agentcontext.LLMConversationRef{
			ID: identity.LLMConversationID, ScopeHash: identity.ScopeHash,
		},
		InputBudget:   agentcontext.ContextBudget{HistoryTokens: 100000},
		Policy:        agentcontext.SourcePolicy{Kind: kind, Version: "source-policy-v1"},
		PromptVersion: "group-quality-v1",
		Now:           time.Unix(2000, 0),
	})
	if err != nil {
		t.Fatalf("群上下文装配失败: %v", err)
	}
	return view
}

func TestGroupQualityFalseTriggerFixture(t *testing.T) {
	current := agentcontext.ContextMessage{
		MessageID: "hub:cur", SenderID: "u2", Content: "这套方案我要再考虑一下",
		SentAt: time.Unix(100, 0), Visibility: agentcontext.VisibilityPublic,
	}
	view := groupFixtureView(t, nil, current, "")
	if fixtureGroupTrigger(view) {
		t.Fatal("群内成员互相聊天且未提及助手时不应触发机器人")
	}
	addressed := current
	addressed.Content = "@客服助手 这套方案我要再考虑一下"
	viewAddressed := groupFixtureView(t, nil, addressed, "")
	if !fixtureGroupTrigger(viewAddressed) {
		t.Fatal("明确 @ 助手时应触发机器人")
	}
}

func fixtureGroupTrigger(view *agentcontext.AgentConversationContext) bool {
	if view == nil {
		return false
	}
	if view.Provenance.VisibilityProof != groupVisibilityProofVerified && view.Scope.IsGroup {
		return false
	}
	return strings.Contains(view.Current.Content, "@客服助手") || strings.Contains(view.Current.Content, "客服")
}

func TestGroupQualityNoiseCompressionFixture(t *testing.T) {
	recent := make([]agentcontext.ContextMessage, 0, 120)
	for i := 0; i < 120; i++ {
		recent = append(recent, agentcontext.ContextMessage{
			MessageID: fmt.Sprintf("hub:%d", i), SenderID: "u1",
			Content: strings.Repeat("闲聊表情", 30), SentAt: time.Unix(int64(i), 0),
			Visibility: agentcontext.VisibilityPublic,
		})
	}
	current := agentcontext.ContextMessage{
		MessageID: "hub:cur", SenderID: "u2", Content: "@客服助手 在吗",
		SentAt: time.Unix(200, 0), Visibility: agentcontext.VisibilityPublic,
	}
	view := groupFixtureView(t, recent, current, "")
	// 小窗口模式：只按 2048 token 预算保留最新噪声，验证压缩率与省略原因。
	small, err := agentcontext.AssembleAgentContext(agentcontext.ContextAssemblerInput{
		Scope:           groupFixtureScope(),
		Current:         current,
		Recent:          recent,
		InputBudget:     agentcontext.ContextBudget{HistoryTokens: 2048},
		Policy:          agentcontext.SourcePolicy{Kind: agentcontext.SourcePolicyGroup, Version: "source-policy-v1"},
		PromptVersion:   "group-quality-v1",
		Now:             time.Unix(2000, 0),
		LLMConversation: view.LLMConversation,
	})
	if err != nil {
		t.Fatalf("小窗口装配失败: %v", err)
	}
	if len(small.Recent) >= len(recent) {
		t.Fatalf("噪声未按预算压缩: kept=%d total=%d", len(small.Recent), len(recent))
	}
	if len(small.Recent) == 0 || len(small.Recent) >= len(recent) {
		t.Fatalf("噪声压缩比例异常: kept=%d total=%d", len(small.Recent), len(recent))
	}
	if len(small.Provenance.Omitted) == 0 {
		t.Fatal("省略原因必须可追溯")
	}
}

func TestGroupQualityEvidenceAttributionFixture(t *testing.T) {
	current := agentcontext.ContextMessage{
		MessageID: "hub:cur", SenderID: "u2", Content: "@客服助手 我要供货",
		SentAt: time.Unix(100, 0), Visibility: agentcontext.VisibilityPublic,
	}
	recent := []agentcontext.ContextMessage{
		{MessageID: "hub:11", SenderID: "u1", Content: "我有十台现货", SentAt: time.Unix(1, 0), Visibility: agentcontext.VisibilityPublic},
		{MessageID: "hub:12", SenderID: "u2", Content: "我在找货源", SentAt: time.Unix(2, 0), Visibility: agentcontext.VisibilityPublic},
	}
	view := groupFixtureView(t, recent, current, "")
	scope := groupFixtureScope()
	in := DetectionInput{
		Scope:       &scope,
		ContextView: view,
		Messages: []DetectionMessage{
			{MessageID: "hub:11", SenderID: "u1", Content: "我有十台现货", Role: "user", Visibility: agentcontext.VisibilityPublic},
			{MessageID: "hub:12", SenderID: "u2", Content: "我在找货源", Role: "user", Visibility: agentcontext.VisibilityPublic},
		},
	}
	messages := BuildLeadDetectionMessages(in)
	if len(messages) != 2 {
		t.Fatalf("可见消息应全部保留: %d", len(messages))
	}
	if !strings.Contains(messages[0].Content, "msg id=hub:11") || !strings.Contains(messages[0].Content, "sender=u1") {
		t.Fatalf("证据归属必须带真实 ID 与发送者: %+v", messages[0])
	}
	if !strings.Contains(messages[1].Content, "sender=u2") {
		t.Fatalf("第二条证据归属错误: %+v", messages[1])
	}
	for _, msg := range messages {
		if strings.Contains(msg.Content, "history_") {
			t.Fatalf("不得使用合成证据 ID: %+v", msg)
		}
	}
}

func TestGroupQualityPrivacyBlockFixture(t *testing.T) {
	current := agentcontext.ContextMessage{
		MessageID: "hub:cur", SenderID: "u2", Content: "@客服助手 查一下订单",
		SentAt: time.Unix(100, 0), Visibility: agentcontext.VisibilityPublic,
	}
	view := groupFixtureView(t, nil, current, "")
	view.SharedBusiness = []agentcontext.BusinessFact{{
		Key: "电话", Value: "13800138000", Grade: agentcontext.FactConfirmed,
		SubjectType: "group_member", SubjectID: "u3",
	}}
	blocked := CheckGroupReplyGate(true, view, "u2", "您的联系电话是 13800138000")
	if blocked.Allowed || len(blocked.Reasons) == 0 {
		t.Fatalf("含其他成员私有事实的回复必须拦截: %+v", blocked)
	}
	allowed := CheckGroupReplyGate(true, view, "u2", "好的，请提供订单号")
	if !allowed.Allowed {
		t.Fatalf("干净回复应放行: %+v", allowed)
	}
	unverified := CheckGroupReplyGate(true, nil, "u2", "你好")
	if unverified.Allowed || len(unverified.Reasons) == 0 {
		t.Fatalf("无可证明上下文时群回复必须 fail-closed: %+v", unverified)
	}
}

func TestGroupQualityContextRotFixture(t *testing.T) {
	key := "ANCHOR-8899"
	recent := make([]agentcontext.ContextMessage, 0, 200)
	recent = append(recent, agentcontext.ContextMessage{
		MessageID: "hub:0", SenderID: "u1", Content: "关键锚点 " + key + strings.Repeat("背景", 100),
		SentAt: time.Unix(0, 0), Visibility: agentcontext.VisibilityPublic,
	})
	for i := 1; i < 200; i++ {
		recent = append(recent, agentcontext.ContextMessage{
			MessageID: fmt.Sprintf("hub:%d", i), SenderID: "u1",
			Content: strings.Repeat("无关噪声", 40), SentAt: time.Unix(int64(i), 0),
			Visibility: agentcontext.VisibilityPublic,
		})
	}
	current := agentcontext.ContextMessage{
		MessageID: "hub:cur", SenderID: "u2", Content: "@客服助手 锚点是什么",
		SentAt: time.Unix(300, 0), Visibility: agentcontext.VisibilityPublic,
	}
	view := groupFixtureView(t, recent, current, "")
	fullBlock := BuildIntentContextBlock(view)
	if strings.Contains(fullBlock, key) {
		t.Fatal("长群历史下最近窗口不应保留最早锚点（context rot 基线）")
	}

	withSummary := *view
	withSummary.OlderSummary = &agentcontext.MemorySummary{
		Summary: "长期摘要：关键锚点 " + key, Grade: agentcontext.FactDerivedSummary,
		SourceCursor: "hub:0", Version: 1,
	}
	summaryBlock := BuildIntentContextBlock(&withSummary)
	if !strings.Contains(summaryBlock, key) {
		t.Fatalf("摘要应恢复被噪声淹没的历史证据: %s", summaryBlock)
	}
}
