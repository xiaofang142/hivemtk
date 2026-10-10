package featureflag

import (
	"testing"
	"time"
)

func TestPollIntervalSeam(t *testing.T) {
	reset := func() { SetPollIntervalProvider(nil) }
	t.Cleanup(reset)
	reset()

	if got := PollInterval(); got != DefaultPollInterval {
		t.Fatalf("未注入时 = %v，期望代码兜底 %v", got, DefaultPollInterval)
	}

	SetPollIntervalProvider(func() time.Duration { return 30 * time.Second })
	if got := PollInterval(); got != 30*time.Second {
		t.Errorf("注入 30s 未生效：got %v", got)
	}

	// time.NewTicker(0) 直接 panic，所以这里回落不是保守，是必须的。
	SetPollIntervalProvider(func() time.Duration { return 0 })
	if got := PollInterval(); got != DefaultPollInterval {
		t.Errorf("注入 0 未回落兜底：got %v，期望 %v（NewTicker(0) 会 panic）", got, DefaultPollInterval)
	}
	SetPollIntervalProvider(func() time.Duration { return -time.Second })
	if got := PollInterval(); got != DefaultPollInterval {
		t.Errorf("注入 -1s 未回落兜底：got %v，期望 %v", got, DefaultPollInterval)
	}

	reset()
	if got := PollInterval(); got != DefaultPollInterval {
		t.Errorf("传 nil 撤销注入后 = %v，期望 %v", got, DefaultPollInterval)
	}
}
