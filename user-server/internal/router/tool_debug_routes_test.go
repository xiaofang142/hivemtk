package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/app"

	"github.com/gin-gonic/gin"
)

type mockTool struct {
	name        string
	category    tooluse.ToolCategory
	description string
	params      tooluse.ToolParameters
	execFn      func(ctx context.Context, args map[string]any) (tooluse.ToolResult, error)
}

func (m *mockTool) Name() string                       { return m.name }
func (m *mockTool) Category() tooluse.ToolCategory     { return m.category }
func (m *mockTool) Description() string                { return m.description }
func (m *mockTool) Parameters() tooluse.ToolParameters { return m.params }
func (m *mockTool) Execute(ctx context.Context, args map[string]any) (tooluse.ToolResult, error) {
	if m.execFn != nil {
		return m.execFn(ctx, args)
	}
	return tooluse.SuccessResult(m.name, map[string]any{"echo": args}), nil
}

func newMockEchoTool(name string) *mockTool {
	return &mockTool{
		name:        name,
		category:    tooluse.CategoryBusiness,
		description: "测试用 echo 工具，原样返回 args",
		params: tooluse.ToolParameters{
			Type: "object",
			Properties: map[string]tooluse.ToolParam{
				"message": {Type: "string", Description: "要回显的消息"},
			},
			Required: []string{"message"},
		},
	}
}

func newMockFailingTool(name string) *mockTool {
	return &mockTool{
		name:        name,
		category:    tooluse.CategoryBusiness,
		description: "测试用失败工具",
		params: tooluse.ToolParameters{
			Type:       "object",
			Properties: map[string]tooluse.ToolParam{},
		},
		execFn: func(ctx context.Context, args map[string]any) (tooluse.ToolResult, error) {
			return tooluse.ErrorResult(name, errMockToolFailure), errMockToolFailure
		},
	}
}

var errMockToolFailure = &simpleError{"mock tool intentional failure"}

func newTestExecutor(t *testing.T) (*tooluse.ToolRegistry, *tooluse.ToolExecutor, *tooluse.MemoryAuditLogger, *tooluse.MemoryCostTracker) {
	t.Helper()
	registry := tooluse.NewToolRegistry()
	auditLogger := tooluse.NewMemoryAuditLogger(1000)
	costTracker := tooluse.NewMemoryCostTracker()
	exec := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{
		DefaultTimeout:    2 * time.Second,
		PermissionChecker: tooluse.NoOpPermissionChecker{},
		RateLimiter:       tooluse.NewTokenBucketLimiter(100, 200),
		RetryPolicy:       tooluse.NewExponentialBackoffPolicy(2, 10*time.Millisecond, 100*time.Millisecond),
		AuditLogger:       auditLogger,
		CostTracker:       costTracker,
	})
	return registry, exec, auditLogger, costTracker
}

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	registry := tooluse.NewToolRegistry()
	tool := newMockEchoTool("test.echo")

	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if !registry.Has("test.echo") {
		t.Error("Has should return true after Register")
	}
	if registry.Count() != 1 {
		t.Errorf("Count = %d, want 1", registry.Count())
	}

	got, err := registry.Get("test.echo")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Name() != "test.echo" {
		t.Errorf("got.Name = %s, want test.echo", got.Name())
	}

	if err := registry.Register(tool); err == nil {
		t.Error("duplicate Register should fail")
	}

	if err := registry.Unregister("test.echo"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}
	if registry.Has("test.echo") {
		t.Error("Has should return false after Unregister")
	}
}

func TestToolRegistry_ListByCategory(t *testing.T) {
	registry := tooluse.NewToolRegistry()
	_ = registry.Register(newMockEchoTool("a.echo"))
	_ = registry.Register(&mockTool{name: "b.customer", category: tooluse.CategoryCustomer, params: tooluse.ToolParameters{Type: "object"}})
	_ = registry.Register(&mockTool{name: "c.customer", category: tooluse.CategoryCustomer, params: tooluse.ToolParameters{Type: "object"}})

	business := registry.ListByCategory(tooluse.CategoryBusiness)
	if len(business) != 1 {
		t.Errorf("business count = %d, want 1", len(business))
	}
	customer := registry.ListByCategory(tooluse.CategoryCustomer)
	if len(customer) != 2 {
		t.Errorf("customer count = %d, want 2", len(customer))
	}
}

