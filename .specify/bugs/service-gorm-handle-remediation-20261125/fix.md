# Bug Fix: Service GORM handle leaks and transaction-scanner false positive

- **Slug**: service-gorm-handle-remediation-20261125
- **Fixed**: 2026-11-25T02:32:48+08:00
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

Removed the three real Service-layer GORM handle defects, moved LeadMiningAgent repository assembly to the Router, and corrected the architecture scanner to recognize repository `WithTransaction` callbacks without weakening handle/GetDB checks. Preserved the existing `trace_learning` same-connection advisory-lock exception and all concurrent work in the shared files.

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/service/channel_dispatch.go` | modified | Replaced the DB constructor with a no-argument default constructor and `NewChannelDispatchServiceWithRepos`; added the narrow `ChannelDispatchConversationReader` port. Kept `gorm.ErrRecordNotFound` handling. |
| `user-server/internal/service/bridge_outbound.go` | modified | Auto-dispatch now uses the default repository-backed constructor. |
| `user-server/internal/service/proactive_reach.go` | modified | Auto-dispatch now uses the default constructor; removed the now-unused `dbstate` import. |
| `user-server/internal/service/webhook_outbound.go` | modified | Auto-dispatch now uses the default constructor. |
| `user-server/internal/service/channel_dispatch_query_errors_test.go` | modified | DB tests now inject the three repositories through a shared test helper; preserved all concurrent query-error cases. |
| `user-server/internal/service/edition_channel_account_matrix_test.go` | modified | Uses the same repository-injection test helper; edition behavior unchanged. |
| `user-server/internal/service/channel_dispatch_dependencies_test.go` | added | Pins injected sticky selection and nil-database fail-closed behavior without a DB. |
| `user-server/internal/service/lead_mining_agent.go` | modified | Deleted `NewLeadMiningAgentService(*gorm.DB)` and the GORM import; retained repository injection and nil-repo guards. |
| `user-server/internal/service/lead_mining_agent_test.go` | added | Pins List/Get/Create/Update/Delete fail-closed behavior for nil repo and nil receiver. |
| `user-server/internal/router/lead_mining_agent_routes.go` | modified | Router now assembles `NewAIAgentRepositoryWithDB(gormDB)` and passes it to the Service. |
| `user-server/internal/service/business_scope.go` | modified | Deleted dead `ApplyCustomerSourceAccountScope` and the unused GORM import; kept pure `CustomerSourceAccountClause`. |
| `user-server/internal/service/arch_gorm_guard_test.go` | modified | Added exact-name `WithTransaction` sanction, fourth positive fixture, and ops `{tx: 2}` ratchet entry with rationale. |
| `docs/architecture/FULL_FEATURE_INVENTORY.md` | modified | Rewrote I-02 to distinguish three fixed handle defects, one allowed `Connection` exception, and the scanner false positive. |
| `.specify/bugs/service-gorm-handle-remediation-20261125/{assessment,fix,test}.md` | added | Spec-kit BUG lane artifacts. |

## Diff Highlights

```go
func NewChannelDispatchService() *ChannelDispatchService {
    return NewChannelDispatchServiceWithRepos(
        repository.NewChannelRepository(),
        repository.NewInboxConversationRepository(),
        repository.NewChannelAccountRepository(),
    )
}
```

```go
switch se.Sel.Name {
case "Transaction", "Connection", "WithTransaction":
default:
    return true
}
```

```go
"user-server/internal/ops/service/ab_experiment.go": {tx: 2},
```

## Tests Added or Updated

- `TestChannelDispatchService_WithReposUsesInjectedDependencies` — proves the Service uses injected repository ports and preserves sticky selection.
- `TestChannelDispatchService_NilDatabaseRepositoriesFailClosed` — preserves the old nil-DB fail-closed outcome using repository injection.
- `TestChannelDispatchService_ChannelQueryErrorIsNotSwallowed`, `ConversationQueryErrorIsNotSwallowed`, `StickyAccountQueryErrorIsNotSwallowed`, `PreferredAccountQueryErrorIsNotSwallowed`, `RoundRobinListErrorIsNotSwallowed`, `SelectionMatrix` — existing concurrent tests updated to the new constructor.
- `TestEditionChannelAccountAndDispatchMatrix` — existing edition matrix updated to the new constructor.
- `TestLeadMiningAgentService_UnavailableStoreFailsClosed` — covers nil repo and nil receiver for all five CRUD/list methods.
- `TestTxCallbacksAreInTransactionPosition` — positive fixture now expects `0/4/0`; negative fixture remains `6/0/0`.
- `TestArchServiceNoNewGormDB` — now measures `handles 0 / transactions 3 / GetDB 0` with two ratchet entries.

## Local Verification

- RED baseline: `go test ./internal/service -run 'TestArchServiceNoNewGormDB|TestTxCallbacksAreInTransactionPosition' -count=1 -v` → exit 1; `TestArchServiceNoNewGormDB` reported four unregistered files and `6/1/0`.
- RED scanner fixture: `go test internal/service/arch_gorm_guard_test.go -run '^TestTxCallbacksAreInTransactionPosition$' -count=1 -v` → exit 1; measured `1/3/0`, expected `0/4/0`.
- RED injection API: `go test ./internal/service -run '^TestChannelDispatchService_WithReposUsesInjectedDependencies$' -count=1 -v` → build failure on undefined `NewChannelDispatchServiceWithRepos`.
- GREEN guards/unit tests: `go test ./internal/service -run 'TestArchServiceNoNewGormDB|TestTxCallbacksAreInTransactionPosition|TestGormGuardMetricHasTeeth|TestChannelDispatchService_WithReposUsesInjectedDependencies|TestChannelDispatchService_NilDatabaseRepositoriesFailClosed|TestLeadMiningAgentService_UnavailableStoreFailsClosed|TestBusinessAccountScope_CustomerSourceAccountClause' -count=1 -v` → PASS; guard log `句柄 0 / 事务 3 / GetDB 0`.
- GREEN affected service tests (DB password sourced silently from `../.env`): ChannelDispatch, edition matrix, LeadMiningAgent, proactive pick/dry-run, bridge outbound, webhook nil/capability tests → `ok hivemtk-user/internal/service 1.321s`.
- GREEN ops: `go test ./internal/ops/service -count=1` → `ok hivemtk-user/internal/ops/service 8.496s`.
- `go build ./...` → exit 0.
- `go vet ./...` → exit 0.
- `python3 scripts/check-layer-boundary.py` → `跨层违规：命中 0 处；基线豁免 0；新增 0`.
- `gofmt -l` on all touched Go files → no output.
- `git diff --check` → exit 0.
- `golangci-lint run ./internal/service ./internal/router ./internal/ops/service` → exit 1 with 42 pre-existing issues in unrelated files (22 gofmt, 3 errcheck, 4 staticcheck, 13 unused); none in I-02 files.
- `golangci-lint run --new-from-patch=<task-only patch> ./internal/service ./internal/router ./internal/ops/service` → `0 issues`.
- `go test ./internal/channelgw/... -count=1` → exit 1 on unrelated concurrent work: three `CS06` 10-second pipeline timeouts, `TestBridgeOutboxParityWithWebhookOutbound` source-seam assertions at `webhook_outbound.go:602/635`, and `TestCS02_BridgeRouterAuth_ValidToken_MaliciousExtraAccountIDDropped` tenant ambiguity. The same failures reproduce when channelgw runs alone.
- Manual preservation checks: staged `channel_dispatch.go` blob remains `9d3b9d227483d2f092724aac4137c60b03053591`; `router-list.md` SHA-256 remains `6e0e83a21a41268d7dd448e492678cee559fa9e0b694b2e804ca6cc4b8484cb6`; `router.go` was not edited.
- No commit, push, stage, reset, stash, or deploy was performed.

## Deviations from Assessment

None. The implementation follows the assessed constructor/port/scanner/ratchet plan. The full channelgw failures are unrelated concurrent-work regressions and were not changed, per the task boundary.

## Follow-ups

- The owning concurrent lanes should resolve the channelgw `CS06`, `CS02`, and outbound-registry parity failures.
- No I-02 follow-up remains; the root handle count is zero and the only remaining callbacks are the sanctioned root `Connection` and ops `WithTransaction` seams.
