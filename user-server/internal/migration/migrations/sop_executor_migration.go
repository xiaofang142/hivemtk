package migrations

import (
	"context"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// SOPExecutorMigration SOP 执行器迁移
type SOPExecutorMigration struct {
	db *gorm.DB
}

// NewSOPExecutorMigration 创建迁移实例
func NewSOPExecutorMigration(db *gorm.DB) *SOPExecutorMigration {
	return &SOPExecutorMigration{db: db}
}

// Version 返回版本号（必须全局唯一）
func (m *SOPExecutorMigration) Version() string {
	return "v2.7.1"
}

// Name 返回迁移名称
func (m *SOPExecutorMigration) Name() string {
	return "SOP 执行器表（sop_exec_events / sop_timers / sop_outbox）"
}

// Description 返回迁移描述
func (m *SOPExecutorMigration) Description() string {
	return "创建 SOP 节点执行器所需的 3 张表（事件流/定时器/Outbox），并扩展 sop_executions 表 4 个字段"
}

// Up 执行迁移
func (m *SOPExecutorMigration) Up(ctx context.Context) error {
	if err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sop_exec_events (
		id BIGSERIAL PRIMARY KEY,
		execution_id BIGINT NOT NULL,
		sop_id BIGINT NOT NULL,
		node_id VARCHAR(50) NOT NULL,
		node_type VARCHAR(30) NOT NULL,
		event_type VARCHAR(30) NOT NULL,
		attempt INTEGER DEFAULT 0,
		status VARCHAR(20),
		input JSONB,
		output JSONB,
		side_effects JSONB,
		error_message TEXT,
		latency_ms INTEGER,
		tokens_used INTEGER,
		trace_id VARCHAR(64),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT uk_sop_exec_events UNIQUE (execution_id, node_id, attempt)
	)`).Error; err != nil {
		return err
	}
	_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sop_exec_events_execution ON sop_exec_events(execution_id, created_at)`).Error
	_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sop_exec_events_trace ON sop_exec_events(trace_id)`).Error

	if err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sop_timers (
		id BIGSERIAL PRIMARY KEY,
		execution_id BIGINT NOT NULL,
		node_id VARCHAR(50) NOT NULL,
		wait_event VARCHAR(30) NOT NULL,
		wait_until TIMESTAMP NOT NULL,
		status VARCHAR(20) DEFAULT 'pending',
		payload JSONB,
		fired_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		return err
	}
	_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sop_timers_due ON sop_timers(status, wait_until) WHERE status = 'pending'`).Error
	_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sop_timers_execution ON sop_timers(execution_id)`).Error

	if err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sop_outbox (
		id BIGSERIAL PRIMARY KEY,
		execution_id BIGINT NOT NULL,
		event_type VARCHAR(50) NOT NULL,
		payload JSONB NOT NULL,
		processed BOOLEAN DEFAULT FALSE,
		processed_at TIMESTAMP,
		retry_count INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		return err
	}
	_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sop_outbox_unprocessed ON sop_outbox(processed, created_at) WHERE processed = FALSE`).Error
	_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sop_outbox_execution ON sop_outbox(execution_id)`).Error

	_ = m.db.Exec(`ALTER TABLE sop_executions ADD COLUMN IF NOT EXISTS last_event_at TIMESTAMP`).Error
	_ = m.db.Exec(`ALTER TABLE sop_executions ADD COLUMN IF NOT EXISTS attempt_count INTEGER DEFAULT 0`).Error
	_ = m.db.Exec(`ALTER TABLE sop_executions ADD COLUMN IF NOT EXISTS trace_id VARCHAR(64)`).Error
	_ = m.db.Exec(`ALTER TABLE sop_executions ADD COLUMN IF NOT EXISTS wait_event VARCHAR(30)`).Error
	_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sop_executions_wait ON sop_executions(status, wait_event) WHERE status = 'running'`).Error

	return nil
}

// Down 回滚
func (m *SOPExecutorMigration) Down(ctx context.Context) error {
	declineIndexDrop(m.Version(), "idx_sop_executions_wait")
	declineColumnDrop(m.Version(), "sop_executions.wait_event", "sop_executions.trace_id",
		"sop_executions.attempt_count", "sop_executions.last_event_at")
	declineTableDrop(m.Version(), "sop_outbox", "sop_timers", "sop_exec_events")
	return nil
}

var _ migration.Migration = (*SOPExecutorMigration)(nil)
