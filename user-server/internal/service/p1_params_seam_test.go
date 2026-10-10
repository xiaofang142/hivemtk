package service

import (
	"testing"
	"time"
)

// resetP1Providers 复位本批四个 provider。
//
// 恢复的是「返回兜底常量的函数」，不是 nil：包级变量的初始值就是这种函数，
// 置 nil 会让后面的读取直接空指针 panic。
// 也不能调 SetXxxProvider(nil) —— setter 的 nil 语义是「不注入」，不是「复位」。
func resetP1Providers(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		audienceDefaultLimitProvider = func() int { return DefaultAudienceLimit }
		audienceMaxLimitProvider = func() int { return MaxAudienceLimit }
		editLockTTLProvider = func() time.Duration { return EditLockTTL }
		l1TTLHoursProvider = func() time.Duration { return L1TTLHours }
	})
}

func TestAudienceLimitSeams(t *testing.T) {
	resetP1Providers(t)

	if got := EffectiveAudienceDefaultLimit(); got != DefaultAudienceLimit {
		t.Errorf("未注入默认人数得到 %d, 期望兜底 %d", got, DefaultAudienceLimit)
	}
	if got := EffectiveAudienceMaxLimit(); got != MaxAudienceLimit {
		t.Errorf("未注入硬上限得到 %d, 期望兜底 %d", got, MaxAudienceLimit)
	}

	SetAudienceDefaultLimitProvider(func() int { return 42 })
	SetAudienceMaxLimitProvider(func() int { return 99 })
	if got := EffectiveAudienceDefaultLimit(); got != 42 {
		t.Errorf("注入 42 后得到 %d", got)
	}
	if got := EffectiveAudienceMaxLimit(); got != 99 {
		t.Errorf("注入 99 后得到 %d", got)
	}

	// 非正数一律回落。上限被改成 0 的后果特别隐蔽：每一轮都截断到空名单，
	// 而空名单在 Reasons 上表现为 no_match，看起来像"条件太严"。
	for _, bad := range []int{0, -1, -1000} {
		n := bad
		SetAudienceDefaultLimitProvider(func() int { return n })
		SetAudienceMaxLimitProvider(func() int { return n })
		if got := EffectiveAudienceDefaultLimit(); got != DefaultAudienceLimit {
			t.Errorf("默认人数注入 %d 得到 %d, 期望兜底 %d", bad, got, DefaultAudienceLimit)
		}
		if got := EffectiveAudienceMaxLimit(); got != MaxAudienceLimit {
			t.Errorf("硬上限注入 %d 得到 %d, 期望兜底 %d", bad, got, MaxAudienceLimit)
		}
	}
}

// TestAudienceConfigLimitNormalization 行为级：归一化真的按配置值走。
// 这是最容易被"只接了默认值忘了上限"破坏的一格——两个 seam 必须同时在链路上。
func TestAudienceConfigLimitNormalization(t *testing.T) {
	resetP1Providers(t)

	// 没写 limit → 走默认
	if got := (AudienceConfig{Tags: []string{"x"}}).limit(); got != DefaultAudienceLimit {
		t.Errorf("未写 limit 得到 %d, 期望 %d", got, DefaultAudienceLimit)
	}
	// 写小了 → 原样
	if got := (AudienceConfig{Tags: []string{"x"}, Limit: 7}).limit(); got != 7 {
		t.Errorf("limit=7 得到 %d", got)
	}
	// 写超了 → 夹到硬上限
	if got := (AudienceConfig{Tags: []string{"x"}, Limit: 99999}).limit(); got != MaxAudienceLimit {
		t.Errorf("limit=99999 得到 %d, 期望夹到 %d", got, MaxAudienceLimit)
	}

	// 两条都改：默认与上限必须一起动，只动一条会露出下面这两格
	SetAudienceDefaultLimitProvider(func() int { return 3 })
	SetAudienceMaxLimitProvider(func() int { return 5 })
	c := AudienceConfig{Tags: []string{"x"}}
	if got := c.limit(); got != 3 {
		t.Errorf("默认改成 3 后未写 limit 得到 %d", got)
	}
	c.Limit = 100
	if got := c.limit(); got != 5 {
		t.Errorf("上限改成 5 后 limit=100 得到 %d", got)
	}
}

func TestEditLockTTLSeam(t *testing.T) {
	resetP1Providers(t)
	if got := EffectiveEditLockTTL(); got != EditLockTTL {
		t.Errorf("未注入得到 %v, 期望兜底 %v", got, EditLockTTL)
	}
	SetEditLockTTLProvider(func() time.Duration { return 90 * time.Second })
	if got := EffectiveEditLockTTL(); got != 90*time.Second {
		t.Errorf("注入 90s 后得到 %v", got)
	}
	// 0 会让锁在写下的瞬间就过期，协作碰撞检测整套失效，而且接口照常返回"抢到了"。
	for _, bad := range []time.Duration{0, -time.Second, -time.Hour} {
		d := bad
		SetEditLockTTLProvider(func() time.Duration { return d })
		if got := EffectiveEditLockTTL(); got != EditLockTTL {
			t.Errorf("注入 %v 得到 %v, 期望兜底 %v", bad, got, EditLockTTL)
		}
	}
}

func TestL1TTLHoursSeam(t *testing.T) {
	resetP1Providers(t)
	if got := EffectiveL1TTLHours(); got != L1TTLHours {
		t.Errorf("未注入得到 %v, 期望兜底 %v", got, L1TTLHours)
	}
	SetL1TTLHoursProvider(func() time.Duration { return 2 * time.Hour })
	if got := EffectiveL1TTLHours(); got != 2*time.Hour {
		t.Errorf("注入 2h 后得到 %v", got)
	}
	for _, bad := range []time.Duration{0, -time.Second, -48 * time.Hour} {
		d := bad
		SetL1TTLHoursProvider(func() time.Duration { return d })
		if got := EffectiveL1TTLHours(); got != L1TTLHours {
			t.Errorf("注入 %v 得到 %v, 期望兜底 %v", bad, got, L1TTLHours)
		}
	}
}

// TestDefaultConstantsAreWhatP1SeedsClaim 兜底常量必须与种子 DefaultValue 一致。
// 种子默认值会被 ResetToDefault / BulkResetGroup 写回库里，两边不一致等于
// 「点一下重置，系统行为就变」。
func TestDefaultConstantsAreWhatP1SeedsClaim(t *testing.T) {
	if DefaultAudienceLimit != 200 {
		t.Errorf("DefaultAudienceLimit = %d, 期望 200（种子 sales.audience_default_limit）", DefaultAudienceLimit)
	}
	if MaxAudienceLimit != 500 {
		t.Errorf("MaxAudienceLimit = %d, 期望 500（种子 sales.audience_max_limit）", MaxAudienceLimit)
	}
	if EditLockTTL != 5*time.Minute {
		t.Errorf("EditLockTTL = %v, 期望 5m（种子 misc.edit_lock_ttl 默认 300s）", EditLockTTL)
	}
	if L1TTLHours != 24*time.Hour {
		t.Errorf("L1TTLHours = %v, 期望 24h（种子 memory.l1_ttl_hours 默认 86400s）", L1TTLHours)
	}
}
