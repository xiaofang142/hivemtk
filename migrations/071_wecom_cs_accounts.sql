-- 071_wecom_cs_accounts.sql
-- specs/025 T703：企业微信客服账号表（与企微自建应用 wecom_accounts 分离）。
--
-- 为什么新表：客服回调按 open_kfid + cursor 增量拉取，游标必须断点续拉；
-- 把 kf_* 字段塞进 wecom_accounts 会让「应用消息」与「客服消息」共用一行，
-- 两套 Token/AESKey 互相覆盖。
CREATE TABLE IF NOT EXISTS wecom_cs_accounts (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT       NOT NULL DEFAULT 1,
    owner_user_id     BIGINT       NOT NULL DEFAULT 0,
    account_name      VARCHAR(100) NOT NULL,
    corp_id           VARCHAR(100) NOT NULL,
    secret            VARCHAR(200) NOT NULL,
    open_kfid         VARCHAR(100) NOT NULL,
    token             VARCHAR(200) NOT NULL DEFAULT '',
    aes_key           VARCHAR(200) NOT NULL DEFAULT '',
    sync_cursor       VARCHAR(512) NOT NULL DEFAULT '',
    cursor_updated_at TIMESTAMPTZ,
    ai_agent_enabled  BOOLEAN      NOT NULL DEFAULT false,
    status            INT          NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_wecom_cs_accounts_tenant ON wecom_cs_accounts (tenant_id);
CREATE INDEX IF NOT EXISTS idx_wecom_cs_accounts_open_kfid ON wecom_cs_accounts (open_kfid);
