package humanize

import "testing"

// resetP1Providers 复位四个 provider。
//
// 恢复的是「返回兜底常量的函数」，不是 nil : 包级变量的初始值就是这种函数，
// 置 nil 会让后面的读取直接空指针 panic（本文件首版就这样炸过）。
// 也不能调 SetXxxProvider(nil) —— setter 的 nil 语义是「不注入」
// （`if fn != nil` 守卫，防装配顺序错时把兜底值顶掉），不是「复位」。
// 按直觉写「传 nil 后回到兜底」的用例会红，而且红得有理有据。
func resetP1Providers(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		boundaryLowProvider = func() float64 { return DefaultBoundaryLow }
		boundaryHighProvider = func() float64 { return DefaultBoundaryHigh }
		sampleRateProvider = func() float64 { return DefaultSampleRate }
		maxRetryProvider = func() int { return DefaultMaxRetry }
	})
}

func TestP1BoundaryAndSampleRateSeams(t *testing.T) {
	resetP1Providers(t)
	// 三个 float 参数的合法区间并不相同，必须逐条声明，不能共用一套坏值：
	//   boundary_low  合法 [0,1)  —— 0 是「所有样本都算边缘」的合法语义
	//   boundary_high 合法 (0,1] —— 0 会让「边缘区间」这个概念消失
	//   sample_rate   合法 [0,1]  —— 0 是「一个都不送 LLM」的合法语义
	cases := []struct {
		name     string
		fallback float64
		read     func() float64
		inject   func(func() float64)
		invalid  []float64
	}{
		{"boundary_low", DefaultBoundaryLow, EffectiveBoundaryLow, SetBoundaryLowProvider,
			[]float64{-0.5, -3, 1.0, 1.5, 99}},
		{"boundary_high", DefaultBoundaryHigh, EffectiveBoundaryHigh, SetBoundaryHighProvider,
			[]float64{0, -0.5, -3, 1.5, 99}},
		{"sample_rate", DefaultSampleRate, EffectiveSampleRate, SetSampleRateProvider,
			[]float64{-0.5, -3, 1.5, 99}},
	}

	// 未注入时必须精确等于兜底常量——这是「改造前行为逐字节一致」那一格。
	for _, c := range cases {
		if got := c.read(); got != c.fallback {
			t.Errorf("%s: 未注入得到 %v, 期望兜底常量 %v", c.name, got, c.fallback)
		}
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			v := 0.42
			c.inject(func() float64 { return v })
			if got := c.read(); got != 0.42 {
				t.Errorf("注入 0.42 后得到 %v", got)
			}
			// 越界值一律回落兜底，而不是把 0/负数/-1 透传给调用方。
			for _, bad := range c.invalid {
				v = bad
				got := c.read()
				if got == bad {
					t.Errorf("%v 被原样透传了（应回落兜底 %v）", bad, c.fallback)
					continue
				}
				if got != c.fallback {
					t.Errorf("注入 %v 得到 %v, 期望兜底 %v", bad, got, c.fallback)
				}
			}
			// 1.0 是合法上界（boundary_high 的语义就是"上界含边缘终点"，
			// sample_rate 的 1.0 是"全部送 LLM"）；boundary_low 的上界不含 1。
			v = 1.0
			if got := c.read(); c.name == "boundary_low" {
				if got != c.fallback {
					t.Errorf("boundary_low 的 1.0 应回落 %v, 得到 %v", c.fallback, got)
				}
			} else if got != 1.0 {
				t.Errorf("1.0 是合法上界，得到 %v", got)
			}
			// setter 传 nil 是空操作，不是复位。
			v = 0.77
			c.inject(func() float64 { return v })
			if got := c.read(); got != 0.77 {
				t.Fatalf("前置条件不成立：注入 0.77 后应为 0.77, 实得 %v", got)
			}
			// 0 对三条各自的合法性：只有 boundary_high 的 0 是非法的。
			switch c.name {
			case "boundary_low", "sample_rate":
				v = 0
				if got := c.read(); got != 0 {
					t.Errorf("%s 的 0 是合法值，得到 %v", c.name, got)
				}
			case "boundary_high":
				v = 0
				if got := c.read(); got != c.fallback {
					t.Errorf("boundary_high 的 0 应回落 %v, 得到 %v", c.fallback, got)
				}
			}
		})
	}
}

func TestSampleRateZeroIsLegal(t *testing.T) {
	resetP1Providers(t)
	// sample_rate 的 0 是合法值（"一个都不送 LLM"），这一点与另外两个 float 参数不同：
	// 那两个的 0 会让"边缘样本"这个概念消失，是行为错误。
	SetSampleRateProvider(func() float64 { return 0 })
	if got := EffectiveSampleRate(); got != 0 {
		t.Errorf("sample_rate=0 应被原样接受，得到 %v", got)
	}
	// 负值仍然回落。
	SetSampleRateProvider(func() float64 { return -0.1 })
	if got := EffectiveSampleRate(); got != DefaultSampleRate {
		t.Errorf("sample_rate=-0.1 应回落 %v, 得到 %v", DefaultSampleRate, got)
	}
}

