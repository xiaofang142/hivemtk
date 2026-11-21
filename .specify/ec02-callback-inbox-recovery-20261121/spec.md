# Feature Specification: EC-02 Durable Callback Inbox and Compensation

**Feature Branch**: `ec02-callback-inbox-recovery-20261121`

**Created**: 2026-11-21

**Status**: In Progress

**Input**: User-authorized EC-02/M02 slice from the outer future-task list, focused on the explicit durable callback inbox, paged compensation, and preventing stale events from overriding newer state. The list is the detailed scope authority.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Preserve verified callback events safely (Priority: P1)

An authenticated callback is validated against its registered subscription, shop, tenant, scope, current consent, and current connection before its minimal event envelope is persisted. Duplicate delivery returns the original durable event. Invalid signatures, cross-shop events, revoked connections, and conflicting replays do not create consumable events.

**Why this priority**: The inbox must not accept an event that can cross authorization boundaries or become a business-side effect without authenticated evidence.

**Independent Test**: Against PostgreSQL, submit valid, duplicate, invalid, cross-shop, and revoked-connection cases; verify persisted rows and eligibility by tenant and subscription.

**Acceptance Scenarios**:

1. **Given** a currently enabled subscription and matching connection, **When** a verified event is ingested, **Then** one minimal event envelope is committed before success acknowledgment.
2. **Given** the same event is delivered again, **When** the same identity and content are verified, **Then** the existing event ID is returned and no second row is created.
3. **Given** invalid proof, wrong shop/tenant/scope, a revoked/reconnected binding, or conflicting replay content, **When** the callback is ingested, **Then** it is not eligible for consumption and no business data is changed.

---

### User Story 2 - Resume paged inbox compensation without omissions (Priority: P1)

A tenant-scoped callback consumer reads eligible persisted events in bounded pages. The page sequence has a durable per-subscription cursor and a fixed high-water mark; the cursor advances only when the exact page has been acknowledged. After a process restart, the same page is returned until acknowledged. Events that arrive beyond the current high-water mark are handled by a subsequent sweep. Pagination remains independent of any worker scheduler.

**Why this priority**: Callback delivery can be retried by a provider, but already accepted inbox events must also remain recoverable after local interruption.

**Independent Test**: Persist more events than one page, read a page, recreate the repository/service, and read again. Verify stable page identity, exact acknowledgment requirements, no skipped IDs, and discovery of an event committed after the captured high-water mark on the next sweep.

**Acceptance Scenarios**:

1. **Given** a tenant has more pending events than one page, **When** a page is requested, **Then** it returns at most the configured limit, a stable keyset position, and a durable high-water mark.
2. **Given** a page was read but not acknowledged, **When** the consumer restarts and requests it again, **Then** the same page boundary and eligible event IDs are returned.
3. **Given** a consumer acknowledges only a subset or supplies a stale cursor, **When** it tries to advance, **Then** the cursor does not move.
4. **Given** an event commits after a sweep's high-water mark, **When** the current sweep is completed, **Then** a subsequent sweep discovers it.
5. **Given** a different tenant or stale/revoked connection requests the cursor, **When** it reads or advances, **Then** access is rejected without revealing another tenant's event IDs.

---

### User Story 3 - Prevent older events from replacing newer resource state (Priority: P1)

For each tenant, subscription, event kind, and resource, only the event with the greatest authenticated event time (then durable event ID as a deterministic tie-breaker) remains eligible as the current inbox head. A delayed older event is retained for audit but marked superseded; it cannot be acknowledged as the current event. Reconnection/consent changes invalidate the old subscription fence.

**Why this priority**: Duplicate or out-of-order delivery must never make a previously observed resource state regress.

**Independent Test**: Insert a newer verified resource event first and then an older signed event. Verify the older row remains in history but is not consumable, the watermark still identifies the newer event, and stale completion is rejected.

**Acceptance Scenarios**:

1. **Given** a newer event is already persisted for a resource, **When** an older valid event arrives later, **Then** the old event is retained as superseded and the resource head is unchanged.
2. **Given** two events have the same signed time, **When** both are persisted, **Then** the greater durable event ID is the deterministic current head.
3. **Given** a consumer holds a page for an event that has since been superseded, **When** it acknowledges that stale page, **Then** the cursor cannot advance past the stale event as if it were current.
4. **Given** connection consent is revoked or replaced, **When** an old subscription is used to resume, **Then** the subscription/connection fence rejects the request.

## Edge Cases

