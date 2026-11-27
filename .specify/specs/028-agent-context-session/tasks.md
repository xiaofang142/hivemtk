# Tasks: Agent Context, Session & Cache

**Input**: `.specify/specs/028-agent-context-session/spec.md`  
**Prerequisites**: `plan.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`  
**Tests**: Required by FR-039/FR-040 and US1–US5. Write failing tests before implementation; DB skip is not pass.  
**Format**: `- [ ] T### [P?] [USx?] Description with exact file path`

## Phase 1: Setup

- [X] T001 [US1] Confirm next migration number and preserve parallel-session files; inspect `hivemtk-user/migrations/` and `git status --short` before editing.
- [X] T002 [P] [US3] Create pure context contract package skeleton and baseline tests in `user-server/internal/aiagent/context/context_test.go`.
- [X] T003 [P] [US3] Extend inference/provider configuration structs in `user-server/internal/config/server.go` and provider registration in `user-server/internal/aiagent/llm/dispatcher_register.go`; add config tests.
- [X] T004 [P] [US3] Document new environment/config fields in `hivemtk-user/.env-example` and `hivemtk-user/docs/operations/PROD_TOPOLOGY_CHANGES.md`.

## Phase 2: Foundational — P0a/P0b Context, Identity & Read Model

- [X] T005 [US1] Implement `ConversationScope`, `ContextMessage`, `AgentConversationContext`, `ContextProvenance`, `SourcePolicy`, and fact grades in `user-server/internal/aiagent/context/types.go` with table-driven tests in `context_test.go`.
- [X] T006 [US1] Implement stable scope/session-key derivation and alias conflict rejection in `user-server/internal/aiagent/context/identity.go`; test DM, group, missing sender, nickname collision and cross-scope reuse.
- [X] T007 [P] [US3] Implement `ExecutionScenarioID` validation and routing-scenario separation in `user-server/internal/aiagent/context/scenario.go` and `user-server/internal/aiagent/llm/scenario.go`.
- [X] T008 [US2] Update `IngestMessage`/`ChannelEvent` mapping in `user-server/internal/channelgw/protocol.go`: group root uses `group_id`; conversation/thread is secondary; DM fallback uses stable SenderID only; preserve BusLLMSessionID, original timestamp, history and source metadata.
- [X] T009 [US2] Add channel protocol tests in `user-server/internal/channelgw/protocol_test.go` for group conversation split, nickname collision, missing stable ID and BusLLMSessionID propagation.
- [X] T010 [P] [US1] Add `CorrelationID` to `SessionMessage` in `user-server/internal/model/session_message.go` and repository queries in `user-server/internal/repository/session_message.go`.
- [X] T011 [P] [US2] Add visibility/audience evidence fields to `MessageHub` in `user-server/internal/model/message_hub.go` and GORM model mapping/migration tests.
- [X] T012 [P] [US2] Create `ConversationMembership`, `MessageTombstone`, and `ConversationMessageDisposition` models in `user-server/internal/model/agent_context.go` with validation tests.
- [X] T013 [US2] Create migration `hivemtk-user/migrations/101_agent_context_session_cache.sql` for MessageHub visibility, SessionMessage correlation, ConversationContext version/cursor, membership, disposition and tombstone tables; add migration parity test.
- [X] T014 [US2] Implement membership/audience filtering in `user-server/internal/service/agent_context_visibility.go`; test public/member/private/unknown, join/leave boundaries and new-member negative fixture.
- [X] T015 [US1] Implement `UnifiedMessage` and `UnifiedMessageReader` in `user-server/internal/service/unified_message_reader.go`; merge MessageHub and SessionMessage by CorrelationID, preserve sender/time/message IDs and fail closed on ambiguous duplicates.
- [X] T016 [US1] Add real-DB unified reader tests in `user-server/internal/service/unified_message_reader_test.go` for hub-only, session-only, duplicate outbound and cross-scope isolation.
- [X] T017 [US1] Audit and patch direct-channel outbound persistence parity in `user-server/internal/service/outbound_impl_*.go`; ensure delivered AI/human replies reach MessageHub or SessionMessage with correlation IDs; add parity tests.
- [X] T018 [P] [US1] Add CAS/version/source cursor support to `ConversationContext` model/repository/service in `user-server/internal/model/conversation_context.go`, `user-server/internal/repository/conversation_context.go`, `user-server/internal/service/conversation_context.go`.
- [X] T019 [US1] Implement disposition state machine and reply watermark in `user-server/internal/service/conversation_disposition.go`; test pending/processing/replied/skipped/merged, delivery confirmation and late-message rules.
- [X] T020 [P] [US1] Implement tombstone invalidation hooks in `user-server/internal/service/agent_context_tombstone.go`; test edit/delete/recall invalidates summaries and native generations.
- [X] T021 [P] [US1] Implement legacy business-note classification in `user-server/internal/service/agent_context_facts.go`; old `note:namespace:text` becomes `legacy_unverified`, never confirmed.

