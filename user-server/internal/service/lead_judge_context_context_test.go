package service

import (
	"strings"
	"testing"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
)

func testLeadJudgeView() *agentcontext.AgentConversationContext {
	return &agentcontext.AgentConversationContext{
		SnapshotID: "snap-judge",
		Scope: agentcontext.ConversationScope{
			TenantID:         7,
			Platform:         "douyin",
			ChannelAccountID: 11,
			BusLLMSessionID:  "llmsess-canonical",
			IsGroup:          true,
			GroupID:          "group-1",
		},
		Current: agentcontext.ContextMessage{
			MessageID: "hub:100", SenderID: "u1", Content: "谁有现货？", Visibility: agentcontext.VisibilityPublic,
		},
		Recent: []agentcontext.ContextMessage{
			{MessageID: "hub:99", SenderID: "u2", Content: "我有工厂现货", Visibility: agentcontext.VisibilityPublic},
			{MessageID: "hub:98", SenderID: "u3", Content: "我的报价是 100", Visibility: agentcontext.VisibilityPrivate},
		},
	}
}

func TestFilterLeadJudgeMessagesByContext(t *testing.T) {
	msgs := []DetectionMessage{
		{MessageID: "hub:99", Content: "我有工厂现货", SenderID: "u2", Visibility: agentcontext.VisibilityPublic},
		{MessageID: "hub:98", Content: "我的报价是 100", SenderID: "u3", Visibility: agentcontext.VisibilityPrivate},
		{MessageID: "hub:100", Content: "谁有现货？", SenderID: "u1", Visibility: agentcontext.VisibilityPublic},
	}
	got := filterLeadJudgeMessages(msgs, testLeadJudgeView())
	if len(got) != 2 || got[0].MessageID != "hub:99" || got[1].MessageID != "hub:100" {
		t.Fatalf("必须按 audience 过滤私有消息，got %+v", got)
	}
}

func TestLeadJudgeMessagesKeepRealEvidenceIDs(t *testing.T) {
	jctx := &LeadJudgeContext{
		Messages: []DetectionMessage{
			{MessageID: "hub:99", Content: "我有工厂现货", SenderID: "u2", Role: "user"},
			{MessageID: "hub:100", Content: "谁有现货？", SenderID: "u1", Role: "user"},
		},
	}
	msgs := leadJudgeMessagesWithEvidence(jctx)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "hub:99") || !strings.Contains(msgs[1].Content, "hub:100") {
		t.Fatalf("必须保留真实 MessageID 证据锚点: %+v", msgs)
	}
}

func TestLeadJudgeDispatchRequestBindsScenario(t *testing.T) {
	jctx := &LeadJudgeContext{
		ContextView: testLeadJudgeView(),
		Messages:    []DetectionMessage{{MessageID: "hub:100", Content: "谁有现货？"}},
	}
	msgs := leadJudgeMessagesWithEvidence(jctx)
	req := leadJudgeDispatchRequest(jctx, msgs)
	if req.ExecutionScenarioID != string(agentcontext.ExecutionScenarioLeadJudge) {
		t.Fatalf("got scenario %q", req.ExecutionScenarioID)
	}
	if req.LLMSessionRef != "llmsess-canonical" || req.RequestIdempotencyKey == "" {
		t.Fatalf("must bind canonical session/idempotency: %+v", req)
	}
}

func TestLeadJudgePromptDowngradesLegacySharedNotes(t *testing.T) {
	jctx := &LeadJudgeContext{SharedBusiness: SharedBusinessInput{
		Notes: []BusinessNote{{Source: "legacy", At: 1, Attribute: BusinessNoteLegacyUnverified, Text: "客户已付款"}},
	}}
	prompt := buildLeadJudgeSystemPrompt(jctx)
	if !strings.Contains(prompt, "待核实") {
		t.Fatalf("legacy note must be visibly downgraded: %q", prompt)
	}
	if strings.Contains(prompt, "属性=既成事实") {
		t.Fatalf("legacy note must never be stated as confirmed fact: %q", prompt)
	}
}

func TestFetchHistoryDetectionMessagesUsesRealIDs(t *testing.T) {
	// 使用纯函数验证转换契约；真实 DB 查询在 T013/T016 已标 not-run。
	rows := []model.MessageHub{
		{ID: 12, MsgID: "real-12", Content: "我有工厂现货", SenderID: "u2", Direction: "inbound"},
		{ID: 13, MsgID: "real-13", Content: "报价 100", SenderID: "bot", Direction: "outbound"},
	}
	got := messageHubsToDetectionMessages(rows, false)
	if len(got) != 2 || got[0].MessageID != "hub:12" || got[1].MessageID != "hub:13" {
		t.Fatalf("must use real DB IDs: %+v", got)
	}
}
