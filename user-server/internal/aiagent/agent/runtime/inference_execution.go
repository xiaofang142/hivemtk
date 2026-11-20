package agent_runtime

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/aiagent/agent/delivery"
	"hivemtk-user/internal/pkg/utils/logger"
)

type ExecutionMode string

const (
	ExecutionModePlanning  ExecutionMode = "planning"
	ExecutionModeExecution ExecutionMode = "execution"
)

type ExecutionStatus string

const (
	ExecutionNotExecuted      ExecutionStatus = "not_executed"
	ExecutionPendingApproval  ExecutionStatus = "pending_approval"
	ExecutionSucceeded        ExecutionStatus = "succeeded"
	ExecutionFailed           ExecutionStatus = "failed"
	ExecutionRejected         ExecutionStatus = "rejected"
	ExecutionHandoffRequested ExecutionStatus = "handoff_requested"
	ExecutionDeliveryPending  ExecutionStatus = "delivery_pending"
)

type ExecutionAuthorization string

const (
	ExecutionApproved        ExecutionAuthorization = "approved"
	ExecutionApprovalPending ExecutionAuthorization = "pending"
	ExecutionDenied          ExecutionAuthorization = "rejected"
)

// ExecutionRequest 保留身份及完整计划；ToolCall 为空表示直答或转人工动作。
type ExecutionRequest struct {
	Payload      CustomerMessagePayload
	AgentContext *AgentContext
	Plan         *ActionPlan
	ToolCall     *PlannedToolCall
	Handoff      bool
}

// ExecutionGate 必须检查授权、风险及审批；未显式批准一律不执行。
// 生产装配须委托既有权限/审批策略，不能把观察态端口自动接为强制放行。
type ExecutionGate interface {
	Authorize(context.Context, ExecutionRequest) (ExecutionAuthorization, error)
}

