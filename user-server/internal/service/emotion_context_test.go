package service

import (
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
)

func emotionContextView(recent ...string) *agentcontext.AgentConversationContext {
	at := time.Date(2026, 11, 27, 11, 0, 0, 0, time.UTC)
	msgs := make([]agentcontext.ContextMessage, 0, len(recent))
	for i, content := range recent {
		msgs = append(msgs, agentcontext.ContextMessage{
			MessageID: "hub:" + string(rune('a'+i)), SenderID: "u1", Role: "user",
			Content: content, SentAt: at.Add(time.Duration(i) * time.Minute),
			Visibility: agentcontext.VisibilityPublic,
		})
	}
	return &agentcontext.AgentConversationContext{
		SnapshotID: "snapshot-emotion",
		Scope: agentcontext.ConversationScope{
			TenantID: 7, Platform: "telegram", ChannelAccountID: 1,
			ConversationID: "conv-1", BusLLMSessionID: "llmsess_bus_1",
		},
		Current: agentcontext.ContextMessage{
			MessageID: "hub:z", SenderID: "u1", Content: "那现在怎么办？",
			SentAt: at.Add(time.Hour), Visibility: agentcontext.VisibilityPublic,
		},
		Recent: msgs,
	}
}

func TestClassifyEmotionWithContextUsesTrajectory(t *testing.T) {
	view := emotionContextView("你们是骗子，我要退款")
	if got := ClassifyEmotion(view.Current.Content); got != EmotionNeutral {
		t.Fatalf("当轮文本单独判断应为中性, got %s", got)
	}
	if got := ClassifyEmotionWithContext(view.Current.Content, view); got != EmotionAnger {
		t.Fatalf("近期愤怒轨迹应升级情绪, got %s", got)
	}
}

func TestClassifyEmotionWithContextRespectsDeEscalation(t *testing.T) {
	view := emotionContextView("你们是骗子，我要退款")
	view.Current.Content = "算了，不用退款了"
	if got := ClassifyEmotionWithContext(view.Current.Content, view); got != EmotionNeutral {
		t.Fatalf("当前明确撤回不满时不得沿用旧轨迹, got %s", got)
	}
}

func TestMatchTransferKeywordsWithContext(t *testing.T) {
	view := emotionContextView("我要转人工")
	view.Current.Content = "嗯"
	if !MatchTransferKeywordsWithContext(view.Current.Content, view) {
		t.Fatal("近期明确转人工请求应被保留")
	}
	view.Current.Content = "不用转人工了"
	if MatchTransferKeywordsWithContext(view.Current.Content, view) {
		t.Fatal("当前撤回转人工请求时不得继续升级")
	}
}

func TestObjectionContextRequestBindsScenario(t *testing.T) {
	view := emotionContextView("价格有点高")
	req := objectionContextDispatchRequest("太贵了", view)
	if req.ExecutionScenarioID != "objection" {
		t.Fatalf("异议识别应使用独立执行场景, got %q", req.ExecutionScenarioID)
	}
	if req.LLMSessionRef != "llmsess_bus_1" {
		t.Fatalf("canonical session 应贯通, got %q", req.LLMSessionRef)
	}
	if req.RequestIdempotencyKey != llm.BuildRequestIdempotencyKey(view.Current.MessageID, "objection", "classify") {
		t.Fatalf("幂等键应绑定消息与执行场景, got %q", req.RequestIdempotencyKey)
	}
	if req.Scenario != llm.ScenarioObjection {
		t.Fatalf("路由档位应保持异议场景, got %s", req.Scenario)
	}
}
