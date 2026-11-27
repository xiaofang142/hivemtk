package service

import (
	"context"
	"strings"
	"testing"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func reactTestView() *agentcontext.AgentConversationContext {
	return &agentcontext.AgentConversationContext{
		SnapshotID: "snap-1",
		Scope: agentcontext.ConversationScope{
			TenantID:         7,
			Platform:         "telegram",
			ChannelAccountID: 9,
			BusLLMSessionID:  "llmsess-react-1",
			ConversationID:   "conv-1",
		},
		Current: agentcontext.ContextMessage{MessageID: "hub:42", SenderID: "u1", Visibility: agentcontext.VisibilityPublic, Content: "这方案不行"},
		Recent: []agentcontext.ContextMessage{
			{MessageID: "hub:41", SenderID: "u1", Visibility: agentcontext.VisibilityPublic, Content: "我要做贷款"},
		},
		SharedBusiness: []agentcontext.BusinessFact{
			{Key: "贷款需求", Value: "客户已确认有贷款需求", Grade: agentcontext.FactConfirmed, SubjectType: "dm_person"},
			{Key: "推荐建议", Value: "建议推荐高额度", Grade: agentcontext.FactSuggestion, SubjectType: "dm_person"},
		},
	}
}

func TestLeadReactDispatchRequestBindsScenarioAndSession(t *testing.T) {
	task := LeadReactTask{ContextView: reactTestView()}
	req := leadReactDispatchRequest(task, "系统", []llm.ChatMessage{{Role: "user", Content: "hi"}}, "round-1")
	if req.ExecutionScenarioID != string(agentcontext.ExecutionScenarioLeadReact) {
		t.Fatalf("执行场景必须是 lead_react, got %q", req.ExecutionScenarioID)
	}
	if req.LLMSessionRef != "llmsess-react-1" {
		t.Fatalf("必须携带 canonical bus session, got %q", req.LLMSessionRef)
	}
	if req.RequestIdempotencyKey == "" {
		t.Fatal("必须带幂等键")
	}
	if !strings.Contains(req.SystemPrompt, "客户已确认有贷款需求") || strings.Contains(req.SystemPrompt, "建议推荐高额度") {
		t.Fatalf("上下文块只能包含 confirmed 事实: %q", req.SystemPrompt)
	}
}

func TestLeadReactDispatchRequestWithoutViewStillBindsScenario(t *testing.T) {
	req := leadReactDispatchRequest(LeadReactTask{CurrentMessage: "x"}, "系统", nil, "round-0")
	if req.ExecutionScenarioID != string(agentcontext.ExecutionScenarioLeadReact) {
		t.Fatalf("无 ContextView 也必须绑定 lead_react, got %q", req.ExecutionScenarioID)
	}
}

func TestLeadReactObservationBudgetKeepsLatestAndRecordsOmitted(t *testing.T) {
	observations := []string{strings.Repeat("旧", 3000), strings.Repeat("A", 100), strings.Repeat("B", 100)}
	kept, omitted := leadReactObservationBudget(observations, 250)
	if len(kept) != 2 || kept[0] != observations[1] || kept[1] != observations[2] {
		t.Fatalf("必须保留最新且满足总预算, kept=%v", kept)
	}
	if len(omitted) != 1 || !strings.Contains(omitted[0], "0") {
		t.Fatalf("必须记录省略原因, omitted=%v", omitted)
	}
}

func TestBuildLeadReactToolInvocationRequiresIdempotencyForHighWrite(t *testing.T) {
	read := buildLeadReactToolInvocation(LeadReactTask{
		TenantID: 7, Platform: "telegram", SourceRef: "acc-1", ScopeValid: true,
		ConversationID: "conv-1", CurrentMessage: "帮我查客户", ContextView: reactTestView(),
	}, "search_customer", "张", "step-1")
	if read.RiskClass != model.ToolRiskRead || read.ToolIdempotencyKey == "" {
		t.Fatalf("读工具也应有稳定幂等键, got %+v", read)
	}

	write := buildLeadReactToolInvocation(LeadReactTask{
		TenantID: 7, Platform: "telegram", SourceRef: "acc-1", ScopeValid: true,
		ConversationID: "conv-1", CurrentMessage: "发私信", ContextView: reactTestView(),
	}, "send_private_message", "张", "step-2")
	if write.RiskClass != model.ToolRiskHighWrite || write.ToolIdempotencyKey == "" || write.Status != model.ToolInvocationPlanned {
		t.Fatalf("高写工具必须有幂等键且初始状态 planned, got %+v", write)
	}
	if write.RequestHash == "" || write.AuthorizationHash == "" {
		t.Fatal("必须记录请求与授权 hash")
	}
}

type fakeLeadReactLedger struct {
	planned   []string
	settled   []string
	planErr   error
	settleErr error
}

func (f *fakeLeadReactLedger) Plan(_ context.Context, entry *model.ToolInvocation) error {
	if f.planErr != nil {
		return f.planErr
	}
	f.planned = append(f.planned, entry.ToolName+":"+entry.ToolIdempotencyKey)
	return nil
}

func (f *fakeLeadReactLedger) Settle(_ context.Context, entry *model.ToolInvocation, status, _ string) error {
	if f.settleErr != nil {
		return f.settleErr
	}
	f.settled = append(f.settled, entry.ToolName+":"+status)
	return nil
}

func TestRunLeadReactLoopUsesLedgerAndContextDispatch(t *testing.T) {
	ledger := &fakeLeadReactLedger{}
	task := LeadReactTask{
		TenantID: 7, Platform: "telegram", SourceRef: "acc-1", ScopeValid: true,
		CanonicalAccountID: 9, ChannelID: 9, AccountIDs: []string{"9"},
		ConversationID: "conv-1", CurrentMessage: "查客户",
		ContextView: reactTestView(), Ledger: ledger,
	}
	dispatchCalls := 0
	dispatch := func(_ context.Context, req llm.DispatchRequest) (*llm.DispatchResult, error) {
		dispatchCalls++
		if req.ExecutionScenarioID != string(agentcontext.ExecutionScenarioLeadReact) {
			t.Fatalf("每轮必须用 lead_react 场景, got %q", req.ExecutionScenarioID)
		}
		if req.RequestIdempotencyKey == "" {
			t.Fatal("每轮必须带幂等键")
		}
		if dispatchCalls == 1 {
			return &llm.DispatchResult{Content: "Action: search_customer\nAction Input: {\"query\":\"张\"}"}, nil
		}
		return &llm.DispatchResult{Content: "Final Answer: 已找到"}, nil
	}
	tool := LeadReactTool{Name: "search_customer", Description: "查询", Run: func(_ context.Context, _ LeadReactTask, _ string) (string, error) {
		return "找到客户", nil
	}}
	outcome := runLeadReactLoop(tenant.WithTenantID(context.Background(), 7), task, []LeadReactTool{tool}, dispatch)
	if outcome.Status != leadReactStatusOK {
		t.Fatalf("期望成功, got %+v", outcome)
	}
	if len(ledger.planned) != 1 || len(ledger.settled) != 1 || ledger.settled[0] != "search_customer:confirmed" {
		t.Fatalf("工具调用必须走台账, planned=%v settled=%v", ledger.planned, ledger.settled)
	}
}
