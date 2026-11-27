-- 102_llm_session_ledger.sql
-- 2026-11-27 LLM 会话续接元数据、加密执行片段与副作用工具台账。
-- 幂等：可重复执行；不存原始消息、客户画像或线索内容。

CREATE TABLE IF NOT EXISTS llm_session_states (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL,
  platform varchar(32) NOT NULL,
  channel_account_id bigint NOT NULL,
  llm_conversation_id varchar(191) NOT NULL,
  scope_hash char(64) NOT NULL,
  bus_llm_session_id_enc bytea NOT NULL,
  bus_llm_session_id_blind char(64) NOT NULL,
  execution_scenario_id varchar(64) NOT NULL,
  dispatch_scenario varchar(64) NOT NULL DEFAULT '',
  provider varchar(64) NOT NULL,
  model varchar(191) NOT NULL,
  protocol varchar(64) NOT NULL,
  region varchar(64) NOT NULL,
  generation bigint NOT NULL DEFAULT 1,
  provider_continuation_ref_enc bytea,
  last_message_id varchar(191) NOT NULL DEFAULT '',
  last_message_cursor varchar(191) NOT NULL DEFAULT '',
  replied_through_cursor varchar(191) NOT NULL DEFAULT '',
  prompt_prefix_version varchar(64) NOT NULL,
  context_schema_version varchar(32) NOT NULL,
  capability_fingerprint char(64) NOT NULL,
  retention_mode varchar(32) NOT NULL DEFAULT 'stateless',
  expires_at timestamp NOT NULL,
  last_used_at timestamp NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT chk_llm_session_retention CHECK (retention_mode IN ('stateless', 'native')),
  CONSTRAINT chk_llm_session_generation CHECK (generation > 0),
  CONSTRAINT chk_llm_session_version CHECK (version > 0)
);
CREATE UNIQUE INDEX IF NOT EXISTS uni_llm_session_state
  ON llm_session_states (
    tenant_id, channel_account_id, bus_llm_session_id_blind, execution_scenario_id,
    provider, model, protocol, region, generation
  );
CREATE INDEX IF NOT EXISTS idx_llm_session_scope
  ON llm_session_states (tenant_id, channel_account_id, scope_hash, execution_scenario_id);
CREATE INDEX IF NOT EXISTS idx_llm_session_expiry ON llm_session_states (expires_at);

CREATE TABLE IF NOT EXISTS scenario_transcripts (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL,
  llm_session_state_id bigint NOT NULL,
  snapshot_id varchar(191) NOT NULL,
  execution_scenario_id varchar(64) NOT NULL,
  generation bigint NOT NULL DEFAULT 1,
  sequence_no bigint NOT NULL,
  entry_type varchar(32) NOT NULL,
  payload_enc bytea NOT NULL,
  payload_hash char(64) NOT NULL,
  provider_request_hash char(64) NOT NULL DEFAULT '',
  request_idempotency_key varchar(191) NOT NULL,
  created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at timestamp NOT NULL,
  CONSTRAINT fk_scenario_transcript_session
    FOREIGN KEY (llm_session_state_id) REFERENCES llm_session_states(id) ON DELETE CASCADE,
  CONSTRAINT chk_scenario_transcript_entry
    CHECK (entry_type IN ('model_step', 'tool_call', 'tool_result', 'handoff', 'compaction')),
  CONSTRAINT chk_scenario_transcript_generation CHECK (generation > 0),
  CONSTRAINT chk_scenario_transcript_sequence CHECK (sequence_no > 0)
);
CREATE UNIQUE INDEX IF NOT EXISTS uni_scenario_transcript_sequence
  ON scenario_transcripts (llm_session_state_id, generation, sequence_no);
CREATE UNIQUE INDEX IF NOT EXISTS uni_scenario_transcript_idempotency
  ON scenario_transcripts (llm_session_state_id, request_idempotency_key);
CREATE INDEX IF NOT EXISTS idx_scenario_transcript_expiry ON scenario_transcripts (expires_at);
CREATE INDEX IF NOT EXISTS idx_scenario_transcript_tenant ON scenario_transcripts (tenant_id, llm_session_state_id);

CREATE TABLE IF NOT EXISTS tool_invocations (
  id bigserial PRIMARY KEY,
  tenant_id bigint NOT NULL,
  transcript_id bigint NOT NULL,
  tool_name varchar(128) NOT NULL,
  tool_idempotency_key varchar(191) NOT NULL DEFAULT '',
  risk_class varchar(32) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'planned',
  authorization_hash char(64) NOT NULL,
  resource_version varchar(191) NOT NULL DEFAULT '',
  request_hash char(64) NOT NULL,
  result_enc bytea,
  result_hash char(64) NOT NULL DEFAULT '',
  external_receipt_id varchar(191) NOT NULL DEFAULT '',
  attempt_count integer NOT NULL DEFAULT 1,
  last_error_code varchar(64) NOT NULL DEFAULT '',
  created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT fk_tool_invocation_transcript
    FOREIGN KEY (transcript_id) REFERENCES scenario_transcripts(id) ON DELETE CASCADE,
  CONSTRAINT chk_tool_invocation_risk CHECK (risk_class IN ('read', 'write', 'high_write')),
  CONSTRAINT chk_tool_invocation_status
    CHECK (status IN ('planned', 'authorized', 'dispatched', 'unknown', 'confirmed')),
  CONSTRAINT chk_tool_invocation_attempt CHECK (attempt_count > 0),
  CONSTRAINT chk_tool_invocation_high_write_key
    CHECK (risk_class <> 'high_write' OR tool_idempotency_key <> '')
);
CREATE UNIQUE INDEX IF NOT EXISTS uni_tool_invocation_idempotency
  ON tool_invocations (tenant_id, tool_idempotency_key)
  WHERE tool_idempotency_key <> '';
CREATE INDEX IF NOT EXISTS idx_tool_invocation_status ON tool_invocations (tenant_id, status, updated_at);
CREATE INDEX IF NOT EXISTS idx_tool_invocation_transcript ON tool_invocations (tenant_id, transcript_id);
