// reach_deferred_quiet_queue.go 主动触达延迟出站的持久队列（D02）
//
// 替代 MemoryQuietHoursQueue 的生产路径：命中 quiet hours / 合规静默时把完整
// 发送意图落 reach_deferred_outbound，进程重启不丢队；到期 worker 按原意图
// 经原发送管线重发（带 bypass 标记，避免重发时再次判延产生重复行）。
package service

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// quietHoursBypassCtxKey 重发链路的 quiet 豁免标记
type quietHoursBypassCtxKey struct{}

// WithQuietHoursBypass 给 ctx 打上豁免标记：延迟队列 worker 的重发不再过
// quiet/compliance 判延（这条消息已经等到窗口放开才重发，再判延会无限自旋）。
func WithQuietHoursBypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, quietHoursBypassCtxKey{}, true)
}

func quietHoursBypassed(ctx context.Context) bool {
	v, _ := ctx.Value(quietHoursBypassCtxKey{}).(bool)
	return v
}

// quietDeferredMaxAttempts 重发失败上限（达到后判 failed，原因留 last_error）
const quietDeferredMaxAttempts = 3

// PersistentQuietHoursQueue 持久 quiet 延迟队列（实现 SendQuietHoursDeferer）。
type PersistentQuietHoursQueue struct {
	repo    *repository.ReachDeferredOutboundRepository
	started atomic.Bool
}

// NewPersistentQuietHoursQueue 构造
func NewPersistentQuietHoursQueue(repo *repository.ReachDeferredOutboundRepository) *PersistentQuietHoursQueue {
	return &PersistentQuietHoursQueue{repo: repo}
}

// Defer 实现 SendQuietHoursDeferer：完整发送意图落库，返回持久 job id。
// job id 写进 req.Metadata["deferred_job_id"]（接口保持 error 单返回值，历史实现不破坏）。
func (q *PersistentQuietHoursQueue) Defer(ctx context.Context, req *ReachSendRequest, sendAt time.Time) error {
	if q.repo == nil {
		return repository.ErrReachDeferredUnavailable
	}
	if req == nil {
		return errNilReachSendRequest
	}
	purpose := model.DeferredReachPurposeQuietHours
	if req.Metadata != nil {
		if p, ok := req.Metadata["defer_purpose"]; ok && p == model.DeferredReachPurposeCompliance {
			purpose = model.DeferredReachPurposeCompliance
		}
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return err
	}
	var snapshot model.JSONMap
	if err := json.Unmarshal(reqJSON, &snapshot); err != nil {
		return err
	}
	rec := &model.ReachDeferredOutbound{
		Channel:     req.Channel,
		AccountID:   req.AccountID,
		RecipientID: req.RecipientID,
		CustomerID:  req.CustomerID,
		OperatorID:  req.OperatorID,
		Purpose:     purpose,
		RequestJSON: snapshot,
		SendAt:      sendAt,
		Status:      model.DeferredReachStatusPending,
	}
	if err := q.repo.CreatePending(ctx, rec); err != nil {
		return err
	}
	if req.Metadata == nil {
		req.Metadata = map[string]string{}
	}
	req.Metadata["deferred_job_id"] = strconv.FormatUint(uint64(rec.ID), 10)
	logger.Warnf("[R-4] 触达命中静默窗口，已持久化延迟出站 job=%d channel=%s recipient=%s send_at=%s",
		rec.ID, req.Channel, req.RecipientID, sendAt.Format(time.RFC3339))
	return nil
}

// Start 启动到期重发 worker：认领（租约+token）→ 原管线重发（bypass quiet）→
// 成功 sent / 失败退避改排 / 次数用尽 failed；租约过期的 sending 行转 unknown 待人工核对。
func (q *PersistentQuietHoursQueue) Start(ctx context.Context, pipeline SendPipeline) {
	if !q.started.CompareAndSwap(false, true) {
		return
	}
	utils.SafeGo(ctx, "reach_send_pipeline.quiet_hours_persistent_queue", func(ctx context.Context) {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			// S-1（SaaS 走查）：本 worker 由装配层用 context.Background() 启动，
			// 没有租户上下文；多租户模式下 reach_deferred_outbound 的任意查询都会被
			// 租户回调拒绝（"需要租户上下文但 ctx 未绑定"），后果是到期延迟消息永远
			// 认领不到——日志每轮刷两行错误，功能却是彻底不生效。
			// 后台任务统一经 tenant.Fanout 进入：off 模式退化为默认租户单次执行，
			// on 模式逐活跃租户执行，回调内的 GORM 访问自动获得租户隔离。
			if _, err := tenant.Fanout(ctx, nil, func(tctx context.Context, _ uint) (int64, error) {
				q.dispatchDue(tctx, pipeline)
				return 0, nil
			}); err != nil {
				logger.Errorf("[R-4] 延迟出站派发失败: %v", err)
			}
		}
	})
}

// dispatchDue 单轮派发（导出为包内可测）
func (q *PersistentQuietHoursQueue) dispatchDue(ctx context.Context, pipeline SendPipeline) {
	now := time.Now()
	q.recoverExpiredLeases(ctx, now)

	picked, err := q.repo.PickDueForUpdate(ctx, now, 100)
	if err != nil {
		logger.Errorf("[R-4] 延迟出站认领失败: %v", err)
		return
	}
	for i := range picked {
		q.replayOne(ctx, pipeline, &picked[i])
	}
}

