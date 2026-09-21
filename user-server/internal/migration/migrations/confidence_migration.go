package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// ConfidenceMigration 置信度驱动转人工迁移 v2.8.0
type ConfidenceMigration struct {
	db *gorm.DB
}

// NewConfidenceMigration 创建迁移实例
func NewConfidenceMigration(db *gorm.DB) *ConfidenceMigration {
	return &ConfidenceMigration{db: db}
}

// Version 返回版本号
func (m *ConfidenceMigration) Version() string { return "v2.8.0" }

// Name 返回迁移名称
func (m *ConfidenceMigration) Name() string { return "置信度驱动转人工（7+1 张表）" }

// Description 返回迁移描述
func (m *ConfidenceMigration) Description() string {
	return "创建 confidence_signals / confidence_calibrations / handoff_decisions / threshold_policies / ab_tests / ab_test_metrics 6 张表"
}

// Up 执行升级
func (m *ConfidenceMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}

	if err := m.createConfidenceSignals(ctx); err != nil {
		return fmt.Errorf("create confidence_signals 失败: %w", err)
	}

	if err := m.createConfidenceCalibrations(ctx); err != nil {
		return fmt.Errorf("create confidence_calibrations 失败: %w", err)
	}

	if err := m.createHandoffDecisions(ctx); err != nil {
		return fmt.Errorf("create handoff_decisions 失败: %w", err)
	}

	if err := m.createThresholdPolicies(ctx); err != nil {
		return fmt.Errorf("create threshold_policies 失败: %w", err)
	}

	if err := m.createABTests(ctx); err != nil {
		return fmt.Errorf("create ab_tests 失败: %w", err)
	}

	if err := m.createABTestMetrics(ctx); err != nil {
		return fmt.Errorf("create ab_test_metrics 失败: %w", err)
	}

	if err := m.seedDefaultPolicies(ctx); err != nil {
		logger.Infof("[ConfidenceMigration] 默认策略种子化提示: %v", err)
	}

	return nil
}

