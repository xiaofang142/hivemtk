package service

import (
	"strings"
	"testing"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
)

func goldenReplyChainView(tenantID uint, busSession string) *agentcontext.AgentConversationContext {
	return &agentcontext.AgentConversationContext{
		SnapshotID: "snap-golden-" + busSession,
		Scope: agentcontext.ConversationScope{
			TenantID: tenantID, Platform: "wecom", ChannelAccountID: 11,
			ConversationID: "conv-1", BusLLMSessionID: busSession,
		},
		Current: agentcontext.ContextMessage{
			MessageID: "hub:9", Role: "user", SenderID: "u-1", Content: "就按上次说的",
			Visibility: agentcontext.VisibilityPublic,
		},
		Recent: []agentcontext.ContextMessage{
			{MessageID: "hub:1", Role: "user", SenderID: "u-1", Content: "聊的是 A 款，预算 5000", Visibility: agentcontext.VisibilityPublic},
			{MessageID: "hub:2", Role: "assistant", Content: "好的，按 A 款给您做方案", Visibility: agentcontext.VisibilityPublic},
			{MessageID: "hub:3", Role: "user", SenderID: "u-1", Content: "不是这个，是另一个：B 款", Visibility: agentcontext.VisibilityPublic},
		},
		SharedBusiness: []agentcontext.BusinessFact{
			{Key: "用途", Value: "客户已确认用途是送礼", Grade: agentcontext.FactConfirmed, SubjectType: "dm_person", SubjectID: "u-1"},
			{Key: "报价", Value: "建议报低价", Grade: agentcontext.FactSuggestion, SubjectType: "dm_person", SubjectID: "u-1"},
		},
	}
}

func TestReplyChainGoldenS1ToS8(t *testing.T) {
	view := goldenReplyChainView(7, "llmsess_golden")

	t.Run("S1 多轮指代", func(t *testing.T) {
		signals := BuildIntentContextSignals("就按上次说的", view)
		if !strings.Contains(signals.ResolvedText, "指代：") {
			t.Fatalf("延续表达必须补全指代: %+v", signals)
		}
	})
	t.Run("S2 多轮纠正", func(t *testing.T) {
		signals := BuildIntentContextSignals("不是这个，是另一个：B 款", view)
		if len(signals.RecentCorrections) == 0 {
			t.Fatalf("必须提取纠正信号: %+v", signals)
		}
		if !signals.Conflict {
			t.Fatalf("与历史纠正冲突时必须标记: %+v", signals)
		}
	})
	t.Run("S3 用户/线索事实分级", func(t *testing.T) {
		block := BuildIntentContextBlock(view)
		if !strings.Contains(block, "客户已确认用途是送礼") {
			t.Fatalf("confirmed 事实应进入上下文: %q", block)
		}
		if strings.Contains(block, "建议报低价") {
			t.Fatalf("suggestion 不得复述为既成事实: %q", block)
		}
	})
	t.Run("S4 canonical session 与场景隔离", func(t *testing.T) {
		req := &SalesRequest{SessionID: "session-old", MessageID: "hub:9", BusLLMSessionID: "llmsess_golden", ContextView: view, Config: dto.DefaultSalesEngineConfig()}
		salesRef, _ := salesGenerateDispatchCallContext(req, "generate")
		intentRef, _ := intentDispatchCallContext(view, string(agentcontext.ExecutionScenarioIntent), "classify")
		if salesRef != "llmsess_golden" || intentRef != "llmsess_golden" {
			t.Fatalf("同一总线 key 必须端到端复用: sales=%q intent=%q", salesRef, intentRef)
		}
		salesReq := salesGeneratePlainDispatchRequest(req, llm.ScenarioSOPReply, "p", "s")
		if salesReq.ExecutionScenarioID == string(agentcontext.ExecutionScenarioIntent) {
			t.Fatalf("意图与销售不得共享执行场景")
		}
	})
	t.Run("S5a prompt cache 前缀稳定", func(t *testing.T) {
		stable := llm.CanonicalPromptRequest{
			Model:           "gpt-test",
			SystemPrompt:    "stable system",
			StableMessages:  []llm.CanonicalMessage{{Role: "system", Content: "stable"}},
			DynamicMessages: []llm.CanonicalMessage{{Role: "user", Content: "turn-1"}},
		}
		first := stable.PrefixHash()
		stable.DynamicMessages[0].Content = "turn-2"
		if first != stable.PrefixHash() {
			t.Fatalf("动态尾变化不得改变前缀 hash")
		}
		if first == "" {
			t.Fatalf("前缀 hash 不应为空")
		}
	})
	t.Run("S5b 个性化请求不走最终答案缓存", func(t *testing.T) {
		req := &SalesRequest{SessionID: "session-old", MessageID: "hub:9", BusLLMSessionID: "llmsess_golden", ContextView: view, Config: dto.DefaultSalesEngineConfig()}
		dispatch := salesGeneratePlainDispatchRequest(req, llm.ScenarioSOPReply, "p", "s")
		if dispatch.CacheKey != "" || dispatch.CacheTTL != 0 {
			t.Fatalf("个性化多轮不得复用最终答案: %+v", dispatch)
		}
	})
	t.Run("S6 来源策略", func(t *testing.T) {
		block := BuildIntentContextBlock(view)
		if !strings.Contains(block, "单聊") || strings.Contains(block, "群聊") {
			t.Fatalf("DM 必须使用单聊策略: %q", block)
		}
	})
	t.Run("S7 跨租户隔离", func(t *testing.T) {
		a, err := agentcontext.DeriveScopeIdentity(goldenReplyChainView(7, "llmsess_a").Scope)
		if err != nil {
			t.Fatalf("derive A: %v", err)
		}
		b, err := agentcontext.DeriveScopeIdentity(goldenReplyChainView(8, "llmsess_b").Scope)
		if err != nil {
			t.Fatalf("derive B: %v", err)
		}
		if a.ScopeHash == b.ScopeHash || a.BusLLMSessionID == b.BusLLMSessionID {
			t.Fatalf("跨租户不得共享 scope/session: %+v vs %+v", a, b)
		}
	})
	t.Run("S8 输入/输出分层与长输出预算", func(t *testing.T) {
		history := salesGenerateHistoryMessages(view)
		if len(history) == 0 || !strings.Contains(history[0].Content, "hub:1") {
			t.Fatalf("跨 Session 历史与真实消息ID必须进入模型输入: %+v", history)
		}
		budget, err := llm.NewContextBudget(llm.ProviderConfig{ContextWindow: 131072, MaxOutputTokens: 8192}, 0)
		if err != nil {
			t.Fatalf("budget: %v", err)
		}
		if got := budget.ClampOutput(999999); got != 8192 {
			t.Fatalf("长输出必须按 provider 能力夹取, got %d", got)
		}
	})
}
