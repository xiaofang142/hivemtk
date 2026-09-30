package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"
)

// BrowserOutreachDedupeRepository 跨任务触达去重仓储。
type BrowserOutreachDedupeRepository interface {
	// FindOutreachHit 按四元键查命中；未命中返回 (nil, nil)。
	FindOutreachHit(ctx context.Context, platform, targetURL, action, copyHash string) (*model.BrowserOutreachDedupe, error)
	// RecordOutreachSend 记录一次发送；唯一冲突幂等（不报错）。
	RecordOutreachSend(ctx context.Context, rec *model.BrowserOutreachDedupe) error
}

type browserOutreachDedupeRepository struct {
	db *gorm.DB
}

// NewBrowserOutreachDedupeRepository 默认仓储。
func NewBrowserOutreachDedupeRepository() BrowserOutreachDedupeRepository {
	return &browserOutreachDedupeRepository{db: _db.GetDB()}
}

// NewBrowserOutreachDedupeRepositoryWithDB 测试注入仓储。
func NewBrowserOutreachDedupeRepositoryWithDB(db *gorm.DB) BrowserOutreachDedupeRepository {
	return &browserOutreachDedupeRepository{db: db}
}

// FindOutreachHit 按四元键查命中。
func (r *browserOutreachDedupeRepository) FindOutreachHit(ctx context.Context, platform, targetURL, action, copyHash string) (*model.BrowserOutreachDedupe, error) {
	var row model.BrowserOutreachDedupe
	err := r.db.WithContext(ctx).
		Where("platform = ? AND target_url = ? AND action = ? AND copy_hash = ?", platform, targetURL, action, copyHash).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// RecordOutreachSend 记录一次发送；唯一冲突幂等。
func (r *browserOutreachDedupeRepository) RecordOutreachSend(ctx context.Context, rec *model.BrowserOutreachDedupe) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(rec).Error
}
