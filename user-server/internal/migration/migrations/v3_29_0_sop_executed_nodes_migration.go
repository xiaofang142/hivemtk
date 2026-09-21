package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// SOPExecutedNodesMigration D02：补 sop_executions.executed_nodes 列
//
// 背景：model.SOPExecution 自 v3.x 起声明 ExecutedNodes JSONArray（gorm tag type:text，
// SAGA 补偿依据，appendExecutedNodeWithStatus 每个节点终态追加后随 execRepo.Save 全字段落库），
// 但 sop_executor_migration(v2.7.1) 仅补了 last_event_at/attempt_count/trace_id/wait_event 四列，
// 全迁移目录始终没有 executed_nodes 的 DDL——存量库上 Save 全字段 UPDATE 会因列不存在而失败，
// Saga 补偿与崩溃重放（决策 D03）均以该列为前提。
//
// 本迁移为幂等增量：ADD COLUMN IF NOT EXISTS，TEXT 可空（JSON 数组序列化形态，
// 与 model 的 gorm:"type:text" 对齐），无默认值，PG 11+ 为元数据级操作免重写表。
type SOPExecutedNodesMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*SOPExecutedNodesMigration)(nil)

func NewSOPExecutedNodesMigration(db *gorm.DB) *SOPExecutedNodesMigration {
	return &SOPExecutedNodesMigration{db: db}
}

func (m *SOPExecutedNodesMigration) Version() string { return "v3.29.0" }

func (m *SOPExecutedNodesMigration) Name() string {
	return "sop_executions 补 executed_nodes 列（SAGA 补偿依据）"
}

func (m *SOPExecutedNodesMigration) Description() string {
	return "D02: sop_executions 增加 executed_nodes TEXT（JSON 数组，节点终态轨迹），修复 model 有字段而表无列导致的 Save 落库失败"
}

func (m *SOPExecutedNodesMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).Exec(
		`ALTER TABLE sop_executions ADD COLUMN IF NOT EXISTS executed_nodes TEXT`,
	).Error; err != nil {
		return fmt.Errorf("add executed_nodes column failed: %w", err)
	}
	if err := m.db.WithContext(ctx).Exec(
		`COMMENT ON COLUMN sop_executions.executed_nodes IS '已完成节点轨迹 JSON 数组（SAGA 补偿依据，元素含 node_id/node_type/status/attempt/finished_at 等）'`,
	).Error; err != nil {

		return nil
	}
	return nil
}

func (m *SOPExecutedNodesMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "sop_executions.executed_nodes")
	return nil
}
