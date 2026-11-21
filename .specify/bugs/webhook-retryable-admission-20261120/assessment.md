# Bug Assessment: Webhook admission can acknowledge an event that was never saved

- **Slug**: webhook-retryable-admission-20261120
- **Created**: 2026-11-20
- **Source**: pasted task-list item and local source review
- **Verdict**: valid
- **Severity**: high

## Report (verbatim or summarized)

The existing FA-CHANNEL-04 item records that webhook deduplication reserves a Redis key before rate-limit and database admission. If either step fails, a platform retry can be classified as a duplicate even though no durable `webhook_events` row exists. The task requires retryable admission while preserving concurrent deduplication.

## Symptom

`WebhookService.Receive` claims an event as a duplicate before it has passed rate limiting or been persisted. A rate-limited request or a transient persistence failure can therefore leave a Redis reservation behind; a retry receives an accepted duplicate response without a durable event. The expected behavior is that only a durable event is eligible for duplicate acknowledgement, and a failed admission remains retryable.

## Reproduction

1. Send a valid webhook with a new event ID while the channel rate bucket is exhausted; inspect the corresponding Redis deduplication key.
2. Send a valid webhook with a new event ID using a cancelled request context so the database write fails; retry the same event with a live context.
3. Submit the same valid event concurrently and inspect the responses, durable row count, and queued jobs.
4. Leave a deduplication reservation without a corresponding row and retry the event; the retry must create the row rather than trust the stale reservation.

## Suspected Code Paths

- `user-server/internal/service/webhook.go:549-581` — calls `isDuplicate` before rate limiting and `WebhookEventRepository.Create`; persistence failures return after the reservation remains.
- `user-server/internal/service/webhook_dedup.go:232-248` — `isDuplicate` uses Redis `SetNX` as an admission decision with no durable-row confirmation or owner-release protocol.
- `user-server/internal/repository/integration.go:414-420` — `WebhookEventRepository.Create` writes the event and is backed by the unique `event_id` constraint.
- `user-server/internal/model/integration.go:230-247` — `WebhookEvent.EventID` is globally unique in `webhook_events`, providing a database-level concurrent idempotency boundary.
- `user-server/internal/service/webhook_recovery.go:450-472` — recovery can replay only rows that reached durable storage; it cannot recover a reservation-only event.

## Root Cause Hypothesis

High confidence: the Redis deduplication reservation is treated as proof of durable acceptance even though it is established before the two operations that can reject the request. The database unique constraint already provides a durable and concurrency-safe event identity, but the current control flow lets a non-durable cache reservation short-circuit that boundary.

## Proposed Remediation

**Preferred**: perform rate limiting before any deduplication admission side effect, then use the durable `webhook_events.event_id` unique constraint as the admission and concurrent-idempotency boundary. Create the event before enqueueing it; map a unique-key conflict to an accepted duplicate, but return persistence errors as rejected and retryable. Do not let a pre-persistence Redis reservation decide whether an event is accepted. This also makes a stale reservation harmless: a retry first establishes the durable row, and the recovery scanner can handle a process exit after persistence but before queue delivery.

Retain the existing queue and recovery behavior so a durable row remains recoverable if enqueueing or the process fails after persistence. Keep direct cache helper coverage where it documents the cache contract, but remove its use as the pre-persistence admission gate.

**Files likely to change**:

- `user-server/internal/service/webhook.go`
- `user-server/internal/service/webhook_channel04_admission_test.go`
- `.specify/bugs/webhook-retryable-admission-20261120/fix.md`
- `.specify/bugs/webhook-retryable-admission-20261120/test.md`

**Tests to add or update**:

- Rate-limit rejection leaves no deduplication reservation.
- A cancelled/transient persistence failure is rejected without a reservation and succeeds on retry.
- Concurrent identical requests produce one durable row and one queued job, with duplicate responses for losing requests.
- A stale reservation without a durable row does not block a retry.

## Risks & Considerations

- Moving duplicate arbitration to the database adds a write attempt for concurrent/replayed events, but it is the existing unique constraint and avoids false acceptance.
- The globally unique `event_id` must continue to include channel/account scope as currently produced by `officialEventID` and fallback generation.
- A process exit after persistence but before enqueue remains dependent on the existing recovery scanner; no new migration is required.
- Full-repository checks may expose unrelated in-flight failures; those must be recorded rather than changed as part of this fix.

## Open Questions

- None blocking the local remediation; production recovery timing remains an operational concern covered by existing scanner configuration.
