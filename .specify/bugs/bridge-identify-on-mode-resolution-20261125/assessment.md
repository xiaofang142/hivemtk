# Bug Assessment: tenant-on channel-level identify_code fallback 使用未绑定 ctx

- **Slug**: bridge-identify-on-mode-resolution-20261125
- **Created**: 2026-11-25T03:15:00+08:00
- **Updated**: 2026-11-25T03:16:00+08:00
- **Source**: E4 full bridge/channelgw 回归 + 主智能体根因复核
- **Verdict**: valid
- **Severity**: high
- **Mode**: approved fix lane（RED→GREEN 已完成，待主智能体独立复验）

## Report

channel-level `ChannelIdentifyCode` fallback 已从记录拿到 `rec.TenantID`，但后续 Channel/ChannelAccount subordinate reads 仍使用原始未绑定 `rctx`。tenant-on 下这些读取被租户隔离拒绝，导致 `ChannelCode` 为空、账号为空，最终误报 `ErrBridgeTenantAmbiguous`。

## Exact Reproduction

`seedCanonicalBridgeFixture` 返回两类不同 code：

- channel-level code：`fixture-identify-douyin-11`（写入 `channel_identify_codes`）；
- account-level code：`account-fixture-douyin-11-diag`（写入 `channel_accounts.identify_code`）。

请求显式携带 channel-level code，因此 `ChannelResolver.ResolveByIdentifyCode` 先查账号冗余码未命中，随后由 `resolveBridgeIdentityByChannelCode` 处理。

诊断日志 `check-e4-diag-identify-resolution-on.log`：

- fixture 只有 1 个 ChannelAccount、1 个 ChannelIdentifyCode；
- tenant-on + 无租户 ctx：TenantID/AccountID/ChannelID 可解析，但 `ChannelCode=""`；
- tenant-on + system scope 或 `tenant.OnlyTenant(ctx, 11)`：`ChannelCode=douyin`，解析成功；
- off-mode：identity-only 解析成功，不绑定租户、不改写原 request。

## Root Cause

`resolveBridgeIdentityByChannelCode(rctx, identifyCode)` 在 `GetByCode` 后已得到 `rec.TenantID`，但：

- `channelRepo.GetByID(rctx, rec.TenantID, rec.ChannelID)`；
- `ChannelAccountService.ListAccounts(rctx, ...)`；

仍使用未绑定 ctx。tenant-on 的隔离 callback 拒绝这些读取；函数把空 ChannelCode/AccountID 返回给上层，`resolveBridgeIdentity` 使用 `channel-<id>` 回退并与请求 channel 比较，误报歧义。

## Required Semantics to Preserve

1. **identify_code-only**：不恢复 `X-Bridge-Token`、`tenant_code` 或 account_id 自证第二权威。
2. **off-mode identity-only**：合法识别码解析身份，但不绑定租户、不改写原 request。
3. **fail-closed**：
   - 缺码/无效码拒绝；
   - 同码多租户歧义拒绝；
   - Channel/ChannelAccount 查询运行错误不得静默降级为未命中；
   - 已有租户与识别码租户冲突拒绝。
4. **no request mutation**：解析阶段不得就地改写调用方 request；只有 `bindBridgeTenant` 现有成功绑定路径可按契约派生新 request ctx。
5. **edition gate 保留**： subordinate Channel 读取必须继续经 `ChannelRepository.GetByID`，不能在 service 复制渠道名单。

## Proposed Remediation（待审批）

在 `resolveBridgeIdentityByChannelCode` 取得 `rec.TenantID` 后，立即派生：

```go
tenantCtx := tenant.OnlyTenant(rctx, rec.TenantID)
```

并仅用 `tenantCtx` 执行 Channel/ChannelAccount subordinate reads；原 `rctx` 保持不变。若读取运行错误，返回可见 error，不伪装成 `(0,nil)` 或歧义。

拟改文件：

- `user-server/internal/bridge/tenant_binding.go`（targeted edit only）

不拟改：

- `ChannelResolver`、repository、service 的全局鉴权语义；
- 外部 staged `handler_http.go`；
- 任何 bartoken/account_id 兼容路径。

## Conflict Precheck（编辑前必须重做）

2026-11-25 03:16 CST 实测：

- `git status --short`：`M  user-server/internal/bridge/tenant_binding.go`
- staged blob SHA256：`8276275a46375d424093cfd62f0f71cb86fb759e9a5e5cd4ba98570ab16da760`
- worktree SHA256：`8276275a46375d424093cfd62f0f71cb86fb759e9a5e5cd4ba98570ab16da760`
- staged/unstaged 内容一致，无额外 unstaged diff。

该文件正被外部会话 staged 修改；未获主智能体批准前不编辑。批准后必须先重新计算 staged blob/worktree SHA 与 diff，任何变化立即停止。

## Adjudication and Implemented Fix

主智能体已批准 targeted edit。实现分两层，均只修改外部 staged `tenant_binding.go`：

- `resolveBridgeIdentityByChannelCode` 在拿到 `rec.TenantID` 后用 `tenant.OnlyTenant(rctx, rec.TenantID)` 执行 Channel/ChannelAccount subordinate reads，并传播运行错误；为避免 `OnlyTenant` 剥离 edition override，内部 child ctx 显式保留原 ctx 的 edition 值；
- `resolveBridgeIdentity` 在渠道比较前用同一内部 child ctx 补读空 `ChannelCode`。

原 `rctx` 与 request 不改写；off-mode 仍在最终返回时保持 identity-only/no binding。

## Tests

- `TestBindBridgeTenant_ValidIdentifyCodeBindsTenant`
- `TestBindBridgeTenant_AliasChannelMatchesCanonical`
- `TestBridgeTenantForRequest_IdentifyCode`
- `TestBindBridgeTenant_ChannelFallbackQueryErrorRejected`
- `TestBindBridgeTenant_ChannelCodeDisabledAccountCannotSend`
- off-mode、缺码/无效码、账号/渠道 mismatch、已绑定不同租户、同租户 noop 回归

RED：`check-e4-on-mode-red-r2.log`（4 个目标用例失败）。
GREEN：`check-e4-on-mode-disabled-green.log`（全部 tenant binding 用例，含 disabled-only channel code 回归通过）。
