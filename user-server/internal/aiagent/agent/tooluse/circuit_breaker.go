package tooluse

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrCircuitOpen = fmt.Errorf("circuit breaker open")
)

// CircuitState 熔断器状态
type CircuitState int32

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// String 状态字符串表示
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half_open"
	}
	return "unknown"
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	FailureThreshold    int
	BaseCooldown        time.Duration
	MaxCooldown         time.Duration
	BackoffMultiplier   float64
	HalfOpenMaxAttempts int
}

// DefaultCircuitBreakerConfig 默认熔断器配置（含指数退避）
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:    5,
		BaseCooldown:        30 * time.Second,
		MaxCooldown:         5 * time.Minute,
		BackoffMultiplier:   2.0,
		HalfOpenMaxAttempts: 1,
	}
}

type toolCircuit struct {
	state            atomic.Int32
	consecutiveFails atomic.Int32
	openedAt         atomic.Int64
	halfOpenAttempts atomic.Int32
	openCount        atomic.Int32
}

func newToolCircuit() *toolCircuit {
	c := &toolCircuit{}
	c.state.Store(int32(CircuitClosed))
	return c
}

func calculateCooldown(cfg CircuitBreakerConfig, openCount int32) time.Duration {
	if openCount <= 1 {
		return cfg.BaseCooldown
	}
	cooldown := float64(cfg.BaseCooldown) * math.Pow(cfg.BackoffMultiplier, float64(openCount-1))
	if cooldown > float64(cfg.MaxCooldown) {
		cooldown = float64(cfg.MaxCooldown)
	}
	return time.Duration(cooldown)
}

func (c *toolCircuit) Allow(now time.Time, cfg CircuitBreakerConfig) bool {
	state := CircuitState(c.state.Load())

	switch state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		openedAt := time.Unix(0, c.openedAt.Load())
		openCount := c.openCount.Load()
		cooldown := calculateCooldown(cfg, openCount)
		if now.Sub(openedAt) >= cooldown {
			if c.state.CompareAndSwap(int32(CircuitOpen), int32(CircuitHalfOpen)) {
				c.halfOpenAttempts.Store(0)
			}
			state = CircuitState(c.state.Load())
			if state == CircuitOpen {
				return false
			}
		} else {
			return false
		}
		fallthrough

	case CircuitHalfOpen:
		if c.halfOpenAttempts.Add(1) > int32(cfg.HalfOpenMaxAttempts) {
			return false
		}
		return true
	}

	return true
}

func (c *toolCircuit) RecordSuccess() {
	c.consecutiveFails.Store(0)
	state := CircuitState(c.state.Load())
	if state == CircuitHalfOpen {
		c.state.Store(int32(CircuitClosed))
		c.halfOpenAttempts.Store(0)
		c.openCount.Store(0)
	}
}

func (c *toolCircuit) RecordFailure(now time.Time, cfg CircuitBreakerConfig) {
	state := CircuitState(c.state.Load())

	if state == CircuitHalfOpen {
		c.state.Store(int32(CircuitOpen))
		c.openedAt.Store(now.UnixNano())
		c.openCount.Add(1)
		return
	}

	fails := c.consecutiveFails.Add(1)
	if int(fails) >= cfg.FailureThreshold {
		if c.state.CompareAndSwap(int32(CircuitClosed), int32(CircuitOpen)) {
			c.openedAt.Store(now.UnixNano())
			c.openCount.Add(1)
		}
	}
}

func (c *toolCircuit) State() CircuitState {
	return CircuitState(c.state.Load())
}

func (c *toolCircuit) ConsecutiveFails() int32 {
	return c.consecutiveFails.Load()
}

func (c *toolCircuit) OpenCount() int32 {
	return c.openCount.Load()
}

// CircuitBreakerRegistry 熔断器注册中心
type CircuitBreakerRegistry struct {
	mu       sync.RWMutex
	circuits map[string]*toolCircuit
	cfg      CircuitBreakerConfig
}

