package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// AgentAssetBindingMigration 为 ai_agents 表新增 asset_bundle_id 列
//
// 智能体绑定资产包(AssetBundle.AssetID)。
// 智能体执行时按此 AssetID 由 AssetBundleService.ResolveSystemPrompt 织入资产包人设/话术。
type AgentAssetBindingMigration struct {
	db *gorm.DB
}

// NewAgentAssetBindingMigration 创建迁移实例
func NewAgentAssetBindingMigration(db *gorm.DB) *AgentAssetBindingMigration {
	return &AgentAssetBindingMigration{db: db}
}

// Version 返回版本号
func (m *AgentAssetBindingMigration) Version() string { return "v3.9.0" }

// Name 返回迁移名称
func (m *AgentAssetBindingMigration) Name() string {
	return "智能体绑定资产包（ai_agents.asset_bundle_id）"
}

// Description 返回迁移描述
func (m *AgentAssetBindingMigration) Description() string {
	return "为 ai_agents 表新增 asset_bundle_id 列，打通 智能体→资产包 绑定"
}

// Up 执行升级
func (m *AgentAssetBindingMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("ai_agents") {
		return nil
	}
	if err := m.db.WithContext(ctx).
		Exec(`ALTER TABLE ai_agents ADD COLUMN IF NOT EXISTS asset_bundle_id varchar(128) DEFAULT ''`).Error; err != nil {
		return fmt.Errorf("新增 ai_agents.asset_bundle_id 失败: %w", err)
	}
	return nil
}

// Down 执行降级（不删列，避免误删绑定数据）
func (m *AgentAssetBindingMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("ai_agents") {
		return nil
	}
	declineColumnDrop(m.Version(), "ai_agents.asset_bundle_id")
	return nil
}
