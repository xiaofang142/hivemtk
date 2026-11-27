package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
)

func TestLongInputLongOutputFixtureQuality(t *testing.T) {
	const requiredCode = "MAGIC-7421"
	messages := make([]agentcontext.ContextMessage, 0, 800)
	messages = append(messages, agentcontext.ContextMessage{
		MessageID: "hub:1", SenderID: "u1", Content: "最初确认的代码是 " + requiredCode + " " + strings.Repeat("背景说明", 60),
		SentAt: time.Unix(1, 0), Visibility: agentcontext.VisibilityPublic,
	})
	for i := 2; i <= 800; i++ {
		messages = append(messages, agentcontext.ContextMessage{
			MessageID: fmt.Sprintf("hub:%d", i), SenderID: "u1",
			Content: fmt.Sprintf("第%d条普通对话。", i) + strings.Repeat("填充内容", 60),
			SentAt:  time.Unix(int64(i), 0), Visibility: agentcontext.VisibilityPublic,
		})
	}

	totalEstimated := 0
	for _, msg := range messages {
		totalEstimated += agentcontext.EstimateContextTokens(msg.Content)
	}
	if totalEstimated <= 8192 {
		t.Fatalf("夹具输入未超过 8192 token: %d", totalEstimated)
	}

	scope := agentcontext.ConversationScope{
		TenantID: 7, Platform: "douyin", ChannelAccountID: 11, StablePeerID: "peer-long",
	}
	current := agentcontext.ContextMessage{
		MessageID: "hub:current", SenderID: "u1", Content: "最初提到的代码是什么？",
		Visibility: agentcontext.VisibilityPublic, SentAt: time.Unix(1000, 0),
	}
	assemble := func(historyTokens int) *agentcontext.AgentConversationContext {
		t.Helper()
		view, err := agentcontext.AssembleAgentContext(agentcontext.ContextAssemblerInput{
			Scope:   scope,
			Current: current,
			Recent:  messages,
			LLMConversation: agentcontext.LLMConversationRef{
				ID: "llmconv_long", ScopeHash: "hash-long",
			},
			InputBudget: agentcontext.ContextBudget{HistoryTokens: historyTokens},
			Policy: agentcontext.SourcePolicy{
				Kind: agentcontext.SourcePolicyDM, Version: "source-policy-v1",
			},
			PromptVersion: "long-fixture-v1",
			Now:           time.Unix(2000, 0),
		})
		if err != nil {
			t.Fatalf("长上下文装配失败: %v", err)
		}
		return view
	}

	longView := assemble(100000)
	shortView := assemble(1024)
	longQuality := fixtureAnswerQuality(longView, requiredCode)
	shortQuality := fixtureAnswerQuality(shortView, requiredCode)
	if longQuality != 1 || shortQuality != 0 {
		t.Fatalf("长窗口应保留早期证据而小窗口不应保留: long=%d short=%d", longQuality, shortQuality)
	}

	budget, err := llm.NewContextBudget(llm.ProviderConfig{
		ContextWindow: 200000, MaxOutputTokens: 8192,
	}, 6000)
	if err != nil {
		t.Fatalf("长输出预算构造失败: %v", err)
	}
	if got := budget.ClampOutput(6000); got <= 4096 {
		t.Fatalf("长输出夹具未达到 >4096: %d", got)
	}
	if budget.TotalInputBudget() <= 8192 {
		t.Fatalf("长输入预算未超过 8192: %d", budget.TotalInputBudget())
	}
}

func fixtureAnswerQuality(view *agentcontext.AgentConversationContext, required string) int {
	if view == nil {
		return 0
	}
	for _, msg := range view.Recent {
		if strings.Contains(msg.Content, required) {
			return 1
		}
	}
	return 0
}
