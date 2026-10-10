package confidence

import (
	"testing"

	"hivemtk-user/internal/dto"
)

// TestVetoLowRAGThresholdSeam 钉住 confidence.veto_low_rag_threshold 接线后的边界行为。
//
// 非法值回落这条格外重要：阈值取到 0 会让「RAGQual < 0」恒成立，
// 也就是每一条消息都触发一票否决 → 全部转人工。这是把服务打瘫的形状，
// 不是「稍微偏了一点」。
func TestVetoLowRAGThresholdSeam(t *testing.T) {
	restore := setVetoProviderForTest(nil)
	defer restore()

	if got := vetoLowRAGThreshold(); got != DefaultVetoLowRAGThreshold {
		t.Errorf("未注入时应为兜底 %v，got %v", DefaultVetoLowRAGThreshold, got)
	}

	SetVetoLowRAGThresholdProvider(func() float64 { return 0.42 })
	if got := vetoLowRAGThreshold(); got != 0.42 {
		t.Errorf("注入后应为 0.42，got %v", got)
	}

	for _, bad := range []float64{0, -0.5, 1.2} {
		SetVetoLowRAGThresholdProvider(func() float64 { return bad })
		if got := vetoLowRAGThreshold(); got != DefaultVetoLowRAGThreshold {
			t.Errorf("注入非法值 %v 时应回落 %v，got %v", bad, DefaultVetoLowRAGThreshold, got)
		}
	}

	SetVetoLowRAGThresholdProvider(func() float64 { return 1.0 })
	if got := vetoLowRAGThreshold(); got != 1.0 {
		t.Errorf("1.0 是合法上界，应被接受，got %v", got)
	}
}

// TestVetoLowRAGCheckUsesSeam 用例要证明「读点真的走了 seam」，而不是只证明
// provider 被 setter 收下：直接构造一条 RAGQual 低于阈值的信号，看否决是否触发。
func TestVetoLowRAGCheckUsesSeam(t *testing.T) {
	restore := setVetoProviderForTest(func() float64 { return 0.9 })
	defer restore()

	// RAGQual=0.5：低于注入的 0.9 → 触发否决
	r := &VetoLowRAG{}
	signals := newFiveSignals(0.5)
	triggered, reason := r.Check(signals, nil)
	if !triggered || reason != "veto_low_rag" {
		t.Errorf("阈值 0.9 时 RAGQual=0.5 应否决，got triggered=%v reason=%q", triggered, reason)
	}

	// 换成 0.2：0.5 高于它 → 不否决。阈值改小必须真的改变行为。
	SetVetoLowRAGThresholdProvider(func() float64 { return 0.2 })
	triggered, reason = r.Check(signals, nil)
	if triggered {
		t.Errorf("阈值 0.2 时 RAGQual=0.5 不应否决，got reason=%q", reason)
	}
}

// TestVetoLowRAGExplicitThresholdStillWins 显式设置在规则实例上的 Threshold
// 仍应优先于参数中心的全局默认值——这是既有语义，接线不能把它改掉。
//
// 判读方向：RAGQual 低于阈值才否决。所以实例阈值 0.8 时 RAGQual=0.5 会被否决，
// 实例阈值 0.2 时同一信号放行。两者结论相反才说明"实例阈值真的盖住了全局默认"。
func TestVetoLowRAGExplicitThresholdStillWins(t *testing.T) {
	restore := setVetoProviderForTest(func() float64 { return 0.05 })
	defer restore()

	// 实例 0.8（比全局默认 0.05 严）：RAGQual=0.5 低于它 → 否决
	r := &VetoLowRAG{Threshold: 0.8}
	if triggered, reason := r.Check(newFiveSignals(0.5), nil); !triggered {
		t.Error("实例阈值 0.8 时 RAGQual=0.5 应否决（实际未否决）；若改用全局默认 0.05 则会放行")
	} else if reason != "veto_low_rag" {
		t.Errorf("否决原因为 %q，期望 veto_low_rag", reason)
	}

	// 实例 0.2（比全局默认 0.05 松）：同一信号高于它 → 放行。
	// 这一格是本用例的关键：若接线改成无视实例阈值、永远用全局默认，
	// 0.5 > 0.05 也会放行，两格结论就一样了，测试就白写。
	r = &VetoLowRAG{Threshold: 0.2}
	if triggered, reason := r.Check(newFiveSignals(0.5), nil); triggered {
		t.Errorf("实例阈值 0.2 时 RAGQual=0.5 不应否决，got reason=%q", reason)
	}
}

func setVetoProviderForTest(fn func() float64) func() {
	old := vetoLowRAGThresholdProvider
	SetVetoLowRAGThresholdProvider(fn)
	if fn == nil {
		vetoLowRAGThresholdProvider = func() float64 { return DefaultVetoLowRAGThreshold }
	}
	return func() { vetoLowRAGThresholdProvider = old }
}

func newFiveSignals(ragQual float64) *dto.FiveSignals {
	return &dto.FiveSignals{RAGQual: ragQual}
}
