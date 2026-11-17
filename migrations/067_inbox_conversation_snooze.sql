-- 067_inbox_conversation_snooze.sql
-- specs/024 T601：收件箱「稍后处理」（snoozed）状态 + 重开计数。
--
-- snoozed 与 closed 的区别：
--   closed  = 这件事结束了；
--   snoozed = 我稍后再看，到期自动回到 open，且不计入 unread 统计（客户并没有新消息）。
--
-- status 是 varchar（不是 pg enum），所以加状态值不需要改类型，只要查询层认这个值。
-- ADD COLUMN IF NOT EXISTS + CREATE INDEX IF NOT EXISTS：可重跑。

ALTER TABLE inbox_conversations
    ADD COLUMN IF NOT EXISTS snoozed_until TIMESTAMPTZ;

ALTER TABLE inbox_conversations
    ADD COLUMN IF NOT EXISTS reopen_count BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN inbox_conversations.snoozed_until IS
    '稍后处理到期时间（specs/024 T601）；status=snoozed 时生效，到期 worker 改回 open';
COMMENT ON COLUMN inbox_conversations.reopen_count IS
    '已关闭→因新入站重开的累计次数（specs/024 T603）';

-- 到期 worker 的扫描键：只查 snoozed 且已到期。
-- 部分索引而非全表索引：非 snoozed 行永远不进这个索引。
CREATE INDEX IF NOT EXISTS idx_inbox_conv_snooze_due
    ON inbox_conversations (tenant_id, snoozed_until)
    WHERE status = 'snoozed' AND deleted_at IS NULL;
