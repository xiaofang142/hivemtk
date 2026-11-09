package service

// intent_chain_flow_e2e_test.go TB-03：意图→工具调用流绑定层接入 CS 主链路的端到端。
//
// 样式沿 cs01/cs02 家族（真库 + 脚本化 LLM fake + httptest 外部依赖），钉四件事：
//  1. 绑定命中 → agentloop 系统提示真的带上"按链优先调用"指引（capture 请求体断言）；
//     未命中/未装配 → 提示零变化（回归面）。
//  2. 绑定工具晋升：绑定链工具过 limitToolsForAgent 截断仍完整在列（纯函数格）。
//  3. 样本①（有运单号）：LLM 调 logistics.track → 真快递聚合 HTTP（httptest）→
//     轨迹事实来自工具结果，回复含锚点、不编造。
//  4. 样本③（未配置实时接口）：logistics.track 返回 fail-closed Notice，
//     回复含 Notice 锚点（不编造轨迹），零 courier 请求。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// ---------- 意图 stub（固定返回 ask_service；fakeIntentRecognizer 是 price_inquiry 固定值，不共用） ----------

type chainIntentStub struct{ intentType string }

func (s chainIntentStub) Recognize(ctx context.Context, sessionID, customerID, text string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{IntentType: s.intentType, IntentName: "测试意图", Confidence: 0.9}, nil
}

// ---------- fake courier（快递聚合 HTTP） ----------

type fakeCourier struct {
	srv  *httptest.Server
	hits atomic.Int32
}

func newFakeCourier(t *testing.T) *fakeCourier {
	t.Helper()
	fc := &fakeCourier{}
	fc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fc.hits.Add(1)
		if r.URL.Path != "/api/track" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tracks": []map[string]string{
				{"time": "2026-11-10 09:00:00", "status": "运输中", "location": "上海转运中心", "description": "已到达【上海】转运中心"},
				{"time": "2026-11-09 20:00:00", "status": "已发货", "location": "深圳", "description": "快件已从【深圳】发出"},
			},
		})
	}))
	t.Cleanup(fc.srv.Close)
	return fc
}

// ---------- 脚本化 LLM fake（第 N 次派发回预设响应；capture system prompt） ----------

type scriptedLLMStep struct {
	toolCalls []map[string]any // 非空 → 回 tool_calls
	content   string           // toolCalls 为空时的最终文本
}

type captureScriptedLLM struct {
	srv  *httptest.Server
	disp *llm.Dispatcher

	mu       sync.Mutex
	prompts  []string // 每次派发的 system prompt 原文
	choices  []string // 每次派发的 tool_choice 原文（TB-08 槽位门断言）
	dispatch atomic.Int32
}

// newCaptureScriptedLLM steps[i] = 第 i 次派发的响应。
func newCaptureScriptedLLM(t *testing.T, steps []scriptedLLMStep) *captureScriptedLLM {
	t.Helper()
	cap := &captureScriptedLLM{}
	cap.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			ToolChoice string `json:"tool_choice"`
		}
		_ = json.Unmarshal(body, &req)
		var sys strings.Builder
		for _, m := range req.Messages {
			if m.Role == "system" {
				sys.WriteString(m.Content)
			}
		}
		cap.mu.Lock()
		cap.prompts = append(cap.prompts, sys.String())
		cap.choices = append(cap.choices, req.ToolChoice)
		cap.mu.Unlock()

		idx := int(cap.dispatch.Add(1)) - 1
		w.Header().Set("Content-Type", "application/json")
		if idx >= len(steps) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "抱歉，我暂时无法处理您的请求，请稍后再试，或回复「转人工」由人工客服为您服务。"}}},
			})
			return
		}
		st := steps[idx]
		if len(st.toolCalls) > 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message":       map[string]any{"role": "assistant", "tool_calls": st.toolCalls},
					"finish_reason": "tool_calls",
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": st.content}, "finish_reason": "stop"}},
		})
	}))
	t.Cleanup(cap.srv.Close)

	disp := llm.NewDispatcher(llm.NewLLMService())
	disp.AddProvider(llm.ProviderConfig{Name: "fake-tb03", APIKey: "test", BaseURL: cap.srv.URL, APIType: "openai", Model: "fake", Enabled: true})
	for _, sc := range []llm.DispatchScenario{
		llm.ScenarioSOPReply, llm.ScenarioObjection, llm.ScenarioFriendlyChat,
		llm.ScenarioIntentRecognize, llm.ScenarioHighQuality, llm.ScenarioLowCost, llm.ScenarioLongSummary,
	} {
		disp.SetRoute(llm.ScenarioRoute{Scenario: sc, Provider: "fake-tb03", Fallbacks: []string{"fake-tb03"}})
	}
	cap.disp = disp
	return cap
}

