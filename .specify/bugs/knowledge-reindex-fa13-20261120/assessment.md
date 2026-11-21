# Bug Assessment: Rebuild all knowledge indexes can silently skip and hide failures

- **Slug**: knowledge-reindex-fa13-20261120
- **Created**: 2026-11-20
- **Source**: caller-supplied task brief (FA-13)
- **Verdict**: valid
- **Severity**: medium

## Report (verbatim or summarized)

FA-13: “Rebuild all indexes” must visit all documents and return real failures. The current service reads only the first 1,000 documents, ignores the reported total, and can return nil when a zero-chunk document has an unreadable source or when final status persistence fails. A successful HTTP response must not be mistaken for completed indexing when work failed.

## Symptom

For a product with more than 1,000 documents, the current product rebuild omits later documents. Several per-document failure paths log or attempt to write a failed state but still return nil, so the controller can report success while the document remains unindexed or its state is stale.

## Reproduction

1. Create at least 1,001 documents for one existing RAG product and invoke `POST /api/knowledge/products/:product_id/rebuild-index`.
2. Observe that `RebuildIndex` obtains only one 1,000-row page and has no continuation.
3. Exercise a zero-chunk document with a missing/empty source file, an embedding failure, or a final document-state write failure. Current paths can return nil despite the unsuccessful outcome.

The source-level paths are verified. A full database/API reproduction has not yet been run.

## Suspected Code Paths

- `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex.go` — `Reindex` hides unreadable-source and final-state-write errors; zero-chunk recovery is asynchronous; `RebuildIndex` asks `List` for one 1,000-row page and loops only those results.
- `user-server/internal/aiagent/knowledge/repository/knowledge_document.go` — current `List` paginates by offset and orders only by `created_at DESC`; it has no fixed-watermark/keyset operation or atomic reindex claim.
- `user-server/internal/aiagent/knowledge/service/knowledge_service_pipeline.go` — async processing marks errors through a void path and `markFailed` suppresses failed-state write errors, preventing a synchronous rebuild from aggregating truthful outcomes.
- `user-server/internal/model/kb_workspace.go` — `KnowledgeDocument` already exposes `embed_status`, `embed_progress`, and `error_msg`, but has no durable reindex retry/attempt count.
- `user-server/internal/aiagent/knowledge/service/knowledge.go` — `GetProgress` exposes existing queryable document state and is the natural place to expose an additive reindex attempt count.
- `user-server/internal/aiagent/knowledge/controller/knowledge_workspace.go` — product rebuild returns HTTP 200 on nil and says “task started”; it currently receives a mixed sync/async service result rather than an aggregate durable job result.
- `user-server/internal/migration/migrations/initial_schema.go` and a new migration implementation/test — register and verify the additive attempt-count column for existing installations.

## Root Cause Hypothesis

**Confidence: high.** `RebuildIndex` performs exactly one `List(...PageSize: 1000)` call and never uses `total`. `Reindex` returns nil after the unreadable-source branch even when `UpdateStatus` fails, and logs rather than returns a final `Update` error. For documents without chunks it starts a detached async operation and returns before the processing outcome is known. The pipeline has no return channel for asynchronous failure and logs state-persistence errors. No persistent retry counter exists on `KnowledgeDocument`.

## Proposed Remediation

**Preferred**: Make product rebuild a truthful synchronous operation, consistent with `RebuildIndex` already awaiting the existing-chunk embedding path: take a fixed maximum document ID for the product, then walk ascending primary-key cursor pages bounded by that watermark. This avoids unstable offset pagination and gives an explicit concurrency contract: database-generated document IDs committed after the initial watermark are excluded from this run and require a subsequent rebuild; documents deleted before their cursor page no longer require work. Keep each selected document in exactly one cursor page. Continue through all selected documents while collecting errors, and return a non-nil aggregate error if any document failed or could not be claimed. Do not report successful completion before all eligible rows have outcomes.