func TestP1MaxRetrySeam(t *testing.T) {
	resetP1Providers(t)
	if got := EffectiveMaxRetry(); got != DefaultMaxRetry {
		t.Errorf("未注入得到 %d, 期望兜底 %d", got, DefaultMaxRetry)
	}
	SetMaxRetryProvider(func() int { return 7 })
	if got := EffectiveMaxRetry(); got != 7 {
		t.Errorf("注入 7 后得到 %d", got)
	}
	// 0 与负数回落：调度侧按次数预算 LLM 调用，静默变 0 会让预算模型与真实行为脱节。
	for _, bad := range []int{0, -1, -100} {
		n := bad
		SetMaxRetryProvider(func() int { return n })
		if got := EffectiveMaxRetry(); got != DefaultMaxRetry {
			t.Errorf("注入 %d 得到 %d, 期望兜底 %d", bad, got, DefaultMaxRetry)
		}
	}
}

// TestNewHumanizeEvalServicePicksUpSeams 钉住构造函数走 seam——这是最容易退化的一格：
// 四个字段都在 NewHumanizeEvalService 里赋值，哪天有人把 EffectiveXxx() 改回常量，
// 上层无论如何注入都不生效，而 seam 单测仍是绿的。
func TestNewHumanizeEvalServicePicksUpSeams(t *testing.T) {
	resetP1Providers(t)
	SetBoundaryLowProvider(func() float64 { return 0.3 })
	SetBoundaryHighProvider(func() float64 { return 0.6 })
	SetSampleRateProvider(func() float64 { return 0.2 })
	SetMaxRetryProvider(func() int { return 5 })

	svc := NewHumanizeEvalService(nil, nil, nil, nil, nil)
	if svc.boundaryLow != 0.3 {
		t.Errorf("boundaryLow = %v, 期望 0.3", svc.boundaryLow)
	}
	if svc.boundaryHigh != 0.6 {
		t.Errorf("boundaryHigh = %v, 期望 0.6", svc.boundaryHigh)
	}
	if svc.sampleRate != 0.2 {
		t.Errorf("sampleRate = %v, 期望 0.2", svc.sampleRate)
	}
	if svc.maxRetry != 5 {
		t.Errorf("maxRetry = %d, 期望 5", svc.maxRetry)
	}
}

// TestExplicitSettersStillWinOverSeams 显式调用 WithXxx 必须仍然赢过参数中心。
// 否则参数化会反过来破坏现有的单实例定制能力。
func TestExplicitSettersStillWinOverSeams(t *testing.T) {
	resetP1Providers(t)
	SetMaxRetryProvider(func() int { return 2 })
	SetSampleRateProvider(func() float64 { return 0.9 })

	svc := NewHumanizeEvalService(nil, nil, nil, nil, nil)
	svc.WithMaxRetry(nil, 9).WithSampleRate(nil, 0.05)
	if svc.maxRetry != 9 {
		t.Errorf("WithMaxRetry(9) 后 maxRetry = %d", svc.maxRetry)
	}
	if svc.sampleRate != 0.05 {
		t.Errorf("WithSampleRate(0.05) 后 sampleRate = %v", svc.sampleRate)
	}
}

// TestDefaultConstantsAreWhatSeedsClaim 兜底常量必须与种子里的 DefaultValue 一致。
// 种子默认值会被 ResetToDefault / BulkResetGroup 写回库里，两边不一致等于
// 「点一下重置，系统行为就变」。这里只钉常量本身的具体值（值放在这一处，
// 与种子的比对放在 app 包的装配测试里，那边能看到 DefaultParamDefs）。
func TestDefaultConstantsAreWhatSeedsClaim(t *testing.T) {
	if DefaultBoundaryLow != 0.70 {
		t.Errorf("DefaultBoundaryLow = %v, 期望 0.70（种子 confidence.humanize_boundary_low）", DefaultBoundaryLow)
	}
	if DefaultBoundaryHigh != 0.85 {
		t.Errorf("DefaultBoundaryHigh = %v, 期望 0.85（种子 confidence.humanize_boundary_high）", DefaultBoundaryHigh)
	}
	if DefaultSampleRate != 0.10 {
		t.Errorf("DefaultSampleRate = %v, 期望 0.10（种子 confidence.humanize_sample_rate）", DefaultSampleRate)
	}
	if DefaultMaxRetry != 3 {
		t.Errorf("DefaultMaxRetry = %d, 期望 3（种子 confidence.humanize_max_retry）", DefaultMaxRetry)
	}
}
