package tooluse

import (
	"context"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"
)

type ToolHandler func(ctx context.Context, args map[string]any) (ToolResult, error)

type ToolDecorator func(next ToolHandler) ToolHandler

type ToolContext struct {
	CallerID    string
	AgentID     string
	CustomerID  string
	SessionID   string
	Permissions []string
	AuditTrace  string
	Source      string
}

type toolContextKey struct{}

func WithToolContext(ctx context.Context, tc *ToolContext) context.Context {
	return context.WithValue(ctx, toolContextKey{}, tc)
}

func GetToolContext(ctx context.Context) *ToolContext {
	if v, ok := ctx.Value(toolContextKey{}).(*ToolContext); ok {
		return v
	}
	return nil
}

type toolNameKey struct{}

func WithToolName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, toolNameKey{}, name)
}

func GetToolName(ctx context.Context) string {
	if v, ok := ctx.Value(toolNameKey{}).(string); ok {
		return v
	}
	return ""
}

type traceIDKey struct{}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

func GetTraceID(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey{}).(string); ok {
		return v
	}
	return ""
}

func ChainDecorators(handler ToolHandler, decorators ...ToolDecorator) ToolHandler {

	for i := len(decorators) - 1; i >= 0; i-- {
		if decorators[i] == nil {
			continue
		}
		handler = decorators[i](handler)
	}
	return handler
}

func BuildDefaultChain(
	handler ToolHandler,
	checker PermissionChecker,
	limiter RateLimiter,
	policy RetryPolicy,
	timeout time.Duration,
	logger AuditLogger,
	costTracker CostTracker,
) ToolHandler {
	return ChainDecorators(handler,
		PermissionDecorator(checker),
		RetryDecorator(policy),
		TimeoutDecorator(timeout),
		RateLimitDecorator(limiter),
		AuditDecorator(logger, costTracker),
	)
}

func BuildChainWithCircuitBreaker(
	handler ToolHandler,
	checker PermissionChecker,
	limiter RateLimiter,
	circuitBreaker *CircuitBreakerRegistry,
	policy RetryPolicy,
	timeout time.Duration,
	logger AuditLogger,
	costTracker CostTracker,
) ToolHandler {
	if circuitBreaker == nil {
		return BuildDefaultChain(handler, checker, limiter, policy, timeout, logger, costTracker)
	}
	return BuildChainWithBreakerDecorator(handler, checker, limiter,
		CircuitBreakerDecorator(circuitBreaker), policy, timeout, logger, costTracker)
}

// BuildChainWithBreakerDecorator 用调用方自备的熔断装饰器建链。
//
// 存在的理由：熔断有生效/观察两种形态（见 CircuitBreakerShadowDecorator），
// 只传 *CircuitBreakerRegistry 无法表达这一选择。breaker 为 nil 时等价 BuildDefaultChain。
func BuildChainWithBreakerDecorator(
	handler ToolHandler,
	checker PermissionChecker,
	limiter RateLimiter,
	breaker ToolDecorator,
	policy RetryPolicy,
	timeout time.Duration,
	logger AuditLogger,
	costTracker CostTracker,
) ToolHandler {
	if breaker == nil {
		return BuildDefaultChain(handler, checker, limiter, policy, timeout, logger, costTracker)
	}
	return ChainDecorators(handler,
		PermissionDecorator(checker),
		RateLimitDecorator(limiter),
		breaker,
		RetryDecorator(policy),
		TimeoutDecorator(timeout),
		AuditDecorator(logger, costTracker),
	)
}

func BuildChainWithCircuitBreakerAndValidator(
	handler ToolHandler,
	checker PermissionChecker,
	limiter RateLimiter,
	circuitBreaker *CircuitBreakerRegistry,
	registry *ToolRegistry,
	policy RetryPolicy,
	timeout time.Duration,
	logger AuditLogger,
	costTracker CostTracker,
) ToolHandler {
	chain := []ToolDecorator{
		PermissionDecorator(checker),
		RateLimitDecorator(limiter),
	}
	if circuitBreaker != nil {
		chain = append(chain, CircuitBreakerDecorator(circuitBreaker))
	}
	if registry != nil {
		chain = append(chain, ParamValidatorDecorator(registry))
	}
	chain = append(chain, RetryDecorator(policy), TimeoutDecorator(timeout), AuditDecorator(logger, costTracker))
	return ChainDecorators(handler, chain...)
}

func (NoOpPermissionChecker) Check(ctx context.Context, toolName string, tc *ToolContext) error {
	return nil
}

func (NoOpRateLimiter) Acquire(ctx context.Context, key string) error { return nil }

func (NoOpAuditLogger) Log(ctx context.Context, entry AuditEntry) {}

func (NoOpCostTracker) Record(ctx context.Context, toolName string, success bool, duration time.Duration) error {
	return nil
}

func (l *TokenBucketLimiter) Acquire(ctx context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: float64(l.burst), lastRef: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.lastRef).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.lastRef = now
	if b.tokens < 1.0 {
		return ErrRateLimited
	}
	b.tokens -= 1.0
	return nil
}

// ExponentialBackoffPolicy 指数退避重试策略
type ExponentialBackoffPolicy struct {
	MaxAttemptsValue int
	BaseDelay        time.Duration
	MaxDelay         time.Duration
	Jitter           bool
}

