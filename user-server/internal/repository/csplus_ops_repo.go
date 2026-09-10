// csplus_ops_repo.go 客服增强操作聚合仓储（DLQ/会话优先级/暂缓/自定义属性）（五层 L5）
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// CSPlusOpsRepository 客服增强 DB 操作收口
type CSPlusOpsRepository struct {
	db *gorm.DB
}

// NewCSPlusOpsRepository 构造
func NewCSPlusOpsRepository(db *gorm.DB) *CSPlusOpsRepository {
	return &CSPlusOpsRepository{db: db}
}

// DLQFailedMessageRow 死信列表行
type DLQFailedMessageRow struct {
	ID        uint
	Platform  string
	MsgID     string
	Direction string
	Content   string
	Extra     []byte
	UpdatedAt time.Time
}

// CountFailedMessages 统计 failed 消息总数
func (r *CSPlusOpsRepository) CountFailedMessages(ctx context.Context) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	var total int64
	err := r.db.WithContext(ctx).Table("message_hub").Where("status = 'failed'").Count(&total).Error
	return total, err
}

// ListFailedMessages 死信列表（updated_at 倒序）
func (r *CSPlusOpsRepository) ListFailedMessages(ctx context.Context, limit int) ([]DLQFailedMessageRow, error) {
	if r.db == nil {
		return nil, nil
	}
	var src []DLQFailedMessageRow
	err := r.db.WithContext(ctx).Table("message_hub").
		Select("id, platform, msg_id, direction, content, extra, sent_at AS updated_at").
		Where("status = 'failed'").
		Order("updated_at DESC").Limit(limit).Scan(&src).Error
	return src, err
}

// RetryFailedMessage 单条重试: failed→pending，返回是否命中
func (r *CSPlusOpsRepository) RetryFailedMessage(ctx context.Context, id uint) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).Table("message_hub").
		Where("id = ? AND status = 'failed'", id).
		Update("status", "pending")
	return res.RowsAffected > 0, res.Error
}

// DropFailedMessage 丢弃死信（删除），返回是否命中
func (r *CSPlusOpsRepository) DropFailedMessage(ctx context.Context, id uint) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).Table("message_hub").Where("id = ? AND status = 'failed'", id).Delete(nil)
	return res.RowsAffected > 0, res.Error
}

// BatchRetryFailed 批量重试（单批上限 500 防风暴）
func (r *CSPlusOpsRepository) BatchRetryFailed(ctx context.Context) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Table("message_hub").
		Where("status = 'failed'").
		Limit(500).
		Update("status", "pending")
	return res.RowsAffected, res.Error
}

// UpdateSessionFieldBySessionID 按 session_id 更新会话字段，返回是否命中
func (r *CSPlusOpsRepository) UpdateSessionFieldBySessionID(ctx context.Context, sessionID, field string, value any) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).Table("customer_sessions").
		Where("session_id = ?", sessionID).
		Update(field, value)
	return res.RowsAffected > 0, res.Error
}

// RecoverSnoozed cron 到期恢复：snoozed_until 已过 → 置 NULL（返回恢复条数）
func (r *CSPlusOpsRepository) RecoverSnoozed(ctx context.Context) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Table("customer_sessions").
		Where("snoozed_until IS NOT NULL AND snoozed_until < NOW()").
		Update("snoozed_until", nil)
	return res.RowsAffected, res.Error
}

// GetCustomerCustomAttributes 读客户 custom_attributes 原文
func (r *CSPlusOpsRepository) GetCustomerCustomAttributes(ctx context.Context, customerID string) (string, error) {
	if r.db == nil {
		return "", nil
	}
	var curStr string
	err := r.db.WithContext(ctx).Table("customers").
		Select("COALESCE(NULLIF(custom_attributes::text,''), '{}')").
		Where("id = ?", customerID).Scan(&curStr).Error
	return curStr, err
}

// UpdateCustomerCustomAttributes 写客户 custom_attributes，返回是否命中
func (r *CSPlusOpsRepository) UpdateCustomerCustomAttributes(ctx context.Context, customerID, raw string) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).Table("customers").
		Where("id = ?", customerID).
		Update("custom_attributes", raw)
	return res.RowsAffected > 0, res.Error
}