type ToolExecutionResult struct {
	ToolName  string `json:"tool_name"`
	Success   bool   `json:"success"`
	Data      string `json:"data,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// ReplyGenerator 接收实际工具结果；Plan.ReplyHint 只是规划上下文，不是最终正文。
type ReplyGenerator interface {
	GenerateReply(context.Context, CustomerMessagePayload, *AgentContext, *ActionPlan, []ToolExecutionResult) (string, error)
}

type executionPorts struct {
	gate      ExecutionGate
	router    ToolRouterExecutor
	generator ReplyGenerator
	reviewer  Reviewer
	trimmer   ResponseTrimmer
	delivery  delivery.Gateway
}

func (o *CoreDataFlowOrchestrator) SetDeliveryGateway(gateway delivery.Gateway) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.deliveryGateway = gateway
}

func (o *CoreDataFlowOrchestrator) SetExecutionGate(gate ExecutionGate) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.executionGate = gate
}
func (o *CoreDataFlowOrchestrator) SetReplyGenerator(generator ReplyGenerator) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.replyGenerator = generator
}
func (o *CoreDataFlowOrchestrator) SetFinalReviewer(reviewer Reviewer) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.finalReviewer = reviewer
}

// InferenceExecutionError 对外只提供稳定代码，原始原因留在错误链及服务日志。
type InferenceExecutionError struct {
	Code  string
	cause error
}

func (e *InferenceExecutionError) Error() string { return e.Code }
func (e *InferenceExecutionError) Unwrap() error { return e.cause }

func (o *CoreDataFlowOrchestrator) executionFailure(ctx context.Context, result *OrchestratorResult, code string, cause error) (*OrchestratorResult, error) {
	result.Outcome, result.ExecutionStatus, result.ErrorCode = InferenceFailed, ExecutionFailed, code
	result.FinalReply, result.Publishable = "", false
	o.recordError()
	logger.Ctx(ctx).Warn().Err(cause).Str("code", code).Msg("[dataflow] 执行失败")
	return result, &InferenceExecutionError{Code: code, cause: cause}
}

func (o *CoreDataFlowOrchestrator) executePlan(ctx context.Context, payload CustomerMessagePayload, agentCtx *AgentContext, decision *InferenceDecision, result *OrchestratorResult, ports executionPorts) (*OrchestratorResult, error) {
	if err := ctx.Err(); err != nil {
		return o.executionFailure(ctx, result, "execution_canceled", err)
	}
	plan := decision.Plan
	direct := plan != nil && plan.SkipLLM && len(plan.ToolCalls) == 0 && decision.Reply != ""
	if ports.gate == nil || ports.delivery == nil || (!decision.HandoffToHuman && (ports.reviewer == nil || (!direct && ports.generator == nil) || (result.PlannedToolCallCount > 0 && ports.router == nil))) {
		return o.executionFailure(ctx, result, "execution_not_configured", nil)
	}
	intent := delivery.Intent{MessageID: payload.MessageID, Channel: payload.ChannelType, AccountID: payload.AccountID, CustomerID: payload.CustomerID, SessionID: result.SessionID, AgentID: agentCtx.AgentID, AgentCode: agentCtx.AgentCode, AgentVersion: agentCtx.Version, Kind: "reply"}
	if decision.HandoffToHuman {
		intent.Kind = "handoff"
	}
	if _, err := delivery.Identity(ctx, intent); err != nil {
		return o.executionFailure(ctx, result, err.Error(), err)
	}
	// 对全部拟执行调用先完成预检；后一个调用待批时，前一个也不得抢跑。
	requests := []*PlannedToolCall{nil}
	if plan != nil && len(plan.ToolCalls) > 0 && !decision.HandoffToHuman {
		requests = make([]*PlannedToolCall, len(plan.ToolCalls))
		for index := range plan.ToolCalls {
			requests[index] = &plan.ToolCalls[index]
		}
	}
	pending := false
	for _, call := range requests {
		state, err := ports.gate.Authorize(ctx, ExecutionRequest{payload, agentCtx, plan, call, decision.HandoffToHuman})
		if err != nil {
			return o.executionFailure(ctx, result, "execution_authorization_failed", err)
		}
		if err := ctx.Err(); err != nil {
			return o.executionFailure(ctx, result, "execution_canceled", err)
		}
		switch state {
		case ExecutionApproved:
		case ExecutionApprovalPending:
			pending = true
		default:
			result.Outcome, result.ExecutionStatus = InferenceRejected, ExecutionRejected
			result.ErrorCode = "execution_not_authorized"
			o.mu.Lock()
			o.stats.RejectedCount++
			o.mu.Unlock()
			return result, nil
		}
	}
	if pending {
		result.Outcome, result.ExecutionStatus = InferencePendingApproval, ExecutionPendingApproval
		return result, nil
	}
	if decision.HandoffToHuman {
		result.Executed = true
		intent.Content = decision.HandoffReason
		return o.submitDelivery(ctx, result, ports.delivery, intent)
	}
	if plan != nil {
		for _, call := range plan.ToolCalls {
			if err := ctx.Err(); err != nil {
				return o.executionFailure(ctx, result, "execution_canceled", err)
			}
			result.Executed = true
			result.ToolCallCount++
			o.recordToolCall()
			output, err := ports.router.Execute(ctx, call.ToolName, call.Args)
			toolResult := ToolExecutionResult{ToolName: call.ToolName, Success: output.Success, Data: output.Data}
			if err == nil {
				err = ctx.Err()
			}
			if err != nil || !output.Success {
				if err == nil {
					err = errors.New(output.Error)
				}
				toolResult.Success, toolResult.Data, toolResult.ErrorCode = false, "", "tool_execution_failed"
				result.ToolResults = append(result.ToolResults, toolResult)
				return o.executionFailure(ctx, result, "tool_execution_failed", err)
			}
			result.ToolResults = append(result.ToolResults, toolResult)
		}
	}
	reply := decision.Reply
	result.Executed = true
	if !direct {
		generated, err := ports.generator.GenerateReply(ctx, payload, agentCtx, plan, result.ToolResults)
		if err != nil {
			return o.executionFailure(ctx, result, "reply_generation_failed", err)
		}
		reply = generated
	}
	// 复核实际将发出的裁剪稿；复核器如给出修改稿，最多再复核一次。
	for attempt := 0; attempt < 2; attempt++ {
		if ports.trimmer != nil {
			reply = ports.trimmer.Trim(reply)
		}
		reply = strings.TrimSpace(reply)
		if err := ctx.Err(); err != nil {
			return o.executionFailure(ctx, result, "execution_canceled", err)
		}
		if reply == "" {
			return o.executionFailure(ctx, result, "reply_empty", nil)
		}
		ic := &InferenceContext{Payload: payload, AgentCtx: agentCtx, Plan: plan, Decision: *decision, Alignment: decision.Alignment, Sentiment: decision.Sentiment, Intent: decision.Intent, Crisis: decision.Crisis}
		ic.Decision.Reply, ic.Decision.Review = reply, nil
		review, err := ports.reviewer.Review(ctx, ic)
		if err != nil {
			return o.executionFailure(ctx, result, "final_review_failed", err)
		}
		if err := ctx.Err(); err != nil {
			return o.executionFailure(ctx, result, "execution_canceled", err)
		}
		if review == nil {
			return o.executionFailure(ctx, result, "final_review_invalid", nil)
		}
		if !review.Passed {
			result.Outcome, result.ExecutionStatus, result.ErrorCode = InferenceRejected, ExecutionRejected, "final_review_rejected"
			o.mu.Lock()
			o.stats.RejectedCount++
			o.mu.Unlock()
			return result, nil
		}
		if review.AdjustedReply != "" && review.AdjustedReply != reply {
			if attempt == 1 {
				return o.executionFailure(ctx, result, "final_review_unstable", nil)
			}
			reply = review.AdjustedReply
			continue
		}
		intent.Content = reply
		return o.submitDelivery(ctx, result, ports.delivery, intent)
	}
	return o.executionFailure(ctx, result, "final_review_invalid", nil)
}

// submitDelivery 将同步调用与可靠受理、外部确认分开；只凭 Publish/Trigger 的 nil 不生成确认。
func (o *CoreDataFlowOrchestrator) submitDelivery(ctx context.Context, result *OrchestratorResult, gateway delivery.Gateway, intent delivery.Intent) (*OrchestratorResult, error) {
	record, err := gateway.Submit(ctx, intent)
	result.Delivery = record
	if err != nil {
		code := "delivery_store_failed"
		if errors.Is(err, delivery.ErrConflict) {
			code = delivery.ErrConflict.Error()
		}
		return o.executionFailure(ctx, result, code, err)
	}
	key, identityErr := delivery.Identity(ctx, intent)
	if identityErr != nil || record == nil || record.Key != key || record.Kind != intent.Kind {
		return o.executionFailure(ctx, result, "delivery_record_invalid", nil)
	}
	switch record.State {
	case delivery.Confirmed:
		if record.ConfirmedCount != 1 || record.AcceptedCount != 1 || record.ReceiptID == "" {
			return o.executionFailure(ctx, result, "delivery_receipt_invalid", nil)
		}
		if intent.Kind == "handoff" {
			result.ExecutionStatus = ExecutionHandoffRequested
			if record.NewConfirmed {
				o.recordHandoff()
			}
		} else {
			result.FinalReply, result.Publishable, result.ExecutionStatus = intent.Content, true, ExecutionSucceeded
			if record.NewConfirmed && result.ToolCallCount == 0 {
				o.recordDirectReply()
			}
		}
	case delivery.Rejected:
		result.Outcome, result.ExecutionStatus, result.ErrorCode = InferenceRejected, ExecutionRejected, record.ErrorCode
		o.mu.Lock()
		o.stats.RejectedCount++
		o.mu.Unlock()
	case delivery.Intended, delivery.Blocked, delivery.Sending, delivery.Unknown, delivery.Accepted:
		result.Outcome, result.ExecutionStatus = InferenceDeliveryPending, ExecutionDeliveryPending
	default:
		return o.executionFailure(ctx, result, "delivery_record_invalid", nil)
	}
	return result, nil
}
