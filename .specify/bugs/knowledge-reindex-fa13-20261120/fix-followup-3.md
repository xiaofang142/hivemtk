# FA-13 quality follow-up fix 3

- Date: 2026-11-20
- Status: **DONE_WITH_CONCERNS**. Implementation checks passed; parent independent verification and the same quality reviewer's approval remain pending. FA-13 stays open.
- Assessment: `assessment-followup-3.md`, written before production changes.
- Review source: user-relayed `QUALITY_CHANGES_REQUIRED`, reviewer `ses_e070e56c0ffeWDtZIiR9GCXkUB`. The user reports existing `SPEC_APPROVED`; this round does not grant or infer quality approval.
- Evidence: `evidence/followup-3/`. Earlier reports and evidence remain unchanged.

## Important 1: preserve ownership during metadata edits

The only production caller of this document repository's `Update` is `KnowledgeService.Update`; its only production caller is `KnowledgeWorkspaceController.UpdateDocument`. The controller reads a document and edits `Title`, `Category`, `Tags`, and `Priority`. `caller-inventory.log` records searches for concrete types, constructors, calls and assignments, including other consumers in merchant/statistics, incremental indexing, agent tools, websitekb, release wiring and OpenAPI. GEO's similarly named document repository is a separate type. OpenAPI's other `Update` calls target source configurations; merchant chunk edits target the chunk repository.

`repository/knowledge_document.go` now updates an explicit map of `title`, `category`, `tags`, `priority`, and repository-owned `updated_at`, scoped by ID and the existing tenant callbacks. It uses `_db.EnsureAffected`, preserving missing-row errors and preventing insert fallback. Map updates retain intentional zero values. No observed legitimate business caller requires broader writes through this method. The controller's existing input rules are unchanged; service/repository zero-value behavior is tested directly.

The whitelist excludes identity/tenant/product/agent assignment, source/file descriptors, arbitrary metadata/import attribution, visibility/help-center/enabled state, processing status/progress/error, statistics/index timestamps, owner token, lease and retry count. A stale metadata object therefore cannot revoke an in-flight worker's claim. This is demonstrated with real PostgreSQL: read the old document, start public Reindex, pause its embedding after claim, advance progress/statistics, save the old object through service.Update, inspect the persisted claim and statistics, then release the original worker and observe indexed/1. A second test covers all four editable fields, zero values, protected fields, cross-tenant rejection and deleted-row non-resurrection.

## Important 2: persist failures at every progress exit

`service/knowledge_service_pipeline.go` now routes failures of progress 10, 30, 60 and 80 through the existing `markReindexFailed` helper. Each call wraps the repository error with `%w` and identifies the progress value. The helper uses a five-second `context.WithTimeout(context.WithoutCancel(ctx), ...)`, retains tenant values, and conditionally writes failed only for the active owner, processing state and live lease. Failure persistence errors are logged and joined with the original error. Zero-chunk Reindex keeps its existing wrapper without adding a redundant second cleanup attempt.

No other production file changed in this round. No model or application migration was added or changed. `knowledge_document.go.followup-3.diff` and `knowledge_service_pipeline.go.followup-3.diff` isolate this round from earlier uncommitted changes in those files.

New service tests exercise each progress value through public zero-chunk Reindex and the acquiring `processDocument` entry. One-shot write errors and cancellation before the update must leave queryable failed/error state, release the lease, retain the expected retry count, and allow immediate explicit retry to indexed with one additional Reindex attempt. Cleanup observes a live, bounded tenant context. Four persistent-error cases preserve both original and cleanup errors; four real replacement-owner claims prove old cleanup is attempted, returns claim-lost, changes no field of the newer document, and leaves the new owner able to complete.

## Minor controller coverage

The new `knowledge_workspace_reindex_completion_test.go` uses the existing authorized fixture and a controlled embedding barrier for a nonempty product. It verifies processing/1 while blocked, no completed response before release, and indexed/1 when HTTP 200 is actually sent. The response writer checks database completion at `WriteHeader`, reducing dependence on goroutine scheduling. This is coverage of existing controller behavior, not a historical controller RED or a production controller change.

## Verification and handoff

See `test-followup-3.md` for actual commands, exits, fixture corrections, TDD history and exclusions. Fourteen selected service tests (the previous ten plus four new tests) pass with race detection, as do three controller tests with race and three migration regressions. Build, vet, layer, architecture, GORM and diff checks pass. The repository-wide naked-goroutine gate and Graphify remain non-green for the recorded existing/environment reasons.

The baseline comparison before report delivery found no missing baseline file or changed file outside this round's source changes. No unrelated work was restored or overwritten. No new agents, staging, commit, push, deployment, outer-queue edit, business-DB connection, cluster stop or deletion occurred. Only fixture-owned synthetic schemas were cleaned by the existing fixture.

The parent must independently verify the concrete artifacts before resubmitting to reviewer `ses_e070e56c0ffeWDtZIiR9GCXkUB`. This session does not perform that independent review, contact the reviewer, update the queue, or close FA-13.
