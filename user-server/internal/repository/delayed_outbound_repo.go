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

// ExpireStale 把 send_at 早于 cutoff 且仍 pending 的记录判为 expired，返回影响行数。
// 边界取严格小于：恰好等于 cutoff 的行本轮不动（时间单调前进，下一轮必然覆盖）。
func (r *DelayedOutboundRepository) ExpireStale(ctx context.Context, cutoff time.Time) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("status = ? AND send_at < ?", "pending", cutoff).
		Update("status", "expired")
	return res.RowsAffected, res.Error
}

// CountStuckSending 只读观测：统计"到期已远超 send_at 却仍卡在 sending"的行数。
// 行进入 sending 时不另记时间戳，故以 send_at 年龄为代理信号——正常一轮投递在秒级
// 收敛，阈值外仍为 sending 即意味着进程在投递中途崩溃。本方法不改任何状态，
// 回收/重投语义待产品拍板后再引入（见 2026-09-19 审计第七轮）。
func (r *DelayedOutboundRepository) CountStuckSending(ctx context.Context, sendAtBefore time.Time) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("status = ? AND send_at < ?", "sending", sendAtBefore).
		Count(&n).Error
	return n, err
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

// HasPendingByConversation 该会话是否还有「尚未投递且仍会被投递」的延迟出站。
//
// 只认 pending：sending 是本轮已抢占的瞬时态，进程崩溃时会永久悬挂在该状态，
// 若把它算作待发，recheck 就会被一条没人投的行长期压制，客户反而永远收不到回复。
func (r *DelayedOutboundRepository) HasPendingByConversation(ctx context.Context, conversationID string) (bool, error) {
	if r.db == nil || conversationID == "" {
		return false, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("conversation_id = ? AND status = ?", conversationID, model.DelayedStatusPending).
		Count(&n).Error
	return n > 0, err
}

// ScheduleRetry 重投失败后原地改排：回到 pending、send_at 推到 nextAt、attempts+1、记 last_error。
// 不新建行：一条待投回复在队列里始终只有一行，避免同一份内容因多次失败被重放多次。
func (r *DelayedOutboundRepository) ScheduleRetry(ctx context.Context, id uint, nextAt time.Time, lastError string) error {
	if r.db == nil || id == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     model.DelayedStatusPending,
			"send_at":    nextAt,
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": truncateForColumn(lastError, 1024),
		}).Error
}

// FinishReplay 重放收口到终态（sent / superseded / failed），失败原因留在 last_error。
func (r *DelayedOutboundRepository) FinishReplay(ctx context.Context, id uint, status string, at time.Time, lastError string) error {
	if r.db == nil || id == 0 {
		return nil
	}
	updates := map[string]any{"status": status, "sent_at": &at}
	if lastError != "" {
		updates["last_error"] = truncateForColumn(lastError, 1024)
	}
	return r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).Where("id = ?", id).Updates(updates).Error
}

// truncateForColumn 按 rune 上限截断入库文本（varchar 超长会整条 UPDATE 失败，
// 宁可截断也不能让失败原因把重试改排一起带崩）。
func truncateForColumn(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
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
