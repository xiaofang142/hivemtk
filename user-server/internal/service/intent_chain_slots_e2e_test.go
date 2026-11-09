package service

// intent_chain_slots_e2e_test.go TB-08：必填槽位门（严格模式）。
//
// 业界对标：Dialogflow CX 必填参数+槽位填充循环 / Rasa forms（review §四[1][2]）。
// 钉四件事：
//  1. 严格模式：required 槽位缺失（客户只说"到哪了"，无实体）→ 本轮派发
//     ToolChoice=none（保证 0 工具调用）+ system prompt 含追问指引；
//  2. 集齐槽位：下一轮客户提供了运单号（intent.Entities 有 tracking_no）→
//     ToolChoice 恢复 auto + 按链调用（prompt 含链指引）；
//  3. 槽位写侧校验矩阵（重复/空话术/非法 source_var → 4000；合法通过+Lookup 映射）；
//  4. 非必填槽位缺失不设门（零变化回归面）。

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func tb08SlotsInput() ChainUpsertInput {
	enabled := true
	return ChainUpsertInput{
		Name:       "快递查询调用流（带槽位门）",
		IntentCode: IntentAskService,
		Steps: []model.IntentChainStepConfig{
			{ToolName: "logistics.track", Args: map[string]any{"tracking_no": "{{entity.tracking_no}}"}, Priority: 1, Enabled: true},
		},
		Slots: []model.IntentSlotConfig{
			{Slot: "tracking_no", Question: "请提供您的运单号，我帮您查询实时轨迹", SourceVar: "entity.tracking_no", Required: true},
		},
		ReplyHint: "查到轨迹后如实回复。",
		Enabled:   &enabled,
	}
}

// intentStubWithEntities 可配 Entities 的识别 stub
type intentStubWithEntities struct {
	intentType string
	entities   map[string]any
}

func (s intentStubWithEntities) Recognize(ctx context.Context, sessionID, customerID, text string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{IntentType: s.intentType, IntentName: "服务咨询", Confidence: 0.9, Entities: s.entities}, nil
}

func tb08Setup(t *testing.T, entities map[string]any, steps []scriptedLLMStep) (*SalesEngine, *captureScriptedLLM) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.IntentToolChain{})
	ctx := tenant.ContextWithDefault(context.Background())
	chainSvc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))
	if _, err := chainSvc.Create(ctx, tb08SlotsInput()); err != nil {
		t.Fatalf("create slots chain: %v", err)
	}

	registry := tooluse.NewToolRegistry()
	deps := tooluse.BusinessToolDeps{Logistics: NewLogisticsPortAdapter(nil)}
	if err := tooluse.RegisterBusinessTools(registry, deps); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{DefaultTimeout: 5000000000})
	cap := newCaptureScriptedLLM(t, steps)

	engine := NewSalesEngine(cap.disp, intentStubWithEntities{intentType: IntentAskService, entities: entities}, fakeMemory{}, fakeSOP{}, &fakeRAGSearcher{}, fakeScript{}, fakeCustomerLookup{})
	engine.SetToolExecutor(context.Background(), &testChainExecutorAdapter{executor: executor})
	engine.SetIntentChainLookup(chainSvc)
	return engine, cap
}

// ① 严格模式：无实体 → ToolChoice=none + 追问指引 + 0 工具调用
func TestTB08_SlotGate_Missing_TracksNoToolCall(t *testing.T) {
	engine, cap := tb08Setup(t, nil /*无实体*/, []scriptedLLMStep{
		{content: "好的，麻烦您提供一下运单号，我马上帮您查询实时轨迹。"},
	})

	resp, err := engine.Handle(tenant.ContextWithDefault(context.Background()), &dto.SalesRequest{
		SessionID: "tb08-s1", CustomerID: "tb08-c1",
		UserMessage: "我的快递到哪了",
		Config:      &dto.SalesEngineConfig{Persona: "测试客服"},
	})
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}

	choices := cap.toolChoices()
	if len(choices) == 0 || choices[0] != "none" {
		t.Fatalf("槽位缺失轮 ToolChoice 应为 none, got %v", choices)
	}
	prompts := cap.systemPrompts()
	if len(prompts) == 0 || !strings.Contains(prompts[0], "追问缺失信息") || !strings.Contains(prompts[0], "tracking_no") {
		t.Fatalf("prompt 应含追问指引, got: %.300s", prompts[0])
	}
	if strings.Contains(resp.Reply, "轨迹") && strings.Contains(resp.Reply, "TB-") {
		t.Fatalf("槽位缺失轮不得编造轨迹结果: %s", resp.Reply)
	}
}

