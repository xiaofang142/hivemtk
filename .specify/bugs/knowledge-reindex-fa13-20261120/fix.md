# Bug Fix: Product reindex now covers every document and reports failure

- **Slug**: knowledge-reindex-fa13-20261120
- **Fixed**: 2026-11-20
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

Product rebuild now traverses a fixed document-ID watermark using ascending keyset pages, atomically claims each eligible document, increments its persisted retry count, and returns an aggregate error if any item fails. Per-document processing is synchronous for rebuilds and persists queryable failure status where storage is available; controller responses distinguish completion from failure.

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex.go` | modified | Fixed-watermark cursor traversal, synchronous processing, error aggregation, and failed-state propagation. |
| `user-server/internal/aiagent/knowledge/service/knowledge_service_pipeline.go` | modified | Return pipeline failures and propagate failed-status write errors. |
| `user-server/internal/aiagent/knowledge/repository/knowledge_document.go` | modified | Add tenant-context keyset queries and atomic claim/count update. |
| `user-server/internal/model/kb_workspace.go` | modified | Add durable `reindex_retry_count` model field. |
| `user-server/internal/aiagent/knowledge/service/knowledge.go` | modified | Expose the attempt count in document progress. |
| `user-server/internal/aiagent/knowledge/controller/knowledge_workspace.go` | modified | Use completion wording and safe HTTP error responses for reindex outcomes. |
| `user-server/internal/migration/migrations/initial_schema.go` | modified | Register the additive migration. |
| `user-server/internal/migration/migrations/knowledge_reindex_attempt_migration.go` | added | Add the counter idempotently and preserve history on downgrade. |
| `user-server/internal/migration/migrations/knowledge_reindex_attempt_migration_test.go` | added | Verify migration upgrade, idempotence, and non-destructive downgrade. |
| `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex_test.go` | added | Exercise 1,001 documents, missing/empty source files, retries, fixed watermark, concurrent claims, embedding failure, and final-state write fallback/propagation. Uses an isolated synthetic PostgreSQL schema; a text-backed test-only `vector` domain avoids requiring pgvector and does not test pgvector operators. |
| `user-server/internal/aiagent/knowledge/controller/knowledge_workspace_reindex_test.go` | added | Verify product-rebuild HTTP failure and completion responses against an isolated synthetic schema. |

## Tests Added or Updated

- `TestRebuildIndex_VisitsAllDocumentsAndPersistsMissingSourceFailure` — all 1,001 rows receive a queryable failed result and one attempt; covers a missing and an empty source.
- `TestReindexCursorWatermarkAndAtomicClaim` — documents above the captured high-water mark are excluded and only one concurrent worker can claim the same row.
- `TestReindex_EmbeddingFailureIsQueryableAndRetryable` — provider failure is returned and stored with attempt count 1.
- `TestReindex_FinalStatusWriteFailurePersistsOrPropagates` — a final indexed-status write failure falls back to a queryable failed state; if that write also fails, both errors propagate.
- `TestRebuildProductIndex_ReturnsFailureWhenDocumentReindexFails` / `TestRebuildProductIndex_ReturnsSuccessAfterCompletion` — controller reports an error for failed work and success only after completion.
- `TestKnowledgeReindexAttemptMigration` — migration upgrade, idempotence, and preserved retry history.

## Local Verification

- Focused synthetic PostgreSQL regression tests passed:
  - service: `go test ./internal/aiagent/knowledge/service -run 'TestRebuildIndex_VisitsAllDocumentsAndPersistsMissingSourceFailure|TestReindexCursorWatermarkAndAtomicClaim|TestReindex_EmbeddingFailureIsQueryableAndRetryable|TestReindex_FinalStatusWriteFailurePersistsOrPropagates' -count=1`
  - controller: `go test ./internal/aiagent/knowledge/controller -run '^TestRebuildProductIndex_Returns(FailureWhenDocumentReindexFails|SuccessAfterCompletion)$' -count=1`
  - migration: `go test ./internal/migration/migrations -run '^TestKnowledgeReindexAttemptMigration$' -count=1`
- `go build ./...` and `go vet ./...` passed.
- `python3 scripts/check-layer-boundary.py` passed with 0 violations; `bash scripts/check-gorm-only.sh` passed; `bash scripts/check-architecture.sh` passed with one existing service-interface warning in `internal/service/webhook_wecomcs_puller.go`.
- `bash user-server/scripts/check_naked_goroutine.sh` remains red on repository-wide pre-existing call sites; none were introduced by this change.
- Full `go test` for the affected package directories is blocked by unrelated test fixtures that require `POSTGRES_TEST_PASSWORD`; the tests using the explicitly authorized synthetic PostgreSQL DSN pass. No database connection was attempted by those blocked tests.
- Graphify refresh was attempted but could not run because the configured Python interpreter cannot import `graphify` (`ModuleNotFoundError`).
- No staging, commit, push, or deployment was performed.

## Deviations from Assessment

- None. The synthetic successful-vector path uses a text-backed test-only `vector` domain, so it validates service status handling without claiming pgvector extension compatibility or vector operator behavior.

## Follow-ups

- None within this scoped fix. Production deployment and any pgvector-specific verification remain outside this request.
