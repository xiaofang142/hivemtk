package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// AssetBundleGateBackfillMigration 生效闸门上线前的存量数据对齐
//
// 审计一轮给 ResolveSystemPrompt 加了生效闸门（draft/archived 不织入人设）。
// 但存量生产数据里，商户低代码表单历史创建的包一直停留在 draft（旧代码保存后
// 只做热启用、不改 status），这些包在旧语义下是「线上生效」的——闸门上线后
// 会被误拦，智能体人设静默回退。本迁移把这些「已托管（热启用 KV="1"）但
// status 仍是 draft」的存量包提升为 active，使部署前后行为一致。
// 之后新建的商户包由 SaveMerchantBundle 直接落 active，不再产生此类行。
type AssetBundleGateBackfillMigration struct {
	db *gorm.DB
}

func NewAssetBundleGateBackfillMigration(db *gorm.DB) *AssetBundleGateBackfillMigration {
	return &AssetBundleGateBackfillMigration{db: db}
}

func (m *AssetBundleGateBackfillMigration) Version() string { return "v3.96.0" }

func (m *AssetBundleGateBackfillMigration) Name() string {
	return "资产包生效闸门存量对齐（draft+已托管 → active）"
}

func (m *AssetBundleGateBackfillMigration) Description() string {
	return "把已被热启用托管但 status 仍为 draft 的存量资产包提升为 active，避免生效闸门上线后误拦存量商户包"
}

func (m *AssetBundleGateBackfillMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("asset_bundles") || !m.db.Migrator().HasTable("system_config_kv") {
		return nil
	}
	// 显式系统作用域（对齐 v3.80.0）：跨两张租户表的 UPDATE...FROM 必须绕开
	// on 模式租户 callback 的 raw SQL 拦截（fail-closed），否则迁移直接红。
	res := m.db.WithContext(tenant.WithSystemScope(ctx)).Exec(`
UPDATE asset_bundles AS ab
SET status = 'active'
FROM system_config_kv AS kv
WHERE ab.status = 'draft'
  AND ab.deleted_at IS NULL
  AND kv.tenant_id = ab.tenant_id
  AND kv."key" = 'bundle.hotplug.' || ab.asset_id
  AND kv.value = '1'`)
	if res.Error != nil {
		return fmt.Errorf("对齐存量已托管 draft 包失败: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		fmt.Printf("[v3.96.0] 已把 %d 个存量 draft+已托管 资产包提升为 active\n", res.RowsAffected)
	}
	return nil
}

func (m *AssetBundleGateBackfillMigration) Down(ctx context.Context) error {
	return nil // 单向数据对齐：降级不回退（回退会让闸门前已生效的包失效）
}

var _ migration.Migration = (*AssetBundleGateBackfillMigration)(nil)
