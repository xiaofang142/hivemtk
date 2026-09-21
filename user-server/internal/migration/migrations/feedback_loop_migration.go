package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// FeedbackLoopMigration 反馈学习闭环迁移 v3.0.0
type FeedbackLoopMigration struct {
	db *gorm.DB
}

// NewFeedbackLoopMigration 创建迁移实例
func NewFeedbackLoopMigration(db *gorm.DB) *FeedbackLoopMigration {
	return &FeedbackLoopMigration{db: db}
}

// Version 返回版本号
func (m *FeedbackLoopMigration) Version() string { return "v3.0.0" }

// Name 返回迁移名称
func (m *FeedbackLoopMigration) Name() string {
	return "反馈学习闭环（6 张新表 + 2 张表扩展）"
}

// Description 返回迁移描述
func (m *FeedbackLoopMigration) Description() string {
	return "创建 feedback_events/feedback_signals/champion_dialogues/prompt_candidates/bandit_arms/prompt_ab_tests 6 张表，并扩展 sop_agents/script_templates 字段"
}

// Up 执行升级
func (m *FeedbackLoopMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}

	if err := m.createFeedbackEvents(ctx); err != nil {
		return fmt.Errorf("create feedback_events 失败: %w", err)
	}

	if err := m.createFeedbackSignals(ctx); err != nil {
		return fmt.Errorf("create feedback_signals 失败: %w", err)
	}

	if err := m.createChampionDialogues(ctx); err != nil {
		return fmt.Errorf("create champion_dialogues 失败: %w", err)
	}

	if err := m.createPromptCandidates(ctx); err != nil {
		return fmt.Errorf("create prompt_candidates 失败: %w", err)
	}

	if err := m.createBanditArms(ctx); err != nil {
		return fmt.Errorf("create bandit_arms 失败: %w", err)
	}

	if err := m.createPromptABTests(ctx); err != nil {
		return fmt.Errorf("create prompt_ab_tests 失败: %w", err)
	}

	if err := m.alterSOPAgents(ctx); err != nil {
		return fmt.Errorf("alter sop_agents 失败: %w", err)
	}

	if err := m.alterScriptTemplates(ctx); err != nil {
		return fmt.Errorf("alter script_templates 失败: %w", err)
	}

	return nil
}

