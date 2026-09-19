package tooluse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func makeFailingHandler(name string, callCount *int32, errMsg string) ToolHandler {
	return func(ctx context.Context, args map[string]any) (ToolResult, error) {
		atomic.AddInt32(callCount, 1)
		return ErrorResult(name, errors.New(errMsg)), errors.New(errMsg)
	}
}

func makeSuccessHandler(name string, callCount *int32) ToolHandler {
	return func(ctx context.Context, args map[string]any) (ToolResult, error) {
		atomic.AddInt32(callCount, 1)
		return SuccessResult(name, map[string]any{"ok": true}), nil
	}
}

func makeCtxWithToolNameAndTrace(name, traceID string) context.Context {
	ctx := context.Background()
	ctx = WithToolName(ctx, name)
	if traceID != "" {
		ctx = WithTraceID(ctx, traceID)
	}
	return ctx
}

// TestD9_1_CircuitBreaker_StateMachine 验证熔断器状态机：
//
//	CLOSED → 失败累计 ≥ threshold → OPEN → 冷却 → HALF_OPEN → 成功 → CLOSED
func TestD9_1_CircuitBreaker_StateMachine(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold:    3,
		BaseCooldown:        50 * time.Millisecond,
		HalfOpenMaxAttempts: 1,
	}
	registry := NewCircuitBreakerRegistry(cfg)
	toolName := "test.circuit"

	if registry.State(toolName) != CircuitClosed {
		t.Fatal("初始应为 CLOSED")
	}
	if !registry.Allow(toolName) {
		t.Fatal("CLOSED 应允许通过")
	}

	registry.RecordFailure(toolName)
	registry.RecordFailure(toolName)
	if registry.State(toolName) != CircuitClosed {
		t.Fatal("2 次失败未达阈值，应仍 CLOSED")
	}
	registry.RecordFailure(toolName)
	if registry.State(toolName) != CircuitOpen {
		t.Fatal("3 次失败达阈值，应 OPEN")
	}

	if registry.Allow(toolName) {
		t.Fatal("OPEN 应拒绝请求")
	}

	time.Sleep(60 * time.Millisecond)
	if !registry.Allow(toolName) {
		t.Fatal("冷却后应允许试探请求（HALF_OPEN）")
	}
	if registry.State(toolName) != CircuitHalfOpen {
		t.Fatal("冷却后应为 HALF_OPEN")
	}

	registry.RecordSuccess(toolName)
	if registry.State(toolName) != CircuitClosed {
		t.Fatal("HALF_OPEN 成功应回到 CLOSED")
	}
}

// TestD9_2_CircuitBreaker_HalfOpenFailure 验证 HALF_OPEN 失败回到 OPEN
func TestD9_2_CircuitBreaker_HalfOpenFailure(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold:    2,
		BaseCooldown:        20 * time.Millisecond,
		HalfOpenMaxAttempts: 1,
	}
	registry := NewCircuitBreakerRegistry(cfg)
	toolName := "test.halfopen_fail"

	registry.RecordFailure(toolName)
	registry.RecordFailure(toolName)
	if registry.State(toolName) != CircuitOpen {
		t.Fatal("2 次失败应 OPEN")
	}

	time.Sleep(30 * time.Millisecond)
	if !registry.Allow(toolName) {
		t.Fatal("HALF_OPEN 应允许试探请求")
	}

	registry.RecordFailure(toolName)
	if registry.State(toolName) != CircuitOpen {
		t.Fatal("HALF_OPEN 失败应回到 OPEN")
	}
}

// TestD9_3_CircuitBreaker_Decorator 验证熔断装饰器拦截
func TestD9_3_CircuitBreaker_Decorator(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold:    2,
		BaseCooldown:        5 * time.Minute,
		HalfOpenMaxAttempts: 1,
	}
	registry := NewCircuitBreakerRegistry(cfg)
	toolName := "test.dec_circuit"

	var calls int32
	failingHandler := makeFailingHandler(toolName, &calls, "downstream error")
	decorated := CircuitBreakerDecorator(registry)(failingHandler)

	ctx := makeCtxWithToolName(toolName)
	for i := 0; i < 2; i++ {
		_, err := decorated(ctx, nil)
		if err == nil {
			t.Fatalf("第 %d 次调用应返回错误", i+1)
		}
	}
	if registry.State(toolName) != CircuitOpen {
		t.Fatalf("2 次失败后应 OPEN，实际 %s", registry.State(toolName))
	}

	beforeCalls := atomic.LoadInt32(&calls)
	_, err := decorated(ctx, nil)
	if err == nil {
		t.Fatal("熔断开启后应返回错误")
	}
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("期望 ErrCircuitOpen 错误，实际 %v", err)
	}
	afterCalls := atomic.LoadInt32(&calls)
	if afterCalls != beforeCalls {
		t.Errorf("熔断后不应调用真实 handler，调用次数不应增加（before=%d after=%d）", beforeCalls, afterCalls)
	}
}

// TestD9_4_CircuitBreaker_ContextCanceledNotCounted 验证 context 取消不计入失败
func TestD9_4_CircuitBreaker_ContextCanceledNotCounted(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold:    2,
		BaseCooldown:        30 * time.Second,
		HalfOpenMaxAttempts: 1,
	}
	registry := NewCircuitBreakerRegistry(cfg)
	toolName := "test.cancel"

	cancelHandler := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		return ErrorResult(toolName, context.Canceled), context.Canceled
	}
	decorated := CircuitBreakerDecorator(registry)(cancelHandler)

	ctx := makeCtxWithToolName(toolName)
	for i := 0; i < 5; i++ {
		_, _ = decorated(ctx, nil)
	}
	if registry.State(toolName) != CircuitClosed {
		t.Fatal("context 取消不应触发熔断")
	}
}