func (c *captureScriptedLLM) systemPrompts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.prompts))
	copy(out, c.prompts)
	return out
}

// toolChoices 每次派发的 tool_choice 序列（TB-08 槽位门断言）。
func (c *captureScriptedLLM) toolChoices() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.choices))
	copy(out, c.choices)
	return out
}

// ---------- 测试本地执行器适配器（镜像 app/tool_executor_adapter.go 最小面） ----------

type testChainExecutorAdapter struct{ executor *tooluse.ToolExecutor }

func (a *testChainExecutorAdapter) ListTools(ctx context.Context) []AgentToolDef {
	fns := a.executor.ListAvailableLLMFunctionsForTenant(ctx)
	out := make([]AgentToolDef, 0, len(fns))
	for _, fn := range fns {
		params := map[string]any{}
		if b, err := json.Marshal(fn.Parameters); err == nil {
			_ = json.Unmarshal(b, &params)
		}
		out = append(out, AgentToolDef{Name: fn.Name, Description: fn.Description, Parameters: params})
	}
	return out
}

func (a *testChainExecutorAdapter) DispatchToolCalls(ctx context.Context, calls []AgentToolCall, toolCtx AgentToolContext) []AgentToolResult {
	llmCalls := make([]tooluse.LLMToolCall, 0, len(calls))
	for _, c := range calls {
		llmCalls = append(llmCalls, tooluse.LLMToolCall{ID: c.ID, Function: tooluse.LLMToolFunction{Name: c.Name, Arguments: c.Arguments}})
	}
	tooluseCtx := &tooluse.ToolContext{AgentID: toolCtx.AgentID, SessionID: toolCtx.SessionID, CustomerID: toolCtx.CustomerID, Source: toolCtx.Source}
	results := a.executor.DispatchByLLMToolCall(ctx, llmCalls, tooluseCtx)
	out := make([]AgentToolResult, 0, len(results))
	for _, r := range results {
		out = append(out, AgentToolResult{ToolCallID: r.ToolCallID, Content: r.Content, Success: r.Success, Card: r.Card})
	}
	return out
}

// ---------- 夹具：真库 + 种子 + 执行器 + 引擎 ----------

type tb03Fixture struct {
	db       *gorm.DB
	chainSvc *IntentToolChainService
	engine   *SalesEngine
	courier  *fakeCourier
	cap      *captureScriptedLLM
}

func tb03Setup(t *testing.T, courierConfigured bool, steps []scriptedLLMStep, intentType string) *tb03Fixture {
	t.Helper()
	db := testutil.NewTestDB(t, &model.IntentToolChain{}, &model.SystemConfigKV{})
	ctx := tenant.ContextWithDefault(context.Background())

	chainSvc := NewIntentToolChainService(repository.NewIntentToolChainRepositoryWithDB(db))
	if _, err := chainSvc.SeedDefaultIntentChains(ctx); err != nil {
		t.Fatalf("种子失败: %v", err)
	}

	courier := newFakeCourier(t)
	if courierConfigured {
		cfgJSON, _ := json.Marshal(ToolIntegrationConfig{
			Logistics: LogisticsIntegration{Enabled: true, Provider: "generic", BaseURL: courier.srv.URL},
		})
		if err := db.Create(&model.SystemConfigKV{TenantID: 1, Key: "agent.tool_integrations", Value: string(cfgJSON)}).Error; err != nil {
			t.Fatalf("写入物流配置失败: %v", err)
		}
	}

	registry := tooluse.NewToolRegistry()
	deps := tooluse.BusinessToolDeps{Logistics: NewLogisticsPortAdapter(nil)}
	if err := tooluse.RegisterBusinessTools(registry, deps); err != nil {
		t.Fatalf("注册业务工具失败: %v", err)
	}
	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{DefaultTimeout: 5000000000}) // 5s
	cap := newCaptureScriptedLLM(t, steps)

	engine := NewSalesEngine(cap.disp, chainIntentStub{intentType: intentType}, fakeMemory{}, fakeSOP{}, &fakeRAGSearcher{}, fakeScript{}, fakeCustomerLookup{})
	engine.SetToolExecutor(context.Background(), &testChainExecutorAdapter{executor: executor})
	engine.SetIntentChainLookup(chainSvc)

	return &tb03Fixture{db: db, chainSvc: chainSvc, engine: engine, courier: courier, cap: cap}
}

