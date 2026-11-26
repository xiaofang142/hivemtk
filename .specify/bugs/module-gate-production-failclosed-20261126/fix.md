# Bug Fix: 模块闸门生产解析故障与兼容边界

- **Slug**: module-gate-production-failclosed-20261126
- **Fixed**: 2026-11-26T01:10:24+08:00
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

生产字符串和数字账号闸门现在将仓储故障、身份歧义、租户缺失、渠道不匹配、解析后账号消失或 ID/租户不一致统一判为关闭；仅健康查询明确确认无匹配时保留 legacy 兼容放行。识别码歧义不再进入名称/数字回退路径。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/service/auto_reply.go` | modified | 增加可注入闸门判定；数字入口校验仓储、请求账号 ID、租户和绑定状态；字符串入口只接受 typed legacy miss。 |
| `user-server/internal/service/channel_resolver.go` | modified | 增加明确 legacy miss 错误；传播识别码歧义、渠道仓储故障和渠道不匹配；多租户缺 tenant 失败；满页查询不宣称无匹配。 |
| `user-server/internal/service/module_gate_decision_test.go` | modified | 覆盖生产解析、仓储不可用、故障分类、缺 tenant、跨租户、ID 不一致、关闭和绑定状态。 |
| `user-server/internal/service/channel_contract_test.go` | modified | 覆盖识别码歧义不回退、健康未命中、满页不完整查询、渠道不匹配和渠道仓储故障。 |

## Tests Added or Updated

- `TestModuleGateDecision_ProductionRepositoryUnavailableFailsClosed`
- `TestModuleGateDecision_ProductionLookupDistinguishesLegacyMissFromFailures`
- `TestAutoReplyEnabledForAccountFailsClosedOnRepositoryFailure`
- `TestAutoReplyEnabledForAccountRejectsMissingTenantInMultiTenantMode`
- `TestModuleGateDecision_RejectsTenantConflictsAndIndependentBindings`
- `TestChannelResolver_ResolveByAccountRefDoesNotFallbackAfterAmbiguousIdentifyCode`
- `TestChannelResolver_ResolveByAccountRefClassifiesOnlyHealthyMissAsLegacy`
- `TestChannelResolver_ResolveByAccountRefIncompleteSearchDoesNotClaimLegacyMiss`
- `TestChannelResolver_ResolveByAccountRefDoesNotTreatChannelMismatchAsLegacyMiss`
- `TestChannelResolver_ResolveByAccountRefChannelStoreFailureIsNotLegacyMiss`
- `TestChannelResolver_ResolveByAccountRefRejectsMissingTenantInMultiTenantMode`

## Local Verification

- 精确定向测试：上述 12 个测试，`go test ./internal/service -run '^(...)$' -count=1 -v` → **pass**，`ok hivemtk-user/internal/service 0.979s`。
- Resolver 回归子集：`go test ./internal/service -run '^TestChannelResolver_ResolveByAccountRef' -count=1` → **pass**，`ok hivemtk-user/internal/service 0.956s`。
- `go build ./...` → **pass**。
- `go vet ./...` → **pass**。
- `python3 scripts/check-layer-boundary.py` → **pass**，扫描 1785 个非测试 Go 文件，跨层违规 0。
- `git diff --check` → **pass**。
- Graphify 刷新按工作区规定尝试，但当前环境缺少 `graphify` Python 模块，命令以 `ModuleNotFoundError` 结束；未伪称刷新成功。

## Deviations from Assessment

- 在 assessment 允许的两个 service 文件内新增 `ErrChannelIdentityLegacyUnmatched`，把“健康查询明确无匹配”与通用身份不确定错误分离；这是为满足 assessment 的 fail-closed 兼容边界所需的最小类型区分。
- 增加渠道仓储故障传播和满页查询保护，并增加解析结果与最终账号 ID 一致性检查；均未扩大文件范围。

## Follow-ups

- 仍需在具备专用测试数据库凭据的环境验证真实仓储、多租户数据隔离、API/渠道入站和两类生产消费者的集成链路。
- 不在本次修复中进行真实消息、模型调用、设备/UI、部署或生产验收。
