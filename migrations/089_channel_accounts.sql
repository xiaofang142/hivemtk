-- 089_channel_accounts.sql
-- 渠道账号主数据（单渠道多账号）。T007 / data-model.md 渠道域。
-- 两类智能体绑定收口在本表；owner_user_ids/data_scope 为数据授权维度（原 RBAC 不改）。
CREATE TABLE IF NOT EXISTS channel_accounts (
    id                   BIGSERIAL    PRIMARY KEY,
    tenant_id            BIGINT       NOT NULL DEFAULT 1,
    channel_id           BIGINT       NOT NULL,
    name                 VARCHAR(100) NOT NULL,
    remark               VARCHAR(500) NOT NULL DEFAULT '',
    self_identify_name   VARCHAR(100) NOT NULL DEFAULT '',
    type                 VARCHAR(32)  NOT NULL DEFAULT 'dm',
    auto_reply_agent_id  BIGINT       NULL,
    lead_mining_agent_id BIGINT       NULL,
    owner_user_ids       JSONB        NOT NULL DEFAULT '[]'::jsonb,
    data_scope           JSONB        NOT NULL DEFAULT '{}'::jsonb,
    identify_code        VARCHAR(128) NOT NULL DEFAULT '',
    status               INT          NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_channel_accounts_tenant ON channel_accounts (tenant_id);
CREATE INDEX IF NOT EXISTS idx_channel_accounts_channel ON channel_accounts (channel_id);
CREATE INDEX IF NOT EXISTS idx_channel_accounts_status ON channel_accounts (status);
-- 识别码是消息入口反查租户/账号的权威键，必须全局唯一。
CREATE UNIQUE INDEX IF NOT EXISTS global_uniq_channel_account_identify_code
    ON channel_accounts (identify_code) WHERE identify_code <> '';
