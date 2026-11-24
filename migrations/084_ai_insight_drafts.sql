-- 084_ai_insight_drafts.sql
-- PR-7/R-06: AI 画像/商机/建议草稿，证据化+人工确认。幂等。
CREATE TABLE IF NOT EXISTS ai_insight_drafts (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL DEFAULT 1,
  draft_id varchar(32) NOT NULL,
  entity_type varchar(32) NOT NULL DEFAULT 'customer',
  entity_id varchar(64) NOT NULL DEFAULT '',
  insight_type varchar(32) NOT NULL DEFAULT 'profile',
  title varchar(255) NOT NULL DEFAULT '',
  content_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  evidence_json jsonb NOT NULL DEFAULT '[]'::jsonb,
  confidence numeric(5,4) NOT NULL DEFAULT 0,
  status varchar(32) NOT NULL DEFAULT 'draft',
  generated_by varchar(64) NOT NULL DEFAULT '',
  confirmed_by bigint,
  confirmed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_ai_insight_drafts_tenant_draft ON ai_insight_drafts(tenant_id, draft_id);
CREATE INDEX IF NOT EXISTS idx_ai_insight_drafts_entity ON ai_insight_drafts(tenant_id, entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_ai_insight_drafts_status ON ai_insight_drafts(tenant_id, status);

ALTER TABLE ai_insight_drafts ADD COLUMN IF NOT EXISTS source_refs_json jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE ai_insight_drafts ADD COLUMN IF NOT EXISTS proposed_action_json jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE ai_insight_drafts ADD COLUMN IF NOT EXISTS applied_action_json jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE ai_insight_drafts ADD COLUMN IF NOT EXISTS model_name varchar(64) NOT NULL DEFAULT '';
ALTER TABLE ai_insight_drafts ADD COLUMN IF NOT EXISTS prompt_version varchar(64) NOT NULL DEFAULT '';
ALTER TABLE ai_insight_drafts ADD COLUMN IF NOT EXISTS review_note text NOT NULL DEFAULT '';