// replayOne 重发一条到期记录：按原意图进原管线，bypass 静默判延。
func (q *PersistentQuietHoursQueue) replayOne(ctx context.Context, pipeline SendPipeline, rec *model.ReachDeferredOutbound) {
	var req ReachSendRequest
	if len(rec.RequestJSON) > 0 {
		raw, err := json.Marshal(rec.RequestJSON)
		if err == nil {
			err = json.Unmarshal(raw, &req)
		}
		if err != nil {
			// 快照坏了重发无意义：落 failed 让人工看原因。
			if ferr := q.repo.MarkFailed(ctx, rec.ID, rec.ClaimToken, "发送意图快照损坏: "+err.Error()); ferr != nil {
				logger.Errorf("[R-4] 延迟出站 %d 快照损坏且终态回写失败: %v", rec.ID, ferr)
			}
			return
		}
	}
	resp := pipeline.Send(WithQuietHoursBypass(ctx), &req)
	switch {
	case resp.Success:
		if err := q.repo.MarkSent(ctx, rec.ID, rec.ClaimToken, time.Now()); err != nil {
			logger.Errorf("[R-4] 延迟出站 %d sent 回写失败: %v", rec.ID, err)
		}
	case resp.Deferred:
		// 兜底：bypass 生效时不应出现；出现（如 bypass 丢失）按改期处理，不重复入队。
		next := resp.DeferredAt
		if next.IsZero() {
			next = time.Now().Add(time.Minute)
		}
		if err := q.repo.ReschedulePending(ctx, rec.ID, rec.ClaimToken, next); err != nil {
			logger.Errorf("[R-4] 延迟出站 %d 改期失败: %v", rec.ID, err)
		}
	default:
		reason := resp.Error
		if reason == "" {
			reason = "重发未成功且无错误信息"
		}
		attempts := rec.Attempts + 1
		if attempts >= quietDeferredMaxAttempts {
			if err := q.repo.MarkFailed(ctx, rec.ID, rec.ClaimToken, reason); err != nil {
				logger.Errorf("[R-4] 延迟出站 %d failed 回写失败: %v", rec.ID, err)
			}
			logger.Warnf("[R-4] 延迟出站 %d 重发 %d 次仍失败，判 failed: %s", rec.ID, attempts, reason)
			return
		}
		next := nextDeferredRetryAt(time.Now())
		if err := q.repo.ScheduleRetry(ctx, rec.ID, rec.ClaimToken, next, reason); err != nil {
			logger.Errorf("[R-4] 延迟出站 %d 改排失败: %v", rec.ID, err)
		}
	}
}

// recoverExpiredLeases 租约过期的 sending 行转 unknown（不自动重发，防对客户双发）。
func (q *PersistentQuietHoursQueue) recoverExpiredLeases(ctx context.Context, now time.Time) {
	expired, err := q.repo.ListExpiredLeaseSending(ctx, now, 100)
	if err != nil {
		logger.Errorf("[R-4] 延迟出站租约过期行查询失败: %v", err)
		return
	}
	for i := range expired {
		if err := q.repo.MarkSendingUnknown(ctx, expired[i].ID, expired[i].ClaimToken, "重发租约过期，外部结果未知"); err != nil {
			logger.Errorf("[R-4] 延迟出站 %d 转 unknown 失败: %v", expired[i].ID, err)
		}
	}
}

// nextDeferredRetryAt 重发退避：1 分钟起步（管道内重试已由管线自己的 retry 步骤处理，
// 这里只管 worker 级的下一轮）。落在静默窗口内则顺延到窗口放开。
// 时区用 cstZone：这是 worker 节奏不是客户语义——真实放行判定在管线
// checkQuietHours 里按客户时区（J-12）再算一次，双闸不冲突。
func nextDeferredRetryAt(now time.Time) time.Time {
	next := now.Add(time.Minute)
	if inQuietHoursWindow(next, quietHoursStartHour, quietHoursEndHour, cstZone) {
		return nextQuietHoursRelease(next, quietHoursEndHour, cstZone)
	}
	return next
}

// ListDeferredOutbounds 管理页查询（透传仓储）
func (q *PersistentQuietHoursQueue) ListDeferredOutbounds(ctx context.Context, channel, status string, page, pageSize int) ([]model.ReachDeferredOutbound, int64, error) {
	return q.repo.ListPending(ctx, channel, status, page, pageSize)
}

// CancelDeferredOutbound 管理页取消（仅 pending）
func (q *PersistentQuietHoursQueue) CancelDeferredOutbound(ctx context.Context, id uint) error {
	return q.repo.CancelPending(ctx, id)
}

// errNilReachSendRequest 入队对象缺失
var errNilReachSendRequest = errDeferredNilRequest{}

type errDeferredNilRequest struct{}

func (errDeferredNilRequest) Error() string {
	return "reach deferred outbound: nil 发送请求不是入队对象"
}

// 建表登记：与 webhook_outbound.go（DelayedOutboundReply）同一机制——
// 本文件是表的主要使用方，init 里挤进启动期 AutoMigrate。
func init() {
	_db.RegisterExtraModels(&model.ReachDeferredOutbound{})
}