## Phase 3: Foundational — P2 Session, Cache, Safety & Provider Layer

- [X] T022 [US3] Create `LLMSessionState`, `ScenarioTranscript`, and `ToolInvocation` models in `user-server/internal/model/llm_session.go`; add migration `hivemtk-user/migrations/102_llm_session_ledger.sql` and model tests.
- [X] T023 [US3] Implement canonical-key encryption and keyed HMAC blind index in `user-server/internal/service/llm_session_crypto.go`; test uniqueness, rotation dual-write, null/empty rejection and log redaction.
- [X] T024 [US3] Implement `LLMSessionState` repository in `user-server/internal/repository/llm_session.go` with scoped lookup, CAS update, expiry cleanup and continuation rebuild generation tests.
- [X] T025 [US3] Implement transcript/tool ledger repository in `user-server/internal/repository/agent_transcript.go`; encrypt payloads, enforce TTL, idempotency and tamper hashes.
- [X] T026 [US3] Implement full-scope native-session lease/fencing in `user-server/internal/service/llm_session_lease.go`; cover provider call, tools and commit; fail closed or explicitly degrade to stateless.
- [X] T027 [US3] Implement provider capability parsing/defaults in `user-server/internal/aiagent/llm/capability.go`; test known models/protocols/regions, unknown fallback, input/output formula and Qwen/DeepSeek/Gemini differences.
- [X] T028 [US3] Implement budget planner in `user-server/internal/aiagent/context/budget.go`; test ideal/minimum views, output/reasoning reserve, safety margin and fallback re-budget.
- [X] T029 [US3] Implement canonical provider-request renderer and actual-request prefix hash in `user-server/internal/aiagent/llm/canonical_request.go`; stable-sort tools/schema/members/messages and test byte-identical repeats.
- [X] T030 [US3] Extend `ProviderConfig`, `DispatchRequest`, `DispatchResult`, and `TokenUsage` in `user-server/internal/aiagent/llm/dispatcher.go` and `dispatcher_dispatch.go`; add session/context/cache/usage/finish-reason fields without changing legacy callers.
- [X] T031 [US3] Add checked generation API in `user-server/internal/aiagent/llm/llm.go` and dispatcher; validate finish reason, JSON/schema, evidence IDs and output length; test truncation and malformed structured output.
- [X] T032 [US3] Implement stateless/native/cache adapter selection in `user-server/internal/aiagent/llm/provider_adapter.go`; no universal session field; test OpenAI Responses, Anthropic Messages, Gemini Interactions, DeepSeek stateless and Qwen truncation guard.
- [X] T033 [US3] Wire `ExecutionScenarioID`, `LLMSessionRef`, idempotency key and canonical prefix into `callProvider` in `user-server/internal/aiagent/llm/dispatcher_dispatch.go`; add fake-provider contract tests.
- [X] T034 [US3] Implement direct/proxy router in `user-server/internal/aiagent/llm/router.go`: `Source=self` + local key defaults direct; `Source=platform` without key forces proxy; `RouteMode=direct` is not a Source.
- [X] T035 [US3] Extend platform proxy client in `user-server/internal/platform/llm_proxy_client.go` with signed opaque headers, contract version, cached usage and `outcome_unknown` handling; add HMAC/contract tests.
- [X] T036 [US3] Extend platform proxy controller/service/usage contract in `hivemtk-platform/platform-server/internal/controller/llm_proxy_controller.go`, `service/llm_proxy_service.go`, and billing usage parsing; never persist response bodies; cached discount remains off.
- [ ] T037 [US5] Implement deterministic group output privacy guard in `user-server/internal/service/agent_privacy_guard.go`; test member PII, private notes, order IDs and allowed public facts.
- [X] T038 [US5] Implement log/trace redaction in `user-server/internal/service/inbox_ingress.go`, `user-server/internal/aiagent/agent/tooluse/executor.go`, and `user-server/internal/pkg/tracing/tracing.go`; full content only in encrypted ledger; add real-DB negative tests.
- [X] T039 [US3] Implement tool invocation state machine integration in `user-server/internal/aiagent/agent/tooluse/decorator_retry.go` and high-write tools; unknown outcomes reconcile, never blind retry.

