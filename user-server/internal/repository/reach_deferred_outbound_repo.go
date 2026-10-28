// reach_deferred_outbound_repo.go 主动触达延迟出站持久化仓储（五层 L5）
//
// D02：reach 发送管线的持久 Deferrer 底座。入队（Defer）、到期认领（租约+token）、
// 终态回写（栅栏）、查询与取消（管理页）都在这里收口。
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReachDeferredOutboundLease 单次重发租约时长（与 D03 同量级：正常投递秒级收敛）
const ReachDeferredOutboundLease = 5 * time.Minute

// errReachDeferredUnavailable 仓储句柄缺失必须报错：调用方（发送管线）把入队失败
// 按「拒绝本次发送」处理；返回 nil 会被当成「已延后」而静默丢消息。
// ErrReachDeferredUnavailable 仓储句柄缺失（导出供 service 层判定）
var ErrReachDeferredUnavailable = errors.New("reach deferred outbound repository not initialized")

// ReachDeferredOutboundRepository reach_deferred_outbound 收口
type ReachDeferredOutboundRepository struct {
	db *gorm.DB
}

// NewReachDeferredOutboundRepository 构造
func NewReachDeferredOutboundRepository(db *gorm.DB) *ReachDeferredOutboundRepository {
	return &ReachDeferredOutboundRepository{db: db}
}

// CreatePending 写入 pending 延迟出站（完整发送意图快照）
func (r *ReachDeferredOutboundRepository) CreatePending(ctx context.Context, rec *model.ReachDeferredOutbound) error {
	if r == nil || r.db == nil {
		return ErrReachDeferredUnavailable
	}
	if rec == nil {
		return errors.New("reach deferred outbound: nil 记录不是入队对象")
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

// PickDueForUpdate 事务：FOR UPDATE SKIP LOCKED 抢占到期的 pending 记录并置 sending，
// 逐行签发 claim_token + lease_until（栅栏回写）。
func (r *ReachDeferredOutboundRepository) PickDueForUpdate(ctx context.Context, now time.Time, limit int) ([]model.ReachDeferredOutbound, error) {
	if r.db == nil {
		return nil, nil
	}
	var picked []model.ReachDeferredOutbound
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ReachDeferredOutbound{}).
			Where("status = ? AND send_at <= ?", model.DeferredReachStatusPending, now).
			Order("send_at ASC").
			Limit(clampBatchLimit(limit)).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Find(&picked).Error; err != nil {
			return err
		}
		if len(picked) == 0 {
			return nil
		}
		for i := range picked {
			token := uuid.NewString()
			leaseUntil := now.Add(ReachDeferredOutboundLease)
			if err := tx.Model(&model.ReachDeferredOutbound{}).Where("id = ?", picked[i].ID).
				Updates(map[string]any{
					"status":      model.DeferredReachStatusSending,
					"claim_token": token,
					"claimed_at":  now,
					"lease_until": leaseUntil,
				}).Error; err != nil {
				return err
			}
			picked[i].Status = model.DeferredReachStatusSending
			picked[i].ClaimToken = token
			claimedAt := now
			picked[i].ClaimedAt = &claimedAt
			picked[i].LeaseUntil = &leaseUntil
		}
		return nil
	})
	return picked, err
}

// MarkSent 重发成功：status → sent（带 token 栅栏）。
func (r *ReachDeferredOutboundRepository) MarkSent(ctx context.Context, id uint, token string, sentAt time.Time) error {
	if r == nil || r.db == nil {
		return ErrReachDeferredUnavailable
	}
	q := r.db.WithContext(ctx).Model(&model.ReachDeferredOutbound{}).Where("id = ?", id)
	if token != "" {
		q = q.Where("claim_token = ?", token)
	}
	return _db.EnsureAffected(q.Updates(map[string]any{
		"status":      model.DeferredReachStatusSent,
		"sent_at":     &sentAt,
		"lease_until": nil,
	}))
}

// ScheduleRetry 重发失败改排：回 pending、send_at 顺延、attempts+1、记 last_error、清租约。
func (r *ReachDeferredOutboundRepository) ScheduleRetry(ctx context.Context, id uint, token string, nextAt time.Time, lastError string) error {
	if r == nil || r.db == nil {
		return ErrReachDeferredUnavailable
	}
	if id == 0 {
		return errors.New("reach deferred outbound: 空 ID 不是改排对象")
	}
	q := r.db.WithContext(ctx).Model(&model.ReachDeferredOutbound{}).Where("id = ?", id)
	if token != "" {
		q = q.Where("claim_token = ?", token)
	}
	return _db.EnsureAffected(q.Updates(map[string]any{
		"status":      model.DeferredReachStatusPending,
		"send_at":     nextAt,
		"attempts":    gorm.Expr("attempts + 1"),
		"last_error":  truncateForColumn(lastError, 1024),
		"claim_token": "",
		"claimed_at":  nil,
		"lease_until": nil,
	}))
}

