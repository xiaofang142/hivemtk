package llm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeChecker struct {
	mu      sync.Mutex
	latency int64
	err     error
	calls   map[string]int
}

func newFakeChecker(latency int64, err error) *fakeChecker {
	return &fakeChecker{
		latency: latency,
		err:     err,
		calls:   make(map[string]int),
	}
}

func (f *fakeChecker) Ping(ctx context.Context, provider *ProviderConfig, config FailoverConfig) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[provider.Name]++
	return f.latency, f.err
}

func newTestFailover(t *testing.T, dispatcher *Dispatcher) *ProviderFailover {
	return NewProviderFailover(dispatcher)
}

// setFailoverPolicyJSON 把一份策略 JSON 灌进参数中心的读取口（装配层注入的那个闭包），
// 用例结束摘回未注入状态。provider 是包级变量，不还原会让下一格"未注入"的断言
// 读到上一格留下的值。
func setFailoverPolicyJSON(t *testing.T, raw string) {
	t.Helper()
	SetFailoverPolicyProvider(func(context.Context) string { return raw })
	t.Cleanup(func() { SetFailoverPolicyProvider(nil) })
}

// 1. 默认配置 - 字段正确
func TestDefaultFailoverConfig(t *testing.T) {
	cfg := DefaultFailoverConfig()
	if cfg.HealthCheckInterval != 30 {
		t.Errorf("expected 30, got %d", cfg.HealthCheckInterval)
	}
	if cfg.FailureThreshold != 5 {
		t.Errorf("expected 5, got %d", cfg.FailureThreshold)
	}
	if cfg.CircuitOpenDuration != 60 {
		t.Errorf("expected 60, got %d", cfg.CircuitOpenDuration)
	}
	if cfg.LocalFallbackProvider != "default" {
		t.Errorf("expected default, got %s", cfg.LocalFallbackProvider)
	}
	if cfg.TemplateReply == "" {
		t.Error("expected non-empty template reply")
	}
}

// 2. 默认策略 - 包含所有场景
func TestDefaultFailoverPolicy(t *testing.T) {
	p := DefaultFailoverPolicy()
	if len(p.Scenarios) == 0 {
		t.Fatal("expected non-empty scenarios")
	}
	list, ok := p.Scenarios["intent_recognize"]
	if !ok || len(list) == 0 {
		t.Fatal("expected intent_recognize scenario")
	}
	if list[0] != "default" {
		t.Errorf("expected default first, got %s", list[0])
	}
}

// 3. 健康检查成功 - 状态 up
func TestHealthCheckSuccess(t *testing.T) {
	d := newTestDispatcher()
	d.AddProvider(ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true})
	f := newTestFailover(t, d)
	f.SetHealthChecker(newFakeChecker(100, nil))
	f.checkOne(context.Background(), &ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true}, f.Config())
	h := f.GetHealth("p1")
	if h == nil {
		t.Fatal("expected health record")
	}
	if h.Status != ProviderStatusUp {
		t.Errorf("expected up, got %s", h.Status)
	}
	if h.ConsecutiveFailures != 0 {
		t.Errorf("expected 0 failures, got %d", h.ConsecutiveFailures)
	}
	if h.LatencyP95Ms != 100 {
		t.Errorf("expected 100ms, got %d", h.LatencyP95Ms)
	}
}

// 4. 健康检查失败 - 累计失败次数
func TestHealthCheckFailure(t *testing.T) {
	d := newTestDispatcher()
	d.AddProvider(ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true})
	f := newTestFailover(t, d)
	f.SetHealthChecker(newFakeChecker(0, errors.New("connect refused")))
	for i := 0; i < 3; i++ {
		f.checkOne(context.Background(), &ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true}, f.Config())
	}
	h := f.GetHealth("p1")
	if h.Status != ProviderStatusDegraded {
		t.Errorf("expected degraded, got %s", h.Status)
	}
	if h.ConsecutiveFailures != 3 {
		t.Errorf("expected 3 failures, got %d", h.ConsecutiveFailures)
	}
}

// 5. 连续 5 次失败 - 触发熔断
func TestCircuitOpen(t *testing.T) {
	d := newTestDispatcher()
	d.AddProvider(ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true})
	f := newTestFailover(t, d)
	f.SetHealthChecker(newFakeChecker(0, errors.New("timeout")))
	for i := 0; i < 5; i++ {
		f.checkOne(context.Background(), &ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true}, f.Config())
	}
	h := f.GetHealth("p1")
	if h.Status != ProviderStatusDown {
		t.Errorf("expected down, got %s", h.Status)
	}
	if h.CircuitOpenUntil.IsZero() {
		t.Error("expected circuit open")
	}
	if !f.IsCircuitOpen("p1") {
		t.Error("expected circuit open")
	}
}