// NewExponentialBackoffPolicy 创建指数退避策略
func NewExponentialBackoffPolicy(maxAttempts int, baseDelay, maxDelay time.Duration) *ExponentialBackoffPolicy {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if baseDelay <= 0 {
		baseDelay = 100 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 10 * time.Second
	}
	return &ExponentialBackoffPolicy{
		MaxAttemptsValue: maxAttempts,
		BaseDelay:        baseDelay,
		MaxDelay:         maxDelay,
		Jitter:           true,
	}
}

// MaxAttempts 最大重试次数
func (p *ExponentialBackoffPolicy) MaxAttempts() int {
	return p.MaxAttemptsValue
}

// NextBackoff 下一次重试延迟
// attempt 从 1 开始（第 1 次重试的延迟）
// delay = baseDelay * 2^(attempt-1)，最大不超过 maxDelay
func (p *ExponentialBackoffPolicy) NextBackoff(attempt int, lastErr error) (time.Duration, bool) {
	if attempt < 1 {
		return 0, false
	}
	if attempt > p.MaxAttemptsValue {
		return 0, false
	}

	delay := float64(p.BaseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(p.MaxDelay) {
		delay = float64(p.MaxDelay)
	}

	if p.Jitter {
		jitter := (rand.Float64() - 0.5) * 0.4 * delay
		delay += jitter
		if delay < 0 {
			delay = float64(p.BaseDelay)
		}
	}
	return time.Duration(delay), true
}

// Log 记录审计日志
func (l *MemoryAuditLogger) Log(ctx context.Context, entry AuditEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) >= l.maxSize {
		newEntries := make([]AuditEntry, len(l.entries)-1, l.maxSize)
		copy(newEntries, l.entries[1:])
		l.entries = newEntries
	}
	l.entries = append(l.entries, entry)
}

// Entries 返回所有审计日志副本
func (l *MemoryAuditLogger) Entries() []AuditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]AuditEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Count 返回审计日志条数
func (l *MemoryAuditLogger) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// Reset 清空审计日志
func (l *MemoryAuditLogger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = l.entries[:0]
}

// Record 记录一次工具调用的成本
func (t *MemoryCostTracker) Record(ctx context.Context, toolName string, success bool, duration time.Duration) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	rec, ok := t.records[toolName]
	if !ok {
		rec = &costRecord{}
		t.records[toolName] = rec
	}
	atomic.AddInt64(&rec.TotalCalls, 1)
	if success {
		atomic.AddInt64(&rec.SuccessCalls, 1)
	} else {
		atomic.AddInt64(&rec.FailedCalls, 1)
	}
	atomic.AddInt64(&rec.TotalDurationMs, duration.Milliseconds())
	return nil
}

// Stats 返回所有工具的计费统计
func (t *MemoryCostTracker) Stats() []CostStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]CostStats, 0, len(t.records))
	for name, rec := range t.records {
		stats := CostStats{
			ToolName:        name,
			TotalCalls:      atomic.LoadInt64(&rec.TotalCalls),
			SuccessCalls:    atomic.LoadInt64(&rec.SuccessCalls),
			FailedCalls:     atomic.LoadInt64(&rec.FailedCalls),
			TotalDurationMs: atomic.LoadInt64(&rec.TotalDurationMs),
		}
		if stats.TotalCalls > 0 {
			stats.SuccessRate = float64(stats.SuccessCalls) / float64(stats.TotalCalls)
			stats.AvgDurationMs = float64(stats.TotalDurationMs) / float64(stats.TotalCalls)
		}
		out = append(out, stats)
	}
	return out
}

// Reset 清空计费统计
func (t *MemoryCostTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = make(map[string]*costRecord)
}

// RetriableError 判断函数类型
type RetriableError func(error) bool

// WithRetriable 为 ExponentialBackoffPolicy 增加自定义可重试错误判断
// 返回一个新的 policy，当 err 不是 retryable 时不再重试
func (p *ExponentialBackoffPolicy) WithRetriable(fn RetriableError) RetryPolicy {
	return &retriablePolicy{inner: p, retriable: fn}
}

type retriablePolicy struct {
	inner     RetryPolicy
	retriable RetriableError
}

func (r *retriablePolicy) MaxAttempts() int {
	return r.inner.MaxAttempts()
}

func (r *retriablePolicy) NextBackoff(attempt int, lastErr error) (time.Duration, bool) {
	if r.retriable != nil && lastErr != nil && !r.retriable(lastErr) {
		return 0, false
	}
	return r.inner.NextBackoff(attempt, lastErr)
}

// ToolRetryPolicies 按工具名配置不同重试策略
type ToolRetryPolicies struct {
	defaultPolicy RetryPolicy
	policies      map[string]RetryPolicy
}

// NewToolRetryPolicies 创建按工具名配置的重试策略集合
func NewToolRetryPolicies(defaultPolicy RetryPolicy) *ToolRetryPolicies {
	if defaultPolicy == nil {
		defaultPolicy = NewExponentialBackoffPolicy(3, 200*time.Millisecond, 5*time.Second)
	}
	return &ToolRetryPolicies{
		defaultPolicy: defaultPolicy,
		policies:      make(map[string]RetryPolicy),
	}
}

// Set 为指定工具设置专用重试策略
func (t *ToolRetryPolicies) Set(toolName string, policy RetryPolicy) {
	t.policies[toolName] = policy
}

// Get 获取指定工具的重试策略，未配置时返回默认策略
func (t *ToolRetryPolicies) Get(toolName string) RetryPolicy {
	if p, ok := t.policies[toolName]; ok {
		return p
	}
	return t.defaultPolicy
}
