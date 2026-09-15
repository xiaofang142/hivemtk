package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// RagRecallMonitorTableName 监控快照表名
//
// 与 service 层常量保持一致；放此处以便 repository 内部使用，避免循环引用。
const RagRecallMonitorTableName = "rag_recall_monitor_snapshots"

// RagRecallAggRow rag_query_logs 召回监控聚合行
//
// 包含 Top-K / Top-1 命中率与平均相似度等监控指标所需字段。
type RagRecallAggRow struct {
	Total         int64
	AvgRecall     float64
	AvgPrecision  float64
	AvgLatency    float64
	AvgSimilarity float64
	ZeroHit       int64
	LowRecall     int64
	Top1Hit       int64
}

// RagRecallMonitorRepository RAG 召回率监控仓储接口
type RagRecallMonitorRepository interface {
	AggregateRecallLogs(ctx context.Context, start, end time.Time) (*RagRecallAggRow, error)
	PluckP95Latency(ctx context.Context, start, end time.Time, offset int) (int64, error)
	CreateSnapshot(ctx context.Context, row map[string]any) error
	ListSnapshots(ctx context.Context, limit int) ([]map[string]any, error)
	EnsureSchema(ctx context.Context) error
}

type ragRecallMonitorRepo struct {
	db *gorm.DB
}

// NewRagRecallMonitorRepository 创建 RAG 召回率监控仓储
//
// ARC-01：db 为 nil 时返回 nil 接口，service 侧以 repo == nil 作为未初始化守卫。
func NewRagRecallMonitorRepository(db *gorm.DB) RagRecallMonitorRepository {
	if db == nil {
		return nil
	}
	return &ragRecallMonitorRepo{db: db}
}

func (r *ragRecallMonitorRepo) AggregateRecallLogs(ctx context.Context, start, end time.Time) (*RagRecallAggRow, error) {
	row := &RagRecallAggRow{}
	if err := r.db.WithContext(ctx).
		Table("rag_query_logs").
		Where("created_at >= ? AND created_at < ?", start, end).
		Select(`
			COUNT(*) AS total,
			COALESCE(AVG(recall), 0) AS avg_recall,
			COALESCE(AVG(precision), 0) AS avg_precision,
			COALESCE(AVG(latency_ms), 0) AS avg_latency,
			COALESCE(AVG(top_similarity), 0) AS avg_similarity,
			COUNT(*) FILTER (WHERE retrieved_count = 0) AS zero_hit,
			COUNT(*) FILTER (WHERE recall < 0.3) AS low_recall,
			COUNT(*) FILTER (WHERE hit_in_top1 = true) AS top1_hit
		`).
		Scan(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (r *ragRecallMonitorRepo) PluckP95Latency(ctx context.Context, start, end time.Time, offset int) (int64, error) {
	var p95 int64
	if err := r.db.WithContext(ctx).
		Table("rag_query_logs").
		Where("created_at >= ? AND created_at < ?", start, end).
		Order("latency_ms ASC").
		Offset(offset).
		Limit(1).
		Pluck("latency_ms", &p95).Error; err != nil {
		return 0, err
	}
	return p95, nil
}

func (r *ragRecallMonitorRepo) CreateSnapshot(ctx context.Context, row map[string]any) error {
	return r.db.WithContext(ctx).Table(RagRecallMonitorTableName).Create(row).Error
}

func (r *ragRecallMonitorRepo) ListSnapshots(ctx context.Context, limit int) ([]map[string]any, error) {
	var rows []map[string]any
	if err := r.db.WithContext(ctx).
		Table(RagRecallMonitorTableName).
		Order("window_start DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *ragRecallMonitorRepo) EnsureSchema(ctx context.Context) error {
	stmt := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			window_start TIMESTAMPTZ NOT NULL,
			window_end TIMESTAMPTZ NOT NULL,
			total_queries BIGINT NOT NULL DEFAULT 0,
			top_k_hit_rate NUMERIC(10,6) NOT NULL DEFAULT 0,
			top_1_hit_rate NUMERIC(10,6) NOT NULL DEFAULT 0,
			avg_recall NUMERIC(10,6) NOT NULL DEFAULT 0,
			avg_precision NUMERIC(10,6) NOT NULL DEFAULT 0,
			avg_similarity NUMERIC(10,6) NOT NULL DEFAULT 0,
			avg_latency_ms NUMERIC(12,2) NOT NULL DEFAULT 0,
			p95_latency_ms BIGINT NOT NULL DEFAULT 0,
			zero_hit_count BIGINT NOT NULL DEFAULT 0,
			low_recall_count BIGINT NOT NULL DEFAULT 0,
			payload TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE INDEX IF NOT EXISTS idx_rag_recall_monitor_window ON %s (window_start DESC);
	`, RagRecallMonitorTableName, RagRecallMonitorTableName)
	return r.db.WithContext(ctx).Exec(stmt).Error
}

var _ RagRecallMonitorRepository = (*ragRecallMonitorRepo)(nil)
