package agent_runtime

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

// InferenceStageError 对外仅公开稳定阶段/代码，内部原因保留在错误链。
type InferenceStageError struct {
	Stage string
	Code  string
	cause error
}

func (e *InferenceStageError) Error() string { return e.Stage + ": " + e.Code }
func (e *InferenceStageError) Unwrap() error { return e.cause }

func stageFailure(stage string, cause error) *InferenceStageError {
	code := "stage_failed"
	if errors.Is(cause, context.DeadlineExceeded) {
		code = "stage_timeout"
	} else if errors.Is(cause, context.Canceled) {
		code = "stage_canceled"
	}
	return &InferenceStageError{Stage: stage, Code: code, cause: cause}
}

// runOnce 是普通与流式入口共享的串行五阶段调度器。
func (c *InferenceCycle) runOnce(ctx context.Context, payload CustomerMessagePayload, agentCtx *AgentContext, handler StreamHandler) (*InferenceDecision, error) {
	c.mu.RLock()
	stopped, provider, store := c.stopped, c.memoryProvider, c.ckptStore
	stages := []InferenceStage{c.PerceptionStage, c.AlignmentStage, c.GatekeeperStage, c.PlannerStage, c.ReviewerStage}
	c.mu.RUnlock()
	if stopped {
		return nil, ErrRuntimeStopped
	}
	start := time.Now()
	if agentCtx == nil {
		agentCtx = &AgentContext{AgentCode: "default", Name: "默认智能体", AgentType: "customer_service", LoadedAt: start}
	}
	tctx, cancel := context.WithTimeout(ctx, c.TotalTimeout)
	defer cancel()
	ic := &InferenceContext{Payload: payload, AgentCtx: agentCtx, StartTime: start, Stages: []StageDecision{}, Decision: InferenceDecision{ReplyType: "text"}}
	if provider != nil {
		memory, err := provider.LoadEpisodicMemory(tctx, payload.SessionID, payload.CustomerID)
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).Msg("[inference_cycle] 情境记忆读取失败")
			ic.Decision.Degradations = append(ic.Decision.Degradations, "memory_unavailable")
		} else {
			ic.EpisodicMemory = memory
		}
	}
	order := make([]string, 0, len(stages))
	for _, stage := range stages {
		if stage != nil {
			order = append(order, stage.Name())
		}
	}
	ckpt := newCycleCheckpoint(tctx, store, payload, ic, order)
	names := []string{"perception", "alignment", "gatekeeper", "planner", "reviewer"}
	for index, stage := range stages {
		name := names[index]
		if stage == nil {
			if index == 2 || index == 4 {
				failure := &InferenceStageError{Stage: name, Code: "stage_not_configured"}
				ic.Stages = append(ic.Stages, StageDecision{Stage: name, Action: "failed", Error: failure.Code, Outcome: InferenceFailed})
				return c.failRun(tctx, ic, ckpt, failure)
			}
			continue
		}
		name = stage.Name()
		if ckpt.skipped(name) {
			continue
		}
		if err := tctx.Err(); err != nil {
			failure := stageFailure(name, err)
			ic.Stages = append(ic.Stages, StageDecision{Stage: name, Action: "failed", Error: failure.Code, Outcome: InferenceFailed})
			return c.failRun(tctx, ic, ckpt, failure)
		}
		if handler != nil {
			handler.OnEvent(StreamEvent{Type: StreamEventStageStart, StageName: name})
		}
		before := len(ic.Stages)
		stageStart := time.Now()
		sctx, scancel := context.WithTimeout(tctx, c.StageTimeout)
		result := stage.Execute(sctx, ic)
		deadline := sctx.Err()
		scancel()
		if result.Decision != nil {
			ic.Decision = mergeDecision(ic.Decision, *result.Decision)
		}
		cause := result.Error
		if deadline != nil {
			cause = deadline
		}
		if cause == nil && ((!result.Continue && !result.EarlyReturn) || (result.Continue && result.EarlyReturn)) {
			cause = errors.New("invalid stage result")
		}
		if cause == nil && result.EarlyReturn && !ic.Decision.HandoffToHuman && index != len(stages)-1 {
			cause = errors.New("early reply bypasses mandatory review")
		}
		trace := StageDecision{Stage: name, Action: "continue", Success: true, Outcome: InferenceSucceeded}
		if len(ic.Stages) > before {
			trace = ic.Stages[len(ic.Stages)-1]
		}
		trace.Stage, trace.Duration, trace.Success, trace.Outcome = name, time.Since(stageStart), cause == nil, InferenceSucceeded
		if cause != nil {
			failure := stageFailure(name, cause)
			trace.Action, trace.Reason, trace.Error, trace.Outcome = "failed", failure.Code, failure.Code, InferenceFailed
		} else {
			trace.Error = ""
		}
		rejected := cause == nil && index == len(stages)-1 && ic.Decision.Review != nil && !ic.Decision.Review.Passed
		if rejected {
			trace.Outcome, trace.Success = InferenceRejected, false
		}
		ic.Stages = append(ic.Stages[:before], trace)
		if handler != nil {
			handler.OnEvent(StreamEvent{Type: StreamEventStageEnd, StageName: name, Duration: trace.Duration, Outcome: trace.Outcome, Error: trace.Error})
		}
		if cause != nil {
			return c.failRun(tctx, ic, ckpt, stageFailure(name, cause))
		}
		if rejected {
			clearPublication(ic)
			ic.Decision.Outcome, ic.Decision.StopReason = InferenceRejected, "review_rejected"
			return c.finishRun(tctx, ic, ckpt), nil
		}
		if ic.Decision.HandoffToHuman {
			ic.Decision.Outcome = InferenceHandoff
			ic.Decision.Publishable = false
			return c.finishRun(tctx, ic, ckpt), nil
		}
		ckpt.complete(tctx, name, ic)
	}
	if ic.Plan != nil {
		ic.Decision.Plan, ic.Decision.Confidence = ic.Plan, ic.Plan.Confidence
		ic.Decision.StopReason = "plan_ready"
		if ic.Plan.SkipLLM {
			if ic.Decision.Reply == "" {
				ic.Decision.Reply = ic.Plan.ReplyHint
			}
			ic.Decision.StopReason = "faq_skip_llm"
		}
	} else if ic.Decision.Reply == "" {
		ic.Decision.StopReason = "no_plan"
	}
	ic.Decision.Outcome = InferenceSucceeded
	ic.Decision.Publishable = ic.Decision.Reply != "" || (ic.Plan != nil && ic.Plan.ReplyHint != "")
	return c.finishRun(tctx, ic, ckpt), nil
}

