# FA-13 follow-up verification 1

- **Date:** 2026-11-20
- **Fix report:** `./fix-followup-1.md`
- **Status:** implementation checks recorded; spec review pending.
- **Scope:** new snapshot-membership and lease/cancellation corrections only. The existing 1,001-document and controller tests were not rerun, as requested.

## Behavioral evidence

| Check | Result | Evidence |
|---|---|---|
| Follow-up RED on unchanged production code | Expected failures; exit 1 | `evidence/followup-1/red-final-command.txt`, `red-final.log`, `red-final.exit`, and `source-hashes-red-final.txt`. Observed the lower sequence ID being included after a later commit, no lease on initial claim / no expired reclaim, and cancellation leaving `processing` because failure writes used the canceled context. The first incomplete fixture run timed out and is not counted as valid RED. |
| Migration registration negative probe | Expected missing-registration failure; exit 1 | `migration-red-overlay-command.txt`, `migration-red-overlay.log`, `migration-red-overlay.exit`. Go overlay removed only the new registration in a temporary file outside the repository. |
| Follow-up focused GREEN | Pass; exit 0 | `final-focused.log`, `final-focused.exit`. Included snapshot interleaving, fresh and expired claims, concurrent exactly-once reclaim, retries, canceled-context cleanup and retry through both stored-chunk and zero-chunk paths, plus existing focused embedding/final-status tests and the lease migration. |
| Focused race run | Pass; exit 0 | `race.log`, `race.exit`; service regressions and lease migration. |
| Lease migration | Pass; exit 0 | Also covered in `final-focused.log` and `race.log`. |

The RED record predates some later additions to the regression test (including explicit tenant/product-isolation and public-entry assertions). Those later assertions are covered by GREEN but are not represented as individually red-tested. The original FA-13 work still has no historical RED evidence.

## Final gates

| Gate | Result | Evidence / notes |
|---|---|---|
| `go build ./... && go vet ./...` | Pass | `build-vet.log`, `build-vet.exit`. |
| `python3 scripts/check-layer-boundary.py` | Pass | `layer.log`, `layer.exit`; 0 violations. |
| `bash scripts/check-architecture.sh` | Pass with existing warning | `architecture.log`, `architecture.exit`; existing warning in `internal/service/webhook_wecomcs_puller.go`, no new violation. |
| `bash user-server/scripts/check-gorm-only.sh` | Pass | `gorm.log`, `gorm.exit`. |
| `git diff --check` | Pass | `diff-check.log`, `diff-check.exit`. |
| `bash user-server/scripts/check_naked_goroutine.sh` | Fail on repository-wide existing call sites | `naked-goroutine.log`, `naked-goroutine.exit`; no new naked goroutine was added in production code. |
| Graphify refresh | Not available | `evidence/followup-1/graphify.log`, `graphify.exit`; `graphify` Python module is missing (`ModuleNotFoundError`), so no graph output was asserted current. |

## Not run / limits

- No full affected-package suite, original 1,001-document test, controller test, or pre-existing migration test was rerun.
- No production database, live API, deployment, pgvector extension/operator check, OS process-kill test, or autonomous recovery scanner was used or claimed.
- The report does not authorize or perform spec/quality review. The same spec reviewer must review first; quality review remains contingent on spec approval.
- No staging, commit, push, or deployment was performed.
