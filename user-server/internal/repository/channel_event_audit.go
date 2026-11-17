package repository

import (
	"context"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelEventAuditRepository 平台事件审计（specs/024 D2/R3）。
type ChannelEventAuditRepository struct {
	db *gorm.DB
}

func NewChannelEventAuditRepository(db *gorm.DB) *ChannelEventAuditRepository {
	return &ChannelEventAuditRepository{db: db}
}

// NewChannelEventAuditRepositoryDefault 用全局连接构造（服务侧默认入口）。
func NewChannelEventAuditRepositoryDefault() *ChannelEventAuditRepository {
	return &ChannelEventAuditRepository{db: _db.GetDB()}
}

// Record 幂等登记一条平台事件：冲突（平台重投）时什么都不做。
// 返回 written 表示本次是否真的落了一行，便于调用方区分「首次」与「重投」。
func (r *ChannelEventAuditRepository) Record(ctx context.Context, row *model.ChannelEventAudit) (bool, error) {
	if r == nil || r.db == nil || row == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