// 6. 熔断重置 - ResetCircuit
func TestResetCircuit(t *testing.T) {
	d := newTestDispatcher()
	d.AddProvider(ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true})
	f := newTestFailover(t, d)
	f.SetHealthChecker(newFakeChecker(0, errors.New("timeout")))
	for i := 0; i < 5; i++ {
		f.checkOne(context.Background(), &ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true}, f.Config())
	}
	if !f.IsCircuitOpen("p1") {
		t.Fatal("expected circuit open")
	}
	if !f.ResetCircuit("p1") {
		t.Fatal("expected reset success")
	}
	if f.IsCircuitOpen("p1") {
		t.Error("expected circuit closed after reset")
	}
	h := f.GetHealth("p1")
	if h.Status != ProviderStatusUp {
		t.Errorf("expected up, got %s", h.Status)
	}
}

// 7. ResetCircuit 不存在的 provider
func TestResetCircuitNonExist(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	if f.ResetCircuit("non-exist") {
		t.Error("expected false for non-exist provider")
	}
}

// 8. 延迟超过阈值 - degraded
func TestDegradedByLatency(t *testing.T) {
	d := newTestDispatcher()
	d.AddProvider(ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true})
	f := newTestFailover(t, d)
	cfg := f.Config()
	cfg.DegradedLatencyMs = 500
	f.ApplyConfig(cfg)
	f.SetHealthChecker(newFakeChecker(800, nil))
	f.checkOne(context.Background(), &ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true}, f.Config())
	h := f.GetHealth("p1")
	if h.Status != ProviderStatusDegraded {
		t.Errorf("expected degraded, got %s", h.Status)
	}
}

// 9. RecordSuccess 重置失败计数
func TestRecordSuccess(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	f.RecordFailure("p1", errors.New("err1"))
	f.RecordFailure("p1", errors.New("err2"))
	f.RecordSuccess("p1", 200)
	h := f.GetHealth("p1")
	if h.ConsecutiveFailures != 0 {
		t.Errorf("expected 0 failures, got %d", h.ConsecutiveFailures)
	}
	if h.Status != ProviderStatusUp {
		t.Errorf("expected up, got %s", h.Status)
	}
}

// 10. RecordFailure 累计
func TestRecordFailure(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	f.RecordFailure("p1", errors.New("err1"))
	h := f.GetHealth("p1")
	if h.ConsecutiveFailures != 1 {
		t.Errorf("expected 1, got %d", h.ConsecutiveFailures)
	}
	if h.Status != ProviderStatusDegraded {
		t.Errorf("expected degraded, got %s", h.Status)
	}
}

// 11. 熔断时间过期 - 自动恢复探测
func TestCircuitExpiry(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	f.RecordFailure("p1", errors.New("err"))
	h := f.GetHealth("p1")
	h.CircuitOpenUntil = time.Now().Add(-time.Second)
	if f.IsCircuitOpen("p1") {
		t.Error("expected circuit closed after expiry")
	}
}

// 12. 未注入读取口时使用默认策略（装配层没接上、或启动早期还没有参数中心）
func TestLoadPolicyNoProvider(t *testing.T) {
	SetFailoverPolicyProvider(nil)
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	p := f.LoadPolicy(context.Background())
	def := DefaultFailoverPolicy()
	if len(p.Scenarios) != len(def.Scenarios) {
		t.Errorf("未注入时场景数 = %d，期望 %d", len(p.Scenarios), len(def.Scenarios))
	}
	if p.Config.HealthCheckInterval != def.Config.HealthCheckInterval {
		t.Errorf("未注入时 health_check_interval = %d，期望 %d",
			p.Config.HealthCheckInterval, def.Config.HealthCheckInterval)
	}
}

