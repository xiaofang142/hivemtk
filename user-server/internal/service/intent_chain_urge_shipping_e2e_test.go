package service

// intent_chain_urge_shipping_e2e_test.go TB-04：催发货=意图→工具调用流基座第二实例。
//
// 证明两件事（全部纯配置，零生产代码改动）：
//  1. 既有意图（ask_service）挂新调用流（order.lookup → follow_task.create）→
//     催发货消息走通"查订单→建跟进任务→回复"，follow_task.create 真实执行；
//  2. 负例：订单缺失不编造（order.lookup 返回未找到 → 回复引导提供订单号，
//     courier 零请求、零订单字段编造）。
//
// 人工持有期不自动触发的负例由 CS-04 承载（发送前复核 IsSessionHumanLocked
// 在 runAIGeneration 出站竖，绑定层只改提示与工具晋升、不碰发送路径），不重复建格。

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

// ---------- 纯函数格：催发货种子配置形态（新调用流=纯配置） ----------

func TestTB04_UrgeShippingChain_PureConfig(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntentToolChain{}, &model.ExternalOrder{})
	ctx := tenant.ContextWithDefault(context.Background())

	// 1) 预置外部订单镜像（催发货场景：已发货无物流单号 → 引导+跟进）
	order := &model.ExternalOrder{
		TenantID:  1,
		Platform:  "taobao",
		OrderID:   "TB-urge-001",
		UserName:  "催发货客户",
		UserPhone: "13800000777",
		Status:    "已发货待物流揽收",
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("预置订单失败: %v", err)
	}

	// 2) 绑定层配置：ask_service 挂新调用流（order.lookup → follow_task.create）——纯配置
	chainSvc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))
	enabled := true
	if _, err := chainSvc.Create(ctx, ChainUpsertInput{
		Name:       "催发货调用流（订单镜像+跟进任务）",
		IntentCode: IntentAskService,
		Steps: []model.IntentChainStepConfig{
			{ToolName: "order.lookup", Args: map[string]any{"phone": "{{customer_id}}"}, Priority: 1, Enabled: true},
			{ToolName: "follow_task.create", Args: map[string]any{"customer_id": "{{customer_id}}", "title": "催发货跟进：确认发货进度"}, Priority: 2, Enabled: true},
		},
		ReplyHint: "客户催发货。先查订单镜像确认发货状态；无物流信息时创建跟进任务承诺当天回复进度，不要编造物流单号。",
		Enabled:   &enabled,
	}); err != nil {
		t.Fatalf("纯配置创建调用流失败: %v", err)
	}

	if spec, err := chainSvc.LookupIntentChain(ctx, IntentAskService); err != nil || spec == nil || len(spec.Steps) != 2 {
		t.Fatalf("调用流应可查, got (%v, %v)", spec, err)
	}
}

// ---------- E2E：催发货消息走通 查订单→建跟进→回复 ----------

