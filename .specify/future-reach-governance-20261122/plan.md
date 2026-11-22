# Implementation plan

Use the current Go five layers, PostgreSQL/GORM, existing ReachSendPipeline, contact_consent_events/preferences/identity links, marketing_flow graph, reach quota, business notifications and Vue admin views. Do not introduce a second sender, order database, permission ledger or default fake provider.

## Constitution check

All repository methods require authenticated tenant context; Controller parses DTO and invokes Service; Router maps and injects only. Network calls occur outside persistence transactions. Typed errors retain outcome uncertainty through reporting and quota settlement. Migrations are additive and preserve existing records; G3 migration reserved v3.108.0 after commerce v3.107.0. No production calls, deployment, commits or pushes during partial completion. One subagent at most; main owns G3 and frontend.

## Sequence and touch points

1. Extend reach_send_pipeline.go/steps.go with conservative outcome handling, per-attempt authorization, isolated DryRun and explicit receipt evidence; add service tests first.
2. Extend contact_consent model/repository/service with scoped evidence and stricter merge/withdrawal checks; update existing consent API/UI.
3. Persist marketing_flow execution/node attempts and absolute dueAt with bounded DAG version and send outbox, extending existing content layers.
4. Extend existing audience/quota/template/lead data paths, fixed control assignment and eligibility, service-event notification, outlet policy and native reply ownership.
5. Extend attribution using authoritative paid/refund/correction records per currency.
6. Add recovery-epoch and privacy/asset verification constraints through existing security/install paths.
7. Integrate existing frontend routes/permissions/i18n/onboarding; run current checks and spec convergence.

## Verification

Use deterministic fake adapters, fake clocks and isolated PostgreSQL 127.0.0.1:8217 only. Capture red tests before implementation; verify timeout call counts, no unknown fallback/retry, revoked consent between attempts, errors and reservations, persistent restart/cancel/concurrency, control-group exclusion and currency corrections. Run go build/vet, affected package tests, architecture/layer/GORM/goroutine gates; frontend full tests/lint/build. UI uses existing authenticated browser and explicit single-element operations only. Live provider, merchant, 24-hour observation and capacity evidence remain distinct.
