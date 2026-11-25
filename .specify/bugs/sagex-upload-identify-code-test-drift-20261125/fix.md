# Bug Fix: SageX upload identify_code authority and installation hardening

- **Slug**: `sagex-upload-identify-code-test-drift-20261125`
- **HEAD**: `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`
- **Assessment**: `./assessment.md`
- **Status**: applied / scoped GREEN / main independently verified (scoped lane)

## Summary

SageX upload now treats `identify_code` as the sole upload credential. The six legacy token-as-identify-code tests were migrated first, then the production upload path was hardened with tenant-aware atomic installation resolution, disabled/account-mismatch rejection, visible persistence errors, installation-ID bounds, typed protocol errors, and identify_code-only documentation. Legacy token fields remain only as deprecated admin/bootstrap compatibility metadata.

## Changes

| File | Change |
|---|---|
| `user-server/internal/service/sagex_upload.go` | Explicit `tenant.OnlyTenant` + `GetByTenantAndInstallationID` + `CreateIfAbsent`; conflict re-read; tenant/account/status validation; backfill/TouchLastSeen errors propagated; installation ID max length; token lookup removed from Upload interface |
| `user-server/internal/controller/sagex_upload.go` | Upload typed 400/401/403/409/500 mapping with static machine reasons; removed unused `extractBearerToken` |
| `user-server/internal/repository/sagex_upload.go` | Tenant-aware lookup/CreateIfAbsent reused; legacy `GetByToken` marked deprecated compatibility only |
| `user-server/internal/model/sagex_upload.go` | Model comments now state identify_code-only upload auth; token marked deprecated compatibility metadata |
| `sagex/docs/integrations/notification-upload-v1.md` | HiveMTK upload section now documents identify_code-only auth, tenant/account/status enforcement, and deprecated token metadata |
| `user-server/internal/service/sagex_upload_test.go` | Valid identify-code resolver fixture; installation-ID-keyed fake repo; six legacy tests migrated |
| `user-server/internal/controller/sagex_upload_test.go` | Positive tests migrated; Bearer-only, invalid-code, and valid-code-without-Bearer contracts added |
| `user-server/internal/service/sagex_upload_hardening_test.go` | New hardening, conflict, error, length, and real-DB concurrency regressions |
| `user-server/internal/controller/sagex_upload_hardening_test.go` | New typed-error/no-token/no-internal-detail regressions |
| `user-server/internal/migration/migrations/v3_115_0_sagex_installation_unique.go` | Ownership snapshot moved outside transaction; Down only removes an index actually created by this migration |
| `user-server/internal/migration/migrations/v3_115_0_sagex_installation_unique_test.go` | Added preexisting/model/production-baseline ownership regressions |

## Stage A — test-only migration

- Replaced token-as-identify-code service fixtures with an injected valid `ChannelResolver`.
- Keyed fake installation lookup by `installation_id`.
- Migrated four service and two controller positive tests to identify_code-only.
- Added Bearer-only→401, invalid identify_code→401, and valid identify_code without Bearer→accepted contracts.
- No production code changed during stage A.

### Stage A RED→GREEN

- RED: `check-sagex-upload-test-migration-red.log` — 4 service + 2 controller failures.
- GREEN: `check-sagex-upload-test-migration-green.log` — service 6/6, controller 5/5, rc=0.

## Stage B — production hardening

- `HandleUpload` pins all installation access to `tenant.OnlyTenant(ctx, resolved.TenantID)`.
- Not-found is distinct from DB error; read/create/re-read errors are returned.
- Existing rows must match resolved tenant and account and have `Status==1`.
- Empty account backfill and `TouchLastSeen` failures are no longer swallowed.
- Installation IDs are trimmed, empty IDs retain the legacy generated prefix, and schema length is enforced without UUID-only validation.
- `CreateIfAbsent` plus conflict re-read provides one row/token under concurrent same-key uploads.
- Controller emits stable typed protocol errors and never includes token/internal error text.
- Upload no longer exposes or calls Bearer-token lookup.

### Stage B RED→GREEN

