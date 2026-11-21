# Quickstart: EC-02 Callback Inbox Recovery

## Prerequisites

- PostgreSQL dedicated fast test database is available at `127.0.0.1:8217`.
- From `user-server/`, set only the test password variable from the inner repository `.env`; do not inherit a production DSN:

```bash
unset POSTGRES_TEST_DSN
export POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217
export POSTGRES_TEST_PASSWORD="$(grep '^POSTGRES_PASSWORD=' ../.env | head -1 | cut -d= -f2-)"
```

## Focused tests

```bash
go test ./internal/service -run '^TestMerchantCallbackInbox_' -count=1 -v
go test ./internal/repository -run '^TestMerchantCallbackInbox_' -count=1 -v
go test ./internal/migration/migrations -run '^TestMerchantCallbackInboxMigration_' -count=1 -v
```

Expected scenarios: valid verified event persistence, duplicate idempotency, older event supersession, deterministic equal-time ordering, tenant/shop/revoke fences, durable page replay after repository reconstruction, exact acknowledgment, high-water rollover, and existing v3.104 inbox backfill preserving event identities.

No live provider is configured or invoked. `nil` production verifier registration remains fail-closed.
