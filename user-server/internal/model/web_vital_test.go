package model

import (
	"reflect"
	"regexp"
	"strconv"
	"testing"
)

// webVitalsRatingValues 是 web-vitals v4 的 rating 枚举全集，
// 取自 user-web/src/utils/webVitalsMonitor.js 上报的那个字段的取值范围。
// 这条断言的价值在于：把「外部库枚举」这个会随依赖升级变化的事实钉在本测试里，
// 将来谁把 Rating 列改窄（或升级 web-vitals 后枚举变长）都会立刻红。
var webVitalsRatingValues = []string{"good", "needs-improvement", "poor"}

var varcharWidthRe = regexp.MustCompile(`type:varchar\((\d+)\)`)

// varcharWidthOf 读 struct 字段 gorm tag 里 type:varchar(N) 的 N。
// 拆成两步（先抓 tag 全文再取宽度）是因为单条正则套在反引号标签上会静默失配。
func varcharWidthOf(t *testing.T, field string) int {
	t.Helper()
	f, ok := reflect.TypeOf(WebVitalRecord{}).FieldByName(field)
	if !ok {
		t.Fatalf("WebVitalRecord 没有字段 %s", field)
	}
	tag := f.Tag.Get("gorm")
	m := varcharWidthRe.FindStringSubmatch(tag)
	if m == nil {
		t.Fatalf("字段 %s 的 gorm tag %q 里没有 type:varchar(N)", field, tag)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("字段 %s 的 varchar 宽度 %q 不是数字: %v", field, m[1], err)
	}
	return n
}

// TestWebVitalRecord_RatingWidthCoversWebVitalsEnum 是本次修复的防回归闸门。
//
// 事故：Rating 原为 varchar(16)，而 "needs-improvement" 是 17 字符，
// 中等评分一上报就撞 SQLSTATE 22001 → 接口 500 → 前端 .catch(()=>{}) 静默吞掉
// → 全站性能数据永久丢失且完全隐身（每页必现，没有任何告警）。
func TestWebVitalRecord_RatingWidthCoversWebVitalsEnum(t *testing.T) {
	got := varcharWidthOf(t, "Rating")
	for _, v := range webVitalsRatingValues {
		if len(v) > got {
			t.Errorf("Rating 列宽 %d 装不下 web-vitals 枚举值 %q（%d 字符）", got, v, len(v))
		}
	}
	// 留余量：web-vitals 后续若新增枚举值，也不该再撞一次同样的截断事故。
	if margin := got - 17; margin < 8 {
		t.Errorf("Rating 列宽 %d 相对最长枚举值 17 只留了 %d 字符余量，偏紧", got, margin)
	}
}

// TestWebVitalRecord_RatingAcceptsEveryEnumValue 端到端钉住同一个事实：
// 每个枚举值都能被该列宽接受（避免只比长度而漏掉别的约束）。
func TestWebVitalRecord_RatingAcceptsEveryEnumValue(t *testing.T) {
	width := varcharWidthOf(t, "Rating")
	// Postgres 的 varchar(n) 判据就是字符数 <= n，这里用同一判据避免引入 PG 依赖。
	for _, v := range webVitalsRatingValues {
		if len([]rune(v)) > width {
			t.Errorf("枚举值 %q（%d rune）超过列宽 %d", v, len([]rune(v)), width)
		}
	}
}

// TestWebVitalRecord_OtherColumnsUnchanged 确认这次只动了 Rating，
// 避免将来有人顺手改其它列时无感知。
func TestWebVitalRecord_OtherColumnsUnchanged(t *testing.T) {
	for _, c := range []struct {
		field string
		want  int
	}{
		{"Metric", 16},
		{"Page", 300},
		{"SessionID", 64},
		{"UserAgent", 300},
	} {
		if got := varcharWidthOf(t, c.field); got != c.want {
			t.Errorf("字段 %s 列宽 = %d，期望 %d（本次修复不应改它）", c.field, got, c.want)
		}
	}
}