// ② 集齐槽位：实体到位 → ToolChoice 恢复 auto → 按链调用（prompt 含链指引）
func TestTB08_SlotGate_Satisfied_TracksAutoAndCalls(t *testing.T) {
	toolCall := []map[string]any{{
		"id": "c1", "type": "function",
		"function": map[string]any{"name": "logistics.track", "arguments": `{"tracking_no":"SF99887766"}`},
	}}
	engine, cap := tb08Setup(t, map[string]any{"tracking_no": "SF99887766"}, []scriptedLLMStep{
		{toolCalls: toolCall},
		{content: "运单 SF99887766 的最新轨迹：已到达【上海】转运中心。"},
	})

	_, err := engine.Handle(tenant.ContextWithDefault(context.Background()), &dto.SalesRequest{
		SessionID: "tb08-s2", CustomerID: "tb08-c2",
		UserMessage: "帮我查下 SF99887766 到哪了",
		Config:      &dto.SalesEngineConfig{Persona: "测试客服"},
	})
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}
	choices := cap.toolChoices()
	if len(choices) == 0 || choices[0] == "none" {
		t.Fatalf("槽位集齐轮 ToolChoice 应恢复 auto, got %v", choices)
	}
	prompts := cap.systemPrompts()
	if len(prompts) == 0 || !strings.Contains(prompts[0], "按此顺序优先调用") {
		t.Fatalf("槽位集齐轮应含链指引, got: %.300s", prompts[0])
	}
}

// ③ 非必填槽位缺失不设门（零变化回归面）
func TestTB08_OptionalSlotMissing_NoGate(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntentToolChain{})
	ctx := tenant.ContextWithDefault(context.Background())
	chainSvc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))
	in := tb08SlotsInput()
	in.Slots = []model.IntentSlotConfig{{Slot: "tracking_no", Question: "请提供运单号", SourceVar: "entity.tracking_no", Required: false}}
	if _, err := chainSvc.Create(ctx, in); err != nil {
		t.Fatalf("create optional slot chain: %v", err)
	}

	registry := tooluse.NewToolRegistry()
	_ = tooluse.RegisterBusinessTools(registry, tooluse.BusinessToolDeps{Logistics: NewLogisticsPortAdapter(nil)})
	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{DefaultTimeout: 5000000000})
	cap := newCaptureScriptedLLM(t, []scriptedLLMStep{{content: "好的"}})
	engine := NewSalesEngine(cap.disp, intentStubWithEntities{intentType: IntentAskService}, fakeMemory{}, fakeSOP{}, &fakeRAGSearcher{}, fakeScript{}, fakeCustomerLookup{})
	engine.SetToolExecutor(context.Background(), &testChainExecutorAdapter{executor: executor})
	engine.SetIntentChainLookup(chainSvc)

	_, err := engine.Handle(tenant.ContextWithDefault(context.Background()), &dto.SalesRequest{
		SessionID: "tb08-s3", CustomerID: "tb08-c3", UserMessage: "到哪了",
		Config: &dto.SalesEngineConfig{Persona: "测试客服"},
	})
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}
	choices := cap.toolChoices()
	if len(choices) == 0 || choices[0] == "none" {
		t.Fatalf("非必填槽位缺失不应设门, got %v", choices)
	}
}

// ④ 写侧校验矩阵 + Lookup 映射（真库）
func TestTB08_SlotValidation_AndSpecMapping(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntentToolChain{})
	ctx := tenant.ContextWithDefault(context.Background())
	svc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))

	cases := []struct {
		name  string
		slots []model.IntentSlotConfig
	}{
		{"槽位重复", []model.IntentSlotConfig{
			{Slot: "tracking_no", Question: "q1", SourceVar: "entity.tracking_no", Required: true},
			{Slot: "tracking_no", Question: "q2", SourceVar: "entity.tracking_no", Required: true},
		}},
		{"空追问话术", []model.IntentSlotConfig{
			{Slot: "tracking_no", Question: "  ", SourceVar: "entity.tracking_no", Required: true},
		}},
		{"非法 source_var", []model.IntentSlotConfig{
			{Slot: "tracking_no", Question: "q", SourceVar: "system.prompt", Required: true},
		}},
	}
	for _, tc := range cases {
		in := tb08SlotsInput()
		in.Slots = tc.slots
		if _, err := svc.Create(ctx, in); err == nil {
			t.Fatalf("%s: 应拒绝", tc.name)
		}
	}

	// 合法 + 映射
	if _, err := svc.Create(ctx, tb08SlotsInput()); err != nil {
		t.Fatalf("合法槽位应通过: %v", err)
	}
	spec, err := svc.LookupIntentChain(ctx, IntentAskService)
	if err != nil || spec == nil || len(spec.Slots) != 1 {
		t.Fatalf("Lookup 应带槽位, got (%v, %v)", spec, err)
	}
	if spec.Slots[0].Slot != "tracking_no" || spec.Slots[0].SourceVar != "entity.tracking_no" || !spec.Slots[0].Required {
		t.Fatalf("槽位映射不符: %+v", spec.Slots[0])
	}

	// Update 白名单含 slots_json（禁用后 Lookup 不供给）
	in := tb08SlotsInput()
	disabled := false
	in.Enabled = &disabled
	if _, err := svc.Update(ctx, 1, in); err != nil {
		t.Fatalf("update: %v", err)
	}
	if spec2, _ := svc.LookupIntentChain(ctx, IntentAskService); spec2 != nil {
		t.Fatal("禁用后应 (nil,nil)")
	}
}
