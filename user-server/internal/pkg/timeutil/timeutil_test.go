package timeutil

import (
	"testing"
	"time"
)

// TestStartOfDay_IsLocalNotUTC 钉死「本地零点 ≠ UTC 零点」这个不变式。
//
// 反向验证：把 StartOfDay 换回 `t.Truncate(24*time.Hour)`，本用例立即失败
// （CST 08:58 时 Truncate 给出 08:00 CST，而正确值是 00:00 CST）。
func TestStartOfDay_IsLocalNotUTC(t *testing.T) {
	cst := time.FixedZone("CST", 8*60*60)
	// 2026-09-16 08:58:32 CST == 2026-09-16 00:58:32 UTC
	in := time.Date(2026, 9, 16, 8, 58, 32, 0, cst)

	got := StartOfDay(in)
	want := time.Date(2026, 9, 16, 0, 0, 0, 0, cst)
	if !got.Equal(want) {
		t.Fatalf("StartOfDay = %v, want %v", got, want)
	}

	// 与错误写法对照：Truncate 会给出 UTC 零点（本地 08:00），必须不相等
	wrong := in.Truncate(24 * time.Hour)
	if got.Equal(wrong) {
		t.Fatalf("StartOfDay 退化成了 Truncate(24h) 的 UTC 零点语义：%v", got)
	}
	if !wrong.Equal(want.Add(8 * time.Hour)) {
		t.Fatalf("Truncate(24h) 的对照值不符合预期：%v", wrong)
	}
}

// TestStartOfDay_UTCIsUnaffected 在 UTC 下两种写法结果一致，
// 故缺陷只在非 UTC 时区暴露 —— 这正是它长期没被发现的原因（CI 跑在 UTC）。
func TestStartOfDay_UTCIsUnaffected(t *testing.T) {
	in := time.Date(2026, 9, 16, 8, 58, 32, 0, time.UTC)
	if got, want := StartOfDay(in), time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("StartOfDay(UTC) = %v, want %v", got, want)
	}
}

func TestStartOfDayOffset(t *testing.T) {
	cst := time.FixedZone("CST", 8*60*60)
	in := time.Date(2026, 9, 16, 8, 58, 32, 0, cst)
	for _, tc := range []struct {
		daysAgo int
		want    time.Time
	}{
		{0, time.Date(2026, 9, 16, 0, 0, 0, 0, cst)},
		{1, time.Date(2026, 9, 15, 0, 0, 0, 0, cst)},
		{2, time.Date(2026, 9, 14, 0, 0, 0, 0, cst)},
	} {
		if got := StartOfDayOffset(in, tc.daysAgo); !got.Equal(tc.want) {
			t.Fatalf("StartOfDayOffset(-%d) = %v, want %v", tc.daysAgo, got, tc.want)
		}
	}
}

func TestEndOfDay(t *testing.T) {
	cst := time.FixedZone("CST", 8*60*60)
	in := time.Date(2026, 9, 16, 8, 58, 32, 0, cst)
	got := EndOfDay(in)
	want := time.Date(2026, 9, 16, 23, 59, 59, 999999999, cst)
	if !got.Equal(want) {
		t.Fatalf("EndOfDay = %v, want %v", got, want)
	}
}
