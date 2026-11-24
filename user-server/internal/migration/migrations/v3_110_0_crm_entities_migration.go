package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// CRMEntitiesMigration v3.110.0（T021）：
// 顶层 migrations/093_crm_entities.sql 的 Go 注册入口（SQL 供人工/测试执行，
// Go 迁移供启动链执行）。仅补三张新实体表 + 合并审计表 + 存量表扩展字段；
// 不重建 companies，不重复 080/customer.go/clue.go/companies 已建模的来源字段。
type CRMEntitiesMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*CRMEntitiesMigration)(nil)

// NewCRMEntitiesMigration 构造。
func NewCRMEntitiesMigration(db *gorm.DB) *CRMEntitiesMigration {
	return &CRMEntitiesMigration{db: db}
}

func (m *CRMEntitiesMigration) Version() string { return "v3.110.0" }

func (m *CRMEntitiesMigration) Name() string { return "CRM 统一主数据实体与合并审计" }

func (m *CRMEntitiesMigration) Description() string {
	return "customers/companies 扩展字段 + contacts/suppliers/service_providers/customer_merges 新表（幂等）"
}

// Up 全部幂等：重复执行不会重建存量表，也不会重复添加来源字段。
func (m *CRMEntitiesMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("crm entities migration: db 未初始化")
	}
	stmts := []string{
		`ALTER TABLE customers ADD COLUMN IF NOT EXISTS industry VARCHAR(50)`,
		`ALTER TABLE customers ADD COLUMN IF NOT EXISTS owner_user_id BIGINT`,
		`ALTER TABLE customers ADD COLUMN IF NOT EXISTS merged_into_id VARCHAR(36)`,
		`CREATE INDEX IF NOT EXISTS idx_customers_industry ON customers(industry)`,
		`CREATE INDEX IF NOT EXISTS idx_customers_owner_user ON customers(owner_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_customers_merged_into ON customers(merged_into_id)`,
		`ALTER TABLE companies ADD COLUMN IF NOT EXISTS region VARCHAR(64)`,
		`CREATE INDEX IF NOT EXISTS idx_companies_region ON companies(region) WHERE deleted_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS contacts (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL DEFAULT 1,
			name VARCHAR(100) NOT NULL,
			customer_id VARCHAR(64) NOT NULL,
			channel_account_id BIGINT,
			contact_info JSONB NOT NULL DEFAULT '{}'::jsonb,
			role VARCHAR(64) NOT NULL DEFAULT '',
			remark TEXT NOT NULL DEFAULT '',
			source VARCHAR(64) NOT NULL DEFAULT '',
			source_ref VARCHAR(255) NOT NULL DEFAULT '',
			created_by BIGINT,
			updated_by BIGINT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_contacts_tenant_customer ON contacts(tenant_id, customer_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_contacts_tenant_account ON contacts(tenant_id, channel_account_id) WHERE deleted_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS suppliers (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL DEFAULT 1,
			name VARCHAR(200) NOT NULL,
			customer_id VARCHAR(64) NOT NULL,
			channel_account_id BIGINT,
			contact_info JSONB NOT NULL DEFAULT '{}'::jsonb,
			role VARCHAR(64) NOT NULL DEFAULT '',
			remark TEXT NOT NULL DEFAULT '',
			source VARCHAR(64) NOT NULL DEFAULT '',
			source_ref VARCHAR(255) NOT NULL DEFAULT '',
			created_by BIGINT,
			updated_by BIGINT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_suppliers_tenant_customer ON suppliers(tenant_id, customer_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_suppliers_tenant_account ON suppliers(tenant_id, channel_account_id) WHERE deleted_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS service_providers (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL DEFAULT 1,
			name VARCHAR(200) NOT NULL,
			customer_id VARCHAR(64) NOT NULL,
			channel_account_id BIGINT,
			contact_info JSONB NOT NULL DEFAULT '{}'::jsonb,
			role VARCHAR(64) NOT NULL DEFAULT '',
			remark TEXT NOT NULL DEFAULT '',
			source VARCHAR(64) NOT NULL DEFAULT '',
			source_ref VARCHAR(255) NOT NULL DEFAULT '',
			created_by BIGINT,
			updated_by BIGINT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_service_providers_tenant_customer ON service_providers(tenant_id, customer_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_service_providers_tenant_account ON service_providers(tenant_id, channel_account_id) WHERE deleted_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS customer_merges (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL DEFAULT 1,
			merge_id VARCHAR(32) NOT NULL,
			one_id VARCHAR(128) NOT NULL DEFAULT '',
			primary_id VARCHAR(36) NOT NULL,
			secondary_id VARCHAR(36) NOT NULL,
			source VARCHAR(64) NOT NULL DEFAULT '',
			rule_name VARCHAR(100) NOT NULL DEFAULT '',
			action VARCHAR(16) NOT NULL DEFAULT 'auto',
			original_values JSONB NOT NULL DEFAULT '{}'::jsonb,
			reason TEXT NOT NULL DEFAULT '',
			operator_id BIGINT,
			operator_name VARCHAR(64) NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_merges_tenant_merge ON customer_merges(tenant_id, merge_id)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_merges_tenant_one ON customer_merges(tenant_id, one_id)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_merges_tenant_primary ON customer_merges(tenant_id, primary_id)`,
	}
	for _, stmt := range stmts {
		if err := m.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("crm entities 迁移失败: %w", err)
		}
	}
	return nil
}

// Down 不可回退：合并审计与补缺实体可能已写入业务数据，删除会丢证据。
func (m *CRMEntitiesMigration) Down(_ context.Context) error { return nil }
