package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// AssetBundleMigration 资产包模式迁移
type AssetBundleMigration struct {
	db *gorm.DB
}

// NewAssetBundleMigration 创建迁移实例
func NewAssetBundleMigration(db *gorm.DB) *AssetBundleMigration {
	return &AssetBundleMigration{db: db}
}

// Version 返回版本号
func (m *AssetBundleMigration) Version() string {
	return "v2.3.0"
}

// Name 返回迁移名称
func (m *AssetBundleMigration) Name() string {
	return "资产包模式 - AssetBundle CRUD + Weave 织布算法"
}

// Description 返回迁移描述
func (m *AssetBundleMigration) Description() string {
	return "创建 asset_bundles / asset_bundle_version_logs 表，支持 OpenAI 兼容 messages 资产包、Weave 织布算法、商户低代码表单"
}

// Up 执行迁移
func (m *AssetBundleMigration) Up(ctx context.Context) error {
	if err := m.db.AutoMigrate(&model.AssetBundle{}); err != nil {
		return fmt.Errorf("migrate asset_bundles failed: %w", err)
	}

	if err := m.db.AutoMigrate(&model.AssetBundleVersionLog{}); err != nil {
		return fmt.Errorf("migrate asset_bundle_version_logs failed: %w", err)
	}

	return nil
}

// Down 回滚
func (m *AssetBundleMigration) Down(ctx context.Context) error {
	declineTableDrop(m.Version(), "asset_bundle_version_logs", "asset_bundles")
	return nil
}

var _ migration.Migration = (*AssetBundleMigration)(nil)
