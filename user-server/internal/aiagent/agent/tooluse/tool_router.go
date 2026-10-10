package tooluse

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ToolRouter 工具路由中心
type ToolRouter struct {
	executor *ToolExecutor

	rateLimiter RateLimiter
	keyBuilder  func(toolName string, tc *ToolContext) string

	failThreshold    int
	cooldownDuration time.Duration

	toolCosts map[string]float64

	circuit map[string]*circuitState

	mu    sync.RWMutex
	stats RouterStats
}

// RouterStats 路由统计
type RouterStats struct {
	TotalCalls       int64
	SuccessCalls     int64
	FailedCalls      int64
	RateLimitedCalls int64
	CircuitOpenCalls int64
	TotalCost        float64
	DefaultToolCost  float64
}

type circuitState struct {
	mu          sync.Mutex
	failCount   int
	openUntil   time.Time
	lastFailure time.Time
}

// RouterConfig 路由配置
type RouterConfig struct {
	FailThreshold    int
	CooldownDuration time.Duration
	DefaultToolCost  float64
}

// NewToolRouter 创建路由中心
func NewToolRouter(executor *ToolExecutor, rateLimiter RateLimiter, cfg RouterConfig) *ToolRouter {
	if rateLimiter == nil {
		rateLimiter = &NoOpRateLimiter{}
	}
	if cfg.FailThreshold <= 0 {
		cfg.FailThreshold = RouterFailThreshold()
	}
	if cfg.CooldownDuration <= 0 {
		cfg.CooldownDuration = RouterCooldownDuration()
	}
	if cfg.DefaultToolCost < 0 {
		cfg.DefaultToolCost = 0.001
	}
	return &ToolRouter{
		executor:         executor,
		rateLimiter:      rateLimiter,
		keyBuilder:       defaultKeyBuilder,
		failThreshold:    cfg.FailThreshold,
		cooldownDuration: cfg.CooldownDuration,
		toolCosts:        make(map[string]float64),
		circuit:          make(map[string]*circuitState),
		stats:            RouterStats{DefaultToolCost: cfg.DefaultToolCost},
	}
}

// SetToolCost 设置工具成本
func (r *ToolRouter) SetToolCost(toolName string, cost float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolCosts[toolName] = cost
}

// RouteResult 路由结果
type RouteResult struct {
	Result      ToolResult
	Err         error
	RateLimit   bool
	CircuitOpen bool
	Cost        float64
}

// Route 路由并执行
//
// 流程：
//  1. 检查熔断
//  2. 检查限流
//  3. 执行工具
//  4. 记录成本 + 更新熔断计数
//  5. 返回结果
func (r *ToolRouter) Route(ctx context.Context, toolName string, args map[string]any, tc *ToolContext) RouteResult {
	if r.executor == nil {
		return RouteResult{Err: errors.New("executor not set")}
	}

	r.mu.Lock()
	r.stats.TotalCalls++
	r.mu.Unlock()

	if r.isCircuitOpen(toolName) {
		r.mu.Lock()
		r.stats.CircuitOpenCalls++
		r.mu.Unlock()
		return RouteResult{
			Err:         errors.New("circuit open for " + toolName),
			CircuitOpen: true,
		}
	}

	key := r.keyBuilder(toolName, tc)
	if err := r.rateLimiter.Acquire(ctx, key); err != nil {
		r.mu.Lock()
		r.stats.RateLimitedCalls++
		r.mu.Unlock()
		return RouteResult{
			Err:       err,
			RateLimit: true,
		}
	}

	execCtx := ctx
	if tc != nil {
		execCtx = WithToolContext(ctx, tc)
	}
	result, err := r.executor.ExecuteByName(execCtx, toolName, args)

	if err != nil || !result.Success {
		r.mu.Lock()
		r.stats.FailedCalls++
		r.mu.Unlock()
		r.recordFailure(toolName)
	} else {
		r.mu.Lock()
		r.stats.SuccessCalls++
		r.mu.Unlock()
		r.recordSuccess(toolName)
	}

	r.mu.RLock()
	cost := r.toolCosts[toolName]
	if cost == 0 {
		cost = r.stats.DefaultToolCost
		if cost == 0 {
			cost = r.muLoadDefaultCost()
		}
	}
	r.mu.RUnlock()

	r.mu.Lock()
	r.stats.TotalCost += cost
	r.mu.Unlock()

	return RouteResult{
		Result: result,
		Err:    err,
		Cost:   cost,
	}
}

