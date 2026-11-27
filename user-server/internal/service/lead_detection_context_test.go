package service

import (
	"strings"
	"testing"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
)

func testLeadDetectionView() *agentcontext.AgentConversationContext {
	return &agentcontext.AgentConversationContext{
		SnapshotID: "snap-1",
		Scope: agentcontext.ConversationScope{
			TenantID:         7,
			Platform:         "douyin",
			ChannelAccountID: 11,
			BusSessionID:     "channel:douyin:11:conv-1",
			BusLLMSessionID:  "llmsess-canonical",
			SessionID:        "session-1",
			ConversationID:   "conv-1",
			IsGroup:          true,
			GroupID:          "group-1",
		},
		Current: agentcontext.ContextMessage{
			MessageID:  "hub:100",
			Role:       "user",
			SenderID:   "u1",
			SenderName: "张三",
			Content:    "那个多少钱？",
			Visibility: agentcontext.VisibilityPublic,
		},
		Recent: []agentcontext.ContextMessage{
			{
				MessageID:  "hub:99",
				Role:       "assistant",
				SenderID:   "bot",
				Content:    "聊的是 A 产品的报价",
				Visibility: agentcontext.VisibilityPublic,
			},
			{
				MessageID:  "hub:98",
				Role:       "user",
				SenderID:   "u2",
				Content:    "我的手机号是 13800138000",
				Visibility: agentcontext.VisibilityPrivate,
			},
		},
	}
}

func TestBuildLeadDetectionMessagesKeepsRealEvidenceAndFiltersPrivate(t *testing.T) {
	view := testLeadDetectionView()
	in := DetectionInput{
		Messages: []DetectionMessage{
			{MessageID: "hub:99", Content: "聊的是 A 产品的报价", Role: "assistant", SenderID: "bot"},
			{MessageID: "hub:98", Content: "我的手机号是 13800138000", Role: "user", SenderID: "u2"},
			{MessageID: "hub:100", Content: "那个多少钱？", Role: "user", SenderID: "u1"},
		},
		ContextView: view,
	}
	msgs := BuildLeadDetectionMessages(in)
	if len(msgs) != 2 {
		t.Fatalf("应过滤不可证明可见的历史，got %d: %+v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0].Content, "hub:99") || !strings.Contains(msgs[1].Content, "hub:100") {
		t.Fatalf("必须保留真实 MessageID 作为证据锚点: %+v", msgs)
	}
	joined := msgs[0].Content + msgs[1].Content
	if strings.Contains(joined, "13800138000") || strings.Contains(joined, "hub:98") {
		t.Fatalf("私有成员消息不得进入模型输入: %q", joined)
	}
	if strings.Contains(joined, "聊的是 A 产品的报价（指代") {
		t.Fatalf("历史不应重复注入指代补全内容")
	}
}

func TestLeadDetectionContextSignals(t *testing.T) {
	view := testLeadDetectionView()
	in := DetectionInput{Messages: []DetectionMessage{{MessageID: "hub:100", Content: "那个多少钱？"}}, ContextView: view}
	signals := LeadDetectionContextSignals(in)
	if !strings.Contains(signals.ResolvedText, "A 产品") {
		t.Fatalf("指代应被近期主题补全, got %q", signals.ResolvedText)
	}
	if got := leadDetectionResolvedContent(in, in.Messages[0]); !strings.Contains(got, "A 产品") {
		t.Fatalf("规则快路径也必须使用补全文本, got %q", got)
	}
}

func TestLeadDetectionDispatchRequestBindsScenarioAndSession(t *testing.T) {
	in := DetectionInput{ContextView: testLeadDetectionView(), Messages: []DetectionMessage{{MessageID: "hub:100", Content: "那个多少钱？"}}}
	req := leadDetectionDispatchRequest(in, "system", []llm.ChatMessage{{Role: "user", Content: "[msg id=hub:100] 那个多少钱？"}})
	if req.ExecutionScenarioID != string(agentcontext.ExecutionScenarioLeadDetect) {
		t.Fatalf("必须使用 lead_detect 执行场景, got %q", req.ExecutionScenarioID)
	}
	if req.LLMSessionRef != "llmsess-canonical" {
		t.Fatalf("必须携带 canonical bus session key, got %q", req.LLMSessionRef)
	}
	if req.RequestIdempotencyKey == "" {
		t.Fatal("必须带请求幂等键")
	}
	if !strings.Contains(req.SystemPrompt, "群聊") {
		t.Fatalf("群聊来源策略必须进入 prompt, got %q", req.SystemPrompt)
	}
}

func TestValidateDetectionEvidenceIDsRejectsHallucination(t *testing.T) {
	allowed := map[string]struct{}{"hub:99": {}, "hub:100": {}}
	got := validateDetectionEvidenceIDs([]string{"hub:100", "hub:999", "hub:99"}, allowed)
	if len(got) != 2 || got[0] != "hub:100" || got[1] != "hub:99" {
		t.Fatalf("只能保留输入中真实存在的证据 ID, got %v", got)
	}
}

func TestLeadDetectionKeyPathUsesResolvedText(t *testing.T) {
	rule := &model.LeadMiningRule{Keywords: model.JSONStrings{"A 产品"}}
	in := DetectionInput{
		Rule:        rule,
		Messages:    []DetectionMessage{{MessageID: "hub:100", Content: "那个多少钱？"}},
		ContextView: testLeadDetectionView(),
	}
	hit := NewLeadKeywordDetector().Detect(in)
	if hit.Payload["match_count"] != 1 {
		t.Fatalf("补全后应命中 A 产品关键词, payload=%v", hit.Payload)
	}
}
