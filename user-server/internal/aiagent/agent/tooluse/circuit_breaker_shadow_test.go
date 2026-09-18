// circuit_breaker_shadow_test.go T-P1-04：观察态（shadow）熔断的契约。
//
// 本卡把 tooluse 里早已实现的熔断器挂上生产装饰链，交付形态是"先只判定不拦截"。
// 因此最要紧的可断言性质不是"熔断会拦"，而是 **shadow 一律不拦**——
// 一旦 shadow 里漏出拦截行为，灰度就变成了未经批准的行为变更。
//
// 三条对照缺一不可，否则"没拦"可能只是"没接上"：
//
//	enforce 同配置确实拦   → 证明装饰器生效
//	shadow 下熔断确实打开   → 证明走到了判定拒绝那一支
//	shadow 下请求全部到达工具本体 → 才叫"放行行为不变"
package tooluse

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// shadowTestConfig 冷却设成一小时：熔断一旦打开就不会因超时而自行进入半开，
// 让"打开之后还在放请求"这件事变得完全确定（不依赖测试跑多快）。
func shadowTestConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:    2,
		BaseCooldown:        time.Hour,
		MaxCooldown:         time.Hour,
		BackoffMultiplier:   2.0,
		HalfOpenMaxAttempts: 1,
	}
}

// countingFailingTool 记录自己被真正调用了几次——"到达工具本体"的唯一凭证。
type countingFailingTool struct {
	BaseTool
	mu    sync.Mutex
	calls int
}

func newCountingFailingTool(name string) *countingFailingTool {
	return &countingFailingTool{BaseTool: BaseTool{
		NameVal:     name,
		CategoryVal: CategoryCustomer,
		ParamsVal:   ToolParameters{Type: "object", Properties: map[string]ToolParam{}},
	}}
}

func (c *countingFailingTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	err := errors.New("downstream unavailable")
	return ErrorResult(c.NameVal, err), err
}

func (c *countingFailingTool) called() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func newExecutorWithBreaker(tool Tool, registry *CircuitBreakerRegistry, shadow bool, onDecision CircuitDecisionFunc) *ToolExecutor {
	reg := NewToolRegistry()
	_ = reg.Register(tool)
	return NewToolExecutor(reg, ToolExecutorConfig{
		DefaultTimeout:       5 * time.Second,
		CircuitBreaker:       registry,
		CircuitBreakerShadow: shadow,
		OnCircuitDecision:    onDecision,
	})
}

func execTool(t *testing.T, exec *ToolExecutor, name string) ExecuteResult {
	t.Helper()
	return exec.Execute(context.Background(), ExecuteRequest{ToolName: name, Args: map[string]any{}})
}

// TestCircuitBreaker_ShadowNeverBlocks AC②：shadow 模式下放行行为不变。
func TestCircuitBreaker_ShadowNeverBlocks(t *testing.T) {
	registry := NewCircuitBreakerRegistry(shadowTestConfig())
	tool := newCountingFailingTool("shadow.tool")
	exec := newExecutorWithBreaker(tool, registry, true, nil)

	const calls = 8
	for i := 0; i < calls; i++ {
		res := execTool(t, exec, "shadow.tool")
		if errors.Is(res.Err, ErrCircuitOpen) {
			t.Fatalf("第 %d 次调用被熔断拦下（shadow 模式不得拦截）：%v", i+1, res.Err)
		}
		if res.Err == nil || res.Err.Error() != "downstream unavailable" {
			t.Fatalf("第 %d 次调用应原样透传工具自身的错误，实际 err=%v", i+1, res.Err)
		}
	}
	if tool.called() != calls {
		t.Errorf("shadow 下 %d 次调用应全部到达工具本体，实际到达 %d 次", calls, tool.called())
	}
	// 前置条件：熔断确实打开了，否则"没拦"是因为压根没进熔断
	if st := registry.State("shadow.tool"); st != CircuitOpen {
		t.Fatalf("前置条件不成立：连续失败 %d 次后熔断应为 open，实际 %s", calls, st)
	}
}

