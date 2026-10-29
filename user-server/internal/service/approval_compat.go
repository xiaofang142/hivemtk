package service

// 兼容层：审批域已迁入 internal/service/approval（按域拆包第五刀前置）。
//
// approval 只依赖 model/repository/logger，反向不引根包：报价自动外发腿走
// approval.QuoteSendService 窄接口（见 approval/approval_auto_send_bridge.go），
// 本文件提供适配器。根包内既有调用点（quote_send / kb_release / human_task）
// 与 router/app/cmd 沿用的 service.* 入口保持不变。

import (
	"context"
	"time"

	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service/approval"
)

type ApprovalRequestService = approval.ApprovalRequestService

type ApprovalSubmitInput = approval.ApprovalSubmitInput

type ApprovalSweepReport = approval.ApprovalSweepReport

type ApprovalSweepWorker = approval.ApprovalSweepWorker

type ApprovalTaskSink = approval.ApprovalTaskSink

type ApprovalVerdict = approval.ApprovalVerdict

type ApprovalWaitNotifier = approval.ApprovalWaitNotifier

type AutoApprovalPolicy = approval.AutoApprovalPolicy

type ApprovalAutoSendBridge = approval.ApprovalAutoSendBridge

const (
	DefaultApprovalSweepInterval = approval.DefaultApprovalSweepInterval

	ApprovalApprove = approval.ApprovalApprove
	ApprovalReject  = approval.ApprovalReject
)

var (
	ErrApprovalAlreadyDecided    = approval.ErrApprovalAlreadyDecided
	ErrApprovalDecideTooLate     = approval.ErrApprovalDecideTooLate
	ErrApprovalIllegalTransition = approval.ErrApprovalIllegalTransition
	ErrApprovalInputInvalid      = approval.ErrApprovalInputInvalid
	ErrApprovalNotFound          = approval.ErrApprovalNotFound
	ErrApprovalResumeNotPending  = approval.ErrApprovalResumeNotPending
	ErrApprovalTTLTooLong        = approval.ErrApprovalTTLTooLong
)

func NewApprovalRequestService(repo repository.ApprovalRequestRepository, policy AutoApprovalPolicy) *ApprovalRequestService {
	return approval.NewApprovalRequestService(repo, policy)
}

func NewApprovalSweepWorker(svc *ApprovalRequestService, interval time.Duration) *ApprovalSweepWorker {
	return approval.NewApprovalSweepWorker(svc, interval)
}

func SetGlobalApprovalRequestService(s *ApprovalRequestService) {
	approval.SetGlobalApprovalRequestService(s)
}

func GlobalApprovalRequestService() *ApprovalRequestService {
	return approval.GlobalApprovalRequestService()
}

// quoteSendAdapter 把根包 *QuoteSendService 适配成 approval.QuoteSendService。
type quoteSendAdapter struct {
	inner *QuoteSendService
}

func (a quoteSendAdapter) Available() bool { return a.inner.Available() }

func (a quoteSendAdapter) Send(ctx context.Context, in approval.QuoteSendRequest) (*approval.QuoteSendOutcome, error) {
	res, err := a.inner.Send(ctx, QuoteSendInput{
		QuoteRowID: in.QuoteRowID,
		ApprovalID: in.ApprovalID,
		Operator:   in.Operator,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &approval.QuoteSendOutcome{}, nil
	}
	return &approval.QuoteSendOutcome{Disposition: res.Disposition, Status: res.Status}, nil
}

// NewApprovalAutoSendBridge 保留根包入口：把报价发送腿工厂适配成 approval 侧窄接口。
func NewApprovalAutoSendBridge(quoteSend func() *QuoteSendService) *ApprovalAutoSendBridge {
	return approval.NewApprovalAutoSendBridge(func() approval.QuoteSendService {
		if quoteSend == nil {
			return nil
		}
		qs := quoteSend()
		if qs == nil {
			return nil
		}
		return quoteSendAdapter{inner: qs}
	})
}