func (m *ConfidenceMigration) createConfidenceSignals(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS confidence_signals (
			id BIGSERIAL PRIMARY KEY,
			signal_id VARCHAR(64) NOT NULL UNIQUE,
			session_id VARCHAR(128) NOT NULL,
			customer_id VARCHAR(128) NOT NULL,
			message_id VARCHAR(128) NOT NULL,
			intent_type VARCHAR(64) NOT NULL,
			intent_conf DECIMAL(5,4) NOT NULL,
			intent_conf_calibrated DECIMAL(5,4) NOT NULL,
			entity_comp DECIMAL(5,4) NOT NULL,
			ctx_relev DECIMAL(5,4) NOT NULL,
			rag_qual DECIMAL(5,4) NOT NULL,
			llm_entropy DECIMAL(5,4) NOT NULL,
			aggregated_conf DECIMAL(5,4) NOT NULL,
			veto_triggered VARCHAR(64) DEFAULT '',
			dynamic_threshold DECIMAL(5,4) NOT NULL,
			decision_band VARCHAR(32) NOT NULL,
			temperature DECIMAL(6,4) NOT NULL DEFAULT 1.0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_signals_session ON confidence_signals(session_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_signals_customer ON confidence_signals(customer_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_signals_band ON confidence_signals(decision_band, created_at DESC)`,
	}
	return execAll(ctx, m.db, stmts)
}

func (m *ConfidenceMigration) createConfidenceCalibrations(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS confidence_calibrations (
			id BIGSERIAL PRIMARY KEY,
			calibration_id VARCHAR(64) NOT NULL UNIQUE,
			signal_type VARCHAR(32) NOT NULL,
			method VARCHAR(32) NOT NULL,
			temperature DECIMAL(6,4) NOT NULL,
			platt_a DECIMAL(8,4) DEFAULT 0,
			platt_b DECIMAL(8,4) DEFAULT 0,
			ece_before DECIMAL(5,4) NOT NULL,
			ece_after DECIMAL(5,4) NOT NULL,
			nll_before DECIMAL(8,4) NOT NULL,
			nll_after DECIMAL(8,4) NOT NULL,
			sample_size INT NOT NULL,
			fit_started_at TIMESTAMPTZ NOT NULL,
			fit_finished_at TIMESTAMPTZ NOT NULL,
			is_active BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_calibrations_active ON confidence_calibrations(signal_type, is_active)`,
	}
	return execAll(ctx, m.db, stmts)
}

func (m *ConfidenceMigration) createHandoffDecisions(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS handoff_decisions (
			id BIGSERIAL PRIMARY KEY,
			decision_id VARCHAR(64) NOT NULL UNIQUE,
			session_id VARCHAR(128) NOT NULL,
			customer_id VARCHAR(128) NOT NULL,
			signal_id VARCHAR(64) NOT NULL,
			reason VARCHAR(64) NOT NULL,
			reason_detail TEXT DEFAULT '',
			confidence DECIMAL(5,4) NOT NULL,
			threshold DECIMAL(5,4) NOT NULL,
			intent_type VARCHAR(64) NOT NULL,
			customer_level VARCHAR(16) DEFAULT 'normal',
			timeslot VARCHAR(16) DEFAULT '',
			agent_availability DECIMAL(5,4) DEFAULT 0,
			assigned_agent_id BIGINT DEFAULT 0,
			assigned_at TIMESTAMPTZ,
			accepted_at TIMESTAMPTZ,
			resolved_at TIMESTAMPTZ,
			customer_accepted BOOLEAN DEFAULT FALSE,
			sla_breached BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_handoff_session ON handoff_decisions(session_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_handoff_agent ON handoff_decisions(assigned_agent_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_handoff_sla ON handoff_decisions(sla_breached, created_at DESC)`,
	}
	return execAll(ctx, m.db, stmts)
}

func (m *ConfidenceMigration) createThresholdPolicies(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS threshold_policies (
			id BIGSERIAL PRIMARY KEY,
			policy_id VARCHAR(64) NOT NULL UNIQUE,
			intent_type VARCHAR(64) NOT NULL,
			base_threshold DECIMAL(5,4) NOT NULL,
			customer_level_weight DECIMAL(5,4) NOT NULL DEFAULT 0.05,
			timeslot_weight DECIMAL(5,4) NOT NULL DEFAULT 0.05,
			agent_availability_weight DECIMAL(5,4) NOT NULL DEFAULT 0.10,
			band_handoff_upper DECIMAL(5,4) NOT NULL DEFAULT 0.40,
			band_fallback_upper DECIMAL(5,4) NOT NULL DEFAULT 0.60,
			band_review_upper DECIMAL(5,4) NOT NULL DEFAULT 0.75,
			review_sla_seconds INT NOT NULL DEFAULT 30,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			version INT NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_policies_intent ON threshold_policies(intent_type, is_active, version DESC)`,
	}
	return execAll(ctx, m.db, stmts)
}

func (m *ConfidenceMigration) createABTests(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ab_tests (
			id BIGSERIAL PRIMARY KEY,
			test_id VARCHAR(64) NOT NULL UNIQUE,
			test_name VARCHAR(128) NOT NULL,
			description TEXT DEFAULT '',
			status VARCHAR(32) NOT NULL DEFAULT 'draft',
			traffic_split JSONB NOT NULL,
			targeting_rule JSONB DEFAULT '{}',
			metrics JSONB NOT NULL,
			control_stats JSONB DEFAULT '{}',
			treatment_stats JSONB DEFAULT '{}',
			mann_whitney_u DECIMAL(10,4) DEFAULT 0,
			mann_whitney_p DECIMAL(8,4) DEFAULT 0,
			bootstrap_ci_lower DECIMAL(5,4) DEFAULT 0,
			bootstrap_ci_upper DECIMAL(5,4) DEFAULT 0,
			bootstrap_n INT DEFAULT 10000,
			started_at TIMESTAMPTZ,
			stopped_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ab_status ON ab_tests(status, started_at DESC)`,
	}
	return execAll(ctx, m.db, stmts)
}

func (m *ConfidenceMigration) createABTestMetrics(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ab_test_metrics (
			id BIGSERIAL PRIMARY KEY,
			test_id VARCHAR(64) NOT NULL,
			group_name VARCHAR(16) NOT NULL,
			metric_name VARCHAR(64) NOT NULL,
			value DECIMAL(15,6) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_abm_test ON ab_test_metrics(test_id, group_name, metric_name, created_at)`,
	}
	return execAll(ctx, m.db, stmts)
}

func (m *ConfidenceMigration) seedDefaultPolicies(ctx context.Context) error {
	var count int64
	if err := m.db.WithContext(ctx).
		Raw(`SELECT COUNT(*) FROM threshold_policies`).
		Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	policies := []struct {
		intentType string
		base       float64
	}{
		{"default", 0.70},
		{"complaint", 0.85},
		{"churn", 0.85},
		{"objection", 0.75},
		{"ask_product", 0.70},
		{"ask_service", 0.70},
		{"price_inquiry", 0.65},
		{"purchase", 0.65},
		{"after_sale", 0.80},
		{"social", 0.50},
		{"greeting", 0.50},
	}
	for _, p := range policies {
		if err := m.db.WithContext(ctx).Exec(`
			INSERT INTO threshold_policies
				(policy_id, intent_type, base_threshold, customer_level_weight,
				 timeslot_weight, agent_availability_weight,
				 band_handoff_upper, band_fallback_upper, band_review_upper,
				 review_sla_seconds, is_active, version)
			VALUES ($1, $2, $3, 0.05, 0.05, 0.10, 0.40, 0.60, 0.75, 30, TRUE, 1)
		`, "policy_"+p.intentType, p.intentType, p.base).Error; err != nil {
			return err
		}
	}
	return nil
}

// Down 回滚（仅删除新表，业务数据保护）
//
// 注意：
//   - 不删除 confidence_calibrations（历史校准数据有价值）
//   - 不删除 threshold_policies（策略配置有价值）
//   - 其他表可安全删除
func (m *ConfidenceMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineTableDrop(m.Version(), "ab_test_metrics", "ab_tests", "handoff_decisions", "confidence_signals")
	return nil
}

func execAll(ctx context.Context, db *gorm.DB, stmts []string) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	for _, sql := range stmts {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("exec failed (%s): %w", sql, err)
		}
	}
	return nil
}

var _ migration.Migration = (*ConfidenceMigration)(nil)
