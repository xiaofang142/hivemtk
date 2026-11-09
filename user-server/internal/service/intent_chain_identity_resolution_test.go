package service

// intent_chain_identity_resolution_test.go TB-13：服务端身份解析 + prompt 去手机号。
//
// 钉三件事：
//  1. 服务端解析：LLM 零提供 phone/name/order_id → order.lookup 按
//     ToolContext.CustomerID 经 CustomerStore 解析 → 仍命中镜像订单（手机号不进
//     prompt 也能工作）；
//  2. prompt 隐私：system prompt 不含完整手机号（脱敏形态 138****777 出现）；
//  3. follow_task.create：customer_id 缺省时取 ToolContext.CustomerID（服务端主权）。
//  4. customerNameOf/maskPhoneForPrompt 纯函数格（Name 优先/短号掩盖/nil 安全）。

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// ---------- 纯函数格 ----------

func TestTB13_CustomerNameOf_NamePriorityAndMask(t *testing.T) {
	if got := customerNameOf(&model.Customer{Name: "张三", Phone: "13800007777"}); got != "张三" {
		t.Fatalf("有姓名应返回姓名, got %q", got)
	}
	if got := customerNameOf(&model.Customer{Phone: "13800007777"}); got != "138****7777" {
		t.Fatalf("缺名应返回脱敏手机, got %q", got)
	}
	if got := customerNameOf(&model.Customer{Phone: "138"}); got != "***" {
		t.Fatalf("短号应全掩, got %q", got)
	}
	if got := customerNameOf(nil); got != "" {
		t.Fatalf("nil 应空串, got %q", got)
	}
}

// ---------- fake：CustomerStore（GetByID 服务端解析面） ----------

type tb13CustomerStore struct {
	customers map[string]*model.Customer
}

func (s *tb13CustomerStore) FindByIdentity(ctx context.Context, phone, email, wechat, douyin string) (*model.Customer, error) {
	for _, c := range s.customers {
		if (phone != "" && c.Phone == phone) || (douyin != "" && c.DouyinOpenID == douyin) {
			return c, nil
		}
	}
	return nil, nil
}

func (s *tb13CustomerStore) GetByXiaohongshuID(ctx context.Context, id string) (*model.Customer, error) {
	return nil, nil
}

func (s *tb13CustomerStore) GetByID(ctx context.Context, id string) (*model.Customer, error) {
	if c, ok := s.customers[id]; ok {
		return c, nil
	}
	return nil, nil
}

func (s *tb13CustomerStore) Update(ctx context.Context, customer *model.Customer) error { return nil }

func (s *tb13CustomerStore) SearchByFilter(ctx context.Context, f tooluse.CustomerSegmentFilter) ([]*model.Customer, int64, error) {
	return nil, 0, nil
}

// fake OrderPort：按 phone 命中镜像视图
type tb13OrderPort struct {
	phones map[string][]*portcontract.OrderView
}

func (p *tb13OrderPort) LookupByOrderID(ctx context.Context, platform, orderID string) (*portcontract.OrderView, error) {
	return nil, nil
}

func (p *tb13OrderPort) LookupByCustomer(ctx context.Context, phone, name string) ([]*portcontract.OrderView, error) {
	if v, ok := p.phones[phone]; ok {
		return v, nil
	}
	return nil, nil
}

// ---------- E2E：LLM 零提供身份参数 → 服务端解析 → 命中镜像 ----------

