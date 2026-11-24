-- 085_lead_mining_extension.sql
-- LM-PR1/LM-PR2: 线索发掘扩展。幂等。
ALTER TABLE clues ADD COLUMN IF NOT EXISTS lead_role varchar(16) NOT NULL DEFAULT 'unknown';
ALTER TABLE clues ADD COLUMN IF NOT EXISTS status varchar(32) NOT NULL DEFAULT 'new';
ALTER TABLE clues ADD COLUMN IF NOT EXISTS evidence_json jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE clues ADD COLUMN IF NOT EXISTS summary text NOT NULL DEFAULT '';
ALTER TABLE clues ADD COLUMN IF NOT EXISTS followup_suggestion text NOT NULL DEFAULT '';
ALTER TABLE clues ADD COLUMN IF NOT EXISTS last_judged_at timestamptz;
ALTER TABLE clues ADD COLUMN IF NOT EXISTS source_channel varchar(32) NOT NULL DEFAULT '';
ALTER TABLE clues ADD COLUMN IF NOT EXISTS source_account_id varchar(128) NOT NULL DEFAULT '';
ALTER TABLE clues ADD COLUMN IF NOT EXISTS conversation_type varchar(16) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_clues_tenant_lead_role ON clues(tenant_id, lead_role);
CREATE INDEX IF NOT EXISTS idx_clues_tenant_status ON clues(tenant_id, status);

CREATE TABLE IF NOT EXISTS lead_mining_judgements (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL DEFAULT 1,
  judgement_id varchar(32) NOT NULL,
  message_id varchar(128) NOT NULL DEFAULT '',
  conversation_id varchar(128) NOT NULL DEFAULT '',
  platform varchar(32) NOT NULL DEFAULT '',
  account_id varchar(128) NOT NULL DEFAULT '',
  is_group boolean NOT NULL DEFAULT false,
  group_id varchar(128) NOT NULL DEFAULT '',
  sender_id varchar(128) NOT NULL DEFAULT '',
  content_hash varchar(64) NOT NULL DEFAULT '',
  context_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  result_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  matched boolean NOT NULL DEFAULT false,
  lead_role varchar(16) NOT NULL DEFAULT 'unknown',
  intent_score int NOT NULL DEFAULT 0,
  reason text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_lmj_tenant_judgement ON lead_mining_judgements(tenant_id, judgement_id);
CREATE INDEX IF NOT EXISTS idx_lmj_tenant_msg ON lead_mining_judgements(tenant_id, message_id);
CREATE INDEX IF NOT EXISTS idx_lmj_tenant_conv ON lead_mining_judgements(tenant_id, conversation_id);

CREATE TABLE IF NOT EXISTS lead_mining_notify_routes (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL DEFAULT 1,
  route_id varchar(32) NOT NULL,
  channel varchar(32) NOT NULL DEFAULT '',
  account_id varchar(128) NOT NULL DEFAULT '',
  notify_channel varchar(16) NOT NULL DEFAULT 'in_app',
  target varchar(255) NOT NULL DEFAULT '',
  enabled boolean NOT NULL DEFAULT true,
  created_by bigint,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_lmnr_tenant_route ON lead_mining_notify_routes(tenant_id, route_id);
CREATE INDEX IF NOT EXISTS idx_lmnr_tenant_channel_account ON lead_mining_notify_routes(tenant_id, channel, account_id);
