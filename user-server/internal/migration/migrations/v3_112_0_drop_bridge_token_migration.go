package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// DropBridgeTokenMigration v3.112.0（T063）：
// 识别码一次性替换旧 Bridge token 的第三步——清理旧凭证 KV 字段。
// 与顶层 migrations/095_drop_bridge_token.sql 是同一份契约的 Go 注册入口。
// 读路径（T058）与双写（T061）在更早步骤已完成，本迁移只删除无读方的残留配置。
type DropBridgeTokenMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*DropBridgeTokenMigration)(nil)

// NewDropBridgeTokenMigration 构造。
func NewDropBridgeTokenMigration(db *gorm.DB) *DropBridgeTokenMigration {
	return &DropBridgeTokenMigration{db: db}
}

func (m *DropBridgeTokenMigration) Version() string { return "v3.112.0" }

func (m *DropBridgeTokenMigration) Name() string {
	return "移除旧 Bridge token 写与字段"
}

func (m *DropBridgeTokenMigration) Description() string {
	return "删除 platform_config_kv / system_config_kv 的 bridge_ingest_token(+_prev) 键；读路径已切渠道识别码"
}

// Up 幂等：DELETE 不存在的键无副作用，不触碰任何业务主数据。
func (m *DropBridgeTokenMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("drop bridge token migration: db 未初始化")
	}
	keys := []string{"bridge_ingest_token", "bridge_ingest_token_prev"}
	for _, table := range []string{"platform_config_kv", "system_config_kv"} {
		if err := m.db.WithContext(ctx).Exec(
			fmt.Sprintf("DELETE FROM %s WHERE key IN (?, ?)", table), keys[0], keys[1],
		).Error; err != nil {
			return fmt.Errorf("清理 %s 旧 Bridge token 键失败: %w", table, err)
		}
	}
	// T061 重置在途窗口字段（与 migrations/095 同契约，AutoMigrate 亦会补列）。
	for _, stmt := range []string{
		"ALTER TABLE channel_identify_codes ADD COLUMN IF NOT EXISTS prev_code VARCHAR(128) NOT NULL DEFAULT ''",
		"ALTER TABLE channel_identify_codes ADD COLUMN IF NOT EXISTS prev_valid_until TIMESTAMPTZ",
	} {
		if err := m.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("补识别码在途窗口字段失败: %w", err)
		}
	}
	return nil
}

// Down 不可逆（旧凭证已退役，不重建 KV 键）。
func (m *DropBridgeTokenMigration) Down(context.Context) error { return nil }