// NewCircuitBreakerRegistry 创建熔断器注册中心
func NewCircuitBreakerRegistry(cfg CircuitBreakerConfig) *CircuitBreakerRegistry {
	if cfg.FailureThreshold <= 0 {
		cfg = DefaultCircuitBreakerConfig()
	}
	if cfg.BackoffMultiplier < 1.0 {
		cfg.BackoffMultiplier = 2.0
	}
	if cfg.MaxCooldown <= 0 {
		cfg.MaxCooldown = 5 * time.Minute
	}
	return &CircuitBreakerRegistry{
		circuits: make(map[string]*toolCircuit),
		cfg:      cfg,
	}
}

// GetCircuit 获取（或创建）指定工具的熔断器
func (r *CircuitBreakerRegistry) GetCircuit(toolName string) *toolCircuit {
	r.mu.RLock()
	c, ok := r.circuits[toolName]
	r.mu.RUnlock()
	if ok {
		return c
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.circuits[toolName]; ok {
		return c
	}
	c = newToolCircuit()
	r.circuits[toolName] = c
	return c
}

// Allow 判断指定工具是否允许请求通过
func (r *CircuitBreakerRegistry) Allow(toolName string) bool {
	c := r.GetCircuit(toolName)
	return c.Allow(time.Now(), r.cfg)
}

// RecordSuccess 记录指定工具调用成功
func (r *CircuitBreakerRegistry) RecordSuccess(toolName string) {
	c := r.GetCircuit(toolName)
	c.RecordSuccess()
}

// RecordFailure 记录指定工具调用失败
func (r *CircuitBreakerRegistry) RecordFailure(toolName string) {
	c := r.GetCircuit(toolName)
	c.RecordFailure(time.Now(), r.cfg)
}

// State 查询指定工具的熔断状态
func (r *CircuitBreakerRegistry) State(toolName string) CircuitState {
	c := r.GetCircuit(toolName)
	return c.State()
}

// ResetTool 清空指定工具的熔断状态（运维出口：下游已恢复，立刻放行）。
//
// 采用"删除条目"而非"改写为 closed"：与 ToolRouter.ResetCircuit 同一口径，
// 让 openCount（决定指数退避长度）一并归零，运维点"重置"得到的是干净起点，
// 而不是"看起来重置了、下次熔断仍要冷却 5 分钟"。
func (r *CircuitBreakerRegistry) ResetTool(toolName string) {
	r.mu.Lock()
	delete(r.circuits, toolName)
	r.mu.Unlock()
}

// ToolCircuitInfo 工具熔断信息
type ToolCircuitInfo struct {
	State            CircuitState `json:"state"`
	ConsecutiveFails int32        `json:"consecutive_fails"`
	OpenCount        int32        `json:"open_count"`
}

// AllStates 查询所有工具的熔断状态
func (r *CircuitBreakerRegistry) AllStates() map[string]ToolCircuitInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]ToolCircuitInfo, len(r.circuits))
	for name, c := range r.circuits {
		out[name] = ToolCircuitInfo{
			State:            c.State(),
			ConsecutiveFails: c.ConsecutiveFails(),
			OpenCount:        c.OpenCount(),
		}
	}
	return out
}

// Config 返回当前配置
func (r *CircuitBreakerRegistry) Config() CircuitBreakerConfig {
	return r.cfg
}

// CircuitBreakerDecorator 熔断器装饰器（生效态：判定拒绝即拦下请求）
func CircuitBreakerDecorator(registry *CircuitBreakerRegistry) ToolDecorator {
	return circuitBreakerDecorator(registry, false, nil)
}

// CircuitBreakerDecoratorWithObserver 生效态装饰器 + 判定回调。
func CircuitBreakerDecoratorWithObserver(registry *CircuitBreakerRegistry, onDecision CircuitDecisionFunc) ToolDecorator {
	return circuitBreakerDecorator(registry, false, onDecision)
}

