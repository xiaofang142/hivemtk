package service

import (
	"strings"
	"testing"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
)

func TestHumanizeContextAwareDispatchRequest(t *testing.T) {
	view := &agentcontext.AgentConversationContext{
		SnapshotID: "snap-humanize",
		Scope: agentcontext.ConversationScope{
			TenantID:         7,
			Platform:         "wecom",
			ChannelAccountID: 11,
			ConversationID:   "conv-humanize",
			BusLLMSessionID:  "llmsess_abc",
		},
		Current: agentcontext.ContextMessage{MessageID: "hub:9", Content: "这个还来得及吗"},
	}
	call := humanizeLLMCallContext(
		agentcontext.ExecutionScenarioHumanizeEval,
		"evaluate",
		view,
	)
	req := humanizeContextAwareDispatchRequest("评估这段回复", call)
	if req.ExecutionScenarioID != string(agentcontext.ExecutionScenarioHumanizeEval) {
		t.Fatalf("拟人评估必须使用独立执行场景, got %q", req.ExecutionScenarioID)
	}
	if req.Scenario != llm.ScenarioObjection {
		t.Fatalf("路由档位仍应为 objection, got %q", req.Scenario)
	}
	if req.LLMSessionRef != "llmsess_abc" {
		t.Fatalf("总线 session 应作为续接键, got %q", req.LLMSessionRef)
	}
	wantID := llm.BuildRequestIdempotencyKey("hub:9", string(agentcontext.ExecutionScenarioHumanizeEval), "evaluate")
	if req.RequestIdempotencyKey != wantID {
		t.Fatalf("幂等键应含消息+场景+步骤, got %q want %q", req.RequestIdempotencyKey, wantID)
	}

	rewriteReq := humanizeContextAwareDispatchRequest(
		"重新生成",
		humanizeLLMCallContext(agentcontext.ExecutionScenarioHumanizeRewrite, "regenerate", view),
	)
	if rewriteReq.ExecutionScenarioID != string(agentcontext.ExecutionScenarioHumanizeRewrite) {
		t.Fatalf("重生必须使用独立执行场景, got %q", rewriteReq.ExecutionScenarioID)
	}
	if rewriteReq.RequestIdempotencyKey == req.RequestIdempotencyKey {
		t.Fatalf("评估与重生不得共享幂等键")
	}
}

func TestHumanizeRegeneratePromptIncludesContextView(t *testing.T) {
	view := &agentcontext.AgentConversationContext{
		SnapshotID: "snap-humanize",
		Scope: agentcontext.ConversationScope{
			TenantID: 7, Platform: "wecom", ChannelAccountID: 11,
			IsGroup: true, GroupID: "group-1", BusLLMSessionID: "llmsess_abc",
		},
		Current: agentcontext.ContextMessage{MessageID: "hub:9", SenderID: "u-1", Content: "这个还来得及吗", Visibility: agentcontext.VisibilityPublic},
		Recent: []agentcontext.ContextMessage{{
			MessageID: "hub:7", SenderID: "u-1", Content: "之前说预算是 5000",
			Visibility: agentcontext.VisibilityPublic,
		}},
	}
	input := &dto.HumanizeEvalInput{CustomerMessage: "这个还来得及吗", AIReply: "可以的", ContextView: view}
	prompt := buildHumanizeRegeneratePrompt(input, nil)
	if !strings.Contains(prompt, "之前说预算是 5000") {
		t.Fatalf("重生 prompt 必须带多轮上下文: %q", prompt)
	}
	if !strings.Contains(prompt, "群聊") {
		t.Fatalf("重生 prompt 必须区分单聊/群聊: %q", prompt)
	}
}
