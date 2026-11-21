# FA-13 quality follow-up assessment 3

- Date: 2026-11-20
- Source: user's relayed QUALITY_CHANGES_REQUIRED from reviewer `ses_e070e56c0ffeWDtZIiR9GCXkUB`; previous spec status is SPEC_APPROVED per user.
- Verdict: valid from source inspection; behavioral reproduction is the next phase, not yet claimed.
- Severity: high. Ordinary metadata writes can revoke active ownership; transient progress failures leave documents busy until lease expiry.
- Boundary: same FA-13, no subagents, staging, commit, push, deploy, outer queue edits, or business DB. Preserve all earlier files/reports/evidence and unrelated inflight changes.

## Read-only findings

### Important 1: stale metadata overwrites processing ownership

`controller/knowledge_workspace.go:336-374` reads a full document at 352, modifies title/category/tags/priority, and invokes `KnowledgeService.Update` at 370. `service/knowledge.go:291-304` forwards that object to the document repository. `repository/knowledge_document.go:348-350` calls `_db.SaveWholeRow`; `internal/pkg/db/write_guard.go:28-35` uses `Select("*").Save(row)`. This deliberately writes zero values as well as populated fields. If a Reindex claim occurs after the read, the stale metadata object rewrites its old owner token, lease, retry count, status, progress, error, and statistics.

Whole-server/repository Go call-site searches found exactly one production call to this concrete repository's `Update`: `KnowledgeService.Update`. Its only production caller is `KnowledgeWorkspaceController.UpdateDocument`. The allowed fields of that endpoint are `Title`, `Category`, `Tags`, `Priority`; repository-owned `UpdatedAt` is also required. No caller needs this method to change source/file descriptors, identity/tenant/product/agent assignment, help-center state/views, visibility/enabled state, arbitrary metadata/import attribution, processing fields, statistics, or index timestamps. Other repositories' `Update` methods (including GEO's separate document type and OpenAPI's source configuration) are different methods and must remain untouched. Consumers in merchant/statistics, incremental indexing, tools, websitekb and OpenAPI do not call this document Update for additional business fields.

Preferred correction: keep the method signature and tenant scoping, replace whole-row save with an explicit map of title/category/tags/priority/updated_at and `_db.EnsureAffected`. Preserve intentional zero values and the existing no-resurrection/not-found contract. No new specialized method or schema is needed based on actual callers.

### Important 2: progress errors bypass bounded failure persistence

`service/knowledge_service_pipeline.go:60/115/164/186` returns each `UpdateStatusForOwner` error directly for progress 10/30/60/80. `service/knowledge_service_reindex.go:57-59` wraps the zero-chunk pipeline error without writing failed. In contrast, embedding and terminal errors use a five-second `context.WithTimeout(context.WithoutCancel(ctx), ...)` failure write with owner/processing/live-lease predicates. A recovered database therefore still leaves a transient progress failure in processing with empty error until the eleven-minute claim expires.

Preferred correction: each progress failure calls the existing cause-preserving `markReindexFailed` helper with a `%w` progress-specific error. That helper already detaches cancellation, bounds cleanup, preserves tenant values, applies owner fencing, logs failure persistence errors, and joins original/cleanup errors. Keep zero-chunk Reindex's wrapping and avoid a second redundant cleanup. This shared pipeline change covers both acquiring async and already-owned Reindex paths.

## Behavioral RED then GREEN plan

1. Real synthetic PG interleaving: read old document, pause a real Reindex in controlled embedding after its claim, advance progress/statistics, save the old object via service.Update, verify owner/lease/count/status/progress/statistics remain current and the original worker completes. Also verify edited metadata/zero values and missing/cross-tenant rows do not insert or update.
2. For each progress 10/30/60/80, inject a single GORM update error before actual SQL and separately cancel the caller immediately before that update. Use zero-chunk public Reindex; assert original error identity, failed/error/lease cleanup, retry count 1, and immediate explicit retry success with count 2. Exercise the acquiring async wrapper too, expecting count 0 before a successful explicit Reindex/count 1.
3. At each progress, replace the old owner through a real expired-lease claim before returning the injected error; assert the cleanup attempt cannot change the new owner's state/lease/count/error/statistics and the new owner can finish. For a persistent update fault, assert both original and cleanup errors are returned and cleanup was attempted under a live, bounded, tenant-bearing context.
4. Record unchanged production source hashes and real expected RED failures before production edits; retain fixture mistakes honestly if any. Do not edit earlier regression tests to obtain RED.
5. After minimal fixes, run all new tests, previous ten service regressions with race, controller tests, three migrations, build/vet/layer/architecture/GORM/diff. Keep known whole-repository naked-goroutine and missing Graphify results explicit.

Minor controller barrier coverage will be added in a new controller test file using the existing synthetic fixture if it remains a small test-only addition. No product/controller behavior change is planned for that item; a passing pre-fix test is coverage, not a historical RED claim.

## Files likely to change

- Production: `user-server/internal/aiagent/knowledge/repository/knowledge_document.go`; `user-server/internal/aiagent/knowledge/service/knowledge_service_pipeline.go` only.
- New tests: `user-server/internal/aiagent/knowledge/service/knowledge_reindex_quality_test.go`; optionally `user-server/internal/aiagent/knowledge/controller/knowledge_workspace_reindex_completion_test.go`.
- This round only: assessment/fix/test-followup-3 and `evidence/followup-3/`.

## Risks, exclusions and review boundary

Do not expand metadata authority beyond observed callers. Keep original errors available via errors.Is, preserve tenant context during cleanup, and never let a failed cleanup overwrite a replacement owner. Fault injection is scoped to the fixture's DB handle and the individual document progress write; it does not stop/delete the cluster or connect anywhere else. Only the explicitly authorized FA12_PG_DSN is used, with temporary owned schemas and text-backed vector domain. No new schema migration is expected, so no PostgreSQL schema skill/workflow is invoked. Earlier TDD gaps remain historical facts. The Graphify report/wiki files are unavailable in the current graph output; no current graph is asserted. `.specify/extensions.yml` has no hooks.

Final delivery must wait for the parent's independent verification and the same quality reviewer; this implementation session does not claim reviewer approval or close FA-13.
