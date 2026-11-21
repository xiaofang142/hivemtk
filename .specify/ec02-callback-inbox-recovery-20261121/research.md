# Research: EC-02 Durable Callback Inbox and Compensation

**Feature**: `ec02-callback-inbox-recovery-20261121`

## Repository findings

- `MerchantCallbackService.Ingest` in `user-server/internal/service/merchant_callback.go` verifies the exact body before subscription lookup, verifies shop/scope/kind and connection consent/credential state, then calls repository `Record`; it ACKs only after `Record` returns committed event identity.
- `MerchantCallbackRepository` in `user-server/internal/repository/merchant_callback.go` currently exposes only `LookupSubscription` and `Record`. `Record` already has an idempotent unique identity, preserves conflicting first content, locks connection then subscription, and rechecks revocation/refresh/consent before insert.
- `MerchantCallbackEvent` has no persisted disposition, resource head, or consumer progress. The callback endpoint's production wiring passes a nil verifier map; this must remain unchanged.
- M02 explicitly requires durable event inbox and pagination compensation, and says old events must not override newer state. M03 separately owns command/event worker lifecycle, leases, retries, shutdown, and dispatch.
- Current latest merchant migration is v3.104.0. The follow-on must be v3.105.0, add upgrade-safe schema/backfill, and never rewrite v3.104.

## Decisions

### D1: Use per-subscription ID-keyset sweeps with a database high-water mark

**Decision**: Persist one compensation cursor row per `(tenant_id, subscription_id)` with a sweep-active flag, `high_water_id`, acknowledged `after_event_id`, and a persisted `page_end_id`. Select a fixed high-water ID under the existing subscription row lock, then persist each page's end ID before returning it. Read rows by `id > after_event_id AND id <= page_end_id ORDER BY id ASC`.

**Rationale**: Event IDs give a deterministic, monotonic keyset and avoid timestamp precision/offset drift. The subscription lock serializes same-subscription event insertion against capture of the high-water mark, so there are no uncommitted earlier same-subscription inserts hidden below the captured maximum. Rows above the snapshot are handled by a new sweep after the current one completes. Persisting the page end means a restarted reader reconstructs exactly the same ID range without process memory.

**Alternatives considered**:
- Timestamp-only cursor: rejected because equal/low-resolution timestamps can omit or duplicate rows.
- Offset pagination: rejected because inserts and disposition updates shift offsets.
- Global event ID cursor without a subscription lock: rejected because a lower sequence can commit after a higher ID and be missed by a fixed snapshot.
- Overlap time window: unnecessary for the local durable inbox; it adds duplicate scans while a locked fixed high-water and repeatable page boundary already provide no-omission recovery.

### D2: Keep events, derive current head via a persisted resource watermark, and never delete history

**Decision**: Add event disposition (`pending`, `superseded`, `applied`) plus an optional `superseded_by_id`; add one watermark per `(tenant, provider, subscription, kind, resource)` with current event ID and signed time. In the same transaction as insert, compare `(SignedAt, event ID)`. The newer tuple becomes head and marks the previous pending head superseded; an older arrival is persisted as superseded by the current head. A same-time later event wins by the greater durable ID.

**Rationale**: Provider event IDs are idempotency keys, not necessarily ordered versions. Authenticated signed event time plus a stable durable ID tie-break is the only version order available in the current verifier contract. Retaining superseded rows preserves replay/audit evidence, while current-head gating prevents old updates from being offered as eligible work.

**Alternatives considered**:
- Trusting arrival order: rejected because webhook delivery can be out of order.
- Deleting stale rows: rejected because the inbox is the durable evidence ledger and duplicates must resolve to the original ID.
- Maintaining a separate business-state projection: rejected because no generic provider business model/consumer exists in EC-02; future state repositories must apply with the supplied event ID/version fence.

### D3: Acknowledge an exact persisted page transactionally; do not schedule work here

**Decision**: Acknowledge using the persisted sweep fence/page boundary and exact page event IDs. In a repository transaction re-lock connection, subscription, then cursor; verify tenant, consent, current connection revision, page boundary, exact ID set, and current resource watermark for every event. Mark only currently pending current-head events `applied`, require every eligible event in the page be included by the caller, treat superseded/already-applied entries as no-op scan rows, then advance `after_event_id` to the persisted `page_end_id`. Any stale or partial ACK rolls back all changes. A connection version change invalidates the old page; the next read restarts a safe sweep from ID zero using the new revision and existing per-event dispositions.

**Rationale**: This closes the durable reader/consumer boundary and blocks stale or partially processed pages from silently moving forward. No scheduler, lease, provider client, or business mutation is required for this capability.

**Alternatives considered**:
- Marking a page processed when read: rejected because a crash would lose unprocessed work.
- Accepting a caller-provided high-water/cursor without durable validation: rejected because stale or forged progress could omit events.
- Adding worker leases/retries: rejected as EC-03 scope.

## Open live-validation dependency

No provider-specific API contract or credentials are approved in this slice. Cryptographic verifier/provider registries remain empty in production; `pending_live_validation` covers provider event ordering guarantees, provider pagination/compensation protocol, and the appropriate authoritative version if signed event time is not a provider-supported monotonic version. No live platform claim is made.