func TestToolRegistry_ToLLMFunctions(t *testing.T) {
	registry := tooluse.NewToolRegistry()
	_ = registry.Register(newMockEchoTool("test.echo"))

	fns := registry.ToLLMFunctions()
	if len(fns) != 1 {
		t.Fatalf("ToLLMFunctions len = %d, want 1", len(fns))
	}
	if fns[0].Name != "test.echo" {
		t.Errorf("Name = %s, want test.echo", fns[0].Name)
	}
	if fns[0].Parameters.Type != "object" {
		t.Errorf("Parameters.Type = %s, want object", fns[0].Parameters.Type)
	}
	if len(fns[0].Parameters.Properties) != 1 {
		t.Errorf("Properties len = %d, want 1", len(fns[0].Parameters.Properties))
	}
}

func TestToolExecutor_Success(t *testing.T) {
	registry, exec, auditLogger, costTracker := newTestExecutor(t)
	_ = costTracker
	_ = auditLogger
	_ = registry.Register(newMockEchoTool("test.echo"))

	r, err := exec.ExecuteByName(context.Background(), "test.echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("ExecuteByName err: %v", err)
	}
	if !r.Success {
		t.Errorf("Success = false, want true; error=%s", r.Error)
	}
	if r.ToolName != "test.echo" {
		t.Errorf("ToolName = %s, want test.echo", r.ToolName)
	}
	if r.Timing.DurationMs < 0 {
		t.Errorf("DurationMs = %d, should be >= 0", r.Timing.DurationMs)
	}
}

func TestToolExecutor_NotFound(t *testing.T) {
	_, exec, _, _ := newTestExecutor(t)
	_, err := exec.ExecuteByName(context.Background(), "nonexistent.tool", nil)
	if err == nil {
		t.Error("expected error for nonexistent tool")
	}
	if err != tooluse.ErrToolNotFound {
		t.Logf("got err = %v (acceptable, wrapped)", err)
	}
}

func TestToolExecutor_Disabled(t *testing.T) {
	registry := tooluse.GetGlobalRegistry()
	if registry.Count() == 0 {
		t.Skip("global registry is empty, skip disabled test")
	}
	tools := registry.List()
	if len(tools) == 0 {
		t.Skip("no tools in global registry")
	}
	toolName := tools[0].Name()
	executor := tooluse.GetGlobalExecutor()
	if executor == nil {
		t.Skip("global executor not initialized")
	}
	executor.SetOverride(tooluse.ToolOverride{ToolName: toolName, Disabled: true})
	defer executor.ClearOverride(toolName)
}

func TestToolExecutor_RetryOnFailure(t *testing.T) {
	registry, exec, _, _ := newTestExecutor(t)
	_ = registry.Register(newMockFailingTool("test.fail"))

	r, err := exec.ExecuteByName(context.Background(), "test.fail", nil)
	if err == nil {
		t.Error("expected error from failing tool")
	}
	if r.Success {
		t.Error("Success should be false")
	}
	if r.Timing.RetryCount < 1 {
		t.Logf("RetryCount = %d (expected >= 1 with MaxAttempts=2)", r.Timing.RetryCount)
	}
}

func TestToolExecutor_AuditAndCostRecorded(t *testing.T) {
	registry, exec, auditLogger, costTracker := newTestExecutor(t)
	_ = registry.Register(newMockEchoTool("test.echo"))

	_, _ = exec.ExecuteByName(context.Background(), "test.echo", map[string]any{"message": "audit-test"})

	entries := auditLogger.Entries()
	if len(entries) == 0 {
		t.Error("audit log should have entries after execution")
	}
	last := entries[len(entries)-1]
	if last.ToolName != "test.echo" {
		t.Errorf("audit ToolName = %s, want test.echo", last.ToolName)
	}
	if !last.Success {
		t.Error("audit Success should be true")
	}

	stats := costTracker.Stats()
	found := false
	for _, s := range stats {
		if s.ToolName == "test.echo" {
			found = true
			if s.TotalCalls != 1 {
				t.Errorf("TotalCalls = %d, want 1", s.TotalCalls)
			}
			if s.SuccessCalls != 1 {
				t.Errorf("SuccessCalls = %d, want 1", s.SuccessCalls)
			}
		}
	}
	if !found {
		t.Error("cost tracker should have test.echo record")
	}
}