// TestCircuitBreaker_EnforceDoesBlocks 反向对照：同一套配置下 enforce 确实会拦。
func TestCircuitBreaker_EnforceDoesBlocks(t *testing.T) {
	registry := NewCircuitBreakerRegistry(shadowTestConfig())
	tool := newCountingFailingTool("enforce.tool")
	exec := newExecutorWithBreaker(tool, registry, false, nil)

	const calls = 8
	blocked := 0
	for i := 0; i < calls; i++ {
		if res := execTool(t, exec, "enforce.tool"); errors.Is(res.Err, ErrCircuitOpen) {
			blocked++
		}
	}
	if blocked == 0 {
		t.Fatal("enforce 模式下一次都没拦 ⇒ 熔断未生效，则 shadow 的不拦毫无意义")
	}
	if reached := tool.called(); reached >= calls {
		t.Errorf("enforce 模式下应有调用被拦下，实际全部到达工具本体（%d 次）", reached)
	}
	// 阈值 2 ⇒ 前两次真跑、后六次被拦
	if tool.called() != 2 {
		t.Errorf("期望仅前 2 次到达工具本体，实际 %d 次", tool.called())
	}
}

// TestCircuitBreaker_NotWiredEqualsBeforeWiring 反向对照②：registry 为 nil 时与接线前一致。
func TestCircuitBreaker_NotWiredEqualsBeforeWiring(t *testing.T) {
	tool := newCountingFailingTool("nil.tool")
	exec := newExecutorWithBreaker(tool, nil, false, nil)
	for i := 0; i < 5; i++ {
		res := execTool(t, exec, "nil.tool")
		if errors.Is(res.Err, ErrCircuitOpen) {
			t.Fatalf("未接熔断却出现拦截：%v", res.Err)
		}
	}
	if tool.called() != 5 {
		t.Errorf("期望 5 次全部到达，实际 %d", tool.called())
	}
	// shadow 标志在无 registry 时也不得凭空造出拦截
	tool2 := newCountingFailingTool("nil2.tool")
	exec2 := newExecutorWithBreaker(tool2, nil, true, nil)
	for i := 0; i < 5; i++ {
		if res := execTool(t, exec2, "nil2.tool"); errors.Is(res.Err, ErrCircuitOpen) {
			t.Fatalf("未接熔断却出现拦截：%v", res.Err)
		}
	}
	if tool2.called() != 5 {
		t.Errorf("期望 5 次全部到达，实际 %d", tool2.called())
	}
}

// recordingTool 只记"有没有真的到达下游"。
func recordingHit(calls *int) ToolHandler {
	return func(ctx context.Context, args map[string]any) (ToolResult, error) {
		*calls++
		return SuccessResult("t", map[string]any{"ok": true}), nil
	}
}

