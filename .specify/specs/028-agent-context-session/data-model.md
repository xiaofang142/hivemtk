# Data Model: Agent Context, Session & Cache

**Feature:** `.specify/specs/028-agent-context-session`  
**Migration target:** `migrations/101_agent_context_session_cache.sql` (confirm next free number at implementation time)

## 1. Existing entities reused

### MessageHub

Authoritative channel message record. Existing fields include tenant, platform, account, message ID, direction, sender/receiver, conversation, group, content, sent time, extra and platform message ID.

Additions:

| Field | Type | Rule |
|---|---|---|
| `visibility` | varchar(32) | `public`, `member`, `private`, `unknown`; default `unknown` for legacy |
| `audience_json` | jsonb | Explicit authorized audience/member IDs when channel proves them; null means unproven |
| `visibility_evidence` | jsonb | Source event/membership evidence; never inferred from display name |
| `recalled_at` | timestamp | Existing recall semantics normalized if not already present |

Legacy `unknown` visibility is fail-closed for group context: only current speaker/current message and explicitly provable recent messages may be used.

### SessionMessage

Business AI/human ledger, not the channel history master.

Add `correlation_id varchar(191)` with index `(tenant_id, session_id, correlation_id)` for deterministic de-duplication against MessageHub outbound receipts.

### ConversationContext

Keep legacy `messages` JSON read-only during migration. Add:

- `version bigint` for CAS;
- `source_cursor varchar(191)` for the last MessageHub cursor included in summary;
- `source_cursor_hash char(64)` for tamper detection;
- `summary_version bigint`;
- `updated_at` already exists.

Stop appending raw messages. Summary/tags/member roles update with `WHERE version = ?`.

## 2. New entities

### 2.1 `ConversationMembership`

Tracks provable group visibility intervals.

| Field | Type | Rule |
|---|---|---|
| `id` | bigserial | Primary key |
| `tenant_id` | bigint | Required |
| `platform` | varchar(32) | Required |
| `channel_account_id` | bigint | Required |
| `group_id` | varchar(191) | Required |
| `member_id` | varchar(191) | Stable channel sender ID |
| `display_name` | varchar(255) | Snapshot only; never identity |
| `joined_at` | timestamp | Nullable if channel proves membership before retained history |
| `left_at` | timestamp | Nullable while active |
| `evidence_source` | varchar(64) | Channel event/admin import/system |
| `evidence_ref` | varchar(191) | Event/message ID |
| `created_at`, `updated_at` | timestamp | Audit |

Unique active membership: `(tenant_id, platform, channel_account_id, group_id, member_id)` where `left_at IS NULL`.

A message is visible to a member only when the message audience explicitly includes the member, or when `visibility=public`, or when membership evidence proves the member was active for the entire message interval. Unknown evidence never grants visibility.

### 2.2 `LLMSessionState`

One row per canonical bus session + execution scenario + provider/model/protocol/region/generation.

| Field | Type | Rule |
|---|---|---|
| `id` | bigserial | Primary key |
| `tenant_id` | bigint | Required |
| `platform` | varchar(32) | Required |
| `channel_account_id` | bigint | Required |
| `llm_conversation_id` | varchar(191) | Server-derived scope correlation |
| `scope_hash` | char(64) | HMAC of tenant/platform/account/conversation-or-group |
| `bus_llm_session_id_enc` | bytea | Encrypted canonical key |
| `bus_llm_session_id_blind` | char(64) | Keyed HMAC blind index for uniqueness |
| `execution_scenario_id` | varchar(64) | Independent execution chain |
| `dispatch_scenario` | varchar(64) | Routing/billing only |
| `provider` | varchar(64) | Required |
| `model` | varchar(191) | Required |
| `protocol` | varchar(64) | Required |
| `region` | varchar(64) | Required/default region |
| `generation` | bigint | Increment on rebuild/policy/model change |
| `provider_continuation_ref_enc` | bytea | Encrypted native continuation ID |
| `last_message_id` | varchar(191) | Stable MessageHub ID |
| `last_message_cursor` | varchar(191) | Immutable ordering cursor |
| `replied_through_cursor` | varchar(191) | Derived from disposition + delivered outbound |
| `prompt_prefix_version` | varchar(64) | Prompt/tool/source policy fingerprint |
| `context_schema_version` | varchar(32) | Context contract version |
| `capability_fingerprint` | char(64) | Provider/model/protocol/region config hash |
| `retention_mode` | varchar(32) | `stateless` or `native` |
| `expires_at` | timestamp | Required |
| `last_used_at` | timestamp | Required |
| `version` | bigint | CAS |
| `created_at`, `updated_at` | timestamp | Audit |

Unique key:
`(tenant_id, channel_account_id, bus_llm_session_id_blind, execution_scenario_id, provider, model, protocol, region, generation)`.

Additional unique alias binding:
`(tenant_id, channel_account_id, bus_llm_session_id_blind)` must not map to another scope hash; enforce in service transaction and DB partial unique/index as supported.

No raw messages, customer profile, or lead content is stored here.

### 2.3 `ConversationMessageDisposition`

Per-inbound user-visible processing state.

