-- 069_channel_interactions.sql
-- specs/024 D8：可交互卡片与平台点击的映射，回答「这次点击对应哪条卡片、点的哪个动作、答复过没有」。
--
-- 只登记、不改消息表：点击不是消息，进 message_hub 会污染会话与未读数。
-- 幂等键 (tenant_id, channel, interaction_id)：平台重投同一回调不建第二行。
-- 状态：pending → answered（答复成功）/ expired（到期未答复，清理 worker 收口）。

CREATE TABLE IF NOT EXISTS channel_interactions (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       VARCHAR(64)  NOT NULL DEFAULT '',
    channel         VARCHAR(32)  NOT NULL,
    account_id      VARCHAR(64)  NOT NULL DEFAULT '',
    interaction_id  VARCHAR(128) NOT NULL,
    conversation_id VARCHAR(128) NOT NULL DEFAULT '',
    platform_msg_id VARCHAR(128) NOT NULL DEFAULT '',
    sender_id       VARCHAR(128) NOT NULL DEFAULT '',
    action          VARCHAR(128) NOT NULL DEFAULT '',
    payload         JSONB,
    status          VARCHAR(16)  NOT NULL DEFAULT 'pending',
    answered_at     TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_interaction_identity
    ON channel_interactions (tenant_id, channel, interaction_id);
CREATE INDEX IF NOT EXISTS idx_channel_interactions_status
    ON channel_interactions (status, expires_at);
CREATE INDEX IF NOT EXISTS idx_channel_interactions_conversation
    ON channel_interactions (conversation_id, created_at DESC);