// TestCircuitBreaker_ShadowSharesEnforceStateMachine shadow 不是"另一套近似判定"。
//
// 三条都要锁住，否则观察期得出的 would_block 数字不能外推到生效态：
//
//	① 打开期间：shadow 放行、enforce 拦下；
//	② 打开期间一次成功**不会**把熔断关掉（两种模式都必须走半开探测才闭合）；
//	③ 冷却过后：两种模式都在半开放行一次，成功即闭合。
func TestCircuitBreaker_ShadowSharesEnforceStateMachine(t *testing.T) {
	ctxOf := func() context.Context { return WithToolName(context.Background(), "t") }

	// ① + ②
	shadowReg := NewCircuitBreakerRegistry(shadowTestConfig())
	shadowReg.RecordFailure("t")
	shadowReg.RecordFailure("t")
	if st := shadowReg.State("t"); st != CircuitOpen {
		t.Fatalf("前置：期望 open，实际 %s", st)
	}
	shadowCalls := 0
	_, err := CircuitBreakerShadowDecorator(shadowReg, nil)(recordingHit(&shadowCalls))(ctxOf(), nil)
	if err != nil {
		t.Fatalf("shadow 不得外抛错误：%v", err)
	}
	if shadowCalls != 1 {
		t.Errorf("shadow 应把请求交给下游，实际下游被调用 %d 次", shadowCalls)
	}
	if st := shadowReg.State("t"); st != CircuitOpen {
		t.Errorf("一次放行成功不得跳过半开直接闭合，实际 %s", st)
	}

	enforceReg := NewCircuitBreakerRegistry(shadowTestConfig())
	enforceReg.RecordFailure("t")
	enforceReg.RecordFailure("t")
	enforceCalls := 0
	_, err = CircuitBreakerDecorator(enforceReg)(recordingHit(&enforceCalls))(ctxOf(), nil)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("生效态在熔断打开期间应返回 ErrCircuitOpen，实际 %v", err)
	}
	if enforceCalls != 0 {
		t.Errorf("生效态拦下时不应调用下游，实际调用 %d 次", enforceCalls)
	}

	// ③ 冷却过后（用短冷却配置，两种模式同一断言）
	shortCfg := CircuitBreakerConfig{
		FailureThreshold: 2, BaseCooldown: 20 * time.Millisecond, MaxCooldown: 20 * time.Millisecond,
		BackoffMultiplier: 2.0, HalfOpenMaxAttempts: 1,
	}
	modes := []struct {
		name string
		make func(r *CircuitBreakerRegistry) ToolDecorator
	}{
		{"shadow", func(r *CircuitBreakerRegistry) ToolDecorator { return CircuitBreakerShadowDecorator(r, nil) }},
		{"enforce", func(r *CircuitBreakerRegistry) ToolDecorator { return CircuitBreakerDecorator(r) }},
	}
	for _, m := range modes {
		reg := NewCircuitBreakerRegistry(shortCfg)
		reg.RecordFailure("t")
		reg.RecordFailure("t")
		if st := reg.State("t"); st != CircuitOpen {
			t.Fatalf("%s 前置：期望 open，实际 %s", m.name, st)
		}
		time.Sleep(30 * time.Millisecond)
		calls := 0
		if _, err := m.make(reg)(recordingHit(&calls))(ctxOf(), nil); err != nil {
			t.Errorf("%s 冷却过后半开探测应放行，实际 err=%v", m.name, err)
		}
		if calls != 1 {
			t.Errorf("%s 半开探测应到达下游一次，实际 %d 次", m.name, calls)
		}
		if st := reg.State("t"); st != CircuitClosed {
			t.Errorf("%s 半开成功后应闭合，实际 %s", m.name, st)
		}
	}
}

// TestCircuitDecisionCounter 判定累计器：计数、比率、排序、复位、nil 安全。
func TestCircuitDecisionCounter(t *testing.T) {
	c := NewCircuitDecisionCounter()
	observe := func(name string, block bool) {
		c.Observe(context.Background(), CircuitDecision{ToolName: name, Shadow: true, WouldBlock: block})
	}
	for i := 0; i < 5; i++ {
		observe("reach.send", i >= 2) // 5 次里 3 次会被拦
	}
	for i := 0; i < 4; i++ {
		observe("crm.query", i >= 3) // 4 次里 1 次
	}
	rep := c.Report()
	if rep.Total != 9 || rep.WouldBlock != 4 {
		t.Fatalf("计数错：%+v", rep)
	}
	if rep.WouldBlockRatePct < 44 || rep.WouldBlockRatePct > 45 {
		t.Fatalf("比率错：%.2f", rep.WouldBlockRatePct)
	}
	if len(rep.PerTool) != 2 || rep.PerTool[0].ToolName != "reach.send" {
		t.Fatalf("应按 would_block 降序：%+v", rep.PerTool)
	}
	if rep.PerTool[1].WouldBlock != 1 || rep.PerTool[1].Total != 4 {
		t.Fatalf("单工具计数错：%+v", rep.PerTool[1])
	}

	c.Reset()
	if got := c.Report(); got.Total != 0 || len(got.PerTool) != 0 {
		t.Fatalf("Reset 后应清零：%+v", got)
	}

	var nilCounter *CircuitDecisionCounter
	nilCounter.Observe(context.Background(), CircuitDecision{}) // 不得 panic
	if got := nilCounter.Report(); got.Total != 0 {
		t.Fatalf("nil 接收者 Report 应为空：%+v", got)
	}
	nilCounter.Reset()
}

