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

// businessTimeZone 是「业务日」所属的时区。
//
// 之所以需要一个显式的业务时区，而不是各算各的：本仓所有 PostgreSQL 连接串都把
// 会话时区钉在 Asia/Shanghai（pkg/db/db.go:31 生产、cmd/seed/main.go:192 种子、
// pkg/testutil/testdb.go:327 测试库），因此 SQL 里的 `DATE(ts)` 分桶、
// `created_at::date` 比较、以及 'YYYY-MM-DD' 字面量的解释**一律按 CST**。
// 而 Go 进程自己的 time.Now() 用的是容器本地时区（CI runner 与多数镜像默认 UTC），
// 于是 `time.Now().Format("2006-01-02")` 生成的边界和 DB 的口径在
// 16:00–23:59 UTC（= CST 次日 00:00–07:59）之间正好差一天。
// 第二十六轮 CI 首次真跑 `-race` 时，internal/repository 的 6 条统计断言就是这么红的
// —— 本地永远复现不出来，因为开发机时区恰好等于会话时区。
var businessTimeZone = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	// 精简镜像可能没有 /usr/share/zoneinfo；中国无夏令时，固定 +8 与 IANA 名等价。
	return time.FixedZone("CST", 8*3600)
}()

// BusinessDate 返回 t 在业务时区（CST）下的日历日，格式 YYYY-MM-DD。
//
// 凡是拿去和 SQL 的 `DATE(...)` / `::date` / 日期字面量比较的日期串，都必须由它生成，
// 而不是 `t.Format("2006-01-02")`（后者跟随宿主机时区，与 DB 分桶口径不一致）。
func BusinessDate(t time.Time) string {
	return t.In(businessTimeZone).Format("2006-01-02")
}

// BusinessToday 返回业务时区下「今天」的日历日。
func BusinessToday() string {
	return BusinessDate(time.Now())
}

// ParseBusinessDate 按业务时区把 YYYY-MM-DD 解析成该日 00:00:00。
//
// 用来替代 `time.Parse("2006-01-02", s)`：后者返回 **UTC 零点**，
// 拿去和 CST 会话时区下写入的时间戳列比较，窗口会整体后移 8 小时
// （每天头 8 小时的记录被静默切掉）。凡是「日期串 → 时间戳列边界」的转换都走这里。
func ParseBusinessDate(value string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", value, businessTimeZone)
}

// StartOfBusinessDay 返回 t 所属业务日（CST）的 00:00:00。
//
// 与 StartOfDay(t) 的分工：StartOfDay 取的是 **t 自身 Location** 的日首，
// 传 `time.Now()` 时就是宿主机时区的零点；本仓时间戳列都在 CST 会话时区下读写，
// 所以「今日 X」这类和 `created_at >= ?` 比较的边界必须用本函数。
// UTC 容器上两者差 8 小时：用 StartOfDay(time.Now()) 统计「今日」会静默漏掉
// 业务日 00:00–08:00 这一段（宿主机 UTC 零点 = CST 上午 8 点）。
func StartOfBusinessDay(t time.Time) time.Time {
	y, m, d := t.In(businessTimeZone).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, businessTimeZone)
}
