package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// RagQueryLogAggRow rag_query_logs 基础聚合行
//
// 与 service.RecallMetrics 字段含义对齐，但保持仓储层独立定义。
type RagQueryLogAggRow struct {
	Total        int64
	AvgRecall    float64
	AvgPrecision float64
	AvgLatency   float64
	ZeroHit      int64
	LowRecall    int64
}

// RagMetricsRepository RAG 召回率监控仓储接口
type RagMetricsRepository interface {
	CreateQueryLog(ctx context.Context, log *model.RagQueryLog) error
	CreateQueryLogsInBatches(ctx context.Context, logs []*model.RagQueryLog, batchSize int) error
	AggregateQueryLogs(ctx context.Context, start, end time.Time, lowRecallThreshold float64) (*RagQueryLogAggRow, error)
	PluckP99Latency(ctx context.Context, start, end time.Time, offset int) (int64, error)
	FindLowRecallQueryLogs(ctx context.Context, threshold float64, limit int) ([]model.RagQueryLog, error)
	FindDailyByWindow(ctx context.Context, start, end time.Time) (*model.RagMetricsDaily, error)
	SaveDaily(ctx context.Context, daily *model.RagMetricsDaily) error
	CreateDaily(ctx context.Context, daily *model.RagMetricsDaily) error
	FindLatestDailies(ctx context.Context, limit int) ([]model.RagMetricsDaily, error)
}

type ragMetricsRepo struct {
	db *gorm.DB
}

// NewRagMetricsRepository 创建 RAG 召回率监控仓储
//
// ARC-01：db 为 nil 时返回 nil 接口，service 侧以 repo == nil 作为未初始化守卫，
// 避免持有非 nil 接口却在方法内对 nil *gorm.DB 解引用 panic。
func NewRagMetricsRepository(db *gorm.DB) RagMetricsRepository {
	if db == nil {
		return nil
	}
	return &ragMetricsRepo{db: db}
}

func (r *ragMetricsRepo) CreateQueryLog(ctx context.Context, log *model.RagQueryLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *ragMetricsRepo) CreateQueryLogsInBatches(ctx context.Context, logs []*model.RagQueryLog, batchSize int) error {
	if len(logs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(logs, batchSize).Error
}

func (r *ragMetricsRepo) AggregateQueryLogs(ctx context.Context, start, end time.Time, lowRecallThreshold float64) (*RagQueryLogAggRow, error) {
	row := &RagQueryLogAggRow{}
	if err := r.db.WithContext(ctx).
		Model(&model.RagQueryLog{}).
		Where("created_at >= ? AND created_at < ?", start, end).
		Select(`
			COUNT(*) AS total,
			COALESCE(AVG(recall), 0) AS avg_recall,
			COALESCE(AVG(precision), 0) AS avg_precision,
			COALESCE(AVG(latency_ms), 0) AS avg_latency,
			COUNT(*) FILTER (WHERE retrieved_count = 0) AS zero_hit,
			COUNT(*) FILTER (WHERE recall < ?) AS low_recall
		`, lowRecallThreshold).
		Scan(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (r *ragMetricsRepo) PluckP99Latency(ctx context.Context, start, end time.Time, offset int) (int64, error) {
	var p99Latency int64
	if err := r.db.WithContext(ctx).
		Model(&model.RagQueryLog{}).
		Where("created_at >= ? AND created_at < ?", start, end).
		Order("latency_ms ASC").
		Offset(offset).
		Limit(1).
		Pluck("latency_ms", &p99Latency).Error; err != nil {
		return 0, err
	}
	return p99Latency, nil
}

func (r *ragMetricsRepo) FindLowRecallQueryLogs(ctx context.Context, threshold float64, limit int) ([]model.RagQueryLog, error) {
	var rows []model.RagQueryLog
	err := r.db.WithContext(ctx).
		Model(&model.RagQueryLog{}).
		Where("recall < ? AND relevant_count > 0", threshold).
		Order("created_at DESC").
		Limit(limit).
		Select("id, query, session_id, recall, precision, latency_ms, retrieved_count, relevant_count, hit_count, created_at").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *ragMetricsRepo) FindDailyByWindow(ctx context.Context, start, end time.Time) (*model.RagMetricsDaily, error) {
	var existing model.RagMetricsDaily
	err := r.db.WithContext(ctx).
		Where("window_start = ? AND window_end = ?", start, end).
		First(&existing).Error
	if err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *ragMetricsRepo) SaveDaily(ctx context.Context, daily *model.RagMetricsDaily) error {
	return r.db.WithContext(ctx).Save(daily).Error
}

func (r *ragMetricsRepo) CreateDaily(ctx context.Context, daily *model.RagMetricsDaily) error {
	return r.db.WithContext(ctx).Create(daily).Error
}

func (r *ragMetricsRepo) FindLatestDailies(ctx context.Context, limit int) ([]model.RagMetricsDaily, error) {
	var rows []model.RagMetricsDaily
	if err := r.db.WithContext(ctx).
		Order("window_start DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// IsRecordNotFound 判断是否为记录未找到错误
func IsRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

var _ RagMetricsRepository = (*ragMetricsRepo)(nil)
