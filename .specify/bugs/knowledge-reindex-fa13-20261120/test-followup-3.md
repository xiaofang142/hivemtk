# FA-13 quality follow-up verification 3

- Date: 2026-11-20
- Status: **DONE_WITH_CONCERNS**; parent independent verification and original quality reviewer approval pending. **FA-13 remains open.**
- Assessment/fix: `assessment-followup-3.md`, `fix-followup-3.md`.
- All evidence paths below are relative to `evidence/followup-3/`. Each actual invocation has a `*-command.txt`, `.log` and `.exit` record. `rerun-commands.txt` collects reproducible commands; `handoff-verification.json` validates results and source hashes.

## Behavioral checks

| Actual check | Result / exit | Evidence |
|---|---|---|
| Original behavioral RED, before production edits | Expected exit 1. Both metadata tests failed; all 16 progress error/cancellation cases skipped cleanup and could not immediately retry; all 8 persistent-error/replacement-owner cases lacked the cleanup attempt. The real paused worker lost its claim after stale metadata save. | `red-command.txt`, `red.log`, `red.exit`, `source-hashes-red.json`, `red-validation-before-fix.json` |
| Metadata focused GREEN | Both tests pass, exit 0: real PostgreSQL interleaving preserves owner/lease/count/status/progress/error/stats and allows original worker completion; whitelist, editable zero values, protected fields, tenant scope and no resurrection verified. | `metadata-green-command.txt`, `metadata-green.log`, `metadata-green.exit` |
| Initial progress focused GREEN | Two top-level tests, 24 cases pass, exit 0. Subsequent race run exposed a cancellation-fixture timing issue described below. | `progress-green-command.txt`, `progress-green.log`, `progress-green.exit` |
| Revised cancellation fixture against captured pre-fix production | Expected exit 1 under race; both metadata tests and all 16+8 matrix cases still fail for the intended behaviors. Go overlay uses captured pre-edit production only; working production files remain untouched. | `red-recheck-command.txt`, `red-recheck.log`, `red-recheck.exit`, `red-recheck-overlay.json`, `red-recheck-validation.json` |
| Final service regressions with race | **14 top-level tests pass**, exit 0, including the original ten and four new tests. New matrices contain 16 error/cancellation cases and 8 persistent-error/owner-replacement cases; no race report or skipped test. | `service-race-2-command.txt`, `service-race-2.log`, `service-race-2.exit` |
| Controller regressions with race | **3 tests pass**, exit 0: failure response, empty-product completion, and nonempty controlled embedding proving no early success and persisted indexed/1 before 200. No race report or skipped test. | `controller-race-2-command.txt`, `controller-race-2.log`, `controller-race-2.exit` |
| Existing retry-count, lease, owner migrations | **3 tests pass**, exit 0. No new application migration in this round. | `migrations-command.txt`, `migrations.log`, `migrations.exit` |

Each progress 10/30/60/80 is tested for a one-shot write error and cancellation before update through both public Reindex and acquiring pipeline entry. The cleanup callback checks a non-canceled context with a positive deadline no longer than five seconds and tenant 413. Stored failed/error/cleared-lease state and retry count are read from PostgreSQL. Reindex attempts remain 1 then 2; acquiring async attempts remain 0 then explicit Reindex 1. Each test removes the injection and immediately retries to indexed, without expiring the lease manually. Separate lost-owner tests expire the old lease solely to perform a real replacement claim, compare the entire newer row before/after cleanup, and let that owner finish.

The previous ten service tests cover snapshot membership, expired reclaim, embedding cancellation/retry for stored and zero chunks, shared async/Reindex claims, timeout-sized leases, resumed stale owners, the 1,001-document rebuild, cursor/atomic claim, embedding failure/retry, and terminal-status fallback/failure propagation. All ten ran with race in the final command, not just the six from follow-up 2.

## Honest fixture and TDD history

1. The original `red.log` was produced before either production edit. `red-validation-before-fix.json` confirms production and test source hashes then matched the pre-edit record. Original RED test source is retained as `knowledge_reindex_quality_test.go.original-red.txt`.
2. The first controller race run (`controller-race.log`, exit 1) failed during fixture DDL: the tenant callback rejected `ALTER TABLE knowledge_chunks`. Only the fixture's dedicated schema DDL was changed to explicit system scope. `controller-race-2.log` is the valid passing run. The setup failure is not behavioral RED.
3. The first full service race run (`service-race.log`, exit 1) exposed nondeterministic fixture timing: canceling in a callback before `gorm:update` occurred after GORM began a transaction. `database/sql` could auto-rollback, and GORM's `%v; %w` error append left `context canceled` only as text when rollback returned `sql.ErrTxDone`, breaking `errors.Is(context.Canceled)`. Failure cleanup itself persisted successfully in that run. The test now cancels before `gorm:begin_transaction` and does not inject `context.Canceled` via AddError: actual GORM BeginTx sees the canceled context and returns the cancellation. Error identity assertions were retained. Production code was not expanded to alter GORM's error handling.
4. This test-only timing correction changes the original RED test hash. The final source was rechecked under race using Go overlay against captured pre-fix production; `red-recheck-validation.json` records the intended failures, hashes, and unchanged working sources. This is a supplemental post-fix negative control, not a replacement historical pre-edit RED. Final GREEN uses the identical final test source without overlay.
5. Original FA-13 implementation still has no historical RED. Follow-up 1's later-added assertions still have only its recorded GREEN coverage. This round does not retroactively supply those missing histories. The optional controller test verifies existing behavior and has no behavioral controller RED claim.