// CircuitBreakerShadowDecorator 观察态装饰器：照常记账、照常判定，但**绝不拦下请求**。
//
// 与生效态的唯一差别是 `!allowed` 那一支不外抛 ErrCircuitOpen、请求照常进 next。
// 因此 shadow 记到的成败是下游的真实结局，而不是"被拦下所以没机会测"——
// 这正是灰度前想要的对比数据（多少调用本会被拦）。
//
// 判定复用同一个 `registry.Allow()` 与同一套状态机，不是另算一套近似：
// 打开后仍要走半开探测才会闭合（一次放行成功**不会**把熔断偷偷关掉），
// 冷却时长同样按 openCount 指数退避。唯一差别是熔断已 open 期间 shadow 还会继续
// 累加 consecutiveFails——该值在 open 态不参与任何判定，闭合时又被清零，故不改变状态轨迹。
func CircuitBreakerShadowDecorator(registry *CircuitBreakerRegistry, onDecision CircuitDecisionFunc) ToolDecorator {
	return circuitBreakerDecorator(registry, true, onDecision)
}

func circuitBreakerDecorator(registry *CircuitBreakerRegistry, shadow bool, onDecision CircuitDecisionFunc) ToolDecorator {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, args map[string]any) (ToolResult, error) {
			if registry == nil {
				return next(ctx, args)
			}
			toolName := GetToolName(ctx)

			allowed := registry.Allow(toolName)
			if onDecision != nil {
				onDecision(ctx, registry.Decision(toolName, shadow, !allowed))
			}

			if !allowed {
				if !shadow {
					return ErrorResult(toolName, fmt.Errorf("%w: tool=%s state=%s",
						ErrCircuitOpen, toolName, registry.State(toolName))), ErrCircuitOpen
				}
			}

			result, err := next(ctx, args)

			if err != nil || !result.Success {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return result, err
				}
				registry.RecordFailure(toolName)
			} else {
				registry.RecordSuccess(toolName)
			}

			return result, err
		}
	}
}

// CircuitDecision 一次熔断判定的可观测快照。
//
// WouldBlock 在两种模式下含义一致：本次判定是否拒绝。差别只在 Shadow——
// Shadow=true 时请求仍被放行，该值只是"若生效则会被拦"。
type CircuitDecision struct {
	ToolName         string        `json:"tool_name"`
	Shadow           bool          `json:"shadow"`
	WouldBlock       bool          `json:"would_block"`
	State            CircuitState  `json:"state"`
	ConsecutiveFails int32         `json:"consecutive_fails"`
	OpenCount        int32         `json:"open_count"`
	FailureThreshold int           `json:"failure_threshold"`
	BaseCooldown     time.Duration `json:"base_cooldown"`
}

// CircuitDecisionFunc 熔断判定回调；nil 表示不上报。
type CircuitDecisionFunc func(ctx context.Context, d CircuitDecision)

// Decision 读取指定工具当前的熔断快照，组装一次判定记录。
func (r *CircuitBreakerRegistry) Decision(toolName string, shadow, wouldBlock bool) CircuitDecision {
	c := r.GetCircuit(toolName)
	cfg := r.Config()
	return CircuitDecision{
		ToolName:         toolName,
		Shadow:           shadow,
		WouldBlock:       wouldBlock,
		State:            c.State(),
		ConsecutiveFails: c.ConsecutiveFails(),
		OpenCount:        c.OpenCount(),
		FailureThreshold: cfg.FailureThreshold,
		BaseCooldown:     cfg.BaseCooldown,
	}
}

// circuitToolStat 单工具判定累计
type circuitToolStat struct {
	total      int64
	wouldBlock int64
	lastState  CircuitState
	lastFails  int32
}

