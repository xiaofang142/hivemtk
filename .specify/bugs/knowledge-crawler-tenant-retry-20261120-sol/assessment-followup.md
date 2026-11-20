# FA-12 follow-up assessment (read-only source investigation)

2026-11-20; verdict valid, severity high. Original assessment is preserved.

The initial assessment correctly identified status-write failures, but its QA assumption was incomplete. `kb_crawler.go:crawlOne` records hash and success counters before `qa.EnqueueDocument`; `kb_qa_split.go:68-89` queues only doc/KB IDs and drains under `context.Background()`, with errors only logged. Thus TENANT_MODE=on can reject QA reads and retries are suppressed. `QASplitService.ProcessDocument` already returns errors and only returns nil after candidate persistence (or valid empty QA output); this is an existing, concrete terminal contract to reuse synchronously in the crawler.

`IngestCrawledPage` also treats arbitrary document lookup errors as missing records, and its background context retains a possible SystemScope exemption. Preserve source tenant at every repository boundary, strip SystemScope, reject mismatched explicit source/request tenants, fail closed for missing tenant in multi-tenant mode, and propagate actual lookup failures without creating duplicates. Keep single-tenant zero-value fixture compatibility via explicit default tenant.

New files/changes permitted by this follow-up: `kb_crawler.go`, `knowledge_base_import.go`, and same-package FA-12 regression tests; `knowledge_base.go` can be restored to its original concrete repository shape if controlled GORM callbacks provide sufficient testability. No repository/schema/SQL changes are required. Test fixtures use GORM's PostgreSQL dialect with `DisableAutomaticPing` and callbacks replacing query/create/update, so no connection or destructive helper executes.

Tests before implementation: status write failures on baseline import code; tenant source/system-scope isolation; same-hash retry after vector/QA/update failures; lookup errors; fresh service over the same controlled durable state; batch start-write error and partially-failed retry. Only fake-backed durability is claimed; no live PostgreSQL or real process crash evidence is fabricated.
