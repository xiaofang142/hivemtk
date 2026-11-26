# Bug Verification: 模块闸门生产解析故障与兼容边界

- **Slug**: module-gate-production-failclosed-20261126
- **Tested**: 2026-11-26T01:24:05+08:00
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

原始仓储不可用放行和识别码歧义回退在修复后的生产判定路径中不再复现，新增的租户、账号、渠道和不完整查询边界也通过。结果为 partial，因为本轮按 assessment 约束未运行真实数据库、API、渠道、模型、设备或生产验收。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (post-fix) | `TestModuleGateDecision_ProductionRepositoryUnavailableFailsClosed`；`db.SetTestDB(nil)`后调用生产闸门 | pass | 仓储不可用返回 `ModuleGateError`，数字账号入口关闭；未连接数据库。 |
| New / updated tests | 精确定向 `go test ./internal/service -run '^(TestModuleGateDecision_States|TestModuleGateDecision_ProductionRepositoryUnavailableFailsClosed|TestModuleGateDecision_ProductionLookupDistinguishesLegacyMissFromFailures|TestAutoReplyEnabledForAccountFailsClosedOnRepositoryFailure|TestAutoReplyEnabledForAccountRejectsMissingTenantInMultiTenantMode|TestModuleGateDecision_RejectsTenantConflictsAndIndependentBindings|TestChannelResolver_ResolveByAccountRefDoesNotFallbackAfterAmbiguousIdentifyCode|TestChannelResolver_ResolveByAccountRefClassifiesOnlyHealthyMissAsLegacy|TestChannelResolver_ResolveByAccountRefIncompleteSearchDoesNotClaimLegacyMiss|TestChannelResolver_ResolveByAccountRefDoesNotTreatChannelMismatchAsLegacyMiss|TestChannelResolver_ResolveByAccountRefChannelStoreFailureIsNotLegacyMiss|TestChannelResolver_ResolveByAccountRefRejectsMissingTenantInMultiTenantMode)$' -count=1 -v` | pass | 12 个闸门/解析测试全部通过；01:24:05+08 fresh rerun。 |
| Regression suite | `go test ./internal/service -run '^TestChannelResolver_ResolveByAccountRef' -count=1` | pass | Resolver 相关回归通过。 |
| Build / vet | `go build ./...`；`go vet ./...` | pass | 01:24+08 fresh rerun，两项均 exit 0。 |
| Architecture check | `python3 scripts/check-layer-boundary.py` | pass | 01:24+08 fresh rerun；1785 个非测试 Go 文件，跨层违规 0。 |
| Formatting / diff check | `gofmt`；`git diff --check` | pass | gofmt 已在最终源码改动后执行；01:24+08 fresh diff check 无空白错误。 |
| Graphify refresh | `python3 -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"` | not-run | 环境缺少 `graphify` 模块，命令报 `ModuleNotFoundError`。 |

## Output Excerpts

- `PASS` / `ok hivemtk-user/internal/service 1.237s`（01:24:05+08）
- `ok hivemtk-user/internal/service 0.956s`
- 分层检查：`跨层违规：命中 0 处；基线豁免 0；新增 0`

## Residual Risks

- 定向测试使用 fake/in-memory repository 和 nil DB seam，不证明真实数据库 schema、租户 callback、API 装配或渠道上报链路。
- 本轮没有运行可能触发外部模型/网络的宽泛 service 测试，也没有执行生产部署或真实消息发送。
- `graphify` 本地依赖缺失，代码图未刷新。

## Recommendation

保留修复并将本 bug 标记为限定范围 partial；在具备专用测试数据库和测试渠道的环境补做集成验收后，再决定是否关闭 `LEAD-20261125-03` 的整项任务。不得把本报告升格为生产或端到端验收证据。
