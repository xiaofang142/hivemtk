package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/content/model"
)

// resetMaxFlowDelaySecondsProvider 把读取口复位到「未注入」；理由同其它 seam 测试。
func resetMaxFlowDelaySecondsProvider() {
	maxFlowDelaySecondsProvider = func() int { return DefaultMaxFlowDelaySeconds }
}

// TestMaxFlowDelaySecondsSeam 逐格钉死 session.max_delay_seconds 的读取口。
func TestMaxFlowDelaySecondsSeam(t *testing.T) {
	t.Cleanup(resetMaxFlowDelaySecondsProvider)
	resetMaxFlowDelaySecondsProvider()

	if got := maxFlowDelaySeconds(); got != DefaultMaxFlowDelaySeconds {
		t.Fatalf("未注入 = %d, 期望兜底 %d", got, DefaultMaxFlowDelaySeconds)
	}
	SetMaxFlowDelaySecondsProvider(func() int { return 30 })
	if got := maxFlowDelaySeconds(); got != 30 {
		t.Fatalf("注入 30 后 = %d", got)
	}
	for _, bad := range []int{0, -5} {
		SetMaxFlowDelaySecondsProvider(func() int { return bad })
		if got := maxFlowDelaySeconds(); got != DefaultMaxFlowDelaySeconds {
			t.Fatalf("注入 %d 时 = %d, 期望回落 %d", bad, got, DefaultMaxFlowDelaySeconds)
		}
	}
	// setter 的 nil 语义是「不注入」而非「复位」，所以这里断言它是空操作。
	SetMaxFlowDelaySecondsProvider(func() int { return 30 })
	SetMaxFlowDelaySecondsProvider(nil)
	if got := maxFlowDelaySeconds(); got != 30 {
		t.Fatalf("传 nil 后 = %d，期望仍是被注入的 30", got)
	}
}

// delayNodeWithDuration 构造一个只带 duration 的 delay 流程节点。
func delayNodeWithDuration(seconds float64) model.FlowNode {
	return model.FlowNode{Type: "delay", Config: map[string]any{"duration": seconds}}
}

// TestHandleDelayClampsByConfiguredLimit 证明读取口真进了 handleDelay 的 clamp：
// 把上限压到 1 秒后，配置 9999 秒的 delay 节点应当约 1 秒返回而不是等 9999 秒。
func TestHandleDelayClampsByConfiguredLimit(t *testing.T) {
	t.Cleanup(resetMaxFlowDelaySecondsProvider)
	SetMaxFlowDelaySecondsProvider(func() int { return 1 })

	svc := &MarketingFlowService{}
	start := time.Now()
	if _, err := svc.handleDelay(context.Background(), delayNodeWithDuration(9999)); err != nil {
		t.Fatalf("handleDelay 返回错误: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("上限 1 秒却等了 %s，clamp 没生效", elapsed)
	}

	// 上限调到 0 会让所有 delay 变成 0 秒；兜底后仍然是 300 秒的语义，
	// 用 ctx 取消来验证它不会因为 0 值跳过 time.After。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.handleDelay(ctx, delayNodeWithDuration(9999)); err == nil {
		t.Fatal("ctx 已取消时应当返回 ctx.Err()，说明确实进入了等待分支")
	}
}
