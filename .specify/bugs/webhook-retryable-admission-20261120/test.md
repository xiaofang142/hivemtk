# Bug Verification: Webhook admission can acknowledge an event that was never saved

- **Slug**: webhook-retryable-admission-20261120
- **Tested**: 2026-11-21
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

The focused regressions confirm rate-limit and persistence failures remain retryable, an orphaned Redis reservation cannot impersonate a durable event, concurrent duplicate deliveries create one durable row and one queued job, and a durable event survives an abrupt process exit after commit and is replayed once by a fresh process. After the separately authorized local knowledge transaction-boundary fix, the complete service package passes in 264.160s, including the architecture guard and all six admission/recovery regressions. Local code verification passes; the overall item remains partial because real-channel/rollout acceptance has not been performed.

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Regression reproduction (pre-fix) | Focused FA-CHANNEL-04 tests | fail as expected | The original implementation left Redis keys after rate-limit rejection and canceled persistence, and treated an orphaned key as an accepted duplicate. |
| Reproduction (post-fix) | Focused `TestWebhookReceive_...` cases for rate-limit retry, canceled persistence retry, stale reservation, concurrent delivery, and recovery with queue unavailable | pass | Uses the dedicated local PostgreSQL test instance; one durable row per event, scanner processes the aged event once. |
| Process-exit/restart recovery | `go test ./internal/service -run '^TestWebhookReceive_ProcessRestartRecovery$' -count=1 -timeout 2m` | pass | A child exits with status 86 immediately after the durable insert commit; the parent verifies the unprocessed row, then a second child scans the same database, processes it once, and does not replay it again. Child processes directly reuse the parent's isolated database and never reset it. |
| Focused admission/recovery and compatibility suite | `go test ./internal/service -run '^TestWebhookReceive_(RateLimitDoesNotReserveDedupKey|PersistFailureCanRetry|StaleReservationDoesNotBlockDurableAdmission|ConcurrentReplayCreatesOneRowAndOneJob|DurableEventRecoversWhenQueueUnavailable|ProcessRestartRecovery)$|^TestWebhookService_(Receive_(Duplicate|RateLimit)|Dedup_)|^TestReceive_Telegram_OfficialUpdateIDDedupsReencodedReplay$|^TestWebhook_DuplicateEvent$' -count=1 -timeout 5m` | pass | Latest rerun with the required dedicated PostgreSQL test credentials: `ok hivemtk-user/internal/service 7.319s`. |
| New / updated tests | `go test ./internal/service -run 'TestWebhookReceive_(RateLimitDoesNotReserveDedupKey|PersistFailureCanRetry|StaleReservationDoesNotBlockDurableAdmission|ConcurrentReplayCreatesOneRowAndOneJob|DurableEventRecoversWhenQueueUnavailable|ProcessRestartRecovery)|TestWebhookService_Receive_(Duplicate|RateLimit)|TestWebhookService_Dedup_|TestReceive_Telegram_OfficialUpdateIDDedupsReencodedReplay|TestWebhook_DuplicateEvent' -count=1 -timeout 5m` | pass | Latest run: `ok hivemtk-user/internal/service 7.319s`. |
| Regression suite | `go test ./internal/service -count=1 -p 1 -timeout 30m -json` | pass | November 21 complete run: exit 0, 264.160s, 6225 passed test/subtest results, 0 failed, 5 existing skips. No command-line exclusion; architecture and all six admission/recovery cases passed. The historical 330.760s architecture failure was resolved by the separately authorized repository-boundary fix; see `../service-gorm-transaction-seam-20261120/test.md`. |
| Knowledge regression after boundary fix | `go test ./internal/aiagent/knowledge/... -count=1 -p 1 -timeout 15m -json` | pass | 274 passed test/subtest results, 0 failed, 6 real-inference skips. New transaction/rollback/context/tenant tests actually ran. |
| Build / vet / boundary | `go build ./...`; `go vet ./...`; `python3 scripts/check-layer-boundary.py` | pass | Layer checker: 1604 production Go files, 0 violations. |
| Formatting / diff | `gofmt -d ...`; `git diff --check` | pass | No formatting or whitespace diff. |
| Lint | `make lint` | pass | `0 issues` after applying gofmt to the pre-existing field alignment at `user-server/internal/model/order_change_request.go:46`. |
| Graph refresh | Required Python graph rebuild command; prior `/opt/homebrew/bin/graphify --help` check | blocked | November 21 rebuild still fails with `ModuleNotFoundError: No module named 'graphify'`; no dependency or environment installation was attempted. |

## Output Excerpts

- Pre-fix: failed expected assertions for rate-limit reservation, persistence-failure reservation, and stale-reservation duplicate acknowledgement.
- `ok hivemtk-user/internal/service 7.319s` (latest focused admission/recovery and compatibility run)
- Historical run: `FAIL hivemtk-user/internal/service 330.760s` due to the knowledge service exposing two transaction callbacks against allowance 1.
- November 21 complete run: root service `Action=pass`, `Elapsed=264.16`, `exit_code=0`; the architecture guard and all six FA-CHANNEL-04 admission/recovery cases passed.
- Knowledge transaction callbacks now zero; its cleared allowance was removed as required by the bidirectional ratchet, with all scanner logic and remaining limits preserved.
- Complete regression logs and skip details: `/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/langbot-gorm-20261121-n147sjeo/{knowledge.jsonl,root-service.jsonl,summary.json}`.
- `make lint`: `0 issues` after the formatting correction.
- `python3 scripts/check-layer-boundary.py`: 1604 production files scanned, 0 violations.

## Residual Risks

- Process restart was injected after the database commit and before `Receive` could return or queue the event. Recovery used a deterministic local handler, not a real channel adapter; external delivery and platform-level exactly-once behavior remain unverified.
- Five existing root-service skips cover two local-API integration cases, ingress wiring, real-browser login and a runtime-database website E2E case; six knowledge skips require the unavailable real inference stack. Full names and reasons are recorded in `../service-gorm-transaction-seam-20261120/test.md`. None of the six FA-CHANNEL-04 admission/recovery tests or architecture tests were skipped.
- Graph refresh could not run because the graphify module is unavailable in this worktree environment.

## Recommendation

The local admission/recovery fix and full-package gate are verified. Keep FA-CHANNEL-04 unchecked pending its required real-channel/rollout acceptance. Process-boundary durability and scanner replay are covered locally; the authorized knowledge-boundary fix removed the former architecture blocker in this isolated worktree without changing concurrent main-checkout source or increasing any allowance.