const tb03TrackAnchor = "已到达【上海】转运中心"

// ---------- 样本①：有运单号 → 真快递查询 → 轨迹回复（不编造） ----------

func TestTB03_ExpressChain_TrackReply(t *testing.T) {
	toolCall := []map[string]any{{
		"id":   "call-1",
		"type": "function",
		"function": map[string]any{
			"name":      "logistics.track",
			"arguments": `{"tracking_no":"SF123456789","carrier":"SF"}`,
		},
	}}
	fx := tb03Setup(t, true, []scriptedLLMStep{
		{toolCalls: toolCall},
		{content: "您的快递 SF123456789 最新轨迹：" + tb03TrackAnchor + "（测试话术回填工具结果）"},
	}, IntentAskService)

	resp, err := fx.engine.Handle(tenant.ContextWithDefault(context.Background()), &dto.SalesRequest{
		SessionID: "tb03-s1", CustomerID: "tb03-c1",
		UserMessage: "我的快递到哪了，运单号 SF123456789",
		Config:      &dto.SalesEngineConfig{Persona: "测试客服"},
	})
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}
	if fx.courier.hits.Load() != 1 {
		t.Fatalf("快递聚合 API 应恰被调 1 次, got %d", fx.courier.hits.Load())
	}
	if !strings.Contains(resp.Reply, tb03TrackAnchor) {
		t.Fatalf("回复应含工具返回的轨迹锚点, got: %s", resp.Reply)
	}

	// 绑定命中 → 系统提示带"按链优先调用"指引（第一拍 system prompt）
	prompts := fx.cap.systemPrompts()
	if len(prompts) == 0 {
		t.Fatal("应捕获到 LLM 请求")
	}
	if !strings.Contains(prompts[0], "按此顺序优先调用") || !strings.Contains(prompts[0], "order.lookup → logistics.track") {
		t.Fatalf("system prompt 应含绑定链指引, got: %.200s", prompts[0])
	}
}

// ---------- 回归面：未装配绑定层 → 提示零变化 ----------

func TestTB03_NoLookup_PromptUnchanged(t *testing.T) {
	toolCall := []map[string]any{{
		"id": "call-1", "type": "function",
		"function": map[string]any{"name": "logistics.track", "arguments": `{"tracking_no":"SF123456789"}`},
	}}
	db := testutil.NewTestDB(t, &model.IntentToolChain{}, &model.SystemConfigKV{})
	courier := newFakeCourier(t)
	cfgJSON, _ := json.Marshal(ToolIntegrationConfig{
		Logistics: LogisticsIntegration{Enabled: true, Provider: "generic", BaseURL: courier.srv.URL},
	})
	_ = db.Create(&model.SystemConfigKV{TenantID: 1, Key: "agent.tool_integrations", Value: string(cfgJSON)}).Error

	registry := tooluse.NewToolRegistry()
	_ = tooluse.RegisterBusinessTools(registry, tooluse.BusinessToolDeps{Logistics: NewLogisticsPortAdapter(nil)})
	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{DefaultTimeout: 5000000000})
	cap := newCaptureScriptedLLM(t, []scriptedLLMStep{
		{toolCalls: toolCall},
		{content: "轨迹：" + tb03TrackAnchor},
	})
	engine := NewSalesEngine(cap.disp, chainIntentStub{intentType: IntentAskService}, fakeMemory{}, fakeSOP{}, &fakeRAGSearcher{}, fakeScript{}, fakeCustomerLookup{})
	engine.SetToolExecutor(context.Background(), &testChainExecutorAdapter{executor: executor})
	// 注意：不注入 intentChain

	_, err := engine.Handle(tenant.ContextWithDefault(context.Background()), &dto.SalesRequest{
		SessionID: "tb03-s2", CustomerID: "tb03-c2",
		UserMessage: "快递到哪了",
		Config:      &dto.SalesEngineConfig{Persona: "测试客服"},
	})
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}
	prompts := cap.systemPrompts()
	if len(prompts) == 0 {
		t.Fatal("应捕获到 LLM 请求")
	}
	if strings.Contains(prompts[0], "按此顺序优先调用") {
		t.Fatal("未装配绑定层时 system prompt 不得出现链指引（零变化回归面）")
	}
}

