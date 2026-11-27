package tooluse

import "context"

// ToolExecutionControl 描述一次工具调用的副作用边界；由上层在执行前写入 ctx。
// 未声明时保持既有重试行为；high_write 或结果未知时必须禁止自动重放。
type ToolExecutionControl struct {
	RiskClass         string
	IdempotencyKey    string
	AuthorizationHash string
	ResourceVersion   string
	UnknownOutcome    bool
}

type toolExecutionControlKey struct{}

func WithToolExecutionControl(ctx context.Context, control ToolExecutionControl) context.Context {
	return context.WithValue(ctx, toolExecutionControlKey{}, control)
}

func GetToolExecutionControl(ctx context.Context) *ToolExecutionControl {
	if v, ok := ctx.Value(toolExecutionControlKey{}).(ToolExecutionControl); ok {
		return &v
	}
	return nil
}

func toolRetryForbidden(ctx context.Context) bool {
	control := GetToolExecutionControl(ctx)
	if control == nil {
		return false
	}
	return control.UnknownOutcome || control.RiskClass == "high_write" || control.RiskClass == ""
}
