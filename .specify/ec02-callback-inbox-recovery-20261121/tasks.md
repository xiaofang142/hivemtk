# Tasks: EC-02 Durable Callback Inbox and Compensation

**Input**: `spec.md`, `plan.md`, `research.md`, `data-model.md`, and `contracts/consumer-interface.md` in this directory.

**Prerequisites**: Constitution v1.0.4; Go backend and PostgreSQL test infrastructure already exist.

**Tests**: Explicitly required by the caller and feature specification. Write PostgreSQL tests before implementation and confirm they fail for missing functionality.

**Organization**: Tasks are grouped by the three independently verifiable user stories. Work is performed serially in this lane.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm migration and package touch points without changing unrelated configuration.

- [x] T001 Confirm current highest merchant migration and module table ownership for callback-related tables; record evidence in `.specify/ec02-callback-inbox-recovery-20261121/verification.md`.
- [x] T002 Confirm production callback verifier registration remains empty in `user-server/internal/router/merchant_callback_routes.go`; do not register a provider.

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Define durable inbox event, resource-head, and compensation-cursor storage.

- [x] T003 Add event disposition, supersession link, and verified connection/credential revision fields to `user-server/internal/model/merchant_callback.go`.
- [x] T004 Add tenant-scoped resource watermark and compensation cursor models in `user-server/internal/model/merchant_callback.go` with composite indexes and table names.
- [x] T005 Add v3.105.0 idempotent migration in `user-server/internal/migration/migrations/v3_105_0_merchant_callback_inbox.go` and register it in `user-server/internal/migration/migrations/initial_schema.go`; preserve v3.104.0 and callback rows.
- [x] T006 Add migration upgrade/idempotency/backfill/tenant-unique tests in `user-server/internal/migration/migrations/v3_105_0_merchant_callback_inbox_test.go`.

## Phase 3: User Story 1 - Preserve verified callback events safely (Priority: P1)

**Goal**: Keep exact-body verification, binding, idempotency, and commit-before-ack while recording a deterministic resource head and stale-event disposition.

**Independent Test**: PostgreSQL verified ingress creates one pending head; duplicate replay creates no new row; later older/equal-time events cannot become an obsolete head; invalid binding/revocation yields zero eligible rows.

### Tests for User Story 1

- [x] T007 [US1] Add PostgreSQL cases with prefix `TestMerchantCallbackInbox_` to `user-server/internal/repository/merchant_callback_test.go` for newer-then-older resource events, equal signed timestamps, independent tenant/subscription/resource identities, and unchanged original conflict rows.
- [x] T008 [US1] Add service coverage with prefix `TestMerchantCallbackInbox_` to `user-server/internal/service/merchant_callback_test.go` for capturing connection and credential revisions in a verified event while preserving verifier-before-lookup behavior.

### Implementation for User Story 1

- [x] T009 [US1] Extend `MerchantCallbackRepository.Record` in `user-server/internal/repository/merchant_callback.go` to persist verified connection/credential revisions and atomically update the resource watermark/dispositions under existing connection-then-subscription locks.
- [x] T010 [US1] Extend `MerchantCallbackService.Ingest` in `user-server/internal/service/merchant_callback.go` to pass only the verified connection snapshot revisions into the durable event envelope.
- [x] T011 [US1] Verify conflict detection in `user-server/internal/repository/merchant_callback.go` includes stable signed event identity and never overwrites the original inbox row.

**Checkpoint**: Stale events remain auditable but do not remain current or eligible; ingestion remains fail-closed and provider-free.

## Phase 4: User Story 2 - Resume paged inbox compensation without omissions (Priority: P1)

**Goal**: Add a tenant-scoped, bounded, restartable per-subscription ID-keyset sweep with a persisted high-water and repeatable page boundary.

**Independent Test**: Create more than two pages, read a page and reconstruct repository state, verify stable page IDs, ACK exactly, traverse without omission, then confirm a later insert is found in the next sweep; invalid tenant/fence/partial ACK makes no progress.

### Tests for User Story 2

