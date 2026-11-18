-- 070_channel_evidence_identity.sql — 证据链幂等键加入 account_id（specs/024 三审 T209）
--
-- 契约（2026-11-18 对抗复核）：同一 msg_id 在不同账号下是两份独立证据；
-- 跨账号去重会丢证据。交互同口径（不能跨账号去重）。
-- 旧索引已随 068/069 在既有部署落地 ⇒ 不能改旧文件，幂等重建索引：
-- 每次执行先 DROP 后 CREATE，重复执行结果一致。

DROP INDEX IF EXISTS idx_channel_event_audit;
CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_event_audit
    ON channel_event_audits (tenant_id, channel, account_id, channel_msg_id, event_type);

DROP INDEX IF EXISTS idx_interaction_identity;
CREATE UNIQUE INDEX IF NOT EXISTS idx_interaction_identity
    ON channel_interactions (tenant_id, channel, account_id, interaction_id);
