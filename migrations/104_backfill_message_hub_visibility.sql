-- 2026-11-27 生产真实流量修复：历史入站 hub 行默认 visibility=unknown，
-- 导致群聊跨消息上下文被 audience 过滤全部丢弃。按权威 scope 回填：
-- 群聊只认 group_id，私聊只认稳定 sender_id/conversation_id，绝不使用昵称。
-- 幂等：仅更新仍为 unknown 且有可证明稳定根身份的 inbound 行。
BEGIN;

UPDATE message_hub
SET visibility = CASE WHEN is_group THEN 'member' ELSE 'public' END,
    audience_json = CASE
        WHEN is_group THEN jsonb_build_array(group_id)
        ELSE jsonb_build_array(COALESCE(NULLIF(sender_id, ''), NULLIF(conversation_id, '')))
    END,
    visibility_evidence = CASE
        WHEN is_group THEN jsonb_build_object(
            'source', 'backfill_channel_ingress',
            'is_group', true,
            'group_id', group_id
        )
        ELSE jsonb_build_object('source', 'backfill_channel_ingress')
    END,
    extra = CASE
        WHEN extra IS NULL OR extra = '' THEN jsonb_build_object('correlation_id', msg_id)::text
        ELSE (extra::jsonb || jsonb_build_object('correlation_id', msg_id))::text
    END
WHERE visibility = 'unknown'
  AND direction = 'inbound'
  AND (
      (is_group AND NULLIF(group_id, '') IS NOT NULL)
      OR ((NOT is_group) AND (NULLIF(sender_id, '') IS NOT NULL OR NULLIF(conversation_id, '') IS NOT NULL))
  )
  AND (extra IS NULL OR extra = '' OR extra ~ '^[[:space:]]*\{');

COMMIT;
