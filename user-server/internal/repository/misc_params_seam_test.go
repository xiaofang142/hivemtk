package repository

import (
	"testing"
	"time"
)

// TestPollingLockStaleThresholdSeam 轮询锁过期阈值的接线。
//
// 这个 seam 是本仓既有的样板（telegram_polling_lock.go 先有，后 15 条照它铺），
// 所以连"兜底值必须等于种子 DefaultValue"一起钉住，防止后来者改兜底时没人发现
// 参数中心的默认值与代码脱钩。
func TestPollingLockStaleThresholdSeam(t *testing.T) {
	restore := pollingLockStaleThresholdProvider
	t.Cleanup(func() { pollingLockStaleThresholdProvider = restore })

	if got, want := PollingLockStaleThreshold, 60*time.Second; got != want {
		t.Fatalf("兜底值 = %v，与参数中心 misc.polling_lock_stale_threshold 的 DefaultValue（60 秒 = %v）不一致", got, want)
	}

	SetPollingLockStaleThresholdProvider(nil)
	if got := GetPollingLockStaleThreshold(); got != PollingLockStaleThreshold {
		t.Fatalf("nil 注入时 = %v，期望回落 %v", got, PollingLockStaleThreshold)
	}

	SetPollingLockStaleThresholdProvider(func() time.Duration { return 15 * time.Minute })
	if got := GetPollingLockStaleThreshold(); got != 15*time.Minute {
		t.Fatalf("注入后 = %v，期望 15m", got)
	}
}

// TestPreviewMaxLenSeam 会话预览长度的接线。
func TestPreviewMaxLenSeam(t *testing.T) {
	restore := previewMaxLenProvider
	t.Cleanup(func() { previewMaxLenProvider = restore })

	if got, want := defaultPreviewMaxLen, 500; got != want {
		t.Fatalf("兜底值 = %v，与参数中心 misc.preview_max_len 的 DefaultValue（%d）不一致", got, want)
	}

	SetPreviewMaxLenProvider(nil)
	if got := previewMaxLen(); got != defaultPreviewMaxLen {
		t.Fatalf("nil 注入时 = %v，期望回落 %d", got, defaultPreviewMaxLen)
	}

	SetPreviewMaxLenProvider(func() int { return 42 })
	if got := previewMaxLen(); got != 42 {
		t.Fatalf("注入后 = %v，期望 42", got)
	}
	SetPreviewMaxLenProvider(func() int { return 0 })
	if got := previewMaxLen(); got != defaultPreviewMaxLen {
		t.Fatalf("注入 0 时 = %v，期望回落 %d（0 会把预览全截没）", got, defaultPreviewMaxLen)
	}
}