// 12b. 读取口存在但参数值为空：等价于"没有覆盖"，必须逐项回落内置默认，
// 而不是把零值配置当成运维真的填了一份 0 超时、空兜底 provider 的策略。
func TestLoadPolicyEmptyValueKeepsDefaults(t *testing.T) {
	setFailoverPolicyJSON(t, "")
	f := newTestFailover(t, newTestDispatcher())
	p := f.LoadPolicy(context.Background())
	def := DefaultFailoverPolicy()
	if p.Config != def.Config {
		t.Errorf("空参数值应等于内置默认配置\ngot  = %+v\nwant = %+v", p.Config, def.Config)
	}
	if len(p.Scenarios) != len(def.Scenarios) {
		t.Errorf("空参数值应保留默认场景表：got %d 条，want %d 条", len(p.Scenarios), len(def.Scenarios))
	}
}

// 12c. 每次调用都重新问读取口。这里注入的是闭包而不是值，为的就是"运维在管理台
// 改完策略，下一个健康检查 tick 直接生效"；一旦有人在 LoadPolicy 里缓存结果，
// 这条会立刻红。
func TestLoadPolicyRereadsProviderEachCall(t *testing.T) {
	var calls int
	SetFailoverPolicyProvider(func(context.Context) string {
		calls++
		if calls == 1 {
			return `{"config":{"failure_threshold":3}}`
		}
		return `{"config":{"failure_threshold":9}}`
	})
	t.Cleanup(func() { SetFailoverPolicyProvider(nil) })

	f := newTestFailover(t, newTestDispatcher())
	if got := f.LoadPolicy(context.Background()).Config.FailureThreshold; got != 3 {
		t.Fatalf("首读 FailureThreshold = %d，期望 3", got)
	}
	if got := f.LoadPolicy(context.Background()).Config.FailureThreshold; got != 9 {
		t.Errorf("二读 FailureThreshold = %d，期望 9（读到旧值说明策略被缓存了，改配置得重启才生效）", got)
	}
	if calls != 2 {
		t.Errorf("读取口调用次数 = %d，期望 2", calls)
	}
}

// 12d. 只写 config、不写 scenarios：候选表必须原样保留。
// 这一格盯的是 LoadPolicy 里 "len(loaded.Scenarios) > 0" 那道闸——JSON 里缺 scenarios
// 时 Go 解出来是 nil map，没有这道闸就等于把 7 个场景的候选表整体清空，
// 而清空不会报错，只会让每个意图少掉几级降级。
func TestLoadPolicyConfigOnlyKeepsDefaultScenarios(t *testing.T) {
	setFailoverPolicyJSON(t, `{"config":{"failure_threshold":2}}`)
	f := newTestFailover(t, newTestDispatcher())
	p := f.LoadPolicy(context.Background())
	def := DefaultFailoverPolicy()
	if len(p.Scenarios) != len(def.Scenarios) {
		t.Errorf("只改 config 时场景表应保留默认 %d 条，got %d 条：%v", len(def.Scenarios), len(p.Scenarios), p.Scenarios)
	}
	if p.Config.FailureThreshold != 2 {
		t.Errorf("config 字段应被覆盖：FailureThreshold = %d，期望 2", p.Config.FailureThreshold)
	}
}

// 13. LoadPolicy 用上参数中心注入的整份策略 JSON：config 里 >0/非空的字段覆盖默认，
// scenarios 整表替换。断言逐字段写，是为了让"漏 merge 某一个字段"这种改动能被点名。
func TestLoadPolicyFromParamCenter(t *testing.T) {
	setFailoverPolicyJSON(t, `{"config":{"health_check_interval":60,"failure_threshold":3,"circuit_open_duration":120,"degraded_latency_ms":5000,"local_fallback_provider":"my_local","template_reply":"custom reply","health_check_path":"/v1/health"},"scenarios":{"intent_recognize":["default","custom"]}}`)
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	p := f.LoadPolicy(context.Background())
	if p.Config.HealthCheckInterval != 60 {
		t.Errorf("expected 60, got %d", p.Config.HealthCheckInterval)
	}
	if p.Config.FailureThreshold != 3 {
		t.Errorf("expected 3, got %d", p.Config.FailureThreshold)
	}
	if p.Config.CircuitOpenDuration != 120 {
		t.Errorf("expected 120, got %d", p.Config.CircuitOpenDuration)
	}
	if p.Config.DegradedLatencyMs != 5000 {
		t.Errorf("expected 5000, got %d", p.Config.DegradedLatencyMs)
	}
	if p.Config.LocalFallbackProvider != "my_local" {
		t.Errorf("expected my_local, got %s", p.Config.LocalFallbackProvider)
	}
	if p.Config.TemplateReply != "custom reply" {
		t.Errorf("expected custom reply, got %s", p.Config.TemplateReply)
	}
	if p.Config.HealthCheckPath != "/v1/health" {
		t.Errorf("expected /v1/health, got %s", p.Config.HealthCheckPath)
	}
	list := p.Scenarios["intent_recognize"]
	if len(list) != 2 || list[1] != "custom" {
		t.Errorf("expected [default, custom], got %v", list)
	}
	// scenarios 是**整表替换**（不是按 key 合并）：JSON 写了 scenarios 就以它为准。
	// 这里锁死这个形状，因为它是运维最容易踩的一侧——少写一个场景不会报错，
	// 只是那个场景改由调度路由 + 本地兜底给候选（见 buildCandidates），降级层级会变少。
	if len(p.Scenarios) != 1 {
		t.Errorf("scenarios 应整表替换为 JSON 里的 1 条，got %d 条：%v", len(p.Scenarios), p.Scenarios)
	}
	if _, ok := p.Scenarios["sop_reply"]; ok {
		t.Errorf("JSON 里没有 sop_reply，替换后不应仍有该场景")
	}
}

