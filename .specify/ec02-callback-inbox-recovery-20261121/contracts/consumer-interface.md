# Consumer Interface Contract: EC-02 Callback Inbox

This is an internal repository/service contract for a future EC-03 consumer. It does not expose an HTTP API and does not start a worker.

## Read a page

`ReadCompensationPage(ctx, subscriptionID, limit) -> MerchantCallbackCompensationPage`

- Requires a non-empty authenticated tenant context; system scope is not accepted.
- Revalidates the enabled subscription, current shop/connection/consent, scope, and live credential status.
- Clamps page size to a safe range (default 50, maximum 100).
- Captures/persists a fixed per-subscription `high_water_id` at sweep start and persists `page_end_id` before returning.
- Returns event rows in ascending ID order, no greater than the active high-water, with their disposition and current-head eligibility. Consumers must only act on `pending` current-head events.
- Repeated reads before acknowledgment return the same page ID boundary after process restart. Disposition may have advanced to `superseded` if a newer event arrived; such a row is no longer eligible for business action but remains in the scanned page.
- Returns only minimal verified event fields; no original body or credential material.

## Acknowledge a page

`AcknowledgeCompensationPage(ctx, page, appliedEventIDs) -> error`

- Caller supplies the page snapshot and IDs it successfully applied to the appropriate idempotent downstream state.
- Repository verifies tenant/subscription/current connection/consent/credential revision and exact persisted sweep/page fences.
- Repository reloads and compares the exact set of page IDs. Caller cannot move the cursor by supplying an arbitrary boundary or subset.
- Every pending current-head event in the page must be listed in `appliedEventIDs`; IDs already superseded by a newer event are safely skipped and must not appear in the applied set. Already-applied history after a fence-triggered restart is also skipped.
- If any listed event is no longer the current head, the entire operation fails without changing dispositions or cursor.
- On success, current pending events become `applied`; the cursor advances atomically to the persisted page end; the next page or next sweep is available.

## Ordering and downstream projection fence

- Ingestion preserves all unique event evidence, with one current head per tenant/subscription/kind/resource.
- The authenticated event ordering is `(signed_at, durable_event_id)`. An event lower than the persisted head is superseded before it can be offered as eligible work.
- A downstream projection must make its own write conditional on the event ordering/fence and idempotency key; this EC-02 interface does not perform provider business writes or update a provider-specific order/aftersale projection.
- A newer event committed after a page high-water is discovered by the next sweep. A newer event committed before acknowledgment supersedes the old head, causing the old page ACK to roll back.

## Error behavior

- Missing/system tenant, disabled/unknown binding, wrong subscription, revoked/expired/refreshing connection, consent change, or stale connection revision: fail closed with no cursor progress.
- Partial/mismatched/replayed page ACK: fail closed with no cursor progress.
- Persistence/commit error: return a repository storage error; caller must not claim durable acknowledgment.

## Explicit exclusion

Worker lifecycle, claim leases, retry policy, graceful shutdown, dead-letter handling, external provider pagination APIs, and actual business-state mutation are EC-03/provider-adapter work. Production verifier registration remains empty until independently approved and live validated.
