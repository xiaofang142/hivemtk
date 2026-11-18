-- 074_wecombot_accounts.sql
-- specs/025 T1001：企业微信智能机器人账号表（AES-CBC 回调 + webhook 推送）。
CREATE TABLE IF NOT EXISTS wecombot_accounts (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT       NOT NULL DEFAULT 1,
    owner_user_id     BIGINT       NOT NULL DEFAULT 0,
    account_name      VARCHAR(100) NOT NULL,
    token             VARCHAR(200) NOT NULL DEFAULT '',
    aes_key           VARCHAR(200) NOT NULL DEFAULT '',
    webhook_url       VARCHAR(500) NOT NULL DEFAULT '',
    ai_agent_enabled  BOOLEAN      NOT NULL DEFAULT false,
    status            INT          NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_wecombot_accounts_tenant ON wecombot_accounts (tenant_id);
