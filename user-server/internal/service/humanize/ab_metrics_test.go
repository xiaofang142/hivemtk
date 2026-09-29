package humanize

import (
	"sync"
	"testing"
	"time"
)

// TestABRecorder_AssignBucket_Deterministic 验证分桶稳定性
//
// 业界 A/B 测试核心要求：同一 customerID 始终分到同组
func TestABRecorder_AssignBucket_Deterministic(t *testing.T) {
	r := NewABRecorder(50)
	for i := 0; i < 10; i++ {
		id := "customer-123"
		g1 := r.AssignBucket(id)
		g2 := r.AssignBucket(id)
		if g1 != g2 {
			t.Errorf("bucket must be stable for same ID: %s vs %s", g1, g2)
		}
	}
}

// TestABRecorder_AssignBucket_EmptyID 验证空 ID 进 control
func TestABRecorder_AssignBucket_EmptyID(t *testing.T) {
	r := NewABRecorder(50)
	if g := r.AssignBucket(""); g != ABGroupControl {
		t.Errorf("empty ID should go to control, got %s", g)
	}
}

// TestABRecorder_AssignBucket_Distribution 验证分桶分布
func TestABRecorder_AssignBucket_Distribution(t *testing.T) {
	r := NewABRecorder(50)
	control, treatment := 0, 0
	for i := 0; i < 1000; i++ {
		id := "c-" + itoa(i)
		if r.AssignBucket(id) == ABGroupControl {
			control++
		} else {
			treatment++
		}
	}

	if control < 400 || control > 600 {
		t.Errorf("expected ~500 control, got %d", control)
	}
	if treatment < 400 || treatment > 600 {
		t.Errorf("expected ~500 treatment, got %d", treatment)
	}
}

// TestABRecorder_AssignBucket_TrafficSplit 验证不同 split 比例
func TestABRecorder_AssignBucket_TrafficSplit(t *testing.T) {
	r := NewABRecorder(20)
	control, treatment := 0, 0
	for i := 0; i < 1000; i++ {
		if r.AssignBucket("c-"+itoa(i)) == ABGroupControl {
			control++
		} else {
			treatment++
		}
	}

	if control < 150 || control > 250 {
		t.Errorf("expected ~200 control for 20%% split, got %d", control)
	}
}

// TestABRecorder_RecordScore 验证分数记录与均值计算
func TestABRecorder_RecordScore(t *testing.T) {
	r := NewABRecorder(50)
	r.RecordScore(ABGroupControl, 0.5, 1000)
	r.RecordScore(ABGroupControl, 0.7, 2000)
	ctrl, _ := r.Snapshot()
	if ctrl.SampleCount != 2 {
		t.Errorf("expected 2 samples, got %d", ctrl.SampleCount)
	}
	if ctrl.MeanScore != 0.6 {
		t.Errorf("expected mean 0.6, got %v", ctrl.MeanScore)
	}
	if ctrl.SumFirstReplyMs != 3000 {
		t.Errorf("expected sum 3000, got %d", ctrl.SumFirstReplyMs)
	}
}

// TestABRecorder_RecordOutcome 验证 outcome 计数
func TestABRecorder_RecordOutcome(t *testing.T) {
	r := NewABRecorder(50)
	r.RecordScore(ABGroupTreatment, 0.8, 1500)
	r.RecordOutcome(ABGroupTreatment, "conversion")
	r.RecordOutcome(ABGroupTreatment, "conversion")
	r.RecordOutcome(ABGroupTreatment, "churn")
	r.RecordOutcome(ABGroupTreatment, "negative_reply")

	_, trt := r.Snapshot()
	if trt.ConversionCount != 2 {
		t.Errorf("expected 2 conversions, got %d", trt.ConversionCount)
	}
	if trt.ChurnCount != 1 {
		t.Errorf("expected 1 churn, got %d", trt.ChurnCount)
	}
	if trt.NegativeReplies != 1 {
		t.Errorf("expected 1 negative reply, got %d", trt.NegativeReplies)
	}
}

// TestABRecorder_ConcurrentSafety 验证并发安全
func TestABRecorder_ConcurrentSafety(t *testing.T) {
	r := NewABRecorder(50)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			r.RecordScore(ABGroupControl, float64(i%10)/10, int64(i*100))
		}(i)
		go func(i int) {
			defer wg.Done()
			r.RecordOutcome(ABGroupControl, "conversion")
		}(i)
	}
	wg.Wait()
	ctrl, _ := r.Snapshot()
	if ctrl.SampleCount != 100 {
		t.Errorf("expected 100 samples, got %d", ctrl.SampleCount)
	}
	if ctrl.ConversionCount != 100 {
		t.Errorf("expected 100 conversions, got %d", ctrl.ConversionCount)
	}
}