// ---------- 样本③：未配置实时接口 → fail-closed Notice（不编造轨迹） ----------

func TestTB03_ExpressChain_UnconfiguredFailClosed(t *testing.T) {
	toolCall := []map[string]any{{
		"id": "call-1", "type": "function",
		"function": map[string]any{"name": "logistics.track", "arguments": `{"tracking_no":"SF123456789"}`},
	}}
	fx := tb03Setup(t, false /*courierConfigured*/, []scriptedLLMStep{
		{toolCalls: toolCall},
		{content: "物流查询结果：" + "当前未配置实时快递接口" + "（回填工具 Notice）"},
	}, IntentAskService)

	resp, err := fx.engine.Handle(tenant.ContextWithDefault(context.Background()), &dto.SalesRequest{
		SessionID: "tb03-s3", CustomerID: "tb03-c3",
		UserMessage: "快递到哪了",
		Config:      &dto.SalesEngineConfig{Persona: "测试客服"},
	})
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}
	if fx.courier.hits.Load() != 0 {
		t.Fatalf("未配置时不得请求快递 API, got %d 次", fx.courier.hits.Load())
	}
	if !strings.Contains(resp.Reply, "未配置实时快递接口") {
		t.Fatalf("回复应含 fail-closed Notice 锚点, got: %s", resp.Reply)
	}
}

// ---------- 纯函数格：工具晋升 + 提示渲染 ----------

func TestTB03_PromoteChainTools_StableFront(t *testing.T) {
	tools := []AgentToolDef{
		{Name: "rag.search"}, {Name: "customer.search"}, {Name: "logistics.track"},
		{Name: "card.show"}, {Name: "order.lookup"},
	}
	spec := &agent_runtime.IntentChainSpec{IntentCode: "ask_service", Steps: []agent_runtime.IntentChainStep{
		{ToolName: "order.lookup", Enabled: true, Priority: 1},
		{ToolName: "logistics.track", Enabled: true, Priority: 2},
	}}
	out := promoteIntentChainTools(tools, spec)
	if len(out) != len(tools) {
		t.Fatalf("晋升不得丢工具: got %d want %d", len(out), len(tools))
	}
	if out[0].Name != "order.lookup" || out[1].Name != "logistics.track" {
		t.Fatalf("绑定链工具应前置, got %s,%s", out[0].Name, out[1].Name)
	}
	// 其余保序
	rest := []string{out[2].Name, out[3].Name, out[4].Name}
	if strings.Join(rest, ",") != "rag.search,customer.search,card.show" {
		t.Fatalf("其余工具应保序, got %v", rest)
	}
	// 链上不存在的工具 → 原样返回
	same := promoteIntentChainTools(tools, &agent_runtime.IntentChainSpec{Steps: []agent_runtime.IntentChainStep{{ToolName: "unknown.tool", Enabled: true}}})
	if len(same) != len(tools) || same[0].Name != "rag.search" {
		t.Fatal("链工具不在清单时序应不变")
	}
}

func TestTB03_RenderChainHint(t *testing.T) {
	spec := &agent_runtime.IntentChainSpec{Steps: []agent_runtime.IntentChainStep{
		{ToolName: "order.lookup", Enabled: true},
		{ToolName: "logistics.track", Enabled: true},
		{ToolName: "disabled.step", Enabled: false},
	}}
	hint := renderIntentChainHint(spec)
	if !strings.Contains(hint, "order.lookup → logistics.track") {
		t.Fatalf("提示应含链序列且跳过禁用步, got: %s", hint)
	}
	if renderIntentChainHint(nil) != "" {
		t.Fatal("nil spec → 空提示")
	}
	allDisabled := &agent_runtime.IntentChainSpec{Steps: []agent_runtime.IntentChainStep{{ToolName: "x", Enabled: false}}}
	if renderIntentChainHint(allDisabled) != "" {
		t.Fatal("全禁用 → 空提示")
	}
}