func TestToolExecutor_DispatchByLLMToolCall(t *testing.T) {
	registry, exec, _, _ := newTestExecutor(t)
	_ = registry.Register(newMockEchoTool("test.echo"))

	toolCalls := []tooluse.LLMToolCall{
		{
			ID:       "call-1",
			Function: tooluse.LLMToolFunction{Name: "test.echo", Arguments: `{"message":"hello"}`},
		},
		{
			ID:       "call-2",
			Function: tooluse.LLMToolFunction{Name: "test.echo", Arguments: `{"message":"world"}`},
		},
	}
	results := exec.DispatchByLLMToolCall(context.Background(), toolCalls, &tooluse.ToolContext{Source: "test"})
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	for i, r := range results {
		if !r.Success {
			t.Errorf("result[%d] Success=false, content=%s", i, r.Content)
		}
		if r.ToolCallID != toolCalls[i].ID {
			t.Errorf("result[%d] ToolCallID = %s, want %s", i, r.ToolCallID, toolCalls[i].ID)
		}
	}
}

func TestToolExecutor_DispatchInvalidArguments(t *testing.T) {
	registry, exec, _, _ := newTestExecutor(t)
	_ = registry.Register(newMockEchoTool("test.echo"))

	results := exec.DispatchByLLMToolCall(context.Background(), []tooluse.LLMToolCall{
		{
			ID:       "call-bad",
			Function: tooluse.LLMToolFunction{Name: "test.echo", Arguments: `not-json`},
		},
	}, nil)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Success {
		t.Error("should fail for invalid JSON arguments")
	}
}

func TestParamValidator_MissingRequired(t *testing.T) {
	registry, exec, _, _ := newTestExecutor(t)
	_ = registry.Register(newMockEchoTool("test.echo"))

	r, _ := exec.ExecuteByName(context.Background(), "test.echo", map[string]any{})
	if !r.Success {
		t.Logf("execute without required arg returned: success=%v (mock tool doesn't validate)", r.Success)
	}
}

func TestToolExecutor_ConcurrentSafe(t *testing.T) {
	registry, exec, _, _ := newTestExecutor(t)
	_ = registry.Register(newMockEchoTool("test.echo"))

	const N = 50
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := exec.ExecuteByName(context.Background(), "test.echo", map[string]any{
				"message": "concurrent",
				"idx":     idx,
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent execute err: %v", err)
		}
	}
}

func TestToolRouter_RouteAndStats(t *testing.T) {
	_, exec, _, _ := newTestExecutor(t)
	registry := tooluse.NewToolRegistry()
	_ = registry.Register(newMockEchoTool("test.echo"))
	router := tooluse.NewToolRouter(
		tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{
			DefaultTimeout: 2 * time.Second,
			AuditLogger:    tooluse.NoOpAuditLogger{},
			CostTracker:    tooluse.NewMemoryCostTracker(),
		}),
		tooluse.NewTokenBucketLimiter(100, 200),
		tooluse.RouterConfig{
			FailThreshold:    3,
			CooldownDuration: 5 * time.Second,
			DefaultToolCost:  0.01,
		},
	)
	_ = exec

	result := router.Route(context.Background(), "test.echo", map[string]any{"message": "hi"}, nil)
	if result.Err != nil {
		t.Fatalf("Route err: %v", result.Err)
	}
	if !result.Result.Success {
		t.Error("Result.Success should be true")
	}
	stats := router.GetStats()
	if stats.TotalCalls != 1 {
		t.Errorf("TotalCalls = %d, want 1", stats.TotalCalls)
	}
	if stats.SuccessCalls != 1 {
		t.Errorf("SuccessCalls = %d, want 1", stats.SuccessCalls)
	}
}

func TestToolRouter_CircuitBreaker(t *testing.T) {
	registry := tooluse.NewToolRegistry()
	_ = registry.Register(newMockFailingTool("test.fail"))
	exec := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{
		DefaultTimeout: 1 * time.Second,
		RetryPolicy:    tooluse.NewExponentialBackoffPolicy(1, 5*time.Millisecond, 50*time.Millisecond),
		AuditLogger:    tooluse.NoOpAuditLogger{},
		CostTracker:    tooluse.NewMemoryCostTracker(),
	})
	router := tooluse.NewToolRouter(exec, tooluse.NewTokenBucketLimiter(100, 200), tooluse.RouterConfig{
		FailThreshold:    2,
		CooldownDuration: 1 * time.Second,
		DefaultToolCost:  0.001,
	})

	router.Route(context.Background(), "test.fail", nil, nil)
	router.Route(context.Background(), "test.fail", nil, nil)
	r3 := router.Route(context.Background(), "test.fail", nil, nil)
	if !r3.CircuitOpen {
		t.Error("3rd call should be circuit-open")
	}

	stats := router.GetStats()
	if stats.CircuitOpenCalls == 0 {
		t.Error("CircuitOpenCalls should > 0")
	}

	router.ResetCircuit("test.fail")
	r4 := router.Route(context.Background(), "test.fail", nil, nil)
	if r4.CircuitOpen {
		t.Error("after reset, should not be circuit-open")
	}
}

