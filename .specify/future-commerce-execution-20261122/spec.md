# Future commerce execution

## Scope and source

2026-11-22 authorized implementation of EC-03 and EC-07–21 backend in this isolated worktree. Authority: outer docs/未来任务清单.md M03/M07–21, §7.3.5, §7.4 RV02/03/08/09/10 and §7.5 BR02/03/05/06/08/09. Frontend, EC-02 callback compensation and EC-22+ belong to coordinator. No commit, push, deployment or real external effects.

## User stories and acceptance

- US1 P0: authorized tenant/shop operator reads existing external_orders mirror and current provider snapshot, proposes structured action, sees missing parameters, obligations and expiry, obtains valid approval and submits one stable business revision. Cross-tenant/shop/buyer references deny access; mirror alone cannot authorize writes.
- US2 P0: operation, approval, attempt, order guard, minimum-unit currency reservation and outbox survive restart. Same key/same parameters returns original; changed parameters conflict. Concurrent intents and native/ERP/manual paths share the order guard. Unknown never releases reservation or blindly resubmits.
- US3 P0: worker claims short leases, records dispatch before outbound, recovers by Observe first. Current connection credential revision/scope, actor/approver role/limit, policy and stop-write revisions rechecked. Cancellation competes atomically with dispatch. Authoritative action postconditions, cash state and manual evidence remain distinct.
- US4 P1: fixed platform profiles implement registered action schema/obligation checks, official workbench claim/steps/attestation/review and managed observation. API routes require injected approved transport, never fake production adapters. Douyin awaiting_customer and write-after-disconnect readback, refunds, reverse steps and ERP single-owner routing have fault fixtures.
- US5 P0: all production profiles retain pending_live_validation; no automatic support assertion. Build/vet, affected race tests, migration/tenant/HTTP/fault checks record real compact results in tasks.md.

## Functional requirements

FR-001 five layers; DTO responses never leak credential ciphertext, token, or full address. FR-002 extend/link existing external_orders, after_sales and order_change_requests rather than replacing their primary records. FR-003 stable tenant/shop/action/intent/revision fingerprint includes canonical parameters and selected write route. FR-004 immutable approval snapshot and 15 minute expiry; snapshot 30 seconds or funds 15 seconds and fresh preflight before dispatch. FR-005 finite 60 second lease and fencing; dispatch crash enters unknown and subsequent work only observes. FR-006 only platform evidence plus action-specific postconditions may mark succeeded; manual attestation is explicitly human provenance. FR-007 no unknown timeout release; resource guards outlive network slots. FR-008 typed business event schema/version/trace and transactional outbox support internal integrations without resending the business action. FR-009 quantity and refund balance/currency enforce checked integer arithmetic. FR-010 per-profile routes/action restrictions are executable; credentials and platform live contract prerequisites fail closed.

## Success criteria and limits

Zero duplicate external submits in deterministic lease/crash/replay tests, zero unauthorized reads/writes, zero evidence-free completion; all enabled fixture profiles exercise normal/reject/unknown. Engineering verification does not establish live shop authorization, production funds, load capacity, backup recovery or 24 hour release observation. These remain pending_live_validation and explicitly unclaimed.
