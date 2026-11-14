// Package repository 提供 AssetBundleVersionLog 的 CRUD 仓储。
package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// AssetBundleVersionLogRepository 资产包版本日志仓储接口
type AssetBundleVersionLogRepository interface {
	Create(ctx context.Context, m *model.AssetBundleVersionLog) error
	ListByAssetID(ctx context.Context, assetID string, limit int) ([]*model.AssetBundleVersionLog, error)
}

type assetBundleVersionLogRepo struct {
	db *gorm.DB
}

func NewAssetBundleVersionLogRepository(db *gorm.DB) AssetBundleVersionLogRepository {
	return &assetBundleVersionLogRepo{db: db}
}

func (r *assetBundleVersionLogRepo) Create(ctx context.Context, m *model.AssetBundleVersionLog) error {
	if m == nil {
		return errors.New("asset bundle version log is nil")
	}
	return r.db.WithContext(ctx).Create(m).Error
}

// ListByAssetID 按 asset_id 倒序列出版本轨迹（新→旧）。limit<=0 默认 50，封顶 200。
func (r *assetBundleVersionLogRepo) ListByAssetID(ctx context.Context, assetID string, limit int) ([]*model.AssetBundleVersionLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	list := make([]*model.AssetBundleVersionLog, 0, limit)
	err := r.db.WithContext(ctx).
		Where("asset_id = ?", assetID).
		Order("id DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}
