package service

import (
	"strings"
	"testing"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
)

func salesContextTestRequest() *SalesRequest {
	return &SalesRequest{
		SessionID:       "session-1",
		CustomerID:      "c-1",
		MessageID:       "hub:9",
		UserMessage:     "这个还来得及吗",
		Platform:        "wecom",
		BusLLMSessionID: "llmsess_abc",
		ContextView: &agentcontext.AgentConversationContext{
			SnapshotID: "snap-1",
			Scope: agentcontext.ConversationScope{
				TenantID: 7, Platform: "wecom", ChannelAccountID: 11,
				BusLLMSessionID: "llmsess_abc",
			},
			Current: agentcontext.ContextMessage{
				MessageID: "hub:9", Role: "user", Content: "这个还来得及吗",
				Visibility: agentcontext.VisibilityPublic,
			},
			Recent: []agentcontext.ContextMessage{
				{MessageID: "hub:1", Role: "user", SenderID: "u-1", Content: "之前说预算是 5000", Visibility: agentcontext.VisibilityPublic},
				{MessageID: "hub:2", Role: "assistant", Content: "好的，我帮您看看", Visibility: agentcontext.VisibilityPublic},
				{MessageID: "hub:3", Role: "user", SenderID: "u-2", Content: "私人手机号 13800138000", Visibility: agentcontext.VisibilityPrivate},
			},
		},
	}
}

func TestSalesGenerateDispatchCallContext(t *testing.T) {
	req := salesContextTestRequest()
	sessionRef, idem := salesGenerateDispatchCallContext(req, "generate")
	if sessionRef != "llmsess_abc" {
		t.Fatalf("总线 session 应作为续接键, got %q", sessionRef)
	}
	want := llm.BuildRequestIdempotencyKey("hub:9", string(agentcontext.ExecutionScenarioSalesGenerate), "generate")
	if idem != want {
		t.Fatalf("幂等键错误, got %q want %q", idem, want)
	}
}

func TestSalesGenerateHistoryUsesVisibleUnifiedMessages(t *testing.T) {
	msgs := salesGenerateHistoryMessages(salesContextTestRequest().ContextView)
	if len(msgs) != 2 {
		t.Fatalf("仅保留可证明可见的历史, got %d: %+v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0].Content, "hub:1") || !strings.Contains(msgs[0].Content, "之前说预算是 5000") {
		t.Fatalf("历史应保留消息ID证据: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" {
		t.Fatalf("出站历史应映射 assistant, got %+v", msgs[1])
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "13800138000") {
			t.Fatalf("私有历史不得进入模型: %+v", m)
		}
	}
}

func TestSalesGeneratePlainRequestSkipsAnswerCacheWithContext(t *testing.T) {
	req := salesContextTestRequest()
	withCtx := salesGeneratePlainDispatchRequest(req, llm.ScenarioSOPReply, "prompt", "system")
	if withCtx.CacheKey != "" || withCtx.CacheTTL != 0 {
		t.Fatalf("个性化多轮不得走最终答案缓存: %+v", withCtx)
	}
	if withCtx.ExecutionScenarioID != string(agentcontext.ExecutionScenarioSalesGenerate) {
		t.Fatalf("必须使用 sales_generate 执行场景, got %q", withCtx.ExecutionScenarioID)
	}
	if withCtx.LLMSessionRef != "llmsess_abc" {
		t.Fatalf("必须携带 canonical session, got %q", withCtx.LLMSessionRef)
	}
	req.ContextView = nil
	req.BusLLMSessionID = ""
	legacy := salesGeneratePlainDispatchRequest(req, llm.ScenarioSOPReply, "prompt", "system")
	if legacy.CacheKey == "" {
		t.Fatalf("无上下文的纯知识请求保留既有答案缓存快路")
	}
}

func TestSalesGenerateRetryBudgetOnTruncation(t *testing.T) {
	next, done := salesGenerateRetryMaxTokens(1024, "length", "被截断的回复", false)
	if done || next != 2048 {
		t.Fatalf("截断输出应加倍预算重试, next=%d done=%v", next, done)
	}
	_, done = salesGenerateRetryMaxTokens(1024, "length", "被截断的回复", true)
	if !done {
		t.Fatalf("只允许一次重预算重试")
	}
	_, done = salesGenerateRetryMaxTokens(1024, "stop", "完整回复", false)
	if !done {
		t.Fatalf("正常完成不应重试")
	}
}
