# Bug Verification: Service GORM handle leaks and transaction-scanner false positive

- **Slug**: service-gorm-handle-remediation-20261125
- **Tested**: 2026-11-25T02:34:00+08:00
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

The original I-02 symptom no longer reproduces: both architecture guards pass, the Service scan is `handles 0 / transactions 3 / GetDB 0`, and the affected Service/ops tests pass. The overall result is `partial` only because the broader channelgw suite still fails in unrelated concurrent work (CS06 pipeline timeouts, CS02 tenant ambiguity, and outbound-registry source-seam assertions).

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (post-fix) | `go test ./internal/service -run 'TestArchServiceNoNewGormDB|TestTxCallbacksAreInTransactionPosition' -count=1 -v` | pass | Both PASS; `句柄 0 / 事务 3 / GetDB 0`. |
| Guard teeth | `go test ./internal/service -run 'TestGormGuardMetricHasTeeth|TestChannelDispatchService_WithReposUsesInjectedDependencies|TestChannelDispatchService_NilDatabaseRepositoriesFailClosed|TestLeadMiningAgentService_UnavailableStoreFailsClosed|TestBusinessAccountScope_CustomerSourceAccountClause' -count=1 -v` | pass | Injection, nil semantics, scanner teeth, and pure scope clause all PASS. |
| Affected Service regression | `go test ./internal/service -run 'TestChannelDispatchService_|TestEditionChannelAccountAndDispatchMatrix|TestLeadMiningAgentService_|TestProactiveReachService_PickChannel_|TestProactiveReachDryRun_UsesActualAccount|TestDeliverBridgeOutbound_DirectToOutbox|TestDeliverBridgeOutbound_NotReady|TestNewWebhookService_WithoutDB_KeepsNilSemantics|TestWebhookInbound_CapabilityMatchesDispatchSwitch|TestBridgeOutboxParityWithWebhookOutbound' -count=1` | pass | `ok hivemtk-user/internal/service 1.569s`; DB password sourced silently from `../.env`. |
| Ops regression | `go test ./internal/ops/service -count=1` | pass | `ok hivemtk-user/internal/ops/service 9.937s`. |
| Build | `go build ./...` | pass | Exit 0. |
| Vet | `go vet ./...` | pass | Exit 0. |
| Layer boundary | `python3 scripts/check-layer-boundary.py` | pass | `跨层违规：命中 0 处；基线豁免 0；新增 0`. |
| Formatting | `gofmt -l` on all touched Go files | pass | No output. |
| Diff whitespace | `git diff --check` | pass | Exit 0. |
| Package-scoped lint | `golangci-lint run ./internal/service ./internal/router ./internal/ops/service` | fail (unrelated) | 42 pre-existing issues in unrelated files: 22 gofmt, 3 errcheck, 4 staticcheck, 13 unused. None in I-02 files. |
| Task-diff-scoped lint | `golangci-lint run --new-from-patch=<task-only patch> ./internal/service ./internal/router ./internal/ops/service` | pass | `0 issues`. |
| Channelgw full regression | `go test ./internal/channelgw/... -count=1` | fail (unrelated) | Three CS06 timeouts, `TestBridgeOutboxParityWithWebhookOutbound`, and one CS02 tenant-ambiguity assertion; reproduced when channelgw ran alone. |
| Preservation | `shasum -a 256 router-list.md`; `git ls-files -s` for staged blobs | pass | `router-list.md` hash unchanged at `6e0e83a2…b8484cb6`; staged `channel_dispatch.go` blob unchanged at `9d3b9d227…53591`; `router.go` not edited. |

## Output Excerpts

```text
=== RUN   TestArchServiceNoNewGormDB
    arch_gorm_guard_test.go:698: 扫描完成 757 个文件；快照登记 2 个；三档合计 句柄 0 / 事务 3 / GetDB 0
--- PASS: TestArchServiceNoNewGormDB (0.26s)
=== RUN   TestTxCallbacksAreInTransactionPosition
--- PASS: TestTxCallbacksAreInTransactionPosition (0.00s)
PASS
ok  hivemtk-user/internal/service  1.347s
```

```text
ok  hivemtk-user/internal/service  1.569s
ok  hivemtk-user/internal/ops/service  9.937s
0 issues.
```

Unrelated channelgw failures:

```text
--- FAIL: TestCS06_PipelineSampleA_KnowledgeHit_FullChainWithTraceReconciliation
    等待超时: 样本① AI 腿出站落库
--- FAIL: TestCS06_PipelineSampleB_LowConfidence_HandoffNoticeNotModelReply
    等待超时: 样本② 转接公告落库
--- FAIL: TestCS06_PipelineReplayAndSendFailure_Negatives
    等待超时: 失败负例出站行落库
--- FAIL: TestBridgeOutboxParityWithWebhookOutbound
    webhook_outbound.go:602 不应再直接做桥接能力查询
--- FAIL: TestCS02_BridgeRouterAuth_ValidToken_MaliciousExtraAccountIDDropped
    合法 token 应 200，实际 404
```

## Residual Risks

- I-02 itself is closed by direct reproduction and guard evidence, but the shared worktree is not globally green because of the unrelated channelgw failures and 42 pre-existing package-lint issues.
- No commit, push, deploy, or staging was performed. Concurrent staged/untracked work remains intact.
- `trace_learning/service.go` was deliberately not refactored; its single `Connection` callback remains the root-domain transaction exception.

## Recommendation

Close inventory finding I-02: its three real handle defects are removed and the scanner false positive is corrected without weakening the guard. Do not claim the full worktree regression suite is green until the owning concurrent lanes resolve the channelgw and package-lint failures.
