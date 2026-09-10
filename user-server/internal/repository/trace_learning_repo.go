// trace_learning_repo.go 追踪自学习仓储（评估日志/批次候选/权重排行）（五层 L5）
package repository

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"

	"gorm.io/gorm"
)

// TraceLearningRepository trace_learning 域数据访问收口
type TraceLearningRepository struct {
	db *gorm.DB
}

// NewTraceLearningRepository 构造
func NewTraceLearningRepository(db *gorm.DB) *TraceLearningRepository {
	return &TraceLearningRepository{db: db}
}

// GetDB 暴露底层连接（仅供 trace_learning 聚合/权重调整等只读协作方复用同一连接/事务）
func (r *TraceLearningRepository) GetDB() *gorm.DB {
	if r == nil {
		return nil
	}
	return r.db
}

// UpsertAttemptedEvalLog 幂等写评估审计（trace_id 命中即跳过）
func (r *TraceLearningRepository) UpsertAttemptedEvalLog(ctx context.Context, log *model.TraceEvalLog) error {
	return r.db.WithContext(ctx).Where("trace_id = ?", log.TraceID).Assign(*log).FirstOrCreate(log).Error
}

// ListPendingTraceIDs 批次候选：窗口内 AI dispatch 节点带 reply 的未评估 trace（id 升序）
func (r *TraceLearningRepository) ListPendingTraceIDs(ctx context.Context, sinceHours, limit int) ([]string, error) {
	sub := r.db.WithContext(ctx).Table("message_trace").
		Select("trace_id").
		Where("node = ?", tracing.NodeAIDispatch).
		Where("output::text LIKE ?", `%"`+`reply`+`"%`)
	if sinceHours > 0 {
		sub = sub.Where("created_at >= now() - make_interval(hours => ?)", sinceHours)
	}
	var traceIDs []string
	err := r.db.WithContext(ctx).
		Table("message_trace").
		Select("trace_id").
		Where("trace_id IN (?)", sub).
		Where("trace_id NOT IN (SELECT trace_id FROM trace_eval_log)").
		Group("trace_id").
		Order("MAX(id) ASC").
		Limit(limit).
		Pluck("trace_id", &traceIDs).Error
	return traceIDs, err
}

// ListRecentEvalLogs 最近评估记录（created_at 倒序）
func (r *TraceLearningRepository) ListRecentEvalLogs(ctx context.Context, limit int) ([]model.TraceEvalLog, error) {
	var logs []model.TraceEvalLog
	err := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// ListTopWeightDeviations 权重偏离 1.0 最大的知识 chunk
func (r *TraceLearningRepository) ListTopWeightDeviations(ctx context.Context, limit int) ([]map[string]any, error) {
	var rows []map[string]any
	err := r.db.WithContext(ctx).
		Table("knowledge_chunks").
		Select("id, content, weight, hit_count").
		Where("weight <> 1").
		Order("abs(weight - 1) DESC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}
