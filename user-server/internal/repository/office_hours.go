// office_hours.go 办公时间相关仓储
package repository

import (
	"context"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// OfficeHoursRepo 办公时间仓储
type OfficeHoursRepo struct {
	db *gorm.DB
}

// NewOfficeHoursRepo 构造
// NewOfficeHoursRepo 构造（无参，内部取全局 DB；对齐 customer_session.go 先例）
func NewOfficeHoursRepo() *OfficeHoursRepo {
	return &OfficeHoursRepo{db: _db.GetDB()}
}

// NewOfficeHoursRepoWithDB 显式注入 DB（测试用）
func NewOfficeHoursRepoWithDB(db *gorm.DB) *OfficeHoursRepo {
	return &OfficeHoursRepo{db: db}
}

// CountAwayReplyRecent 统计指定会话在最近 duration 内的 away 自动回复数（防重复发送）
func (r *OfficeHoursRepo) CountAwayReplyRecent(ctx context.Context, conversationID string, within time.Duration) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND trace_id = ? AND sent_at > ?", conversationID, "away", time.Now().Add(-within)).
		Model(&model.MessageHub{}).Count(&cnt).Error
	return cnt, err
}

// CreateMessageHub 创建消息中心记录
func (r *OfficeHoursRepo) CreateMessageHub(ctx context.Context, rec *model.MessageHub) error {
	return r.db.WithContext(ctx).Create(rec).Error
}
