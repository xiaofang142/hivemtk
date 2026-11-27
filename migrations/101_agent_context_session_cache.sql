-- 101_agent_context_session_cache.sql
-- 2026-11-27 智能体会话上下文：可见性证据、逐消息处置状态与失效标记。
-- 幂等：可重复执行；legacy 消息可见性保持 unknown，不推断 audience。

-- 1) MessageHub：群聊输入授权证据。NULL/空 audience/evidence 均视为不可证明。
ALTER TABLE message_hub ADD COLUMN IF NOT EXISTS visibility varchar(32) NOT NULL DEFAULT 'unknown';
ALTER TABLE message_hub ADD COLUMN IF NOT EXISTS audience_json jsonb;
ALTER TABLE message_hub ADD COLUMN IF NOT EXISTS visibility_evidence jsonb;
ALTER TABLE message_hub DROP CONSTRAINT IF EXISTS chk_message_hub_visibility;
ALTER TABLE message_hub ADD CONSTRAINT chk_message_hub_visibility
  CHECK (visibility IN ('public', 'member', 'private', 'unknown'));
CREATE INDEX IF NOT EXISTS idx_message_hub_visibility ON message_hub (visibility);

-- 2) SessionMessage：与 MessageHub 出站回执确定性去重的关联键。
ALTER TABLE session_messages ADD COLUMN IF NOT EXISTS correlation_id varchar(100);
CREATE INDEX IF NOT EXISTS idx_session_messages_correlation
  ON session_messages (tenant_id, session_id, correlation_id);

-- 3) ConversationContext：legacy messages 只读兼容，新写入使用 CAS/source cursor。
ALTER TABLE conversation_contexts ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;
ALTER TABLE conversation_contexts ADD COLUMN IF NOT EXISTS source_cursor varchar(191) NOT NULL DEFAULT '';
ALTER TABLE conversation_contexts ADD COLUMN IF NOT EXISTS source_cursor_hash char(64) NOT NULL DEFAULT '';
ALTER TABLE conversation_contexts ADD COLUMN IF NOT EXISTS summary_version bigint NOT NULL DEFAULT 1;

-- 4) 可证明的群成员可见区间。同一成员最多一条 active（left_at IS NULL）记录。
CREATE TABLE IF NOT EXISTS conversation_memberships (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL,
  platform varchar(32) NOT NULL,
  channel_account_id bigint NOT NULL,
  group_id varchar(191) NOT NULL,
  member_id varchar(191) NOT NULL,
  display_name varchar(255) NOT NULL DEFAULT '',
  joined_at timestamp,
  left_at timestamp,
  evidence_source varchar(64) NOT NULL,
  evidence_ref varchar(191) NOT NULL,
  created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT chk_conv_membership_interval CHECK (joined_at IS NULL OR left_at IS NULL OR left_at >= joined_at)
);
CREATE UNIQUE INDEX IF NOT EXISTS uni_conv_membership_active
  ON conversation_memberships (tenant_id, platform, channel_account_id, group_id, member_id)
  WHERE left_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_conv_membership_active
  ON conversation_memberships (tenant_id, platform, channel_account_id, group_id, member_id, left_at);

-- 5) 逐入站消息处置台账。只有连续 replied + delivered_at 才可推进回复水位。
CREATE TABLE IF NOT EXISTS conversation_message_dispositions (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL,
  platform varchar(32) NOT NULL,
  channel_account_id bigint NOT NULL,
  conversation_id varchar(191) NOT NULL,
  group_id varchar(191),
  message_id varchar(191) NOT NULL,
  message_cursor varchar(191) NOT NULL,
  bus_llm_session_id_blind char(64) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'pending',
  reply_message_id varchar(191) NOT NULL DEFAULT '',
  delivered_at timestamp,
  late boolean NOT NULL DEFAULT false,
  reason varchar(255) NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT chk_conv_disposition_status
    CHECK (status IN ('pending', 'processing', 'replied', 'skipped', 'merged'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uni_agent_conv_disposition
  ON conversation_message_dispositions (tenant_id, platform, channel_account_id, conversation_id, message_id);
CREATE INDEX IF NOT EXISTS idx_conv_disposition_watermark
  ON conversation_message_dispositions (tenant_id, platform, channel_account_id, conversation_id, message_cursor, status, delivered_at);
CREATE INDEX IF NOT EXISTS idx_conv_disposition_session
  ON conversation_message_dispositions (tenant_id, channel_account_id, bus_llm_session_id_blind);

-- 6) 编辑/删除/撤回/脱敏 tombstone，供摘要和 native generation 失效。
CREATE TABLE IF NOT EXISTS message_tombstones (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL,
  platform varchar(32) NOT NULL,
  channel_account_id bigint NOT NULL,
  conversation_id varchar(191) NOT NULL,
  message_id varchar(191) NOT NULL,
  tombstone_type varchar(32) NOT NULL,
  effective_at timestamp NOT NULL,
  source_cursor varchar(191) NOT NULL,
  created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT chk_message_tombstone_type
    CHECK (tombstone_type IN ('edited', 'deleted', 'recalled', 'redacted'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uni_agent_msg_tombstone
  ON message_tombstones (tenant_id, platform, channel_account_id, message_id, tombstone_type);
CREATE INDEX IF NOT EXISTS idx_message_tombstones_cursor
  ON message_tombstones (tenant_id, platform, channel_account_id, conversation_id, source_cursor);
