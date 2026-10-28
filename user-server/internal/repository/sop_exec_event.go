package repository

import (
	"context"
	"errors"
	"hivemtk-user/internal/model"
	"time"

	"gorm.io/gorm"
)

// SOPExecEventRepository SOP 执行事件仓储
type SOPExecEventRepository struct {
	db *gorm.DB
}

// NewSOPExecEventRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
func NewSOPExecEventRepositoryDefault() *SOPExecEventRepository {
	return NewSOPExecEventRepository(GetDB())
}

func NewSOPExecEventRepository(db *gorm.DB) *SOPExecEventRepository {
	return &SOPExecEventRepository{db: db}
}

// Create 创建 SOP 执行事件（append-only：事件流不做表级幂等）
//
// 历史：v2.7.1 曾建 UNIQUE (execution_id, node_id, attempt)，但节点执行器对同一
// (node, attempt) 依次写 started/executed/completed 多条，第二条起必撞约束、被降级
// 日志吞掉——事件流实际只剩每 (node, attempt) 第一条（审计回放与卡死计数双双失真，
// 内容审计 2026-10-28 §5-①）。v3.86.1 已 DROP 该约束改普通索引；幂等性由调用方
// （outbox/状态机）负责，本表不背。
func (r *SOPExecEventRepository) Create(ctx context.Context, event *model.SOPExecEvent) error {
	if r == nil || r.db == nil {
		return errors.New("sop exec event repository not initialized")
	}
	return r.db.WithContext(ctx).Create(event).Error
}

// CountRecentByExecutionID 统计指定执行 ID 在 since 之后的执行事件数
//
// 用于卡死检测：最近有事件表示节点仍在推进，不算卡死。
func (r *SOPExecEventRepository) CountRecentByExecutionID(ctx context.Context, executionID uint, since time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sop exec event repository not initialized")
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.SOPExecEvent{}).
		Where("execution_id = ? AND created_at > ?", executionID, since).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
