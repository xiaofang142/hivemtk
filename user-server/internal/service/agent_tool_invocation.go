package service

import (
	"errors"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
)

var ErrAgentToolRetryBlocked = errors.New("工具副作用未确认，禁止自动重试")

// CanRetryAgentToolInvocation 只允许在副作用尚未派出、或已确认且有幂等键时重试。
func CanRetryAgentToolInvocation(inv *model.ToolInvocation) bool {
	if inv == nil {
		return false
	}
	switch inv.Status {
	case model.ToolInvocationPlanned, model.ToolInvocationAuthorized:
		return true
	case model.ToolInvocationConfirmed:
		return inv.ToolIdempotencyKey != ""
	default:
		return false
	}
}

// AgentToolExecutionControl 把账本状态映射为执行期副作用边界。
func AgentToolExecutionControl(inv *model.ToolInvocation) tooluse.ToolExecutionControl {
	if inv == nil {
		return tooluse.ToolExecutionControl{RiskClass: model.ToolRiskHighWrite, UnknownOutcome: true}
	}
	return tooluse.ToolExecutionControl{
		RiskClass:         inv.RiskClass,
		IdempotencyKey:    inv.ToolIdempotencyKey,
		AuthorizationHash: inv.AuthorizationHash,
		ResourceVersion:   inv.ResourceVersion,
		UnknownOutcome: inv.Status == model.ToolInvocationUnknown ||
			inv.Status == model.ToolInvocationDispatched ||
			(inv.RiskClass == model.ToolRiskHighWrite && inv.Status != model.ToolInvocationConfirmed),
	}
}

// RequireAgentToolRetry 在自动重试前调用；unexpected unknown/dispatched 直接拒绝。
func RequireAgentToolRetry(inv *model.ToolInvocation) error {
	if !CanRetryAgentToolInvocation(inv) {
		return ErrAgentToolRetryBlocked
	}
	return nil
}
