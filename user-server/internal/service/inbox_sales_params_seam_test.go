package service

import (
	"testing"
)

// resetLeadOpportunityThresholdProviders 把两处线索阈值读取口复位到「未注入」；
// 生产 setter 的 nil 语义是「不注入」，所以测试只能同包直接写包内变量复位。
func resetLeadOpportunityThresholdProviders() {
	tgLeadOpportunityThresholdProvider = func() int { return DefaultTgLeadOpportunityThreshold }
	unifiedMinerOpportunityThresholdProvider = func() int { return DefaultUnifiedMinerOpportunityThreshold }
}

func resetPreviewSampleLimitProvider() {
	previewSampleLimitProvider = func() int { return DefaultPreviewSampleLimit }
}

// TestLeadOpportunityThresholdSeams 逐格钉死 inbox_sales 两处线索机会阈值的读取口。
func TestLeadOpportunityThresholdSeams(t *testing.T) {
	t.Cleanup(resetLeadOpportunityThresholdProviders)
	resetLeadOpportunityThresholdProviders()
	resetPreviewSampleLimitProvider()

	cases := []struct {
		name string
		set  func(func() int)
		get  func() int
		fb   int
	}{
		{"tg", SetTgLeadOpportunityThresholdProvider, tgLeadOpportunityThreshold, DefaultTgLeadOpportunityThreshold},
		{"unified", SetUnifiedMinerOpportunityThresholdProvider, unifiedMinerOpportunityThreshold, DefaultUnifiedMinerOpportunityThreshold},
	}

	for _, c := range cases {
		t.Run(c.name+"/未注入等于种子默认值", func(t *testing.T) {
			if got := c.get(); got != c.fb {
				t.Fatalf("未注入 = %d, 期望兜底 %d", got, c.fb)
			}
		})
		t.Run(c.name+"/注入生效", func(t *testing.T) {
			c.set(func() int { return 77 })
			if got := c.get(); got != 77 {
				t.Fatalf("注入 77 后 = %d", got)
			}
		})
		t.Run(c.name+"/非正值回落兜底", func(t *testing.T) {
			for _, bad := range []int{0, -1} {
				c.set(func() int { return bad })
				if got := c.get(); got != c.fb {
					t.Fatalf("注入 %d 时 = %d, 期望回落 %d", bad, got, c.fb)
				}
			}
		})
		t.Run(c.name+"/setter 传 nil 是空操作", func(t *testing.T) {
			// nil 语义是「不注入」而非「复位」：装配顺序错时传 nil 不该把兜底值顶掉。
			c.set(func() int { return 77 })
			c.set(nil)
			if got := c.get(); got != 77 {
				t.Fatalf("传 nil 后 = %d，期望仍是被注入的 77", got)
			}
			resetLeadOpportunityThresholdProviders()
		})
	}
}

// TestDetectTelegramIntentRespectsThresholdSeam 证明读取口真进了打分判定：
// 「微信」这条纯信号打分 43，恰好卡在默认阈值 40 与 50 之间，
// 把阈值抬到 50 之后它应该从「商机」掉回普通线索。
func TestDetectTelegramIntentRespectsThresholdSeam(t *testing.T) {
	t.Cleanup(resetLeadOpportunityThresholdProviders)

	score, _, opp := DetectTelegramIntent("微信")
	if score <= DefaultTgLeadOpportunityThreshold || score >= 50 {
		t.Fatalf("样本打分 %d 不在 (%d, 50) 区间内，测不出阈值效果", score, DefaultTgLeadOpportunityThreshold)
	}
	if !opp {
		t.Fatalf("打分 %d 在默认阈值 %d 之上却没判成商机", score, DefaultTgLeadOpportunityThreshold)
	}

	SetTgLeadOpportunityThresholdProvider(func() int { return 50 })
	if _, _, opp = DetectTelegramIntent("微信"); opp {
		t.Fatal("阈值抬到 50 后打分 43 应不再算商机")
	}
}

// TestDetectUnifiedIntentRespectsThresholdSeam 同上，走统一挖掘那条判定路径。
func TestDetectUnifiedIntentRespectsThresholdSeam(t *testing.T) {
	t.Cleanup(resetLeadOpportunityThresholdProviders)

	score, _, opp := DetectUnifiedIntent("微信", nil, nil)
	if score <= DefaultUnifiedMinerOpportunityThreshold || score >= 50 {
		t.Fatalf("样本打分 %d 不在 (%d, 50) 区间内，测不出阈值效果", score, DefaultUnifiedMinerOpportunityThreshold)
	}
	if !opp {
		t.Fatalf("打分 %d 在默认阈值 %d 之上却没判成商机", score, DefaultUnifiedMinerOpportunityThreshold)
	}

	SetUnifiedMinerOpportunityThresholdProvider(func() int { return 50 })
	if _, _, opp = DetectUnifiedIntent("微信", nil, nil); opp {
		t.Fatal("阈值抬到 50 后打分 43 应不再算商机")
	}
}

// TestPreviewSampleLimitSeam 逐格钉死 inbox_sales.preview_sample_limit 的读取口。
func TestPreviewSampleLimitSeam(t *testing.T) {
	t.Cleanup(resetPreviewSampleLimitProvider)
	resetPreviewSampleLimitProvider()

	if got := previewSampleLimit(); got != DefaultPreviewSampleLimit {
		t.Fatalf("未注入 = %d, 期望兜底 %d", got, DefaultPreviewSampleLimit)
	}
	SetPreviewSampleLimitProvider(func() int { return 5 })
	if got := previewSampleLimit(); got != 5 {
		t.Fatalf("注入 5 后 = %d", got)
	}
	for _, bad := range []int{0, -3} {
		SetPreviewSampleLimitProvider(func() int { return bad })
		if got := previewSampleLimit(); got != DefaultPreviewSampleLimit {
			t.Fatalf("注入 %d 时 = %d, 期望回落 %d", bad, got, DefaultPreviewSampleLimit)
		}
	}
	// setter 的 nil 语义是「不注入」而非「复位」，所以这里断言它是空操作。
	SetPreviewSampleLimitProvider(func() int { return 5 })
	SetPreviewSampleLimitProvider(nil)
	if got := previewSampleLimit(); got != 5 {
		t.Fatalf("传 nil 后 = %d，期望仍是被注入的 5", got)
	}
}
