package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// LeadMiningMigration v3.111.0（T034）：
// 顶层 migrations/094_lead_mining.sql 的 Go 注册入口。覆盖三路证据、二次判定扩展、
// 通知路由三级回退、共享对话上下文与 clues 归档字段。全部幂等，可重复执行。
// lead_mining_rules 由 v3.109.0 建立，本迁移只做 IF NOT EXISTS 兜底，不改其列定义。
type LeadMiningMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*LeadMiningMigration)(nil)

func NewLeadMiningMigration(db *gorm.DB) *LeadMiningMigration {
	return &LeadMiningMigration{db: db}
}

func (m *LeadMiningMigration) Version() string { return "v3.111.0" }

func (m *LeadMiningMigration) Name() string { return "US2 线索全链路审计扩展" }

func (m *LeadMiningMigration) Description() string {
	return "新增 lead_mining_hits / conversation_contexts，扩展 judgements / notify_routes / clues（与 migrations/094 同契约）"
}

func (m *LeadMiningMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("lead mining migration: db 未初始化")
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS lead_mining_rules (
			id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL DEFAULT 1,
			scope VARCHAR(16) NOT NULL DEFAULT 'global', scope_id BIGINT NOT NULL DEFAULT 0,
			keywords JSONB NOT NULL DEFAULT '[]'::jsonb, semantic_keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
			system_description TEXT NOT NULL DEFAULT '', enabled BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_lead_mining_rules_scope ON lead_mining_rules (tenant_id, scope, scope_id)`,
		`CREATE TABLE IF NOT EXISTS lead_mining_hits (
			id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL DEFAULT 1,
			judgement_id VARCHAR(32) NOT NULL DEFAULT '', type VARCHAR(16) NOT NULL DEFAULT '',
			payload JSONB NOT NULL DEFAULT '{}'::jsonb, evidence_message_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
			complete BOOLEAN NOT NULL DEFAULT true, degraded BOOLEAN NOT NULL DEFAULT false,
			reason TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_lmh_tenant_judgement ON lead_mining_hits (tenant_id, judgement_id)`,
		`CREATE INDEX IF NOT EXISTS idx_lmh_tenant_type ON lead_mining_hits (tenant_id, type)`,
		`ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS is_clue BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS clue_type VARCHAR(32) NOT NULL DEFAULT ''`,
		`ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS summary TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS context_message_ids JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS source_hit_ids JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS evidence_complete BOOLEAN NOT NULL DEFAULT true`,
		`ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS degraded BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS channel_account_id BIGINT NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS idx_lmj_tenant_is_clue ON lead_mining_judgements (tenant_id, is_clue)`,
		`CREATE INDEX IF NOT EXISTS idx_lmj_tenant_channel_account ON lead_mining_judgements (tenant_id, channel_account_id)`,
		`ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS channel_account_id BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS scope VARCHAR(16) NOT NULL DEFAULT 'tenant'`,
		`ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS priority INT NOT NULL DEFAULT 0`,
		`ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS notify_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS notify_types JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS delivery_records JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`CREATE INDEX IF NOT EXISTS idx_lmnr_tenant_account ON lead_mining_notify_routes (tenant_id, channel_account_id)`,
		`CREATE TABLE IF NOT EXISTS conversation_contexts (
			id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL DEFAULT 1,
			channel_account_id BIGINT NOT NULL DEFAULT 0, external_conversation_id VARCHAR(128) NOT NULL DEFAULT '',
			member_roles JSONB NOT NULL DEFAULT '{}'::jsonb, messages JSONB NOT NULL DEFAULT '[]'::jsonb,
			tags JSONB NOT NULL DEFAULT '[]'::jsonb, summary TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_conv_ctx_tenant_account_conv ON conversation_contexts (tenant_id, channel_account_id, external_conversation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_conv_ctx_tenant_conv ON conversation_contexts (tenant_id, external_conversation_id)`,
		`ALTER TABLE clues ADD COLUMN IF NOT EXISTS customer_id VARCHAR(64) NOT NULL DEFAULT ''`,
		`ALTER TABLE clues ADD COLUMN IF NOT EXISTS user_info JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`ALTER TABLE clues ADD COLUMN IF NOT EXISTS source_hit_ids JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`ALTER TABLE clues ADD COLUMN IF NOT EXISTS archived_at BIGINT`,
		`CREATE INDEX IF NOT EXISTS idx_clues_tenant_customer ON clues (tenant_id, customer_id)`,
		`CREATE INDEX IF NOT EXISTS idx_clues_tenant_archived ON clues (tenant_id, archived_at)`,
	}
	for _, stmt := range stmts {
		if err := m.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("lead mining 迁移失败: %w", err)
		}
	}
	return nil
}

func (m *LeadMiningMigration) Down(_ context.Context) error { return nil }
