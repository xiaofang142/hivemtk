# Test Report: Knowledge batch import and chunk feedback

## Passed

- `npm test -- --run tests/unit/knowledge_batch_import.test.js tests/unit/knowledge_batch_import_feedback.test.js` — 2 files, 12 tests passed, covering parser parity, accepted/error feedback, per-document pending→indexed, failed→retry, retry/API errors, safe messages, concurrency cap, and timer cancellation/late responses.
- `npm run test -- --maxWorkers=2` — latest run: 104 files, 988 tests passed (374.11s).
- `npm run lint:check` — latest run: exit 0, no errors or warnings.
- `npm run i18n:check` — passed; no missing non-base locale keys.
- `go test ./internal/aiagent/knowledge/service -run '^Test(ChunkReembedRejectsProviderVectorsFilteredByModelDimension|ReindexChunksAfterWrite)' -count=1` — passed; the adapter test uses a local `httptest` TEI endpoint returning a vector rejected by the model registry dimension guard. No live provider call.
- `go test ./internal/aiagent/knowledge/controller -run '^TestGetDocumentProgressRequiresProductScope$' -count=1` — passed; missing product scope is rejected before service access.
- `go test ./internal/aiagent/knowledge/repository -run '^TestChunk(UpdateFieldsInvalidateStaleEmbedding|EmbeddingFailedFieldsKeepVectorUnavailable)$' -count=1` — passed.
- `go test ./internal/aiagent/knowledge/controller -run '^TestChunkWriteIndexingFailureReturnsSafeServiceUnavailable$' -count=1` — passed.
- `go build ./... && go vet ./...` — passed; the compiler emitted two upstream `go-m1cpu` clang VLA-extension warnings.
- Scoped `git diff --check` — passed.

## Not verified

- No browser/page test, HTTP-to-database integration, real embedding provider, lost-response/restart scenario, or real per-document asynchronous indexing lifecycle was exercised. Progress/retry UI behavior and API parameter forwarding were tested with mocks, not a running backend.
- The Go helper `testutil.NewTestDB` drops/recreates its dedicated per-process database. To honor the no-destructive-actions constraint, no database-backed package test or test-database reset was run. Repository tests assert update-field maps, not PostgreSQL behavior.
- Graphify rebuild was attempted but unavailable because the Python environment lacks the `graphify` module (`ModuleNotFoundError`); no package installation or environment modification was made.

The batch copy now accurately reports acceptance and the page displays scoped per-document progress with retry. Checklist items remain open wherever their criteria require database, browser, lost-response/restart, or real asynchronous terminal-state evidence not covered above.
