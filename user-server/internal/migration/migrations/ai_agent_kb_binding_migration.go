package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// AIAgentKBBindingMigration AI 智能体知识库绑定迁移
type AIAgentKBBindingMigration struct {
	db *gorm.DB
}

// NewAIAgentKBBindingMigration 创建迁移实例
func NewAIAgentKBBindingMigration(db *gorm.DB) *AIAgentKBBindingMigration {
	return &AIAgentKBBindingMigration{db: db}
}

// Version 返回版本号
func (m *AIAgentKBBindingMigration) Version() string { return "v3.14.0" }

// Name 返回迁移名称
func (m *AIAgentKBBindingMigration) Name() string {
	return "AI 智能体知识库绑定 - faq_entry_ids / sop_template_ids"
}

// Description 返回迁移描述
func (m *AIAgentKBBindingMigration) Description() string {
	return "2026-07-31：在 ai_agents 表新增 faq_entry_ids / sop_template_ids 两个 text[] 字段，绑定关系与现有 rag_product_ids / sop_ids / script_library_ids 一致；Layer1 匹配按 agent_id 过滤实现多渠道/多场景知识库隔离"
}

// Up 执行迁移
func (m *AIAgentKBBindingMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`ALTER TABLE ai_agents ADD COLUMN IF NOT EXISTS faq_entry_ids TEXT[] NOT NULL DEFAULT '{}'`,
		`ALTER TABLE ai_agents ADD COLUMN IF NOT EXISTS sop_template_ids TEXT[] NOT NULL DEFAULT '{}'`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return fmt.Errorf("ai_agent_kb_binding 迁移失败: %w (SQL: %s)", err, s)
		}
	}
	return nil
}

// Down 回滚
func (m *AIAgentKBBindingMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "ai_agents.sop_template_ids", "ai_agents.faq_entry_ids")
	return nil
}

var _ migration.Migration = (*AIAgentKBBindingMigration)(nil)
