# FA-13 follow-up fix 2

- **Date:** 2026-11-20
- **Status:** DONE_WITH_CONCERNS; implementation evidence ready for the parent's independent rerun and the original spec reviewer's re-review. No spec or quality approval is claimed.
- **Scope:** `assessment-followup-2.md`: shared async/Reindex ownership, timeout-sized leases, and stale-owner persistence fencing.
- **Historical boundary:** prior FA-13 reports and evidence remain unchanged. This round is recorded under `evidence/followup-2/`.

## Changes

| Area | Resulting behavior |
|---|---|
| Document repository | Async and Reindex share a product-scoped, tenant-context-scoped row-lock claim. A fresh processing lease rejects another claimant. Each successful claim receives a separate random owner token. Only Reindex claims increment `reindex_retry_count`. |
| Lease lifetime | Async claims cover the configured timeout or a longer remaining caller deadline, plus one minute. Reindex retains its ten-minute execution limit and claims an eleven-minute lease. Owner progress writes preserve the original lease. |
| Pipeline and Reindex | `processDocument` acquires ownership; already-claimed zero-chunk Reindex work calls `processDocumentOwned` without acquiring a second time. Duplicate async work stops before parsing, chunk creation, or embedding. |
| Document writes | Progress, chunk statistics, success, and failure writes require the active owner token, processing state, and unexpired lease. Zero affected rows returns `ErrKnowledgeDocumentClaimLost`. Failure cleanup retains its bounded cancellation-detached context and terminal-write fallback. |
| Chunk and parent repositories | Owner-sensitive creation and vector persistence lock the document inside the same short transaction as the writes, serializing them against reclaim. External embedding requests run outside those locks. |
| Contextual enhancement | Pipeline passes the owner token into the enhancer. Contextual chunk writes lock and check the owner inside their transaction. This path has a verification gap described below. |
| Model and migration | Hidden `reindex_owner_token` varchar(64), not null with an empty default; additive/idempotent v3.101.0 migration registered after v3.100.0. `Down` retains the column. |

An independent token is necessary because the existing retry counter is an externally meaningful Reindex-attempt count. Using it for every async claim would change that meaning. The lease timestamp is also not an owner identity. The migration test's valid RED (`owner-migration-red-2.log`) precedes the model/registration/migration addition.

## Review concerns and limits

1. Contextual enhancement fencing is implemented but not behaviorally exercised by the recorded service regressions: the existing enhancer reads omit the supplied context, and the synthetic tenant callback rejects the initial document query. The logs show this non-blocking warning. Do not treat those runs as proof of contextual write fencing.
2. The optional external indexer has an owner check immediately before `BuildIndex`; this is not atomic with that external call. No cross-system fencing guarantee or external indexer race test is claimed. Cache invalidation and aggregate product statistics after terminal success are also outside the owner transaction.
3. Focused suites pass, but full affected-package and repository-wide test suites were not run. The fake-PDF test stopped at its separate missing-password safety gate without connecting; no business database fallback was attempted.
4. Repository-wide naked-goroutine checking still exits 1 on existing call sites, and Graphify refresh exits 1 because the Python module is unavailable. No new production goroutine is introduced by this correction.
5. Tests use the authorized synthetic PostgreSQL fixture with text-backed vectors. No production migration, pgvector operator verification, OS process-kill recovery, or exactly-once external embedding guarantee is claimed.

No staging, commit, push, deployment, business-database access, outer-queue update, or subagent invocation was performed. Same spec reviewer re-review is required before quality review or any queue update; the parent must independently rerun the checks.
