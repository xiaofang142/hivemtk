package monitor

import (
	"testing"
	"time"
)

func TestNodeHealthWindowSeam(t *testing.T) {
	reset := func() { SetNodeHealthWindowProvider(nil) }
	t.Cleanup(reset)
	reset()

	if got := NodeHealthWindow(); got != DefaultNodeHealthWindow {
		t.Fatalf("未注入时 = %v，期望代码兜底 %v", got, DefaultNodeHealthWindow)
	}

	SetNodeHealthWindowProvider(func() time.Duration { return 7 * time.Hour })
	if got := NodeHealthWindow(); got != 7*time.Hour {
		t.Errorf("注入 7h 未生效：got %v", got)
	}

	// 0 会把 since 推到当下，聚合出的健康率恒为空列表——界面看上去像"所有渠道都没心跳"。
	SetNodeHealthWindowProvider(func() time.Duration { return 0 })
	if got := NodeHealthWindow(); got != DefaultNodeHealthWindow {
		t.Errorf("注入 0 未回落兜底：got %v，期望 %v", got, DefaultNodeHealthWindow)
	}
	SetNodeHealthWindowProvider(func() time.Duration { return -time.Hour })
	if got := NodeHealthWindow(); got != DefaultNodeHealthWindow {
		t.Errorf("注入 -1h 未回落兜底：got %v，期望 %v", got, DefaultNodeHealthWindow)
	}

	reset()
	if got := NodeHealthWindow(); got != DefaultNodeHealthWindow {
		t.Errorf("传 nil 撤销注入后 = %v，期望 %v", got, DefaultNodeHealthWindow)
	}
}
