package service

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
)

var errLeadNotifyServiceUnavailable = errors.New("lead notify service unavailable")

// ListDeliveries 返回当前租户的独立送达历史；路由删除不影响这里的事实。
func (s *LeadNotifyService) ListDeliveries(ctx context.Context, query model.LeadNotifyDeliveryQuery) ([]*model.LeadNotifyDelivery, error) {
	if s == nil || s.outbox == nil {
		return nil, errLeadNotifyServiceUnavailable
	}
	return s.outbox.ListDeliveries(ctx, query)
}

// SummarizeDeliveries 返回按状态聚合的部分成功汇总。
func (s *LeadNotifyService) SummarizeDeliveries(ctx context.Context, query model.LeadNotifyDeliveryQuery) ([]model.LeadNotifyDeliverySummary, error) {
	if s == nil || s.outbox == nil {
		return nil, errLeadNotifyServiceUnavailable
	}
	return s.outbox.SummarizeDeliveries(ctx, query)
}

func (s *LeadNotifyService) ListOutboxByEvent(ctx context.Context, tenantID uint, eventID string) ([]*model.LeadNotifyOutbox, error) {
	if s == nil || s.outbox == nil {
		return nil, errLeadNotifyServiceUnavailable
	}
	return s.outbox.ListOutboxByEvent(ctx, tenantID, eventID)
}

func (s *LeadNotifyService) RetryManually(ctx context.Context, id uint64) error {
	if s == nil || s.outbox == nil {
		return errLeadNotifyServiceUnavailable
	}
	return s.outbox.RetryManually(ctx, id)
}

func (s *LeadNotifyService) Cancel(ctx context.Context, id uint64) error {
	if s == nil || s.outbox == nil {
		return errLeadNotifyServiceUnavailable
	}
	return s.outbox.Cancel(ctx, id)
}