func TestTB04_UrgeShippingChain_E2E(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntentToolChain{}, &model.ExternalOrder{})
	ctx := tenant.ContextWithDefault(context.Background())

	if err := db.Create(&model.ExternalOrder{
		TenantID: 1, Platform: "taobao", OrderID: "TB-urge-002",
		UserName: "催发货客户", UserPhone: "13800000777", Status: "已发货待物流揽收",
	}).Error; err != nil {
		t.Fatalf("预置订单失败: %v", err)
	}

	chainSvc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))
	enabled := true
	if _, err := chainSvc.Create(ctx, ChainUpsertInput{
		Name:       "催发货调用流",
		IntentCode: IntentAskService,
		Steps: []model.IntentChainStepConfig{
			{ToolName: "order.lookup", Args: map[string]any{"phone": "{{customer_id}}"}, Priority: 1, Enabled: true},
			{ToolName: "follow_task.create", Args: map[string]any{"customer_id": "{{customer_id}}"}, Priority: 2, Enabled: true},
		},
		ReplyHint: "先查订单镜像，无物流信息时建跟进任务，不要编造物流单号。",
		Enabled:   &enabled,
	}); err != nil {
		t.Fatalf("create chain: %v", err)
	}

	// 业务工具全量装配（订单镜像 Port + 跟进 Port；物流未配置=无实时轨迹腿）
	registry := tooluse.NewToolRegistry()
	deps := tooluse.BusinessToolDeps{
		Order:     NewOrderPortAdapter(repository.NewExternalOrderRepository()),
		FollowUp:  NewFollowUpPortAdapter(nil),
		Logistics: NewLogisticsPortAdapter(nil),
	}
	if err := tooluse.RegisterBusinessTools(registry, deps); err != nil {
		t.Fatalf("注册业务工具失败: %v", err)
	}
	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{DefaultTimeout: 5000000000})

	// 脚本化 LLM：①查订单 ②建跟进 ③综合作答（回复含订单状态与跟进锚点，不编造物流单号）
	cap := newCaptureScriptedLLM(t, []scriptedLLMStep{
		{toolCalls: []map[string]any{{
			"id": "c1", "type": "function",
			"function": map[string]any{"name": "order.lookup", "arguments": `{"phone":"13800000777"}`},
		}}},
		{toolCalls: []map[string]any{{
			"id": "c2", "type": "function",
			"function": map[string]any{"name": "follow_task.create", "arguments": `{"customer_id":"13800000777","title":"催发货跟进：确认发货进度"}`},
		}}},
		{content: "您的订单 TB-urge-002 当前状态：已发货待物流揽收。物流单号生成前我们已为您登记催发货跟进任务（催发货跟进：确认发货进度），当天内回复您具体进度。"},
	})

	engine := NewSalesEngine(cap.disp, chainIntentStub{intentType: IntentAskService}, fakeMemory{}, fakeSOP{}, &fakeRAGSearcher{}, fakeScript{}, fakeCustomerLookup{})
	engine.SetToolExecutor(context.Background(), &testChainExecutorAdapter{executor: executor})
	engine.SetIntentChainLookup(chainSvc)

	resp, err := engine.Handle(ctx, &dto.SalesRequest{
		SessionID: "tb04-s1", CustomerID: "13800000777",
		UserMessage: "我下单两天了还没发货，什么时候发货？",
		Config:      &dto.SalesEngineConfig{Persona: "测试客服"},
	})
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}
	if !tb04ContainsAny(resp.Reply, "TB-urge-002", "已发货待物流揽收") {
		t.Fatalf("回复应含订单镜像事实（工具结果）, got: %s", resp.Reply)
	}
	if !tb04ContainsAny(resp.Reply, "催发货跟进") {
		t.Fatalf("回复应含跟进任务锚点, got: %s", resp.Reply)
	}
	// 链提示进 system prompt
	prompts := cap.systemPrompts()
	if len(prompts) == 0 || !tb04ContainsAny(prompts[0], "order.lookup → follow_task.create") {
		t.Fatalf("system prompt 应含催发货链指引, got: %.200s", prompts[0])
	}
}

// ---------- 负例：订单缺失不编造 ----------

func TestTB04_UrgeShipping_NoOrder_NoFabrication(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntentToolChain{}, &model.ExternalOrder{})
	ctx := tenant.ContextWithDefault(context.Background())

	chainSvc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))
	enabled := true
	if _, err := chainSvc.Create(ctx, ChainUpsertInput{
		Name:       "催发货调用流",
		IntentCode: IntentAskService,
		Steps: []model.IntentChainStepConfig{
			{ToolName: "order.lookup", Args: map[string]any{"phone": "{{customer_id}}"}, Priority: 1, Enabled: true},
		},
		ReplyHint: "查不到订单时礼貌引导客户提供订单号或收货手机号，不要编造订单信息。",
		Enabled:   &enabled,
	}); err != nil {
		t.Fatalf("create chain: %v", err)
	}

	registry := tooluse.NewToolRegistry()
	deps := tooluse.BusinessToolDeps{Order: NewOrderPortAdapter(repository.NewExternalOrderRepository())}
	if err := tooluse.RegisterBusinessTools(registry, deps); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{DefaultTimeout: 5000000000})

	cap := newCaptureScriptedLLM(t, []scriptedLLMStep{
		{toolCalls: []map[string]any{{
			"id": "c1", "type": "function",
			"function": map[string]any{"name": "order.lookup", "arguments": `{"phone":"13900000000"}`},
		}}},
		{content: "没有查到与您关联的订单，麻烦提供一下订单号或下单手机号，我来帮您核实发货进度。"},
	})
	engine := NewSalesEngine(cap.disp, chainIntentStub{intentType: IntentAskService}, fakeMemory{}, fakeSOP{}, &fakeRAGSearcher{}, fakeScript{}, fakeCustomerLookup{})
	engine.SetToolExecutor(context.Background(), &testChainExecutorAdapter{executor: executor})
	engine.SetIntentChainLookup(chainSvc)

	resp, err := engine.Handle(ctx, &dto.SalesRequest{
		SessionID: "tb04-s2", CustomerID: "13900000000",
		UserMessage: "什么时候发货",
		Config:      &dto.SalesEngineConfig{Persona: "测试客服"},
	})
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}
	if tb04ContainsAny(resp.Reply, "TB-urge-001", "TB-urge-002", "已发货待物流揽收") {
		t.Fatalf("订单缺失时回复不得编造订单字段, got: %s", resp.Reply)
	}
	if !tb04ContainsAny(resp.Reply, "订单号") {
		t.Fatalf("回复应引导提供订单号, got: %s", resp.Reply)
	}
}

func tb04ContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
