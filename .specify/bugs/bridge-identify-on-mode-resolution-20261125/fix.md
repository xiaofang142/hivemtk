# Bug Fix: tenant-on identify_code fallback 使用已解析租户 ctx

- **Slug**: bridge-identify-on-mode-resolution-20261125
- **Assessment**: ./assessment.md
- **Status**: applied（待主智能体独立复验）
- **Applied**: 2026-11-25T03:20:22+08:00

## Summary

只 targeted edit `user-server/internal/bridge/tenant_binding.go`：

- channel-level fallback 的 Channel/ChannelAccount subordinate reads 改用 `tenant.OnlyTenant(rctx, rec.TenantID)`，并显式保留 edition override；
- `resolveBridgeIdentity` 在渠道比较前用同一内部 child ctx 补读空 `ChannelCode`；
- subordinate 查询运行错误原样返回，不再静默降级；
- 原 request/rctx 不改写，off-mode identity-only、edition gate、ambiguity/error fail-closed 保持。

## Conflict Safety

编辑前 staged blob/worktree SHA 均为 `8276275a46375d424093cfd62f0f71cb86fb759e9a5e5cd4ba98570ab16da760`；未覆盖外部 staged 内容，只追加 targeted diff。未改 `channel_resolver.go` 或任何 repository。

## Verification

- RED：`check-e4-on-mode-red-r2.log`，4 个目标用例失败。
- GREEN：`check-e4-on-mode-disabled-green.log`，`TestBindBridgeTenant_*` 与 `TestBridgeTenantForRequest*` 全部 PASS，含 disabled-only channel code 零出站校验。
- 未 commit/push、未部署、未外发、未 UI。
