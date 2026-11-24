# Bug Assessment: Service GORM handle leaks and transaction-scanner false positive

- **Slug**: service-gorm-handle-remediation-20261125
- **Created**: 2026-11-25T02:23:52+08:00
- **Source**: pasted text — inventory finding I-02 remediation request from the parent session
- **Verdict**: valid
- **Severity**: medium

## Report (verbatim or summarized)

The parent session reported that `go test ./internal/service -run 'TestArchServiceNoNewGormDB|TestTxCallbacksAreInTransactionPosition' -count=1 -v` fails `TestArchServiceNoNewGormDB` with four unregistered files, six handles, one transaction callback, and zero `GetDB` calls. It supplied the diagnosis and required remediation: remove three real Service-layer GORM handle defects, recognize repository `WithTransaction` callbacks, register the two legitimate ops callbacks in the ratchet, preserve the existing `trace_learning` `Connection` exception, update inventory I-02, and do not commit, push, deploy, touch MM-owned `router.go`, or regenerate concurrently edited `router-list.md`.

## Symptom

The architecture guard rejects four Service files. Two constructors and one dead exported query helper really do accept/return `*gorm.DB`; the ops A/B service's two callbacks are legitimate repository transaction seams but are misclassified as handles. The expected end state is zero Service handles, three sanctioned transaction callbacks (one root `Connection` plus two ops `WithTransaction`), and zero `GetDB` calls.

## Reproduction

From `hivemtk-user/user-server`:

```bash
go test ./internal/service -run 'TestArchServiceNoNewGormDB|TestTxCallbacksAreInTransactionPosition' -count=1 -v
```

Observed on 2026-11-25 at 02:23:52 +08:00:

```text
扫描完成 757 个文件；快照登记 1 个；三档合计 句柄 6 / 事务 1 / GetDB 0
service 层 gorm 守卫棘轮被触发（4 条）：
  新增: user-server/internal/ops/service/ab_experiment.go 有 句柄 2 / 事务 0 / GetDB 0
  新增: user-server/internal/service/business_scope.go 有 句柄 2 / 事务 0 / GetDB 0
  新增: user-server/internal/service/channel_dispatch.go 有 句柄 1 / 事务 0 / GetDB 0
  新增: user-server/internal/service/lead_mining_agent.go 有 句柄 1 / 事务 0 / GetDB 0
--- FAIL: TestArchServiceNoNewGormDB
--- PASS: TestTxCallbacksAreInTransactionPosition
```

## Suspected Code Paths

- `user-server/internal/service/channel_dispatch.go:27` — `NewChannelDispatchService(db *gorm.DB)` constructs three repositories; this is a real handle leak.
- `user-server/internal/service/bridge_outbound.go:113`, `proactive_reach.go:671`, `webhook_outbound.go:619` — production callers pass `dbstate.Get()` into the leaking constructor.
- `user-server/internal/service/edition_channel_account_matrix_test.go:57` and `channel_dispatch_query_errors_test.go:61,91,113,170` — tests use the same constructor and must move to repository injection.
- `user-server/internal/service/lead_mining_agent.go:28` — `NewLeadMiningAgentService(db *gorm.DB)` constructs `AIAgentRepository`; this is a real handle leak.
- `user-server/internal/router/lead_mining_agent_routes.go:15` — the only production caller; Router is the correct assembly point for `NewLeadMiningAgentServiceWithRepo(repository.NewAIAgentRepositoryWithDB(gormDB))`.
- `user-server/internal/service/business_scope.go:152` — exported `ApplyCustomerSourceAccountScope(q *gorm.DB, scope)` has no production or test callers; its parameter and return value account for two handles. `CustomerSourceAccountClause` is the live pure API.
- `user-server/internal/ops/service/ab_experiment.go:74,132` — two `func(tx *gorm.DB)` literals are passed directly to `ABExperimentRepository.WithTransaction`; they are transaction callbacks, not held handles.
- `user-server/internal/service/arch_gorm_guard_test.go:286-307` — the scanner sanctions only `.Transaction(...)` and `.Connection(...)`, so it misses the repository's standard `WithTransaction` seam.
- `user-server/internal/service/trace_learning/service.go:168` — existing root-domain `Connection` callback holds one PostgreSQL connection for advisory-lock acquire/release; this is the sanctioned `gormGuardRootMaxTxCallbacks = 1` exception and must remain byte-for-byte unchanged.

## Root Cause Hypothesis