// GetStats 获取统计
func (r *ToolRouter) GetStats() RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stats
}

// ResetStats 重置统计
func (r *ToolRouter) ResetStats() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats = RouterStats{}
}

// ResetCircuit 重置指定工具的熔断
func (r *ToolRouter) ResetCircuit(toolName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.circuit, toolName)
}

func (r *ToolRouter) isCircuitOpen(toolName string) bool {
	r.mu.RLock()
	c, ok := r.circuit[toolName]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.openUntil) {
		return true
	}
	if !c.openUntil.IsZero() {
		c.failCount = 0
		c.openUntil = time.Time{}
	}
	return false
}

func (r *ToolRouter) recordFailure(toolName string) {
	r.mu.Lock()
	c, ok := r.circuit[toolName]
	if !ok {
		c = &circuitState{}
		r.circuit[toolName] = c
	}
	threshold := r.failThreshold
	cooldown := r.cooldownDuration
	r.mu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.failCount++
	c.lastFailure = time.Now()
	if threshold > 0 && c.failCount >= threshold {
		c.openUntil = time.Now().Add(cooldown)
	}
}

func (r *ToolRouter) recordSuccess(toolName string) {
	r.mu.RLock()
	c, ok := r.circuit[toolName]
	r.mu.RUnlock()
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failCount = 0
	c.openUntil = time.Time{}
}

func (r *ToolRouter) muLoadDefaultCost() float64 {
	return 0.001
}

func defaultKeyBuilder(toolName string, tc *ToolContext) string {
	if tc != nil && tc.AgentID != "" {
		return toolName + ":" + tc.AgentID
	}
	return toolName
}

// ErrRouterUnavailable 路由不可用
var ErrRouterUnavailable = errors.New("tool router unavailable")

// DefaultRouterFailThreshold ToolRouter 内置熔断（失败即切同类工具）的连续失败
// 次数兜底值。
const DefaultRouterFailThreshold = 5

// DefaultRouterCooldownDuration 上面那个熔断的冷却期兜底值。
const DefaultRouterCooldownDuration = 30 * time.Second

var (
	routerFailThresholdProvider    func() int
	routerCooldownDurationProvider func() time.Duration
)

// SetRouterFailThresholdProvider 注入熔断阈值；传 nil 视为不注入。
func SetRouterFailThresholdProvider(fn func() int) { routerFailThresholdProvider = fn }

// SetRouterCooldownDurationProvider 注入熔断冷却期；传 nil 视为不注入。
func SetRouterCooldownDurationProvider(fn func() time.Duration) { routerCooldownDurationProvider = fn }

// RouterFailThreshold 生效的熔断阈值。非正值一律回落兜底——阈值 0 会让任意一次
// 失败都触发换工具，一个抖动就把整条工具链掀掉。
func RouterFailThreshold() int {
	if p := routerFailThresholdProvider; p != nil {
		if n := p(); n > 0 {
			return n
		}
	}
	return DefaultRouterFailThreshold
}

// RouterCooldownDuration 生效的熔断冷却期。非正值一律回落兜底——冷却 0 等于没有
// 冷却，失败的工具会被立刻重新选中，退化成不停重试。
func RouterCooldownDuration() time.Duration {
	if p := routerCooldownDurationProvider; p != nil {
		if d := p(); d > 0 {
			return d
		}
	}
	return DefaultRouterCooldownDuration
}
