# EC-02 Verification — Durable Callback Inbox and Compensation

Date: 2026-11-23. Scope: local engineering slice only. Production callback verifier
registration stays empty; no provider/client/worker/HTTP/business mutation is added.

## T001 — Migration & table ownership evidence

- Highest callback-related migration: `v3_105_0_merchant_callback_inbox.go`
  (`NewMerchantCallbackInboxMigration`), registered in
  `user-server/internal/migration/migrations/initial_schema.go:356`. It follows
  `NewMerchantCallbackMigration` (`:354`) and precedes the separate capability-evidence
  migration (`:358`). Migrations are additive; v3.104.0 and existing callback rows preserved.
- Table ownership (from `user-server/internal/model/merchant_callback.go`):
  `merchant_callback_events` (disposition/supersession/revision fields, lines 42-43),
  `merchant_callback_resource_watermarks` (line 72),
  `merchant_callback_compensation_cursors` (line 93). All tenant-scoped with composite indexes.

## T002 / T019 — Production verifier remains empty

`user-server/internal/router/merchant_callback_routes.go:14` calls
`NewMerchantCallbackService(..., nil)` — verifier map is empty; no provider registration,
no business-state or external-platform mutation.

## Test database note

DB-backed tests use the inner test container via `POSTGRES_TEST_PASSWORD` (password redacted
here; sourced from the local test environment, not committed). Tests fail closed without it.

## T006 / T020 — Migration & targeted test output

```
$ POSTGRES_TEST_PASSWORD=<redacted> go test -race -count=1 ./internal/service/ \
    ./internal/repository/ ./internal/migration/migrations/ \
    -run 'TestMerchantCallbackInbox_|CallbackInbox' -v
ok  hivemtk-user/internal/service            2.926s
    --- PASS: TestMerchantCallbackInbox_CapturesRevisionsBeforeLookup (0.64s)
    --- PASS: TestMerchantCallbackInbox_ServiceBoundaryDelegation (0.07s)
    --- PASS: TestMerchantCallbackInbox_ServiceBoundaryFailClosed (0.04s)
ok  hivemtk-user/internal/repository          2.408s
    --- PASS: TestMerchantCallbackInbox_ResourceWatermarkPreventsStaleAndBreaksTies (0.39s)
    --- PASS: TestMerchantCallbackInbox_CompensationPageSurvivesRestartAndFindsLaterHighWater (0.17s)
    --- PASS: TestMerchantCallbackInbox_CompensationPageRejectsWrongTenantAndStaleConnection (0.06s)
ok  hivemtk-user/internal/migration/migrations 2.787s
    --- PASS: TestMerchantCallbackInboxMigration_BackfillsOldInboxIdempotently (1.07s)
```
All 7 targeted cases PASS under `-race`. `gofmt -l` on changed Go files: empty (formatted).

## T021 — Static gates

| Gate | Result |
|---|---|
| `go build ./...` | rc=0 (empty output) |
| `go vet ./internal/service/ ./internal/repository/` | rc=0 (empty output) |
| `python3 scripts/check-layer-boundary.py` | ✅ 0 new cross-layer violations |
| `bash scripts/check-architecture.sh` | 2 pre-existing reds, both `internal/service/oneid_merge_rule.go:317` `db.GetDB()` — parallel-session in-flight file, not introduced by this slice; not claimed |
| `bash user-server/scripts/check_naked_goroutine.sh` | only finding is the script's own doc example (`SafeGoWithRetry`); no real naked `go func(` in changed files |
| `bash user-server/scripts/check-gorm-only.sh` | ✅ 0 `.Exec(`/`.Raw(` in business packages |

## T022 — Live validation (NOT performed)

`pending_live_validation`: provider signature verification, real callback ordering/fence
behavior, and provider pagination/compensation APIs are not validated. Production verifier
map remains empty. This local slice does not claim live acceptance; per §7.2.8 the EC-02
row stays open until live evidence exists.

## Boundary

No HTTP route, credential/raw body exposure, worker scheduler, retry loop, provider client,
or financial side effect was added. Service layer methods are thin, tenant-guarded
delegations to the repository, which performs full fail-closed validation.