// 14. buildCandidates 包含本地兜底
func TestBuildCandidatesIncludesFallback(t *testing.T) {
	d := newTestDispatcher()
	d.SetRoute(ScenarioRoute{Scenario: ScenarioIntentRecognize, Provider: "deepseek", Fallbacks: []string{"qwen"}})
	f := newTestFailover(t, d)
	policy := DefaultFailoverPolicy()
	list := f.buildCandidates(ScenarioIntentRecognize, policy)
	found := false
	for _, n := range list {
		if n == "default" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected default in candidates, got %v", list)
	}
}

// 15. IsDegraded 判断降级响应
func TestIsDegraded(t *testing.T) {
	if IsDegraded(nil) {
		t.Error("nil should not be degraded")
	}
	r := &DispatchResult{Provider: "degraded", Model: "template"}
	if !IsDegraded(r) {
		t.Error("expected degraded")
	}
	r2 := &DispatchResult{Provider: "deepseek", Model: "deepseek-chat"}
	if IsDegraded(r2) {
		t.Error("normal result should not be degraded")
	}
}

// 16. HTTPHealthChecker 无 BaseURL 视为本地（成功）
func TestHTTPHealthCheckerLocal(t *testing.T) {
	c := NewHTTPHealthChecker()
	latency, err := c.Ping(context.Background(), &ProviderConfig{Name: "local"}, DefaultFailoverConfig())
	if err != nil {
		t.Errorf("expected nil err for local, got %v", err)
	}
	if latency != 0 {
		t.Errorf("expected 0 latency for local, got %d", latency)
	}
}

// 17. HTTPHealthChecker nil provider
func TestHTTPHealthCheckerNil(t *testing.T) {
	c := NewHTTPHealthChecker()
	_, err := c.Ping(context.Background(), nil, DefaultFailoverConfig())
	if err == nil {
		t.Error("expected error for nil provider")
	}
}

// 18. GetHealth 不存在的 provider 返回 nil
func TestGetHealthNonExist(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	if h := f.GetHealth("non-exist"); h != nil {
		t.Errorf("expected nil, got %v", h)
	}
}

// 19. GetAllHealth 返回所有
func TestGetAllHealth(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	f.RecordSuccess("p1", 100)
	f.RecordSuccess("p2", 200)
	all := f.GetAllHealth()
	if len(all) != 2 {
		t.Errorf("expected 2, got %d", len(all))
	}
}

// 20. ApplyConfig 修改配置
func TestApplyConfig(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	cfg := f.Config()
	cfg.FailureThreshold = 10
	f.ApplyConfig(cfg)
	if f.Config().FailureThreshold != 10 {
		t.Errorf("expected 10, got %d", f.Config().FailureThreshold)
	}
}

// 21. DispatchWithFailover 全部失败 → 降级响应
func TestDispatchWithFailoverAllFailed(t *testing.T) {
	d := newTestDispatcher()
	d.AddProvider(ProviderConfig{Name: "p1", BaseURL: "http://nonexist.invalid", Enabled: true})
	f := newTestFailover(t, d)
	result, err := f.DispatchWithFailover(context.Background(), DispatchRequest{
		Scenario: ScenarioIntentRecognize,
		Prompt:   "hello",
	})
	if err != nil {
		t.Fatalf("expected nil err (degraded), got %v", err)
	}
	if !IsDegraded(result) {
		t.Errorf("expected degraded result, got provider=%s model=%s content=%s",
			result.Provider, result.Model, result.Content)
	}
	if result.Content == "" {
		t.Error("degraded response should have non-empty content")
	}
}

