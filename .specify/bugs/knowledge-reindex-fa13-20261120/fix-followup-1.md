# FA-13 follow-up fix 1

- **Date:** 2026-11-20
- **Scope:** snapshot-consistent rebuild membership and recoverable reindex processing leases, following the existing `assessment-followup.md` only.
- **Status:** implemented; awaiting the original spec reviewer’s review.
- **Historical boundary:** the original FA-13 implementation had no recorded RED run. This report does not revise that history or change `fix.md`.

## Changes

| File | Change |
|---|---|
| `user-server/internal/aiagent/knowledge/repository/knowledge_document.go` | Freeze product document IDs with bounded ascending keyset reads in a read-only PostgreSQL REPEATABLE READ transaction; atomically claim fresh/eligible rows and reclaim only absent/expired processing leases; set or clear the lease with status changes. |
| `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex.go` | Process the frozen IDs after snapshot collection; bound each reindex operation below the lease duration; persist failures through a bounded context detached from parent cancellation while preserving context values. |
| `user-server/internal/aiagent/knowledge/service/knowledge_service_pipeline.go` | Use the same bounded detached failure persistence for synchronous pipeline failures, including zero-chunk reindex work. |
| `user-server/internal/model/kb_workspace.go` | Add nullable, non-serialized `reindex_lease_until` field. |
| `user-server/internal/migration/migrations/knowledge_reindex_lease_migration.go` | Add an additive/idempotent lease-column migration; `Down` retains lease history. |
| `user-server/internal/migration/migrations/knowledge_reindex_lease_migration_test.go` | Verify registration, upgrade, idempotence, lease persistence, and non-destructive `Down` in a temporary synthetic schema. |
| `user-server/internal/migration/migrations/initial_schema.go` | Register the lease migration as v3.100.0. |
| `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex_test.go` | Update the repository snapshot helper assertion. |
| `user-server/internal/aiagent/knowledge/service/knowledge_reindex_followup_test.go` | Add service-level snapshot interleaving/isolation, lease expiry/concurrent reclaim/retry, and cancellation cleanup/retry coverage for chunked and zero-chunk paths. |

## Behavioral limits

- The lease supports recovery when a later explicit document or product reindex runs; no autonomous lease scanner is added.
- Expiry/reclaim is tested by persisting an expired timestamp, not by killing an OS process. External embedding is not claimed exactly-once.
- Synthetic PostgreSQL uses the existing text-backed vector fixture; pgvector operators and production behavior were not exercised.
- Existing FA-13 reports remain unchanged. Follow-up evidence is isolated under `evidence/followup-1/`.
- No staging, commit, push, deployment, outer-queue edit, business DB use, or destructive operation was performed.