// TestD10_1_DeadLetterQueue_PushAndGet 验证 Push + Get
func TestD10_1_DeadLetterQueue_PushAndGet(t *testing.T) {
	queue := NewDeadLetterQueue(100, time.Hour)
	args := map[string]any{"phone": "13800138000"}
	toolCtx := &ToolContext{AgentID: "agent-1", CustomerID: "cust-1"}

	id := queue.Push("test.tool", args, errors.New("mock error"), "trace-1", toolCtx, 2)
	if id == "" {
		t.Fatal("Push 应返回非空 ID")
	}

	entry, ok := queue.Get(id)
	if !ok {
		t.Fatal("Get 应找到死信条目")
	}
	if entry.ToolName != "test.tool" {
		t.Errorf("ToolName 期望 test.tool，实际 %s", entry.ToolName)
	}
	if entry.Error != "mock error" {
		t.Errorf("Error 期望 'mock error'，实际 %q", entry.Error)
	}
	if entry.TraceID != "trace-1" {
		t.Errorf("TraceID 期望 trace-1，实际 %s", entry.TraceID)
	}
	if entry.RetryCount != 2 {
		t.Errorf("RetryCount 期望 2，实际 %d", entry.RetryCount)
	}
	if entry.Status != DeadLetterPending {
		t.Errorf("Status 期望 pending，实际 %s", entry.Status)
	}
	if entry.ToolCtx == nil || entry.ToolCtx.AgentID != "agent-1" {
		t.Errorf("ToolCtx 应保留，实际 %+v", entry.ToolCtx)
	}
}

// TestD10_2_DeadLetterQueue_ListByTool 验证按工具名查询
func TestD10_2_DeadLetterQueue_ListByTool(t *testing.T) {
	queue := NewDeadLetterQueue(100, time.Hour)
	queue.Push("tool.a", nil, errors.New("err1"), "t1", nil, 0)
	queue.Push("tool.b", nil, errors.New("err2"), "t2", nil, 0)
	queue.Push("tool.a", nil, errors.New("err3"), "t3", nil, 0)

	listA := queue.ListByTool("tool.a")
	if len(listA) != 2 {
		t.Fatalf("tool.a 应有 2 条，实际 %d", len(listA))
	}
	listB := queue.ListByTool("tool.b")
	if len(listB) != 1 {
		t.Fatalf("tool.b 应有 1 条，实际 %d", len(listB))
	}
	if listB[0].Error != "err2" {
		t.Errorf("tool.b 错误信息错乱：%s", listB[0].Error)
	}
}

// TestD10_3_DeadLetterQueue_LRU_Eviction 验证容量上限淘汰
func TestD10_3_DeadLetterQueue_LRU_Eviction(t *testing.T) {
	queue := NewDeadLetterQueue(3, time.Hour)
	queue.Push("tool.1", nil, errors.New("e1"), "", nil, 0)
	queue.Push("tool.2", nil, errors.New("e2"), "", nil, 0)
	queue.Push("tool.3", nil, errors.New("e3"), "", nil, 0)
	queue.Push("tool.4", nil, errors.New("e4"), "", nil, 0)

	all := queue.ListAll()
	if len(all) != 3 {
		t.Fatalf("容量 3 应保留 3 条，实际 %d", len(all))
	}
	for _, e := range all {
		if e.ToolName == "tool.1" {
			t.Errorf("tool.1 应被淘汰，但仍存在")
		}
	}
}

// TestD10_4_DeadLetterQueue_UpdateStatus 验证状态更新
func TestD10_4_DeadLetterQueue_UpdateStatus(t *testing.T) {
	queue := NewDeadLetterQueue(100, time.Hour)
	id := queue.Push("test.tool", nil, errors.New("err"), "", nil, 0)

	if err := queue.UpdateStatus(id, DeadLetterReplaying); err != nil {
		t.Fatalf("UpdateStatus 失败：%v", err)
	}
	entry, _ := queue.Get(id)
	if entry.Status != DeadLetterReplaying {
		t.Errorf("Status 期望 replaying，实际 %s", entry.Status)
	}

	if err := queue.UpdateStatus("nonexistent", DeadLetterReplayed); err == nil {
		t.Fatal("不存在的 ID 应报错")
	}
}

// TestD10_5_DeadLetterQueue_Cleanup 验证过期清理
func TestD10_5_DeadLetterQueue_Cleanup(t *testing.T) {
	queue := NewDeadLetterQueue(100, 10*time.Millisecond)
	queue.Push("test.tool", nil, errors.New("err"), "", nil, 0)

	time.Sleep(20 * time.Millisecond)
	removed := queue.Cleanup()
	if removed != 1 {
		t.Fatalf("应清理 1 条，实际 %d", removed)
	}
	if len(queue.ListAll()) != 0 {
		t.Fatal("清理后应无条目")
	}
}

// TestD10_6_DeadLetterQueue_Decorator 验证死信装饰器在最终失败时入队
func TestD10_6_DeadLetterQueue_Decorator(t *testing.T) {
	queue := NewDeadLetterQueue(100, time.Hour)
	var calls int32
	failingHandler := makeFailingHandler("test.dlq_dec", &calls, "downstream failure")
	decorated := DeadLetterQueueDecorator(queue)(failingHandler)

	ctx := makeCtxWithToolNameAndTrace("test.dlq_dec", "trace-d10-6")

	_, err := decorated(ctx, map[string]any{"k": "v"})
	if err == nil {
		t.Fatal("应返回失败")
	}

	pending := queue.ListPending()
	if len(pending) != 1 {
		t.Fatalf("期望 1 条待处理死信，实际 %d", len(pending))
	}
	entry := pending[0]
	if entry.ToolName != "test.dlq_dec" {
		t.Errorf("ToolName 期望 test.dlq_dec，实际 %s", entry.ToolName)
	}
	if entry.TraceID != "trace-d10-6" {
		t.Errorf("TraceID 期望 trace-d10-6，实际 %s", entry.TraceID)
	}
}

// TestD10_7_DeadLetterQueue_Decorator_SkipsNonRetryable 验证不可重试错误不入队
func TestD10_7_DeadLetterQueue_Decorator_SkipsNonRetryable(t *testing.T) {
	queue := NewDeadLetterQueue(100, time.Hour)
	bizErrHandler := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		return ToolResult{
			Success:  false,
			Error:    "customer already_exists",
			ToolName: "test.biz_err",
		}, nil
	}
	decorated := DeadLetterQueueDecorator(queue)(bizErrHandler)
	ctx := makeCtxWithToolName("test.biz_err")

	_, _ = decorated(ctx, nil)
	if len(queue.ListAll()) != 0 {
		t.Fatal("业务错误（不可重试）不应入队")
	}
}

