package agent_runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// EpisodicMemoryProvider 跨会话情境记忆提供者（E1 补全）
// 由外部注入，InferenceCycle 在 RunOnce 开头调用 LoadEpisodicMemory 读取
// 跨会话上下文（L1/L2/L3/L4 汇总），填入 InferenceContext.EpisodicMemory，
// 供 PlannerStage 在 ReplyHint 中引用，使情境记忆影响决策。
// 典型实现：包装 service.MemorySystem.BuildFullContext / Recall。
type EpisodicMemoryProvider interface {
	LoadEpisodicMemory(ctx context.Context, sessionID, customerID string) (string, error)
}

// InferenceCycle 推理闭环
type InferenceCycle struct {
	PerceptionStage InferenceStage
	AlignmentStage  InferenceStage
	GatekeeperStage InferenceStage
	PlannerStage    InferenceStage
	ReviewerStage   InferenceStage

	StageTimeout time.Duration

	TotalTimeout time.Duration

	memoryProvider EpisodicMemoryProvider
	ckptStore      StageCheckpointStore

	mu        sync.RWMutex
	stopped   bool
	lastStats CycleStats
}

// SetMemoryProvider 注入跨会话情境记忆提供者（E1 补全）
// 非并发安全，应在初始化阶段调用
func (c *InferenceCycle) SetMemoryProvider(p EpisodicMemoryProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memoryProvider = p
}

// SetCheckpointStore 注入阶段边界检查点存储（T-P1-01 断点续跑挂载）。
//
// 调用方：internal/app.InitAgentCheckpointStore → InitInferenceOrchestrator。
// 传 nil 或未注册 FF_LTC_CHECKPOINT 时推理链路与挂载前完全一致。
func (c *InferenceCycle) SetCheckpointStore(s StageCheckpointStore) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ckptStore = s
}

// CycleStats 闭环统计
type CycleStats struct {
	TotalRuns      int64
	SuccessRuns    int64
	EscalationRuns int64
	FailureRuns    int64
	RejectedRuns   int64
	DegradedRuns   int64
	AvgDurationMs  int64
}

// InferenceCycleConfig 配置
type InferenceCycleConfig struct {
	StageTimeout time.Duration
	TotalTimeout time.Duration
}

// NewInferenceCycle 默认推理闭环
func NewInferenceCycle() *InferenceCycle {
	return &InferenceCycle{
		PerceptionStage: NewDefaultPerceptionStage(),
		AlignmentStage:  NewDefaultAlignmentScorer(),
		GatekeeperStage: NewDefaultCrisisDetector(),
		PlannerStage:    NewDefaultTaskPlanner(),
		ReviewerStage:   NewDefaultReviewer(),
		StageTimeout:    2 * time.Second,
		TotalTimeout:    8 * time.Second,
	}
}

// NewInferenceCycleWithStages 自定义阶段
func NewInferenceCycleWithStages(
	perception, alignment, gatekeeper, planner InferenceStage,
) *InferenceCycle {
	return &InferenceCycle{
		PerceptionStage: perception,
		AlignmentStage:  alignment,
		GatekeeperStage: gatekeeper,
		PlannerStage:    planner,
		ReviewerStage:   NewDefaultReviewer(),
		StageTimeout:    2 * time.Second,
		TotalTimeout:    8 * time.Second,
	}
}

// NewInferenceCycleWithConfig 完整自定义
func NewInferenceCycleWithConfig(cfg InferenceCycleConfig, perception, alignment, gatekeeper, planner InferenceStage) *InferenceCycle {
	cycle := NewInferenceCycleWithStages(perception, alignment, gatekeeper, planner)
	if cfg.StageTimeout > 0 {
		cycle.StageTimeout = cfg.StageTimeout
	}
	if cfg.TotalTimeout > 0 {
		cycle.TotalTimeout = cfg.TotalTimeout
	}
	return cycle
}

func (c *InferenceCycle) SetReviewerStage(r InferenceStage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ReviewerStage = r
}

// RunOnce 执行一次完整推理闭环
//
// 入参：
//   - payload: 客户消息载荷
//   - agentCtx: 智能体上下文（可为空，将用默认占位）
//
// 返回：
//   - InferenceDecision: 最终决策
//   - error: 阶段技术故障、超时或无效结果；业务拒绝见 Outcome。
//
// 行为：
//  1. 构造 InferenceContext
//  2. 顺序执行五阶段（感知 → 对齐 → 硬闸 → 规划 → 复核）
//  3. 每阶段都设独立超时
//  4. 收集所有阶段决策
//  5. 汇总到 InferenceDecision
func (c *InferenceCycle) RunOnce(ctx context.Context, payload CustomerMessagePayload, agentCtx *AgentContext) (*InferenceDecision, error) {
	return c.runOnce(ctx, payload, agentCtx, nil)
}

func mergeDecision(base, override InferenceDecision) InferenceDecision {
	merged := base
	if override.HandoffToHuman {
		merged.HandoffToHuman = true
	}
	if override.HandoffReason != "" {
		merged.HandoffReason = override.HandoffReason
	}
	if override.Plan != nil {
		merged.Plan = override.Plan
	}
	if override.Reply != "" {
		merged.Reply = override.Reply
	}
	if override.ReplyType != "" {
		merged.ReplyType = override.ReplyType
	}
	if override.Confidence > 0 {
		merged.Confidence = override.Confidence
	}
	if override.StopReason != "" {
		merged.StopReason = override.StopReason
	}
	// merged 无危机信号时才采纳 override 的（原空 if 分支为该语义的残缺写法）
	if merged.Crisis.Level == CrisisNone && merged.Crisis.Reason == "" {
		if override.Crisis.Level != CrisisNone || override.Crisis.Reason != "" {
			merged.Crisis = override.Crisis
		}
	}
	if override.Review != nil {
		merged.Review = override.Review
	}
	return merged
}

func (c *InferenceCycle) recordStats(d InferenceDecision) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastStats.TotalRuns++
	switch d.Outcome {
	case InferenceHandoff:
		c.lastStats.EscalationRuns++
	case InferenceSucceeded:
		c.lastStats.SuccessRuns++
	case InferenceRejected:
		c.lastStats.RejectedRuns++
	case InferenceDegraded:
		c.lastStats.DegradedRuns++
	default:
		c.lastStats.FailureRuns++
	}
	durMs := d.TotalDuration.Milliseconds()
	if c.lastStats.AvgDurationMs == 0 {
		c.lastStats.AvgDurationMs = durMs
	} else {
		c.lastStats.AvgDurationMs = (c.lastStats.AvgDurationMs*9 + durMs) / 10
	}
}

// GetStats 获取统计
func (c *InferenceCycle) GetStats() CycleStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastStats
}

// Stop 停止推理闭环
func (c *InferenceCycle) Stop(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return errors.New("inference cycle already stopped")
	}
	c.stopped = true
	return nil
}

// Reset 重置（用于测试）
func (c *InferenceCycle) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = false
	c.lastStats = CycleStats{}
}

// ErrInferenceTimeout 推理超时
var ErrInferenceTimeout = errors.New("inference cycle timeout")

// ErrInvalidPayload 无效载荷
var ErrInvalidPayload = errors.New("invalid inference payload")

// ValidatePayload 校验载荷
func ValidatePayload(p CustomerMessagePayload) error {
	if p.ChannelType == "" {
		return fmt.Errorf("%w: channel_type required", ErrInvalidPayload)
	}
	if p.CustomerID == "" {
		return fmt.Errorf("%w: customer_id required", ErrInvalidPayload)
	}
	return nil
}