Make per-document reindex claim atomic (transition to processing and increment a persisted `reindex_retry_count` only if not already processing) so two workers do not concurrently embed the same document. Reindex existing chunks synchronously; run the zero-chunk source/pipeline path synchronously with an error result. Persist `failed` plus `error_msg` for missing/empty sources, embedding/validation/persistence failures, and final-state-write failures where storage remains available; propagate any failed state write rather than suppressing it. Existing `GET /documents/:id/progress` exposes the queryable status/error and should add the counter. The existing per-document reindex endpoint is the explicit retry entry; a retry claims a failed row and increments its attempt count. Product rebuild should return an error (controller's existing HTTP error path) if any row fails, and its success message should describe completion, not task launch. No separate task table or new status enum is indicated by the current API contract.

If repository or request evidence shows that the product endpoint must remain asynchronous, stop and revise this assessment before implementing: asynchronous acceptance needs a durable product-level task record and a query route/status contract, not a fire-and-forget goroutine.

**Files likely to change**:
- `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex.go`
- `user-server/internal/aiagent/knowledge/service/knowledge_service_pipeline.go`
- `user-server/internal/aiagent/knowledge/service/knowledge.go`
- `user-server/internal/aiagent/knowledge/repository/knowledge_document.go`
- `user-server/internal/model/kb_workspace.go`
- `user-server/internal/aiagent/knowledge/controller/knowledge_workspace.go`
- `user-server/internal/migration/migrations/initial_schema.go`
- `user-server/internal/migration/migrations/knowledge_reindex_attempt_migration.go` (new; version must follow the current registered maximum)
- `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex_test.go` (new)
- `user-server/internal/aiagent/knowledge/repository/knowledge_document_reindex_test.go` (new)
- `user-server/internal/migration/migrations/knowledge_reindex_attempt_migration_test.go` (new)
- `user-server/internal/aiagent/knowledge/controller/knowledge_workspace_reindex_test.go` (new, if controller contract needs direct regression coverage)

**Tests to add or update**:
- Run a synthetic PostgreSQL fixture containing at least 1,001 product documents through product rebuild; assert each pre-watermark document is claimed exactly once and the result is an error when deliberately failed docs exist.
- Assert a document inserted after the fixed watermark is excluded from this run, and cursor ordering remains ascending/unique despite concurrent inserts; document the exclusion contract.
- Assert a missing source, empty source, embedding failure, and final indexed-state write failure leave queryable failed/error state and a durable incremented retry count (where the database permits the fallback failure-state write).
- Assert a second concurrent worker cannot claim a processing document or repeat embedding side effects; assert retrying a failed document is an explicit second attempt.
- Assert the controller does not return a success response when the synchronous service reports per-document failures, and success is returned only after completion.
- Test migration upgrade/idempotence and rollback/compatibility behavior against only the authorized synthetic PostgreSQL schema; no pgvector extension is needed for the missing-source and embedding-failure cases.

## Risks & Considerations

- `knowledge_documents` is tenant-scoped. All new repository exports must accept `ctx` first and retain normal tenant callbacks; no `SystemScope` fallback is permitted. A background context must preserve the caller tenant if any async path remains.
- A fixed `MAX(id)` watermark with keyset pagination includes only rows in that run's committed ID range. Concurrent inserts are explicitly deferred; this is not a transaction-wide snapshot. Auto-generated monotonic IDs are assumed; explicit lower-ID inserts are outside the established document-write contract and must not be introduced by the implementation.
- A persistent database outage can prevent any failure marker from being written to that same database. The endpoint must return failure and log the persistence error; durable queryability is only guaranteeable when a fallback write can reach storage. Tests should inject a single final-state failure followed by a successful failed-state write, and separately verify hard propagation if both writes fail.
- Adding `reindex_retry_count` is an additive schema change with default zero, no tenant-specific index/unique constraint, and no existing row rewrite. Register a versioned, idempotent migration; rollback must not silently erase operational attempt history (follow repository's decline-destructive-Down convention if applicable).
- Synchronous processing of a large product can exceed HTTP/client timeouts. If that is an unacceptable API constraint, the alternative needs a durable job record, batch progress query, and 202 Accepted semantics; do not claim task acceptance from the current mixed execution model.
- Embedding persistence uses PostgreSQL `vector` casts for non-empty chunks. Verification must avoid pgvector by using isolated missing-source and injected embedding-failure cases where possible; any vector-success verification requiring pgvector must be reported separately as not run.

## Open Questions

- [NEEDS CLARIFICATION: The current endpoint mixes synchronous vector work with asynchronous zero-chunk work. This assessment prefers synchronous completion/error semantics. If product requirements require true asynchronous acceptance, a durable product-level job entity and query API are out of this scoped fix and require a revised assessment.]
- [NEEDS CLARIFICATION: Failure-state queryability cannot be guaranteed while the database itself is unavailable; the implementation can only persist a failed marker if a subsequent write succeeds.]
