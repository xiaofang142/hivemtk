# Bug Fix: Webhook admission can acknowledge an event that was never saved

- **Slug**: webhook-retryable-admission-20261120
- **Fixed**: 2026-11-20
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

Removed the pre-persistence Redis reservation from `Receive`. Rate limiting now precedes durable event creation, and the database unique event ID arbitrates duplicates; requests fail closed when persistence is unavailable and remain rejected on other write errors.

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/service/webhook.go` | modified | Removed the pre-write `isDuplicate` short circuit; require a connected event repository; retain duplicate acknowledgement only for a durable unique-key conflict. |
| `user-server/internal/service/webhook_channel04_admission_test.go` | added | Adds rate-limit retry, cancelled-write retry, stale reservation, concurrent duplicate admission, same-process recovery, and abrupt process-exit/restart recovery coverage. |

## Tests Added or Updated

- `user-server/internal/service/webhook_channel04_admission_test.go::TestWebhookReceive_RateLimitDoesNotReserveDedupKey` — rejection leaves no cache reservation; retry after token recovery stores one row.
- `user-server/internal/service/webhook_channel04_admission_test.go::TestWebhookReceive_PersistFailureCanRetry` — canceled database write is not acknowledged or reserved; retry stores one row.
- `user-server/internal/service/webhook_channel04_admission_test.go::TestWebhookReceive_StaleReservationDoesNotBlockDurableAdmission` — an orphaned legacy cache key cannot block a new durable event.
- `user-server/internal/service/webhook_channel04_admission_test.go::TestWebhookReceive_ConcurrentReplayCreatesOneRowAndOneJob` — eight concurrent identical deliveries produce one durable row and one queued job; the rest are acknowledged as duplicates.
- `user-server/internal/service/webhook_channel04_admission_test.go::TestWebhookReceive_DurableEventRecoversWhenQueueUnavailable` — confirms a durable event remains eligible for the recovery scanner and is processed at most once when the in-memory queue is unavailable.
- `user-server/internal/service/webhook_channel04_admission_test.go::TestWebhookReceive_ProcessRestartRecovery` — child process exits with code 86 immediately after the durable insert commits; a fresh child opens the same parent-owned isolated PostgreSQL database, replays the event once, and does not replay it again after marking it processed. The handler is a local deterministic test function; this does not prove external delivery.

## Local Verification

- TDD red evidence: before the production change, the focused regression run failed at the intended assertions: rate-limit rejection left a dedup key, canceled persistence left a key, and an orphaned reservation returned an accepted duplicate. The concurrent harness was corrected to stop workers before testing queue cardinality; the corrected test then verifies one queued job.
- Commands run: targeted FA-CHANNEL-04 tests, including abrupt process-exit/restart recovery and Receive/dedup compatibility tests → pass (`ok hivemtk-user/internal/service 7.319s`, latest rerun with dedicated PostgreSQL credentials).
- Historical run: `go test ./internal/service -count=1 -p 1 -timeout 30m` failed after 330.760s at `TestArchServiceNoNewGormDB` (knowledge callbacks 2 versus allowance 1). After explicit scope expansion, the separate `service-gorm-transaction-seam-20261120` fix moved atomic deletion into the repository and removed the now-zero allowance; concurrent main-checkout code was untouched.
- November 21 rerun: `go test ./internal/service -count=1 -p 1 -timeout 30m -json` → pass in 264.160s; 6225 passed test/subtest results, 0 failed, 5 existing environment/fixture skips. No command-line skip filter; the architecture guard and all six admission/recovery tests actually passed. Knowledge packages also passed (274 test/subtest passes, 0 failures, 6 real-inference skips); exact reasons and logs are in `../service-gorm-transaction-seam-20261120/test.md`.
- Commands run: `go build ./...` → pass; `go vet ./...` → pass; `python3 scripts/check-layer-boundary.py` → pass (1604 files, 0 violations); `gofmt -d ...` and `git diff --check` → clean.
- Commands run: `make lint` → pass (`0 issues`) after applying gofmt to the pre-existing field alignment at `user-server/internal/model/order_change_request.go:46`.
- Commands run: graph rebuild command and `/opt/homebrew/bin/graphify --help` → unavailable because the configured Python `graphify` module is absent from the interpreter's import path (`ModuleNotFoundError: No module named 'graphify'`). No environment or dependency installation was attempted.
- Process lifecycle check: parent observed child exit code 86 and verified the durable row remained unprocessed; a new child process replayed the row once and a second scan returned zero. The child reused the existing isolated database directly and did not call test DB setup/reset helpers.

## Deviations from Assessment

- Added fail-closed handling for a disconnected event repository after verifying that the old code could report acceptance without durable storage. This is required to preserve the assessment's “no false acceptance” condition.

## Follow-ups

- The restart regression verifies durable insert and scanner replay across actual process boundaries using a deterministic local handler. Local code verification and the complete service package gate now pass. The overall item remains partial pending real-channel/rollout acceptance: this test does not prove real platform delivery, external exactly-once semantics or a full production service restart. No commit, push or deployment was performed.
