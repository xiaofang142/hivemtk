package agent_runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
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
//   - error: 仅在编排器本身失败时返回错误
//
// 行为：
//  1. 构造 InferenceContext
//  2. 顺序执行 4 个阶段（感知 → 对齐 → 门禁 → 规划）
//  3. 每阶段都设独立超时
//  4. 收集所有阶段决策
//  5. 汇总到 InferenceDecision
func (c *InferenceCycle) RunOnce(ctx context.Context, payload CustomerMessagePayload, agentCtx *AgentContext) (*InferenceDecision, error) {
	if c.stopped {
		return nil, ErrRuntimeStopped
	}

	start := time.Now()

	if agentCtx == nil {
		agentCtx = &AgentContext{
			AgentID:   0,
			AgentCode: "default",
			Name:      "默认智能体",
			AgentType: "customer_service",
			LoadedAt:  time.Now(),
		}
	}

	tctx, cancel := context.WithTimeout(ctx, c.TotalTimeout)
	defer cancel()

	ic := &InferenceContext{
		Payload:   payload,
		AgentCtx:  agentCtx,
		StartTime: start,
		Stages:    []StageDecision{},
		Decision: InferenceDecision{
			ReplyType:  "text",
			Confidence: 0,
		},
	}

	c.mu.RLock()
	provider := c.memoryProvider
	store := c.ckptStore
	c.mu.RUnlock()
	if provider != nil {
		mem, err := provider.LoadEpisodicMemory(tctx, payload.SessionID, payload.CustomerID)
		if err != nil {
			logger.Warnf("[inference_cycle] load episodic memory failed session=%s customer=%s err=%v",
				payload.SessionID, payload.CustomerID, err)
		} else if mem != "" {
			ic.EpisodicMemory = mem
		}
	}

	// 阶段边界检查点：store 未注入 / 开关关闭时 ckpt 为 nil，下面所有 ckpt.* 调用
	// 全部空转，执行序列与挂载前逐语句一致。
	ckpt := newCycleCheckpoint(tctx, store, payload, ic, c.orderedStageNames())

	var percepResult, alignResult *StageResult

	// 感知 → 对齐必须串行:对齐评分读取感知产出的 Sentiment/Intent,
	// 此前并行执行曾造成数据竞争(race detector 实锤)与评分脏读;
	// 感知为 LLM 调用、对齐为本地打分,串行无性能损失
	if c.PerceptionStage != nil && !ckpt.skipped(c.PerceptionStage.Name()) {
		sctx, scancel := context.WithTimeout(tctx, c.StageTimeout)
		r := c.PerceptionStage.Execute(sctx, ic)
		scancel()
		if r.Error != nil {
			logger.Warnf("[inference_cycle] stage=perception error=%v", r.Error)
		}
		if r.Decision != nil {
			ic.Decision = mergeDecision(ic.Decision, *r.Decision)
		}
		if r.EarlyReturn {
			percepResult = &r
		} else {
			ckpt.complete(tctx, c.PerceptionStage.Name(), ic)
		}
	}

	if c.AlignmentStage != nil && !ckpt.skipped(c.AlignmentStage.Name()) {
		sctx, scancel := context.WithTimeout(tctx, c.StageTimeout)
		r := c.AlignmentStage.Execute(sctx, ic)
		scancel()
		if r.Error != nil {
			logger.Warnf("[inference_cycle] stage=alignment error=%v", r.Error)
		}
		if r.Decision != nil {
			ic.Decision = mergeDecision(ic.Decision, *r.Decision)
		}
		if r.EarlyReturn {
			alignResult = &r
		} else {
			ckpt.complete(tctx, c.AlignmentStage.Name(), ic)
		}
	}

	if percepResult != nil || alignResult != nil {
		earlyStageName := "perception"
		if alignResult != nil {
			earlyStageName = "alignment"
		}
		ic.Decision.TotalDuration = time.Since(start)
		ic.Decision.Crisis = ic.Crisis
		ic.Decision.Sentiment = ic.Sentiment
		ic.Decision.Intent = ic.Intent
		ic.Decision.Alignment = ic.Alignment
		c.recordStats(ic.Decision)
		ckpt.finish(tctx, ic)
		logger.Infof("[inference_cycle] early return at stage=%s handoff=%v reason=%s duration=%s",
			earlyStageName, ic.Decision.HandoffToHuman, ic.Decision.StopReason, ic.Decision.TotalDuration)
		return &ic.Decision, nil
	}

	for _, stage := range []InferenceStage{c.GatekeeperStage, c.PlannerStage, c.ReviewerStage} {
		if stage == nil || ckpt.skipped(stage.Name()) {
			continue
		}

		sctx, scancel := context.WithTimeout(tctx, c.StageTimeout)
		result := stage.Execute(sctx, ic)
		scancel()

		if result.Error != nil {
			logger.Warnf("[inference_cycle] stage=%s error=%v", stage.Name(), result.Error)
		}

		if result.EarlyReturn {
			if result.Decision != nil {
				ic.Decision = mergeDecision(ic.Decision, *result.Decision)
			}
			ic.Decision.TotalDuration = time.Since(start)
			ic.Decision.Crisis = ic.Crisis
			ic.Decision.Sentiment = ic.Sentiment
			ic.Decision.Intent = ic.Intent
			ic.Decision.Alignment = ic.Alignment
			c.recordStats(ic.Decision)
			ckpt.finish(tctx, ic)
			logger.Infof("[inference_cycle] early return at stage=%s handoff=%v reason=%s duration=%s",
				stage.Name(), ic.Decision.HandoffToHuman, ic.Decision.StopReason, ic.Decision.TotalDuration)
			return &ic.Decision, nil
		}
		ckpt.complete(tctx, stage.Name(), ic)
	}

	ic.Decision.TotalDuration = time.Since(start)
	if ic.Plan != nil {
		ic.Decision.ReplyType = "text"
		ic.Decision.Confidence = ic.Plan.Confidence
		if ic.Plan.SkipLLM {
			ic.Decision.Reply = ic.Plan.ReplyHint
			ic.Decision.ReplyType = "text"
			ic.Decision.StopReason = "faq_skip_llm"
		} else {
			ic.Decision.StopReason = "plan_ready"
		}
	} else {
		ic.Decision.StopReason = "no_plan"
	}
	ic.Decision.Crisis = ic.Crisis
	ic.Decision.Sentiment = ic.Sentiment
	ic.Decision.Intent = ic.Intent
	ic.Decision.Alignment = ic.Alignment

	c.recordStats(ic.Decision)
	ckpt.finish(tctx, ic)
	logger.Infof("[inference_cycle] completed trace=%s stages=%d plan_type=%s confidence=%.2f duration=%s",
		payload.TraceID, len(ic.Stages),
		planTypeOf(ic.Plan), ic.Decision.Confidence, ic.Decision.TotalDuration)

	return &ic.Decision, nil
}

// orderedStageNames 本次闭环实际参与调度的阶段序列（恢复游标的比对依据）。
func (c *InferenceCycle) orderedStageNames() []string {
	names := make([]string, 0, 5)
	for _, s := range []InferenceStage{c.PerceptionStage, c.AlignmentStage, c.GatekeeperStage, c.PlannerStage, c.ReviewerStage} {
		if s != nil {
			names = append(names, s.Name())
		}
	}
	return names
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

func planTypeOf(p *ActionPlan) string {
	if p == nil {
		return "none"
	}
	return p.PlanType
}

func (c *InferenceCycle) recordStats(d InferenceDecision) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastStats.TotalRuns++
	if d.HandoffToHuman {
		c.lastStats.EscalationRuns++
	} else if d.Reply != "" || d.Plan != nil {
		c.lastStats.SuccessRuns++
	} else {
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
