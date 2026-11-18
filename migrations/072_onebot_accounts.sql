-- 072_onebot_accounts.sql
-- specs/025 T803：OneBot v11 协议端账号表（NapCat/Lagrange）。
--
-- AccessToken 同时是反向 WS 握手凭证与 HTTP API 调用凭证；SelfID 为协议端
-- 登录的 QQ 号，用于把连上来的连接映射回账号行（多账号并存时不互相顶替）。
CREATE TABLE IF NOT EXISTS onebot_accounts (
    id             BIGSERIAL PRIMARY KEY,
    tenant_id      BIGINT       NOT NULL DEFAULT 1,
    owner_user_id  BIGINT       NOT NULL DEFAULT 0,
    account_name   VARCHAR(100) NOT NULL,
    self_id        VARCHAR(32)  NOT NULL DEFAULT '',
    http_base_url  VARCHAR(500) NOT NULL DEFAULT '',
    access_token   VARCHAR(200) NOT NULL DEFAULT '',
    ai_agent_enabled BOOLEAN    NOT NULL DEFAULT false,
    last_online_at TIMESTAMPTZ,
    status         INT          NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_onebot_accounts_tenant ON onebot_accounts (tenant_id);
CREATE INDEX IF NOT EXISTS idx_onebot_accounts_self_id ON onebot_accounts (self_id);