func (m *FeedbackLoopMigration) createFeedbackEvents(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS feedback_events (
			id                BIGSERIAL PRIMARY KEY,
			event_id          VARCHAR(64) NOT NULL UNIQUE,
			session_id        VARCHAR(50) NOT NULL,
			customer_id       VARCHAR(64) NOT NULL,
			sop_id            BIGINT DEFAULT 0,
			execution_id      BIGINT DEFAULT 0,
			variant           VARCHAR(50) DEFAULT '',
			prompt_candidate_id BIGINT DEFAULT 0,
			event_type        VARCHAR(30) NOT NULL,
			signal_key        VARCHAR(50) NOT NULL,
			signal_value      JSONB NOT NULL,
			weight            DECIMAL(4,2) NOT NULL DEFAULT 0,
			reward            DECIMAL(6,3) NOT NULL DEFAULT 0,
			ai_reply          TEXT DEFAULT '',
			customer_msg      TEXT DEFAULT '',
			metadata          JSONB DEFAULT '{}',
			created_by        BIGINT DEFAULT 0,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_events_session ON feedback_events(session_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_events_sop_variant ON feedback_events(sop_id, variant, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_events_prompt ON feedback_events(prompt_candidate_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_events_type_signal ON feedback_events(event_type, signal_key, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_events_customer ON feedback_events(customer_id, created_at)`,
	}
	return execAllFeedbackLoop(ctx, m.db, stmts)
}

func (m *FeedbackLoopMigration) createFeedbackSignals(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS feedback_signals (
			id                BIGSERIAL PRIMARY KEY,
			session_id        VARCHAR(50) NOT NULL UNIQUE,
			customer_id       VARCHAR(64) NOT NULL,
			sop_id            BIGINT DEFAULT 0,
			variant           VARCHAR(50) DEFAULT '',
			prompt_candidate_id BIGINT DEFAULT 0,
			aggregated_reward DECIMAL(8,3) NOT NULL DEFAULT 0,
			signal_count      INT NOT NULL DEFAULT 0,
			signal_breakdown  JSONB NOT NULL DEFAULT '{}',
			outcome           VARCHAR(20) NOT NULL DEFAULT 'pending',
			is_champion       BOOLEAN NOT NULL DEFAULT FALSE,
			session_started_at TIMESTAMPTZ,
			session_ended_at   TIMESTAMPTZ,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_signals_sop_variant ON feedback_signals(sop_id, variant, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_signals_prompt ON feedback_signals(prompt_candidate_id, outcome, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_signals_champion ON feedback_signals(is_champion, created_at) WHERE is_champion = TRUE`,
	}
	return execAllFeedbackLoop(ctx, m.db, stmts)
}

func (m *FeedbackLoopMigration) createChampionDialogues(ctx context.Context) error {
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`CREATE TABLE IF NOT EXISTS champion_dialogues (
			id                  BIGSERIAL PRIMARY KEY,
			dialogue_fingerprint VARCHAR(64) NOT NULL UNIQUE,
			session_id          VARCHAR(50) NOT NULL,
			customer_id         VARCHAR(64) NOT NULL,
			staff_id            BIGINT DEFAULT 0,
			staff_name          VARCHAR(100) DEFAULT '',
			scenario            VARCHAR(50) NOT NULL,
			journey_stage       VARCHAR(30) DEFAULT '',
			customer_msg        TEXT NOT NULL,
			champion_reply      TEXT NOT NULL,
			context_msgs        JSONB DEFAULT '{}',
			embedding           vector(1024) NOT NULL,
			cluster_id          BIGINT DEFAULT 0,
			reward              DECIMAL(6,3) NOT NULL DEFAULT 0,
			conversion_achieved BOOLEAN NOT NULL DEFAULT FALSE,
			extracted_scripts   JSONB DEFAULT '[]',
			extracted_at        TIMESTAMPTZ,
			created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_champion_dialogues_embedding ON champion_dialogues USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)`,
		`CREATE INDEX IF NOT EXISTS idx_champion_dialogues_cluster ON champion_dialogues(cluster_id, reward DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_champion_dialogues_scenario ON champion_dialogues(scenario, journey_stage, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_champion_dialogues_staff ON champion_dialogues(staff_id, created_at)`,
	}
	return execAllFeedbackLoop(ctx, m.db, stmts)
}

func (m *FeedbackLoopMigration) createPromptCandidates(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS prompt_candidates (
			id                  BIGSERIAL PRIMARY KEY,
			sop_node_id         VARCHAR(50) DEFAULT '',
			sop_id              BIGINT DEFAULT 0,
			scenario            VARCHAR(50) NOT NULL,
			version             VARCHAR(20) NOT NULL,
			title               VARCHAR(100) NOT NULL,
			system_prompt       TEXT NOT NULL,
			user_prompt_template TEXT NOT NULL,
			variables           JSONB DEFAULT '{}',
			parent_id           BIGINT DEFAULT 0,
			improvement_notes   TEXT DEFAULT '',
			status              VARCHAR(20) NOT NULL DEFAULT 'draft',
			alpha               DECIMAL(8,2) NOT NULL DEFAULT 2,
			beta                DECIMAL(8,2) NOT NULL DEFAULT 2,
			sample_count        INT NOT NULL DEFAULT 0,
			success_count       INT NOT NULL DEFAULT 0,
			avg_reward          DECIMAL(6,3) NOT NULL DEFAULT 0,
			promoted_at         TIMESTAMPTZ,
			retired_at          TIMESTAMPTZ,
			retired_reason      VARCHAR(100) DEFAULT '',
			reviewed_by         BIGINT DEFAULT 0,
			reviewed_at         TIMESTAMPTZ,
			generated_by        VARCHAR(20) NOT NULL DEFAULT 'auto',
			created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_prompt_candidates_node ON prompt_candidates(sop_node_id, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_prompt_candidates_scenario ON prompt_candidates(scenario, status)`,
		`CREATE INDEX IF NOT EXISTS idx_prompt_candidates_parent ON prompt_candidates(parent_id)`,
	}
	return execAllFeedbackLoop(ctx, m.db, stmts)
}

func (m *FeedbackLoopMigration) createBanditArms(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS bandit_arms (
			id                BIGSERIAL PRIMARY KEY,
			experiment_id     VARCHAR(64) NOT NULL,
			experiment_type   VARCHAR(20) NOT NULL,
			arm_key           VARCHAR(100) NOT NULL,
			sop_id            BIGINT DEFAULT 0,
			variant           VARCHAR(50) DEFAULT '',
			prompt_candidate_id BIGINT DEFAULT 0,
			alpha             DECIMAL(10,2) NOT NULL DEFAULT 1,
			beta              DECIMAL(10,2) NOT NULL DEFAULT 1,
			total_trials      BIGINT NOT NULL DEFAULT 0,
			success_trials    BIGINT NOT NULL DEFAULT 0,
			sum_reward        DECIMAL(12,3) NOT NULL DEFAULT 0,
			avg_reward        DECIMAL(8,4) NOT NULL DEFAULT 0,
			min_traffic_pct   DECIMAL(5,2) NOT NULL DEFAULT 10.00,
			max_traffic_pct   DECIMAL(5,2) NOT NULL DEFAULT 60.00,
			current_traffic_pct DECIMAL(5,2) NOT NULL DEFAULT 0,
			status            VARCHAR(20) NOT NULL DEFAULT 'exploring',
			promoted_at       TIMESTAMPTZ,
			retired_at        TIMESTAMPTZ,
			posterior_best_prob DECIMAL(5,4),
			last_sampled_at   TIMESTAMPTZ,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(experiment_id, arm_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bandit_arms_experiment ON bandit_arms(experiment_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_bandit_arms_sop ON bandit_arms(sop_id, status)`,
	}
	return execAllFeedbackLoop(ctx, m.db, stmts)
}

func (m *FeedbackLoopMigration) createPromptABTests(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS prompt_ab_tests (
			id              BIGSERIAL PRIMARY KEY,
			experiment_id   VARCHAR(64) NOT NULL UNIQUE,
			experiment_type VARCHAR(20) NOT NULL,
			sop_id          BIGINT DEFAULT 0,
			sop_node_id     VARCHAR(50) DEFAULT '',
			name            VARCHAR(100) NOT NULL,
			description     TEXT DEFAULT '',
			arm_keys        JSONB NOT NULL,
			config          JSONB NOT NULL,
			status          VARCHAR(20) NOT NULL DEFAULT 'running',
			started_at      TIMESTAMPTZ,
			ended_at        TIMESTAMPTZ,
			winner_arm_key  VARCHAR(100) DEFAULT '',
			auto_promote    BOOLEAN NOT NULL DEFAULT TRUE,
			created_by      BIGINT DEFAULT 0,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_prompt_ab_tests_status ON prompt_ab_tests(status, started_at)`,
	}
	return execAllFeedbackLoop(ctx, m.db, stmts)
}

func (m *FeedbackLoopMigration) alterSOPAgents(ctx context.Context) error {
	stmt := `DO $$
BEGIN
	IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'sop_agents' AND column_name = 'use_bandit') THEN
		ALTER TABLE sop_agents ADD COLUMN IF NOT EXISTS use_bandit BOOLEAN NOT NULL DEFAULT FALSE;
	END IF;
END $$`
	return execAllFeedbackLoop(ctx, m.db, []string{stmt})
}

func (m *FeedbackLoopMigration) alterScriptTemplates(ctx context.Context) error {
	stmts := []string{
		`DO $$
BEGIN
	IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'script_templates' AND column_name = 'source') THEN
		ALTER TABLE script_templates ADD COLUMN IF NOT EXISTS source VARCHAR(20) NOT NULL DEFAULT 'manual';
	END IF;
	IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'script_templates' AND column_name = 'effectiveness_score') THEN
		ALTER TABLE script_templates ADD COLUMN IF NOT EXISTS effectiveness_score DECIMAL(3,2) NOT NULL DEFAULT 0;
	END IF;
	IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'script_templates' AND column_name = 'trigger_keywords') THEN
		ALTER TABLE script_templates ADD COLUMN IF NOT EXISTS trigger_keywords VARCHAR(500) DEFAULT '';
	END IF;
	IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'script_templates' AND column_name = 'journey_stage') THEN
		ALTER TABLE script_templates ADD COLUMN IF NOT EXISTS journey_stage VARCHAR(30) DEFAULT '';
	END IF;
	IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'script_templates' AND column_name = 'champion_dialogue_id') THEN
		ALTER TABLE script_templates ADD COLUMN IF NOT EXISTS champion_dialogue_id BIGINT DEFAULT 0;
	END IF;
