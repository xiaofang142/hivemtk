package migrations

import (
	"context"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// AIAgentExtensionMigration ai_agents 表扩展迁移
type AIAgentExtensionMigration struct {
	db *gorm.DB
}

// NewAIAgentExtensionMigration 创建迁移实例
func NewAIAgentExtensionMigration(db *gorm.DB) *AIAgentExtensionMigration {
	return &AIAgentExtensionMigration{db: db}
}

// Version 返回版本号（必须全局唯一）
func (m *AIAgentExtensionMigration) Version() string {
	return "v2.2.1"
}

// Name 返回迁移名称
func (m *AIAgentExtensionMigration) Name() string {
	return "ai_agents 扩展 2 字段（决策策略 / A/B 实验）"
}

// Description 返回迁移描述
func (m *AIAgentExtensionMigration) Description() string {
	return "为 ai_agents 表新增 decision_strategy_ids / ab_experiment_ids 字段（TEXT[]），支持 智能体挂载多逻辑"
}

// Up 执行迁移
func (m *AIAgentExtensionMigration) Up(ctx context.Context) error {
	if !m.db.Migrator().HasTable("ai_agents") {
		return nil
	}

	if !m.db.Migrator().HasColumn(&struct {
		DecisionStrategyIDs string `gorm:"type:text[]"`
	}{}, "DecisionStrategyIDs") {
		if err := m.db.Exec("ALTER TABLE ai_agents ADD COLUMN IF NOT EXISTS decision_strategy_ids TEXT[] DEFAULT '{}'").Error; err != nil {
			return err
		}
	}

	if !m.db.Migrator().HasColumn(&struct {
		ABExperimentIDs string `gorm:"type:text[]"`
	}{}, "ABExperimentIDs") {
		if err := m.db.Exec("ALTER TABLE ai_agents ADD COLUMN IF NOT EXISTS ab_experiment_ids TEXT[] DEFAULT '{}'").Error; err != nil {
			return err
		}
	}

	return nil
}

// Down 回滚
func (m *AIAgentExtensionMigration) Down(ctx context.Context) error {
	if !m.db.Migrator().HasTable("ai_agents") {
		return nil
	}
	declineColumnDrop(m.Version(), "ai_agents.ab_experiment_ids", "ai_agents.decision_strategy_ids")
	return nil
}

var _ migration.Migration = (*AIAgentExtensionMigration)(nil)