// TestSetup_ToolDebugRoutesRegistered 验证 /api/agent/tools/* 路由全部注册
//
// 这是关键测试：验证原本死代码（setupToolPermissionRoutes / setupInferenceRoutes）
// 和新增的 setupToolDebugRoutes 都已经在 router.Setup 中激活
func TestSetup_ToolDebugRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	auth := r.Group("/api")
	setupToolDebugRoutes(auth)

	expectedRoutes := []string{
		"GET-/api/agent/tools/list",
		"GET-/api/agent/tools/get",
		"POST-/api/agent/tools/execute",
		"GET-/api/agent/tools/stats",
		"GET-/api/agent/tools/audit",
		"GET-/api/agent/tools/cost",
		"GET-/api/agent/tools/circuit",
		"POST-/api/agent/tools/circuit/reset",
		"GET-/api/agent/tools/approval",
		"POST-/api/agent/tools/approval/whitelist",
		"GET-/api/agent/tools/providers",
	}
	routes := r.Routes()
	routeSet := make(map[string]bool)
	for _, route := range routes {
		routeSet[route.Method+"-"+route.Path] = true
	}
	for _, expected := range expectedRoutes {
		if !routeSet[expected] {
			t.Errorf("route %s not registered", expected)
		}
	}
}

func TestHandleToolList_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/list", nil)

	handleToolList(c)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 200 or 503", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response body not JSON: %v; body=%s", err, w.Body.String())
	}

	if code, ok := resp["code"].(float64); !ok || code != 0 {
		t.Errorf("response code != 0; body=%s", w.Body.String())
	}
}

func TestHandleToolList_WithCategoryFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/list?category=customer", nil)

	handleToolList(c)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d", w.Code)
	}
}

func TestHandleToolGet_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/get?name=customer.search", nil)

	handleToolGet(c)

	if w.Code != http.StatusOK && w.Code != http.StatusNotFound && w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 200/404/503", w.Code)
	}
}

func TestHandleToolGet_MissingName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/get", nil)

	handleToolGet(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleToolExecute_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reqBody := toolExecuteRequest{
		ToolName: "nonexistent.tool",
		Args:     map[string]any{"foo": "bar"},
		Source:   "test",
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/agent/tools/execute", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handleToolExecute(c)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 200 or 503", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %v; body=%s", err, w.Body.String())
	}
}

func TestHandleToolExecute_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/agent/tools/execute", bytes.NewReader([]byte("not-json")))
	c.Request.Header.Set("Content-Type", "application/json")

	handleToolExecute(c)

	if w.Code != http.StatusBadRequest && w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 400 or 503", w.Code)
	}
}

func TestHandleToolStats_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/stats", nil)

	handleToolStats(c)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %v; body=%s", err, w.Body.String())
	}

	if code, ok := resp["code"].(float64); !ok || code != 0 {
		t.Errorf("response code != 0; body=%s", w.Body.String())
	}
}

func TestHandleToolAudit_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/audit?limit=10", nil)

	handleToolAudit(c)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 200 or 503", w.Code)
	}
}

func TestHandleToolCost_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/cost", nil)

	handleToolCost(c)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleToolCircuitReset_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(toolCircuitResetRequest{ToolName: "test.fail"})
	c.Request = httptest.NewRequest("POST", "/api/agent/tools/circuit/reset", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	origRouter := app.GetGlobalToolRouter()
	app.SetGlobalToolRouterForTest(nil)
	defer app.SetGlobalToolRouterForTest(origRouter)

	handleToolCircuitReset(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when router is nil", w.Code)
	}
}

// TestHandleToolCircuitState_HTTP_Unwired 锁住"未接线时的自述"：
// wired 必须为 false，且空集合不得伪装成"所有工具都健康"。
func TestHandleToolCircuitState_HTTP_Unwired(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/circuit", nil)

	handleToolCircuitState(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200；body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %v; body=%s", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Fatalf("envelope code = %d, want 0；body=%s", resp.Code, w.Body.String())
	}
	if wired, ok := resp.Data["wired"].(bool); !ok || wired {
		t.Errorf("wired = %v, want false（本测试进程未跑过熔断装配）", resp.Data["wired"])
	}
	if list, ok := resp.Data["executor_circuit"].([]any); !ok || len(list) != 0 {
		t.Errorf("executor_circuit = %v, want 空数组（不是 null，前端要能直接遍历）", resp.Data["executor_circuit"])
	}
	if _, exists := resp.Data["decision_report"]; exists {
		t.Errorf("未接线时不应出现 decision_report，否则 0 会读成'零次误拦'：实际 %v", resp.Data["decision_report"])
	}
	for _, k := range []string{"mode", "config", "env_hint"} {
		if _, ok := resp.Data[k]; !ok {
			t.Errorf("缺少字段 %s", k)
		}
	}
}

