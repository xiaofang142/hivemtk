# Bug Verification: SageX upload identify_code authority and installation hardening

- **Slug**: `sagex-upload-identify-code-test-drift-20261125`
- **HEAD**: `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`
- **Assessment**: `./assessment.md`
- **Fix**: `./fix.md`
- **Result**: scoped GREEN; full packages retain only documented pre-existing failures; main independently verified (scoped lane)

## Stage A RED

Command:

```text
go test ./internal/service ./internal/controller -run '^TestSageXUpload' -count=1 -v
```

- Service: 4 failures, all `sagex: unauthorized`.
- Controller: 2 failures, HTTP 401.
- Log: `check-sagex-upload-test-migration-red.log`.

## Stage A GREEN

Same command after test-only migration:

- Service: 6/6 PASS.
- Controller: 5/5 PASS, including Bearer-only, invalid-code, and valid-code-without-Bearer contracts.
- rc=0.
- Log: `check-sagex-upload-test-migration-green.log`.

## Stage B RED

Command:

```text
go test -p 1 ./internal/service ./internal/controller \
  -run '^(TestSageXUploadHardening|TestSageXUploadControllerHardening)' \
  -count=1 -v
```

Expected failures covered:

- legacy non-tenant-aware lookup/create path;
- disabled installation accepted;
- same-tenant different-account installation accepted;
- backfill and TouchLastSeen errors swallowed;
- oversized installation ID accepted;
- conflict path did not re-read;
- typed controller errors/no-token boundary missing.

Log: `check-sagex-upload-hardening-red.log`.

## Stage B GREEN

Same command after production hardening:

- Service hardening: 10/10 PASS, including unique-conflict reread and real-DB concurrency.
- Controller hardening: 1 top-level + 7 subtests PASS.
- rc=0.
- Logs: `check-sagex-upload-hardening-green.log`, `check-sagex-upload-unique-conflict-green.log`.

## Combined Upload+Bootstrap Regression

Command:

```text
go test -p 1 ./internal/service ./internal/controller \
  -run '^(TestSageXUpload|TestSageXBootstrap)' \
  -count=1 -v
```

Result:

- **35 top-level PASS**
- **17 subtests PASS**
- **0 FAIL / 0 SKIP**
- **rc=0**

Log: `check-sagex-upload-bootstrap-combined-r3.log`.

This proves the Upload hardening did not regress the independently verified Bootstrap behavior.

## Repository and Concurrency

| Check | Result | Log |
|---|---|---|
| Repository full | rc=0，157.419s | `check-sagex-upload-repository-full-final.log` |
| Service same-installation concurrency | 3/3 PASS，rc=0 | `check-sagex-upload-service-concurrency-r3.log` |
| Repository create/create-if-absent concurrency | 6/6 PASS，rc=0 | `check-sagex-upload-repository-concurrency-r3.log` |

## Migration Verification

Focused command:

```text
go test ./internal/migration/migrations \
  -run '^TestSageXInstallationUniqueMigration' \
  -count=1 -v
```

Result: **6/6 PASS，rc=0**.

Covered:

- partial unique index creation and idempotency;
- duplicate data fail-closed;
- Down removes only an index created by the migration;
- Down preserves a preexisting model-owned index;
- production AutoMigrate baseline ownership (`outside=true`, `inside=true`);
- nil DB fail-closed.

Log: `check-sagex-bootstrap-index-ownership-green-r2.log`.

Full migration package:

- `TestFullMigrationChainUpThenRollback` passes; no v3.115.0 Down-loss.
- Only `TestEveryImplementedMigrationIsRegistered` fails for pre-existing unregistered `v3.114.0`.
- Clean HEAD reproduction: `migration-v3114-clean-head2c5-baseline.log`.
- Full log: `check-sagex-upload-migration-full-final-r3.log`.

## Full Package Baseline Classification

### Controller

Full controller package rc=1 with only:

- `TestBridgeOutboxWakeController_InitialSignalHasNoPayloadOrClaim`
- `TestBridgeOutboxWakeController_RealBridgeAuthentication/有效凭证仍须明确租户`

Both fail identically on clean HEAD `2c5c53e3d` (`400` vs expected `200`). No SageXUpload/Bootstrap failures.

Logs:

- `check-sagex-upload-controller-full-final.log`
- `controller-wake-clean-head2c5-baseline.log`

### Service

Full service package rc=1 with 26 top-level failures, all a subset of the existing baseline:

- current-only failures: 0
- SageXUpload/Bootstrap failures: 0

Log: `check-sagex-upload-service-full-final.log`.

## Build and Architecture Gates

| Gate | Result | Log |
|---|---|---|
| `go build ./...` | rc=0 | `check-sagex-upload-build-final.log` |
| `go vet ./...` | rc=0 | `check-sagex-upload-vet-final.log` |
| Layer boundary | rc=0，1758 files / 0 new violations | `check-sagex-upload-layer-final.log` |
| Architecture | rc=0，only existing `wecomCSService` warning | `check-sagex-upload-architecture-final.log` |

## Contract Cleanup Checks

- `extractBearerToken` removed from Upload controller.
- `GetByToken` removed from `SageXInstallationStore`; repository method retained and marked deprecated compatibility only.
- Model and SageX protocol documentation now state identify_code-only upload authentication.
- Admin/bootstrap token metadata remains, but is not described or tested as upload authentication.

## Main Independent Verification

- Verdict: **PASS / scoped lane independently verified** at user HEAD `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`.
- Stage A and combined Upload+Bootstrap rc=0; combined run = 35 top-level tests + 17 subtests, 0 FAIL/0 SKIP.
- Migration-focused ownership suite 6/6 PASS; repository full rc=0.
- Controller full has exactly the two clean-HEAD BridgeOutboxWake failures; failure set comparison rc=0.
- Migration full has exactly the clean-HEAD unregistered v3.114.0 registration failure.
- Service full has 26 failures, all a subset of the 30-failure clean baseline; current-only=0 and no SageXUpload/Bootstrap failure.
- Build/vet/layer 1758/0/architecture all rc=0, with only the pre-existing `wecomCSService` warning.
- Corrected continuation: service concurrency 3/3 PASS and repository concurrency 6/6 PASS, with explicit RUN-count assertions; corrected 11-file hash diff rc=0; `git diff --check` rc=0; HEAD checked before/after every step.
- The initial main script’s docs-path and concurrency-regex defects were explicitly discarded as harness noise and corrected before closure.
- Evidence: `main-sagex-upload-verification-20261125.txt`, `main-sagex-upload-verification-summary.txt`, `main-sagex-upload-continuation-summary.txt`.

## Residual / Deferred

- v3.114.0 registration remains a separate baseline gap.
- BridgeOutboxWake failures remain a separate baseline lane.
- Full-service baseline failures were not expanded or repaired.
- No Bearer-auth restoration, rate-limit/audit work, route removal, UI or real-device work was performed.
- No commit/push/deploy/real outbound actions were performed.
- Main independent verification is complete for this scoped lane.