// TestCircuitBreaker_DecisionCallbackFires 回调次数与调用次数一致，且字段如实反映判定。
func TestCircuitBreaker_DecisionCallbackFires(t *testing.T) {
	registry := NewCircuitBreakerRegistry(shadowTestConfig())
	var mu sync.Mutex
	var got []CircuitDecision
	onDecision := func(_ context.Context, d CircuitDecision) {
		mu.Lock()
		got = append(got, d)
		mu.Unlock()
	}
	tool := newCountingFailingTool("cb.tool")
	exec := newExecutorWithBreaker(tool, registry, true, onDecision)

	const calls = 6
	for i := 0; i < calls; i++ {
		execTool(t, exec, "cb.tool")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != calls {
		t.Fatalf("判定回调 %d 次，调用 %d 次（每次调用应判定一次）", len(got), calls)
	}
	blocked := 0
	for _, d := range got {
		if !d.Shadow {
			t.Fatal("shadow 模式下判定应带 Shadow=true")
		}
		if d.ToolName != "cb.tool" || d.FailureThreshold != 2 || d.BaseCooldown != time.Hour {
			t.Fatalf("判定字段不全：%+v", d)
		}
		if d.WouldBlock {
			blocked++
		}
	}
	if blocked != calls-2 {
		t.Errorf("阈值 2 ⇒ 前 2 次放行、后 %d 次应记为 would_block，实际 %d", calls-2, blocked)
	}
}

// TestCircuitBreaker_EnforceReportsAndRejects 生效态也要出报告（转阻断后仍要看得见拦了多少）。
func TestCircuitBreaker_EnforceReportsAndRejects(t *testing.T) {
	registry := NewCircuitBreakerRegistry(shadowTestConfig())
	reported := 0
	var mu sync.Mutex
	counter := NewCircuitDecisionCounter()
	onDecision := func(ctx context.Context, d CircuitDecision) {
		mu.Lock()
		reported++
		mu.Unlock()
		counter.Observe(ctx, d)
	}
	tool := newCountingFailingTool("enf.tool")
	exec := newExecutorWithBreaker(tool, registry, false, onDecision)

	for i := 0; i < 4; i++ {
		execTool(t, exec, "enf.tool")
	}
	mu.Lock()
	gotReported := reported
	mu.Unlock()
	if gotReported != 4 {
		t.Fatalf("生效态判定回调 %d 次，期望 4", gotReported)
	}
	rep := counter.Report()
	if rep.Total != 4 || rep.WouldBlock == 0 {
		t.Fatalf("生效态报告异常：%+v", rep)
	}
	if rep.PerTool[0].LastState.String() != "open" {
		t.Errorf("末态应为 open，实际 %s", rep.PerTool[0].LastState.String())
	}
}

// TestCircuitBreakerRegistry_ResetTool 运维复位：条目删除 ⇒ 状态与退避历史一起归零。
func TestCircuitBreakerRegistry_ResetTool(t *testing.T) {
	registry := NewCircuitBreakerRegistry(shadowTestConfig())
	registry.RecordFailure("r.tool")
	registry.RecordFailure("r.tool")
	if st := registry.State("r.tool"); st != CircuitOpen {
		t.Fatalf("前置：期望 open，实际 %s", st)
	}
	if registry.AllStates()["r.tool"].OpenCount == 0 {
		t.Fatal("前置：open_count 应已累加")
	}

	registry.ResetTool("r.tool")
	if _, ok := registry.AllStates()["r.tool"]; ok {
		t.Fatal("复位后不应再能看到该工具的熔断条目")
	}
	if st := registry.State("r.tool"); st != CircuitClosed {
		t.Fatalf("复位后应为 closed，实际 %s", st)
	}
	if !registry.Allow("r.tool") {
		t.Fatal("复位后应立即放行")
	}
}

// countingLimiter 记录 Acquire 被调用几次——用来分辨限流器在重试内侧还是外侧。
type countingLimiter struct {
	mu  sync.Mutex
	acq int
}

func (l *countingLimiter) Acquire(ctx context.Context, key string) error {
	l.mu.Lock()
	l.acq++
	l.mu.Unlock()
	return nil
}

func (l *countingLimiter) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.acq
}

