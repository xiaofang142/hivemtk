package service

import (
	"context"
	"math"
	"strings"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

type answerScopeTools struct{}

func (answerScopeTools) ListTools(context.Context) []AgentToolDef {
	return []AgentToolDef{{Name: "order.create", Description: "创建订单"}}
}

func answerScopeReusableResponse() *SalesResponse {
	return &SalesResponse{
		Reply:       "这是一条完整、可核实的知识答案。",
		Intent:      &dto.RecognizeResult{IntentType: IntentGreeting, Confidence: .99},
		Memory:      &dto.DialogueMemory{},
		RAGChunks:   []dto.RAGChunk{{Content: "已发布的知识事实。", Score: .9}},
		LLMProvider: "local-test", LLMModel: "model-test",
		Steps: []dto.SalesStepLog{{Step: "6_generate_candidate", Status: "ok"}},
	}
}

func TestAnswerCacheScope_ResultRequiresCompleteGenerationProof(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*SalesResponse)
	}{
		{name: "模型来源未知", mutate: func(r *SalesResponse) { r.LLMProvider = "" }},
		{name: "模型身份未知", mutate: func(r *SalesResponse) { r.LLMModel = "" }},
		{name: "必要步骤超时", mutate: func(r *SalesResponse) { r.Steps[0].Status = "timeout" }},
		{name: "必要步骤仍在运行", mutate: func(r *SalesResponse) { r.Steps[0].Status = "running" }},
		{name: "降级文案", mutate: func(r *SalesResponse) { r.Degraded = true }},
		{name: "卡片操作", mutate: func(r *SalesResponse) { r.Cards = []dto.RichCard{{Title: "操作卡片"}} }},
		{name: "人工移交", mutate: func(r *SalesResponse) { r.TransferredToHuman = true }},
		{name: "拒答哨兵", mutate: func(r *SalesResponse) { r.Reply = "  no_reply  " }},
		{name: "客户专属条件", mutate: func(r *SalesResponse) { r.Memory.Budget = "专属预算" }},
		{name: "检索分数非法", mutate: func(r *SalesResponse) { r.RAGChunks[0].Score = math.NaN() }},
		{name: "失败后继续生成", mutate: func(r *SalesResponse) { r.Steps[0].Status = "fail_soft" }},
		{name: "业务流程", mutate: func(r *SalesResponse) { r.MatchedSOP = &dto.SOPAgent{} }},
		{name: "分条行为计划", mutate: func(r *SalesResponse) { r.SendPlan = &dto.SendPlanDTO{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := answerScopeReusableResponse()
			if !answerCacheResultReusable(r, .7) {
				t.Fatal("完整纯知识正常对照不能被全部拒绝")
			}
			tc.mutate(r)
			if answerCacheResultReusable(r, .7) {
				t.Fatal("来源未知、未完成或带业务动作的结果不得成为可复用最终答案")
			}
		})
	}
}
func (answerScopeTools) DispatchToolCalls(context.Context, []AgentToolCall, AgentToolContext) []AgentToolResult {
	return nil
}

type answerScopeMemory struct{ fakeMemory }

func (answerScopeMemory) GetOrCreateMemory(context.Context, string, string) (*model.DialogueMemory, error) {
	return &model.DialogueMemory{Budget: "客户专属预算"}, nil
}

type answerScopeSOP struct{ fakeSOP }

func (answerScopeSOP) MatchByIntent(context.Context, string) ([]model.SOPAgent, error) {
	return []model.SOPAgent{{Name: "必须执行的业务流程"}}, nil
}

type answerScopeComplaint struct{ fakeIntentRecognizer }

func (answerScopeComplaint) PreviewIntent(context.Context, string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{IntentType: IntentComplaint, Confidence: .99}, nil
}

func (answerScopeComplaint) Recognize(context.Context, string, string, string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{IntentType: IntentComplaint, Confidence: .99}, nil
}

