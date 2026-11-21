# Bug Verification: Product reindex visits all documents and exposes failures

- **Slug**: knowledge-reindex-fa13-20261120
- **Tested**: 2026-11-20
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

The FA-13 reproduction passed against an isolated synthetic PostgreSQL schema: a 1,001-document rebuild visited every row, persisted failure state and attempt counts, and returned a non-nil aggregate error. Watermark/claim concurrency, provider failure, final-state failure fallback/propagation, controller responses, and migration behavior also passed. Broader package suites remain inconclusive because unrelated fixtures require `POSTGRES_TEST_PASSWORD`; no production database or live API was used.

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (post-fix) | Synthetic PostgreSQL `TestRebuildIndex_VisitsAllDocumentsAndPersistsMissingSourceFailure` | pass | Exercises 1,001 documents, including missing and empty sources; verifies aggregate error, all rows failed/queryable, retry count 1, and explicit retry count 2. |
| Watermark and concurrency | `go test ./internal/aiagent/knowledge/service -run 'TestReindexCursorWatermarkAndAtomicClaim' -count=1` | pass | A post-watermark ID is excluded; concurrent claims yield one winner. |
| Embedding and state persistence | Service focused test command below | pass | Synthetic provider 503, fallback failed-state persistence, and propagation when both final-state writes fail. Uses a text-backed `vector` test domain, not pgvector. |
| Controller contract | `go test ./internal/aiagent/knowledge/controller -run '^TestRebuildProductIndex_Returns(FailureWhenDocumentReindexFails|SuccessAfterCompletion)$' -count=1` | pass | Error produces HTTP 500 and a queryable failed row; completed empty-product rebuild produces HTTP 200 with completion text. |
| Migration | `go test ./internal/migration/migrations -run '^TestKnowledgeReindexAttemptMigration$' -count=1` | pass | Upgrade, idempotence, and preserving the counter column on downgrade. |
| Focused service regressions | `FA12_PG_DSN='<authorized synthetic DSN>' go test ./internal/aiagent/knowledge/service -run 'TestRebuildIndex_VisitsAllDocumentsAndPersistsMissingSourceFailure|TestReindexCursorWatermarkAndAtomicClaim|TestReindex_EmbeddingFailureIsQueryableAndRetryable|TestReindex_FinalStatusWriteFailurePersistsOrPropagates' -count=1` | pass | `ok hivemtk-user/internal/aiagent/knowledge/service 8.436s`. |
| Build and static typing | `go build ./... && go vet ./...` | pass | Both completed successfully. |
| Layer boundary | `python3 scripts/check-layer-boundary.py` | pass | 0 violations. |
| Architecture checks | `bash scripts/check-architecture.sh` | pass with warning | Existing warning in `internal/service/webhook_wecomcs_puller.go` about a service interface; no new violations. |
| GORM-only check | `bash user-server/scripts/check-gorm-only.sh` | pass | No business-package `.Exec`/`.Raw` violations. |
| Goroutine check | `bash user-server/scripts/check_naked_goroutine.sh` | fail (pre-existing) | Reports repository-wide existing bare goroutines, none added by the FA-13 changes. |
| Full affected package suites | `go test ./internal/aiagent/knowledge/controller ./internal/aiagent/knowledge/service ./internal/migration/migrations -count=1` | blocked | Unrelated tests fail closed before connecting because `POSTGRES_TEST_PASSWORD` is unset. The explicitly authorized synthetic-DSN focused tests pass. |
| Graphify refresh | `python3 -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"` | not-run | Attempted; current Python environment reports `ModuleNotFoundError: No module named 'graphify'`. |

## Output Excerpts

```text
ok   hivemtk-user/internal/aiagent/knowledge/controller  0.584s
ok   hivemtk-user/internal/aiagent/knowledge/service     8.436s
ok   hivemtk-user/internal/migration/migrations         0.410s
PASS: build and vet
跨层违规：命中 0 处；基线豁免 0；新增 0
```

The full package run showed the unrelated fail-closed fixture error:

```text
测试库配置未通过安全门（未发起任何连接）：test password is missing; set POSTGRES_TEST_PASSWORD or POSTGRES_PASSWORD
```

## Residual Risks

- The package-wide suites could not fully run without their separately configured test database password.
- The synthetic schema's text-backed `vector` domain validates status orchestration only; pgvector extension behavior, operators, and performance were not tested.
- No live HTTP deployment or production database verification was performed.

## Recommendation

Keep the fix in place. The scoped synthetic PostgreSQL reproduction and critical regression tests pass, but mark verification partial until the affected package suites can run in their normal test-database environment. No commit, push, or deployment was performed.