END $$`,
		`CREATE INDEX IF NOT EXISTS idx_script_templates_source ON script_templates(source, journey_stage, effectiveness_score DESC)`,
	}
	return execAllFeedbackLoop(ctx, m.db, stmts)
}

// Down 回滚（删除新表 + 扩展字段）
//
// 注意：
//   - 6 张新表可安全删除
//   - sop_agents.use_bandit 字段删除（不丢业务数据）
//   - script_templates 的扩展字段删除（不丢业务数据）
func (m *FeedbackLoopMigration) Down(ctx context.Context) error {
	stmts := []string{
		`DROP INDEX IF EXISTS idx_script_templates_source`,
		`ALTER TABLE script_templates DROP COLUMN IF EXISTS champion_dialogue_id`,
		`ALTER TABLE script_templates DROP COLUMN IF EXISTS journey_stage`,
		`ALTER TABLE script_templates DROP COLUMN IF EXISTS trigger_keywords`,
		`ALTER TABLE script_templates DROP COLUMN IF EXISTS effectiveness_score`,
		`ALTER TABLE script_templates DROP COLUMN IF EXISTS source`,
		`ALTER TABLE sop_agents DROP COLUMN IF EXISTS use_bandit`,
	}
	declineTableDrop(m.Version(), "prompt_ab_tests", "bandit_arms", "prompt_candidates",
		"champion_dialogues", "feedback_signals", "feedback_events")
	return execAllFeedbackLoop(ctx, m.db, stmts)
}

func execAllFeedbackLoop(ctx context.Context, db *gorm.DB, stmts []string) error {
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

var _ migration.Migration = (*FeedbackLoopMigration)(nil)
