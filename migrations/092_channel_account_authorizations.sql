-- 092_channel_account_authorizations.sql
-- T017 / FR-016/027：渠道账号数据授权表（原 RBAC 表与守卫挂载顺序一律不改，
-- 本表是叠加的数据权限维度）。
CREATE TABLE IF NOT EXISTS channel_account_authorizations (
    id                    BIGSERIAL    PRIMARY KEY,
    tenant_id             BIGINT       NOT NULL DEFAULT 1,
    user_id               BIGINT       NOT NULL,
    channel_account_id    BIGINT       NOT NULL,
    visible_industry_scope JSONB       NOT NULL DEFAULT '[]'::jsonb,
    role                  VARCHAR(32)  NOT NULL DEFAULT 'viewer',
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_channel_account_auth_tenant_user
    ON channel_account_authorizations (tenant_id, user_id);
CREATE INDEX IF NOT EXISTS idx_channel_account_auth_account
    ON channel_account_authorizations (channel_account_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_channel_account_auth
    ON channel_account_authorizations (tenant_id, user_id, channel_account_id);