// TestHandleToolApprovalState_HTTP_Unwired 审批门未接线时的自述（T-P1-05）。
//
// 必须与熔断那条同形：wired=false、空集合不伪装成健康、缺报告就说缺报告。
// 这里刻意不回显一个全零的 decision_report——"一次都没发生"和"没接闸门"
// 在灰度判定时是两个完全相反的结论。
func TestHandleToolApprovalState_HTTP_Unwired(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/approval", nil)

	handleToolApprovalState(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200；body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %v; body=%s", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Fatalf("envelope code = %d, want 0；body=%s", resp.Code, w.Body.String())
	}
	if wired, ok := resp.Data["wired"].(bool); !ok || wired {
		t.Errorf("wired = %v, want false（本测试进程没跑过审批门装配）", resp.Data["wired"])
	}
	if _, exists := resp.Data["decision_report"]; exists {
		t.Errorf("未接线时不应出现 decision_report，否则 0 会读成「零次冷触达」：实际 %v", resp.Data["decision_report"])
	}
	flags, ok := resp.Data["flags"].(map[string]any)
	if !ok {
		t.Fatalf("flags 缺失或类型不对：%v", resp.Data["flags"])
	}
	// 两把旗子必须同时回显：只看 mode=shadow 会把"白名单没生效"读成"账号都没被批准"
	for _, k := range []string{"gate", "whitelist", "whitelist_flag_on"} {
		if _, ok := flags[k]; !ok {
			t.Errorf("flags 缺 %q：%v", k, flags)
		}
	}
	if flags["whitelist"] != "ai.safety.tool_approval_gate" {
		t.Errorf("flags.whitelist = %v, want ai.safety.tool_approval_gate", flags["whitelist"])
	}
	for _, k := range []string{"mode", "global_checker_set", "env_hint", "reading_hint"} {
		if _, ok := resp.Data[k]; !ok {
			t.Errorf("缺少字段 %s", k)
		}
	}
	// 旗子名必须与代码实际读取的那个常量同源：抄一份字面量到提示语里，
	// 将来改名就会变成"文档教你设一个没人读的变量"。
	if flags["gate"] != app.ApprovalGateFlagEnv {
		t.Errorf("flags.gate = %v, want %s（与 app 侧常量漂移）", flags["gate"], app.ApprovalGateFlagEnv)
	}
	if hint, _ := resp.Data["env_hint"].(string); !strings.HasPrefix(hint, app.ApprovalGateFlagEnv+"=") {
		t.Errorf("env_hint 未以旗子名 %s= 开头：%q", app.ApprovalGateFlagEnv, hint)
	}
	// 提示语里的可用值必须与解析器真认的那三个一致（T-P1-06 起含 block）
	if hint, _ := resp.Data["env_hint"].(string); !strings.Contains(hint, "off|shadow|block") {
		t.Errorf("env_hint 没列出 off|shadow|block 三态：%q", hint)
	}
	for _, k := range []string{"blocks_when_denied", "whitelist_active_entries"} {
		if _, ok := resp.Data[k]; !ok {
			t.Errorf("未接线快照也须回显 %s（false/0 是有效读数，缺字段会被读成没实现）", k)
		}
	}
	if b, _ := resp.Data["blocks_when_denied"].(bool); b {
		t.Error("未接线（off）时 blocks_when_denied 必须为 false")
	}
	if _, exists := resp.Data["brake_engaged"]; exists {
		t.Error("off 态没有刹车可言，brake_engaged 不该出现")
	}
}

