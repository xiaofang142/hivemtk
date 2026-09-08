-- 056: 统一收件箱会话去重 — uk_inbox_conv_channel 唯一索引
-- 背景：webhook_inbox.go 先查后插存在并发竞态（webhook 至少一次重发），
-- 同一 (platform, account_id, customer_id) 会产生多条会话、未读数分裂。
-- 与 model.InboxConversation 的 uniqueIndex tag 对齐（v3.37.0）。
-- 可重入：先删重再建索引，全部幂等。

-- 1) 清理历史重复行：每组 (platform, account_id, customer_id) 仅保留 id 最大（最新）的一条
DELETE FROM inbox_conversations a
USING inbox_conversations b
WHERE a.platform = b.platform
  AND a.account_id = b.account_id
  AND a.customer_id = b.customer_id
  AND a.id < b.id;

-- 2) 建唯一索引（幂等）
CREATE UNIQUE INDEX IF NOT EXISTS uk_inbox_conv_channel
    ON inbox_conversations (platform, account_id, customer_id);
