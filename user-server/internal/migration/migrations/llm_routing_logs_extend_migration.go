package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// LLMRoutingLogsExtendMigration 扩展 llm_routing_logs 表字段（v3.7.0）
type LLMRoutingLogsExtendMigration struct {
	db *gorm.DB
}

// NewLLMRoutingLogsExtendMigration 创建扩展迁移实例
func NewLLMRoutingLogsExtendMigration(db *gorm.DB) *LLMRoutingLogsExtendMigration {
	return &LLMRoutingLogsExtendMigration{db: db}
}

// Version 返回版本号
func (m *LLMRoutingLogsExtendMigration) Version() string { return "v3.7.0" }

// Name 返回迁移名称
func (m *LLMRoutingLogsExtendMigration) Name() string {
	return "扩展 llm_routing_logs 字段（model_type / vendor / base_url / is_fallback / cost_split / token_source）"
}

// Description 描述
func (m *LLMRoutingLogsExtendMigration) Description() string {
	return "为支撑本地/云端 token 计量三档（actual/estimated/missing）、厂商成本归集、出域审计、降级率统计，扩展 llm_routing_logs 表的 10 个字段"
}

// Up 执行升级
func (m *LLMRoutingLogsExtendMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`ALTER TABLE llm_routing_logs ADD COLUMN IF NOT EXISTS model_type      VARCHAR(16)  NOT NULL DEFAULT 'local'`,
		`ALTER TABLE llm_routing_logs ADD COLUMN IF NOT EXISTS vendor         VARCHAR(64)  NOT NULL DEFAULT 'unknown'`,
		`ALTER TABLE llm_routing_logs ADD COLUMN IF NOT EXISTS base_url       VARCHAR(512) NOT NULL DEFAULT ''`,
		`ALTER TABLE llm_routing_logs ADD COLUMN IF NOT EXISTS is_fallback    BOOLEAN      NOT NULL DEFAULT FALSE`,
		`ALTER TABLE llm_routing_logs ADD COLUMN IF NOT EXISTS prompt_cost      NUMERIC(14,6) NOT NULL DEFAULT 0`,
		`ALTER TABLE llm_routing_logs ADD COLUMN IF NOT EXISTS completion_cost  NUMERIC(14,6) NOT NULL DEFAULT 0`,
		`ALTER TABLE llm_routing_logs ADD COLUMN IF NOT EXISTS token_source   VARCHAR(16)  NOT NULL DEFAULT 'missing'`,
		`ALTER TABLE llm_routing_logs ADD COLUMN IF NOT EXISTS estimator      VARCHAR(32)  NOT NULL DEFAULT ''`,
		`ALTER TABLE llm_routing_logs ADD COLUMN IF NOT EXISTS source         VARCHAR(32)  NOT NULL DEFAULT 'dispatch'`,
		`ALTER TABLE llm_routing_logs ADD COLUMN IF NOT EXISTS scenario_provider VARCHAR(160) NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_llm_routing_logs_scenario_provider ON llm_routing_logs (scenario, provider, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_routing_logs_model_type_created ON llm_routing_logs (model_type, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_routing_logs_token_source ON llm_routing_logs (token_source, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_routing_logs_vendor ON llm_routing_logs (vendor, created_at DESC)`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return fmt.Errorf("llm_routing_logs 扩展字段失败 (%s): %w", s[:min(40, len(s))], err)
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Down 回滚
func (m *LLMRoutingLogsExtendMigration) Down(ctx context.Context) error {
	declineIndexDrop(m.Version(), "idx_llm_routing_logs_vendor", "idx_llm_routing_logs_token_source",
		"idx_llm_routing_logs_model_type_created", "idx_llm_routing_logs_scenario_provider")
	declineColumnDrop(m.Version(), "llm_routing_logs.scenario_provider", "llm_routing_logs.source",
		"llm_routing_logs.estimator", "llm_routing_logs.token_source", "llm_routing_logs.completion_cost",
		"llm_routing_logs.prompt_cost", "llm_routing_logs.is_fallback", "llm_routing_logs.base_url",
		"llm_routing_logs.vendor", "llm_routing_logs.model_type")
	return nil
}

var _ migration.Migration = (*LLMRoutingLogsExtendMigration)(nil)