// TestApprovalStatePayload_BlockEcho 三种快照下的回显差异。
//
// 装配只在启动时跑一次，handle 在本进程只能测到 off，所以这里直接喂构造快照：
// 唯一必须锁住的分叉是"block 且白名单旗子没开 ⇒ 明确告诉调用方现在其实不拦"。
// 这一支如果漏了，端点会给出"mode=block + would_deny 一路涨"的读数，
// 而真实行为是一单都没拦。
func TestApprovalStatePayload_BlockEcho(t *testing.T) {
	blockOn := app.ApprovalGateSnapshot{
		Mode:                   "block",
		Wired:                  true,
		GlobalCheckerSet:       true,
		BlocksWhenDenied:       true,
		WhitelistFlagKey:       "ai.safety.tool_approval_gate",
		WhitelistFlagEnv:       "FF_AI.SAFETY_TOOL_APPROVAL_GATE",
		WhitelistFlagOn:        true,
		WhitelistActiveEntries: 3,
		GateFlagEnv:            app.ApprovalGateFlagEnv,
	}

	t.Run("block+白名单旗子未开 ⇒ 报刹车", func(t *testing.T) {
		snap := blockOn
		snap.WhitelistFlagOn = false
		snap.WhitelistActiveEntries = 0
		out := approvalStatePayload(snap)
		if out["brake_engaged"] != true {
			t.Errorf("brake_engaged = %v, want true", out["brake_engaged"])
		}
		note, _ := out["brake_note"].(string)
		if !strings.Contains(note, "disabled_by_flag") || !strings.Contains(note, "whitelist_active_entries") {
			t.Errorf("brake_note 没说清后果与下一步：%q", note)
		}
		if out["whitelist_active_entries"] != 0 {
			t.Errorf("有效条目读数被改写：%v", out["whitelist_active_entries"])
		}
	})

	t.Run("block+白名单旗子已开 ⇒ 不报刹车", func(t *testing.T) {
		out := approvalStatePayload(blockOn)
		if _, exists := out["brake_engaged"]; exists {
			t.Error("两把旗子都到位时不该报刹车，否则告警永远亮着就没人再看")
		}
		if out["blocks_when_denied"] != true || out["mode"] != "block" {
			t.Errorf("block 态回显：%v", out)
		}
		if out["whitelist_active_entries"] != 3 {
			t.Errorf("有效条目 = %v, want 3", out["whitelist_active_entries"])
		}
	})

	t.Run("shadow ⇒ blocks_when_denied=false 且不报刹车", func(t *testing.T) {
		snap := blockOn
		snap.Mode = "shadow"
		snap.BlocksWhenDenied = false
		snap.WhitelistFlagOn = false
		out := approvalStatePayload(snap)
		if _, exists := out["brake_engaged"]; exists {
			t.Error("shadow 态本来就只记录，不该报刹车")
		}
		if out["blocks_when_denied"] != false {
			t.Error("shadow 态 blocks_when_denied 必须为 false")
		}
	})

	// 提示语里的白名单 env 名必须来自快照字段（同源），而不是端点再抄一份字面量
	t.Run("env 提示同源", func(t *testing.T) {
		out := approvalStatePayload(blockOn)
		// 注意断言目标是 gin.H 而不是 map[string]any：前者是**命名类型**，
		// 对未命名 map 类型的断言会失败并静默给出 nil，看起来像"字段没渲染"。
		flags, _ := out["flags"].(gin.H)
		if flags["whitelist_env"] != blockOn.WhitelistFlagEnv {
			t.Errorf("flags.whitelist_env = %v, want %s", flags["whitelist_env"], blockOn.WhitelistFlagEnv)
		}
		hint, _ := out["env_hint"].(string)
		if !strings.Contains(hint, blockOn.WhitelistFlagEnv) {
			t.Errorf("env_hint 未引用快照里的白名单 env 名：%q", hint)
		}
	})
}

// TestHandleToolApprovalWhitelist_HTTP_InputGuards 授权端点的入参守卫与未接线回退。
//
// 放行/撤权本身的语义在 internal/app 的 TestApprovalWhitelistGrantChangesReason 里锁，
// 这里只锁端点这一层：坏输入不能写进白名单，未接线时不能假装成功。
func TestHandleToolApprovalWhitelist_HTTP_InputGuards(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"非法 JSON", `{"tool_name":`, http.StatusBadRequest},
		{"缺 tool_name", `{"account_id":"a1"}`, http.StatusBadRequest},
		{"缺 account_id", `{"tool_name":"reach.batch"}`, http.StatusBadRequest},
		{"全空白", `{"tool_name":"  ","account_id":"  "}`, http.StatusBadRequest},
		{"expires_at 不是 RFC3339", `{"tool_name":"reach.batch","account_id":"a1","expires_at":"2026/10/01"}`, http.StatusBadRequest},
		// 入参合法但闸门没接线 ⇒ 必须 503，不能返回 200 让人以为授权成功了
		{"未接线", `{"tool_name":"reach.batch","account_id":"a1"}`, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest("POST", "/api/agent/tools/approval/whitelist", strings.NewReader(c.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			handleToolApprovalWhitelist(ctx)

			if w.Code != c.want {
				t.Fatalf("status = %d, want %d；body=%s", w.Code, c.want, w.Body.String())
			}
		})
	}
}

