# FA-13 follow-up verification 2

- **Date:** 2026-11-20
- **Status:** DONE_WITH_CONCERNS; parent independent rerun and original spec reviewer re-review pending.
- **Fix report:** `fix-followup-2.md`.
- **Evidence directory:** `evidence/followup-2/`; all paths below are relative to it.

## Behavioral evidence

| Check | Actual result | Evidence |
|---|---|---|
| Three core regressions before production changes | Expected failures, exit 1: duplicate async worker overwrote active status; two-hour configuration received about fifteen minutes of lease; resumed old owner replaced the newer vector and returned no fencing error. | `red-final-2-command.txt`, `red-final-2.log`, `red-final-2.exit`, `source-hashes-red-final-2.txt` |
| Focused GREEN | Four top-level tests passed, exit 0: three core regressions plus concurrent expired-lease reclaim. | `green-focused-2.log`, `green-focused-2.exit` |
| Final service regressions | Ten top-level tests passed, exit 0, including snapshot membership, expired reclaim, cancellation/retry (stored and zero chunks), three core regressions, 1,001 documents, cursor/atomic claim, embedding failure/retry, and terminal-write fallback/failure propagation. | `service-final.log`, `service-final.exit` |
| Controller regressions | Two tests passed, exit 0: rebuild failure response and success after completion. | `controller-regressions.log`, `controller-regressions.exit` |
| Race detector | Six top-level service tests passed, exit 0; snapshot membership, expired reclaim, cancellation/retry, and the three core regressions. No race report. | `service-race.log`, `service-race.exit` |
| Owner migration RED | Expected missing v3.101.0 registration failure, exit 1, before the owner model/migration addition. | `owner-migration-red-2.log`, `owner-migration-red-2.exit` |
| Migration regressions | Retry-count, lease, and owner migrations passed, exit 0. Owner coverage includes registration, first upgrade, repeated upgrade, token insertion, and column retention on Down. | `migration-regressions.log`, `migration-regressions.exit` |

The core regression test file and the existing Reindex test file retain exactly the hashes recorded in `source-hashes-red-final-2.txt`. Earlier failed fixture attempts remain preserved and are not substituted for the valid RED. `owner-migration-red.log` failed during fixture setup because the absent token column was dropped unconditionally; `owner-migration-red-2.log` is the corrected valid migration RED.

The first service regression run exposed a terminal-status fallback regression in this implementation. It was corrected using owner-conditional failure persistence; `service-regressions-2.log` and `service-final.log` cover both successful failed-state fallback and propagation when both terminal writes fail. The initial `green-focused-1.log` also contains a fake-PDF test blocked by its missing-password safety gate; it is not an all-green run.

## Build and static gates

| Gate | Result | Evidence |
|---|---|---|
| `go build ./...` and `go vet ./...` | Both exit 0; recaptured with complete command/output/exit records during handoff. | `build-vet-final.log`, `build-vet-final.exit` |
| Layer boundary | Exit 0; zero violations. | `layer.log`, `layer.exit` |
| Architecture | Exit 0; existing small-service-interface warning in `internal/service/webhook_wecomcs_puller.go`. | `architecture.log`, `architecture.exit` |
| GORM-only | Exit 0. | `gorm.log`, `gorm.exit` |
| Whitespace | Exit 0. | `diff-check.log`, `diff-check.exit`; handoff check in `handoff-verification.json` |
| Naked goroutines | Exit 1 on existing repository-wide call sites. No new production goroutine in this correction. | `naked-goroutine.log`, `naked-goroutine.exit` |
| Graphify refresh | Exit 1: `ModuleNotFoundError: No module named 'graphify'`. No current graph is claimed. | `graphify.log`, `graphify.exit` |

`handoff-verification.json` records evidence checks, final source SHA-256 values, regression-source comparison, and final whitespace/format checks. `rerun-commands.txt` provides focused reproduction commands using only the authorized synthetic database.

## Limits and review boundary

- Contextual enhancement does not reach its write path in the recorded synthetic runs: initial reads lack the supplied tenant context and fail closed. Its owner fencing has no behavioral GREEN evidence here.
- Optional external indexer publication has a pre-call ownership check, not an atomic cross-system fence. Post-success cache invalidation and product aggregates are outside the document-owner transaction. See `fix-followup-2.md` for review concerns.
- The stale-owner regression uses a public Reindex for the old worker and repository-controlled reclaim/new-vector/terminal writes for the newer owner; it does not run two complete Reindex pipelines.
- No full affected-package suite, live API, production database, deployment, real pgvector operator test, or process-kill test was run. No production rollout is implied.
- This report is implementation verification, not spec/quality approval. Await the parent's independent rerun and the same spec reviewer's re-review before quality review or queue updates.
- No staging, commit, push, deployment, business database, outer-queue update, or subagents.
