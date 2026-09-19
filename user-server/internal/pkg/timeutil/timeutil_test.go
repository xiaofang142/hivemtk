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

// useHostZone 把进程本地时区交给测试控制，返回 setter；Cleanup 负责还原原始值。
// businessTimeZone 在包初始化时就固定了，不受这里影响 —— 这正是下面几个用例要钉住的
// 性质：业务日口径不跟着宿主机时区漂。用例之间不得并行。
func useHostZone(t *testing.T) func(*time.Location) {
	t.Helper()
	original := time.Local
	t.Cleanup(func() { time.Local = original })
	return func(loc *time.Location) { time.Local = loc }
}

// TestBusinessDateIgnoresHostZone 复现第二十六轮 CI 的缺陷形状：
// 容器时区是 UTC，而所有 PG 连接串把会话时区钉在 Asia/Shanghai，
// 于是 UTC 16:00 之后算出的「今天」比 DB 的 DATE(ts) 少一天。
func TestBusinessDateIgnoresHostZone(t *testing.T) {
	setHost := useHostZone(t)
	instant := time.Date(2026, 9, 18, 17, 30, 0, 0, time.UTC) // = CST 2026-09-19 01:30

	setHost(time.UTC)
	if got := instant.Format("2006-01-02"); got != "2026-09-18" {
		t.Fatalf("前置假设被破坏：宿主机时区的 Format 本应给出错的一天，实得 %q", got)
	}
	if got := BusinessDate(instant); got != "2026-09-19" {
		t.Errorf("BusinessDate = %q，期望业务时区的 2026-09-19", got)
	}

	setHost(time.FixedZone("CST", 8*3600))
	if got := BusinessDate(instant); got != "2026-09-19" {
		t.Errorf("换宿主时区后 BusinessDate 漂移了：= %q，期望 2026-09-19", got)
	}
}

// TestParseBusinessDateIsCSTMidnight 保证日期串解析出的边界就是业务日的 00:00，
// 而不是 time.Parse 给出的 UTC 00:00（后者与 CST 写入的时间戳比较会整体后移 8 小时）。
func TestParseBusinessDateIsCSTMidnight(t *testing.T) {
	useHostZone(t)(time.UTC)

	got, err := ParseBusinessDate("2026-09-19")
	if err != nil {
		t.Fatalf("ParseBusinessDate 报错: %v", err)
	}
	if want := "2026-09-19T00:00:00+08:00"; got.Format(time.RFC3339) != want {
		t.Errorf("ParseBusinessDate = %s，期望 %s", got.Format(time.RFC3339), want)
	}

	naive, err := time.Parse("2006-01-02", "2026-09-19")
	if err != nil {
		t.Fatalf("对照用的 time.Parse 不该失败: %v", err)
	}
	if got.Equal(naive) {
		t.Error("ParseBusinessDate 退化成了 time.Parse（UTC 零点），8 小时偏移回来了")
	}

	if _, err := ParseBusinessDate("19/09/2026"); err == nil {
		t.Error("非法格式必须报错")
	}
}

// TestBusinessDateParseRoundTrip 保证「格式化 → 解析」不跨日漂移，
// 两者必须共用同一个业务时区。
func TestBusinessDateParseRoundTrip(t *testing.T) {
	useHostZone(t)(time.UTC)

	for _, offset := range []time.Duration{0, 8 * time.Hour, 16 * time.Hour, 23*time.Hour + 59*time.Minute} {
		now := time.Now().Add(offset)
		parsed, err := ParseBusinessDate(BusinessDate(now))
		if err != nil {
			t.Fatalf("BusinessDate 的产物解析失败: %v", err)
		}
		if BusinessDate(parsed) != BusinessDate(now) {
			t.Errorf("offset=%v 往返漂移: %s -> %s", offset, BusinessDate(now), BusinessDate(parsed))
		}
		if !parsed.Before(now) {
			t.Errorf("offset=%v 解析出的日首不该晚于原时刻", offset)
		}
	}
}

// TestStartOfBusinessDayIgnoresHostZone 钉死「今日」边界按业务日而非宿主机日切。
//
// 缺陷形状（第二十六轮 A2 只修了一半的那一半）：live_code / community 的
// `created_at >= StartOfDay(time.Now())` 在 UTC 容器上取的是 UTC 零点 = CST 08:00，
// 于是业务日 00:00–08:00 的增量被静默排除在「今日」之外 —— 症状是计数偏小，
// 不是报错，且本地（宿主机 CST）永远看不出来。
//
// 反向验证：把实现换回 StartOfDay(t)，本用例在 setHost(UTC) 那一步立即失败。
func TestStartOfBusinessDayIgnoresHostZone(t *testing.T) {
	setHost := useHostZone(t)
	// 2026-09-18 17:30 UTC == 2026-09-19 01:30 CST：宿主机日与业务日不同一天。
	instant := time.Date(2026, 9, 18, 17, 30, 0, 0, time.UTC)
	want := time.Date(2026, 9, 19, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))

	setHost(time.UTC)
	if got := StartOfDay(instant); !got.Equal(want.Add(-16 * time.Hour)) {
		t.Fatalf("前置假设被破坏：StartOfDay 本应给出错的 UTC 零点（= 业务日首前 16h），实得 %v", got)
	}
	if got := StartOfBusinessDay(instant); !got.Equal(want) {
		t.Errorf("StartOfBusinessDay(UTC 宿主) = %v，期望 %v", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}

	setHost(time.FixedZone("CST", 8*3600))
	if got := StartOfBusinessDay(instant); !got.Equal(want) {
		t.Errorf("换宿主时区后 StartOfBusinessDay 漂移了：= %v，期望 %v", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}

	// 与 BusinessDate/ParseBusinessDate 必须同源：否则「日期串」与「时间戳边界」两条路会各自漂移。
	parsed, err := ParseBusinessDate(BusinessDate(instant))
	if err != nil {
		t.Fatalf("ParseBusinessDate(BusinessDate(..)) 不该失败: %v", err)
	}
	if !parsed.Equal(StartOfBusinessDay(instant)) {
		t.Errorf("日首两条算法不一致: %v vs %v", parsed, StartOfBusinessDay(instant))
	}
}
