package service

import (
	"strings"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
)

func g1g8View() *agentcontext.AgentConversationContext {
	now := time.Unix(1700000000000, 0)
	msg := func(id, sender, content string, visibility agentcontext.Visibility) agentcontext.ContextMessage {
		return agentcontext.ContextMessage{
			MessageID: id, Origin: agentcontext.OriginMessageHub,
			HistorySource: agentcontext.HistorySourceMessageHub,
			Role:          "user", SenderID: sender, Content: content,
			Visibility: visibility, SentAt: now,
		}
	}
	view := &agentcontext.AgentConversationContext{
		SnapshotID: "snap-g1g8",
		Scope: agentcontext.ConversationScope{
			TenantID: 7, Platform: "telegram", ChannelAccountID: 11,
			ConversationID: "conv-1", BusLLMSessionID: "llmsess_root",
		},
		Current: msg("hub:9", "buyer-1", "上次说的那个还有吗", agentcontext.VisibilityPublic),
		Recent: []agentcontext.ContextMessage{
			msg("hub:7", "buyer-1", "我想了解 A 产品的价格", agentcontext.VisibilityPublic),
			msg("hub:8", "bot-1", "A 产品报价为 100", agentcontext.VisibilityPublic),
		},
		SharedBusiness: []agentcontext.BusinessFact{{
			Key: "confirmed-fact", Value: "客户已确认预算 100", Grade: agentcontext.FactConfirmed,
			Source: "customer", SourceMessageID: "hub:7",
		}},
		Provenance: agentcontext.ContextProvenance{VisibilityProof: "audience_verified"},
	}
	return view
}

