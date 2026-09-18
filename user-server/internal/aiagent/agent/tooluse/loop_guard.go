package tooluse

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrLoopDetected 工具调用循环被检测到
var ErrLoopDetected = fmt.Errorf("tool loop detected: same tool called too many times with equivalent args")

// StopReason Agent Loop 结构化停止原因（A-3）。
//
// 统一枚举供 Guard 停止判定与 trace span status 使用；
// 与 MASTER_COMPETITIVE_DECISIONS.md 接口契约速查保持一致。
type StopReason string

const (
	StopReasonNone           StopReason = ""
	StopReasonLoopLimit      StopReason = "loop_limit"
	StopReasonTimeLimit      StopReason = "time_limit"
	StopReasonTokenLimit     StopReason = "token_limit"
	StopReasonCostLimit      StopReason = "cost_limit"
	StopReasonApprovalDenied StopReason = "approval_denied"
	StopReasonCompleted      StopReason = "completed"
	StopReasonError          StopReason = "error"
)

// StopReasonOf 将错误分类为结构化 StopReason（A-3：统一出口，供调用方写 trace span status）。
func StopReasonOf(err error) StopReason {
	switch {
	case err == nil:
		return StopReasonCompleted
	case errors.Is(err, ErrLoopDetected):
		return StopReasonLoopLimit
	case errors.Is(err, ErrApprovalDenied):
		return StopReasonApprovalDenied
	case errors.Is(err, context.DeadlineExceeded):
		return StopReasonTimeLimit
	default:
		return StopReasonError
	}
}

// CostDriftFactor 成本漂移熔断倍率（A-2 简化版）：单轮成本 > 已有轮次均值 × 该倍数即熔断。
const CostDriftFactor = 5.0

// LoopGuardConfig 循环检测配置
type LoopGuardConfig struct {
	MaxRepeatCount int
	WindowSize     time.Duration
	MaxTraces      int
	Enabled        bool
	// CostBudgetUSD 单 trace 累计美元成本预算（A-2）；<=0 表示禁用成本护栏
	CostBudgetUSD float64
}

// DefaultLoopGuardConfig 默认循环检测配置
func DefaultLoopGuardConfig() LoopGuardConfig {
	return LoopGuardConfig{
		MaxRepeatCount: 3,
		WindowSize:     60 * time.Second,
		MaxTraces:      10000,
		Enabled:        true,
	}
}

// LoopGuard 工具调用循环检测器
//
// 线程安全；按 trace_id 维度跟踪调用历史
type LoopGuard struct {
	mu     sync.Mutex
	traces map[string]*traceHistory
	config LoopGuardConfig
}

type traceHistory struct {
	calls    []callRecord
	lastSeen time.Time

	costTotal  float64
	roundCosts []float64
	lastStop   StopReason
}

type callRecord struct {
	toolName  string
	argsHash  string
	timestamp time.Time
}

// NewLoopGuard 创建循环检测器
func NewLoopGuard(config LoopGuardConfig) *LoopGuard {
	if config.MaxRepeatCount <= 0 {
		config.MaxRepeatCount = 3
	}
	if config.WindowSize <= 0 {
		config.WindowSize = 60 * time.Second
	}
	if config.MaxTraces <= 0 {
		config.MaxTraces = 10000
	}
	return &LoopGuard{
		traces: make(map[string]*traceHistory),
		config: config,
	}
}

// CheckAndRecord 检查是否允许调用，并记录本次调用
//
// 返回值：
//   - nil: 允许调用（已记录到历史）
//   - ErrLoopDetected: 检测到循环，拒绝调用（不记录）
func (g *LoopGuard) CheckAndRecord(traceID, toolName string, args map[string]any) error {
	if g == nil || !g.config.Enabled {
		return nil
	}

	argsHash := structuralFingerprint(args)
	key := traceID
	if key == "" {
		key = "_no_trace_:" + toolName
	}

	now := time.Now()
	windowStart := now.Add(-g.config.WindowSize)

	g.mu.Lock()
	defer g.mu.Unlock()

	history, ok := g.traces[key]
	if !ok {
		history = &traceHistory{calls: make([]callRecord, 0, 8)}
		g.traces[key] = history

		if len(g.traces) > g.config.MaxTraces {
			g.evictOldestLocked()
		}
	}
	history.lastSeen = now

	dedupeIdx := 0
	for i, c := range history.calls {
		if c.timestamp.After(windowStart) {
			history.calls[dedupeIdx] = history.calls[i]
			dedupeIdx++
		}
	}
	history.calls = history.calls[:dedupeIdx]

	repeatCount := 0
	for _, c := range history.calls {
		if c.toolName == toolName && c.argsHash == argsHash {
			repeatCount++
		}
	}

	if repeatCount >= g.config.MaxRepeatCount {
		history.lastStop = StopReasonLoopLimit
		return ErrLoopDetected
	}

	history.calls = append(history.calls, callRecord{
		toolName:  toolName,
		argsHash:  argsHash,
		timestamp: now,
	})
	return nil
}

