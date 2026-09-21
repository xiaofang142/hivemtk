package migrations

import (
	"context"
	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// WorkflowVisualOrchestratorMigration 可视化工作流编排器迁移
// 新增 workflow_versions、workflow_executions 表，支持版本管理与执行监控
type WorkflowVisualOrchestratorMigration struct {
	db *gorm.DB
}

// NewWorkflowVisualOrchestratorMigration 创建可视化工作流编排器迁移
func NewWorkflowVisualOrchestratorMigration(db *gorm.DB) *WorkflowVisualOrchestratorMigration {
	return &WorkflowVisualOrchestratorMigration{db: db}
}

// Version 返回版本号
func (m *WorkflowVisualOrchestratorMigration) Version() string {
	return "v1.8.0"
}

// Name 返回迁移名称
func (m *WorkflowVisualOrchestratorMigration) Name() string {
	return "可视化工作流编排器"
}

// Description 返回迁移描述
func (m *WorkflowVisualOrchestratorMigration) Description() string {
	return "新增工作流版本表、执行实例表，支持可视化编排器的版本管理与执行监控"
}

// Up 执行升级
func (m *WorkflowVisualOrchestratorMigration) Up(ctx context.Context) error {

	m.db.Exec(`
		CREATE TABLE IF NOT EXISTS workflow_versions (
			id BIGSERIAL PRIMARY KEY,
			workflow_id VARCHAR(64) NOT NULL,
			version INT NOT NULL,
			name VARCHAR(200) NOT NULL,
			description TEXT,
			definition JSONB NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT 'draft', -- draft/published/archived
			changelog TEXT,
			created_by VARCHAR(64),
			created_at TIMESTAMPTZ DEFAULT now(),
			updated_at TIMESTAMPTZ DEFAULT now(),
			UNIQUE (workflow_id, version)
		)
	`)

	m.db.Exec("ALTER TABLE workflow_versions ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT now()")
	m.db.Exec("ALTER TABLE workflow_versions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT now()")

	m.db.Exec("CREATE INDEX IF NOT EXISTS idx_workflow_versions_wf_id ON workflow_versions(workflow_id)")
	m.db.Exec("CREATE INDEX IF NOT EXISTS idx_workflow_versions_status ON workflow_versions(status)")

	m.db.Exec(`
		CREATE TABLE IF NOT EXISTS workflow_executions (
			id BIGSERIAL PRIMARY KEY,
			workflow_id VARCHAR(64) NOT NULL,
			version INT NOT NULL,
			trigger_payload JSONB,
			status VARCHAR(16) NOT NULL, -- running/completed/failed/terminated
			current_node_id VARCHAR(64),
			context JSONB, -- 运行时上下文 (变量、循环计数等)
			started_at TIMESTAMPTZ DEFAULT now(),
			finished_at TIMESTAMPTZ,
			error TEXT,
			created_at TIMESTAMPTZ DEFAULT now(),
			updated_at TIMESTAMPTZ DEFAULT now()
		)
	`)

	m.db.Exec("ALTER TABLE workflow_executions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT now()")
	m.db.Exec("ALTER TABLE workflow_executions ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT now()")

	m.db.Exec("CREATE INDEX IF NOT EXISTS idx_workflow_executions_wf_id ON workflow_executions(workflow_id)")
	m.db.Exec("CREATE INDEX IF NOT EXISTS idx_workflow_executions_status ON workflow_executions(status)")
	m.db.Exec("CREATE INDEX IF NOT EXISTS idx_workflow_executions_started ON workflow_executions(started_at DESC)")

	m.db.Exec(`
		CREATE TABLE IF NOT EXISTS workflow_node_executions (
			id BIGSERIAL PRIMARY KEY,
			execution_id BIGINT NOT NULL REFERENCES workflow_executions(id) ON DELETE CASCADE,
			node_id VARCHAR(64) NOT NULL,
			node_type VARCHAR(32) NOT NULL, -- trigger/action/condition/subflow
			node_name VARCHAR(200),
			input_data JSONB,
			output_data JSONB,
			status VARCHAR(16) NOT NULL, -- pending/running/completed/failed/skipped
			started_at TIMESTAMPTZ,
			finished_at TIMESTAMPTZ,
			duration_ms INT,
			error TEXT,
			created_at TIMESTAMPTZ DEFAULT now()
		)
	`)

	m.db.Exec("ALTER TABLE workflow_node_executions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT now()")

	m.db.Exec("CREATE INDEX IF NOT EXISTS idx_wf_node_exec_exec_id ON workflow_node_executions(execution_id)")
	m.db.Exec("CREATE INDEX IF NOT EXISTS idx_wf_node_exec_node_id ON workflow_node_executions(node_id)")

	return nil
}

// Down 执行降级
func (m *WorkflowVisualOrchestratorMigration) Down(ctx context.Context) error {
	declineTableDrop(m.Version(), "workflow_node_executions", "workflow_executions", "workflow_versions")
	return nil
}

var _ migration.Migration = (*WorkflowVisualOrchestratorMigration)(nil)