// TestD10_8_DeadLetterQueue_Decorator_SuccessNotEnqueued 验证成功调用不入队
func TestD10_8_DeadLetterQueue_Decorator_SuccessNotEnqueued(t *testing.T) {
	queue := NewDeadLetterQueue(100, time.Hour)
	var calls int32
	successHandler := makeSuccessHandler("test.success", &calls)
	decorated := DeadLetterQueueDecorator(queue)(successHandler)

	ctx := makeCtxWithToolName("test.success")
	r, err := decorated(ctx, nil)
	if err != nil || !r.Success {
		t.Fatal("成功 handler 应返回成功")
	}
	if len(queue.ListAll()) != 0 {
		t.Fatal("成功调用不应入死信队列")
	}
}

// TestD10_9_DeadLetterReplayer 验证死信重放
func TestD10_9_DeadLetterReplayer(t *testing.T) {
	registry := NewToolRegistry()
	toolName := "test.replay_tool"
	var calls int32
	mockTool := newMockTool(toolName, CategoryCustomer, func(ctx context.Context, args map[string]any) (ToolResult, error) {
		atomic.AddInt32(&calls, 1)
		return SuccessResult(toolName, "ok"), nil
	})
	_ = registry.Register(mockTool)

	executor := NewToolExecutor(registry, ToolExecutorConfig{
		DefaultTimeout: 5 * time.Second,
	})

	queue := NewDeadLetterQueue(100, time.Hour)
	args := map[string]any{"phone": "13900001234"}
	id := queue.Push(toolName, args, errors.New("original failure"), "trace-replay", nil, 1)

	replayer := NewDeadLetterReplayer(queue, executor)
	if err := replayer.Replay(context.Background(), id); err != nil {
		t.Fatalf("Replay 失败：%v", err)
	}

	entry, _ := queue.Get(id)
	if entry.Status != DeadLetterReplayed {
		t.Errorf("Status 期望 replayed，实际 %s", entry.Status)
	}

	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("Replay 应调用真实 handler 1 次，实际 %d", atomic.LoadInt32(&calls))
	}
}

// TestD11_1_ResultCache_SetGet 验证基础 Set/Get
func TestD11_1_ResultCache_SetGet(t *testing.T) {
	cache := NewResultCache(100, time.Minute)
	toolName := "customer.search"
	args := map[string]any{"phone": "13800138000"}
	result := SuccessResult(toolName, map[string]any{"id": "cust-001"})

	cache.Set(toolName, args, result, 0)
	got, ok := cache.Get(toolName, args)
	if !ok {
		t.Fatal("缓存应命中")
	}
	if !got.Success {
		t.Error("缓存结果应 Success=true")
	}
	if got.AuditTrace != "cache_hit" {
		t.Errorf("AuditTrace 应标记 cache_hit，实际 %q", got.AuditTrace)
	}
}

// TestD11_2_ResultCache_TTLExpiry 验证 TTL 过期
func TestD11_2_ResultCache_TTLExpiry(t *testing.T) {
	cache := NewResultCache(100, time.Minute)
	toolName := "test.ttl"
	args := map[string]any{"k": "v"}
	result := SuccessResult(toolName, "data")

	cache.Set(toolName, args, result, 10*time.Millisecond)
	if _, ok := cache.Get(toolName, args); !ok {
		t.Fatal("TTL 未过期前应命中")
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok := cache.Get(toolName, args); ok {
		t.Fatal("TTL 过期后应未命中")
	}
}

// TestD11_3_ResultCache_LRU_Eviction 验证 LRU 淘汰
func TestD11_3_ResultCache_LRU_Eviction(t *testing.T) {
	cache := NewResultCache(2, time.Minute)

	cache.Set("t.a", map[string]any{"i": 1}, SuccessResult("t.a", "1"), 0)
	cache.Set("t.b", map[string]any{"i": 2}, SuccessResult("t.b", "2"), 0)
	cache.Set("t.c", map[string]any{"i": 3}, SuccessResult("t.c", "3"), 0)

	if _, ok := cache.Get("t.a", map[string]any{"i": 1}); ok {
		t.Error("t.a 应被 LRU 淘汰")
	}
	if _, ok := cache.Get("t.b", map[string]any{"i": 2}); !ok {
		t.Error("t.b 应仍在缓存")
	}
	if _, ok := cache.Get("t.c", map[string]any{"i": 3}); !ok {
		t.Error("t.c 应仍在缓存")
	}
}

// TestD11_4_ResultCache_DisabledTools 验证默认禁用工具不缓存
func TestD11_4_ResultCache_DisabledTools(t *testing.T) {
	cache := NewResultCache(100, time.Minute)
	args := map[string]any{"order_id": "ord-1"}
	result := SuccessResult("order.create", "created")

	cache.Set("order.create", args, result, 0)
	if _, ok := cache.Get("order.create", args); ok {
		t.Error("order.create 默认应禁用缓存")
	}
}

// TestD11_5_ResultCache_FailedResultNotCached 验证失败结果不缓存
func TestD11_5_ResultCache_FailedResultNotCached(t *testing.T) {
	cache := NewResultCache(100, time.Minute)
	args := map[string]any{"q": "test"}
	failedResult := ErrorResult("test.search", errors.New("db error"))

	cache.Set("test.search", args, failedResult, 0)
	if _, ok := cache.Get("test.search", args); ok {
		t.Error("失败结果不应缓存")
	}
}

// TestD11_6_ResultCache_Decorator 验证缓存装饰器
func TestD11_6_ResultCache_Decorator(t *testing.T) {
	cache := NewResultCache(100, time.Minute)
	var calls int32
	handler := makeSuccessHandler("test.cache_dec", &calls)
	decorated := ResultCacheDecorator(cache)(handler)

	ctx := makeCtxWithToolName("test.cache_dec")
	args := map[string]any{"q": "v1"}

	r1, err := decorated(ctx, args)
	if err != nil || !r1.Success {
		t.Fatal("首次调用应成功")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("首次应调用真实 handler，实际 %d", atomic.LoadInt32(&calls))
	}

	r2, err := decorated(ctx, args)
	if err != nil || !r2.Success {
		t.Fatal("缓存命中应成功")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("缓存命中后不应调用真实 handler，实际 %d", atomic.LoadInt32(&calls))
	}
	if r2.AuditTrace != "cache_hit" {
		t.Errorf("缓存命中应标记 cache_hit，实际 %q", r2.AuditTrace)
	}

	_, _ = decorated(ctx, map[string]any{"q": "v2"})
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("不同参数应未命中，调用真实 handler，实际 %d", atomic.LoadInt32(&calls))
	}
}

