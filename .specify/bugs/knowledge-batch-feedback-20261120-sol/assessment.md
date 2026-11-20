# Bug Assessment: Knowledge batch import and chunk feedback

- **Slug**: knowledge-batch-feedback-20261120-sol
- **Created**: 2026-11-20
- **Source**: pasted audit findings and request to automatically handle fixable findings
- **Verdict**: valid
- **Severity**: medium

## Report

Five audited issues are in scope: accepted `.txt` files fail preview; quoted newlines in CSV previews are truncated; string-shaped batch errors render blank; the page calls asynchronous document acceptance “import complete”; and chunk re-embedding failures are swallowed after content is saved. The fix must preserve the distinction between content persistence and index readiness, and must not expose stale vectors for changed content.

Review follow-up adds two omissions in those same fixes: the shared embedding-dimension filter can silently remove requested vectors before chunk re-embedding, and the CSV preview currently removes blank-content rows/empty tag entries that the server retains. The latest caller request expands completion to the remaining actionable batch-index feedback: reuse the existing document-progress and reindex APIs to show per-document terminal state and allow retry of failed indexing. Do not add rerank to the embedding path or call live third-party APIs.

## Symptom

The batch page can reject a supported text file before import, show CSV preview content that differs from the uploaded file’s parsed content, hide per-row rejection reasons, and imply asynchronous acceptance is indexing completion. Chunk edits can return success after embedding failure, while the prior vector remains available for retrieval despite changed content.

## Reproduction

1. Preview an auto-detected `.txt` file: `BatchImport.vue` defaults unknown suffixes to JSON although the server accepts `.txt` as text.
2. Preview CSV whose quoted content contains a newline: the client splits physical lines before parsing records.
3. Render `errors: ["...reason..."]`: the result table asks for an object property `value` from each string.
4. Complete a batch import while `KnowledgeService.Import` returns before its asynchronous indexing pipeline finishes: the page says “导入完成”.
5. Update a chunk while embedding or embedding persistence fails: `UpdateChunk` logs the failure and returns nil; the repository update does not clear the pgvector column or reset index status. `SplitChunk` has the same swallowed-error path.

## Suspected Code Paths

- `user-web/src/views/KnowledgeWorkspace/BatchImport.vue`: format detection, client CSV parser, error table, result title/toast.
- `user-server/internal/aiagent/knowledge/service/knowledge_merchant_chunk.go`: update/split re-embedding and swallowed failures.
- `user-server/internal/aiagent/knowledge/repository/knowledge_chunk.go`: chunk updates and embedding status/vector persistence.
- `user-server/internal/aiagent/knowledge/controller/knowledge_merchant.go`: common chunk-write error response.
- `user-server/internal/pkg/i18n/backend_phrases.go`: localized safe partial-indexing error response.
- `user-server/internal/aiagent/knowledge/service/knowledge.go` and `knowledge_merchant_batch.go`: async-import semantics and strict vector completeness validation for chunk re-embedding.
- Review follow-up: `knowledge.go` chunk re-embedding must reject any dimension-filtered target before persistence is reported successful; `batchImportParser.js` must preserve backend CSV blank-content records and empty tag tokens for preview parity.

## Merit and severity

Valid, medium severity. The batch defects mislead users or prevent supported input; the chunk path can make stale semantic retrieval possible and reports a false success. Existing audit evidence reproduces the frontend parser and response-shape issues, and source tracing establishes the async and chunk error paths. Full browser, business database, and external embedding verification have not been performed.

## Preferred remediation

- Extract batch preview parsing to a small module and align extension detection and CSV parsing with server behavior, including TXT, CRLF, escaped quotes, and quoted newlines.
- Render string errors directly; use accurate “accepted/processing” copy and an explicit indexing-delay hint, with translations in all nine locale catalogs. Reuse existing `GET /api/knowledge/documents/:id/progress` and `POST /api/knowledge/documents/:id/reindex` endpoints for bounded per-document polling and explicit retry of failed documents; stop polling when terminal or the view is disposed. Do not claim accepted means indexed.
- Clear the old pgvector value and set chunk embedding state to pending whenever edited content is persisted. On re-embedding failure, mark affected chunks failed where possible, log internal details, and return a typed safe error for update and split. Map that sentinel to a service-unavailable response; the existing UI error path must leave the editor open.
- Use focused Vitest parser/component tests and Go service/repository/controller tests; use only the dedicated local test database at 127.0.0.1:8217 if needed, never the business database.

