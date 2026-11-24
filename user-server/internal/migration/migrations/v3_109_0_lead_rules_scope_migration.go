package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// LeadRulesScopeMigration v3.109.0（T014）：
// lead_mining_configs 是「每租户一行」（主键 = tenant_id，不是全局 id=1）。
// 三粒度规则落地时给每个存量租户生成一条 global 规则，且不碰原配置行。
// 与顶层 migrations/091_lead_rules_migrate.sql 是同一份契约的 Go 注册入口
// （SQL 文件供人工/测试执行，Go 迁移供启动链执行）。
type LeadRulesScopeMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*LeadRulesScopeMigration)(nil)

// NewLeadRulesScopeMigration 构造。
func NewLeadRulesScopeMigration(db *gorm.DB) *LeadRulesScopeMigration {
	return &LeadRulesScopeMigration{db: db}
}

func (m *LeadRulesScopeMigration) Version() string { return "v3.109.0" }

func (m *LeadRulesScopeMigration) Name() string {
	return "线索规则三粒度（global/channel/account）存量回填"
}

func (m *LeadRulesScopeMigration) Description() string {
	return "lead_mining_configs 加 scope/scope_id；每租户回填一条 global lead_mining_rules，不丢存量配置"
}

// Up 幂等：重复执行不会重复生成规则，也不会覆盖手工改过的配置。
func (m *LeadRulesScopeMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("lead rules scope migration: db 未初始化")
	}
	stmts := []string{
		`ALTER TABLE lead_mining_configs ADD COLUMN IF NOT EXISTS scope VARCHAR(16) NOT NULL DEFAULT 'global'`,
		`ALTER TABLE lead_mining_configs ADD COLUMN IF NOT EXISTS scope_id BIGINT NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS lead_mining_rules (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL DEFAULT 1,
			scope VARCHAR(16) NOT NULL DEFAULT 'global',
			scope_id BIGINT NOT NULL DEFAULT 0,
			keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
			semantic_keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
			system_description TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_lead_mining_rules_scope
			ON lead_mining_rules (tenant_id, scope, scope_id)`,
		`INSERT INTO lead_mining_rules
			(tenant_id, scope, scope_id, keywords, semantic_keywords, system_description, enabled, created_at, updated_at)
		 SELECT c.tenant_id, 'global', 0,
		        CASE WHEN c.keywords ~ '^\s*\[' THEN c.keywords::jsonb ELSE '[]'::jsonb END,
		        '[]'::jsonb, COALESCE(c.requirement, ''), c.enabled, now(), now()
		   FROM lead_mining_configs c
		  WHERE NOT EXISTS (SELECT 1 FROM lead_mining_rules r
		                     WHERE r.tenant_id = c.tenant_id AND r.scope = 'global' AND r.scope_id = 0)
		 ON CONFLICT DO NOTHING`,
		`UPDATE lead_mining_configs SET scope = 'global', scope_id = 0
		  WHERE scope IS NULL OR scope = '' OR scope_id IS NULL`,
	}
	for _, stmt := range stmts {
		if err := m.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("lead rules scope 迁移失败: %w", err)
		}
	}
	return nil
}

// Down 不可回退：回填出的规则可能被人工改过，删除会丢数据。
func (m *LeadRulesScopeMigration) Down(_ context.Context) error { return nil }