// TestD11_7_ResultCache_Invalidate 验证按工具失效
func TestD11_7_ResultCache_Invalidate(t *testing.T) {
	cache := NewResultCache(100, time.Minute)
	args1 := map[string]any{"q": "a"}
	args2 := map[string]any{"q": "b"}

	cache.Set("test.tool", args1, SuccessResult("test.tool", "1"), 0)
	cache.Set("test.tool", args2, SuccessResult("test.tool", "2"), 0)
	cache.Set("other.tool", args1, SuccessResult("other.tool", "3"), 0)

	cache.Invalidate("test.tool")
	if _, ok := cache.Get("test.tool", args1); ok {
		t.Error("Invalidate 后 test.tool args1 应失效")
	}
	if _, ok := cache.Get("test.tool", args2); ok {
		t.Error("Invalidate 后 test.tool args2 应失效")
	}
	if _, ok := cache.Get("other.tool", args1); !ok {
		t.Error("Invalidate test.tool 不应影响 other.tool")
	}
}

type mockToolForValidator struct {
	name        string
	params      ToolParameters
	executeFunc func(ctx context.Context, args map[string]any) (ToolResult, error)
}

func (m *mockToolForValidator) Name() string               { return m.name }
func (m *mockToolForValidator) Category() ToolCategory     { return CategoryCustomer }
func (m *mockToolForValidator) Description() string        { return "mock tool" }
func (m *mockToolForValidator) Parameters() ToolParameters { return m.params }
func (m *mockToolForValidator) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	return m.executeFunc(ctx, args)
}

// TestD12_1_ParamValidator_RequiredMissing 验证必填字段缺失
func TestD12_1_ParamValidator_RequiredMissing(t *testing.T) {
	registry := NewToolRegistry()
	tool := &mockToolForValidator{
		name: "test.required",
		params: ToolParameters{
			Type: "object",
			Properties: map[string]ToolParam{
				"phone": {Type: "string", Description: "手机号"},
			},
			Required: []string{"phone"},
		},
		executeFunc: func(ctx context.Context, args map[string]any) (ToolResult, error) {
			return SuccessResult("test.required", "ok"), nil
		},
	}
	_ = registry.Register(tool)

	var calls int32
	handler := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		atomic.AddInt32(&calls, 1)
		return SuccessResult("test.required", "ok"), nil
	}
	decorated := ParamValidatorDecorator(registry)(handler)

	ctx := makeCtxWithToolName("test.required")
	r, err := decorated(ctx, map[string]any{})
	if err == nil {
		t.Fatal("缺少必填字段应返回错误")
	}
	if !errors.Is(err, ErrParamValidationFailed) {
		t.Errorf("期望 ErrParamValidationFailed，实际 %v", err)
	}
	if r.Success {
		t.Error("应返回失败结果")
	}
	if !strings.Contains(r.Error, "missing required param: phone") {
		t.Errorf("错误信息应含 missing required param，实际 %q", r.Error)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Error("校验失败不应调用真实 handler")
	}
}

// TestD12_2_ParamValidator_TypeMismatch 验证类型不匹配
func TestD12_2_ParamValidator_TypeMismatch(t *testing.T) {
	registry := NewToolRegistry()
	tool := &mockToolForValidator{
		name: "test.type",
		params: ToolParameters{
			Type: "object",
			Properties: map[string]ToolParam{
				"phone":  {Type: "string"},
				"age":    {Type: "integer"},
				"active": {Type: "boolean"},
			},
		},
	}
	_ = registry.Register(tool)

	decorated := ParamValidatorDecorator(registry)(func(ctx context.Context, args map[string]any) (ToolResult, error) {
		return SuccessResult("test.type", "ok"), nil
	})
	ctx := makeCtxWithToolName("test.type")

	_, err := decorated(ctx, map[string]any{"phone": 123})
	if err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Errorf("phone 类型错误应报错，实际 %v", err)
	}

	_, err = decorated(ctx, map[string]any{"age": "abc"})
	if err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Errorf("age 类型错误应报错，实际 %v", err)
	}

	_, err = decorated(ctx, map[string]any{"active": "yes"})
	if err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Errorf("active 类型错误应报错，实际 %v", err)
	}

	_, err = decorated(ctx, map[string]any{"phone": "138", "age": 18, "active": true})
	if err != nil {
		t.Errorf("正确类型应通过校验，实际 %v", err)
	}
}

// TestD12_3_ParamValidator_Enum 验证枚举值
func TestD12_3_ParamValidator_Enum(t *testing.T) {
	registry := NewToolRegistry()
	tool := &mockToolForValidator{
		name: "test.enum",
		params: ToolParameters{
			Type: "object",
			Properties: map[string]ToolParam{
				"status": {
					Type: "string",
					Enum: []string{"active", "inactive", "pending"},
				},
			},
		},
	}
	_ = registry.Register(tool)

	decorated := ParamValidatorDecorator(registry)(func(ctx context.Context, args map[string]any) (ToolResult, error) {
		return SuccessResult("test.enum", "ok"), nil
	})
	ctx := makeCtxWithToolName("test.enum")

	_, err := decorated(ctx, map[string]any{"status": "invalid_status"})
	if err == nil {
		t.Fatal("非枚举值应报错")
	}
	if !strings.Contains(err.Error(), "not in enum") {
		t.Errorf("错误信息应含 not in enum，实际 %v", err)
	}

	_, err = decorated(ctx, map[string]any{"status": "active"})
	if err != nil {
		t.Errorf("枚举内值应通过，实际 %v", err)
	}
}

// TestD12_4_ParamValidator_NilRegistry 验证 nil registry 放行
func TestD12_4_ParamValidator_NilRegistry(t *testing.T) {
	decorated := ParamValidatorDecorator(nil)(func(ctx context.Context, args map[string]any) (ToolResult, error) {
		return SuccessResult("test.nil", "ok"), nil
	})
	ctx := makeCtxWithToolName("test.nil")
	_, err := decorated(ctx, map[string]any{})
	if err != nil {
		t.Errorf("nil registry 应放行，实际 %v", err)
	}
}