High confidence: Service constructors were allowed to take a database handle and assemble repositories internally, while the architecture guard's transaction-position allowlist was narrower than the repository transaction API already used by the codebase. The dead business-scope helper is leftover query mutation in the Service layer. `gorm.ErrRecordNotFound` is an error sentinel, not a database handle, and must remain in `channel_dispatch.go`.

## Proposed Remediation

**Preferred**:

1. Replace the ChannelDispatch DB constructor with a no-argument default constructor plus `NewChannelDispatchServiceWithRepos(...)`. Define a narrow consumer-side conversation-reader interface so the Service depends only on `FindLatestByPlatformAndPeer`. Keep channel/account repository interfaces and the existing selection/error behavior. Update the three production callers to the default constructor; because the repository default constructors resolve the same `dbstate` global, nil/default behavior is preserved without a handle in the Service signature.
2. Delete `NewLeadMiningAgentService(db *gorm.DB)` and its GORM import. Assemble the existing repository in `lead_mining_agent_routes.go` and pass it to `NewLeadMiningAgentServiceWithRepo`. Preserve the nil-repository fail-closed behavior and add direct tests.
3. Delete the dead `ApplyCustomerSourceAccountScope` helper and the now-unused GORM import. Keep `CustomerSourceAccountClause` and its tests unchanged.
4. Add exact-name `WithTransaction` sanction to the AST scanner, extend the positive transaction-position fixture from three to four callbacks, and register `user-server/internal/ops/service/ab_experiment.go` as `{tx: 2}` with a rationale comment. Do not change handle/GetDB checks or the root transaction maximum.
5. Update inventory I-02 to distinguish the three fixed handle defects, the allowed `Connection` exception, and the corrected `WithTransaction` scanner false positive.

**Alternatives**:

- Keep a compatibility DB constructor: rejected because it preserves the exact architecture violation this finding requires removing.
- Move ChannelDispatch assembly into `router.go`: rejected for this task because `router.go` is MM-owned and the live callers are deep Service paths; the no-argument default plus explicit repository constructor follows existing `ChannelResolver`/`ChannelAccountService` patterns.

**Files likely to change**:

- `user-server/internal/service/channel_dispatch.go`
- `user-server/internal/service/bridge_outbound.go`
- `user-server/internal/service/proactive_reach.go`
- `user-server/internal/service/webhook_outbound.go`
- `user-server/internal/service/channel_dispatch_query_errors_test.go`
- `user-server/internal/service/edition_channel_account_matrix_test.go`
- `user-server/internal/service/channel_dispatch_dependencies_test.go` (new)
- `user-server/internal/service/lead_mining_agent.go`
- `user-server/internal/service/lead_mining_agent_test.go` (new)
- `user-server/internal/router/lead_mining_agent_routes.go`
- `user-server/internal/service/business_scope.go`
- `user-server/internal/service/arch_gorm_guard_test.go`
- `docs/architecture/FULL_FEATURE_INVENTORY.md` (workspace root)
- `.specify/bugs/service-gorm-handle-remediation-20261125/{assessment,fix,test}.md`

**Tests to add or update**:

- RED: a ChannelDispatch dependency-injection test that uses injected repositories without a database and pins nil-database fail-closed behavior.
- RED: extend `TestTxCallbacksAreInTransactionPosition` with a repository `WithTransaction` positive fixture and expect four transaction callbacks, zero handles.
- Update existing ChannelDispatch DB tests to assemble repositories explicitly.
- Add LeadMiningAgent nil-repository tests for List/Get/Create/Update/Delete and nil receiver behavior.
- Re-run the architecture guards and require handles 0 / transactions 3 / GetDB 0, with the root domain still handles 0 / transactions 1.

## Risks & Considerations

- Constructor signature changes can break callers; repository-wide symbol search found only the listed production/test callers.
- Global-default assembly must preserve the old explicit `dbstate.Get()` semantics; the repository default constructors all resolve that same global pointer.
- The ops ratchet entry must remain transaction-only. Broadening the scanner to arbitrary callback method names or weakening handle/GetDB checks would hide future defects.
- `channel_dispatch.go` and its two tests already contain concurrent staged/untracked work. Only targeted hunks may be changed; none of that work may be discarded.
- `router.go` is MM-owned; `router-list.md` has concurrent edits. Neither may be touched.
- No database migration, API contract change, external network call, or deployment is required.

## Open Questions

None. The parent session supplied the diagnosis, required API direction, and verification scope.
