package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/browser_automation/model"

	"gorm.io/gorm"
)

// BrowserProfileHealthRepository 单主 Profile 平台健康仓储
type BrowserProfileHealthRepository interface {
	// GetByPlatform 按平台取健康行；无行返回 gorm.ErrRecordNotFound
	GetByPlatform(ctx context.Context, platform string) (*model.BrowserProfileHealth, error)
	// MarkBlocked 标记封号（幂等：无行则建，有行则刷新 blocked/reason/blocked_at）
	MarkBlocked(ctx context.Context, platform, reason string) error
	// MarkRecovered 人工恢复：清 Blocked/BlockedAt（不清行）
	MarkRecovered(ctx context.Context, platform string) error
}

type browserProfileHealthRepo struct {
	db *gorm.DB
}

// NewBrowserProfileHealthRepositoryWithDB 显式注入 gormDB（路由装配用，测试可替换）
func NewBrowserProfileHealthRepositoryWithDB(db *gorm.DB) BrowserProfileHealthRepository {
	return &browserProfileHealthRepo{db: db}
}

func (r *browserProfileHealthRepo) GetByPlatform(ctx context.Context, platform string) (*model.BrowserProfileHealth, error) {
	var h model.BrowserProfileHealth
	if err := r.db.WithContext(ctx).Where("platform = ?", platform).First(&h).Error; err != nil {
		return nil, err
	}
	return &h, nil
}

func (r *browserProfileHealthRepo) MarkBlocked(ctx context.Context, platform, reason string) error {
	now := time.Now()
	h, err := r.GetByPlatform(ctx, platform)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return r.db.WithContext(ctx).Create(&model.BrowserProfileHealth{
				Platform:  platform,
				Blocked:   true,
				Reason:    reason,
				BlockedAt: &now,
			}).Error
		}
		return err
	}
	h.Blocked = true
	h.Reason = reason
	h.BlockedAt = &now
	return r.db.WithContext(ctx).Save(h).Error
}

func (r *browserProfileHealthRepo) MarkRecovered(ctx context.Context, platform string) error {
	return r.db.WithContext(ctx).Model(&model.BrowserProfileHealth{}).
		Where("platform = ?", platform).
		Updates(map[string]any{"blocked": false, "reason": "", "blocked_at": nil}).Error
}
