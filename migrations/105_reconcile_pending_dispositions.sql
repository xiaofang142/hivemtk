-- 2026-11-27 生产真实流量修复：修复前 AI 生成失败/转人工/投递失败未落终止状态，
-- 老的 pending 记录不会因代码修复自动收敛。把这些超过回复窗口且仍 pending 的
-- 历史台账按保守口径标 skipped，避免回复水位被永久卡住。
-- 幂等：仅更新仍为 pending 且超过 10 分钟的行，version 单调递增。
BEGIN;

UPDATE conversation_message_dispositions
SET status = 'skipped',
    reason = 'stale_pending_reconciled',
    version = version + 1,
    updated_at = now()
WHERE status = 'pending'
  AND created_at < now() - interval '10 minutes';

COMMIT;