func TestLeadRuntimeG1ToG8(t *testing.T) {
	t.Run("G1 rule fast path consumes context", func(t *testing.T) {
		view := g1g8View()
		in := DetectionInput{ContextView: view, Messages: []DetectionMessage{{
			MessageID: view.Current.MessageID, Content: view.Current.Content,
			Role: "user", SenderID: view.Current.SenderID, Visibility: agentcontext.VisibilityPublic,
		}}}
		signals := LeadDetectionContextSignals(in)
		if !strings.Contains(signals.ResolvedText, "A 产品") {
			t.Fatalf("指代补全失败: %q", signals.ResolvedText)
		}
		resolved := leadDetectionResolvedContent(in, in.Messages[0])
		if !strings.Contains(resolved, "A 产品") {
			t.Fatalf("规则快路径未使用补全文本: %q", resolved)
		}
	})

	t.Run("G2 real message id evidence", func(t *testing.T) {
		view := g1g8View()
		view.Current = agentcontext.ContextMessage{
			MessageID: "hub:123", Origin: agentcontext.OriginMessageHub,
			HistorySource: agentcontext.HistorySourceMessageHub, Role: "user",
			SenderID: "buyer-1", Content: "有采购需求", Visibility: agentcontext.VisibilityPublic,
			SentAt: time.Unix(1700000000000, 0),
		}
		in := DetectionInput{ContextView: view, Messages: []DetectionMessage{{
			MessageID: "hub:123", Content: "有采购需求", Role: "user",
			SenderID: "buyer-1", Visibility: agentcontext.VisibilityPublic,
		}}}
		messages := BuildLeadDetectionMessages(in)
		if len(messages) == 0 || !strings.Contains(messages[0].Content, "id=hub:123") {
			t.Fatalf("历史消息必须带真实 MessageID 锚点: %#v", messages)
		}
		allowed := map[string]struct{}{"hub:123": {}}
		valid := validateDetectionEvidenceIDs([]string{"hub:123", "hub:999"}, allowed)
		if len(valid) != 1 || valid[0] != "hub:123" {
			t.Fatalf("幻觉证据 ID 未被拒绝: %#v", valid)
		}
	})

	t.Run("G3 dm history crosses business sessions", func(t *testing.T) {
		view := g1g8View()
		in := DetectionInput{ContextView: view, Messages: []DetectionMessage{
			{MessageID: "hub:7", Content: "旧会话消息", Role: "user", SenderID: "buyer-1", Visibility: agentcontext.VisibilityPublic},
			{MessageID: "hub:8", Content: "新会话消息", Role: "assistant", SenderID: "bot-1", Visibility: agentcontext.VisibilityPublic},
		}}
		joined := strings.Join(messagesContent(BuildLeadDetectionMessages(in)), "\n")
		if !strings.Contains(joined, "hub:7") || !strings.Contains(joined, "hub:8") {
			t.Fatalf("跨业务 SessionID 的 conversation 历史未合并: %s", joined)
		}
	})

	t.Run("G4 group cross-message context", func(t *testing.T) {
		view := g1g8View()
		view.Scope.IsGroup = true
		view.Scope.GroupID = "group-1"
		view.Scope.ConversationID = "thread-9"
		view.Provenance.VisibilityProof = "group_audienceVerified"
		task := LeadReactTask{CurrentMessage: "群里继续聊", ContextView: view, Scope: &view.Scope}
		req := leadReactDispatchRequest(task, "基础提示", []llm.ChatMessage{{Role: "user", Content: "继续"}}, "round-0")
		if req.ExecutionScenarioID != string(agentcontext.ExecutionScenarioLeadReact) {
			t.Fatalf("ReAct 执行场景错误: %q", req.ExecutionScenarioID)
		}
		if req.LLMSessionRef != "llmsess_root" || req.RequestIdempotencyKey == "" {
			t.Fatalf("canonical session/幂等键缺失: %#v", req)
		}
		if !strings.Contains(req.SystemPrompt, "A 产品") {
			t.Fatalf("群聊上下文块未进入 system prompt: %s", req.SystemPrompt)
		}
	})

	t.Run("G5 group audience filter and output gate", func(t *testing.T) {
		view := g1g8View()
		view.Scope.IsGroup = true
		view.Scope.GroupID = "group-1"
		view.Provenance.VisibilityProof = "group_audienceVerified"
		blocked := CheckGroupReplyGate(true, view, "buyer-1", "您的订单 ORD-2026-11 已安排")
		if blocked.Allowed {
			t.Fatalf("群聊输出隐私守卫未拦截订单号: %#v", blocked)
		}
		unverified := CheckGroupReplyGate(true, &agentcontext.AgentConversationContext{}, "buyer-1", "普通回复")
		if unverified.Allowed || len(unverified.Reasons) == 0 {
			t.Fatalf("缺 audience 证明的群回复必须 fail-closed: %#v", unverified)
		}
	})

	t.Run("G6 same bus key isolates execution scenarios", func(t *testing.T) {
		view := g1g8View()
		intentRef, intentKey := intentDispatchCallContext(view, string(agentcontext.ExecutionScenarioIntent), "recognize")
		reactRef, reactKey := intentDispatchCallContext(view, string(agentcontext.ExecutionScenarioLeadReact), "round-0")
		if intentRef != reactRef {
			t.Fatalf("canonical bus key 应复用: %q vs %q", intentRef, reactRef)
		}
		if intentKey == reactKey {
			t.Fatalf("不同执行场景的幂等键不得相同: %q", intentKey)
		}
	})

	t.Run("G7 checked output rejects bad model results", func(t *testing.T) {
		truncated := llm.GenerateResult{Content: "部分", FinishReason: "length"}
		if _, err := llm.CheckGenerateResult(truncated); err == nil {
			t.Fatal("finish_reason=length 必须拒绝")
		}
		err := llm.ValidateAllowedEvidenceIDs("证据 hub:999", map[string]bool{"hub:123": true})
		if err == nil {
			t.Fatal("未授权证据 ID 必须拒绝")
		}
	})

	t.Run("G8 long input and output budget clamp", func(t *testing.T) {
		budget, err := llm.NewContextBudget(llm.ProviderConfig{
			ContextWindow: 131072, MaxInputTokens: 120000, MaxOutputTokens: 8192,
		}, 1024)
		if err != nil {
			t.Fatalf("预算创建失败: %v", err)
		}
		if budget.EffectiveInput > 120000 || budget.EffectiveInput <= 0 {
			t.Fatalf("输入预算未按能力夹取: %#v", budget)
		}
		if got := budget.ClampOutput(99999); got > 8192 || got <= 0 {
			t.Fatalf("输出预算未按能力夹取: %d", got)
		}
	})
}

func messagesContent(messages []llm.ChatMessage) []string {
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		out = append(out, m.Content)
	}
	return out
}
