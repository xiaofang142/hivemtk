package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// HP1Migration 缺口修复迁移
type HP1Migration struct {
	db *gorm.DB
}

// NewHP1Migration 创建迁移实例
func NewHP1Migration(db *gorm.DB) *HP1Migration {
	return &HP1Migration{db: db}
}

// Version 返回版本号
func (m *HP1Migration) Version() string { return "v3.2.0" }

// Name 返回迁移名称
func (m *HP1Migration) Name() string {
	return "H 域 P1 缺口修复 - 线索评分 + RFM + 流失挽回"
}

// Description 返回迁移描述
func (m *HP1Migration) Description() string {
	return "创建 clue_scores / clue_engagement_events / customer_rfm / recovery_queue 4 张表，支撑线索评分（0-100）、RFM 分层、流失客户自动加入挽回队列"
}

// Up 执行升级
func (m *HP1Migration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}

	if err := m.createClueScores(ctx); err != nil {
		return fmt.Errorf("create clue_scores 失败: %w", err)
	}

	if err := m.createClueEngagementEvents(ctx); err != nil {
		return fmt.Errorf("create clue_engagement_events 失败: %w", err)
	}

	if err := m.createCustomerRFM(ctx); err != nil {
		return fmt.Errorf("create customer_rfm 失败: %w", err)
	}

	if err := m.createRecoveryQueue(ctx); err != nil {
		return fmt.Errorf("create recovery_queue 失败: %w", err)
	}

	return nil
}

func (m *HP1Migration) createClueScores(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS clue_scores (
			id            BIGSERIAL PRIMARY KEY,
			clue_id       VARCHAR(36) NOT NULL UNIQUE,
			account       VARCHAR(255) NOT NULL DEFAULT '',
			total_score   INT NOT NULL DEFAULT 0,
			grade         VARCHAR(8) NOT NULL DEFAULT 'D',
			confidence    INT NOT NULL DEFAULT 0,
			channel_score INT NOT NULL DEFAULT 0,
			verify_score  INT NOT NULL DEFAULT 0,
			profile_score INT NOT NULL DEFAULT 0,
			engagement_score INT NOT NULL DEFAULT 0,
			recency_score INT NOT NULL DEFAULT 0,
			factors_json  TEXT NOT NULL DEFAULT '{}',
			model_version VARCHAR(16) NOT NULL DEFAULT 'h-score-1',
			scored_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_clue_scores_account ON clue_scores(account)`,
		`CREATE INDEX IF NOT EXISTS idx_clue_scores_total ON clue_scores(total_score DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_clue_scores_grade ON clue_scores(grade)`,
	}
	return execAllMP1(ctx, m.db, stmts)
}

func (m *HP1Migration) createClueEngagementEvents(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS clue_engagement_events (
			id         BIGSERIAL PRIMARY KEY,
			clue_id    VARCHAR(36) NOT NULL,
			event_type VARCHAR(32) NOT NULL,
			channel    VARCHAR(32) NOT NULL DEFAULT '',
			payload    TEXT NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_engagement_clue ON clue_engagement_events(clue_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_engagement_type ON clue_engagement_events(event_type)`,
	}
	return execAllMP1(ctx, m.db, stmts)
}

func (m *HP1Migration) createCustomerRFM(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS customer_rfm (
			id              BIGSERIAL PRIMARY KEY,
			customer_id     VARCHAR(36) NOT NULL UNIQUE,
			unified_id      VARCHAR(64) NOT NULL DEFAULT '',
			recency_days    INT NOT NULL DEFAULT 9999,
			frequency       INT NOT NULL DEFAULT 0,
			monetary_total  BIGINT NOT NULL DEFAULT 0,
			avg_order_value BIGINT NOT NULL DEFAULT 0,
			r_score         INT NOT NULL DEFAULT 1,
			f_score         INT NOT NULL DEFAULT 1,
			m_score         INT NOT NULL DEFAULT 1,
			composite_score INT NOT NULL DEFAULT 0,
			segment         VARCHAR(16) NOT NULL DEFAULT 'churn',
			churn_risk_level VARCHAR(8) NOT NULL DEFAULT 'high',
			churn_score     INT NOT NULL DEFAULT 100,
			last_active_at  TIMESTAMPTZ,
			computed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_rfm_unified ON customer_rfm(unified_id)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_rfm_segment ON customer_rfm(segment)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_rfm_composite ON customer_rfm(composite_score DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_rfm_churn ON customer_rfm(churn_risk_level)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_rfm_recency ON customer_rfm(recency_days)`,
	}
	return execAllMP1(ctx, m.db, stmts)
}

func (m *HP1Migration) createRecoveryQueue(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS recovery_queue (
			id              BIGSERIAL PRIMARY KEY,
			customer_id     VARCHAR(36) NOT NULL,
			unified_id      VARCHAR(64) NOT NULL DEFAULT '',
			account         VARCHAR(255) NOT NULL DEFAULT '',
			reason          VARCHAR(32) NOT NULL DEFAULT 'churn',
			strategy        VARCHAR(32) NOT NULL DEFAULT 'sms_coupon',
			priority        INT NOT NULL DEFAULT 5,
			stage           VARCHAR(16) NOT NULL DEFAULT 'queued',
			attempts        INT NOT NULL DEFAULT 0,
			max_attempts    INT NOT NULL DEFAULT 3,
			last_attempt_at TIMESTAMPTZ,
			next_attempt_at TIMESTAMPTZ,
			last_channel    VARCHAR(32) NOT NULL DEFAULT '',
			last_result     VARCHAR(255) NOT NULL DEFAULT '',
			recovered_at    TIMESTAMPTZ,
			recovery_value  BIGINT NOT NULL DEFAULT 0,
			meta_json       TEXT NOT NULL DEFAULT '{}',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_recovery_customer ON recovery_queue(customer_id)`,
		`CREATE INDEX IF NOT EXISTS idx_recovery_unified ON recovery_queue(unified_id)`,
		`CREATE INDEX IF NOT EXISTS idx_recovery_stage ON recovery_queue(stage)`,
		`CREATE INDEX IF NOT EXISTS idx_recovery_priority ON recovery_queue(priority, next_attempt_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_recovery_active ON recovery_queue(customer_id) WHERE stage IN ('queued','running')`,
	}
	return execAllMP1(ctx, m.db, stmts)
}

// Down 回滚
func (m *HP1Migration) Down(ctx context.Context) error {
	declineTableDrop(m.Version(), "recovery_queue", "clue_engagement_events", "clue_scores")
	stmts := []string{
		`DROP TABLE IF EXISTS customer_rfm`,
	}
	return execAllMP1(ctx, m.db, stmts)
}

var _ migration.Migration = (*HP1Migration)(nil)
