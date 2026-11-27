package service

import (
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

func TestAgentToolInvocationRetryGate(t *testing.T) {
	tests := []struct {
		inv  *model.ToolInvocation
		want bool
	}{
		{inv: nil, want: false},
		{inv: &model.ToolInvocation{Status: model.ToolInvocationPlanned}, want: true},
		{inv: &model.ToolInvocation{Status: model.ToolInvocationAuthorized}, want: true},
		{inv: &model.ToolInvocation{Status: model.ToolInvocationDispatched}, want: false},
		{inv: &model.ToolInvocation{Status: model.ToolInvocationUnknown}, want: false},
		{inv: &model.ToolInvocation{Status: model.ToolInvocationConfirmed, ToolIdempotencyKey: "idem"}, want: true},
		{inv: &model.ToolInvocation{Status: model.ToolInvocationConfirmed}, want: false},
	}
	for _, tt := range tests {
		got := CanRetryAgentToolInvocation(tt.inv)
		if got != tt.want {
			t.Fatalf("重试判据错误：status=%+v got=%v want=%v", tt.inv, got, tt.want)
		}
		err := RequireAgentToolRetry(tt.inv)
		if got && err != nil || !got && !errors.Is(err, ErrAgentToolRetryBlocked) {
			t.Fatalf("RequireAgentToolRetry 不一致：got=%v err=%v", got, err)
		}
	}
}

func TestAgentToolExecutionControlBlocksUnknownHighWrite(t *testing.T) {
	control := AgentToolExecutionControl(&model.ToolInvocation{
		RiskClass: model.ToolRiskHighWrite, Status: model.ToolInvocationUnknown, ToolIdempotencyKey: "idem",
	})
	if !control.UnknownOutcome || control.RiskClass != model.ToolRiskHighWrite {
		t.Fatalf("unknown high_write 必须标记副作用未知：%+v", control)
	}
	control = AgentToolExecutionControl(&model.ToolInvocation{RiskClass: model.ToolRiskRead, Status: model.ToolInvocationConfirmed})
	if control.UnknownOutcome {
		t.Fatalf("已确认只读工具不应标记未知：%+v", control)
	}
	control = AgentToolExecutionControl(nil)
	if !control.UnknownOutcome || control.RiskClass != model.ToolRiskHighWrite {
		t.Fatalf("nil 账本必须 fail-closed：%+v", control)
	}
}
