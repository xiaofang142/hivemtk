-- 079_company_saas_evolution.sql
-- CRM P0: 演进 053 companies 为 SaaS 可用主数据，并引入 customer_company_relations。
-- 决策默认（待拍板）：D-01 占用 079；D-02 customer.company_id 双写过渡，relation 为主。
-- 幂等：可重复执行。

ALTER TABLE companies ADD COLUMN IF NOT EXISTS tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE companies ADD COLUMN IF NOT EXISTS owner_user_id BIGINT;
ALTER TABLE companies ADD COLUMN IF NOT EXISTS source VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE companies ADD COLUMN IF NOT EXISTS source_ref VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE companies ADD COLUMN IF NOT EXISTS created_by BIGINT;
ALTER TABLE companies ADD COLUMN IF NOT EXISTS updated_by BIGINT;

CREATE INDEX IF NOT EXISTS idx_companies_tenant ON companies(tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_companies_tenant_name ON companies(tenant_id, name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_companies_owner ON companies(owner_user_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS customer_company_relations (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    customer_id VARCHAR(36) NOT NULL,
    company_id BIGINT NOT NULL REFERENCES companies(id),
    relation_type VARCHAR(32) NOT NULL DEFAULT 'primary',
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    source VARCHAR(64) NOT NULL DEFAULT '',
    source_ref VARCHAR(255) NOT NULL DEFAULT '',
    created_by BIGINT,
    updated_by BIGINT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ccr_tenant_customer ON customer_company_relations(tenant_id, customer_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_ccr_tenant_company ON customer_company_relations(tenant_id, company_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_ccr_primary ON customer_company_relations(tenant_id, customer_id, is_primary) WHERE deleted_at IS NULL;

-- 同租户同客户同公司同关系类型仅一条活跃记录
CREATE UNIQUE INDEX IF NOT EXISTS uk_ccr_active
    ON customer_company_relations(tenant_id, customer_id, company_id, relation_type)
    WHERE deleted_at IS NULL;

-- D-02 双写回填：将 customers.company_id 回填为 primary relation（仅当 company_id 非空）
INSERT INTO customer_company_relations (
    tenant_id, customer_id, company_id, relation_type, is_primary, source, created_at, updated_at
)
SELECT
    c.tenant_id,
    c.id,
    c.company_id,
    'primary',
    TRUE,
    'migration_079_backfill',
    NOW(),
    NOW()
FROM customers c
WHERE c.company_id IS NOT NULL
  AND c.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM customer_company_relations r
      WHERE r.tenant_id = c.tenant_id
        AND r.customer_id = c.id
        AND r.company_id = c.company_id
        AND r.relation_type = 'primary'
        AND r.deleted_at IS NULL
  );

COMMENT ON TABLE customer_company_relations IS 'CRM P0: 客户-公司关系表，支持一客户多公司/多角色；is_primary 与 customers.company_id 双写过渡(D-02)';
