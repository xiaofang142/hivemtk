# Implementation Plan: Agent Context, Session & Cache

**Branch/Feature**: `028-agent-context-session`  
**Date**: 2026-11-27  
**Spec**: `spec.md`  
**Design**: `docs/superpowers/specs/2026-11-27-agent-context-session-cache-design.md`  
**Research**: `docs/research/AGENT-CONTEXT-SESSION-CACHE-2026-11-27.md`

## Summary

Build one user-server-owned conversation context and execution-session layer for all customer-facing LLM paths. MessageHub remains the channel fact source; a UnifiedMessageReader supplements missing AI/human outbound records from SessionMessage. Every inbound message produces an immutable, audience-filtered context snapshot with customer/lead/memory facts and provenance. The message-bus `LLMSessionID` is the canonical continuation key; independent `ExecutionScenarioID` state rows prevent routing scenarios from sharing provider continuation chains.

Long context is capability-driven and budgeted per provider/model/protocol/region. Session state, prompt-prefix caching, and final-answer caching are separate mechanisms with separate metrics. Native continuation is off by default and requires an explicit allowlist; otherwise requests use stateless replay. The implementation includes concurrency fencing, per-message reply disposition, tool side-effect idempotency, deterministic group privacy guards, log/trace redaction, and cross-repository proxy contracts.

## Technical Context

- **Language/Runtime:** Go 1.23+, Gin, GORM, PostgreSQL, Redis.
- **Architecture:** Router → Controller → Service → Repository → Model; new pure context/policy code lives under `internal/aiagent/context`, orchestration under `internal/service`, persistence under `internal/repository`/`internal/model`.
- **LLM paths:** Existing dispatcher plus provider adapters; Agent Runtime LocalLLMClient must reuse the same capability/session contract.
- **Storage:** PostgreSQL for session state, dispositions, transcript/tool ledger and blind indexes; Redis for full-scope native continuation leases/fencing; MessageHub for channel facts.
- **Testing:** Go `testing`, table-driven unit tests, real PostgreSQL integration tests on `127.0.0.1:8217`, Redis integration where available, fake provider contract tests, opt-in real provider/sandbox tests.
- **Target:** Linux server and local macOS development; no UI work in this feature.
- **Performance goals:** Warm prompt-cache p50 TTFT at least 20% better than cold baseline where provider supports caching; no quality regression on golden single/group fixtures; fallback re-budgets before sending.
- **Constraints:** Tenant/account/conversation fail-closed; no new default data-egress path; platform never persists user conversation content; DB skip is not a pass; no mid-task commits.
- **Scale:** Cursor-based history rather than fixed 100/200-message windows; provider context up to registered capability, not assumed universal 1M.

## Constitution Check

| Gate | Status | Evidence |
|---|---|---|
| Five-layer architecture | PASS | New Controller-free context services; repositories own SQL; models own persistence structs. |
| Tenant fail-closed | PASS | Scope required in context, history, session, lease, disposition and ledger keys. |
| No new default external egress | PASS | Uses configured providers only; tenant/scenario disable and region allowlist. |
| Platform data boundary | PASS | Proxy is transient and explicit; no session/history/profile/lead persistence in platform. |
| Real verification | PASS | Fake and real-provider tests are distinct; DB skip reported as not-run. |
| Go build/vet/gofmt/tests | REQUIRED | Must run before convergence and final claim. |
| One final commit/push | REQUIRED | Only feature-owned files staged; user/parallel changes excluded. |
| SDD six stages | IN PROGRESS | specify draft complete; plan/tasks/implement/converge follow. |

No constitution violations requiring complexity exceptions.

## Core Decisions

1. **Execution identity:** `BusLLMSessionID` is canonical continuation key; `ExecutionScenarioID` isolates execution chains; `DispatchScenario` remains routing/billing only.
2. **Conversation root:** group uses `group_id`; conversation/thread is subordinate. DM uses stable SenderID; SenderName is never identity.
3. **Facts:** MessageHub first, SessionMessage supplemental, CorrelationID dedup. Legacy notes are `legacy_unverified`.
4. **Audience:** Group history is filtered by visibility and membership intervals; unproven visibility fails closed.
5. **Concurrency:** Native continuation holds a full-scope lease across provider/tools/commit with fencing; per-message disposition drives reply watermarks.
6. **Provider modes:** Stateless replay by default; native session allowlist by provider/model/protocol/region; prompt cache independent.
7. **Canonical requests:** Stable sorting and actual adapter-request prefix hash; dynamic data appended late.
8. **Privacy:** Group output guard plus log/trace redaction; full content only in encrypted restricted ledger.
9. **Proxy:** `Source=platform` without local key forces explicit proxy; discount passthrough is off until billing contract supports it.

