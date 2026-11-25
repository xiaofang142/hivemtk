# Bug Verification: Go unused cleanup and integration evidence

- **Slug**: go-unused-production-20261125
- **Tested**: 2026-11-25
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: integrated-lint-pass; final-suite-pending

## Summary

The primary integration preserved the 23 original business prototypes and two archive predicates with precise unused explanations, removed the 16 obsolete declarations, and corrected the unrelated controller import and final formatting findings. The complete `make lint` run in `user-go-integration-fixed-lint.log` exited 0. Final whole-workspace verification remains pending; retained prototypes are not claimed as production integrations.

## Historical Delegated Verification

The following checks and failures describe the earlier delegated stage, before the primary integration decision in `fix.md`. At that point 23 original findings and two new archive findings still required review, and the controller import blocked whole-server build and lint. Their historical results are preserved below.

## Checks Performed

Commands run from `user-server/` except the explicitly noted repository-root checks.

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Updated bridge tests, final verification | `go test ./internal/bridge -run '^(TestHTTPIngestDefaults\|TestCollectHTTPRequestInfo\|TestHTTPIngestRequest_JsonBind)$'` | pass | Exit 0; request metadata, preview truncation, JSON rebinding. |
| Bounded logging reader | `go test ./internal/bridge -run '^TestHTTPIngestHandler_BodyLimitBeforeIdentity$/^logging-preview-read-is-bounded$'` | pass | Exit 0; uses the actual bounded production helper. |
| Updated AI evidence tests and retained TikTok factory test, final verification | `go test ./internal/service -run '^(TestResolveAISuggestionsEvidence_.*\|TestLeadAdapterForTikTokIsNotDouyin)$'` | pass | Exit 0; source/time fixtures, invalid references, score separation, platform/clue-type separation. |
| Content test compilation | `go test ./internal/content/service -run '^$'` | pass | Exit 0; no DB tests run. |
| Affected production packages | `go build ./internal/bridge/... ./internal/content/service/... ./internal/service/...` | pass | Exit 0. |
| Whole server build | `go build ./...` | fail | Exit 1; concurrent controller change has an unused `fmt` import. |
| Whole server unused reproduction | `golangci-lint run --enable-only=unused ./... --timeout 20m` | fail | Exit 1; same controller typecheck error prevents a whole-server lint inventory. |
| Affected-package unused reproduction | `golangci-lint run --enable-only=unused ./internal/bridge/... ./internal/content/service/... ./internal/service/... --timeout 20m` | fail | Exit 1; 25 unused findings, detailed below. |
| Diff whitespace | Repository-root `git diff --check -- <the 16 changed Go paths listed in fix.md>` | pass | Exit 0; confined to this subtask's files. |
| Required graph rebuild | Repository-root `python3 -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"` | not-run | Tool invoked but import failed: graphify is not installed in this Python environment. |
| Full DB test suites | Not run | skipped | Delegated scope permits only no-DB tests/compilation; no DB tests run in parallel. |

## Output Excerpts

```text
ok  hivemtk-user/internal/bridge   0.655s
ok  hivemtk-user/internal/bridge   0.915s
ok  hivemtk-user/internal/service  1.276s
ok  hivemtk-user/internal/content/service  0.502s [no tests to run]
```

Full build and full lint blocker at the time of execution:

```text
internal/controller/followup_record.go:5:2: "fmt" imported and not used
```

Scoped lint summary:

```text
internal/content/service/marketing_flow.go:286:32: func (*MarketingFlowService).executeFlow is unused (unused)
internal/service/aftersale.go:155:28: func (*AfterSaleService).applyRemoteWriteBack is unused (unused)
internal/service/customer_notification.go: ... 20 unused findings
internal/service/lead_archive_consistency.go:5:6: func leadArchiveAllowed is unused (unused)
internal/service/lead_archive_consistency.go:11:6: func leadArchiveTimestamp is unused (unused)
internal/service/lead_channel_wiring_contract.go:8:6: func leadChannelWiringAllowsMining is unused (unused)
25 issues:
* unused: 25
```

The `lead_archive_consistency.go` findings were absent from the original 39-item lint log. Original findings retained: 1 marketing executor + 1 after-sale helper + 20 notification/outbox declarations + 1 lead-wiring helper = 23. Original declarations removed: 16.

Graph rebuild blocker:

```text
ModuleNotFoundError: No module named 'graphify'
```

## Residual Risks

- The old marketing executor, after-sale rollback helper, lead wiring gate, and notification/outbox prototype have only test callers. Deleting or relocating them without preserving their behavioral assertions would conceal missing production coverage.
- The notification "rebuild" test shares an in-memory map and does not establish process-restart durability. No production persistent outbox or worker was found for this group.
- The retained DB-dependent TikTok dispatch/enqueue tests were reviewed, not executed; factory/platform identity coverage passed without DB setup.
- Other agents are editing the shared worktree. Whole-server errors and newly introduced findings must be reconciled by the primary agent before the final workspace gate.
- No source was edited during this verification stage; no staging, commits, or pushes were performed.

## Historical Recommendation

Keep the overall lint bug open. Review the four explicit behavior decisions in `fix.md`, preserve their assertions on the active production entry points, and then remove or implement the remaining declarations within the primary agent's authorized scope. Reassess those groups using this evidence; after the controller typecheck issue and new archive findings are resolved, rerun the full server build and lint gate. This report does not claim that all 39 findings have been resolved.

## Integration Sync and Final Full-Scope Retry

The primary agent reported that it owns the customer AI title parameter/assignment removal, XLSX error propagation, formatting of those shared files, environment-coverage baseline cleanup for the removed LLM environment key, regenerated `ENV_REFERENCE.md`, and edition environment documentation. This subtask did not repeat or overwrite that work.

After that sync, reran both full-scope checks:

```text
$ go build ./...
# hivemtk-user/internal/controller
internal/controller/followup_record.go:5:2: "fmt" imported and not used
exit 1

$ golangci-lint run --enable-only=unused ./... --timeout 20m
internal/controller/account.go:1: : # hivemtk-user/internal/controller
internal/controller/followup_record.go:5:2: "fmt" imported and not used (typecheck)
1 issues:
* typecheck: 1
exit 1
```

The controller remains outside this delegated unused-file scope. The scoped lint evidence above is the available 25-finding inventory. Final extension-hook check still reads `hooks: {}`; no mandatory hooks are registered. No staging, commits, or pushes were performed.
