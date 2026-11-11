package connector

// bridge_test.go §3.12 刀1：连接器端点→tooluse 工具桥接契约。
//
// 判据（对应清单行）：
//  1. preset 注册的新端点**零平台专属代码**成为工具（fakeMart 虚构平台纯数据注册）；
//  2. 硬判据=动态注册工具必须同覆盖 审批/审计/风控/限流 装饰器链——走真实
//     ToolExecutor 执行面断言（照 openapi_tools_decorator_test 手法：直接调
//     tool.Execute 会绕过整条链，承诺就无人验证）+ WithApproval 包装在位断言；
//  3. 负例三件套：未注册方言拒绝 / SSRF 拒绝（无外发）/ 能力标签缺失不桥接不外发。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

// ---------- 夹具：记录型装饰器端口 ----------

type recordingLimiter struct {
	mu   sync.Mutex
	keys []string
}

func (l *recordingLimiter) Acquire(_ context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.keys = append(l.keys, key)
	return nil
}

func (l *recordingLimiter) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.keys))
	copy(out, l.keys)
	return out
}

type recordingPermissionChecker struct {
	mu    sync.Mutex
	tools []string
}

func (c *recordingPermissionChecker) Check(_ context.Context, toolName string, _ *tooluse.ToolContext) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools = append(c.tools, toolName)
	return nil
}

func (c *recordingPermissionChecker) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.tools))
	copy(out, c.tools)
	return out
}

// fakeAgentGrants AgentGrantReader 最小实现（风控门判据面）。
type fakeAgentGrants struct{}

func (fakeAgentGrants) AgentHasTool(_, _ string) bool        { return false }
func (fakeAgentGrants) AgentWhitelistConfigured(string) bool { return false }
func (fakeAgentGrants) ListConfiguredAgents() []string       { return nil }
func (fakeAgentGrants) ListAgentWhitelist(string) []string   { return nil }

// ---------- 判据 1：虚构平台零代码桥接 + schema/名称冻结 ----------

func TestBridge_FictionalPresetBecomesTools(t *testing.T) {
	// 虚构平台 fakeMart：除 preset 数据外无任何平台专属代码。
	preset := ConnectorPreset{
		Name:   "bridge_fakeMart", // sanitize → bridge_fakemart
		Config: ConnectorConfig{BaseURL: "https://fakemart.example.com", Provider: "generic", Auth: ConnectorAuth{Type: "bearer", Key: "k"}},
		Endpoints: []EndpointConfig{
			{
				Name:         "query_order", // 能力全 query_ 前缀 → readonly
				Method:       "GET",
				Path:         "/api/v1/orders",
				Capabilities: []string{"query_order"},
				ParamMap:     map[string]string{"order_id": "platform_order_id"},
			},
			{
				Name:         "modify_order", // 非 query_ 前缀 → high_write + WithApproval
				Method:       "POST",
				Path:         "/api/v1/orders/modify",
				Capabilities: []string{"modify_order"},
			},
		},
	}
	tools, skipped, presetErrs := BridgePresetsWithExecutor(NewExecutorWithValidator(func(string) error { return nil }), preset)
	if len(presetErrs) != 0 {
		t.Fatalf("不应有 preset 级错误: %v", presetErrs)
	}
	if len(skipped) != 0 {
		t.Fatalf("不应有跳过端点: %v", skipped)
	}
	if len(tools) != 2 {
		t.Fatalf("应桥接 2 个工具, got %d", len(tools))
	}

	byName := map[string]tooluse.Tool{}
	for _, tl := range tools {
		byName[tl.Name()] = tl
	}
	// C1-1 tool_name 冻结：connector.<preset>.<endpoint>，preset 名 sanitize。
	qo, ok := byName["connector.bridge_fakemart.query_order"]
	if !ok {
		t.Fatalf("缺 query_order 工具, got %v", keysOf(byName))
	}
	if qo.Category() != tooluse.CategoryBusiness {
		t.Fatalf("桥接工具应归业务类, got %v", qo.Category())
	}
	// C1-3 能力标签→风险映射：query_* → readonly。
	if lvl, declared := tooluse.EffectiveRisk(qo); lvl != tooluse.RiskReadonly || !declared {
		t.Fatalf("query_order 应声明 readonly, got %q declared=%v", lvl, declared)
	}
	// C1-2 schema 冻结：ParamMap 键为入参，string 型，description 标注映射目标。
	params := qo.Parameters()
	if params.Type != "object" || len(params.Properties) != 1 {
		t.Fatalf("schema 应恰 1 个属性, got %+v", params)
	}
	if p, ok := params.Properties["order_id"]; !ok || p.Type != "string" || p.Description != "映射到平台参数 platform_order_id" {
		t.Fatalf("order_id 参数 schema 不符: %+v", params.Properties["order_id"])
	}

	// C1-3：modify_order → high_write + WithApproval 强制（沿改单先例）。
	mo, ok := byName["connector.bridge_fakemart.modify_order"]
	if !ok {
		t.Fatalf("缺 modify_order 工具, got %v", keysOf(byName))
	}
	if lvl, _ := tooluse.EffectiveRisk(mo); lvl != tooluse.RiskHighWrite {
		t.Fatalf("modify_order 应为 high_write, got %q", lvl)
	}
	if !tooluse.IsApprovalWrapped(mo) {
		t.Fatal("RiskHighWrite 端点必须被 WithApproval 包装（改单先例；审批面在位可断言）")
	}
	if tooluse.IsApprovalWrapped(qo) {
		t.Fatal("readonly 端点不应被审批包装（映射规则：仅 RiskHighWrite 强制）")
	}
}