func clearPublication(ic *InferenceContext) {
	ic.Plan, ic.Decision.Plan, ic.Decision.Reply = nil, nil, ""
	ic.Decision.HandoffToHuman, ic.Decision.HandoffReason, ic.Decision.Publishable = false, "", false
	if ic.Decision.Review != nil {
		review := *ic.Decision.Review
		review.AdjustedReply = ""
		ic.Decision.Review = &review
	}
}

func collectDecision(ic *InferenceContext) {
	ic.Decision.TotalDuration = time.Since(ic.StartTime)
	ic.Decision.Crisis, ic.Decision.Sentiment = ic.Crisis, ic.Sentiment
	ic.Decision.Intent, ic.Decision.Alignment = ic.Intent, ic.Alignment
	ic.Decision.Stages = append([]StageDecision{}, ic.Stages...)
}

func (c *InferenceCycle) failRun(ctx context.Context, ic *InferenceContext, ckpt *cycleCheckpoint, failure *InferenceStageError) (*InferenceDecision, error) {
	logger.Ctx(ctx).Warn().Err(failure.cause).Str("stage", failure.Stage).Str("code", failure.Code).Msg("[inference_cycle] 阶段失败")
	clearPublication(ic)
	ic.Decision.Outcome, ic.Decision.FailedStage, ic.Decision.ErrorCode = InferenceFailed, failure.Stage, failure.Code
	ic.Decision.StopReason = failure.Code
	collectDecision(ic)
	ckpt.failed(ctx, ic)
	c.recordStats(ic.Decision)
	return &ic.Decision, failure
}

func (c *InferenceCycle) finishRun(ctx context.Context, ic *InferenceContext, ckpt *cycleCheckpoint) *InferenceDecision {
	if len(ic.Decision.Degradations) > 0 && ic.Decision.Outcome == InferenceSucceeded {
		ic.Decision.Outcome = InferenceDegraded
	}
	collectDecision(ic)
	ckpt.finish(ctx, ic)
	if len(ic.Decision.Degradations) > 0 && ic.Decision.Outcome == InferenceSucceeded {
		ic.Decision.Outcome = InferenceDegraded
	}
	c.recordStats(ic.Decision)
	return &ic.Decision
}