- Hardening RED: `check-sagex-upload-hardening-red.log` — tenant-aware atomic path, disabled/account mismatch, backfill/touch errors, ID bounds, conflict handling, and typed controller behaviors failed as expected.
- Hardening GREEN: `check-sagex-upload-hardening-green.log` — service hardening and controller typed-error suites passed.
- Unique-conflict reread RED: `check-sagex-upload-unique-conflict-red.log`.
- Unique-conflict reread GREEN: `check-sagex-upload-unique-conflict-green.log`.
- Combined Upload+Bootstrap regression: `check-sagex-upload-bootstrap-combined-r3.log` — **35 top-level PASS + 17 subtests PASS，0 FAIL/SKIP，rc=0**.

## Migration Ownership Correction

The first full migration run exposed a v3.115.0 Down regression: unconditional index removal deleted a model-owned baseline index. The migration now:

1. reads `preexistingIndex` before opening the transaction;
2. skips creation when the index preexisted;
3. confirms again inside the transaction before creating;
4. sets `createdIndex=true` only after a successful create;
5. makes Down a no-op for preexisting/model-owned indexes.

Focused migration regressions: **6/6 PASS**, including production AutoMigrate baseline ownership (`outside=true`, `inside=true`). Log: `check-sagex-bootstrap-index-ownership-green-r2.log`.

## Local Verification

| Check | Result | Log |
|---|---|---|
| Combined Upload+Bootstrap | 35 top-level + 17 subtests PASS，rc=0 | `check-sagex-upload-bootstrap-combined-r3.log` |
| Repository full | rc=0，157.419s | `check-sagex-upload-repository-full-final.log` |
| Service concurrency | 3/3 PASS，rc=0 | `check-sagex-upload-service-concurrency-r3.log` |
| Repository concurrency | 2 tests × 3 = 6/6 PASS，rc=0 | `check-sagex-upload-repository-concurrency-r3.log` |
| Migration focused | 6/6 PASS，rc=0 | `check-sagex-bootstrap-index-ownership-green-r2.log` |
| Migration full | rc=1，only pre-existing v3.114.0 registration failure | `check-sagex-upload-migration-full-final-r3.log` |
| Controller full | rc=1，only 2 pre-existing BridgeOutboxWake failures | `check-sagex-upload-controller-full-final.log` |
| Service full | rc=1，26 baseline failures，current-only=0，SageX failures=0 | `check-sagex-upload-service-full-final.log` |
| Build | rc=0 | `check-sagex-upload-build-final.log` |
| Vet | rc=0 | `check-sagex-upload-vet-final.log` |
| Layer boundary | rc=0，1758 files / 0 new violations | `check-sagex-upload-layer-final.log` |
| Architecture | rc=0，only existing `wecomCSService` warning | `check-sagex-upload-architecture-final.log` |

## Baseline Classification

- `TestEveryImplementedMigrationIsRegistered` fails on clean HEAD `2c5c53e3d` because `v3.114.0` is implemented but unregistered. Evidence: `migration-v3114-clean-head2c5-baseline.log`. Not changed in this lane.
- `TestBridgeOutboxWakeController_InitialSignalHasNoPayloadOrClaim` and `TestBridgeOutboxWakeController_RealBridgeAuthentication/有效凭证仍须明确租户` fail identically on clean HEAD. Evidence: `controller-wake-clean-head2c5-baseline.log`. Not changed in this lane.
- Full service failures are a subset of the existing baseline; no current-only failures and no SageXUpload/Bootstrap failures.

## Deviations from Assessment

The migration Down implementation was tightened after the full-chain regression: ownership is now snapshotted before the transaction, and Down only removes an index created by the same migration instance. This preserves the approved “Down only rolls back this lane’s new index” requirement while protecting AutoMigrate-owned baseline indexes.

## Follow-ups

- Main independent verification completed for the Upload hardening and migration ownership behavior; evidence is recorded in `test.md`.
- v3.114.0 registration and BridgeOutboxWake baseline failures remain separate lanes.
- No commit/push/deploy/real outbound/UI actions were performed.
