package trace_learning

import "testing"

// resetInsightMaxLenProvider 把读取口复位到「未注入」；理由同其它 seam 测试。
func resetInsightMaxLenProvider() {
	insightMaxLenProvider = func() int { return DefaultInsightMaxLen }
}

// TestInsightMaxLenSeam 逐格钉死 inbox_sales.geo_lead_preview_max_len 的读取口。
// seam 本就存在，这里补的是非正值守卫：截断长度 ≤0 会把每条洞察都截成空串。
func TestInsightMaxLenSeam(t *testing.T) {
	t.Cleanup(resetInsightMaxLenProvider)
	resetInsightMaxLenProvider()

	if got := InsightMaxLen(); got != DefaultInsightMaxLen {
		t.Fatalf("未注入 = %d, 期望兜底 %d", got, DefaultInsightMaxLen)
	}
	SetInsightMaxLenProvider(func() int { return 40 })
	if got := InsightMaxLen(); got != 40 {
		t.Fatalf("注入 40 后 = %d", got)
	}
	for _, bad := range []int{0, -1} {
		SetInsightMaxLenProvider(func() int { return bad })
		if got := InsightMaxLen(); got != DefaultInsightMaxLen {
			t.Fatalf("注入 %d 时 = %d, 期望回落 %d", bad, got, DefaultInsightMaxLen)
		}
	}
	// setter 的 nil 语义是「不注入」而非「复位」，所以这里断言它是空操作。
	SetInsightMaxLenProvider(func() int { return 40 })
	SetInsightMaxLenProvider(nil)
	if got := InsightMaxLen(); got != 40 {
		t.Fatalf("传 nil 后 = %d，期望仍是被注入的 40", got)
	}
}