// TestD13_1_RetryDecorator_NonRetryableErrorSkips 验证不可重试错误立即返回
func TestD13_1_RetryDecorator_NonRetryableErrorSkips(t *testing.T) {
	policy := NewExponentialBackoffPolicy(5, 10*time.Millisecond, 100*time.Millisecond)
	var calls int32

	permHandler := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		atomic.AddInt32(&calls, 1)
		return ErrorResult("test.perm", ErrPermissionDenied), ErrPermissionDenied
	}
	decorated := RetryDecorator(policy)(permHandler)
	ctx := makeCtxWithToolName("test.perm")
	_, _ = decorated(ctx, nil)
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("ErrPermissionDenied 不应重试，调用次数应=1，实际 %d", atomic.LoadInt32(&calls))
	}

	atomic.StoreInt32(&calls, 0)
	rateHandler := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		atomic.AddInt32(&calls, 1)
		return ErrorResult("test.rate", ErrRateLimited), ErrRateLimited
	}
	decorated = RetryDecorator(policy)(rateHandler)
	ctx = makeCtxWithToolName("test.rate")
	_, _ = decorated(ctx, nil)
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("ErrRateLimited 不应重试，调用次数应=1，实际 %d", atomic.LoadInt32(&calls))
	}
}

// TestD13_2_RetryDecorator_NonRetryableResultPattern 验证业务错误（不可重试 result）立即返回
func TestD13_2_RetryDecorator_NonRetryableResultPattern(t *testing.T) {
	policy := NewExponentialBackoffPolicy(5, 1*time.Millisecond, 10*time.Millisecond)
	var calls int32

	bizHandler := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		atomic.AddInt32(&calls, 1)
		return ToolResult{
			Success:  false,
			Error:    "customer not_found",
			ToolName: "test.biz",
		}, nil
	}
	decorated := RetryDecorator(policy)(bizHandler)
	ctx := makeCtxWithToolName("test.biz")
	r, _ := decorated(ctx, nil)
	if r.Success {
		t.Error("应返回失败")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("not_found 业务错误不应重试，调用次数应=1，实际 %d", atomic.LoadInt32(&calls))
	}
}

// TestD13_3_RetryDecorator_RetryableErrorRetries 验证可重试错误触发重试
func TestD13_3_RetryDecorator_RetryableErrorRetries(t *testing.T) {
	policy := NewExponentialBackoffPolicy(3, 1*time.Millisecond, 10*time.Millisecond)
	var calls int32

	retryableHandler := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		atomic.AddInt32(&calls, 1)
		return ErrorResult("test.retry", errors.New("connection refused")), errors.New("connection refused")
	}
	decorated := RetryDecorator(policy)(retryableHandler)
	ctx := makeCtxWithToolName("test.retry")
	_, _ = decorated(ctx, nil)
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("可重试错误应重试 3 次，实际 %d", atomic.LoadInt32(&calls))
	}
}

// TestD13_4_RetryDecorator_RetryThenSucceed 验证重试后成功
func TestD13_4_RetryDecorator_RetryThenSucceed(t *testing.T) {
	policy := NewExponentialBackoffPolicy(5, 1*time.Millisecond, 10*time.Millisecond)
	var calls int32

	handler := func(ctx context.Context, args map[string]any) (ToolResult, error) {
		c := atomic.AddInt32(&calls, 1)
		if c < 2 {
			return ErrorResult("test.rs", errors.New("network error")), errors.New("network error")
		}
		return SuccessResult("test.rs", "ok"), nil
	}
	decorated := RetryDecorator(policy)(handler)
	ctx := makeCtxWithToolName("test.rs")
	r, err := decorated(ctx, nil)
	if err != nil {
		t.Errorf("重试后成功应返回 nil err，实际 %v", err)
	}
	if !r.Success {
		t.Error("应返回成功")
	}
	if r.Timing.RetryCount != 1 {
		t.Errorf("RetryCount 应=1（重试 1 次后成功），实际 %d", r.Timing.RetryCount)
	}
}

// TestD13_5_isNonRetryableError_Unit 验证错误分类函数
func TestD13_5_isNonRetryableError_Unit(t *testing.T) {
	if !isNonRetryableError(ErrPermissionDenied) {
		t.Error("ErrPermissionDenied 应不可重试")
	}
	if !isNonRetryableError(ErrRateLimited) {
		t.Error("ErrRateLimited 应不可重试")
	}
	if !isNonRetryableError(ErrCircuitOpen) {
		t.Error("ErrCircuitOpen 应不可重试")
	}
	if !isNonRetryableError(context.Canceled) {
		t.Error("context.Canceled 应不可重试")
	}
	if !isNonRetryableError(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded 应不可重试")
	}
	if !isNonRetryableError(errors.New("invalid argument: phone")) {
		t.Error("'invalid argument' 应不可重试")
	}
	if !isNonRetryableError(errors.New("validation failed: missing required")) {
		t.Error("'validation failed' 应不可重试")
	}

	if isNonRetryableError(errors.New("connection refused")) {
		t.Error("'connection refused' 应可重试")
	}
	if isNonRetryableError(errors.New("timeout: dial tcp")) {
		t.Error("'timeout: dial tcp' 应可重试")
	}
	if isNonRetryableError(nil) {
		t.Error("nil 应不可重试（短路）")
	}
}

