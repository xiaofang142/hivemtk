# Bug Fix: Knowledge document deletion transaction boundary

- **Slug**: service-gorm-transaction-seam-20261120
- **Fixed**: 2026-11-21
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

`KBDocumentRepository.DeleteWithChunks` owns lookup, chunk deletion, document deletion and commit. Service now calls that domain operation and retains best-effort file and index cleanup only after successful commit. No GORM handle or callback remains in the service interface or implementation.

## Changes

- `repository/kb_document.go`: replace unused generic Transaction forwarding with `DeleteWithChunks(ctx, id) (*KBDocument, error)`; preserve query predicates, context, delete order, chunk-error wrapping and rollback. Return a document only on successful commit.
- `service/knowledge_base.go`: replace Transaction in `kbDocumentStore` with DeleteWithChunks, delegate atomic database work, and remove GORM/root chunk-model imports. Public constructors and DeleteDocument signature are unchanged.
- `service/knowledge_base_import_fa12_test.go`: adapt its processing fake to the narrowed repository interface.
- `service/knowledge_tx_seam_test.go`: add a repository-operation regression, context/error propagation, real PostgreSQL rollback after chunk and document failures, index cleanup, tenant rejection and canceled-context checks. Keep cascade, file removal and typed NotFound coverage.
- `internal/service/arch_gorm_guard_test.go`: remove only the now-zero knowledge file's `{tx:1}` exception and update explanatory comments. The bidirectional ratchet requires deletion of cleared entries. All scanner logic, root limits and remaining exception are unchanged.

## Local Checks

- Before production edits, `TestTxSeam_DeleteDocumentUsesRepositoryOperation` failed both branches because the old implementation invoked the Transaction fake (`transaction is not used by FA-12 test`), rather than the domain operation.
- After the fix, `go test ./internal/aiagent/knowledge/service -run '^TestTxSeam_' -count=1 -v` passed in 1.169s. All PostgreSQL cases actually ran; no test was skipped. Both rollback injection points, cross-tenant/missing-tenant rejection and canceled contexts passed.
- `go test ./internal/service -run '^(TestArchServiceNoNewGormDB|TestGormGuardMetricHasTeeth|TestTxCallbacksAreInTransactionPosition)$' -count=1 -v` passed in 1.020s. Same 10 domains / 692 files scanned; callbacks decreased from 3 to 1 overall and registered exceptions from 2 to 1.
- An initial `make lint` from the Go subdirectory reported no such target; lint must be run from the app root. Full build, lint and regression results are recorded separately in test.md.

## Deviations from Assessment

None. Removing the cleared exception was explicitly assessed and tightens the architecture guard. There is no allowance increase or skipped guard.

## Scope and Residual Risks

No dependency, schema, migration, tenant configuration or API change. Keep existing best-effort cleanup semantics: a later filesystem/index cleanup error does not undo an already committed database deletion. This fix is local to the isolated worktree. Concurrent main-checkout source is untouched; no commit, push or deployment performed. Real-channel delivery and rollout acceptance remain distinct from this database-boundary fix.