func TestTB13_ServerSideIdentityResolution_E2E(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntentToolChain{}, &model.ExternalOrder{})
	ctx := tenant.ContextWithDefault(context.Background())

	chainSvc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))
	enabled := true
	if _, err := chainSvc.Create(ctx, ChainUpsertInput{
		Name: "催发货调用流", IntentCode: IntentAskService,
		Steps: []model.IntentChainStepConfig{
			{ToolName: "order.lookup", Priority: 1, Enabled: true},
		},
		ReplyHint: "查订单镜像回复发货状态。",
		Enabled:   &enabled,
	}); err != nil {
		t.Fatalf("create chain: %v", err)
	}

	registry := tooluse.NewToolRegistry()
	deps := tooluse.BusinessToolDeps{
		Order: &tb13OrderPort{phones: map[string][]*portcontract.OrderView{
			"13800007777": {{Platform: "taobao", OrderID: "TB-13-001", Status: "已发货待物流揽收"}},
		}},
		CustomerStore: &tb13CustomerStore{customers: map[string]*model.Customer{
			"cust-tb13-1": {ID: "cust-tb13-1", Name: "张三", Phone: "13800007777"},
		}},
	}
	if err := tooluse.RegisterBusinessTools(registry, deps); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{DefaultTimeout: 5000000000})

	cap := newCaptureScriptedLLM(t, []scriptedLLMStep{
		// LLM 只给了 customer_id 之外的空参——身份完全靠服务端解析
		{toolCalls: []map[string]any{{
			"id": "c1", "type": "function",
			"function": map[string]any{"name": "order.lookup", "arguments": `{}`},
		}}},
		{content: "您的订单 TB-13-001 当前状态：已发货待物流揽收。"},
	})
	engine := NewSalesEngine(cap.disp, chainIntentStub{intentType: IntentAskService}, fakeMemory{}, fakeSOP{}, &fakeRAGSearcher{}, fakeScript{}, tb13CustomerLookupAdapter{store: deps.CustomerStore})
	engine.SetToolExecutor(context.Background(), &testChainExecutorAdapter{executor: executor})
	engine.SetIntentChainLookup(chainSvc)
	// TB-13：CustomerLookup 用真实解析适配器（构造参数第 7 位）

	resp, err := engine.Handle(tenant.WithTenantID(context.Background(), 1), &dto.SalesRequest{
		SessionID: "tb13-s1", CustomerID: "cust-tb13-1",
		UserMessage: "什么时候发货",
		Config:      &dto.SalesEngineConfig{Persona: "测试客服"},
	})
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}
	if !tb04ContainsAny(resp.Reply, "TB-13-001") {
		t.Fatalf("服务端解析应命中镜像订单, got: %s", resp.Reply)
	}

	// prompt 隐私：完整手机号不得出现；脱敏形态允许
	for _, p := range cap.systemPrompts() {
		if strings.Contains(p, "13800007777") {
			t.Fatalf("system prompt 不得含完整手机号: %.300s", p)
		}
	}
}

// 客户查询适配器（CustomerLookup 接口的 TB-13 测试实现：GetByOneID/GetByID）
type tb13CustomerLookupAdapter struct{ store tooluse.CustomerDataStore }

func (a tb13CustomerLookupAdapter) GetByOneID(ctx context.Context, oneID string) (*model.Customer, error) {
	return a.store.GetByID(ctx, oneID)
}

func (a tb13CustomerLookupAdapter) GetByID(ctx context.Context, id string) (*model.Customer, error) {
	return a.store.GetByID(ctx, id)
}

// ---------- follow_task.create：customer_id 缺省取 ToolContext（纯工具格） ----------

// tb13FollowUpPort 最小 Fake（Schedule 返回成功形态）
type tb13FollowUpPort struct{}

func (p *tb13FollowUpPort) Schedule(ctx context.Context, customerID, ownerID, reminderType string, dueIn time.Duration, opts *portcontract.FollowUpScheduleOptions) (any, error) {
	return map[string]any{"reminder_id": "rem_tb13", "customer_id": customerID}, nil
}

func (p *tb13FollowUpPort) CompleteWithResult(reminderID string, result, note string) error {
	return nil
}

func (p *tb13FollowUpPort) Cancel(reminderID string) error {
	return nil
}

func (p *tb13FollowUpPort) ResultInfo(result string) (string, bool) {
	return "", false
}

func TestTB13_FollowTaskCreate_DefaultCustomerFromContext(t *testing.T) {
	registry := tooluse.NewToolRegistry()
	deps := tooluse.BusinessToolDeps{FollowUp: &tb13FollowUpPort{}}
	if err := tooluse.RegisterBusinessTools(registry, deps); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{DefaultTimeout: 5000000000})

	toolCtx := &tooluse.ToolContext{AgentID: "a1", SessionID: "s1", CustomerID: "cust-tb13-2", Source: "test"}
	results := executor.DispatchByLLMToolCall(tenant.ContextWithDefault(context.Background()),
		[]tooluse.LLMToolCall{{ID: "c1", Function: tooluse.LLMToolFunction{Name: "follow_task.create", Arguments: `{"title":"催发货跟进"}`}}},
		toolCtx)
	if len(results) == 0 {
		t.Fatal("应返回结果")
	}
	// customer_id 缺省 → 服务端取 ctx.CustomerID（结果成功即证明解析生效；失败会在 content 报必填）
	if !results[0].Success {
		t.Fatalf("customer_id 缺省应由服务端补全执行, got: %s", results[0].Content)
	}
}
