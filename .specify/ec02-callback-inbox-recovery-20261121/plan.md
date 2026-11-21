# Implementation Plan: EC-02 Durable Callback Inbox and Compensation

**Branch**: `ec02-callback-inbox-recovery-20261121` | **Date**: 2026-11-21 | **Spec**: [spec.md](spec.md)

**Input**: User-authorized EC-02/M02 scope from `docs/未来任务清单.md` §7.1.8 and M02 detailed spec, restricted to durable inbox, pagination/recovery, and stale-event safety.

## Summary

Extend the existing merchant callback inbox rather than creating another event pipeline. Preserve current raw-body signature verification, subscription/shop lookup, connection consent checks, and commit-before-ack behavior. Add a resource head watermark so a late older event is retained but permanently superseded, a per-subscription persisted compensation cursor with bounded keyset pages and a fixed high-water ID, and exact-page acknowledgment with a current connection/consent fence. A new migration follows v3.104.0 and migrates the existing inbox rows without deleting them. Keep provider/verifier registries empty and do not add network, business, financial, or worker side effects.

## Technical Context

**Language/Version**: Go (repository go.mod; current supported toolchain)

**Primary Dependencies**: Existing Gin/GORM/PostgreSQL stack; no new dependency.

**Storage**: PostgreSQL; existing `merchant_callback_events`, `merchant_callback_subscriptions`, and `merchant_connections`, plus two narrowly scoped tables for cursor state and resource head watermark.

**Testing**: Go standard testing plus the local `testify/require` package convention; real PostgreSQL through `internal/pkg/testutil.NewTestDB` on the dedicated 8217 test instance.

**Target Platform**: Existing user-server service.

**Project Type**: Go HTTP backend / repository persistence.

**Performance Goals**: Compensation reads use a bounded keyset page (maximum 100 rows) and indexed tenant/subscription/event-ID predicates; no total-count query or offset scan.

**Constraints**: Five-layer boundary; all consumer queries fail closed without a tenant; transaction lock order matches callback ingest (connection, subscription, cursor); raw callback bodies and tokens are not persisted; cursor advancement requires an exact page; no worker/dispatch lifecycle or live provider.

**Scale/Scope**: One existing callback subscription per approved binding; two added persistence entities; bounded pages across the current inbox.

## Constitution Check

- **Five-layer architecture**: Pass. Changes remain in the existing model/repository/service/migration layers; no controller/router or frontend change.
- **Tenant isolation**: Pass by design gate. Subscription, connection, cursor, event, and watermark reads are scoped by tenant and subscription; missing/system scope rejects.
- **No raw DB in service**: Pass. All SQL/GORM and multi-row transactions remain in repository/migration.
- **Security/minimal retention**: Pass by design gate. Verification remains first; body digest and minimal authenticated identifiers only; no credentials or raw body added.
- **No live platform integration**: Pass. Production callback route remains configured with a nil verifier map; no external calls or side effects.
- **Tests**: Required PostgreSQL coverage for exact page replay, high-water rollover, restart, cross-tenant rejection, stale-event supersession, current-head acknowledgment, and migration of existing v3.104 rows.

## Research Decisions

See [research.md](research.md). The critical design choice is an ID keyset scoped to a subscription with an event-insert lock and a persisted high-water ID. This avoids timestamps as a lossy pagination boundary and makes later commits visible to the next sweep. The consumer must receive only current pending events; exact-page acknowledgment rechecks the same subscription/connection/consent fence under the repository transaction.

## Data Model and Contracts

- See [data-model.md](data-model.md) for fields, constraints, lifecycle, and historical backfill.
- See [contracts/consumer-interface.md](contracts/consumer-interface.md) for repository/service page and acknowledgment semantics.
- No tenant-facing endpoint is introduced; EC-03 owns scheduling, leases, retries, and lifecycle.

## Project Structure

### Documentation (this feature)

```text
.specify/ec02-callback-inbox-recovery-20261121/
├── spec.md
├── plan.md
├── tasks.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/consumer-interface.md
├── checklists/requirements.md
└── verification.md
```

### Source Code (repository root)

```text
user-server/internal/model/merchant_callback.go
user-server/internal/repository/merchant_callback.go
user-server/internal/repository/merchant_callback_test.go
user-server/internal/service/merchant_callback.go
user-server/internal/service/merchant_callback_test.go
user-server/internal/migration/migrations/v3_105_0_merchant_callback_inbox.go
user-server/internal/migration/migrations/v3_105_0_merchant_callback_inbox_test.go
user-server/internal/migration/migrations/initial_schema.go
```

**Structure Decision**: Extend current merchant callback files; add one focused repository test file and one versioned migration/test. Do not add a new domain, controller, API, generic event bus, or worker package.

## Migration and Upgrade Safety

The current latest merchant migration observed is v3.104.0. Add v3.105.0 without editing earlier migrations. Its `Up` is idempotent, preserves inbox rows, creates the two new keyed tables, adds callback-event disposition/revision columns and indexes, and backfills resource heads in deterministic `(signed_at, id)` order. Migration tests start from the v3.104 schema with rows present, run upgrade twice, verify winner/superseded history and tenant-scoped unique constraints, and preserve event identities.

## Verification Strategy

1. Add failing repository/service PostgreSQL tests for cursor restart/page replay, exact acknowledgment, high-water rollover, tenant isolation, connection revision fencing, and stale events.
2. Run only the new focused test prefixes and the v3.105 migration tests against the dedicated test DB.
3. Run changed-package build/vet and required static backend gates; avoid the coordinator's independent broad tests.
4. Record exact command output, skipped cases and environment limits in `verification.md`; no real provider/live validation is claimed.

## Complexity Tracking

None. The two added tables are required to persist an explicit compensation cursor and latest resource event fence; both are subordinate to the existing callback inbox and do not create a parallel event system.
