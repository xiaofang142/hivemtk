// Package timeutil 提供与时间边界计算相关的小工具。
//
// 存在的理由（2026-09-16 审计 · TZ-01）：
// 仓库里原本用 `time.Now().Truncate(24 * time.Hour)` 求「今天零点」，
// 这个写法是**错的**。Go 的 `Time.Truncate` 按「自 year 1 起的绝对时间」对齐，
// 与 Location 无关 —— 因此在 CST(UTC+8) 下它得到的是 **UTC 零点 = 本地 08:00**，
// 而不是本地零点。后果是所有"今日"统计实际统计的是"本地 08:00 之后"，
// 每天前 8 小时的数据被静默漏掉。
//
// 正确做法是用 `time.Date` 在**该时刻自身的 Location** 上重建零点。
// 同源缺陷在 platform-server/internal/utils/timeutil 也修过（同一天）。
package timeutil

import "time"

// StartOfDay 返回 t 所在时区当天的 00:00:00.000000000。
//
// 与 `t.Truncate(24 * time.Hour)` 的区别：后者恒定返回 UTC 零点，
// 在非 UTC 时区下会整体偏移一个时区差。
func StartOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// StartOfDayOffset 返回 t 所在时区、往前推 n 天那天的 00:00:00。
// n=0 等价于 StartOfDay(t)。按日历日回退（而非减 24h*n），
// 因此跨夏令时的时区也不会漂移。
func StartOfDayOffset(t time.Time, daysAgo int) time.Time {
	return StartOfDay(t.AddDate(0, 0, -daysAgo))
}

// EndOfDay 返回 t 所在时区当天 23:59:59.999999999。
func EndOfDay(t time.Time) time.Time {
	return StartOfDay(t).AddDate(0, 0, 1).Add(-time.Nanosecond)
}