## Build and static checks

| Command (actual working directory is recorded alongside each log) | Exit / result | Evidence stem |
|---|---|---|
| `go build ./...` | 0 | `build` |
| `go vet ./...` | 0 | `vet` |
| `python3 scripts/check-layer-boundary.py` | 0; zero violations | `layer` |
| `bash scripts/check-architecture.sh` | 0; existing warning in `internal/service/webhook_wecomcs_puller.go` about a service interface | `architecture` |
| `bash user-server/scripts/check-gorm-only.sh` | 0 | `gorm` |
| `git diff --check` | 0 | `diff-check` |
| `bash user-server/scripts/check_naked_goroutine.sh` | **1**, existing repository-wide production call sites; this correction adds no production goroutine | `naked-goroutine` |
| `python3 -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"` | **1**, `ModuleNotFoundError: No module named 'graphify'`; no refreshed graph claimed | `graphify` |

`caller-inventory.log` preserves the concrete repository consumers, Update call sites and controller field assignments. `workspace-before.json` and `workspace-preservation-check.json` compare pre-existing files without reverting any inflight work. The two `.followup-3.diff` files show only this round's production edits relative to the captured pre-edit files, including earlier uncommitted FA-13 work. Final source hashes and exact selected test counts are in `handoff-verification.json`.

## Limits and independent review boundary

- Only the authorized `FA12_PG_DSN` socket/port/user/database was used; fixtures reject other targets. No `testutil.NewTestDB`, business DB, cluster stop/deletion, or schema cleanup beyond fixture-owned schemas. Vectors use a fixture-local text domain; real pgvector operations/performance are untested.
- New cancellation cases cover cancellation before the progress update transaction begins. They do not prove cancellation behavior during SQL, GORM rollback error identity, a driver/network outage, or an actually hung cleanup hitting its five-second deadline. The bounded cleanup context is observed; persistent failures are injected at GORM callbacks.
- The acquiring pipeline tests call `processDocument` directly to observe returned errors. They do not exercise the logging-only detached goroutine wrapper itself. The owner-replacement matrix drives the newer owner by repository claim/terminal writes, not a second complete Reindex worker.
- Follow-up-2 limits remain: contextual enhancement initially reads without supplied tenant context and does not reach its write path in those fixtures; optional external-index publication has a pre-call owner check rather than atomic cross-system fencing; post-terminal cache/product aggregates remain outside the owner transaction. No new guarantee is claimed for those paths.
- No full affected-package suite was run in this round; unrelated fixtures require separately authorized DB setup. No live API/browser, production deployment, real external provider, OS process-kill recovery, exactly-once external embedding guarantee, or external-indexer race test.
- No new agent, staging, commit, push, deployment or outer-queue edit. Parent independent verification must precede resubmission to the same quality reviewer `ses_e070e56c0ffeWDtZIiR9GCXkUB`. Neither independent verification nor approval is claimed here; FA-13 stays open.

## Parent acceptance after implementation handoff

The preceding pending status records the implementer's handoff, before independent review. The parent subsequently reran the final selected tests against the same authorized synthetic PostgreSQL cluster, with `-race`, `-count=1`, and the test selections recorded in `rerun-commands.txt`: service 14 top-level tests passed (`18.612s`), controller 3 passed (`2.212s`), and migration 3 passed (`2.064s`). The migration rerun also used `-race`. `go build ./...`, `go vet ./...`, layer boundary (1,604 production Go files, zero violations), architecture, GORM-only, and `git diff --check` all exited 0. The architecture check retained its existing `wecomCSService` warning.

Specification reviewer `ses_e080949d3ffe5AAEH7GJyY2kLm` approved the shared ownership/snapshot correction. Quality reviewer `ses_e070e56c0ffeWDtZIiR9GCXkUB` subsequently returned **QUALITY_APPROVED** for the current correction, with no remaining Critical, Important, or Minor findings within FA-13. That reviewer checked 19 current source hashes and the metadata, progress-failure, and nonempty-controller test paths; it did not connect to PostgreSQL or rerun the database tests.

FA-13 is accepted within its synthetic verification scope. At final parent inspection the concurrently edited outer `docs/未完成任务清单.md` already contained zero FA-13 task rows or references; the parent did not overwrite that document to repeat the removal. Other tasks and in-flight changes remain outside this acceptance. No next task was started, and no staging, commit, push, production migration, or deployment was performed. The limits above and the historical test-first evidence gaps remain unchanged.
