-- 081_followup_records.sql
-- PR-4/R-03: 完整跟进记录。FollowUpReminder 保留为提醒语义。
CREATE TABLE IF NOT EXISTS followup_records (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL DEFAULT 1,
  record_id varchar(32) NOT NULL,
  customer_id varchar(36) NOT NULL DEFAULT '',
  company_id bigint,
  contact_id bigint,
  opportunity_id bigint,
  reminder_id varchar(32) NOT NULL DEFAULT '',
  owner_id bigint NOT NULL DEFAULT 0,
  followup_type varchar(32) NOT NULL DEFAULT 'note',
  channel varchar(32) NOT NULL DEFAULT '',
  content text NOT NULL DEFAULT '',
  result text NOT NULL DEFAULT '',
  next_action text NOT NULL DEFAULT '',
  next_followup_at timestamptz,
  source varchar(64) NOT NULL DEFAULT '',
  source_ref varchar(255) NOT NULL DEFAULT '',
  evidence_json jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_by bigint,
  updated_by bigint,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_followup_records_tenant_record ON followup_records(tenant_id, record_id);
CREATE INDEX IF NOT EXISTS idx_followup_records_tenant_customer ON followup_records(tenant_id, customer_id);
CREATE INDEX IF NOT EXISTS idx_followup_records_tenant_owner ON followup_records(tenant_id, owner_id);
CREATE INDEX IF NOT EXISTS idx_followup_records_tenant_next ON followup_records(tenant_id, next_followup_at);
CREATE INDEX IF NOT EXISTS idx_followup_records_reminder ON followup_records(reminder_id);