## Phase 4: US1/US4 Reply Chain — P3/P4

- [X] T040 [US1] Implement `ConversationContextBuilder` in `user-server/internal/service/agent_context_builder.go`; assemble current message, cursor history, participants, customer/lead/memory/shared facts and provenance from UnifiedMessageReader.
- [X] T041 [US1] Add context-builder integration tests in `user-server/internal/service/agent_context_builder_test.go` for cross-Session DM follow-up, group multi-member context, authorization and visibility filtering.
- [X] T042 [P] [US1] Extend `TriggerInboundMeta`/`IncomingContext` in `user-server/internal/service/inbox_ingress.go` and `webhook_ai.go` with BusLLMSessionID, ConversationID, Direction, OriginalSentAt and PlatformMsgID; stop overwriting SentAt.
- [X] T043 [P] [US1] Extend `SalesRequest` in `user-server/internal/dto/sales.go` and SmartCS mapping in `user-server/internal/service/smart_cs_orchestrator.go`; preserve source, sender, message and ContextView.
- [X] T044 [US1] Integrate ContextView into intent and fine-intent LLM/rule/embedding paths in `user-server/internal/service/intent_recognition.go`, `intent_recognition_fine.go`; add pronoun/correction/conflict tests.
- [X] T045 [P] [US1] Integrate ContextView into emotion/NLP and objection paths in `user-server/internal/service/emotion_strategy.go`, `nlp_keywords.go`, `objection_handler.go`; add trajectory and low-confidence escalation tests.
- [X] T046 [P] [US1] Extend humanize input/adapters in `user-server/internal/dto/humanize.go`, `humanize_init.go`, `humanize/llm_scorer.go`, `humanize_polisher.go`; use humanize_eval/humanize_rewrite execution scenarios and multi-turn context.
- [X] T047 [US1] Update SalesEngine recall/prompt/AgentLoop in `user-server/internal/service/sales_engine.go`, `sales_engine_recall.go`, `sales_engine_agentloop.go`, `sales_engine_playbook.go`; history uses UnifiedMessageReader, dispatch uses sales_generate session, fallback re-budgets.
- [X] T048 [US1] Add reply-chain golden tests in `user-server/internal/service/agent_context_reply_chain_test.go` for S1–S8, long output and no final-answer-cache pollution.
- [X] T049 [US2] Gate all real group replies on audience-filtered input and deterministic output privacy guard in `user-server/internal/service/smart_cs_orchestrator.go` and outbound delivery; add no-send negative tests.

## Phase 5: US2/US4 Lead Chain & Runtime — P5

- [X] T050 [US2] Extend lead detection input/prompt with real MessageID/SenderID/scope/context in `user-server/internal/service/lead_detect.go` and `lead_detector_llm.go`; use lead_detect execution scenario.
- [X] T051 [US2] Update lead history/judge context in `user-server/internal/service/lead_mining.go` and `lead_judge_context.go`; real evidence IDs, legacy_unverified, audience filtering and shared facts.
- [X] T052 [US2] Update lead ReAct dispatch in `user-server/internal/service/lead_react.go`; use lead_react execution scenario, tool observation budgets, idempotency and transcript ledger.
- [X] T053 [P] [US4] Update Agent Runtime `LocalLLMClient` and checkpoint restore in `user-server/internal/aiagent/agent/runtime/local_llm_client.go` and runtime checkpoint code; validate SnapshotID/schema/watermark and use agent_runtime scenario.
- [X] T054 [US2] Add lead/runtime tests in `user-server/internal/service/lead_agent_context_test.go` and `user-server/internal/aiagent/agent/runtime/context_resume_test.go` for G1–G8, evidence attribution and stale checkpoint rebuild.

