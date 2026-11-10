package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// LLMRoutingLogsMigration 创建 llm_routing_logs + llm_routing_audit 两张表
type LLMRoutingLogsMigration struct {
	db *gorm.DB
}

func NewLLMRoutingLogsMigration(db *gorm.DB) *LLMRoutingLogsMigration {
	return &LLMRoutingLogsMigration{db: db}
}

func (m *LLMRoutingLogsMigration) Version() string { return "v3.6.0" }

func (m *LLMRoutingLogsMigration) Name() string {
	return "创建 llm_routing_logs + llm_routing_audit 两张表"
}

func (m *LLMRoutingLogsMigration) Description() string {
	return "创建 llm_routing_logs（dispatch 调用日志）与 llm_routing_audit（路由变更审计）两张表，支撑本地模型总量统计与场景路由审计/灰度"
}

func (m *LLMRoutingLogsMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS llm_routing_logs (
			id              BIGSERIAL PRIMARY KEY,
			trace_id        VARCHAR(64),
			scenario        VARCHAR(64)    NOT NULL,
			provider        VARCHAR(64)    NOT NULL,
			model           VARCHAR(128),
			prompt_tokens   INT            NOT NULL DEFAULT 0,
			completion_tokens INT          NOT NULL DEFAULT 0,
			total_tokens    INT            NOT NULL DEFAULT 0,
			cost            NUMERIC(14,6)  NOT NULL DEFAULT 0,
			latency_ms      INT            NOT NULL DEFAULT 0,
			success         BOOLEAN        NOT NULL DEFAULT TRUE,
			error_msg       TEXT,
			from_cache      BOOLEAN        NOT NULL DEFAULT FALSE,
			created_at      TIMESTAMPTZ    NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_routing_logs_scenario ON llm_routing_logs (scenario)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_routing_logs_provider ON llm_routing_logs (provider)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_routing_logs_created_at ON llm_routing_logs (created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_routing_logs_trace_id ON llm_routing_logs (trace_id)`,
		`CREATE TABLE IF NOT EXISTS llm_routing_audit (
			id              BIGSERIAL PRIMARY KEY,
			scenario        VARCHAR(64)    NOT NULL,
			version         INT            NOT NULL DEFAULT 1,
			prev_provider   VARCHAR(64),
			new_provider    VARCHAR(64)    NOT NULL,
			prev_fallbacks  TEXT,
			new_fallbacks   TEXT,
			action          VARCHAR(32)    NOT NULL,
			operator        VARCHAR(64),
			trace_id        VARCHAR(64),
			created_at      TIMESTAMPTZ    NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_routing_audit_scenario ON llm_routing_audit (scenario)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_routing_audit_created_at ON llm_routing_audit (created_at)`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return fmt.Errorf("llm_routing_logs/audit 建表失败: %w", err)
		}
	}
	return nil
}

func (m *LLMRoutingLogsMigration) Down(ctx context.Context) error {
	// llm_routing_audit 与 llm_routing_logs 同由模型（LLMRoutingAudit/LLMRoutingLog）
	// 经 AutoMigrate 持有：基线已建表，Up 的 CREATE TABLE IF NOT EXISTS 是幂等空转，
	// Down 不得删表（删表连带销毁模型列与 tenant_id 索引＝销毁在用数据，
	// a_full_chain_migration_test.go 逐迁移归属比对钉死）；仅撤销 Up 新建的索引。
	stmts := []string{
		"DROP INDEX IF EXISTS idx_llm_routing_audit_created_at",
		"DROP INDEX IF EXISTS idx_llm_routing_audit_scenario",
		"DROP INDEX IF EXISTS idx_llm_routing_logs_trace_id",
		"DROP INDEX IF EXISTS idx_llm_routing_logs_created_at",
		"DROP INDEX IF EXISTS idx_llm_routing_logs_provider",
		"DROP INDEX IF EXISTS idx_llm_routing_logs_scenario",
	}
	declineTableDrop(m.Version(), "llm_routing_logs", "llm_routing_audit")
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return fmt.Errorf("llm_routing_logs/audit 回滚失败: %w", err)
		}
	}
	return nil
}

var _ migration.Migration = (*LLMRoutingLogsMigration)(nil)
