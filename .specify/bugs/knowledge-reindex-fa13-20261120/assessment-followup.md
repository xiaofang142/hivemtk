# FA-13 follow-up assessment

- Date: 2026-11-20
- Source: SPEC_CHANGES_REQUIRED from `ses_e080949d3ffe5AAEH7GJyY2kLm`, supplied by parent
- Verdict: valid (source confirmed; behavioral reproduction follows in fix phase)
- Scope: same branch/workspace, sole implementer; no staging, commit, push, deploy or outer queue changes

The initial implementation lacked recorded RED evidence. Historical TDD cannot be reconstructed or claimed retroactively. Existing assessment/fix/test reports stay unchanged. These corrections will have tests written first and frozen real RED/GREEN logs, commands, exit codes and source hashes under `evidence/`.

## Root causes and corrected contract

1. MAX(id) does not delimit commit visibility. A transaction can reserve a sequence ID below the maximum and commit between pages. Freeze only IDs visible in one PostgreSQL REPEATABLE READ read-only snapshot, using bounded ascending keyset queries. Later commits belong to the next rebuild regardless of ID. Collect IDs only; close the transaction before any document processing. Keep tenant callbacks and product predicates on every read.
2. `processing` currently refuses claims forever. Add a dedicated durable lease expiry column, atomically set it with processing status and incremented attempt count, and allow a single atomic reclaim only after expiry. Existing rows without a lease are legacy/unleased and recoverable; ordinary processing updates establish a lease so live imports are protected. Bound reindex execution below the lease duration and bound detached failure cleanup. Do not use `updated_at` as the lease. Failure cleanup uses `context.WithoutCancel` plus timeout, retaining tenant and trace values. A dead worker requires the next explicit document/product reindex after expiry; no autonomous recovery scanner is claimed.

## Files likely to change

- `user-server/internal/aiagent/knowledge/repository/knowledge_document.go`: snapshot collection and atomic leased claim; processing/terminal lease updates.
- `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex.go`: process frozen IDs, bounded execution, cancellation failure cleanup.
- `user-server/internal/aiagent/knowledge/service/knowledge_service_pipeline.go`: preserve failure causes and detached bounded cleanup for synchronous zero-chunk reindex.
- `user-server/internal/model/kb_workspace.go`: nullable `reindex_lease_until` timestamp.
- `user-server/internal/migration/migrations/knowledge_reindex_lease_migration.go` and `_test.go`: new additive/idempotent migration, preserve column on Down.
- `user-server/internal/migration/migrations/initial_schema.go`: register next free version.
- `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex_test.go`: replace obsolete watermark-helper assertions with stable snapshot/service behavior.
- `user-server/internal/aiagent/knowledge/service/knowledge_reindex_followup_test.go`: public-service interleavings, lease recovery and cancellation regressions.
- This slug's follow-up reports and evidence files only. Existing reports and unrelated changes are protected.

## Ordered implementation and acceptance

1. Write behavioral tests without changing product code. Synthetic PG lower sequence ID remains uncommitted until the public RebuildIndex has read its first page; then commit it before later pages/process completion. Assert exactly the documented visible membership, no duplicate attempts, tenant/product isolation, and inclusion on the next run. Check snapshot transaction closed before claims.
2. Public Reindex tests: fresh lease refused; concurrent expired claims produce one winner held behind a clean barrier; failure count increments, queryable failure, explicit retry; canceled/deadline-expired parents still persist failure with preserved tenant/trace and a bounded cleanup deadline, including zero-chunk pipeline entry.
3. Freeze expected assertion failures plus source hashes before product edits. Add migration regression before implementing migration. Implement minimal corrections, then freeze GREEN and run >=1001-document, controller, migration and focused race regressions.
4. Write fix-followup, enter verification-only phase, run build/vet/layer/architecture/GORM/diff and record actual results in test-followup. Review remains with parent: same spec reviewer first; quality review only after spec approval. No subagents launched here.

## Limits

Only the already authorized local synthetic PG cluster is used. No testutil.NewTestDB, business DB, pgvector installation, cluster stop/removal, external messages, or git history changes. The text-backed test vector domain does not validate pgvector operators. Lease recovery tests simulate persisted expired state; they do not claim an OS process kill or exactly-once external embedding. All selected IDs require O(N) memory; the short read transaction spans ID collection only. Live HTTP timeouts and complete DB unavailability retain the original documented limitations. A Go overlay of committed HEAD sources may demonstrate original defects now; it cannot supply missing historical RED evidence.
