package service

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/pkg/tenant"
)

const leadNotifyWorkerLease = 5 * time.Minute

type LeadNotifyWorkerReport struct {
	Reclaimed           int64 `json:"reclaimed"`
	Claimed             int   `json:"claimed"`
	Delivered           int   `json:"delivered"`
	Retried             int   `json:"retried"`
	Exhausted           int   `json:"exhausted"`
	ReceiptUnknown      int   `json:"receipt_unknown"`
	AuthorizationDenied int   `json:"authorization_denied"`
	Failed              int   `json:"failed"`
}

// LeadNotifyWorker 只消费耐久 outbox；即时尝试与后台重试共用 LeadNotifyService.processClaimed。
type LeadNotifyWorker struct {
	svc *LeadNotifyService
}

func NewLeadNotifyWorker(svc *LeadNotifyService) *LeadNotifyWorker {
	return &LeadNotifyWorker{svc: svc}
}

func (w *LeadNotifyWorker) RunOnce(ctx context.Context) (LeadNotifyWorkerReport, error) {
	var report LeadNotifyWorkerReport
	if w == nil || w.svc == nil || w.svc.outbox == nil {
		return report, errLeadNotifyServiceUnavailable
	}
	now := time.Now().UTC()
	systemCtx := tenant.WithSystemScope(ctx)
	reclaimed, err := w.svc.outbox.ReclaimStaleSending(systemCtx, now.Add(-leadNotifyWorkerLease))
	if err != nil {
		return report, err
	}
	report.Reclaimed = reclaimed
	rows, err := w.svc.outbox.ClaimDueForUpdate(systemCtx, now, 100)
	if err != nil {
		return report, err
	}
	report.Claimed = len(rows)
	for _, row := range rows {
		itemCtx := tenant.OnlyTenant(ctx, row.TenantID)
		status, processErr := w.svc.processClaimed(itemCtx, row)
		switch status {
		case "delivered":
			report.Delivered++
		case "retry":
			report.Retried++
		case "exhausted":
			report.Exhausted++
		case "receipt_unknown":
			report.ReceiptUnknown++
		case "authorization_denied":
			report.AuthorizationDenied++
		default:
			report.Failed++
		}
		if processErr != nil && !errors.Is(processErr, errLeadNotifyAuthorization) {
			report.Failed++
		}
	}
	return report, nil
}

// ProcessDue 是 RunOnce 的语义别名，供装配与测试使用同一生产入口。
func (w *LeadNotifyWorker) ProcessDue(ctx context.Context) (LeadNotifyWorkerReport, error) {
	return w.RunOnce(ctx)
}
