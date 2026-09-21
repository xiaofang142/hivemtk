package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// RagMonitoringMigration RAG 监控与风控迁移 v2.8.0
type RagMonitoringMigration struct {
	db *gorm.DB
}

// NewRagMonitoringMigration 创建迁移实例
func NewRagMonitoringMigration(db *gorm.DB) *RagMonitoringMigration {
	return &RagMonitoringMigration{db: db}
}

// Version 返回版本号
func (m *RagMonitoringMigration) Version() string { return "v2.8.1" }

// Name 返回迁移名称
func (m *RagMonitoringMigration) Name() string {
	return "RAG 监控与风控（召回率日志/聚合/预警）"
}

// Description 返回迁移描述
func (m *RagMonitoringMigration) Description() string {
	return "新建 rag_query_logs / rag_metrics_daily 表, 支撑召回率监控、5 分钟聚合、健康度评分 (rag_alerts 表由 v3.17 单独清理)"
}

// Up 执行升级
func (m *RagMonitoringMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}

	if err := m.createRagQueryLogs(ctx); err != nil {
		return fmt.Errorf("create rag_query_logs 失败: %w", err)
	}
	if err := m.createRagMetricsDaily(ctx); err != nil {
		return fmt.Errorf("create rag_metrics_daily 失败: %w", err)
	}
	return nil
}

func (m *RagMonitoringMigration) createRagQueryLogs(ctx context.Context) error {
	stmt := `
		CREATE TABLE IF NOT EXISTS rag_query_logs (
			id                BIGSERIAL    PRIMARY KEY,
			query             TEXT         NOT NULL,
			query_hash        VARCHAR(64),
			session_id        VARCHAR(128),
			product_id        BIGINT       DEFAULT 0,
			retrieved_doc_ids TEXT,
			relevant_doc_ids  TEXT,
			retrieved_count   INT          DEFAULT 0,
			relevant_count    INT          DEFAULT 0,
			hit_count         INT          DEFAULT 0,
			precision         DECIMAL(6,4) DEFAULT 0,
			recall            DECIMAL(6,4) DEFAULT 0,
			latency_ms        BIGINT       DEFAULT 0,
			top_k             INT          DEFAULT 5,
			source            VARCHAR(32)  DEFAULT 'hybrid',
			created_at        TIMESTAMP    NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_rag_query_logs_created ON rag_query_logs(created_at);
		CREATE INDEX IF NOT EXISTS idx_rag_query_logs_session ON rag_query_logs(session_id);
		CREATE INDEX IF NOT EXISTS idx_rag_query_logs_product ON rag_query_logs(product_id);
		CREATE INDEX IF NOT EXISTS idx_rag_query_logs_hash ON rag_query_logs(query_hash);
	`
	return m.db.WithContext(ctx).Exec(stmt).Error
}

func (m *RagMonitoringMigration) createRagMetricsDaily(ctx context.Context) error {
	stmt := `
		CREATE TABLE IF NOT EXISTS rag_metrics_daily (
			id               BIGSERIAL     PRIMARY KEY,
			window_start     TIMESTAMP     NOT NULL,
			window_end       TIMESTAMP     NOT NULL,
			total_queries    BIGINT        DEFAULT 0,
			avg_recall       DECIMAL(6,4)  DEFAULT 0,
			avg_precision    DECIMAL(6,4)  DEFAULT 0,
			avg_latency_ms   DECIMAL(10,2) DEFAULT 0,
			p99_latency_ms   BIGINT        DEFAULT 0,
			zero_hit_count   BIGINT        DEFAULT 0,
			low_recall_count BIGINT        DEFAULT 0,
			created_at       TIMESTAMP     NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_rag_metrics_daily_window ON rag_metrics_daily(window_start);
	`
	return m.db.WithContext(ctx).Exec(stmt).Error
}

// Down 执行降级
//
// 注意：删除监控表会丢失历史数据，但业务核心数据（knowledge_documents 等）不受影响
// 私域部署: rag_alerts 由 v3.17 单独 DROP, 本 Down 不再处理
func (m *RagMonitoringMigration) Down(ctx context.Context) error {
	declineTableDrop(m.Version(), "rag_metrics_daily", "rag_query_logs")
	return nil
}

var _ migration.Migration = (*RagMonitoringMigration)(nil)