// CircuitDecisionCounter 熔断判定累计器。
//
// 存在的理由：shadow 模式的价值全在"和生效后的行为对比"，若判定只打在日志里，
// 想回答"这一周会被拦多少次"就得去 grep 日志。T-P1-06 转阻断前必须给出这份对比报告，
// 所以在观察态就把计数留在进程内，由调试 API 直接读。
//
// 键空间有界：toolName 来自 ToolRegistry 已注册工具（Executor 先查表再走装饰链），
// 未注册名根本到不了这里。并发安全。
type CircuitDecisionCounter struct {
	mu      sync.Mutex
	perTool map[string]*circuitToolStat
	total   int64
	blocked int64
}

// NewCircuitDecisionCounter 创建判定累计器
func NewCircuitDecisionCounter() *CircuitDecisionCounter {
	return &CircuitDecisionCounter{perTool: make(map[string]*circuitToolStat)}
}

// Observe 实现 CircuitDecisionFunc，可直接或间作接线。
func (c *CircuitDecisionCounter) Observe(_ context.Context, d CircuitDecision) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total++
	if d.WouldBlock {
		c.blocked++
	}
	st, ok := c.perTool[d.ToolName]
	if !ok {
		st = &circuitToolStat{}
		c.perTool[d.ToolName] = st
	}
	st.total++
	if d.WouldBlock {
		st.wouldBlock++
	}
	st.lastState = d.State
	st.lastFails = d.ConsecutiveFails
}

// CircuitDecisionReport 判定累计快照（供调试 API / shadow 期对比报告）
type CircuitDecisionReport struct {
	Total             int64             `json:"total"`
	WouldBlock        int64             `json:"would_block"`
	WouldBlockRatePct float64           `json:"would_block_rate_pct"`
	PerTool           []CircuitToolStat `json:"per_tool"`
}

// CircuitToolStat 单工具判定累计
type CircuitToolStat struct {
	ToolName   string       `json:"tool_name"`
	Total      int64        `json:"total"`
	WouldBlock int64        `json:"would_block"`
	LastState  CircuitState `json:"last_state"`
	LastFails  int32        `json:"last_consecutive_fails"`
}

// Report 返回快照（按 would_block 降序、同数按名字典序，便于报告直接阅读）
func (c *CircuitDecisionCounter) Report() CircuitDecisionReport {
	if c == nil {
		return CircuitDecisionReport{}
	}
	c.mu.Lock()
	out := CircuitDecisionReport{Total: c.total, WouldBlock: c.blocked}
	if out.Total > 0 {
		out.WouldBlockRatePct = float64(out.WouldBlock) / float64(out.Total) * 100
	}
	per := make([]CircuitToolStat, 0, len(c.perTool))
	for name, st := range c.perTool {
		per = append(per, CircuitToolStat{
			ToolName: name, Total: st.total, WouldBlock: st.wouldBlock,
			LastState: st.lastState, LastFails: st.lastFails,
		})
	}
	c.mu.Unlock()

	sort.Slice(per, func(i, j int) bool {
		if per[i].WouldBlock != per[j].WouldBlock {
			return per[i].WouldBlock > per[j].WouldBlock
		}
		return per[i].ToolName < per[j].ToolName
	})
	out.PerTool = per
	return out
}

// Reset 清零（测试与灰度复盘用）
func (c *CircuitDecisionCounter) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.perTool = make(map[string]*circuitToolStat)
	c.total = 0
	c.blocked = 0
	c.mu.Unlock()
}

// NoOpCircuitBreakerRegistry 空操作熔断器
type NoOpCircuitBreakerRegistry struct{}

func (NoOpCircuitBreakerRegistry) Allow(toolName string) bool            { return true }
func (NoOpCircuitBreakerRegistry) RecordSuccess(toolName string)         {}
func (NoOpCircuitBreakerRegistry) RecordFailure(toolName string)         {}
func (NoOpCircuitBreakerRegistry) State(toolName string) CircuitState    { return CircuitClosed }
func (NoOpCircuitBreakerRegistry) AllStates() map[string]ToolCircuitInfo { return nil }
func (NoOpCircuitBreakerRegistry) Config() CircuitBreakerConfig          { return DefaultCircuitBreakerConfig() }
