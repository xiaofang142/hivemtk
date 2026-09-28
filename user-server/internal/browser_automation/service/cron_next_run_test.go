package service

// 契约：next_run_at 存的必须是「下一次触发时刻」。
//
// 曾经的形态是：注册时根本不写这一列，只有触发回调写，而那次把 next_run_at 和
// last_run_at 一起写成了同一个 now —— 于是第一次触发前它恒为 NULL，触发后恒等于
// 「上一次触发的时刻」。两个读数都不是列名的意思，而时区能力（CRON_TZ=）
// 恰好只有在这一列上才看得见证据。

import (
	"testing"
	"time"
)

func TestNextRunAtIsTheNextFireNotNow(t *testing.T) {
	// 5 段 */5 → 注册用 6 段 "0 */5 * * * *"：10:03:12 之后的下一次是 10:05:00
	from := time.Date(2026, 9, 28, 10, 3, 12, 0, time.Local)
	next := nextRunAt("*/5 * * * *", "", from)
	if next == nil {
		t.Fatal("合法表达式算不出下一次：这一列会被写成 NULL")
	}
	if want := time.Date(2026, 9, 28, 10, 5, 0, 0, from.Location()); !next.Equal(want) {
		t.Errorf("下一次 = %v want %v", next, want)
	}
	if !next.After(from) {
		t.Errorf("下一次 %v 不在 from %v 之后（写成 now 就是这一类的读数）", next, from)
	}
}

// G20 的可见证据：同一句「每天 12 点」，CRON_TZ=UTC 与 CRON_TZ=Asia/Shanghai
// 的绝对触发时刻必须差 8 小时。若 toSixField 的 TZ 前缀没进解析器，两腿会给出同一个瞬间。
func TestNextRunAtHonorsTimeZone(t *testing.T) {
	sh, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("本机无 Asia/Shanghai 时区数据：%v", err)
	}
	from := time.Date(2026, 9, 28, 10, 3, 0, 0, sh)
	shanghai := nextRunAt("0 12 * * *", "Asia/Shanghai", from)
	utc := nextRunAt("0 12 * * *", "UTC", from)
	if shanghai == nil || utc == nil {
		t.Fatalf("解析失败：shanghai=%v utc=%v", shanghai, utc)
	}
	if want := time.Date(2026, 9, 28, 12, 0, 0, 0, sh); !shanghai.Equal(want) {
		t.Errorf("上海 12 点的下一次 = %v want %v", shanghai, want)
	}
	if !shanghai.Add(8 * time.Hour).Equal(*utc) {
		t.Errorf("时区没参与调度：%v(上海) 与 %v(UTC) 相差不是 8h", shanghai, utc)
	}
}

// 解析不出来时宁可给 NULL：回一个 now 之类的假数就是把这个字段从「没数据」变成「错数据」，
// 前端显示出来比空白更难发现（旧实现留下的正是这种东西）。
func TestNextRunAtUnknownIsNil(t *testing.T) {
	if got := nextRunAt("bad expr here", "", time.Now()); got != nil {
		t.Errorf("坏表达式的下一次 = %v，want nil（写 NULL 让界面显示「—」）", got)
	}
}