// TestD13_6_isNonRetryableResult_Unit 验证结果分类函数
func TestD13_6_isNonRetryableResult_Unit(t *testing.T) {
	cases := []struct {
		name   string
		result ToolResult
		want   bool
	}{
		{"success", ToolResult{Success: true}, false},
		{"not_found", ToolResult{Success: false, Error: "customer not_found"}, true},
		{"already_exists", ToolResult{Success: false, Error: "order already_exists"}, true},
		{"already_used", ToolResult{Success: false, Error: "coupon already_used"}, true},
		{"expired", ToolResult{Success: false, Error: "coupon expired"}, true},
		{"insufficient", ToolResult{Success: false, Error: "insufficient_balance"}, true},
		{"permission_denied", ToolResult{Success: false, Error: "permission_denied"}, true},
		{"invalid_argument", ToolResult{Success: false, Error: "invalid_argument: phone"}, true},
		{"validation_failed", ToolResult{Success: false, Error: "validation_failed: phone"}, true},
		{"chinese_not_found", ToolResult{Success: false, Error: "客户不存在"}, true},
		{"chinese_already_exists", ToolResult{Success: false, Error: "订单已存在"}, true},
		{"chinese_already_used", ToolResult{Success: false, Error: "优惠券已使用"}, true},
		{"chinese_expired", ToolResult{Success: false, Error: "活动已过期"}, true},
		{"chinese_invalid", ToolResult{Success: false, Error: "参数无效"}, true},
		{"chinese_validation_failed", ToolResult{Success: false, Error: "参数校验失败"}, true},
		{"retryable_network", ToolResult{Success: false, Error: "connection refused"}, false},
		{"retryable_timeout", ToolResult{Success: false, Error: "i/o timeout"}, false},
		{"retryable_500", ToolResult{Success: false, Error: "HTTP 500 internal"}, false},
	}
	for _, tc := range cases {
		got := isNonRetryableResult(tc.result)
		if got != tc.want {
			t.Errorf("case %q: 期望 %v，实际 %v", tc.name, tc.want, got)
		}
	}
}

// TestD14_1_AuditEntryToRecord 验证 AuditEntry → DB 模型转换
func TestD14_1_AuditEntryToRecord(t *testing.T) {
	entry := AuditEntry{
		TraceID:       "trace-d14-1",
		ToolName:      "test.db",
		CallerID:      "caller-1",
		AgentID:       "agent-1",
		CustomerID:    "cust-1",
		SessionID:     "sess-1",
		Success:       true,
		Error:         "",
		Duration:      1500 * time.Millisecond,
		RetryCount:    2,
		AuditTrace:    "audit-trace-1",
		ArgsSummary:   `{"phone":"138****0000"}`,
		ResultSummary: `{"id":"cust-1"}`,
		ExecutedAt:    time.Now(),
	}

	record := auditEntryToRecord(entry)
	if record.TraceID != entry.TraceID {
		t.Errorf("TraceID 不匹配")
	}
	if record.ToolName != entry.ToolName {
		t.Errorf("ToolName 不匹配")
	}
	if record.Success != entry.Success {
		t.Errorf("Success 不匹配")
	}
	if record.DurationMs != 1500 {
		t.Errorf("DurationMs 期望 1500，实际 %d", record.DurationMs)
	}
	if record.RetryCount != 2 {
		t.Errorf("RetryCount 期望 2，实际 %d", record.RetryCount)
	}
}

// TestD14_2_DBAuditLogger_NilDBNoOp 验证 nil DB 不崩溃
func TestD14_2_DBAuditLogger_NilDBNoOp(t *testing.T) {
	logger := NewDBAuditLogger(nil, 10, nil)
	logger.Log(context.Background(), AuditEntry{
		ToolName: "test.nodb",
	})
	logger.Close()
}

// TestD14_3_DBAuditLogger_FallbackOnNilDB 验证 nil DB 时降级到 fallback
func TestD14_3_DBAuditLogger_FallbackOnNilDB(t *testing.T) {
	memoryLogger := NewMemoryAuditLogger(100)
	fallback := NewCompositeAuditLogger(memoryLogger, nil, nil)
	fallback.Log(context.Background(), AuditEntry{
		ToolName: "test.fallback",
	})
	entries := memoryLogger.Entries()
	if len(entries) != 1 {
		t.Fatalf("fallback 应记录 1 条，实际 %d", len(entries))
	}
	if entries[0].ToolName != "test.fallback" {
		t.Errorf("ToolName 期望 test.fallback，实际 %s", entries[0].ToolName)
	}
}

// TestD14_4_DBAuditLogger_CloseFlushes 验证 Close 时 flush 剩余 batch
func TestD14_4_DBAuditLogger_CloseFlushes(t *testing.T) {
	memoryLogger := &captureLogger{}
	logger := NewDBAuditLogger(nil, 100, memoryLogger)

	for i := 0; i < 5; i++ {
		logger.Log(context.Background(), AuditEntry{
			ToolName: fmt.Sprintf("test.flush.%d", i),
		})
	}
	logger.Close()

	if len(memoryLogger.entries) != 5 {
		t.Errorf("Close 后应 flush 5 条到 fallback，实际 %d", len(memoryLogger.entries))
	}
}

type captureLogger struct {
	entries []AuditEntry
	mu      sync.Mutex
}

func (c *captureLogger) Log(ctx context.Context, entry AuditEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, entry)
}

// TestD14_5_DBAuditLogger_PersistsRows 验证审计真的能落库（T-P1-08 AC①）。
//
// 这里刻意覆盖两个"接了但没落"的真实坑：
//  1. 中文摘要按字节硬砍会产出非法 UTF-8，PG 直接拒绝整批（实测 error 22032）；
//  2. varchar(64) 列被超长 trace_id 撑爆，CreateInBatches 整批回滚。
//
// 原 TestD14_5_AutoMigrateAuditTable_NilDB 随 AutoMigrateAuditTable 一起删除：
// 那个函数就是"看着能建表、其实没人调"的元凶，建表职责已交还 internal/pkg/db 清单。
func TestD14_5_DBAuditLogger_PersistsRows(t *testing.T) {
	testDB := testutil.NewTestDB(t, &model.ToolCallAudit{})

	// 中文为主、远超 200 字节的摘要：旧实现会在多字节字符中间切断。
	chineseArgs := strings.Repeat("客户张三的订单备注信息与地址", 40)
	oversizedTrace := strings.Repeat("t", 200)

	logger := NewDBAuditLogger(testDB, 100, nil)
	ctx := context.Background()
	const total = 7
	for i := 0; i < total; i++ {
		logger.Log(ctx, AuditEntry{
			TraceID:     oversizedTrace,
			ToolName:    fmt.Sprintf("test.persist.%d", i),
			Success:     i%2 == 0,
			Error:       "boom",
			Duration:    time.Duration(i+1) * 150 * time.Millisecond,
			ArgsSummary: chineseArgs,
			ExecutedAt:  time.Now().Add(-time.Duration(i) * time.Second),
		})
	}
	logger.Close() // Close 会排空队列，因此返回后所有行必须已在库里

	stats := logger.Stats()
	if stats.DBRows != total {
		t.Fatalf("期望落库 %d 行，实际 %d（fell_back=%d fail_batches=%d）", total, stats.DBRows, stats.FellBack, stats.FailBatches)
	}

	var rows []model.ToolCallAudit
	if err := testDB.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读回审计行失败：%v", err)
	}
	if len(rows) != total {
		t.Fatalf("表内期望 %d 行，实际 %d", total, len(rows))
	}
	for _, row := range rows {
		if !utf8.ValidString(row.ArgsSummary) {
			t.Errorf("ArgsSummary 含非法 UTF8 序列（截断切进多字节字符里了）：%q", row.ArgsSummary)
		}
		if len(row.TraceID) > auditColTraceID {
			t.Errorf("trace_id 未裁到列宽内：%d 字节 > %d", len(row.TraceID), auditColTraceID)
		}
	}
	if rows[0].DurationMs != 150 {
		t.Errorf("耗时应落库（CS-58）：期望 150ms，实际 %d", rows[0].DurationMs)
	}
	if rows[1].Success {
		t.Error("Success=false 的条目不应记成成功")
	}
}