func TestAnswerCacheScope_ConfigurationAndExecutionProof(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*SmartCSOrchestrator, *IncomingContext, *AgentContext)
		blocked bool
	}{
		{name: "代理身份变化", mutate: func(_ *SmartCSOrchestrator, _ *IncomingContext, a *AgentContext) { a.AgentID++ }},
		{name: "提示变化", mutate: func(_ *SmartCSOrchestrator, _ *IncomingContext, a *AgentContext) {
			a.Persona = "新的完整回答策略"
		}},
		{name: "模型变化", mutate: func(_ *SmartCSOrchestrator, _ *IncomingContext, a *AgentContext) { a.LLMModel = "model-b" }},
		{name: "渠道变化", mutate: func(_ *SmartCSOrchestrator, i *IncomingContext, _ *AgentContext) {
			i.Platform = model.PlatformXiaohongshu
		}},
		{name: "账号变化", mutate: func(_ *SmartCSOrchestrator, i *IncomingContext, _ *AgentContext) { i.AccountID = "another-account" }},
		{name: "运行时工具", blocked: true, mutate: func(o *SmartCSOrchestrator, _ *IncomingContext, _ *AgentContext) {
			o.engine.SetToolExecutor(context.Background(), answerScopeTools{})
		}},
		{name: "必须审核", blocked: true, mutate: func(_ *SmartCSOrchestrator, _ *IncomingContext, a *AgentContext) { a.EnableContentAudit = true }},
		{name: "动态资产", blocked: true, mutate: func(_ *SmartCSOrchestrator, _ *IncomingContext, a *AgentContext) { a.AssetBundleID = "asset-1" }},
		{name: "客户个性事实", blocked: true, mutate: func(o *SmartCSOrchestrator, _ *IncomingContext, _ *AgentContext) {
			o.engine.memory = answerScopeMemory{}
		}},
		{name: "全局流程不能绕过", blocked: true, mutate: func(o *SmartCSOrchestrator, _ *IncomingContext, _ *AgentContext) { o.engine.sop = answerScopeSOP{} }},
		{name: "意图否决不能绕过", blocked: true, mutate: func(o *SmartCSOrchestrator, _ *IncomingContext, _ *AgentContext) {
			o.engine.intent = answerScopeComplaint{}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := cs03NewCacheFixture(t, cs03DetEmbedder{}, 0.9)
			ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
			in := &IncomingContext{Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA, Content: cs03Question}
			session := &model.CustomerSession{TenantID: tenant.DefaultTenantID, Platform: in.Platform, AccountID: in.AccountID,
				SessionID: "scope-session", UserID: cs03CustA}
			agent := &AgentContext{AgentID: fixture.agentID, EnableRAG: true,
				LLMProviderConfig: dto.LLMProviderConfig{APIKey: "scope-secret", Model: "model-a"}}
			before := fixture.orch.answerCacheNamespace(ctx, in, session, agent, "v1")
			if before == "" || before != fixture.orch.answerCacheNamespace(ctx, in, session, agent, "v1") {
				t.Fatal("稳定纯知识配置必须有可重复的正常快路，不可通过全部拒绝伪造隔离")
			}
			tc.mutate(fixture.orch, in, agent)
			if in.Platform != session.Platform || in.AccountID != session.AccountID {
				session.Platform, session.AccountID = in.Platform, in.AccountID
			}
			after := fixture.orch.answerCacheNamespace(ctx, in, session, agent, "v1")
			if tc.blocked {
				if after != "" {
					t.Fatal("可能执行工具、审核或个性化的路线不能复用最终答案")
				}
			} else if after == "" || after == before {
				t.Fatal("不同输出配置不能借同一个最终答案缓存")
			}
			if len(before) != 64 || strings.Contains(before, "scope-secret") {
				t.Fatal("存储键必须是固定长度配置摘要，不能暴露凭证")
			}
		})
	}
}