// TestABRecorder_Compare_TreatmentWins 验证 treatment 显著胜出
func TestABRecorder_Compare_TreatmentWins(t *testing.T) {
	r := NewABRecorder(50)

	for i := 0; i < 1000; i++ {
		r.RecordScore(ABGroupControl, 0.5, 1000)
		if i < 50 {
			r.RecordOutcome(ABGroupControl, "conversion")
		}
		if i < 5 {
			r.RecordOutcome(ABGroupControl, "churn")
		}
	}

	for i := 0; i < 1000; i++ {
		r.RecordScore(ABGroupTreatment, 0.7, 1500)
		if i < 120 {
			r.RecordOutcome(ABGroupTreatment, "conversion")
		}
		if i < 8 {
			r.RecordOutcome(ABGroupTreatment, "churn")
		}
	}
	comp := r.Compare()
	if comp.Winner != ABGroupTreatment {
		t.Errorf("treatment should win, got %s", comp.Winner)
	}
	if comp.ConversionDelta < 0.05 {
		t.Errorf("expected conversion delta > 0.05, got %v", comp.ConversionDelta)
	}
	if !comp.MinSampleReached {
		t.Error("min sample should be reached")
	}
}

// TestABRecorder_Compare_ControlWins 验证 control 显著胜出
func TestABRecorder_Compare_ControlWins(t *testing.T) {
	r := NewABRecorder(50)
	for i := 0; i < 1000; i++ {
		r.RecordScore(ABGroupControl, 0.5, 1000)
		if i < 200 {
			r.RecordOutcome(ABGroupControl, "conversion")
		}
	}
	for i := 0; i < 1000; i++ {
		r.RecordScore(ABGroupTreatment, 0.4, 2000)
		if i < 80 {
			r.RecordOutcome(ABGroupTreatment, "conversion")
		}
	}
	comp := r.Compare()
	if comp.Winner != ABGroupControl {
		t.Errorf("control should win, got %s", comp.Winner)
	}
}

// TestABRecorder_Compare_InconclusiveSmallSample 验证小样本 inconclusive
func TestABRecorder_Compare_InconclusiveSmallSample(t *testing.T) {
	r := NewABRecorder(50)
	for i := 0; i < 10; i++ {
		r.RecordScore(ABGroupControl, 0.5, 1000)
	}
	for i := 0; i < 10; i++ {
		r.RecordScore(ABGroupTreatment, 0.7, 1000)
	}
	comp := r.Compare()
	if comp.Winner != "inconclusive" {
		t.Errorf("small sample should be inconclusive, got %s", comp.Winner)
	}
	if comp.MinSampleReached {
		t.Error("min sample should NOT be reached")
	}
}

// TestABRecorder_Compare_InconclusiveNoDifference 验证无显著差异
func TestABRecorder_Compare_InconclusiveNoDifference(t *testing.T) {
	r := NewABRecorder(50)

	for i := 0; i < 500; i++ {
		r.RecordScore(ABGroupControl, 0.5, 1000)
		if i < 50 {
			r.RecordOutcome(ABGroupControl, "conversion")
		}
	}
	for i := 0; i < 500; i++ {
		r.RecordScore(ABGroupTreatment, 0.5, 1000)
		if i < 50 {
			r.RecordOutcome(ABGroupTreatment, "conversion")
		}
	}
	comp := r.Compare()
	if comp.Winner != "inconclusive" {
		t.Errorf("similar conversion should be inconclusive, got %s", comp.Winner)
	}
}

// TestABRecorder_RatesCalculation 验证各种率计算
func TestABRecorder_RatesCalculation(t *testing.T) {
	m := &HumanizeABMetrics{
		SampleCount:     100,
		ConversionCount: 10,
		ChurnCount:      5,
		NegativeReplies: 3,
		SumFirstReplyMs: 5000,
	}
	if m.ConversionRate() != 0.1 {
		t.Errorf("conversion rate should be 0.1, got %v", m.ConversionRate())
	}
	if m.ChurnRate() != 0.05 {
		t.Errorf("churn rate should be 0.05, got %v", m.ChurnRate())
	}
	if m.NegativeReplyRate() != 0.03 {
		t.Errorf("negative reply rate should be 0.03, got %v", m.NegativeReplyRate())
	}
	if m.AvgFirstReplyMsSec() != 50 {
		t.Errorf("avg first reply should be 50ms, got %v", m.AvgFirstReplyMsSec())
	}
}

