package service

import (
	"testing"
	"time"
)

// 这批用例钉的是 confidence 组 5 条接线的**边界行为**：
// 参数被改成非法值时必须回落兜底，而不是把非法值灌进判定逻辑。
// 装配层测试（app/confidence_params_wiring_test.go）证明的是「改了真生效」，
// 证明不了「改坏了会怎样」——那是这两组用例的职责。

// floatSeam 是 4 个 0-1 区间的阈值共用的表。
func TestConfidenceFloatSeams(t *testing.T) {
	t.Run("FewShotMinCos", func(t *testing.T) {
		restore := SetFewShotMinCosProviderForTest(nil)
		defer restore()

		if got := fewShotMinCos(); got != FewShotMinCosDefault {
			t.Errorf("未注入时应为兜底 %v，got %v", FewShotMinCosDefault, got)
		}
		SetFewShotMinCosProviderForTest(func() float64 { return 0.92 })
		if got := fewShotMinCos(); got != 0.92 {
			t.Errorf("注入后应为 0.92，got %v", got)
		}
		for _, bad := range []float64{0, -0.1, 1.5} {
			SetFewShotMinCosProviderForTest(func() float64 { return bad })
			if got := fewShotMinCos(); got != FewShotMinCosDefault {
				t.Errorf("注入非法值 %v 时应回落 %v，got %v", bad, FewShotMinCosDefault, got)
			}
		}
		SetFewShotMinCosProviderForTest(func() float64 { return 1.0 })
		if got := fewShotMinCos(); got != 1.0 {
			t.Errorf("1.0 是合法上界，应被接受，got %v", got)
		}
	})

	t.Run("WeakTruthMinConfidence", func(t *testing.T) {
		restore := SetWeakTruthMinConfidenceProviderForTest(nil)
		defer restore()

		if got := weakTruthMinConfidence(); got != WeakTruthMinConfidence {
			t.Errorf("未注入时应为兜底 %v，got %v", WeakTruthMinConfidence, got)
		}
		SetWeakTruthMinConfidenceProviderForTest(func() float64 { return 0.55 })
		if got := weakTruthMinConfidence(); got != 0.55 {
			t.Errorf("注入后应为 0.55，got %v", got)
		}
		for _, bad := range []float64{0, -1, 2} {
			SetWeakTruthMinConfidenceProviderForTest(func() float64 { return bad })
			if got := weakTruthMinConfidence(); got != WeakTruthMinConfidence {
				t.Errorf("注入非法值 %v 时应回落 %v，got %v", bad, WeakTruthMinConfidence, got)
			}
		}
	})

	t.Run("EmbRetryCooldown", func(t *testing.T) {
		restore := SetEmbRetryCooldownProviderForTest(nil)
		defer restore()

		if got := embRetryCooldown(); got != EmbRetryCooldownDefault {
			t.Errorf("未注入时应为兜底 %v，got %v", EmbRetryCooldownDefault, got)
		}
		SetEmbRetryCooldownProviderForTest(func() time.Duration { return 15 * time.Minute })
		if got := embRetryCooldown(); got != 15*time.Minute {
			t.Errorf("注入后应为 15m，got %v", got)
		}
		for _, bad := range []time.Duration{0, -time.Second} {
			SetEmbRetryCooldownProviderForTest(func() time.Duration { return bad })
			if got := embRetryCooldown(); got != EmbRetryCooldownDefault {
				t.Errorf("注入非法值 %v 时应回落 %v，got %v", bad, EmbRetryCooldownDefault, got)
			}
		}
	})
}

// SetXxxProviderForTest 只在本文件可见：测试与实现同包，不需要导出到公共 API。
// 语义是「临时把 provider 换掉，返回还原函数」，传 nil 表示"回到未注入状态"
// 而不是"把 provider 置空"——后者会让首次读取直接 panic。
func SetFewShotMinCosProviderForTest(fn func() float64) func() {
	old := fewShotMinCosProvider
	SetFewShotMinCosProvider(fn)
	if fn == nil {
		fewShotMinCosProvider = func() float64 { return FewShotMinCosDefault }
	}
	return func() { fewShotMinCosProvider = old }
}

func SetWeakTruthMinConfidenceProviderForTest(fn func() float64) func() {
	old := weakTruthMinConfidenceProvider
	SetWeakTruthMinConfidenceProvider(fn)
	if fn == nil {
		weakTruthMinConfidenceProvider = func() float64 { return WeakTruthMinConfidence }
	}
	return func() { weakTruthMinConfidenceProvider = old }
}

func SetEmbRetryCooldownProviderForTest(fn func() time.Duration) func() {
	old := embRetryCooldownProvider
	SetEmbRetryCooldownProvider(fn)
	if fn == nil {
		embRetryCooldownProvider = func() time.Duration { return EmbRetryCooldownDefault }
	}
	return func() { embRetryCooldownProvider = old }
}
