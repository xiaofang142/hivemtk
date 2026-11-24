# Tasks: commerce execution backend

## Phase 1: foundation

- [x] T001 Read instructions/constitution/spec-kit and outer M03/M07–21, §7.3.5/7.4/7.5; establish spec.md/plan.md/tasks.md in this directory.
- [x] T002 Add persistent models and migration v3.107.0 in user-server/internal/model/commerce.go, migration/migrations/v3_107_0_commerce.go, initial_schema.go and module_tables.go.
- [x] T003 Add strict tenant/order/policy/member repository transactions in user-server/internal/repository/commerce.go.

## Phase 2: controlled execution

- [x] T004 [US1] Define API DTO and fixed profiles/action schemas in user-server/internal/dto/commerce.go and service/commerce_profiles.go.
- [x] T005 [US1] Implement read/propose/preflight/approval, current role/scope/credential/policy checks in service/commerce.go.
- [x] T006 [US2] Implement submit idempotency, resource/amount/quantity reservation, attempts/outbox and cancel CAS in repository/commerce.go and service/commerce.go.
- [ ] T007 [US3] Implement finite lease dispatch/observe recovery, authoritative postconditions, unknown escalation and event envelope in service/commerce_execution.go.
- [x] T008 [US3] Implement shop policy/member administration and official claim/evidence/review without evidence escalation in service/commerce.go.
- [x] T009 [US1] Wire controller/router and production fail-closed injection in controller/commerce.go, router/commerce_routes.go and business routes.

## Phase 3: platforms and verification

- [x] T010 [US4] Implement fixed per-profile obligations and official workbench/managed workflows, approved connector transport port and Douyin address/refund/reverse/ERP fixture behaviors in service/commerce_profiles.go and commerce_adapter.go.
- [x] T011 [US5] Verify dedicated DB auth first, repository isolation/atomic rollback/concurrent reserve/fencing/idempotency/migration in *_test.go.
- [x] T012 [US5] Verify per-profile success/reject/unknown, revoked approval/scope/connection, awaiting_customer, readback/crash, funds/return/exchange, manual evidence and HTTP contracts with targeted -race tests.
- [ ] T013 [US5] Run full go build/vet, affected package race checks and architecture gates; record compact real results below.
- [x] T014 [US5] Converge spec/plan/task inventory, append any remaining engineering fixes and hand API contract/file list to coordinator.

## Dependencies and execution

Linear T001→T002→T003→T004→T005→T006→T007→T008→T009→T010→T011→T012→T013→T014. No additional agents. Frontend/callback compensation/G3 files owned by coordinator are excluded.

## Validation

2026-11-22 dedicated PostgreSQL auth preflight PASS using container configuration without printing secrets, client pinned 127.0.0.1:8217 and POSTGRES_TEST_DBNAME=user_db_test. Full go build/vet PASS before final convergence patches. Commerce race run service/router/migrations: 15 top-level + 53 subtests PASS, 0 fail/skip; includes 29 profiles with normal/native rejection/unknown recovery, Douyin buyer review/disconnect, current role/limit/credential/scope/pause/approval expiry, refunds/currency/reverse actions, concurrent intent reservations, lease recovery, dual worker, remote success + local persistence failure, ERP single route, HTTP DTO/prod-provider denial, migration idempotency, legacy generic aftersale business errors. Log: /var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/zcode-bash-khxIpL/sess_subagent_agent_b15fcc2a-8a31-4a1f-9a4a-ac95e126242b/call_d92e46da389f45a99adb4a45f3fb8b29-stdout.log.

Final layer-boundary (1642 production files, zero violations), architecture, GORM-only and diff checks PASS. Global naked goroutine gate FAIL on existing non-commerce production paths; no new commerce production naked goroutine. scripts/api_verify_full.py not-run: it targets unrelated full application admin/bridge chain and needs a running authorized local API/admin fixture; HTTP httptest and real DB assertions cover commerce instead. Frontend/UI owned by coordinator, not verified here. No load/DR/24-hour observation or production platform calls.

Production/live status remains pending_live_validation. Profile tests exercise injectable semantic contracts and shared workflow; they do not implement or validate real vendor wire APIs. Existing v3.106.0 unchanged; v3.107.0 additive preserves historical financial/idempotency facts on Down. Plan deviation: auxiliary documentation consolidated into these three authorized artifacts/source DTOs; no external side effects, commit/push/deploy.

## Phase 4: Convergence

- [x] T015 CRITICAL per FR-004/US3: close final code audit fixes in user-server/internal/service/commerce.go and repository/commerce.go (partial): prevent second workbench claim after dispatch, carry externalRef into Observe, lock current SystemUser role rows, continue manual_review observation. Add linked-record and reclaim regressions then rerun build/vet/race.
- [ ] T016 per US4/RV10: provide actual approved per-platform wire contract/transport implementations, pinned shop/region API versions and authoritative workbench evidence ingestion in service/commerce_adapter.go (missing). Current semantic wrapper plus fixture profile matrix is not a native vendor implementation. Keep production nil-provider and EC-12–19 incomplete until those contracts and live validation are supplied.
- [ ] T017 per FR-008/US3/RV09: complete general message/reach command classification interoperability, event outbox consumer acknowledgements/dead-letter retry budgets/owner observability, lease heartbeat and per-tenant fairness capacity verification in service/commerce_execution.go and app/commerce_execution_wiring.go (partial). Current commerce bounded scanner and durable events do not prove the full EC-03 cross-domain lifecycle.
- [ ] T018 per M08/M14/US4: wire existing Agent BusinessToolProvider/checkpoints to commerce proposals and complete warehouse receipt/inspection/inventory reservation event chain with source-specific evidence and durable correction ledger (partial). Current registered schemas and independent return/exchange operation contracts are a tested foundation, not the complete warehouse product.
- [ ] T019 per BR09/M20: finish authenticated owner re-verification for approval-policy lowering, manual platform evidence review/definitive not-executed closure and automatic source-bound case/workbench handshake before production use (partial). Current policy writes require current DB admin and increment version; human evidence remains manual_review and never releases unknown funds.

Final convergence validation: latest full build PASS. Package-level final vet/race was blocked by coordinator's in-flight contact_consent_scope_test.go:14 unused repo (not edited here). Selected actual platform GoFiles from `go list` plus commerce_test.go/aftersale_test.go were then tested with -race: 15 top-level + 45 subtests PASS including final existing-case association and repeat-claim prevention. This selected-file run is explicitly separate from the earlier complete-package run. Final independent router/repository/migrations -race PASS: 3 top-level + 8 subtests. Final checks therefore cover 18 top-level + 53 subtests, 0 failed/skipped in these scoped runs. Logs: /var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/zcode-bash-khxIpL/sess_subagent_agent_b15fcc2a-8a31-4a1f-9a4a-ac95e126242b/call_06a4cfe514d6493ab29fc9232ac2ee09-stdout.log and /var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/zcode-bash-khxIpL/sess_subagent_agent_b15fcc2a-8a31-4a1f-9a4a-ac95e126242b/call_f26a0503790f4bedb5f495b0cc3c40b5-stdout.log. Full package vet/race should be repeated by coordinator after its in-flight test compiles.

Convergence verdict: actionable gaps remain; engineering slice implemented and verified as above, full EC scope is **not Converged**. T016–19 retain exact unbuilt product obligations instead of pretending missing external adapters are completed by mocks. Coordinator requested stopping at a recoverable point after final tests; no external task rows removed.