// TestABRecorder_RatesEmptyMetrics 验证空指标返回 0
func TestABRecorder_RatesEmptyMetrics(t *testing.T) {
	m := &HumanizeABMetrics{}
	if m.ConversionRate() != 0 {
		t.Error("empty metrics should yield 0 rates")
	}
	if m.ChurnRate() != 0 {
		t.Error("empty metrics should yield 0 churn rate")
	}
}

// TestABRecorder_DefaultTrafficSplit 验证非法 split 归一化
func TestABRecorder_DefaultTrafficSplit(t *testing.T) {
	r := NewABRecorder(0)
	if r.traffic != 50 {
		t.Errorf("traffic=0 should normalize to 50, got %d", r.traffic)
	}
	r = NewABRecorder(150)
	if r.traffic != 50 {
		t.Errorf("traffic=150 should normalize to 50, got %d", r.traffic)
	}
}

// TestTracker_ElapsedMs 验证耗时跟踪
func TestTracker_ElapsedMs(t *testing.T) {
	tr := NewTracker()

	if tr.ElapsedMs() < 0 {
		t.Error("elapsed should be non-negative")
	}

}

// TestABRecorder_PersistHook 验证持久化钩子
//
// 注意：RecordScore 同步记录 score + first_reply 两个指标，异步触发 hook
// 用 channels 收集所有 hook 调用，验证至少包含预期的指标名
func TestABRecorder_PersistHook(t *testing.T) {
	r := NewABRecorder(50)
	type call struct {
		testID, group, metric, customerID string
		value                             float64
	}
	calls := make(chan call, 10)
	r.SetPersistHook(func(testID, group, metricName, customerID string, value float64) {
		calls <- call{testID, group, metricName, customerID, value}
	})

	r.RecordScore(ABGroupControl, 0.85, 1000, "c1")
	r.RecordOutcome(ABGroupTreatment, "conversion", "c2")

	gotCalls := make([]call, 0, 5)
	timeout := time.After(2 * time.Second)
collect:
	for len(gotCalls) < 3 {
		select {
		case c := <-calls:
			gotCalls = append(gotCalls, c)
		case <-timeout:
			// 必须跳出外层 for：只 break select 的话 timeout 已关，会空转死循环
			break collect
		}
	}
	if len(gotCalls) < 3 {
		t.Fatalf("expected at least 3 hook calls, got %d", len(gotCalls))
	}

	for _, c := range gotCalls {
		if c.testID != "humanize_ab" {
			t.Errorf("testID should be humanize_ab, got %s", c.testID)
		}
	}

	seenMetrics := make(map[string]bool)
	for _, c := range gotCalls {
		seenMetrics[c.metric] = true
	}
	for _, expected := range []string{"humanize_score", "first_reply_ms", "conversion"} {
		if !seenMetrics[expected] {
			t.Errorf("expected metric %s to be in hook calls, got metrics: %v", expected, seenMetrics)
		}
	}

	for _, c := range gotCalls {
		if c.metric == "humanize_score" && c.value != 0.85 {
			t.Errorf("humanize_score should be 0.85, got %v", c.value)
		}
		if c.metric == "first_reply_ms" && c.value != 1000 {
			t.Errorf("first_reply_ms should be 1000, got %v", c.value)
		}
		if c.metric == "conversion" && c.value != 1.0 {
			t.Errorf("conversion should be 1.0, got %v", c.value)
		}
	}
}

// TestABRecorder_PersistHook_NilSafe 验证 nil hook 不破坏
func TestABRecorder_PersistHook_NilSafe(t *testing.T) {
	r := NewABRecorder(50)

	r.RecordScore(ABGroupControl, 0.85, 1000, "c1")
	r.RecordOutcome(ABGroupControl, "conversion", "c1")

	ctrl, _ := r.Snapshot()
	if ctrl.SampleCount != 1 {
		t.Errorf("in-memory state should be intact, got %d samples", ctrl.SampleCount)
	}
}

// TestABRecorder_PersistHook_ReentrantSafety 验证 set hook 时的并发安全
func TestABRecorder_PersistHook_ReentrantSafety(t *testing.T) {
	r := NewABRecorder(50)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.SetPersistHook(func(_, _, _, _ string, _ float64) {})
		}()
		go func() {
			defer wg.Done()
			r.RecordScore(ABGroupControl, 0.5, 100, "c1")
		}()
	}
	wg.Wait()

}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}
