package repository

import (
	"context"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EmailSendRepository interface {
	Create(ctx context.Context, email *model.EmailSend) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.EmailSend, error)
	List(ctx context.Context) ([]*model.EmailSend, error)
	Delete(ctx context.Context, id uuid.UUID) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status int) error
	ClaimDueForUpdate(ctx context.Context, now time.Time, limit int) ([]*model.EmailSend, error)
	ExpireStalePending(ctx context.Context, cutoff time.Time) (int64, error)
	ReclaimStaleSending(ctx context.Context, olderThan time.Time) (int64, error)
}

type emailSendRepo struct {
	db *gorm.DB
}

func NewEmailSendRepository() EmailSendRepository {
	return &emailSendRepo{db: _db.GetDB()}
}

// emailScheduledAt 排期时刻的单一事实源。
//
// send_time 为 NULL 是合法入库形状（dto.SendEmailRequest 的 immediateSend 与 sendTime
// 都不给时就是这样），而 SQL 三值逻辑下 `NULL <= now` 不为真 —— 直接用 send_time 比较
// 会让这类行既永不投递、又永不过期，等于把"排了时间却永不发送"换成"没排时间就永不发送"。
// 认领与过期两处判据必须引用同一个表达式，否则一条腿判到期、另一条腿判未到期，
// 行就会在 pending 里永久留存。
const emailScheduledAt = "COALESCE(send_time, created_at)"

func (r *emailSendRepo) Create(ctx context.Context, email *model.EmailSend) error {
	return r.db.WithContext(ctx).Create(email).Error
}

func (r *emailSendRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.EmailSend, error) {
	var email model.EmailSend
	if err := r.db.WithContext(ctx).First(&email, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &email, nil
}

// ClaimDueForUpdate 事务内用 FOR UPDATE SKIP LOCKED 认领一批到期的 pending 并置为 sending。
//
// 形状取自本仓已有的三条同族认领（approval_request.go:ExpirePendingBatch、
// order_draft.go、sop_timer.go:FindDueForUpdate）：多副本 worker 各拿一份**不相交**的集合，
// 谁也不等谁的锁。多副本是 DEPLOYMENT_GUIDE 里写明的受支持形态，不是假设。
func (r *emailSendRepo) ClaimDueForUpdate(ctx context.Context, now time.Time, limit int) ([]*model.EmailSend, error) {
	if r.db == nil {
		return nil, nil
	}
	var picked []*model.EmailSend
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND "+emailScheduledAt+" <= ?", model.EmailStatusPending, now).
			Order(emailScheduledAt + " ASC, id ASC")
		if limit > 0 {
			q = q.Limit(limit)
		}
		var rows []*model.EmailSend
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, 0, len(rows))
		for _, e := range rows {
			ids = append(ids, e.ID)
		}
		// 回写只认"锁住的那批 id 且仍是 pending"：读到的行与写回的行必须是同一集合，
		// 否则并发改状态时会出现"给一条已经不是 pending 的行盖上 sending"。
		if err := tx.Model(&model.EmailSend{}).
			Where("id IN ? AND status = ?", ids, model.EmailStatusPending).
			Update("status", model.EmailStatusSending).Error; err != nil {
			return err
		}
		for _, e := range rows {
			e.Status = model.EmailStatusSending
		}
		picked = rows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return picked, nil
}

// ExpireStalePending 把排期时刻早于 cutoff 却仍 pending 的行判为 expired，返回影响行数。
//
// 边界取严格小于（同 reach_delayed_outbound 的 ExpireStale）：恰好等于 cutoff 的行本轮不动，
// 时间单调前进，下一轮必然覆盖。这一格存在的理由是排水一旦被装上进程，
// 没有上限就意味着进程停机几周后重启会把几周前排的邮件一次性群发出去。
func (r *emailSendRepo) ExpireStalePending(ctx context.Context, cutoff time.Time) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.EmailSend{}).
		Where("status = ? AND "+emailScheduledAt+" < ?", model.EmailStatusPending, cutoff).
		Update("status", model.EmailStatusExpired)
	return res.RowsAffected, res.Error
}

// ReclaimStaleSending 把认领后超过 olderThan 仍停在 sending 的行回捞为 pending。
//
// sending 行没有独立的认领时刻，用 updated_at 作代理：认领那一次 UPDATE 正是它最后一次被写。
// 回捞是 at-least-once —— 崩溃点若在 SMTP 已递交之后，收件人会收到两封同样的邮件；
// 不回捞的替代方案是那封邮件永远停在 sending，谁也不会再碰它。
func (r *emailSendRepo) ReclaimStaleSending(ctx context.Context, olderThan time.Time) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.EmailSend{}).
		Where("status = ? AND updated_at < ?", model.EmailStatusSending, olderThan).
		Update("status", model.EmailStatusPending)
	return res.RowsAffected, res.Error
}

func (r *emailSendRepo) List(ctx context.Context) ([]*model.EmailSend, error) {
	var emails []*model.EmailSend
	// 默认 50 兜底;已达上限时调用方需分页重取
	q := applyListLimit(r.db.WithContext(ctx), 50)
	if err := q.Find(&emails).Error; err != nil {
		return nil, err
	}
	return emails, nil
}

func (r *emailSendRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&model.EmailSend{}, "id = ?", id).Error
}

func (r *emailSendRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status int) error {
	return r.db.WithContext(ctx).Model(&model.EmailSend{}).Where("id = ?", id).Update("status", status).Error
}