Alternatives rejected: merely returning the embedding provider error (leaks implementation details and leaves stale vectors); rolling back the content update (cannot atomically roll back the external embedding call); adding a new batch-status backend endpoint (the current per-document progress/reindex APIs can be reused).

## Files likely to change

- `user-web/src/views/KnowledgeWorkspace/BatchImport.vue`
- `user-web/src/views/KnowledgeWorkspace/batchImportParser.js` (new)
- `user-web/tests/unit/knowledge_batch_import.test.js` (new)
- `user-web/tests/unit/knowledge_batch_import_feedback.test.js` (new)
- `user-web/src/i18n/locales/ar.json`
- `user-web/src/i18n/locales/de.json`
- `user-web/src/i18n/locales/en.json`
- `user-web/src/i18n/locales/es.json`
- `user-web/src/i18n/locales/fr.json`
- `user-web/src/i18n/locales/ja.json`
- `user-web/src/i18n/locales/pt.json`
- `user-web/src/i18n/locales/ru.json`
- `user-web/src/i18n/locales/zh.json`
- `user-server/internal/aiagent/knowledge/service/knowledge_merchant_chunk.go`
- `user-server/internal/aiagent/knowledge/service/knowledge.go`
- `user-server/internal/aiagent/knowledge/service/knowledge_service_pipeline.go`
- `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex.go`
- `user-server/internal/aiagent/knowledge/service/knowledge_merchant_chunk_feedback_test.go` (new)
- `user-server/internal/aiagent/knowledge/controller/knowledge_workspace.go`
- `user-server/internal/aiagent/knowledge/controller/knowledge_workspace_progress_scope_test.go` (new)
- `user-web/src/api/knowledge.js`
- `user-server/internal/aiagent/knowledge/repository/knowledge_chunk_feedback_test.go` (new)
- `user-server/internal/aiagent/knowledge/repository/knowledge_chunk.go`
- `user-server/internal/aiagent/knowledge/controller/knowledge_merchant.go`
- `user-server/internal/pkg/i18n/backend_phrases.go`
- `user-server/internal/aiagent/knowledge/controller/knowledge_merchant_chunk_feedback_test.go` (new)
- `.specify/bugs/knowledge-batch-feedback-20261120-sol/{assessment,fix,test}.md`
- `/Users/xiaofang/Documents/www/go/hivemtk/docs/未完成任务清单.md` (audit statuses/evidence only)

## Risks and verification

No schema migration is expected: `knowledge_chunks.embedding`, `embed_status`, and `embedding_source` are already used by current code. Verify pgvector NULL exclusion and failed/pending status behavior against the dedicated test DB; if local test DB access/configuration fails, record that limitation rather than touching or rebuilding databases. Run affected Vitest tests, full frontend tests and lint/i18n checks, plus Go build and knowledge service/controller/repository package tests as practical. No browser or production checks are in scope.

For the review follow-up, use a local `httptest.Server` with the third-party embedding adapter (never a live provider) to exercise an output that passes provider-configured dimensionality but is rejected by the model registry; prove chunk re-embedding returns an error rather than silently persisting a subset. Add CSV parser tests for empty/whitespace content, comma-only records, and empty tag tokens. Keep the generic dimension filter unchanged for unrelated callers.

Latest scope expansion: component tests should mock existing document progress/reindex APIs, verify status terminal states and retry behavior, and ensure polling timers stop on disposal. Use bounded concurrency for progress requests; do not require a real browser, database, or third-party API. Follow-up source tracing found that dimension-filtered vectors could also make normal async import or manual Reindex report a document indexed after silently persisting only a subset. Apply strict all-target validation in those paths too: fail the document, persist no partial vector subset, and expose accurate status for retry. Keep unrelated callers of the shared filter unchanged; use the already assessed service files and safe localized service error text. A later review found the authenticated progress endpoint did not scope document reads by product; require a non-empty product_id, query by product+document ID, return not-found on mismatches, and update the only frontend caller to pass the batch product. Test missing-scope rejection without DB; database-backed cross-product verification remains out of scope due destructive test helper behavior.