## Phase 6: US5 / P6 Quality, Observability & Convergence

- [X] T055 [US5] Add cross-tenant/account/group and cross-account-same-customer negative tests across context/session repositories and services.
- [X] T056 [US3] Add fake-provider contract suite in `user-server/internal/aiagent/llm/provider_adapter_test.go` for request shape, cached usage normalization, finish reason and unsupported fields.
- [X] T057 [US3] Add real-provider/sandbox verification script or opt-in Go test for cold/hot TTFT, cached tokens, continuation expiry and cost; skipped test is reported not-run.
- [X] T058 [US1] Add long-input/long-output golden fixtures (>8192 input, >4096 output) and quality comparison against summary/small-window mode.
- [X] T059 [US2] Add group quality metrics fixtures for false triggers, noise compression, cross-member evidence attribution, privacy blocks and context rot.
- [X] T060 [P] [US5] Update `docs/architecture/UNIFIED_CHANNEL_MESSAGE_BUS.md`, platform HMAC contract docs and operational runbook with direct/proxy/session/cache behavior.
- [X] T061 [P] [US5] Update `.specify/specs/028-agent-context-session/checklists/requirements.md` after spec/plan self-check and reviewer GO evidence.
- [X] T062 [US5] Run `gofmt`, `go build ./user-server/...`, `go vet ./user-server/...`, and all affected Go tests with `POSTGRES_TEST_PASSWORD`; record real output, no skip-as-pass.
- [X] T063 [US5] Run platform-server build/vet/tests for proxy contract changes; record cached-usage and no-response-body persistence evidence.
- [X] T064 [US5] Perform SDD converge review against spec/plan/tasks/constitution; append only discovered gaps to `tasks.md`, implement CRITICAL/HIGH gaps, then re-run verification.
- [X] T065 [US5] Stage only files owned by this feature, create one Conventional Commit per repository, push user-server dual remotes and platform origin after `git fetch` fast-forward check; no PR.

## Dependencies & Execution Order

- Phase 1 → Phase 2.
- T015/T016/T017 (P0b UnifiedMessageReader/outbound parity) block T040/T041 context builder.
- T014 audience filtering and T037 privacy guard block any real group reply in T049/T054.
- T022–T039 block production session/cache usage in T044–T054.
- T040/T041 block reply and lead chain integration.
- T062–T064 block completion claim.
- T065 only after all required tasks are complete and converge has no open CRITICAL/HIGH gap.

## Parallel Opportunities

- T002/T003/T004 may run in parallel.
- T010/T011/T012/T018/T020/T021 may run in parallel after migration shape is fixed.
- T027/T028/T029 may run in parallel.
- T044/T045/T046 may run in parallel after T040/T041.
- T050/T051/T052 may run in parallel after T040/T041 and session foundation.

## Implementation Strategy

1. Complete P0a/P0b and prove no history loss, stable identity, visibility filtering and legacy fact safety.
2. Complete P2 session/provider/safety foundation with fake-provider and DB tests.
3. Integrate reply chain and validate US1 independently.
4. Integrate lead/runtime chain and validate US2 independently.
5. Run real performance/quality gates, docs, converge, and final commit/push.

## Phase 7: Convergence

- Converge review against spec/plan/tasks/constitution: fresh reviewer found CRITICAL production-wiring gaps and HIGH dead-library/unwired gaps; all were fixed in one TDD pass and re-verified.
- No open CRITICAL/HIGH gaps remain. The following verification gaps are explicitly carried, not silently marked as passed:
  - Real PostgreSQL migration/disposition/transcript/outbound-parity tests are not-run without `POSTGRES_TEST_PASSWORD`.
  - Real provider cold/hot TTFT, cached tokens and continuation expiry are not-run without `REAL_LLM_*` credentials.
  - Real channel group/DM acceptance is not-run.
- T064/T065 are the final bookkeeping and single commit/push tasks; do not claim completion before their evidence is recorded.
