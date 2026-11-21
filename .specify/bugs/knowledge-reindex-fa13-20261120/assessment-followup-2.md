# FA-13 follow-up assessment 2

- Date: 2026-11-20
- Scope: ordinary async indexing and Reindex ownership collision, configured async timeout lease lifetime, and stale-owner fencing.
- Status: DONE_WITH_CONCERNS; implementation and focused verification recorded in `fix-followup-2.md` and `test-followup-2.md`. Await parent independent rerun and the same spec reviewer's re-review.
- Constraints: retain existing uncommitted workspace changes; no staging, commit, push, deployment, business DB, queue edits, or subagents.

## Findings

1. `Import` schedules `processDocumentAsync`, but that worker calls `processDocument` which unconditionally writes `processing`; it does not use `ClaimForReindex`. A Reindex that claims first can therefore have its status/lease overwritten and both workers can reach the embedding provider.
2. `async_processing_timeout` is configured independently (up to 7200 seconds), while the repository currently grants a fixed 15-minute lease. `UpdateStatus(processing)` also resets it to that fixed duration on progress writes. A live async worker can therefore become reclaimable before its execution context expires.
3. Claims are not represented by an owner identity. Status, chunk-stat, and embedding writes are keyed only by document/chunk IDs, so a worker that resumes after lease expiry can overwrite work done by a newer claimant.

## Corrected contract

- Async pipeline and Reindex acquire the same atomic, product-and-tenant-scoped claim before any processing side effect. A worker that encounters a fresh owner performs no work; Reindex continues to report the busy condition.
- Every claim gets a distinct persisted owner token. The lease covers the configured worker timeout plus a bounded safety margin; progress writes preserve (rather than shorten or reset) that lease.
- Owner-sensitive progress, chunk/embedding persistence, statistics, success, and failure writes are conditional on the current owner. A stale owner cannot mutate a document after another claim succeeds.
- Keep Reindex's execution timeout bounded. Preserve compatibility for direct legacy `processDocument` callers by making it an acquiring wrapper and route already-claimed Reindex processing through an owner-aware internal path.
- No schema addition unless owner identity cannot be safely represented by existing columns; if required, add one additive migration and test it in the authorized synthetic PostgreSQL fixture.

## Test-first sequence

1. Add a controlled embedding barrier where Reindex owns a document before the async worker starts; prove only one embedding request occurs.
2. Configure an async timeout longer than the old lease and assert a blocked live worker retains a lease beyond that timeout.
3. Let one owner block in embedding, expire its lease, let a second owner finish, then release the first and assert the first cannot replace the newer vector or terminal state.
4. Run the three tests against unchanged production code, capture exact expected failures, exit status, and test-source hashes under `evidence/followup-2/` before modifying production.
5. Implement minimal shared ownership/fencing, rerun the focused tests and relevant existing service/controller/migration/race checks, then record the actual result and limits. Same spec reviewer re-review remains required before quality review or any queue update.

## Files in scope

- `user-server/internal/aiagent/knowledge/repository/knowledge_document.go`
- `user-server/internal/aiagent/knowledge/repository/knowledge_chunk.go` and parent repository only if required for atomic owner checks
- `user-server/internal/aiagent/knowledge/service/knowledge_service_pipeline.go`
- `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex.go`
- `user-server/internal/model/kb_workspace.go` and additive migration/registration only if required
- FA-13 `assessment-followup-2.md`, `fix-followup-2.md`, `test-followup-2.md`, and separate `evidence/followup-2/`

Other dirty/untracked files are pre-existing user work and remain untouched.
