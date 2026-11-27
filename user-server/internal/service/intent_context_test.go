package service

import (
	"context"
	"strings"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/pkg/tenant"
)

func testIntentContextView() *agentcontext.AgentConversationContext {
	at := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	return &agentcontext.AgentConversationContext{
		SnapshotID: "snapshot-intent",
		Scope: agentcontext.ConversationScope{
			TenantID: 7, Platform: "telegram", ChannelAccountID: 1,
			ConversationID: "conv-1", BusLLMSessionID: "llmsess_bus_1",
		},
		Current: agentcontext.ContextMessage{
			MessageID: "hub:3", SenderID: "u1", SenderName: "张三",
			Content: "那个多少钱？", SentAt: at.Add(3 * time.Minute),
		},
		Recent: []agentcontext.ContextMessage{
			{
				MessageID: "hub:1", SenderID: "bot", Role: "assistant",
				Content: "我们上次聊的是企业版报价方案", SentAt: at,
				Visibility: agentcontext.VisibilityPublic,
			},
			{
				MessageID: "hub:2", SenderID: "u1", Role: "user",
				Content: "不对，不是基础版，是专业版", SentAt: at.Add(time.Minute),
				Visibility: agentcontext.VisibilityPublic,
			},
			{
				MessageID: "hub:9", SenderID: "u2", Role: "user",
				Content: "私人手机号 13800000000", SentAt: at.Add(2 * time.Minute),
				Visibility: agentcontext.VisibilityUnknown,
			},
		},
		Customer: &agentcontext.CustomerContext{
			SubjectType: "dm_person", SubjectID: "u1", DisplayName: "张三",
			Facts: []agentcontext.BusinessFact{
				{Key: "预算", Value: "两万元", Grade: agentcontext.FactConfirmed, Source: "customer"},
				{Key: "推测预算", Value: "三万元", Grade: agentcontext.FactSuggestion, Source: "model"},
				{Key: "旧备注", Value: "已签约", Grade: agentcontext.FactLegacyUnverified, Source: "legacy"},
			},
		},
		Lead: &agentcontext.LeadContext{
			SubjectType: "dm_person", SubjectID: "u1",
			Requirement: "需要专业版报价",
		},
		SharedBusiness: []agentcontext.BusinessFact{
			{Key: "待办", Value: "发送报价单", Grade: agentcontext.FactSuggestion, SubjectType: "group_member", SubjectID: "u2"},
		},
	}
}

func TestBuildIntentContextSignalsResolvesPronoun(t *testing.T) {
	signals := BuildIntentContextSignals("那个多少钱？", testIntentContextView())
	if !strings.Contains(signals.ResolvedText, "企业版报价方案") {
		t.Fatalf("指代应补出上一轮主题, got %q", signals.ResolvedText)
	}
	if signals.RecentTopic != "企业版报价方案" {
		t.Fatalf("主题提取错误, got %q", signals.RecentTopic)
	}
}

func TestBuildIntentContextSignalsDetectsCorrectionConflict(t *testing.T) {
	signals := BuildIntentContextSignals("基础版多少钱？", testIntentContextView())
	if !signals.Conflict {
		t.Fatal("当前消息重复了近期被否定的主题，应标记冲突")
	}
	if len(signals.RecentCorrections) == 0 {
		t.Fatal("近期否定/纠正应被记录")
	}
}

func TestBuildIntentContextBlockOnlyStatesConfirmedFacts(t *testing.T) {
	block := BuildIntentContextBlock(testIntentContextView())
	if !strings.Contains(block, "单聊") || !strings.Contains(block, "张三") {
		t.Fatalf("来源策略与当前发言者应进入上下文, got %q", block)
	}
	if !strings.Contains(block, "企业版报价方案") {
		t.Fatalf("近期历史应进入上下文, got %q", block)
	}
	if !strings.Contains(block, "两万元") {
		t.Fatalf("已确认事实应进入上下文, got %q", block)
	}
	if strings.Contains(block, "三万元") || strings.Contains(block, "已签约") {
		t.Fatalf("建议/历史未核实备注不得当事实复述, got %q", block)
	}
	if strings.Contains(block, "13800000000") {
		t.Fatalf("不可证明可见性的消息不得进入模型上下文, got %q", block)
	}
}

func TestRecognizeWithContextRulePathUsesResolvedText(t *testing.T) {
	r := &IntentRecognizer{}
	got, err := r.RecognizeWithContext(tenant.WithTenantID(context.Background(), 7), "s", "c", "那个多少钱？", testIntentContextView())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !strings.HasPrefix(got.Method, "rule") {
		t.Fatalf("规则快路径应仍可用, got %+v", got)
	}
	if got.IntentType != IntentPriceInquiry {
		t.Fatalf("上下文补全后应命中价格咨询, got %+v", got)
	}
}

func TestRecognizeIntentWithContextUsesResolvedText(t *testing.T) {
	r := &IntentRecognizer{}
	got, err := r.RecognizeIntentWithContext(tenant.WithTenantID(context.Background(), 7), "那个多少钱？", "c", "s", testIntentContextView())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !strings.HasPrefix(got.Method, "rule") {
		t.Fatalf("精细意图规则快路径应仍可用, got %+v", got)
	}
	if got.Major == "" {
		t.Fatal("精细意图应有大类")
	}
}
