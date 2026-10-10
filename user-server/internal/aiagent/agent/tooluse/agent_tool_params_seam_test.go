package tooluse

import (
	"testing"
	"time"
)

// 阶段一 agent_tool 组接线：4 条 seam 逐格钉死「未注入==代码兜底 / 注入生效 /
// 非法值回落 / setter 传 nil 等于撤销注入」。

func TestAgentToolIntSeams(t *testing.T) {
	t.Cleanup(func() {
		SetMaxConcurrentProvider(nil)
		SetMaxContentLenProvider(nil)
		SetRouterFailThresholdProvider(nil)
	})

	for _, tc := range []struct {
		name       string
		fallback   int
		uninjected func() int
		set        func(func() int)
		get        func() int
	}{
		{"max_concurrent", DefaultMaxConcurrent, MaxConcurrent, SetMaxConcurrentProvider, MaxConcurrent},
		{"max_content_len", DefaultMaxContentLen, MaxContentLen, SetMaxContentLenProvider, MaxContentLen},
		{"fail_threshold", DefaultRouterFailThreshold, RouterFailThreshold, SetRouterFailThresholdProvider, RouterFailThreshold},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.set(nil)
			if got := tc.uninjected(); got != tc.fallback {
				t.Fatalf("未注入时 = %d，期望代码兜底 %d", got, tc.fallback)
			}
			tc.set(func() int { return 777 })
			if got := tc.get(); got != 777 {
				t.Errorf("注入后 = %d，期望 777", got)
			}
			tc.set(func() int { return 0 })
			if got := tc.get(); got != tc.fallback {
				t.Errorf("注入 0 后 = %d，期望回落 %d", got, tc.fallback)
			}
			tc.set(nil)
			if got := tc.get(); got != tc.fallback {
				t.Errorf("传 nil 撤销注入后 = %d，期望回到兜底 %d", got, tc.fallback)
			}
		})
	}
}

func TestRouterCooldownDurationSeam(t *testing.T) {
	t.Cleanup(func() { SetRouterCooldownDurationProvider(nil) })
	SetRouterCooldownDurationProvider(nil)

	if got := RouterCooldownDuration(); got != DefaultRouterCooldownDuration {
		t.Fatalf("未注入时 = %v，期望兜底 %v", got, DefaultRouterCooldownDuration)
	}
	SetRouterCooldownDurationProvider(func() time.Duration { return 3 * time.Minute })
	if got := RouterCooldownDuration(); got != 3*time.Minute {
		t.Errorf("注入后 = %v，期望 3m", got)
	}
	SetRouterCooldownDurationProvider(func() time.Duration { return 0 })
	if got := RouterCooldownDuration(); got != DefaultRouterCooldownDuration {
		t.Errorf("注入 0 后 = %v，期望回落 %v（冷却 0 等于没有冷却，失败工具会被立刻重选）", got, DefaultRouterCooldownDuration)
	}
	SetRouterCooldownDurationProvider(nil)
	if got := RouterCooldownDuration(); got != DefaultRouterCooldownDuration {
		t.Errorf("传 nil 撤销注入后 = %v，期望回到兜底 %v", got, DefaultRouterCooldownDuration)
	}
}

// TestNewToolRouterPicksUpSeams 钉住构造器真的读了 seam。两个字段在 RouterConfig 里
// 是「<=0 才取兜底」，显式传值必须仍然赢过参数中心——否则调用方精心传的配置会被
// 数据库里的全局默认悄悄顶掉。
func TestNewToolRouterPicksUpSeams(t *testing.T) {
	t.Cleanup(func() {
		SetRouterFailThresholdProvider(nil)
		SetRouterCooldownDurationProvider(nil)
	})

	SetRouterFailThresholdProvider(func() int { return 9 })
	SetRouterCooldownDurationProvider(func() time.Duration { return 99 * time.Second })

	r := NewToolRouter(nil, nil, RouterConfig{})
	if r.failThreshold != 9 {
		t.Errorf("RouterConfig{} 时 failThreshold = %d，期望取到 seam 的 9", r.failThreshold)
	}
	if r.cooldownDuration != 99*time.Second {
		t.Errorf("RouterConfig{} 时 cooldownDuration = %v，期望取到 seam 的 99s", r.cooldownDuration)
	}

	r2 := NewToolRouter(nil, nil, RouterConfig{FailThreshold: 2, CooldownDuration: time.Second})
	if r2.failThreshold != 2 {
		t.Errorf("显式传 FailThreshold=2 被覆盖成 %d：调用方显式配置必须赢过参数中心", r2.failThreshold)
	}
	if r2.cooldownDuration != time.Second {
		t.Errorf("显式传 CooldownDuration=1s 被覆盖成 %v", r2.cooldownDuration)
	}
}
