package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// AssetBundlePlatformRefMigration 为 asset_bundles 增加平台提交回执四列
//
// 此前 SubmitToPlatform 每次都调 CreateAsset 新建平台资产，且 platform_asset_id
// 不落本地库：连点/重试会制造多个 pending 平台资产，审核结果也无回程
// （本地永远看不到 审核中/通过/驳回）。落库平台资产 ID 与审核状态后：
//  1. 重复提交复用既有平台资产（幂等）；
//  2. 列表/详情可回显审核进度与驳回理由。
type AssetBundlePlatformRefMigration struct {
	db *gorm.DB
}

func NewAssetBundlePlatformRefMigration(db *gorm.DB) *AssetBundlePlatformRefMigration {
	return &AssetBundlePlatformRefMigration{db: db}
}

func (m *AssetBundlePlatformRefMigration) Version() string { return "v3.94.0" }

func (m *AssetBundlePlatformRefMigration) Name() string {
	return "资产包平台提交回执（platform_asset_id / platform_status）"
}

func (m *AssetBundlePlatformRefMigration) Description() string {
	return "为 asset_bundles 增加 platform_asset_id/platform_status/platform_review_note/platform_submitted_at，提交平台幂等化并可回显审核状态"
}

func (m *AssetBundlePlatformRefMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("asset_bundles") {
		return nil
	}
	stmts := []string{
		`ALTER TABLE asset_bundles ADD COLUMN IF NOT EXISTS platform_asset_id bigint DEFAULT 0`,
		`ALTER TABLE asset_bundles ADD COLUMN IF NOT EXISTS platform_status varchar(32) DEFAULT ''`,
		`ALTER TABLE asset_bundles ADD COLUMN IF NOT EXISTS platform_review_note text DEFAULT ''`,
		`ALTER TABLE asset_bundles ADD COLUMN IF NOT EXISTS platform_submitted_at timestamptz NULL`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return fmt.Errorf("新增 asset_bundles 平台回执列失败: %w", err)
		}
	}
	return nil
}

// Down 执行降级（不删列：回执数据保留，与 v3.9.0 绑定列同口径）
func (m *AssetBundlePlatformRefMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("asset_bundles") {
		return nil
	}
	declineColumnDrop(m.Version(), "asset_bundles.platform_asset_id")
	return nil
}

var _ migration.Migration = (*AssetBundlePlatformRefMigration)(nil)