// TestBuildChainWithBreakerDecoratorNilEqualsDefaultChain breaker 为 nil 时必须退化到 BuildDefaultChain。
//
// 链序不是风格问题：BuildDefaultChain 把 RateLimit 放在 Retry 的**内侧**（每次重试各自过一遍限流），
// 熔断链把 RateLimit 放在 Retry 的**外侧**（整次调用只过一遍）。本卡换掉了 executor 的建链入口，
// 若 nil 分支不小心走了熔断链，限流配额消耗会从 MaxAttempts 倍变成 1 倍——用调用次数能直接测出来。
func TestBuildChainWithBreakerDecoratorNilEqualsDefaultChain(t *testing.T) {
	failing := ToolHandler(func(ctx context.Context, args map[string]any) (ToolResult, error) {
		return ErrorResult("x", errors.New("boom")), errors.New("boom")
	})
	policy := NewExponentialBackoffPolicy(3, time.Millisecond, 2*time.Millisecond)
	audit := NewMemoryAuditLogger(8)
	cost := NewMemoryCostTracker()
	ctx := WithToolName(context.Background(), "x")

	run := func(h ToolHandler) {
		_, _ = h(ctx, nil)
	}

	defaultLim := &countingLimiter{}
	run(BuildDefaultChain(failing, NoOpPermissionChecker{}, defaultLim, policy, time.Second, audit, cost))
	if n := defaultLim.count(); n != 3 {
		t.Fatalf("对照组（BuildDefaultChain）每次重试各过一遍限流，期望 3 次，实际 %d", n)
	}

	nilLim := &countingLimiter{}
	run(BuildChainWithBreakerDecorator(failing, NoOpPermissionChecker{}, nilLim, nil, policy, time.Second, audit, cost))
	if n := nilLim.count(); n != defaultLim.count() {
		t.Fatalf("breaker 为 nil 时链序与 BuildDefaultChain 不一致：限流被调用 %d 次，对照组 %d 次", n, defaultLim.count())
	}

	breakerLim := &countingLimiter{}
	breaker := CircuitBreakerDecorator(NewCircuitBreakerRegistry(shadowTestConfig()))
	run(BuildChainWithBreakerDecorator(failing, NoOpPermissionChecker{}, breakerLim, breaker, policy, time.Second, audit, cost))
	if n := breakerLim.count(); n == defaultLim.count() {
		t.Fatalf("接入熔断后仍走了默认链（限流 %d 次）", n)
	}
}

// TestCircuitBreaker_ConcurrentExecution 并发跑一遍装饰链 + 判定累计器。
//
// T-P1-02 的教训：竞争检测要**真的重叠**才有效——这里 8 个 goroutine 各 50 次调用共享
// 同一个 registry 与 counter，且读侧（State/AllStates/Report）在同一批 goroutine 里紧循环，
// 而不是事后单独读一次。
func TestCircuitBreaker_ConcurrentExecution(t *testing.T) {
	registry := NewCircuitBreakerRegistry(shadowTestConfig())
	counter := NewCircuitDecisionCounter()
	var observed int
	var mu sync.Mutex
	onDecision := func(ctx context.Context, d CircuitDecision) {
		counter.Observe(ctx, d)
		mu.Lock()
		observed++
		mu.Unlock()
	}
	tool := newCountingFailingTool("conc.tool")
	exec := newExecutorWithBreaker(tool, registry, true, onDecision)

	const writers, perWriter = 8, 50
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := WithToolName(context.Background(), "conc.tool")
			for i := 0; i < perWriter; i++ {
				_ = registry.State("conc.tool")
				_ = registry.AllStates()
				_ = counter.Report()
				exec.Execute(ctx, ExecuteRequest{ToolName: "conc.tool", Args: map[string]any{}})
			}
		}()
	}
	wg.Wait()

	rep := counter.Report()
	if want := int64(writers * perWriter); rep.Total != want {
		t.Errorf("判定回调应等于调用次数：total=%d 期望 %d", rep.Total, want)
	}
	if rep.WouldBlock == 0 {
		t.Error("并发跑完后应已出现会被拦的判定")
	}
	if got := tool.called(); got == 0 {
		t.Error("shadow 下并发调用应全部到达工具本体")
	}
}
