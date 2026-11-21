# Data Model: EC-02 Durable Callback Inbox and Compensation

## Existing entity extension: `MerchantCallbackEvent`

Table: `merchant_callback_events` (created by v3.104.0; extended by v3.105.0).

- Existing identity remains unique on `(tenant_id, provider, subscription_id, event_key)`.
- Existing evidence remains minimal: authenticated shop, consent version, kind, scope, provider resource ID, body digest, verifier version, signed time and validity time.
- Add `connection_version` and `credential_version` captured from the connection snapshot verified by the current ingest transaction.
- Add `disposition` (`pending`, `superseded`, `applied`) with default `pending` for existing rows and `superseded_by_id` nullable.
- Add a keyset index on `(tenant_id, subscription_id, id)` and a current-event lookup index on `(tenant_id, subscription_id, kind, resource_id, disposition, signed_at, id)` as supported by the model/migration.
- Events are retained. Conflicting replay never modifies the existing row.

## New entity: `MerchantCallbackResourceWatermark`

Table: `merchant_callback_resource_watermarks`.

- Key: `(tenant_id, provider, subscription_id, kind, resource_id)`.
- Stores the current head `event_id`, `signed_at`, `consent_version`, and source connection/credential revision.
- Every event insert and watermark/disposition change is one transaction under the existing connection then subscription lock order.
- Winner order is lexicographic `(signed_at, event_id)`. An older event remains stored but is marked superseded. A newer event supersedes the former pending head. If former head was already applied, it remains historical; the watermark moves to the newer event while the former completion remains recorded.

## New entity: `MerchantCallbackCompensationCursor`

Table: `merchant_callback_compensation_cursors`.

- Key: `(tenant_id, subscription_id)`.
- Stores `active`, `high_water_id`, `after_event_id`, `page_end_id`, `page_limit`, `connection_id`, `connection_version`, `credential_version`, `consent_version`, and `updated_at`.
- `active=false` means the next read begins a new sweep and captures a fresh high-water mark. Active page bounds survive process restart.
- `after_event_id` advances only as part of an exact page acknowledgment. `page_end_id` is set at first read and remains stable until acknowledgment.
- All values are internal repository state. Tenant and system-scope checks are applied before any row lookup.

## Relationships and authorization

- Event → subscription and event → connection are constrained by tenant, provider, shop, and consent version.
- Watermark and cursor are subscription-local and tenant-scoped.
- Read/ack validate subscription enabled state, current connection active state, required scope, matching shop/provider, consent version, and live credential status. A connection/credential revision mismatch invalidates an outstanding page; the next read restarts from zero while retaining event disposition.

## Migration/backfill

v3.105.0 extends the existing v3.104.0 tables without dropping data. It adds event fields with safe defaults, creates both new tables and their composite unique keys, then scans existing callback events in `(tenant_id, provider, subscription_id, kind, resource_id, signed_at ASC, id ASC)` order to construct each resource's greatest current head and mark earlier pending history superseded. Running `Up` twice preserves event rows and produces the same head. `Down` is a no-op to preserve acknowledged evidence and authorization fences.
