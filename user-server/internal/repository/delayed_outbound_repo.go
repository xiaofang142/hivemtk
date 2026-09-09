// delayed_outbound_repo.go AI 回复延迟出站与 bridge 出站持久化仓储（五层 L5）
package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DelayedOutboundRepository reach_delayed_outbound 队列收口
type DelayedOutboundRepository struct {
	db *gorm.DB
}

// NewDelayedOutboundRepository 构造
func NewDelayedOutboundRepository(db *gorm.DB) *DelayedOutboundRepository {
	return &DelayedOutboundRepository{db: db}
}

// CreatePending 写入 pending 延迟回复记录
func (r *DelayedOutboundRepository) CreatePending(ctx context.Context, rec *model.DelayedOutboundReply) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

// PickDueForUpdate 事务：FOR UPDATE SKIP LOCKED 抢占到期的 pending 记录并置 sending
func (r *DelayedOutboundRepository) PickDueForUpdate(ctx context.Context, now time.Time, limit int) ([]model.DelayedOutboundReply, error) {
	if r.db == nil {
		return nil, nil
	}
	var picked []model.DelayedOutboundReply
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT * FROM reach_delayed_outbound WHERE status = ? AND send_at <= ? ORDER BY send_at ASC LIMIT ? FOR UPDATE SKIP LOCKED`,
			"pending", now, limit).Scan(&picked).Error; err != nil {
			return err
		}
		if len(picked) == 0 {
			return nil
		}
		ids := make([]uint, 0, len(picked))
		for _, rec := range picked {
			ids = append(ids, rec.ID)
		}
		return tx.Model(&model.DelayedOutboundReply{}).Where("id IN ?", ids).
			Update("status", "sending").Error
	})
	return picked, err
}

// PluckDueIDs 无事务 fallback：取到期 pending 记录 ID
func (r *DelayedOutboundRepository) PluckDueIDs(ctx context.Context, now time.Time, limit int) ([]uint, error) {
	if r.db == nil {
		return nil, nil
	}
	var ids []uint
	err := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("status = ? AND send_at <= ?", "pending", now).
		Order("send_at ASC").Limit(limit).
		Pluck("id", &ids).Error
	return ids, err
}

// MarkSendingIfPending CAS：pending → sending，返回是否抢占成功
func (r *DelayedOutboundRepository) MarkSendingIfPending(ctx context.Context, ids []uint) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("id IN ? AND status = ?", ids, "pending").Update("status", "sending")
	return res.RowsAffected > 0, res.Error
}

// ListSendingByID 取 sending 状态记录（fallback 抢占后回读）
func (r *DelayedOutboundRepository) ListSendingByID(ctx context.Context, ids []uint, limit int) ([]model.DelayedOutboundReply, error) {
	if r.db == nil {
		return nil, nil
	}
	var picked []model.DelayedOutboundReply
	err := r.db.WithContext(ctx).Where("id IN ? AND status = ?", ids, "sending").
		Order("send_at ASC").Limit(limit).
		Find(&picked).Error
	return picked, err
}

// MarkSent 重放成功：status → sent
func (r *DelayedOutboundRepository) MarkSent(ctx context.Context, id uint, sentAt time.Time) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).Where("id = ?", id).
		Updates(map[string]any{"status": "sent", "sent_at": &sentAt}).Error
}

// CreateMessageHubIdempotent 幂等写 message_hub（platform+msg_id+conversation_id 冲突忽略），
// 返回实际持久化的行与首次错误（供调用方重试判定）。
func (r *DelayedOutboundRepository) CreateMessageHubIdempotent(ctx context.Context, msg *model.MessageHub) (persisted *model.MessageHub, firstErr error) {
	if r.db == nil {
		return msg, nil
	}
	conflict := clause.OnConflict{
		Columns:   []clause.Column{{Name: "platform"}, {Name: "msg_id"}, {Name: "conversation_id"}},
		DoNothing: true,
	}
	if err := r.db.WithContext(ctx).Clauses(conflict).Create(msg).Error; err != nil {
		return nil, err
	}
	return msg, nil
}
