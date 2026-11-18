-- 073_slack_accounts.sql
-- specs/025 T901：Slack app 账号表（Events API 签名 + chat.postMessage Bot Token）。
CREATE TABLE IF NOT EXISTS slack_accounts (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT       NOT NULL DEFAULT 1,
    owner_user_id     BIGINT       NOT NULL DEFAULT 0,
    account_name      VARCHAR(100) NOT NULL,
    team_id           VARCHAR(64)  NOT NULL DEFAULT '',
    app_id            VARCHAR(64)  NOT NULL DEFAULT '',
    bot_token         TEXT         NOT NULL DEFAULT '',
    signing_secret    VARCHAR(200) NOT NULL DEFAULT '',
    ai_agent_enabled  BOOLEAN      NOT NULL DEFAULT false,
    status            INT          NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_slack_accounts_tenant ON slack_accounts (tenant_id);
CREATE INDEX IF NOT EXISTS idx_slack_accounts_team_id ON slack_accounts (team_id);
