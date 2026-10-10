package app

import (
	"context"
	"time"

	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/humanize"
)

// WireP1ConfigParams 阶段三 3.1 第一批：把 9 条 P1 点位接进参数中心。
//
// 分组口径：
//   - 前 4 条走 provider seam，落点在 internal/service/humanize（下层包，不能反向
//     import internal/service 读参数中心，否则 service/humanize → service 成环）。
//   - sales 两条、misc.edit_lock_ttl、memory.l1_ttl_hours 也在 internal/service 本包内，
//     同样走 seam——不因为"同包就能直连"就混用两种范式：本批的判据是「读取点在不在
//     internal/service 包内」，在则 seam，不在则直连，全仓统一。
//   - geo.default_visibility_days 走直连：internal/geo/service 早已 import
//     internal/service（llm.go、keyword_pipeline.go），无环可成。
//
// bg 用 context.Background() 而不用装配时的 ctx：provider 是延迟读的，捕获请求级
// ctx 会随那个请求取消一起失效，之后每次读都拿到取消的 ctx。
func WireP1ConfigParams() []string {
	bg := context.Background()
	cp := service.GlobalConfigParam()
	var wired []string

	humanize.SetBoundaryLowProvider(func() float64 {
		return cp.GetFloat(bg, "confidence", "humanize_boundary_low", humanize.DefaultBoundaryLow)
	})
	wired = append(wired, "confidence.humanize_boundary_low")
	humanize.SetBoundaryHighProvider(func() float64 {
		return cp.GetFloat(bg, "confidence", "humanize_boundary_high", humanize.DefaultBoundaryHigh)
	})
	wired = append(wired, "confidence.humanize_boundary_high")
	humanize.SetSampleRateProvider(func() float64 {
		return cp.GetFloat(bg, "confidence", "humanize_sample_rate", humanize.DefaultSampleRate)
	})
	wired = append(wired, "confidence.humanize_sample_rate")
	humanize.SetMaxRetryProvider(func() int {
		return cp.GetInt(bg, "confidence", "humanize_max_retry", humanize.DefaultMaxRetry)
	})
	wired = append(wired, "confidence.humanize_max_retry")

	service.SetAudienceDefaultLimitProvider(func() int {
		return cp.GetInt(bg, "sales", "audience_default_limit", service.DefaultAudienceLimit)
	})
	wired = append(wired, "sales.audience_default_limit")
	service.SetAudienceMaxLimitProvider(func() int {
		return cp.GetInt(bg, "sales", "audience_max_limit", service.MaxAudienceLimit)
	})
	wired = append(wired, "sales.audience_max_limit")

	service.SetEditLockTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "misc", "edit_lock_ttl", service.EditLockTTL)
	})
	wired = append(wired, "misc.edit_lock_ttl")

	service.SetL1TTLHoursProvider(func() time.Duration {
		return cp.GetDuration(bg, "memory", "l1_ttl_hours", service.L1TTLHours)
	})
	wired = append(wired, "memory.l1_ttl_hours")

	return wired
}