## Project Structure

### Feature documents

```text
.specify/specs/028-agent-context-session/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── conversation-context.md
│   ├── llm-session-provider.md
│   └── platform-proxy.md
├── checklists/
│   └── requirements.md
└── tasks.md
```

### Source code

```text
user-server/internal/
├── aiagent/
│   ├── context/
│   │   ├── types.go
│   │   ├── scope.go
│   │   ├── policy.go
│   │   ├── budget.go
│   │   ├── prompt.go
│   │   └── privacy.go
│   └── llm/
│       ├── capabilities.go
│       ├── session.go
│       ├── checked.go
│       ├── dispatcher.go
│       ├── dispatcher_dispatch.go
│       ├── llm.go
│       └── ... existing adapter files
├── model/
│   ├── llm_session_state.go
│   ├── conversation_message_disposition.go
│   ├── scenario_transcript.go
│   └── tool_invocation.go
├── repository/
│   ├── llm_session_state.go
│   ├── conversation_message_disposition.go
│   ├── scenario_transcript.go
│   ├── tool_invocation.go
│   └── unified_message.go
├── service/
│   ├── agent_conversation_context.go
│   ├── unified_message_reader.go
│   ├── llm_session_service.go
│   ├── conversation_privacy_guard.go
│   ├── context_log_redaction.go
│   ├── conversation_context.go
│   ├── inbox_ingress.go
│   ├── webhook_ai.go
│   ├── smart_cs_orchestrator.go
│   ├── sales_engine*.go
│   ├── intent_recognition*.go
│   ├── humanize*.go
│   └── lead_*.go
├── channelgw/
│   └── protocol.go
├── dto/
│   └── sales.go
├── platform/
│   └── llm_proxy_client.go
└── config/
    └── server.go

migrations/
└── 101_agent_context_session.sql

platform-server/internal/
├── controller/llm_proxy_controller.go
├── service/llm_proxy_service.go
└── model/llm_billing.go
```

## Phases

### P0a — Contracts and stable identity

Define pure context/session/capability contracts, execution scenarios, stable group/DM root rules, canonical key derivation and failure behavior. No production call-site rewiring yet.

### P0b — Unified history and outbound parity

Implement UnifiedMessageReader, SessionMessage CorrelationID, outbound MessageHub persistence gaps, and real DB fixtures proving no AI/human outbound loss or duplicate history.

### P1 — Context builder and fact/audience policy

Build cursor history, customer/lead/memory/shared-business snapshot, audience filtering, provenance, legacy-note downgrade, ConversationContext CAS/versioning and JSON-history retirement path.

### P2 — Session, provider, privacy and observability foundation

Implement session state/blind index, leases/fencing, dispositions, transcript/tool ledger, provider capability registry, checked output API, canonical actual-request hashing, cached usage, route policy, privacy guard and log/trace redaction. Includes explicit proxy contract updates.

### P3 — Intent/NLP/emotion/objection/humanize

Wire lightweight and full ContextViews into all relevant LLM paths and fast paths; use independent ExecutionScenarioID values.

### P4 — Sales/SmartCS/AgentLoop

Preserve inbound metadata into SalesRequest, use UnifiedMessageReader and full layered context, apply output/privacy guards before real group replies.

### P5 — Lead and Agent Runtime

Wire Detect/Judge/ReAct and Agent Runtime into the same snapshot/session contracts; preserve real evidence IDs and tool provenance.

### P6 — Quality, performance, rollout and convergence

Run golden single/group fixtures, real provider/sandbox cache tests where configured, long input/output tests, concurrency/failure tests, full Go verification, documentation/config updates and SDD convergence.

## File Touchpoint Rules

- Existing large service files receive focused helper extraction only when needed; no wholesale restructure.
- No Controller or Service may directly own SQL; new persistence goes through Repository interfaces.
- Platform repository changes are limited to transient proxy protocol/usage; no user business tables or content storage.
- New environment variables must be added to `.env.example` and `docs/operations/PROD_TOPOLOGY_CHANGES.md`.
- All new migrations use the next free number after current `100_*`, verified immediately before creation.

## Rollout and Rollback

- Provider/scenario capability flags default to conservative stateless mode.
- Native session and long-context expansion are separately gated by allowlist and golden-fixture quality.
- Rollback disables native/long-context flags and returns to stateless compact context; schema remains backward compatible.
- Proxy discount passthrough remains off until an explicit billing contract release.