// TestD14_5a_cutBytes_RuneBoundary 验证按字节裁剪不会切进多字节字符。
func TestD14_5a_cutBytes_RuneBoundary(t *testing.T) {
	s := "中文abc" // 中/文 各 3 字节
	cases := []struct {
		max  int
		want string
	}{
		{max: 7, want: "中文a"}, // 7 会切在"文"中间 → 回退到 6 字节
		{max: 3, want: "中"},   // 3 正好
		{max: 2, want: ""},    // 2 落在首字符内部 → 回退到 0
		{max: 100, want: s},   // 不截
		{max: 0, want: s},     // 非正上限 = 不裁剪
	}
	for _, c := range cases {
		got := cutBytes(s, c.max)
		if got != c.want {
			t.Errorf("cutBytes(%q, %d) = %q，期望 %q", s, c.max, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("cutBytes(%q, %d) 产出非法 UTF8：%q", s, c.max, got)
		}
	}
}

// TestD14_5b_SummarizeArgs_ChineseSafe 是缺陷②的回归测试：
// 摘要入口直接产出可安全落库的字符串（旧实现按字节硬砍会切进中文里）。
func TestD14_5b_SummarizeArgs_ChineseSafe(t *testing.T) {
	args := map[string]any{
		"content":   strings.Repeat("您好，您的订单已经发货请留意物流信息", 30),
		"customer":  strings.Repeat("周小明", 40),
		"phone":     "13800001111",
		"empty_val": strings.Repeat("≤", 200),
	}
	got := summarizeArgs(args)
	if !utf8.ValidString(got) {
		t.Fatalf("summarizeArgs 产出非法 UTF8（该字符串一旦落库会整批被 PG 拒收）：%q", got)
	}
	if len(got) > 203 { // 200 字节上限 + "..." 后缀
		t.Errorf("摘要长度失控：%d 字节", len(got))
	}
	if strings.Contains(got, "13800001111") {
		t.Error("phone 应被脱敏")
	}
	for _, r := range got {
		if r == utf8.RuneError {
			t.Fatalf("摘要含 U+FFFD 替换符，说明截断切坏了字符")
		}
	}
	if out := summarizeResult(strings.Repeat("订单已取消，退款将于三个工作日内到账", 30)); !utf8.ValidString(out) {
		t.Errorf("summarizeResult 产出非法 UTF8：%q", out)
	}
}

// TestD14_6_ToolCallAuditRecord_TableName 验证表名
func TestD14_6_ToolCallAuditRecord_TableName(t *testing.T) {
	var record ToolCallAuditRecord
	if record.TableName() != "tool_call_audits" {
		t.Errorf("TableName 期望 tool_call_audits，实际 %s", record.TableName())
	}
}

// TestD15_1_ToolAlertManager_FailureRate 验证失败率告警
func TestD15_1_ToolAlertManager_FailureRate(t *testing.T) {
	manager := NewToolAlertManager()
	var alerts []AlertEvent
	var mu sync.Mutex
	manager.AddHandler(AlertHandlerFunc(func(event AlertEvent) {
		mu.Lock()
		defer mu.Unlock()
		alerts = append(alerts, event)
	}))

	for i := 0; i < 10; i++ {
		entry := AuditEntry{
			ToolName: "test.alert_rate",
			Success:  i >= 6,
			Duration: 100 * time.Millisecond,
		}
		manager.OnToolCall(entry)
	}

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()

	rateAlerts := 0
	for _, a := range alerts {
		if strings.Contains(a.Title, "失败率过高") {
			rateAlerts++
		}
	}
	if rateAlerts == 0 {
		t.Errorf("应触发失败率告警，实际 alerts=%d", rateAlerts)
	}
}

// TestD15_2_ToolAlertManager_LatencyAlert 验证耗时告警
func TestD15_2_ToolAlertManager_LatencyAlert(t *testing.T) {
	manager := NewToolAlertManager()
	var alert AlertEvent
	var got sync.WaitGroup
	got.Add(1)
	manager.AddHandler(AlertHandlerFunc(func(event AlertEvent) {
		alert = event
		got.Done()
	}))

	manager.OnToolCall(AuditEntry{
		ToolName: "test.latency",
		Success:  true,
		Duration: 6 * time.Second,
	})
	got.Wait()

	if alert.Level != AlertWarning {
		t.Errorf("Level 期望 warning，实际 %s", alert.Level)
	}
	if !strings.Contains(alert.Title, "耗时过长") {
		t.Errorf("Title 应含'耗时过长'，实际 %q", alert.Title)
	}
	if alert.Extra["duration_ms"] != int64(6000) {
		t.Errorf("duration_ms 期望 6000，实际 %v", alert.Extra["duration_ms"])
	}
}

// TestD15_3_ToolAlertManager_CircuitOpen 验证熔断告警
func TestD15_3_ToolAlertManager_CircuitOpen(t *testing.T) {
	manager := NewToolAlertManager()
	var alert AlertEvent
	var got sync.WaitGroup
	got.Add(1)
	manager.AddHandler(AlertHandlerFunc(func(event AlertEvent) {
		alert = event
		got.Done()
	}))

	manager.AlertCircuitOpen("test.circuit_alert", CircuitOpen)
	got.Wait()

	if alert.Level != AlertCritical {
		t.Errorf("熔断告警 Level 期望 critical，实际 %s", alert.Level)
	}
	if !strings.Contains(alert.Title, "熔断器开启") {
		t.Errorf("Title 应含'熔断器开启'，实际 %q", alert.Title)
	}
	if alert.Extra["circuit_state"] != "open" {
		t.Errorf("circuit_state 期望 open，实际 %v", alert.Extra["circuit_state"])
	}
}

// TestD15_4_ToolAlertManager_DeadLetterBacklog 验证死信堆积告警
func TestD15_4_ToolAlertManager_DeadLetterBacklog(t *testing.T) {
	manager := NewToolAlertManager()
	var alert AlertEvent
	var got sync.WaitGroup
	got.Add(1)
	manager.AddHandler(AlertHandlerFunc(func(event AlertEvent) {
		alert = event
		got.Done()
	}))

	manager.AlertDeadLetterBacklog("test.dlq_alert", 150)
	got.Wait()

	if alert.Level != AlertCritical {
		t.Errorf(">100 条堆积应为 critical，实际 %s", alert.Level)
	}
	if !strings.Contains(alert.Title, "死信队列堆积") {
		t.Errorf("Title 应含'死信队列堆积'，实际 %q", alert.Title)
	}
	if alert.Extra["backlog_count"] != 150 {
		t.Errorf("backlog_count 期望 150，实际 %v", alert.Extra["backlog_count"])
	}
}

// TestD15_5_ToolAlertManager_DeadLetterBacklog_Warning 验证 <100 死信为 warning
func TestD15_5_ToolAlertManager_DeadLetterBacklog_Warning(t *testing.T) {
	manager := NewToolAlertManager()
	var alert AlertEvent
	var got sync.WaitGroup
	got.Add(1)
	manager.AddHandler(AlertHandlerFunc(func(event AlertEvent) {
		alert = event
		got.Done()
	}))

	manager.AlertDeadLetterBacklog("test.dlq_warn", 50)
	got.Wait()

	if alert.Level != AlertWarning {
		t.Errorf("50 条堆积应为 warning，实际 %s", alert.Level)
	}
}

// TestD15_6_ToolAlertManager_WindowReset 验证 1 分钟窗口重置
func TestD15_6_ToolAlertManager_WindowReset(t *testing.T) {
	manager := NewToolAlertManager()

	for i := 0; i < 10; i++ {
		manager.OnToolCall(AuditEntry{
			ToolName: "test.window",
			Success:  false,
			Duration: 100 * time.Millisecond,
		})
	}
	stats := manager.Stats()
	if stats["test.window"]["total_calls"] != 10 {
		t.Errorf("窗口内 total_calls 期望 10，实际 %v", stats["test.window"]["total_calls"])
	}
	if stats["test.window"]["failed_calls"] != 10 {
		t.Errorf("窗口内 failed_calls 期望 10，实际 %v", stats["test.window"]["failed_calls"])
	}

	manager.mu.Lock()
	if tracker, ok := manager.failureRateMap["test.window"]; ok {
		tracker.windowStart = time.Now().Add(-2 * time.Minute)
	}
	manager.mu.Unlock()

	manager.OnToolCall(AuditEntry{
		ToolName: "test.window",
		Success:  true,
		Duration: 100 * time.Millisecond,
	})

	stats = manager.Stats()
	if stats["test.window"]["total_calls"] != 1 {
		t.Errorf("窗口重置后 total_calls 期望 1，实际 %v", stats["test.window"]["total_calls"])
	}
	if stats["test.window"]["failed_calls"] != 0 {
		t.Errorf("窗口重置后 failed_calls 期望 0，实际 %v", stats["test.window"]["failed_calls"])
	}
}

// TestD15_7_ToolAlertManager_HandlerPanicRecovery 验证 handler panic 不影响其他 handler
func TestD15_7_ToolAlertManager_HandlerPanicRecovery(t *testing.T) {
	manager := NewToolAlertManager()

	panicHandler := AlertHandlerFunc(func(event AlertEvent) {
		panic("intentional panic in handler")
	})
	called := int32(0)
	normalHandler := AlertHandlerFunc(func(event AlertEvent) {
		atomic.AddInt32(&called, 1)
	})
	manager.AddHandler(panicHandler)
	manager.AddHandler(normalHandler)

	manager.AlertCircuitOpen("test.panic", CircuitOpen)
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("panic handler 不应影响其他 handler，normalHandler 应被调用 1 次，实际 %d", atomic.LoadInt32(&called))
	}
}

