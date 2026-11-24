-- 086_lead_mining_feedback_quality.sql
-- LM-PR6: 线索发掘误报反馈、回放与质量看板。幂等。
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS feedback_status varchar(16) NOT NULL DEFAULT 'none'; -- none/false_positive/valid
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS feedback_reason text NOT NULL DEFAULT '';
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS feedback_by varchar(64) NOT NULL DEFAULT '';
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS feedback_at timestamptz;

ALTER TABLE clues ADD COLUMN IF NOT EXISTS feedback_status varchar(16) NOT NULL DEFAULT 'none'; -- none/false_positive/valid
ALTER TABLE clues ADD COLUMN IF NOT EXISTS feedback_reason text NOT NULL DEFAULT '';
ALTER TABLE clues ADD COLUMN IF NOT EXISTS feedback_by varchar(64) NOT NULL DEFAULT '';
ALTER TABLE clues ADD COLUMN IF NOT EXISTS feedback_at bigint;

CREATE INDEX IF NOT EXISTS idx_lmj_tenant_feedback ON lead_mining_judgements(tenant_id, feedback_status);
CREATE INDEX IF NOT EXISTS idx_clues_tenant_feedback ON clues(tenant_id, feedback_status);