// MarkFailed 终态 failed（重发次数用尽），原因留在 last_error。
func (r *ReachDeferredOutboundRepository) MarkFailed(ctx context.Context, id uint, token, lastError string) error {
	if r == nil || r.db == nil {
		return ErrReachDeferredUnavailable
	}
	q := r.db.WithContext(ctx).Model(&model.ReachDeferredOutbound{}).
		Where("id = ? AND status = ?", id, model.DeferredReachStatusSending)
	if token != "" {
		q = q.Where("claim_token = ?", token)
	}
	return _db.EnsureAffected(q.Updates(map[string]any{
		"status":      model.DeferredReachStatusFailed,
		"last_error":  truncateForColumn(lastError, 1024),
		"lease_until": nil,
	}))
}

// ReschedulePending 重发时管线再次判延（quiet 参数变化等）：同一行改期，不产生重复行。
func (r *ReachDeferredOutboundRepository) ReschedulePending(ctx context.Context, id uint, token string, nextAt time.Time) error {
	if r == nil || r.db == nil {
		return ErrReachDeferredUnavailable
	}
	q := r.db.WithContext(ctx).Model(&model.ReachDeferredOutbound{}).
		Where("id = ? AND status = ?", id, model.DeferredReachStatusSending)
	if token != "" {
		q = q.Where("claim_token = ?", token)
	}
	return _db.EnsureAffected(q.Updates(map[string]any{
		"status":      model.DeferredReachStatusPending,
		"send_at":     nextAt,
		"claim_token": "",
		"claimed_at":  nil,
		"lease_until": nil,
	}))
}

// ListExpiredLeaseSending 列出租约已过期的 sending 行（回收对象）
func (r *ReachDeferredOutboundRepository) ListExpiredLeaseSending(ctx context.Context, now time.Time, limit int) ([]model.ReachDeferredOutbound, error) {
	if r.db == nil {
		return nil, nil
	}
	var rows []model.ReachDeferredOutbound
	err := r.db.WithContext(ctx).
		Where("status = ? AND lease_until IS NOT NULL AND lease_until < ?", model.DeferredReachStatusSending, now).
		Order("send_at ASC").Limit(clampBatchLimit(limit)).
		Find(&rows).Error
	return rows, err
}

// MarkSendingUnknown 把 sending 行判为 unknown（外部结果未知，不自动重发）
func (r *ReachDeferredOutboundRepository) MarkSendingUnknown(ctx context.Context, id uint, token, reason string) error {
	if r == nil || r.db == nil {
		return ErrReachDeferredUnavailable
	}
	q := r.db.WithContext(ctx).Model(&model.ReachDeferredOutbound{}).
		Where("id = ? AND status = ?", id, model.DeferredReachStatusSending)
	if token != "" {
		q = q.Where("claim_token = ?", token)
	}
	return _db.EnsureAffected(q.Updates(map[string]any{
		"status":      model.DeferredReachStatusUnknown,
		"lease_until": nil,
		"last_error":  truncateForColumn("发送结果未知待核对: "+reason, 1024),
	}))
}

// ResolveUnknown 人工核对后的收敛出口：pending（确认未发，重新投）/ sent（确认已达）/ cancelled。
func (r *ReachDeferredOutboundRepository) ResolveUnknown(ctx context.Context, id uint, target string, sendAt time.Time, note string) error {
	if r == nil || r.db == nil {
		return ErrReachDeferredUnavailable
	}
	if id == 0 {
		return errors.New("reach deferred outbound: 空 ID 不是核对对象")
	}
	updates := map[string]any{"lease_until": nil}
	switch target {
	case model.DeferredReachStatusPending:
		updates["status"] = model.DeferredReachStatusPending
		updates["send_at"] = sendAt
	case model.DeferredReachStatusSent:
		updates["status"] = model.DeferredReachStatusSent
		updates["sent_at"] = &sendAt
	case model.DeferredReachStatusCancelled:
		updates["status"] = model.DeferredReachStatusCancelled
	default:
		return fmt.Errorf("reach deferred outbound: 不支持的 unknown 核对出口 %q（允许 pending/sent/cancelled）", target)
	}
	if note != "" {
		updates["last_error"] = truncateForColumn("人工核对: "+note, 1024)
	}
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.ReachDeferredOutbound{}).
		Where("id = ? AND status = ?", id, model.DeferredReachStatusUnknown).Updates(updates))
}

// ListPending 管理页查询：按状态/渠道列延迟出站（租户隔离由 callback 注入）
func (r *ReachDeferredOutboundRepository) ListPending(ctx context.Context, channel, status string, page, pageSize int) ([]model.ReachDeferredOutbound, int64, error) {
	if r.db == nil {
		return nil, 0, nil
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	q := r.db.WithContext(ctx).Model(&model.ReachDeferredOutbound{})
	if channel != "" {
		q = q.Where("channel = ?", channel)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.ReachDeferredOutbound
	if err := q.Order("send_at ASC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// CancelPending 管理页取消：仅 pending 可取消（sending/终态行不可）。
func (r *ReachDeferredOutboundRepository) CancelPending(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return ErrReachDeferredUnavailable
	}
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.ReachDeferredOutbound{}).
		Where("id = ? AND status = ?", id, model.DeferredReachStatusPending).
		Update("status", model.DeferredReachStatusCancelled))
}