// TestD15_8_AlertEvent_MarshalJSON 验证 AlertEvent JSON 序列化
func TestD15_8_AlertEvent_MarshalJSON(t *testing.T) {
	event := AlertEvent{
		Level:    AlertCritical,
		Title:    "测试告警",
		Message:  "测试消息",
		ToolName: "test.json",
		Extra: map[string]any{
			"count": 42,
		},
	}
	b, err := event.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON 失败：%v", err)
	}
	s := string(b)
	if !strings.Contains(s, "测试告警") {
		t.Errorf("JSON 应含 Title，实际 %s", s)
	}
	if !strings.Contains(s, "critical") {
		t.Errorf("JSON 应含 level=critical，实际 %s", s)
	}
}

// TestD15_9_CompositeAuditLogger 验证复合 logger 同时写入 + 触发告警
func TestD15_9_CompositeAuditLogger(t *testing.T) {
	memoryLogger := NewMemoryAuditLogger(100)
	manager := NewToolAlertManager()
	var alertCount int32
	manager.AddHandler(AlertHandlerFunc(func(event AlertEvent) {
		atomic.AddInt32(&alertCount, 1)
	}))

	composite := NewCompositeAuditLogger(memoryLogger, nil, manager)

	composite.Log(context.Background(), AuditEntry{
		ToolName: "test.composite",
		Success:  true,
		Duration: 100 * time.Millisecond,
	})

	entries := memoryLogger.Entries()
	if len(entries) != 1 {
		t.Errorf("memory logger 应收到 1 条，实际 %d", len(entries))
	}

	composite.Log(context.Background(), AuditEntry{
		ToolName: "test.composite_slow",
		Success:  true,
		Duration: 6 * time.Second,
	})
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&alertCount) == 0 {
		t.Error("应触发耗时告警")
	}
}
