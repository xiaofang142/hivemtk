package app

import (
	"context"
	"time"

	"hivemtk-user/internal/aiagent/mcp"
	contentsvc "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/pagination"
	"hivemtk-user/internal/repository"
	appsvc "hivemtk-user/internal/service"
	tracelearn "hivemtk-user/internal/service/trace_learning"
)

// WireInboxSalesSessionPaginationConfigParams 把 config_params 的 inbox_sales 组、
// session 组与 pagination 组接进八个点位。
//
// 分层口径：aiagent/mcp、content/service、pkg/pagination 三个包都在依赖链更底层，
// 不能反向 import internal/service，所以注入统一在装配层做；inbox_sales 四条本来就在
// internal/service 里，注入也放在这里，让「一组接线一个函数」这个约定不被打断。
//
// 三条需要额外说明的：
//   - session.active_ttl 只在 repository 侧注入一次。service/customer_session.go 的
//     AutoCloseStaleSessions 直接调 repository.SessionActiveTTL() 取值，两侧天然同源；
//     曾经 service 侧另有一份 const，两处各读各的，改一边另一边不动的风险已经存在过。
//   - session.max_delay_seconds 种子类型是 duration，实现侧的单位是秒；这里读成
//     time.Duration 再折回整秒，两侧单位不统一时最容易出「差 1000 倍」的坑，
//     所以折算点就留在这一行，不下沉到包内。
//     它管的是营销流程 delay 节点单次等待的上限（超过截断），不是触达排程的「超期
//     放弃」——后者在这套代码里不存在，别照着旧种子描述去理解它。
//   - pagination 组只接了 cursor_page_size。page_max_size / page_default_size 所在的
//     ParsePagination 全树只有测试调用，没有生产消费点，没接（原因写在种子 Description 里）。
//
// Get* 调用与键名字面量必须写在同一行：scripts/check-config-param-readpoints.py
// 认的就是这个形状。
func WireInboxSalesSessionPaginationConfigParams() []string {
	bg := context.Background()
	cp := appsvc.GlobalConfigParam()
	wired := make([]string, 0, 8)

	appsvc.SetTgLeadOpportunityThresholdProvider(func() int {
		return cp.GetInt(bg, "inbox_sales", "tg_lead_opportunity_threshold", appsvc.DefaultTgLeadOpportunityThreshold)
	})
	wired = append(wired, "inbox_sales.tg_lead_opportunity_threshold")

	appsvc.SetUnifiedMinerOpportunityThresholdProvider(func() int {
		appsvc.SetLeadLLMRefineProviders(
			func() bool { return cp.GetBool(bg, "lead", "llm_refine_enabled", true) },
			func() string { return cp.GetString(bg, "lead", "industry_profile", "") },
		)
		return cp.GetInt(bg, "inbox_sales", "unified_miner_lead_threshold", appsvc.DefaultUnifiedMinerOpportunityThreshold)
	})
	wired = append(wired, "inbox_sales.unified_miner_lead_threshold")

	appsvc.SetPreviewSampleLimitProvider(func() int {
		return cp.GetInt(bg, "inbox_sales", "preview_sample_limit", appsvc.DefaultPreviewSampleLimit)
	})
	wired = append(wired, "inbox_sales.preview_sample_limit")

	// seam 本就存在（internal/service/trace_learning/insights.go），这里只补注入。
	tracelearn.SetInsightMaxLenProvider(func() int {
		return cp.GetInt(bg, "inbox_sales", "geo_lead_preview_max_len", tracelearn.DefaultInsightMaxLen)
	})
	wired = append(wired, "inbox_sales.geo_lead_preview_max_len")

	repository.SetSessionActiveTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "session", "active_ttl", repository.DefaultSessionActiveTTL)
	})
	wired = append(wired, "session.active_ttl")

	mcp.SetSessionIdleTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "session", "idle_ttl", mcp.DefaultSessionIdleTTL)
	})
	wired = append(wired, "session.idle_ttl")

	contentsvc.SetMaxFlowDelaySecondsProvider(func() int {
		return int(cp.GetDuration(bg, "session", "max_delay_seconds", contentsvc.DefaultMaxFlowDelaySeconds*time.Second).Seconds())
	})
	wired = append(wired, "session.max_delay_seconds")

	pagination.SetCursorPageSizeProvider(func() int {
		return cp.GetInt(bg, "pagination", "cursor_page_size", pagination.DefaultCursorPageSize)
	})
	wired = append(wired, "pagination.cursor_page_size")

	return wired
}