- [x] T012 [US2] Add PostgreSQL `TestMerchantCallbackInbox_` cases to `user-server/internal/repository/merchant_callback_test.go` for page/high-water persistence, repository restart/repeat read, exact-page acknowledgment, partial/forged/stale ACK rejection, post-high-water event discovery, connection revision invalidation, and cross-tenant/system-scope rejection.
- [x] T013 [US2] Add service-boundary `TestMerchantCallbackInbox_` cases to `user-server/internal/service/merchant_callback_test.go` proving page read/ack delegates only through repository contracts and propagates fail-closed errors.

### Implementation for User Story 2

- [x] T014 [US2] Add bounded keyset `ReadCompensationPage` and exact-page `AcknowledgeCompensationPage` repository methods in `user-server/internal/repository/merchant_callback.go`, using tenant scope and connection→subscription→cursor lock ordering.
- [x] T015 [US2] Add internal page/result types and thin service methods in `user-server/internal/service/merchant_callback.go`; expose no HTTP route, credential, raw body, worker scheduler, or provider client.
- [x] T016 [US2] Ensure sweep capture and page advancement are durable in `user-server/internal/repository/merchant_callback.go`; restart repeats the stored page and new IDs above the snapshot are available in a subsequent sweep.

**Checkpoint**: A restarted reader cannot silently skip accepted events, cursor progress requires exact ACK, and all authorization/revision fences remain current.

## Phase 5: User Story 3 - Prevent older events from replacing newer resource state (Priority: P1)

**Goal**: Ensure page eligibility and acknowledgment consult the current resource watermark, rejecting stale completion while safely scanning superseded history.

**Independent Test**: Read a pending event page, ingest a newer event for the same resource, then verify stale event is not eligible and cannot be acknowledged as current; cursor can progress only as a no-op for the superseded row.

### Tests for User Story 3

- [x] T017 [US3] Add PostgreSQL `TestMerchantCallbackInbox_` cases to `user-server/internal/repository/merchant_callback_test.go` for stale page after newer insert, event ID tie-break, only-current-head acknowledgment, and no cursor movement after invalid current-head ACK.

### Implementation for User Story 3

- [x] T018 [US3] Make page projection and ACK validation in `user-server/internal/repository/merchant_callback.go` require the event to match the persisted resource watermark before marking it applied; keep superseded rows as audit history.
- [x] T019 [US3] Confirm `user-server/internal/router/merchant_callback_routes.go` remains wired with an empty verifier map and that no business-state or external platform mutation is introduced.

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Verify the specified subset, preserve unrelated working changes, and record all evidence.

- [x] T020 Run `gofmt` on only changed Go files and targeted callback repository/service/migration tests with test DB vars from the inner `.env`; write exact output to `.specify/ec02-callback-inbox-recovery-20261121/verification.md` without exposing secrets.
- [x] T021 Run `go build ./...`, `go vet ./...`, `python3 scripts/check-layer-boundary.py`, `bash scripts/check-architecture.sh`, `bash user-server/scripts/check_naked_goroutine.sh`, and `bash user-server/scripts/check-gorm-only.sh` as required; record real pass/fail/skip evidence in `verification.md`.
- [x] T022 Record live provider signature/order/pagination validation as `pending_live_validation` in `.specify/ec02-callback-inbox-recovery-20261121/verification.md`; do not claim live acceptance.

## Dependencies & Execution Order

- Phase 1 confirms the existing migration and owner mapping before schema edits.
- Phase 2 establishes the additive schema required by all stories.
- User Story 1 event-head writes precede User Story 2 paged reads; User Story 3 uses both and follows them.
- Tests precede the corresponding implementation tasks. All tasks run serially; `[P]` work is intentionally not declared.
- Polish runs after all story checkpoints.

## Implementation Strategy

1. Confirm migration/ownership; implement additive migration tests first.
2. Implement event watermark/supersession and verify M02 repeat/conflict semantics.
3. Implement persisted page/high-water cursor and exact acknowledgment, then stale-page fencing.
4. Run only the new focused test prefixes and required local backend gates; record raw results and unresolved live dependency.

## Deviations

- No provider-specific pull/compensation API, worker lease, scheduler, retry loop, shutdown path, or business projection is added. These require provider evidence or belong to EC-03.
- No frontend, controller, router, credentials, external network, outbound send, or financial side effect is in scope.
