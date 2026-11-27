package tooluse

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fixedRetryPolicy struct{}

func (fixedRetryPolicy) MaxAttempts() int { return 3 }
func (fixedRetryPolicy) NextBackoff(int, error) (time.Duration, bool) {
	return 0, true
}

func TestRetryDecoratorAllowsOrdinaryRetries(t *testing.T) {
	calls := 0
	handler := RetryDecorator(fixedRetryPolicy{})(func(ctx context.Context, args map[string]any) (ToolResult, error) {
		calls++
		if calls < 3 {
			return ToolResult{}, errors.New("temporary")
		}
		return ToolResult{Success: true}, nil
	})
	if _, err := handler(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("普通工具应允许重试，实际调用 %d 次", calls)
	}
}

func TestRetryDecoratorBlocksHighWriteBlindRetry(t *testing.T) {
	for _, control := range []ToolExecutionControl{
		{RiskClass: "high_write", IdempotencyKey: "idem-1"},
		{RiskClass: "write", UnknownOutcome: true},
	} {
		calls := 0
		handler := RetryDecorator(fixedRetryPolicy{})(func(ctx context.Context, args map[string]any) (ToolResult, error) {
			calls++
			return ToolResult{}, errors.New("temporary")
		})
		ctx := WithToolExecutionControl(context.Background(), control)
		if _, err := handler(ctx, nil); err == nil {
			t.Fatal("副作用不确定的工具不得重试成功")
		}
		if calls != 1 {
			t.Fatalf("high_write/unknown 不得盲重试，实际调用 %d 次：%+v", calls, control)
		}
	}
}

func TestToolExecutionControlRoundTrip(t *testing.T) {
	want := ToolExecutionControl{RiskClass: "high_write", IdempotencyKey: "idem-1", AuthorizationHash: "auth", ResourceVersion: "v1"}
	ctx := WithToolExecutionControl(context.Background(), want)
	got := GetToolExecutionControl(ctx)
	if got == nil || *got != want {
		t.Fatalf("工具执行控制未透传：%+v", got)
	}
	if GetToolExecutionControl(context.Background()) != nil {
		t.Fatal("无控制时不得伪造默认值")
	}
}
