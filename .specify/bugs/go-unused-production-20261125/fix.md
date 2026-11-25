# Bug Fix: Remove obsolete Go production helpers

- **Slug**: go-unused-production-20261125
- **Fixed**: 2026-11-25
- **Assessment**: ./assessment.md
- **Status**: integrated; final gates pending

## Summary

Removed 16 of the original 39 unused declarations after checking production and test references. The primary reviewer retained the remaining business prototypes with precise `//nolint:unused` explanations because this task consolidates all existing workspace work. The production-only lint configuration is unchanged. Production integration of these prototypes is not claimed.

## Changes

Paths below are relative to `user-server/`.

| File | Change | Notes |
|------|--------|-------|
| `internal/bridge/handler_http.go` | removed wrapper | `collectHTTPRequestInfo` had only test callers; production calls the bounded implementation directly. |
| `internal/bridge/handler_http_test.go` | updated tests | Existing URL, preview, and body rebinding assertions now call `collectHTTPRequestInfoWithLimit`. |
| `internal/bridge/handler_http_body_limit_test.go` | updated test | The bounded-reader probe calls the actual production helper. |
| `internal/bridge/tenant_binding.go` | removed two functions/import | `resolveBridgeTenant` and `bridgeAccountTenant` had no callers; identity resolution uses `resolveBridgeIdentity`. Removed their private-only `dbstate` import. |
| `internal/service/auto_reply.go` | removed helper; updated comment | `resolveModuleChannelAccount` is replaced by `resolveModuleGateDecision`; the comment describes the active resolver's error behavior. |
| `internal/service/customer_360.go` | removed wrapper | Callers already use `resolveCustomerForSessions`; the ID-only wrapper had no callers. |
| `internal/service/customer_ai.go` | removed helper only | `newCustomerFieldEvidence` was test-only and fabricated an epoch timestamp; retained the primary agent's title fix. |
| `internal/service/customer_ai_evidence_test.go` | updated fixtures | Explicit `AIEvidence` values retain source/time, forged-reference, degradation, and score assertions. |
| `internal/service/customer_import_export.go` | removed interface only | `importJobRepo` had no consumer; retained the primary agent's XLSX error handling. |
| `internal/service/douyin_lead_miner.go` | removed helper | `douyinLeadAdapterForHub` served an obsolete miner path; unified discovery preserves `hub.Platform` independently. |
| `internal/service/webhook_batchc_tiktok_test.go` | removed obsolete-helper test | Retained factory/platform separation, inbound dispatch, enqueue, and outbound adapter coverage. |
| `internal/service/lead_intent_llm.go` | removed dead implementation/imports; updated header | Removed `llmRefineTimeout`, `refineLeadWithLLM`, `truncateForPrompt`; retained defaults, providers, setters, and app wiring probes. |
| `internal/service/oneid_merge_preview.go` | removed helper | `identityFieldValue` is superseded by `customerIdentityMap`. |
| `internal/service/oneid_merge_rule.go` | removed helper | `matchRule` is replaced by `evaluateRule` / `ruleScore`; retained `containsSubstr`, which still has callers. |
| `internal/service/reach_send_pipeline_steps.go` | removed wrapper | `executeSendWithFallback` had no callers; production invokes `sendWithFallback` directly. |
| `internal/service/telegram_lead_miner.go` | removed two helpers | `isDuplicateKeyError` / `boolToInt64` had no callers after unified mining migration. |

## Tests Added or Updated

- Existing bridge request-info tests still assert URL/query/header capture, truncation, and JSON rebinding against the bounded production implementation.
- Existing body-limit reader probe still rejects unbounded reads.
- Existing customer AI evidence tests use explicit source/time fixtures; no business implementation was moved into tests.
- Removed `TestDouyinLeadAdapterForHub_SelectsByHubPlatform`, which only exercised the unused legacy factory. Its platform-isolation intent is covered by retained `TestLeadAdapterForTikTokIsNotDouyin`, `TestDispatchDouyin_LeadRowKeepsPlatform`, and `TestDispatchToChannel_DouyinFamilyKeepsChannel`. The latter two require DB setup and were inspected but not run in this subtask.

## Local Verification

