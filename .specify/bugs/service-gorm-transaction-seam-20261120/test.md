# Bug Verification: Knowledge document deletion transaction boundary

- **Slug**: service-gorm-transaction-seam-20261120
- **Tested**: 2026-11-21
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: verified

## Summary

The original architecture failure no longer reproduces. Document lookup and atomic chunk/document deletion now reside in the repository; Service retains post-commit file/index cleanup. The complete root service package and knowledge packages passed, including the new PostgreSQL rollback, context and tenant-isolation regressions. Existing environment-dependent skips are listed below and are not counted as passes.

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Original reproduction | `go test ./internal/service -run '^TestArchServiceNoNewGormDB$' -count=1` before source edits | fail as expected | 1.055s; knowledge file had 2 transaction callbacks against allowance 1; 10 domains / 692 production files scanned. |
| Service delegation regression before fix | `TestTxSeam_DeleteDocumentUsesRepositoryOperation` | fail as expected | Both branches reached the old Transaction fake instead of the repository domain operation. |
| New / updated tests | `go test ./internal/aiagent/knowledge/service -run '^TestTxSeam_' -count=1 -v` | pass | 1.169s; no skips. Cascade, typed NotFound, file/index cleanup, chunk- and document-delete rollback, caller context, foreign/missing tenant rejection and cancellation all exercised. |
| Architecture reproduction after fix | `go test ./internal/service -run '^(TestArchServiceNoNewGormDB|TestGormGuardMetricHasTeeth|TestTxCallbacksAreInTransactionPosition)$' -count=1 -v` | pass | 1.020s; same 692 files. Overall callbacks 3 → 1, knowledge callbacks 2 → 0, registered exceptions 2 → 1. Removed the cleared allowance; scanner, assertions and other limits unchanged. |
| Knowledge regression suite | `go test ./internal/aiagent/knowledge/... -count=1 -p 1 -timeout 15m -json` | pass | Exit 0; 274 passed test/subtest results, 0 failed, 6 environment-dependent skips. Controller 126.706s, crawler 1.345s, repository 8.891s, service 38.238s. Model and kbsearch packages contain no tests. |
| Complete root service suite | `go test ./internal/service -count=1 -p 1 -timeout 30m -json` | pass | Exit 0; 264.160s; 6225 passed test/subtest results, 0 failed, 5 existing environment/fixture-dependent skips. No command-line skip/filter; architecture and all six FA-CHANNEL-04 admission/recovery cases actually passed. |
| Build / vet | `go build ./...`; `go vet ./...` from user-server | pass | Both exit 0. |
| Lint | `make lint` from app root | pass | golangci-lint 2.10.0: `0 issues.` |
| Layer boundary | `python3 scripts/check-layer-boundary.py` | pass | 1604 production Go files; 0 violations, 0 exemptions, 0 new violations. |
| Formatting / whitespace | `gofmt -l` on changed Go files and webhook regression; `git diff --check` | pass | No formatting or whitespace errors. |
| Graph refresh | Required `graphify.watch._rebuild_code` Python command | not-run | Import fails with `ModuleNotFoundError: No module named 'graphify'`; no package/environment installation attempted. |

## Output Excerpts

- Root service package: `Action=pass`, `Elapsed=264.16`, `exit_code=0`; `TestArchServiceNoNewGormDB`, `TestGormGuardMetricHasTeeth` and `TestTxCallbacksAreInTransactionPosition` all passed.
- `TestWebhookReceive_ProcessRestartRecovery` and the five other FA-CHANNEL-04 admission/recovery tests passed in the complete root run.
- Knowledge controller, crawler, repository and service package results all `Action=pass`; every `TestTxSeam_` case and subcase passed.
- `make lint`: `0 issues.`
- Regression JSON, skip reasons and summary: `/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/langbot-gorm-20261121-n147sjeo/{knowledge.jsonl,root-service.jsonl,summary.json}`.
- Build, vet, lint, boundary and diff logs: `/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/langbot-static-20261121-hv6bhgjg/`.

## Residual Risks

- Six knowledge tests skipped because the real inference stack at 127.0.0.1:8208 is unavailable: `TestKM_Playground_WithData`, `TestKBImport_PlainTextStillIndexes`, `TestKBImport_QASplitEnqueuedOnlyWhenRequested`, `TestKBImport_QASplitRequestedButNotWired`, `TestProcessDocumentAsync_PlainTextFallbackPreserved`, `TestRagSearcher_RealVectorSearch`. No real-vector/inference acceptance is claimed.
- Five root-service tests skipped: `TestAIAgent_AssetBundleBinding` and `TestAIAgent_FullChain` require a running local API; `TestDeliverBridgeOutboundSanitizesDouyinText` requires ingress wiring; `TestPlatformAccountService_Login` requires a real browser; `TestWebsiteE2E_ProductQuestionAnsweredByFAQ` requires the runtime database configuration. Tests used only dedicated `POSTGRES_TEST_*` credentials, not the production/runtime database.
- Graph refresh is unavailable because its Python module is missing. Existing best-effort cleanup semantics remain: filesystem/index failures after commit do not undo a successful database deletion.
- Verification is local to the isolated worktree. Concurrent main-checkout source is untouched. No commit, push, deployment, real-channel delivery or production-restart acceptance was performed.

## Recommendation

Close this local transaction-boundary defect as verified. The full-package blocker for the existing FA-CHANNEL-04 item is resolved; update that item's existing evidence while retaining its outstanding real-channel/rollout acceptance. Do not treat deterministic local recovery as proof of external delivery or exactly-once behavior.
