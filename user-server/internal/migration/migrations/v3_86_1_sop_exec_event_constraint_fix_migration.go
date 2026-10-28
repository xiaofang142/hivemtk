// Package migrations 迁移集合。
package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// SOPExecEventConstraintFixMigration v3.86.1（内容审计 2026-10-28 §5-①）：
// 去掉 sop_exec_events 的错误唯一约束 uk_sop_exec_events (execution_id, node_id, attempt)。
//
// 该约束与写路径结构性冲突：节点执行器对同一 (execution_id, node_id, attempt) 依次写
// started/executed/completed（或 failed）多条事件（sop_dispatcher.writeExecEvent），
// 第二条起必撞约束，而调用方按约定"降级仅记日志"——事件流实际只剩每个 (node, attempt)
// 的第一条，审计回放与卡死检测（CountRecentByExecutionID 按行数计）双双失真。
// 事件流按 append-only 语义设计（模型注释即"审计 + 调试回放"），幂等性由调用方
// （outbox/状态机）负责，不落在表约束上。
//
// 处置：DROP 约束；补 (execution_id, node_id, attempt) 普通复合索引保留查询路径；
// 历史被约束吞掉的事件不可恢复（如实登记，不做伪造回填）。
type SOPExecEventConstraintFixMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*SOPExecEventConstraintFixMigration)(nil)

// NewSOPExecEventConstraintFixMigration 构造
func NewSOPExecEventConstraintFixMigration(db *gorm.DB) *SOPExecEventConstraintFixMigration {
	return &SOPExecEventConstraintFixMigration{db: db}
}

func (m *SOPExecEventConstraintFixMigration) Version() string { return "v3.86.1" }

func (m *SOPExecEventConstraintFixMigration) Name() string {
	return "sop_exec_events 去除错误唯一约束（append-only 语义归位）"
}

func (m *SOPExecEventConstraintFixMigration) Description() string {
	return "DROP CONSTRAINT uk_sop_exec_events（与 started/executed/completed 同 attempt 多行写路径冲突，" +
		"事件流被吞成每 (node,attempt) 一条）+ 补普通复合索引"
}

func (m *SOPExecEventConstraintFixMigration) Up(ctx context.Context) error {
	for _, stmt := range []string{
		// 幂等：约束不存在时跳过（PG 无 DROP CONCURRENTLY，直接判存在性）
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uk_sop_exec_events') THEN
				ALTER TABLE sop_exec_events DROP CONSTRAINT uk_sop_exec_events;
			END IF;
		END $$`,
		`CREATE INDEX IF NOT EXISTS idx_sop_exec_events_node_attempt ON sop_exec_events (execution_id, node_id, attempt)`,
	} {
		if err := m.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("exec [%s]: %w", stmt, err)
		}
	}
	return nil
}

func (m *SOPExecEventConstraintFixMigration) Down(_ context.Context) error {
	// 不可回退：回填该约束会把存量多行事件流打成不可写（同批次历史事件无法压缩回一行），
	// 回退唯一效果是让事件写入再次开始失败。
	return nil
}
