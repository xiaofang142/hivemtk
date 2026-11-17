-- 064: 会话生命周期三列（借鉴 Chatwoot message 回调口径，2026-11-17）
--
-- 背景：inbox_conversations 只有 last_message_* 与 unread_count，回答不了
-- 「客户等了多久没人回」「首响用了多久」——这两个问题原本要按 message_hub 反推，
-- 既慢又容易在去重/补录后算错。Chatwoot 的做法是把状态收口在 message 的
-- after_create 回调里（waiting_since / first_reply_created_at），会话行只读。
-- 本迁移把同一口径落到我方会话行：
--   - waiting_since   客户消息落库时若为空则写入；任何非客户消息落库时清空。
--                     客户连发不刷新起点（SLA 看最早那条）。
--   - first_reply_at  首条非客户消息的时间，只写一次。
--   - last_activity_at 最近一条任意方向消息的时间（排序/超时用，不受 unread 清零影响）。
--
-- 写入点：repository.UpsertFromMessageTx（按 last_message_from 分方向）；
-- UpdateLastMessage 也会顺带刷新 last_activity_at。
--
-- 回填口径：
--   last_activity_at ← last_message_at（同源，必然可回填）；
--   waiting_since    ← last_message_at（仅「最后一条是客户消息且未关闭」的会话，
--                      即当前确实在等回复的那批）；
--   first_reply_at   不回填——历史数据没有可靠的首响时刻，宁可空着也不编造。
--
-- 注意：生产 schema 事实源是 user-server AutoMigrate（模型字段已同步加列）；
-- 本文件供离线/人工执行路径使用，幂等可重入。
ALTER TABLE inbox_conversations ADD COLUMN IF NOT EXISTS waiting_since timestamptz;
ALTER TABLE inbox_conversations ADD COLUMN IF NOT EXISTS first_reply_at timestamptz;
ALTER TABLE inbox_conversations ADD COLUMN IF NOT EXISTS last_activity_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_inbox_conversations_waiting_since ON inbox_conversations (waiting_since);
CREATE INDEX IF NOT EXISTS idx_inbox_conversations_last_activity_at ON inbox_conversations (last_activity_at);

UPDATE inbox_conversations
SET last_activity_at = last_message_at
WHERE last_activity_at IS NULL AND last_message_at IS NOT NULL;

UPDATE inbox_conversations
SET waiting_since = last_message_at
WHERE waiting_since IS NULL
  AND last_message_at IS NOT NULL
  AND last_message_from = 'customer'
  AND closed_at IS NULL;
