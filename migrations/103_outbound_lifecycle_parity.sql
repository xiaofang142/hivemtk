-- 103_outbound_lifecycle_parity.sql
-- 2026-11-27 出站生命周期：延迟/重试/重放保留逻辑回复身份、origin、群聊 scope 与原 Extra。
-- 幂等：可重复执行；legacy 空 correlation 不参与唯一约束。

ALTER TABLE reach_delayed_outbound ADD COLUMN IF NOT EXISTS correlation_id varchar(100);
ALTER TABLE reach_delayed_outbound ADD COLUMN IF NOT EXISTS origin varchar(20) NOT NULL DEFAULT 'ai';
ALTER TABLE reach_delayed_outbound ADD COLUMN IF NOT EXISTS session_id varchar(120) NOT NULL DEFAULT '';
ALTER TABLE reach_delayed_outbound ADD COLUMN IF NOT EXISTS is_group boolean NOT NULL DEFAULT false;
ALTER TABLE reach_delayed_outbound ADD COLUMN IF NOT EXISTS group_id varchar(100) NOT NULL DEFAULT '';
ALTER TABLE reach_delayed_outbound ADD COLUMN IF NOT EXISTS visibility varchar(32) NOT NULL DEFAULT 'unknown';
ALTER TABLE reach_delayed_outbound ADD COLUMN IF NOT EXISTS audience_json jsonb;
ALTER TABLE reach_delayed_outbound ADD COLUMN IF NOT EXISTS visibility_evidence jsonb;
ALTER TABLE reach_delayed_outbound ADD COLUMN IF NOT EXISTS hub_extra jsonb;

CREATE INDEX IF NOT EXISTS idx_delayed_outbound_correlation
  ON reach_delayed_outbound (tenant_id, correlation_id);
CREATE UNIQUE INDEX IF NOT EXISTS uni_delayed_outbound_correlation
  ON reach_delayed_outbound (tenant_id, correlation_id)
  WHERE correlation_id <> '';
CREATE INDEX IF NOT EXISTS idx_delayed_outbound_session
  ON reach_delayed_outbound (tenant_id, session_id);
