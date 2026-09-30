package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// SupportMetricsRow 客服支持度量原始聚合行。
type SupportMetricsRow struct {
	TotalSessions       int64
	HandoffSessions     int64
	SelfServiceSessions int64
	MessageTotal        int64
	RagTotal            int64
	RagAvgRecall        float64
	RagAvgPrecision     float64
}

// SupportMetricsRepository 客服支持度量仓储接口。
type SupportMetricsRepository interface {
	Available() bool
	QuerySupportMetrics(ctx context.Context, start, end time.Time) (*SupportMetricsRow, error)
}

type supportMetricsRepo struct {
	db  *gorm.DB
	rag RagMetricsRepository
}

// NewSupportMetricsRepository 构造客服支持度量仓储。
func NewSupportMetricsRepository(db *gorm.DB) SupportMetricsRepository {
	if db == nil {
		return nil
	}
	return &supportMetricsRepo{db: db, rag: NewRagMetricsRepository(db)}
}

// Available 数据层是否可用。
func (r *supportMetricsRepo) Available() bool {
	return r != nil && r.db != nil
}

// QuerySupportMetrics 单次窗口聚合会话与 RAG 召回。
func (r *supportMetricsRepo) QuerySupportMetrics(ctx context.Context, start, end time.Time) (*SupportMetricsRow, error) {
	row := &SupportMetricsRow{}

	type sessionAgg struct {
		Total       int64
		Handoff     int64
		SelfService int64
		MessageSum  int64
	}
	var agg sessionAgg
	err := r.db.WithContext(ctx).
		Model(&model.CustomerSession{}).
		Select(`
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN handoff_at IS NOT NULL THEN 1 ELSE 0 END), 0) AS handoff,
			COALESCE(SUM(CASE WHEN resolved_at IS NOT NULL AND handoff_at IS NULL THEN 1 ELSE 0 END), 0) AS self_service,
			COALESCE(SUM(message_count), 0) AS message_sum`).
		Where("created_at >= ? AND created_at < ?", start, end).
		Scan(&agg).Error
	if err != nil {
		return nil, err
	}
	row.TotalSessions = agg.Total
	row.HandoffSessions = agg.Handoff
	row.SelfServiceSessions = agg.SelfService
	row.MessageTotal = agg.MessageSum

	if r.rag != nil {
		aggRow, ragErr := r.rag.AggregateQueryLogs(ctx, start, end, 0.3)
		if ragErr == nil && aggRow != nil {
			row.RagTotal = aggRow.Total
			row.RagAvgRecall = aggRow.AvgRecall
			row.RagAvgPrecision = aggRow.AvgPrecision
		}
	}
	return row, nil
}