func keysOf(m map[string]tooluse.Tool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------- 判据 2（硬判据）：装饰器链全覆盖 —— 走真实执行面 ----------

func TestBridge_ExecutesThroughFullDecoratorChain(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"order_id":"FM-1","status":"shipped"}`))
	}))
	defer upstream.Close()

	// 桥接（测试接缝 Executor 放行环回上游）→ 注册进注册中心 → executor 建链。
	tools, _, _ := BridgePresetsWithExecutor(NewExecutorWithValidator(func(string) error { return nil }),
		ConnectorPreset{
			Name:   "chain_fakemart",
			Config: ConnectorConfig{BaseURL: upstream.URL, Provider: "generic", Auth: ConnectorAuth{Type: "bearer", Key: "k"}},
			Endpoints: []EndpointConfig{
				{Name: "query_order", Method: "GET", Path: "/api/v1/orders", Capabilities: []string{"query_order"},
					ParamMap: map[string]string{"order_id": "platform_order_id"}},
				{Name: "modify_order", Method: "POST", Path: "/api/v1/orders/modify", Capabilities: []string{"modify_order"}},
			},
		})
	reg := tooluse.NewToolRegistry()
	for _, tl := range tools {
		if err := reg.Register(tl); err != nil {
			t.Fatalf("注册失败: %v", err)
		}
	}
	limiter := &recordingLimiter{}
	checker := &recordingPermissionChecker{}
	audit := tooluse.NewMemoryAuditLogger(10)
	observer := tooluse.NewMemoryRiskObserver(10)
	exec := tooluse.NewToolExecutor(reg, tooluse.ToolExecutorConfig{
		DefaultTimeout:    3 * time.Second,
		PermissionChecker: checker,
		RateLimiter:       limiter,
		AuditLogger:       audit,
		CostTracker:       tooluse.NewMemoryCostTracker(),
		RiskGrants:        fakeAgentGrants{},
		RiskObserver:      observer,
	})

	// —— 只读工具：权限/限流/审计/风控观察四链齐开火 ——
	res := exec.Execute(context.Background(), tooluse.ExecuteRequest{
		ToolName: "connector.chain_fakemart.query_order",
		Args:     map[string]any{"order_id": "FM-1"},
		ToolCtx:  &tooluse.ToolContext{CallerID: "caller-1", AgentID: "agent-1"},
	})
	if res.Err != nil || !res.Success {
		t.Fatalf("只读工具执行失败: err=%v result=%+v", res.Err, res)
	}
	if hits.Load() != 1 {
		t.Fatalf("上游应恰命中 1 次, got %d", hits.Load())
	}
	if data, ok := res.Data.(map[string]any); !ok || data["ok"] != true {
		t.Fatalf("统一信封应进 ToolResult.Data, got %+v", res.Data)
	}
	if got := limiter.snapshot(); len(got) != 1 || got[0] != "caller-1:connector.chain_fakemart.query_order" {
		t.Fatalf("限流装饰器未按 CallerID:toolName 开火: %v", got)
	}
	if got := checker.snapshot(); len(got) != 1 || got[0] != "connector.chain_fakemart.query_order" {
		t.Fatalf("权限装饰器未按工具名开火: %v", got)
	}
	if audit.Count() == 0 {
		t.Fatal("审计装饰器未记录本次调用")
	}
	if last := audit.Entries(); last[len(last)-1].ToolName != "connector.chain_fakemart.query_order" {
		t.Fatalf("审计条目工具名不符: %+v", last[len(last)-1])
	}

	// —— 高危工具：WithApproval 在位（审批面）+ 风控门观察到 high_write 判定 ——
	res2 := exec.Execute(context.Background(), tooluse.ExecuteRequest{
		ToolName: "connector.chain_fakemart.modify_order",
		Args:     map[string]any{"note": "客户要求改地址"},
		ToolCtx:  &tooluse.ToolContext{CallerID: "caller-1", AgentID: "agent-1"},
	})
	if res2.Err != nil || !res2.Success {
		t.Fatalf("高危工具执行失败: err=%v result=%+v", res2.Err, res2)
	}
	if hits.Load() != 2 {
		t.Fatalf("上游应累计命中 2 次, got %d", hits.Load())
	}
	// 审批面：高危桥接工具被 WithApproval 包装（结构断言，见 TestBridge_FictionalPresetBecomesTools
	// 的 IsApprovalWrapped；此处复核执行路径拿到的仍是同一包装实例）。
	chainTool, err := reg.Get("connector.chain_fakemart.modify_order")
	if err != nil || !tooluse.IsApprovalWrapped(chainTool) {
		t.Fatalf("注册中心的高危桥接工具必须保持 WithApproval 包装: err=%v", err)
	}
	// 风控面：观察器应记到该 high_write 工具的判定（shadow 态 allowed=true，would_deny=1
	// 因 fakeAgentGrants 无白名单——这正是风控门的判据产出，不是拒绝）。
	var riskSeen bool
	for _, d := range observer.Snapshot() {
		if d.ToolName == "connector.chain_fakemart.modify_order" {
			riskSeen = true
			if d.Level != tooluse.RiskHighWrite || d.Reason != tooluse.RiskReasonWhitelistAbsent {
				t.Fatalf("高危工具风控判定不符: %+v", d)
			}
		}
	}
	if !riskSeen {
		t.Fatal("风控装饰器未观察到高危桥接工具的调用（治理旁路）")
	}
	// 审计/限流对高危工具同样开火（四链全覆盖的高危半边）。
	if got := limiter.snapshot(); len(got) != 2 {
		t.Fatalf("高危工具限流未开火: %v", got)
	}
	if got := checker.snapshot(); len(got) != 2 {
		t.Fatalf("高危工具权限未开火: %v", got)
	}
	if audit.Count() != 2 {
		t.Fatalf("高危工具审计未记录: %d 条", audit.Count())
	}
}

// ---------- 判据 3：负例三件套 ----------

func TestBridge_Negative_UnregisteredDialectNotBridged(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer upstream.Close()

	tools, skipped, presetErrs := BridgePresetsWithExecutor(NewExecutorWithValidator(func(string) error { return nil }),
		ConnectorPreset{
			Name:      "ghost_shop",
			Config:    ConnectorConfig{BaseURL: upstream.URL, Provider: "no_such_dialect"},
			Endpoints: []EndpointConfig{{Name: "query_any", Method: "GET", Path: "/", Capabilities: []string{"query_any"}}},
		})
	if len(tools) != 0 {
		t.Fatalf("未注册方言的 preset 不得产生工具: %v", namesOf(tools))
	}
	if len(presetErrs) != 1 || !contains(presetErrs[0], "未注册") {
		t.Fatalf("应报方言未注册: %v", presetErrs)
	}
	if len(skipped) != 0 {
		t.Fatalf("preset 级拒绝不应计入端点跳过: %v", skipped)
	}
	if hits.Load() != 0 {
		t.Fatalf("未注册方言不得外发: hits=%d", hits.Load())
	}
}

func TestBridge_Negative_MissingCapabilitiesNotBridged(t *testing.T) {
	tools, skipped, _ := BridgePresetsWithExecutor(NewExecutorWithValidator(func(string) error { return nil }),
		ConnectorPreset{
			Name:   "nocap_shop",
			Config: ConnectorConfig{BaseURL: "https://nocap.example.com", Provider: "generic"},
			Endpoints: []EndpointConfig{
				{Name: "no_caps", Method: "GET", Path: "/x", Capabilities: nil},
				{Name: "empty_caps", Method: "GET", Path: "/y", Capabilities: []string{}},
			},
		})
	if len(tools) != 0 {
		t.Fatalf("能力标签缺失的端点不得成为工具: %v", namesOf(tools))
	}
	if len(skipped) != 2 {
		t.Fatalf("两个缺能力标签端点都应跳过, got %v", skipped)
	}
	for _, s := range skipped {
		if !contains(s.Reason, "能力标签缺失") {
			t.Fatalf("跳过原因应说明能力标签缺失: %+v", s)
		}
	}
}

func TestBridge_Negative_SSRFRefusedNoOutflow(t *testing.T) {
	// 生产口径（NewExecutor 严格 SSRF）：桥接工具对环回/元数据基址执行即拒绝，
	// 且零外发（httptest 环回上游命中数恒 0——校验器在拨号前拦截）。
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer upstream.Close()

	tools, _, _ := BridgePresetsWithExecutor(NewExecutor(), // 生产构造：严格口径
		ConnectorPreset{
			Name:      "ssrf_shop",
			Config:    ConnectorConfig{BaseURL: upstream.URL, Provider: "generic"},
			Endpoints: []EndpointConfig{{Name: "query_leak", Method: "GET", Path: "/secret", Capabilities: []string{"query_leak"}}},
		})
	if len(tools) != 1 {
		t.Fatalf("桥接本身不受基址影响（SSRF 是执行期防线）, got %d", len(tools))
	}
	res, err := tools[0].Execute(context.Background(), nil)
	if err == nil || res.Success {
		t.Fatalf("环回基址执行必须被拒绝: err=%v res=%+v", err, res)
	}
	if !contains(err.Error(), "非公网") && !contains(err.Error(), "base_url") {
		t.Fatalf("拒绝原因应出自 SSRF 口径: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("SSRF 拒绝必须零外发: hits=%d", hits.Load())
	}
	// 元数据地址同样拒绝（复用 tooluse 既有口径）。
	tools2, _, _ := BridgePresetsWithExecutor(NewExecutor(),
		ConnectorPreset{
			Name:      "ssrf_meta",
			Config:    ConnectorConfig{BaseURL: "http://169.254.169.254/latest/meta-data", Provider: "generic"},
			Endpoints: []EndpointConfig{{Name: "query_meta", Method: "GET", Path: "/", Capabilities: []string{"query_meta"}}},
		})
	if _, err := tools2[0].Execute(context.Background(), nil); err == nil {
		t.Fatal("元数据地址必须被拒绝")
	}
}

// ---------- 桥接器装配面：Provider 形态 + 空表零行为 ----------

func TestBridge_ProviderAndEmptyRegistry(t *testing.T) {
	p := &BridgeProvider{}
	if p.Name() != "connector_bridge" || p.Category() != tooluse.CategoryBusiness {
		t.Fatalf("Provider 元信息不符: %s %v", p.Name(), p.Category())
	}

	// 好 preset + 坏 preset（方言未注册）同表：Provide 不返回 error（RegisterAll 对
	// Provider error 的语义是丢弃该 Provider 全部工具——无辜 preset 不得被拖下水），
	// 坏 preset 端点缺席即 fail-closed，其余照常成工具。
	RegisterPreset(ConnectorPreset{
		Name:      "prov_good",
		Config:    ConnectorConfig{BaseURL: "https://prov.example.com", Provider: "generic"},
		Endpoints: []EndpointConfig{{Name: "query_thing", Method: "GET", Path: "/t", Capabilities: []string{"query_thing"}}},
	})
	RegisterPreset(ConnectorPreset{
		Name:      "prov_bad_dialect",
		Config:    ConnectorConfig{BaseURL: "https://bad.example.com", Provider: "no_such_dialect_prov"},
		Endpoints: []EndpointConfig{{Name: "query_bad", Method: "GET", Path: "/b", Capabilities: []string{"query_bad"}}},
	})
	provided, err := (&BridgeProvider{Executor: NewExecutorWithValidator(func(string) error { return nil })}).Provide(
		tooluse.ProviderContext{})
	if err != nil {
		t.Fatalf("Provide 不得因单个坏 preset 报错（会丢弃无辜工具）: %v", err)
	}
	got := namesOf(provided)
	if !containsSlice(got, "connector.prov_good.query_thing") {
		t.Fatalf("好 preset 工具应在列: %v", got)
	}
	if containsSlice(got, "connector.prov_bad_dialect.query_bad") {
		t.Fatalf("方言未注册的 preset 端点不得成为工具: %v", got)
	}
}

func containsSlice(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// ---------- 边界：sanitize 与重名 ----------

func TestBridge_SanitizeAndDuplicatePreset(t *testing.T) {
	// 非 ASCII 端点名：折叠为下划线后 trim 为空 → fail-closed 跳过（C1-1）。
	tools, skipped, _ := BridgePresetsWithExecutor(NewExecutorWithValidator(func(string) error { return nil }),
		ConnectorPreset{
			Name:   "Sanitize-Shop.01",
			Config: ConnectorConfig{BaseURL: "https://s.example.com", Provider: "generic"},
			Endpoints: []EndpointConfig{
				{Name: "查询订单", Method: "GET", Path: "/q", Capabilities: []string{"query_order"}},
				{Name: "Query-Stock 2", Method: "GET", Path: "/s", Capabilities: []string{"query_stock"}},
			},
		})
	if len(tools) != 1 || tools[0].Name() != "connector.sanitize_shop_01.query_stock_2" {
		t.Fatalf("混合名应折叠为下划线、纯非 ASCII 应跳过, got tools=%v skipped=%v", namesOf(tools), skipped)
	}
	if len(skipped) != 1 || skipped[0].Endpoint != "查询订单" {
		t.Fatalf("纯非 ASCII 端点名应跳过, got %v", skipped)
	}

	// 重名 preset panic（配置错误即暴露，沿方言注册表同款）。
	defer func() {
		if recover() == nil {
			t.Fatal("重复注册 preset 应 panic")
		}
	}()
	RegisterPreset(ConnectorPreset{Name: "dup_test_preset"})
	RegisterPreset(ConnectorPreset{Name: "dup_test_preset"})
}

// ---------- 执行语义：平台错误原样进 error 面 ----------

func TestBridge_PlatformErrorSurfaces(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer upstream.Close()

	tools, _, _ := BridgePresetsWithExecutor(NewExecutorWithValidator(func(string) error { return nil }),
		ConnectorPreset{
			Name:      "err_shop",
			Config:    ConnectorConfig{BaseURL: upstream.URL, Provider: "generic"},
			Endpoints: []EndpointConfig{{Name: "query_boom", Method: "GET", Path: "/boom", Capabilities: []string{"query_boom"}}},
		})
	res, err := tools[0].Execute(context.Background(), nil)
	if err == nil || res.Success {
		t.Fatal("平台非 200 必须进 error 面，不得包装成成功")
	}
	if !errors.Is(err, err) || !contains(res.Error, "status 502") {
		t.Fatalf("错误应含平台状态: %v / %s", err, res.Error)
	}
}

func namesOf(tools []tooluse.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name())
	}
	return out
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