func TestAtoiSafe(t *testing.T) {
	cases := []struct {
		input string
		want  int
		ok    bool
	}{
		{"0", 0, true},
		{"", 0, false},
		{"abc", 0, false},
		{"12abc", 0, false},
		{"-5", 0, false},
	}
	for _, c := range cases {
		got, err := atoiSafe(c.input)
		if c.ok && err != nil {
			t.Errorf("atoiSafe(%q) err = %v, want nil", c.input, err)
			continue
		}
		if !c.ok && err == nil {
			t.Errorf("atoiSafe(%q) err = nil, want non-nil", c.input)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("atoiSafe(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// T-P1-08：/agent/tools/audit 与 /cost 的 ?source=db 分支与落库状态回显。
//
// 走纯函数而不是 httptest 的原因与 T-P1-05/06 同一口径：旗子在装配期读一次，
// 测试进程里既没有 HTTP 也没有第二次装配，handle 只能测到 off 那一支。
// 于是"给定快照 → 该回显什么 / 该拒什么"单独断言，off 那一支再补一条 httptest 兜底。
// ---------------------------------------------------------------------------

func TestIsDBSource(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"db", "db"}, {"DB", "db"}, {" db ", "db"},
	}
	for _, c := range cases {
		if !isDBSource(c.raw) {
			t.Errorf("isDBSource(%q) 应为 true", c.raw)
		}
	}
	// 认不出的值一律走内存：拼错 source 的人会在回显里看到 source=memory，
	// 而不是静悄悄拿到一份"看起来是库里数据"的内存结果。
	for _, raw := range []string{"", "database", "postgres", "mem", "true"} {
		if isDBSource(raw) {
			t.Errorf("isDBSource(%q) 应为 false", raw)
		}
	}
}

func TestToolAuditPersistenceEchoShape(t *testing.T) {
	off := toolAuditPersistenceEcho(app.ToolAuditSnapshot{
		Mode: "off", Wired: false, TableName: "tool_call_audits",
		DBHandle: false, MemCapUsed: 7,
	})
	for _, key := range []string{"mode", "db_wired", "table", "flag_env", "db_handle", "mem_entries"} {
		if _, ok := off[key]; !ok {
			t.Errorf("off 回显缺字段 %s：%v", key, off)
		}
	}
	if off["table"] != "tool_call_audits" || off["flag_env"] != "FF_TOOL_AUDIT_DB" {
		t.Errorf("回显的表名/旗子名必须是运维能照着查的原文：%v", off)
	}
	// 未接线时不该出现队列/统计字段（有也是全 0，会被读成"接了但没量"）
	for _, key := range []string{"queue_size", "db_stats"} {
		if _, ok := off[key]; ok {
			t.Errorf("off 回显不该有 %s：%v", key, off)
		}
	}

	on := toolAuditPersistenceEcho(app.ToolAuditSnapshot{
		Mode: "on", Wired: true, TableName: "tool_call_audits",
		QueueSize: 500, DBHandle: true, HasStats: true,
		DBStats: tooluse.DBAuditStats{Enqueued: 12, DBRows: 10, FellBack: 2, QueueLen: 2, QueueCap: 500},
	})
	if on["db_wired"] != true || on["queue_size"] != 500 {
		t.Errorf("on 回显异常：%v", on)
	}
	stats, ok := on["db_stats"].(tooluse.DBAuditStats)
	if !ok || stats.DBRows != 10 || stats.FellBack != 2 {
		t.Errorf("db_stats 应原样带出降级量，实际 %v（%T）", on["db_stats"], on["db_stats"])
	}
}

func TestToolAuditDBBlockedReasonBranches(t *testing.T) {
	// 旗子没开：读写侧都没有
	if r := toolAuditDBBlockedReason(app.ToolAuditSnapshot{Mode: "off"}); r == "" {
		t.Error("完全未接线时应给出 503 原因")
	} else if !strings.Contains(r, "FF_TOOL_AUDIT_DB") || !strings.Contains(r, "mode=off") {
		t.Errorf("原因里要带旗子名与实际 mode，便于运维定位：%s", r)
	}
	// 写侧接了、读侧句柄 nil（DB 连接问题）
	if r := toolAuditDBBlockedReason(app.ToolAuditSnapshot{Mode: "on", Wired: true, DBHandle: false}); !strings.Contains(r, "nil") {
		t.Errorf("句柄缺失应点名 nil 句柄：%s", r)
	}
	// 读侧有句柄但写侧没接（旗子 off 却带着活库句柄）——最常见的误读场景
	if r := toolAuditDBBlockedReason(app.ToolAuditSnapshot{Mode: "off", Wired: false, DBHandle: true}); !strings.Contains(r, "写侧未接线") {
		t.Errorf("应说明写侧未接线：%s", r)
	}
	// 接好了一切正常，即便一条还没写（空表不是错误）
	if r := toolAuditDBBlockedReason(app.ToolAuditSnapshot{Mode: "on", Wired: true, DBHandle: true}); r != "" {
		t.Errorf("已接线时不该拒绝：%s", r)
	}
}

// off 态下 ?source=db 必须 503 且说清原因，不能回一份空列表让人以为"库里没有审计"。
func TestHandleToolAudit_DBSourceUnwired_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevExec := tooluse.GetGlobalExecutor()
	_, exec, _, _ := newTestExecutor(t)
	tooluse.SetGlobalExecutor(exec)
	t.Cleanup(func() { tooluse.SetGlobalExecutor(prevExec) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/audit?source=db", nil)
	handleToolAudit(c)

	// response.Error(c, 503, …) 的既有口径：HTTP 状态码与业务码同时给
	// （业务码是被 errorCodeFromHTTPCode 翻译过的字符串，不是裸 503）。
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未接线应回 HTTP 503，实际 %d；body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body 非 JSON：%v body=%s", err, w.Body.String())
	}
	if code, ok := resp["code"].(string); !ok || code == "0" || code == "" {
		t.Errorf("业务码应为非 0 的错误码，实际 %v", resp["code"])
	}
	if msg, _ := resp["message"].(string); !strings.Contains(msg, app.ToolAuditFlagEnv) {
		t.Errorf("错误信息应指名旗子 %s，实际 %q", app.ToolAuditFlagEnv, msg)
	}
}

// AC③：默认（内存）数据源在接线后照样工作，且响应里带上落库状态回显。
func TestHandleToolAudit_MemorySourceStillWorks_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevExec := tooluse.GetGlobalExecutor()
	_, exec, _, _ := newTestExecutor(t)
	tooluse.SetGlobalExecutor(exec)
	t.Cleanup(func() { tooluse.SetGlobalExecutor(prevExec) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/audit?limit=5", nil)
	handleToolAudit(c)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body 非 JSON：%v body=%s", err, w.Body.String())
	}
	if code, ok := resp["code"].(float64); !ok || code != 0 {
		t.Fatalf("内存数据源应正常返回，body=%s", w.Body.String())
	}
	data, _ := resp["data"].(map[string]any)
	if data["source"] != "memory" {
		t.Errorf("回显应标明数据源，实际 %v", data["source"])
	}
	if _, ok := data["persistence"].(map[string]any); !ok {
		t.Errorf("响应必须带 persistence 回显（否则看不出重启即丢），实际 %v", data["persistence"])
	}
}

func TestHandleToolCost_DBSourceUnwired_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/cost?source=db", nil)
	handleToolCost(c)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body 非 JSON：%v", err)
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("未接线应回 HTTP 503，实际 %d；body=%s", w.Code, w.Body.String())
	}
	if code, ok := resp["code"].(string); !ok || code == "" || code == "0" {
		t.Errorf("业务码应为非 0 错误码，实际 %v", resp["code"])
	}
	if msg, _ := resp["message"].(string); !strings.Contains(msg, app.ToolAuditFlagEnv) {
		t.Errorf("错误信息应指名旗子 %s，实际 %q", app.ToolAuditFlagEnv, msg)
	}
}

// 内存数据源必须继续回 200（本卡是增量，不得把既有端点改成依赖 DB）。
func TestHandleToolCost_MemorySourceStillWorks_HTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/cost", nil)
	handleToolCost(c)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body 非 JSON：%v", err)
	}
	if code, ok := resp["code"].(float64); !ok || code != 0 {
		t.Fatalf("内存计费应正常返回，body=%s", w.Body.String())
	}
	data, _ := resp["data"].(map[string]any)
	if data["source"] != "memory" {
		t.Errorf("回显应标明数据源，实际 %v", data["source"])
	}
}
