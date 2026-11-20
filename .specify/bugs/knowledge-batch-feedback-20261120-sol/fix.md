# Fix Report: Knowledge batch import and chunk feedback

## Changes

- Extracted batch preview parsing into `batchImportParser.js`; auto-detects `.txt` case-insensitively, derives its title from the filename, trims its body, and parses quoted multiline CSV records with escaped quotes and backend header aliases.
- Updated the batch result table to render string-shaped errors. Reworded the result and toast as accepted/submitted, added an indexing-in-progress caveat, and localized the new copy in all nine locale catalogs. The result now shows per-document index state/progress and supports retry through existing APIs, with bounded polling, limited concurrency, manual refresh, and timer cancellation on close/tab change/unmount.
- Chunk edits now clear the old vector and set embedding state to pending in the content update. If re-embedding fails for update or split, affected chunks are best-effort marked failed and a safe typed error is returned; the controller maps it to HTTP 503 without exposing provider details.
- Re-embedding now rejects any dimension-guard-filtered vector batch instead of treating an empty or partial persistence set as success. The same all-or-nothing validation is applied to normal async document indexing and manual Reindex, so no path marks a document indexed after persisting only a subset. Other callers of the generic filter are unchanged. A local `httptest` TEI endpoint exercises the third-party adapter with a vector dimension that the selected model registry rejects; no live provider call is made.
- CSV preview now retains empty/whitespace content rows for server-side rejection and preserves empty tag elements, matching the current Go parser rather than silently hiding or rewriting those rows.
- Added focused parser/component tests and DB-free repository-field, service-error-flow, and controller-response tests.
- Scoped document-progress reads by required `product_id`; the service fetches by product and document ID, and the frontend sends the batch product ID. Missing product scope is rejected before service access.

## Scope notes

- Content remains saved when re-embedding fails; the error explicitly reports partial persistence rather than implying a rollback.
- Existing parallel changes were preserved. No business database, production system, browser session, deployment, commit, or destructive test database helper was used.
- The batch page’s automatic progress checking is bounded and stops retrying a lookup after its first API error until manual refresh. It shows safe generic feedback, not raw provider/storage error text.
- The dimension mismatch is validated through the local HTTP adapter fixture and strict pre-persistence helper, not through PostgreSQL persistence or a full service/UI request path.
- Progress product scoping has a controller test for rejecting missing scope; cross-product row isolation is not exercised against a database because DB tests are intentionally not run here.
