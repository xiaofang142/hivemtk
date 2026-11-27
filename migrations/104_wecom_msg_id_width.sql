-- 104_wecom_msg_id_width.sql
-- 修复：企微 msgid 长度超过 varchar(100)，导致所有企微发送记录落库失败。
--
-- 背景（2026-11-27 真机实证）：
--   /cgi-bin/message/send 与 /cgi-bin/externalcontact/message/send 返回的 msgid
--   实测长度 128 字符（例：
--   w_RX2-eFmY611r0RM6AvWb_CgYsfrkbRIfwFKs3_o98rUhgLKVfja9Gr09B2l6FTJR6VdhofyGGp-izhefE-fAd7BCqegtxhaMchyVgKhbW7R8lh8iSOCK2bxwSyfjcb）。
--   而 wecom_messages.msg_id 原为 varchar(100)，插入必报
--   `value too long for type character varying(100)` (SQLSTATE 22001)。
--   该错误只打 warn 日志（发送本身已成功），表现为「消息发出去了但记录查不到」——
--   会连带影响：撤回（依赖 platform_msg_id）、对账、发送统计。
--
-- 影响面：仅放宽长度，不改变唯一约束语义（tenant_id, msg_id）仍然有效。

ALTER TABLE wecom_messages
    ALTER COLUMN msg_id TYPE VARCHAR(255);

-- wecom_accounts.webhook_path 同理偏窄的场景一并放宽（回调路径含 account_id 前缀
-- 与租户 code 时可能超过 200），保持与其它渠道一致的 512。
ALTER TABLE wecom_accounts
    ALTER COLUMN webhook_path TYPE VARCHAR(512);