// RecordCost 记录一轮 LLM 调用的美元成本（A-2：调用方每轮传入，由 LoopGuard 累计）。
//
// 返回触发的 StopReason（StopReasonNone 表示未触发可继续）：
//   - 累计成本 >= CostBudgetUSD → StopReasonCostLimit（超预算熔断）
//   - 单轮成本 > 已有轮次均值 × CostDriftFactor → StopReasonCostLimit（漂移熔断，
//     在总预算耗尽前拦截上下文膨胀型故障；无历史轮次或历史均值为 0 时不触发）
//
// 成本护栏独立于 Enabled 开关之外仍受其约束：guard 为 nil 或未启用时不追踪。
func (g *LoopGuard) RecordCost(traceID string, costUSD float64) StopReason {
	if g == nil || !g.config.Enabled || costUSD <= 0 {
		return StopReasonNone
	}
	key := traceID
	if key == "" {
		key = "_no_trace_"
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	history, ok := g.traces[key]
	if !ok {
		history = &traceHistory{calls: make([]callRecord, 0, 8)}
		g.traces[key] = history
		if len(g.traces) > g.config.MaxTraces {
			g.evictOldestLocked()
		}
	}
	history.lastSeen = time.Now()

	n := len(history.roundCosts)
	if n > 0 {
		var sum float64
		for _, c := range history.roundCosts {
			sum += c
		}
		if avg := sum / float64(n); avg > 0 && costUSD > avg*CostDriftFactor {
			history.lastStop = StopReasonCostLimit
			return StopReasonCostLimit
		}
	}

	history.roundCosts = append(history.roundCosts, costUSD)
	history.costTotal += costUSD

	if g.config.CostBudgetUSD > 0 && history.costTotal >= g.config.CostBudgetUSD {
		history.lastStop = StopReasonCostLimit
		return StopReasonCostLimit
	}
	return StopReasonNone
}

// TraceStopReason 返回该 trace 最近一次触发的结构化停止原因（A-3 暴露给调用方）。
// 无记录返回 StopReasonNone。
func (g *LoopGuard) TraceStopReason(traceID string) StopReason {
	if g == nil {
		return StopReasonNone
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if h, ok := g.traces[traceID]; ok {
		return h.lastStop
	}
	return StopReasonNone
}

// UsedCost 返回该 trace 累计美元成本（监控/调试用）
func (g *LoopGuard) UsedCost(traceID string) float64 {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if h, ok := g.traces[traceID]; ok {
		return h.costTotal
	}
	return 0
}

// FinishTrace 结束并清理该 trace 的全部状态（循环历史+成本追踪）。
// 由调用方在 Agent Loop 收尾时调用，防止长驻进程内存累积。
func (g *LoopGuard) FinishTrace(traceID string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.traces, traceID)
}

func (g *LoopGuard) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range g.traces {
		if oldestKey == "" || v.lastSeen.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.lastSeen
		}
	}
	if oldestKey != "" {
		delete(g.traces, oldestKey)
	}
}

// Reset 清空所有调用历史
func (g *LoopGuard) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.traces = make(map[string]*traceHistory)
}

// Stats 返回当前追踪统计（用于监控/调试）
type LoopGuardStats struct {
	ActiveTraces int `json:"active_traces"`
	TotalRecords int `json:"total_records"`
}

// Stats 返回统计信息
func (g *LoopGuard) Stats() LoopGuardStats {
	g.mu.Lock()
	defer g.mu.Unlock()
	total := 0
	for _, h := range g.traces {
		total += len(h.calls)
	}
	return LoopGuardStats{
		ActiveTraces: len(g.traces),
		TotalRecords: total,
	}
}

// LoopGuardDecorator 工具级循环检测装饰器
//
// 在工具执行前检查是否陷入循环，若陷入则返回 ErrLoopDetected。
// guard 为 nil 时直接放行（零开销）。
//
// 装饰器链位置：重试之外（避免重试触发的多次调用被误判为循环）
//   - 位于重试之外：重试是同一逻辑调用的多次尝试，不应触发循环检测
//   - 位于反馈回流之内：循环命中时反馈仍能记录失败事件
//   - 位于死信之内：ErrLoopDetected 是不可重试错误，死信队列自动跳过
func LoopGuardDecorator(guard *LoopGuard) ToolDecorator {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, args map[string]any) (ToolResult, error) {
			if guard == nil {
				return next(ctx, args)
			}

			toolName := GetToolName(ctx)
			traceID := GetTraceID(ctx)

			if err := guard.CheckAndRecord(traceID, toolName, args); err != nil {
				return ErrorResult(toolName, err), err
			}

			return next(ctx, args)
		}
	}
}
