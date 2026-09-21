package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// ChurnScoreMigration D22b：churn_scores 表（BG/NBD 流失概率周批输出）
//
// 每客户一行：BG/NBD 统计量 (x, tx, T) + 拟合参数快照 + P(alive)/E[Y(30)] 输出。
// 周批全量重算 upsert（customer_key 唯一）；params JSONB 存本轮拟合参数，
// 保证分数可复现（不同轮参数不同，重算需当时参数）。
type ChurnScoreMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*ChurnScoreMigration)(nil)

func NewChurnScoreMigration(db *gorm.DB) *ChurnScoreMigration {
	return &ChurnScoreMigration{db: db}
}

func (m *ChurnScoreMigration) Version() string { return "v3.34.0" }

func (m *ChurnScoreMigration) Name() string {
	return "churn_scores 表（BG/NBD 流失评分）"
}

func (m *ChurnScoreMigration) Description() string {
	return "D22b: customer_key 维度 x/tx/T 统计 + P(alive)/E[Y(30)]，周批 upsert"
}

func (m *ChurnScoreMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS churn_scores (
		id BIGSERIAL PRIMARY KEY,
		customer_key VARCHAR(120) NOT NULL,
		x INT NOT NULL DEFAULT 0,
		tx DOUBLE PRECISION NOT NULL DEFAULT 0,
		t_obs DOUBLE PRECISION NOT NULL DEFAULT 0,
		p_alive DOUBLE PRECISION NOT NULL DEFAULT 0,
		expected_purchases_30d DOUBLE PRECISION NOT NULL DEFAULT 0,
		params JSONB NOT NULL DEFAULT '{}',
		stats_count INT NOT NULL DEFAULT 0,
		computed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT uk_churn_customer UNIQUE (customer_key)
	)`).Error; err != nil {
		return err
	}
	return m.db.WithContext(ctx).Exec(
		`CREATE INDEX IF NOT EXISTS idx_churn_scores_p_alive ON churn_scores(p_alive)`).Error
}

func (m *ChurnScoreMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineTableDrop(m.Version(), "churn_scores")
	return nil
}
