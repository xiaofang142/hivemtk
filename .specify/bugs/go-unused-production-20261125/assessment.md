# Bug Assessment: Go unused lint findings

- **Slug**: go-unused-production-20261125
- **Created**: 2026-11-25
- **Source**: pasted text (`.planning/2026-11-25-workspace-commit/user-go-lint.log`)
- **Verdict**: valid
- **Severity**: low

## Report (verbatim or summarized)

The first complete `golangci-lint` run reports 39 `unused` findings in `user-server`. The lint configuration has `tests: false`, which is a production-surface guard and must remain unchanged. The requested scope excludes unrelated gofmt, errcheck, staticcheck, and ineffassign findings.

## Symptom

The Go lint gate fails because internal production helpers, types, and methods have no production callers. Several helpers are leftovers from replaced implementations; a smaller set is referenced only by tests and encodes behavior not currently exercised by the active production path.

## Reproduction

1. Run the repository Go lint command with golangci-lint 2.10.0.
2. Observe 39 findings labeled `(unused)` in the supplied complete log.

## Suspected Code Paths

- `user-server/internal/bridge/handler_http.go` — `collectHTTPRequestInfo` is a test-only wrapper; production uses `collectHTTPRequestInfoWithLimit`.
- `user-server/internal/bridge/tenant_binding.go` — legacy `resolveBridgeTenant` / `bridgeAccountTenant` have no callers; request binding uses identify-code resolution.
- `user-server/internal/content/service/marketing_flow.go` — synchronous `executeFlow` has only test callers; production uses durable execution in `marketing_flow_execution.go`.
- `user-server/internal/service/aftersale.go` — `applyRemoteWriteBack` has only test callers; current `Create` has a distinct persistence/error path.
- `user-server/internal/service/auto_reply.go` — `resolveModuleChannelAccount` is superseded by `resolveModuleGateDecision`.
- `user-server/internal/service/customer_360.go` — `resolveCustomerIDForSessions` is an unreferenced wrapper around `resolveCustomerForSessions`.
- `user-server/internal/service/customer_ai.go` — `newCustomerFieldEvidence` is only used by tests; production constructs evidence with the record's actual timestamp.
- `user-server/internal/service/customer_import_export.go` — `importJobRepo` has no implementer or consumer.
- `user-server/internal/service/customer_notification.go` — the entire delivery/outbox/privacy/recipient group is referenced only by `lead_notify_delivery_test.go`; no production wiring found.
- `user-server/internal/service/douyin_lead_miner.go` — `douyinLeadAdapterForHub` is test-only; current production dispatch uses the unified lead-discovery path.
- `user-server/internal/service/lead_channel_wiring_contract.go` — its gate is referenced only by its contract test; production dispatch has separate admission and enqueue logic.
- `user-server/internal/service/lead_intent_llm.go` — `refineLeadWithLLM` and its timeout/truncation helpers have no caller; active lead detection uses `LeadLLMDetector`.
- `user-server/internal/service/oneid_merge_preview.go` — `identityFieldValue` is unused; preview uses `customerIdentityMap`.
- `user-server/internal/service/oneid_merge_rule.go` — `matchRule` is unused; preview and merge use `evaluateRule` / `ruleScore`.
- `user-server/internal/service/reach_send_pipeline_steps.go` — `executeSendWithFallback` is an unused wrapper; active retries call `sendWithFallback`.
- `user-server/internal/service/telegram_lead_miner.go` — `isDuplicateKeyError` and `boolToInt64` have no callers; Telegram mining was replaced by unified lead discovery.

## Root Cause Hypothesis

High confidence: incremental migrations left private helpers and a repository interface behind after their call sites moved to newer paths. Some test-only code is not dead behavior in the same sense: its tests assert business guarantees that the active production path may not yet implement. The linter's production-only surface is intentionally correct and must not be weakened.

## Proposed Remediation

**Preferred**: remove only unreferenced implementation artifacts whose replacement production path is verified. For `collectHTTPRequestInfo`, preserve its existing assertions by calling the active bounded implementation directly. For customer AI evidence tests, use explicit test evidence values rather than a production helper that fabricates an epoch timestamp. Delete the unreferenced no-op interface and obsolete bridge, account, customer-360, lead-adapter, OneID, reach, and Telegram helpers. Remove the uncalled LLM refinement implementation and its private-only timeout/truncation helpers; retain the exported config seams because app wiring and tests still reference them.

Do not delete or relocate the test-only marketing-flow executor, after-sale write-back helper, lead-channel wiring contract, or customer-notification/outbox group in this pass. Their test assertions cover behavior not shown to be covered by the replacement path. Report these findings for the primary reviewer rather than introducing production wiring or moving business logic into `_test.go` merely to satisfy lint.

**Alternatives**:
- Wire the test-only notification and after-sale prototypes into production: rejected for this lint-only task because it changes business flows and exceeds the requested scope.
- Move helpers into `_test.go` or disable test linting: rejected because that masks missing production wiring or weakens the required production-surface guard.

**Files likely to change**:
- `user-server/internal/bridge/handler_http.go`
- `user-server/internal/bridge/handler_http_test.go`
- `user-server/internal/bridge/handler_http_body_limit_test.go`
- `user-server/internal/bridge/tenant_binding.go`
- `user-server/internal/content/service/marketing_flow.go` (not changed pending reviewer decision)
- `user-server/internal/service/aftersale.go` (not changed pending reviewer decision)
- `user-server/internal/service/auto_reply.go`
- `user-server/internal/service/customer_360.go`
- `user-server/internal/service/customer_ai.go`
- `user-server/internal/service/customer_ai_evidence_test.go`
- `user-server/internal/service/customer_import_export.go`
- `user-server/internal/service/customer_notification.go` (not changed; reviewer decision required)
- `user-server/internal/service/douyin_lead_miner.go`
- `user-server/internal/service/lead_channel_wiring_contract.go` (not changed pending reviewer decision)
- `user-server/internal/service/lead_intent_llm.go`
- `user-server/internal/service/oneid_merge_preview.go`
- `user-server/internal/service/oneid_merge_rule.go`
- `user-server/internal/service/reach_send_pipeline_steps.go`
- `user-server/internal/service/telegram_lead_miner.go`
- `.specify/bugs/go-unused-production-20261125/assessment.md`
- `.specify/bugs/go-unused-production-20261125/fix.md`
- `.specify/bugs/go-unused-production-20261125/test.md`

**Tests to add or update**:
- Keep bridge request-info tests but call `collectHTTPRequestInfoWithLimit` directly.
- Keep customer AI evidence resolution tests using explicit `AIEvidence` fixtures.
- Run focused bridge, service, and content-service tests that do not require database setup; compile affected packages.

## Risks & Considerations

- The notification/outbox tests currently use only an in-memory store; the “survives rebuild” test shares the same map and does not prove process-restart durability.
- The after-sale helper tests assert persistence-failure rollback, but the production `Create` path does not call the helper and currently returns a persistence error without restoring the in-memory status.
- The marketing-flow tests use the old synchronous executor; durable execution tests cover persistence/replay but do not clearly cover every old send-gate scenario.
- `lead_channel_wiring_contract` is a test-only gate. Its relationship to actual inbound admission should be reviewed before removing it.
- Concurrent work has staged and unstaged changes in several target files. Preserve all unrelated hunks and do not stage or commit.

## Open Questions

- Primary reviewer decision: whether to leave the four test-only business behavior groups in place and accept these remaining unused findings, or authorize a separate behavior implementation/removal task.
