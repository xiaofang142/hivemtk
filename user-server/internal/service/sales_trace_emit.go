package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// salesTraceWriter 全链路埋点（T-P8-01 / LTC-21）的写口：只要 Create 一格。
// repository.SalesEventRepository 天然满足本接口；测试可用内存假实现。
type salesTraceWriter interface {
	Create(ctx context.Context, ev *model.SalesEvent) error
}

// emitSalesTrace 写一条链路事件。writer 为 nil 或写入失败都只记 Warn，
// 绝不让埋点失败拖垮业务（与 quote_send.go 的失败策略同构：业务已提交，外发/落库
// 不可回滚，缺一条事件只是追溯链上少一跳）。
func emitSalesTrace(ctx context.Context, w salesTraceWriter, ev *model.SalesEvent, where string) {
	if w == nil {
		return
	}
	if err := w.Create(ctx, ev); err != nil {
		logger.Ctx(ctx).Warn().
			Str("where", where).
			Str("event_type", ev.EventType).
			Str("opportunity_id", ev.OpportunityID).
			Err(err).
			Msg("sales trace: 业务已提交但链路事件没写进去，ListByOpportunityID 在这一跳上断一条")
	}
}
