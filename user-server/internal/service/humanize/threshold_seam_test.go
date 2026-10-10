package humanize

import (
	"context"
	"testing"
)

// TestEffectiveThresholdSeam 钉住 confidence.humanize_default_threshold 接线后的边界行为。
//
// 阈值取 0 是这里最危险的非法值：判定是「分数 >= 阈值即达标」，
// 阈值 0 会让任何文本都判达标，拟人度评估器彻底失效且没有任何报错。
func TestEffectiveThresholdSeam(t *testing.T) {
	restore := setThresholdProviderForTest(nil)
	defer restore()

	if got := EffectiveThreshold(); got != DefaultThreshold {
		t.Errorf("未注入时应为兜底 %v，got %v", DefaultThreshold, got)
	}

	SetDefaultThresholdProvider(func() float64 { return 0.6 })
	if got := EffectiveThreshold(); got != 0.6 {
		t.Errorf("注入后应为 0.6，got %v", got)
	}

	for _, bad := range []float64{0, -0.2, 1.0001} {
		SetDefaultThresholdProvider(func() float64 { return bad })
		if got := EffectiveThreshold(); got != DefaultThreshold {
			t.Errorf("注入非法值 %v 时应回落 %v，got %v", bad, DefaultThreshold, got)
		}
	}

	SetDefaultThresholdProvider(func() float64 { return 1.0 })
	if got := EffectiveThreshold(); got != 1.0 {
		t.Errorf("1.0 是合法上界，应被接受，got %v", got)
	}
}

// TestNewServicePicksUpSeam 构造器必须读 seam 而不是编译期常量。
//
// 单独钉这一格是因为它最容易退化：改 EffectiveThreshold() 的调用点时
// 很容易只改了 WithThreshold 之后的路径，忘了构造器，于是新装的实例
// 仍然是 0.85，而运维以为改成功了。
func TestNewServicePicksUpSeam(t *testing.T) {
	restore := setThresholdProviderForTest(func() float64 { return 0.55 })
	defer restore()

	svc := NewHumanizeEvalService(nil, nil, nil, nil, nil)
	if got := svc.Threshold(); got != 0.55 {
		t.Errorf("新建服务的阈值 = %v，期望 0.55（构造器没走 seam）", got)
	}
}

// TestWithThresholdStillOverrides 显式的 WithThreshold 必须仍然压过参数中心。
// 这是既有的运维接口（env MTK_HUMANIZE_EVAL_THRESHOLD 走的就是它），
// 接线不能把它顶掉。
func TestWithThresholdStillOverrides(t *testing.T) {
	restore := setThresholdProviderForTest(func() float64 { return 0.55 })
	defer restore()

	svc := NewHumanizeEvalService(nil, nil, nil, nil, nil).
		WithThreshold(context.Background(), 0.77)
	if got := svc.Threshold(); got != 0.77 {
		t.Errorf("WithThreshold 之后阈值 = %v，期望 0.77", got)
	}
}

func setThresholdProviderForTest(fn func() float64) func() {
	old := thresholdProvider
	SetDefaultThresholdProvider(fn)
	if fn == nil {
		thresholdProvider = func() float64 { return DefaultThreshold }
	}
	return func() { thresholdProvider = old }
}