| Field | Type | Rule |
|---|---|---|
| `id` | bigserial | Primary key |
| `tenant_id` | bigint | Required |
| `platform` | varchar(32) | Required |
| `channel_account_id` | bigint | Required |
| `conversation_id` | varchar(191) | DM/group root |
| `group_id` | varchar(191) | Nullable for DM |
| `message_id` | varchar(191) | Stable MessageHub ID |
| `message_cursor` | varchar(191) | Ordering/tie-break cursor |
| `bus_llm_session_id_blind` | char(64) | Scope binding |
| `status` | varchar(32) | `pending`, `processing`, `replied`, `skipped`, `merged` |
| `reply_message_id` | varchar(191) | Nullable delivered outbound |
| `delivered_at` | timestamp | Nullable until confirmed |
| `late` | boolean | Default false |
| `reason` | varchar(255) | Machine-readable disposition reason |
| `version` | bigint | CAS |
| `created_at`, `updated_at` | timestamp | Audit |

Unique key: `(tenant_id, platform, channel_account_id, conversation_id, message_id)`.

Only a contiguous sequence of `replied` rows with confirmed `delivered_at` advances the reply watermark. `merged` does not fabricate a reply.

### 2.4 `ScenarioTranscript`

Encrypted, access-controlled reconstruction ledger.

| Field | Type | Rule |
|---|---|---|
| `id` | bigserial | Primary key |
| `tenant_id` | bigint | Required |
| `llm_session_state_id` | bigint | FK |
| `snapshot_id` | varchar(191) | Context snapshot hash |
| `execution_scenario_id` | varchar(64) | Must match session row |
| `sequence_no` | bigint | Monotonic per session generation |
| `entry_type` | varchar(32) | `model_step`, `tool_call`, `tool_result`, `handoff`, `compaction` |
| `payload_enc` | bytea | Encrypted prompt/output/tool evidence |
| `payload_hash` | char(64) | Tamper/dedupe |
| `provider_request_hash` | char(64) | Actual adapter request prefix hash |
| `request_idempotency_key` | varchar(191) | message + execution scenario + step |
| `created_at` | timestamp | Audit |
| `expires_at` | timestamp | TTL |

Unique key: `(llm_session_state_id, generation, sequence_no)`; idempotency unique per session/request key.

### 2.5 `ToolInvocation`

Side-effect-aware tool state machine.

| Field | Type | Rule |
|---|---|---|
| `id` | bigserial | Primary key |
| `tenant_id` | bigint | Required |
| `transcript_id` | bigint | FK |
| `tool_name` | varchar(128) | Required |
| `tool_idempotency_key` | varchar(191) | Required for high-write tools |
| `risk_class` | varchar(32) | `read`, `write`, `high_write` |
| `status` | varchar(32) | `planned`, `authorized`, `dispatched`, `unknown`, `confirmed` |
| `authorization_hash` | char(64) | Scope/audience/resource authorization fingerprint |
| `resource_version` | varchar(191) | Revalidation token |
| `request_hash` | char(64) | Canonical args hash |
| `result_enc` | bytea | Encrypted result/receipt |
| `result_hash` | char(64) | Dedupe/tamper |
| `external_receipt_id` | varchar(191) | Provider/channel receipt when available |
| `attempt_count` | integer | Default 1 |
| `last_error_code` | varchar(64) | No raw args/output |
| `created_at`, `updated_at` | timestamp | Audit |

Unique key: `(tenant_id, tool_idempotency_key)`.

`unknown` high-write results cannot be retried automatically. Recovery must reconcile the external receipt or require operator action.

### 2.6 `MessageTombstone`

Tracks edit/delete/recall invalidation.

| Field | Type | Rule |
|---|---|---|
| `id` | bigserial | Primary key |
| `tenant_id` | bigint | Required |
| `platform` | varchar(32) | Required |
| `channel_account_id` | bigint | Required |
| `conversation_id` | varchar(191) | Required |
| `message_id` | varchar(191) | Required |
| `tombstone_type` | varchar(32) | `edited`, `deleted`, `recalled`, `redacted` |
| `effective_at` | timestamp | Required |
| `source_cursor` | varchar(191) | Invalidation ordering |
| `created_at` | timestamp | Audit |

Unique key: `(tenant_id, platform, channel_account_id, message_id, tombstone_type)`.

A tombstone invalidates summaries and native generations whose cursor includes the affected message.

## 3. Pure domain contracts (no DB)

- `ConversationScope`
- `AgentConversationContext`
- `ContextMessage` with `Visibility`/`Audience`
- `ContextProvenance`
- `CustomerContext`/`LeadContext` with subject type/ID
- `LLMConversationRef`/`LLMSessionRef`/`ProviderContinuationRef`
- `ProviderCapability`
- `ContextBudget`/`OutputBudget`
- `SourcePolicy`
- `PrivacyGuardResult`

These live under `internal/aiagent/context` and contain no GORM imports.

## 4. Migration order

1. Add MessageHub visibility/audience evidence fields and SessionMessage correlation ID.
2. Add ConversationContext version/source cursor.
3. Create membership, disposition, tombstone tables.
4. Create encrypted session state, transcript, tool invocation tables and blind indexes.
5. Backfill legacy MessageHub visibility to `unknown`; do not infer audience.
6. Backfill old business notes as `legacy_unverified` in read projection; no destructive rewrite.

## 5. Retention and deletion

- Session state, transcript and tool invocation rows have TTL and cascade by tenant/session.
- Canonical key and continuation refs are encrypted; blind index supports rotation by dual-write/reindex.
- Provider-side native objects are deleted when API permits; otherwise references are dropped and best-effort expiry is recorded.
- Message tombstones and membership evidence follow the same tenant deletion policy as source messages.