- Empty inbox, no new events, page boundary exactly at high-water, and keyset gaps caused by other subscriptions.
- A page contains superseded rows between eligible rows; cursor movement is bounded by the rows actually evaluated, not by offset pagination.
- A new event arrives while a sweep is in progress; it is outside the captured high-water mark and must be discovered by the next sweep.
- A repository commit fails after attempting to insert or classify an event; no success acknowledgment or cursor advancement is returned.
- A refresh changes the connection revision without changing consent; a stale page cannot advance, and a fresh page restarts safely without losing pending events.
- Missing tenant context, system-scope consumer context, disabled subscription, invalid page limit, and wrong subscription ID fail closed.
- Event body bytes are not retained; only the pre-existing minimal verified event fields and digest are stored.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST persist a callback event only after verifier success and tenant/subscription/shop/scope/connection binding checks pass.
- **FR-002**: Event identity MUST be idempotent within tenant, provider, subscription, and provider event key; conflicting content for an existing identity MUST be rejected without changing the original row.
- **FR-003**: The system MUST persist a per-tenant/per-subscription keyset cursor and a fixed high-water event ID for each compensation sweep; pagination MUST be deterministic and bounded.
- **FR-004**: The cursor MUST advance only after the exact currently eligible event IDs in the scanned page are acknowledged; partial, stale, or wrong-tenant advancement MUST fail closed.
- **FR-005**: A page MUST be repeatable until acknowledgment, and committed events above the captured high-water mark MUST be discoverable in a later sweep without relying on process memory.
- **FR-006**: The current event head MUST be selected per tenant, subscription, event kind, and resource by authenticated `SignedAt` ascending order with event ID as a deterministic tie-breaker; delayed older events MUST remain auditable but MUST NOT remain eligible for consumption.
- **FR-007**: Subscription, connection, consent, tenant, and shop fences MUST be rechecked for page reads and acknowledgments. Revocation or a replaced consent version MUST invalidate old consumer requests.
- **FR-008**: The production callback verifier/provider registry MUST remain empty. This feature MUST NOT make external provider calls, send messages, or execute financial/business writes.
- **FR-009**: Worker scheduling, leases, retries, shutdown, and side-effect dispatch are out of scope and remain EC-03 responsibilities. The deliverable is the durable inbox/page/cursor/eligibility boundary for an EC-03 consumer.
- **FR-010**: Callback event payload retention MUST remain minimized; raw body and credential material MUST NOT be added to the inbox or logs.

### Key Entities

- **Verified Callback Event**: Minimal durable event identity, tenant/shop/subscription and consent references, signed time, verifier version, digest, connection revision, current disposition, and supersession relationship.
- **Resource Event Watermark**: Current event ID and signed time for one tenant/subscription/event-kind/resource tuple.
- **Compensation Cursor**: Durable tenant/subscription keyset position, captured high-water ID, connection/consent fence, and update time.
- **Subscription and Connection Binding**: Existing authorized mapping and connection state that constrain every ingest, page read, and page acknowledgment.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: All accepted current events are discoverable across bounded pages after a consumer restart, with no omission across at least 100 test events.
- **SC-002**: A repeated read before acknowledgment returns the same page boundary; incomplete or stale acknowledgments advance zero cursor fields.
- **SC-003**: In duplicate, equal-time, and out-of-order cases, each resource has at most one eligible current event and no older event can be acknowledged as current after a newer event is stored.
- **SC-004**: Cross-tenant, wrong-shop, disabled-subscription, revoked-connection, and replaced-consent cases produce zero eligible events and expose zero foreign event identifiers.
- **SC-005**: The existing production ingress continues to fail closed without a configured verifier, and the feature causes no external provider calls, outbound messages, or business/financial writes.

## Assumptions

- The existing callback verifier supplies a provider-authenticated event key, event kind, resource ID, signed time, and validity interval; no provider-specific polling protocol is invented here.
- The existing repository's subscription-row lock serializes accepted event inserts for a subscription. The compensation high-water mark is scoped to that subscription, so committed rows allocated after snapshot are beyond that sweep's visible maximum.
- The durable cursor is an internal repository/service boundary, not a tenant-facing management API.
- Current connection consent remains the authorization fence for already accepted events; a token refresh may change the connection revision but does not erase the accepted event. A stale page revision must be restarted.
- Historical callback rows created by v3.104.0 will be migrated into resource watermarks and stale older rows will be marked superseded without deleting inbox history.
