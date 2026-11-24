-- 093_crm_entities.sql
-- T021 / US1：CRM 统一主数据增量。
-- 只补 contact/supplier/service_provider 新表 + customer_merges 审计表，
-- 以及 customers / companies 的扩展字段。
-- 不重建 companies（053+079 已有应用层），不重复来源字段
-- （080 + customer.go:66-69 + clue.go:38-39 + companies 079 已建模，R7-4/5）。
-- 幂等：全部 IF NOT EXISTS，可重复执行。

-- customers 扩展：行业 / 负责人 / 合并指向
ALTER TABLE customers ADD COLUMN IF NOT EXISTS industry VARCHAR(50);
ALTER TABLE customers ADD COLUMN IF NOT EXISTS owner_user_id BIGINT;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS merged_into_id VARCHAR(36);
CREATE INDEX IF NOT EXISTS idx_customers_industry ON customers(industry);
CREATE INDEX IF NOT EXISTS idx_customers_owner_user ON customers(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_customers_merged_into ON customers(merged_into_id);

-- companies 扩展：区域检索维度（Industry 已由 053 建表时存在）
ALTER TABLE companies ADD COLUMN IF NOT EXISTS region VARCHAR(64);
CREATE INDEX IF NOT EXISTS idx_companies_region ON companies(region) WHERE deleted_at IS NULL;

-- 联系人 / 供应商 / 服务商（三张全新建表）
CREATE TABLE IF NOT EXISTS contacts (
    id                  BIGSERIAL    PRIMARY KEY,
    tenant_id           BIGINT       NOT NULL DEFAULT 1,
    name                VARCHAR(100) NOT NULL,
    customer_id         VARCHAR(64)  NOT NULL,
    channel_account_id  BIGINT,
    contact_info        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    role                VARCHAR(64)  NOT NULL DEFAULT '',
    remark              TEXT         NOT NULL DEFAULT '',
    source              VARCHAR(64)  NOT NULL DEFAULT '',
    source_ref          VARCHAR(255) NOT NULL DEFAULT '',
    created_by          BIGINT,
    updated_by          BIGINT,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_contacts_tenant_customer ON contacts(tenant_id, customer_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_contacts_tenant_account ON contacts(tenant_id, channel_account_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS suppliers (
    id                  BIGSERIAL    PRIMARY KEY,
    tenant_id           BIGINT       NOT NULL DEFAULT 1,
    name                VARCHAR(200) NOT NULL,
    customer_id         VARCHAR(64)  NOT NULL,
    channel_account_id  BIGINT,
    contact_info        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    role                VARCHAR(64)  NOT NULL DEFAULT '',
    remark              TEXT         NOT NULL DEFAULT '',
    source              VARCHAR(64)  NOT NULL DEFAULT '',
    source_ref          VARCHAR(255) NOT NULL DEFAULT '',
    created_by          BIGINT,
    updated_by          BIGINT,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_suppliers_tenant_customer ON suppliers(tenant_id, customer_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_suppliers_tenant_account ON suppliers(tenant_id, channel_account_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS service_providers (
    id                  BIGSERIAL    PRIMARY KEY,
    tenant_id           BIGINT       NOT NULL DEFAULT 1,
    name                VARCHAR(200) NOT NULL,
    customer_id         VARCHAR(64)  NOT NULL,
    channel_account_id  BIGINT,
    contact_info        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    role                VARCHAR(64)  NOT NULL DEFAULT '',
    remark              TEXT         NOT NULL DEFAULT '',
    source              VARCHAR(64)  NOT NULL DEFAULT '',
    source_ref          VARCHAR(255) NOT NULL DEFAULT '',
    created_by          BIGINT,
    updated_by          BIGINT,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_service_providers_tenant_customer ON service_providers(tenant_id, customer_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_service_providers_tenant_account ON service_providers(tenant_id, channel_account_id) WHERE deleted_at IS NULL;

-- 合并审计（FR-003/005）：来源、操作人、时间、原始值均可追溯
CREATE TABLE IF NOT EXISTS customer_merges (
    id              BIGSERIAL    PRIMARY KEY,
    tenant_id       BIGINT       NOT NULL DEFAULT 1,
    merge_id        VARCHAR(32)  NOT NULL,
    one_id          VARCHAR(128) NOT NULL DEFAULT '',
    primary_id      VARCHAR(36)  NOT NULL,
    secondary_id    VARCHAR(36)  NOT NULL,
    source          VARCHAR(64)  NOT NULL DEFAULT '',
    rule_name       VARCHAR(100) NOT NULL DEFAULT '',
    action          VARCHAR(16)  NOT NULL DEFAULT 'auto',
    original_values JSONB        NOT NULL DEFAULT '{}'::jsonb,
    reason          TEXT         NOT NULL DEFAULT '',
    operator_id     BIGINT,
    operator_name   VARCHAR(64)  NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_merges_tenant_merge ON customer_merges(tenant_id, merge_id);
CREATE INDEX IF NOT EXISTS idx_customer_merges_tenant_one ON customer_merges(tenant_id, one_id);
CREATE INDEX IF NOT EXISTS idx_customer_merges_tenant_primary ON customer_merges(tenant_id, primary_id);
