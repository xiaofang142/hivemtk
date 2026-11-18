-- 068_channel_event_audits.sql
-- specs/024 D2/R3：平台事件（编辑/撤回/入群/机器人/交互回调）的落库证据。
--
-- 为什么建表而不是只发事件总线：总线 async best-effort，队列满直接丢；
-- 而「谁在什么时候入群/退群/点了哪个按钮」是资产归属与纠纷复盘要用的证据。
-- 为什么不复用 operation_logs：那张表 tenant_id 是 uint、语义是后台人操作，
-- 平台事件是系统事件、租户键是字符串，混用会让查询口径变糊。
--
-- 幂等：唯一键 (tenant_id, channel, channel_msg_id, event_type)，
-- 平台重投同一事件不会产生第二行（重投是常态：Telegram/Slack 都会重试）。

CREATE TABLE IF NOT EXISTS channel_event_audits (
    id               BIGSERIAL PRIMARY KEY,
    tenant_id        BIGINT       NOT NULL DEFAULT 1,
    channel          VARCHAR(32)  NOT NULL,
    account_id       VARCHAR(64)  NOT NULL DEFAULT '',
    channel_msg_id   VARCHAR(128) NOT NULL DEFAULT '',
    event_type       VARCHAR(64)  NOT NULL,
    sender_id        VARCHAR(128) NOT NULL DEFAULT '',
    sender_name      VARCHAR(128) NOT NULL DEFAULT '',
    conversation_id  VARCHAR(128) NOT NULL DEFAULT '',
    route_rule_id    BIGINT       NOT NULL DEFAULT 0,
    payload          JSONB,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_event_audit
    ON channel_event_audits (tenant_id, channel, channel_msg_id, event_type);
CREATE INDEX IF NOT EXISTS idx_channel_event_audits_conversation
    ON channel_event_audits (conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_channel_event_audits_created
    ON channel_event_audits (created_at DESC);