// 22. Stop 不重复关闭
func TestFailoverStop(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	f.Stop()
	f.Stop()
}

// 23. Start 后 Stop 优雅关闭
func TestFailoverStartStop(t *testing.T) {
	d := newTestDispatcher()
	d.AddProvider(ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true})
	f := newTestFailover(t, d)
	f.SetHealthChecker(newFakeChecker(50, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	f.Stop()
}

// 24. LoadPolicy 异常 JSON 使用默认
func TestLoadPolicyBadJSON(t *testing.T) {
	setFailoverPolicyJSON(t, "not-json")
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	p := f.LoadPolicy(context.Background())
	if p.Config.HealthCheckInterval != 30 {
		t.Errorf("expected default 30, got %d", p.Config.HealthCheckInterval)
	}
	// 非法 JSON 必须整份作废、逐项回落默认，而不是把解析了一半的字段当策略用上。
	def := DefaultFailoverPolicy()
	if p.Config != def.Config {
		t.Errorf("非法 JSON 后配置应逐项回落默认\ngot  = %+v\nwant = %+v", p.Config, def.Config)
	}
	if len(p.Scenarios) != len(def.Scenarios) {
		t.Errorf("非法 JSON 后应保留默认场景表：got %d 条，want %d 条", len(p.Scenarios), len(def.Scenarios))
	}
}

// 25. interval 默认值
func TestIntervalDefault(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	if d := f.interval(); d != 30*time.Second {
		t.Errorf("expected 30s, got %v", d)
	}
}

// 26. SetHealthChecker 注入 nil 不影响默认
func TestSetHealthCheckerNil(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	f.SetHealthChecker(nil)
	latency, err := f.checker.Ping(context.Background(), &ProviderConfig{Name: "local"}, f.Config())
	if err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
	if latency != 0 {
		t.Errorf("expected 0, got %d", latency)
	}
}

// 27. 健康检查成功后状态恢复
func TestRecoverFromDegraded(t *testing.T) {
	d := newTestDispatcher()
	f := newTestFailover(t, d)
	f.SetHealthChecker(newFakeChecker(0, errors.New("err")))
	f.checkOne(context.Background(), &ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true}, f.Config())
	if f.GetHealth("p1").Status != ProviderStatusDegraded {
		t.Fatal("expected degraded")
	}
	f.SetHealthChecker(newFakeChecker(100, nil))
	f.checkOne(context.Background(), &ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true}, f.Config())
	if f.GetHealth("p1").Status != ProviderStatusUp {
		t.Errorf("expected up, got %s", f.GetHealth("p1").Status)
	}
}

// 28. 熔断期间跳过该 provider
func TestCircuitOpenSkipsProvider(t *testing.T) {
	d := newTestDispatcher()
	d.AddProvider(ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true})
	f := newTestFailover(t, d)
	f.mu.Lock()
	f.health["p1"] = &ProviderHealth{
		ProviderName:     "p1",
		Status:           ProviderStatusDown,
		CircuitOpenUntil: time.Now().Add(1 * time.Hour),
	}
	f.mu.Unlock()
	if !f.IsCircuitOpen("p1") {
		t.Fatal("expected circuit open")
	}
}

// 29. 健康检查跳过 disabled provider
func TestCheckAllSkipsDisabled(t *testing.T) {
	d := newTestDispatcher()
	d.AddProvider(ProviderConfig{Name: "p1", BaseURL: "http://example.com", Enabled: true})
	d.AddProvider(ProviderConfig{Name: "p2", BaseURL: "http://example.com", Enabled: false})
	f := newTestFailover(t, d)
	f.SetHealthChecker(newFakeChecker(100, nil))
	f.checkAll(context.Background())
	if f.GetHealth("p1") == nil {
		t.Error("expected p1 checked")
	}
	if f.GetHealth("p2") != nil {
		t.Error("expected p2 skipped")
	}
}

// 30. DefaultFailoverConfig HealthCheckPath
func TestDefaultHealthCheckPath(t *testing.T) {
	cfg := DefaultFailoverConfig()
	if cfg.HealthCheckPath != "/health" {
		t.Errorf("expected /health, got %s", cfg.HealthCheckPath)
	}
}
