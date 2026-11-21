package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type OutboundReclaimTask struct {
	repo         *repository.MessageHubRepository
	claimTimeout time.Duration
	staleAfter   time.Duration
}

// OutboundStaleAfterDefault 陈旧非终态老化阈值：超过该年龄仍 pending/inflight 的出站行
// 视为「会话已离开、回复永远送不出去」，巡检期升级 expired 终态（outbound_stale_expired）。
// 七天口径：桥端会话列表的可见窗口（页面/登录态存续）远短于此，能送达的早就送达了。
const OutboundStaleAfterDefault = 7 * 24 * time.Hour

func NewOutboundReclaimTask(repo *repository.MessageHubRepository) *OutboundReclaimTask {
	return &OutboundReclaimTask{
		repo:         repo,
		claimTimeout: InboxOutboundClaimTimeout,
		staleAfter:   OutboundStaleAfterDefault,
	}
}

// RunOnce 跑一轮三步回收，返回 (超时回收数, 到界升级数, 陈旧老化数)。
// 调用者必须携带普通租户及当前精确授权。若后续页失败，计数反映已经提交的页。
func (t *OutboundReclaimTask) RunOnce(ctx context.Context) (reclaimed, exhausted, aged int64, err error) {
	if t == nil || t.repo == nil {
		return 0, 0, 0, nil
	}
	return NewOutboundDeliveryService(t.repo).ReclaimOutbound(ctx, t.claimTimeout, t.staleAfter)
}

var outboundReclaimDisabledOnce sync.Once

// ReportOutboundReclaimDisabled records the startup state while no trusted
// tenant and evidence source is wired for automatic reclaim.
func ReportOutboundReclaimDisabled() {
	outboundReclaimDisabledOnce.Do(func() {
		logger.GetLogger().Warn().Str("reason_code", "outbound_reclaim_evidence_source_missing").
			Msg("[OutboundReclaim] automatic reclaim disabled")
	})
}