- `go test ./internal/bridge -run 'TestHTTPIngestDefaults|TestCollectHTTPRequestInfo|TestHTTPIngestRequest_JsonBind'` → pass (`0.903s`).
- `go test ./internal/bridge -run '^TestHTTPIngestHandler_BodyLimitBeforeIdentity$/^logging-preview-read-is-bounded$'` → pass (`0.915s`).
- `go test ./internal/service -run 'TestResolveAISuggestionsEvidence_.*|TestLeadAdapterForTikTokIsNotDouyin'` → pass (`1.061s`).
- `go test ./internal/content/service -run '^$'` → pass, compiled tests without running DB tests (`0.502s`).
- `go build ./...` → blocked by concurrent `internal/controller/followup_record.go:5:2: "fmt" imported and not used`.
- Full-scope unused lint → blocked by the same controller typecheck error.
- Scoped unused lint over bridge, content service, and service packages → 25 findings: the 23 retained original findings plus 2 new findings in concurrent `lead_archive_consistency.go`; all 16 removed declarations disappeared.
- `git diff --check` restricted to this subtask's Go files → pass.
- Required graphify rebuild → unavailable: `ModuleNotFoundError: No module named 'graphify'`.

## Deviations from Assessment

- Also edited the existing `webhook_batchc_tiktok_test.go` (omitted from the assessment's file list) to remove the obsolete helper's sole test. The retained tests cover the current factory and actual dispatch/enqueue platform isolation; no replacement business logic was added to tests.
- Scoped lint found two additional unused functions, `leadArchiveAllowed` and `leadArchiveTimestamp`, in concurrent `lead_archive_consistency.go`. They were absent from the original 39-item log and remain outside the delegated file scope.
- Concurrent formatting changes exist in touched files. This subtask does not claim ownership of unrelated formatting, title handling, XLSX error handling, staged changes, or other agents' edits.

## Delegated Review Findings

These are primary-reviewer decisions within the coordinated task, not a claim that the lint gate is clear. No staging, commits, pushes, lint configuration changes, or new `nolint` directives were made here.

| Retained group | Original findings | Evidence | Recommendation to primary reviewer |
|----------------|-------------------|----------|------------------------------------|
| `internal/content/service/marketing_flow.go` | 1 | `executeFlow` is only called by graph and send-gate tests; production `TriggerFlow` uses `executeDurableFlow`. Durable tests do not demonstrate all pause/panic/cancel assertions of old tests. | Preserve until the old behavioral assertions are covered against durable execution; then remove the old executor. Do not move it into tests. |
| `internal/service/aftersale.go` | 1 | `applyRemoteWriteBack` is only called by tests; its rollback on persistence failure differs from `Create`, which returns an error with the updated in-memory view. | Preserve until the primary reviewer chooses the intended persistence-failure contract and tests it through `Create`; do not wire this void helper into `Create` and swallow its current error. |
| `internal/service/customer_notification.go` | 20 | Only `lead_notify_delivery_test.go` calls the outbox/history/privacy/recipient group. `memoryLeadNotifyOutbox` has no persistent adapter or worker. The restart test shares a map between instances; it does not prove process durability. | Preserve during this cleanup. Recommend a separate production integration task covering repository-backed atomic claims, worker dispatch, tenant/recipient authorization, redaction, receipts, and restart recovery; do not delete the file wholesale or present these tests as delivered production behavior. |
| `internal/service/lead_channel_wiring_contract.go` | 1 | `leadChannelWiringAllowsMining` is only called by its contract test, while actual admission/discovery/enqueue has distinct checks. | Preserve until the behavioral matrix is verified on the current production entry; remove the orphan only after those assertions are retained there. |

The original 39-item block is therefore reduced by 16 declarations, with 23 intentionally retained under the delegated requirement to escalate missing production behavior. The primary agent owns the remaining decision and full-workspace verification.

## Primary Integration Decision

- Preserve the 23 original test-only declarations and the two new archive predicates. Each retained declaration has a scoped `//nolint:unused` reason naming the missing production integration; no linter configuration or check was disabled.
- Preserve existing legacy marketing and after-sale behavioral assertions without changing `TriggerFlow` or `Create` contracts during this consolidation.
- Correct notification comments: the outbox is an in-memory prototype, and rebuilding an instance over a shared map does not prove process-restart persistence. There is no production worker or persistent adapter for this group.
- Remove the unrelated unused `fmt` import in `internal/controller/followup_record.go` and format the integrated files before final server checks.
- Full build, vet, tests, audit, and lint evidence is recorded in the final verification report when available. The local verification rows above describe the earlier delegated stage.
