package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// AssetBundleRatingMigration 资产包评分表
//
// 此前 asset_bundles.rating/rating_count 无任何写路径（评分只展示不生效）。
// 本迁移创建 asset_bundle_ratings（一人一包一票）并由服务层聚合回写。
type AssetBundleRatingMigration struct {
	db *gorm.DB
}

func NewAssetBundleRatingMigration(db *gorm.DB) *AssetBundleRatingMigration {
	return &AssetBundleRatingMigration{db: db}
}

func (m *AssetBundleRatingMigration) Version() string { return "v3.95.0" }

func (m *AssetBundleRatingMigration) Name() string {
	return "资产包评分表（asset_bundle_ratings）"
}

func (m *AssetBundleRatingMigration) Description() string {
	return "创建 asset_bundle_ratings 表（一人一包一票 + 聚合回写 asset_bundles.rating/rating_count），补齐评分写路径"
}

func (m *AssetBundleRatingMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).AutoMigrate(&model.AssetBundleRating{}); err != nil {
		return fmt.Errorf("migrate asset_bundle_ratings failed: %w", err)
	}
	return nil
}

func (m *AssetBundleRatingMigration) Down(ctx context.Context) error {
	declineTableDrop(m.Version(), "asset_bundle_ratings")
	return nil
}

var _ migration.Migration = (*AssetBundleRatingMigration)(nil)
