-- 075_custom_accounts.sql
-- specs/025 T1101：通用自定义渠道账号表（固定 JSON 契约 + HMAC 签名 + 回调 URL）。
CREATE TABLE IF NOT EXISTS custom_accounts (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT       NOT NULL DEFAULT 1,
    owner_user_id     BIGINT       NOT NULL DEFAULT 0,
    account_name      VARCHAR(100) NOT NULL,
    secret            VARCHAR(200) NOT NULL DEFAULT '',
    callback_url      VARCHAR(500) NOT NULL DEFAULT '',
    ai_agent_enabled  BOOLEAN      NOT NULL DEFAULT false,
    status            INT          NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_custom_accounts_tenant ON custom_accounts (tenant_id);
