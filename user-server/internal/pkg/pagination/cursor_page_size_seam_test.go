package pagination

import "testing"

// resetCursorPageSizeProvider 把读取口复位到「未注入」；理由同其它 seam 测试。
func resetCursorPageSizeProvider() {
	cursorPageSizeProvider = func() int { return DefaultCursorPageSize }
}

// TestCursorPageSizeSeam 逐格钉死 pagination.cursor_page_size 的读取口。
func TestCursorPageSizeSeam(t *testing.T) {
	t.Cleanup(resetCursorPageSizeProvider)
	resetCursorPageSizeProvider()

	if got := CursorPageSize(); got != DefaultCursorPageSize {
		t.Fatalf("未注入 = %d, 期望兜底 %d", got, DefaultCursorPageSize)
	}
	SetCursorPageSizeProvider(func() int { return 250 })
	if got := CursorPageSize(); got != 250 {
		t.Fatalf("注入 250 后 = %d", got)
	}
	// ≤0 会让 ClampLimit 把一切非正数 limit 变成 0 → SQL LIMIT 0 → 列表恒空。
	for _, bad := range []int{0, -10} {
		SetCursorPageSizeProvider(func() int { return bad })
		if got := CursorPageSize(); got != DefaultCursorPageSize {
			t.Fatalf("注入 %d 时 = %d, 期望回落 %d", bad, got, DefaultCursorPageSize)
		}
	}
	// setter 的 nil 语义是「不注入」而非「复位」，所以这里断言它是空操作。
	SetCursorPageSizeProvider(func() int { return 250 })
	SetCursorPageSizeProvider(nil)
	if got := CursorPageSize(); got != 250 {
		t.Fatalf("传 nil 后 = %d，期望仍是被注入的 250", got)
	}
}

// TestClampLimitFollowsCursorPageSizeSeam 证明读取口真进了 ClampLimit / IsValidLimit。
func TestClampLimitFollowsCursorPageSizeSeam(t *testing.T) {
	t.Cleanup(resetCursorPageSizeProvider)

	SetCursorPageSizeProvider(func() int { return 50 })

	if got := ClampLimit(0); got != 50 {
		t.Fatalf("ClampLimit(0) = %d, 期望 50", got)
	}
	if got := ClampLimit(80); got != 50 {
		t.Fatalf("ClampLimit(80) = %d, 期望被截到 50", got)
	}
	if got := ClampLimit(20); got != 20 {
		t.Fatalf("ClampLimit(20) = %d, 期望原样放行", got)
	}
	if IsValidLimit(60) {
		t.Fatal("上限改成 50 后 60 应判为非法")
	}
	if !IsValidLimit(50) {
		t.Fatal("50 应是合法上界")
	}
}
