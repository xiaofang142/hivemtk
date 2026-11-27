package context

import (
	"strings"
	"testing"
	"time"
)

func testAssemblerInput() ContextAssemblerInput {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	return ContextAssemblerInput{
		Scope:   ConversationScope{TenantID: 7, Platform: "telegram", ChannelAccountID: 3, ConversationID: "dm-1", BusLLMSessionID: "llmsess-1", StablePeerID: "peer-1"},
		Current: ContextMessage{MessageID: "hub:10", Role: "user", Content: "现在预算多少", SentAt: now, Visibility: VisibilityPublic},
		Recent: []ContextMessage{
			{MessageID: "hub:2", Role: "user", Content: "旧消息", SentAt: now.Add(-10 * time.Minute), Visibility: VisibilityPublic},
			{MessageID: "hub:9", Role: "assistant", Content: "已介绍套餐", SentAt: now.Add(-time.Minute), Visibility: VisibilityPublic},
		},
		Policy:          SourcePolicy{Kind: SourcePolicyDM, Version: "dm-v1"},
		PromptVersion:   "prompt-v1",
		LLMConversation: LLMConversationRef{ID: "llmconv-1", ScopeHash: "scope-1"},
		InputBudget:     ContextBudget{MaxInputTokens: 1000, HistoryTokens: 200, SafetyMarginTokens: 50},
		OutputBudget:    OutputBudget{MaxOutputTokens: 512},
		Now:             now,
	}
}

func TestAssembleAgentContextDMKeepsCurrentAndDeterministicSnapshot(t *testing.T) {
	input := testAssemblerInput()
	a, err := AssembleAgentContext(input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := AssembleAgentContext(input)
	if err != nil {
		t.Fatal(err)
	}
	if a.SnapshotID == "" || a.SnapshotID != b.SnapshotID {
		t.Fatalf("快照 ID 必须稳定：%q vs %q", a.SnapshotID, b.SnapshotID)
	}
	if len(a.Recent) != 2 || a.Recent[0].MessageID != "hub:2" {
		t.Fatalf("历史顺序错误：%+v", a.Recent)
	}
	if a.Current.MessageID != "hub:10" {
		t.Fatalf("当前消息丢失：%+v", a.Current)
	}
	if a.Provenance.ContextSchemaVersion != ContextSchemaVersion || a.Provenance.SourcePolicyVersion != "dm-v1" {
		t.Fatalf("来源版本丢失：%+v", a.Provenance)
	}
}

func TestAssembleAgentContextTruncatesHistoryByTokenBudget(t *testing.T) {
	input := testAssemblerInput()
	input.InputBudget.HistoryTokens = 16
	input.Recent = []ContextMessage{
		{MessageID: "hub:2", Role: "user", Content: strings.Repeat("旧", 40), SentAt: input.Now.Add(-3 * time.Minute), Visibility: VisibilityPublic},
		{MessageID: "hub:9", Role: "assistant", Content: strings.Repeat("新", 40), SentAt: input.Now.Add(-time.Minute), Visibility: VisibilityPublic},
	}
	ctx, err := AssembleAgentContext(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Recent) != 1 || ctx.Recent[0].MessageID != "hub:9" {
		t.Fatalf("应按预算保留最新历史：%+v", ctx.Recent)
	}
	if !strings.Contains(strings.Join(ctx.Provenance.Omitted, ","), "history_budget") {
		t.Fatalf("截断原因未记录：%+v", ctx.Provenance.Omitted)
	}
}

func TestAssembleAgentContextGroupFiltersVisibilityAndPrivateFacts(t *testing.T) {
	input := testAssemblerInput()
	input.Scope.IsGroup = true
	input.Scope.GroupID = "group-1"
	input.Policy = SourcePolicy{Kind: SourcePolicyGroup, Version: "group-v1"}
	input.Recent = []ContextMessage{
		{MessageID: "hub:1", Role: "user", Content: "公开消息", SentAt: input.Now.Add(-2 * time.Minute), Visibility: VisibilityPublic},
		{MessageID: "hub:2", Role: "user", Content: "未知可见性", SentAt: input.Now.Add(-time.Minute), Visibility: VisibilityUnknown},
	}
	input.Customer = &CustomerContext{SubjectType: "group_member", SubjectID: "other", Facts: []BusinessFact{{Key: "phone", Value: "13800138000", Grade: FactConfirmed, SubjectType: "group_member", SubjectID: "other"}}}
	input.Lead = &LeadContext{SubjectType: "group", SubjectID: "group-1", Facts: []BusinessFact{{Key: "budget", Value: "50万", Grade: FactConfirmed, SubjectType: "group", SubjectID: "group-1"}}}
	ctx, err := AssembleAgentContext(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Recent) != 1 || ctx.Recent[0].MessageID != "hub:1" {
		t.Fatalf("未知可见性历史必须过滤：%+v", ctx.Recent)
	}
	if ctx.Customer == nil || len(ctx.Customer.Facts) != 0 {
		t.Fatalf("其他群成员的私有事实不得进上下文：%+v", ctx.Customer)
	}
	if ctx.Lead == nil || len(ctx.Lead.Facts) != 1 {
		t.Fatalf("群级事实应保留：%+v", ctx.Lead)
	}
	if ctx.Provenance.VisibilityProof != "group_audienceVerified" {
		t.Fatalf("可见性证明状态错误：%q", ctx.Provenance.VisibilityProof)
	}
}

func TestAssembleAgentContextUnprovenPolicyDropsHistoryAndFacts(t *testing.T) {
	input := testAssemblerInput()
	input.Policy = SourcePolicy{Kind: SourcePolicyUnproven, Version: "unproven-v1"}
	input.Customer = &CustomerContext{Facts: []BusinessFact{{Key: "x", Value: "y", Grade: FactConfirmed}}}
	ctx, err := AssembleAgentContext(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Recent) != 0 {
		t.Fatalf("不可证明来源只允许当前消息：%+v", ctx.Recent)
	}
	if ctx.Customer != nil && len(ctx.Customer.Facts) != 0 {
		t.Fatalf("不可证明来源不得携带画像：%+v", ctx.Customer)
	}
	if ctx.Provenance.VisibilityProof != "visibility_unproven" {
		t.Fatalf("可见性状态错误：%q", ctx.Provenance.VisibilityProof)
	}
}

func TestAssembleAgentContextRejectsInvalidScope(t *testing.T) {
	input := testAssemblerInput()
	input.Scope.TenantID = 0
	if _, err := AssembleAgentContext(input); err == nil {
		t.Fatal("缺租户必须 fail-closed")
	}
	input = testAssemblerInput()
	input.Current.MessageID = ""
	if _, err := AssembleAgentContext(input); err == nil {
		t.Fatal("当前消息缺真实 ID 必须拒绝")
	}
}
