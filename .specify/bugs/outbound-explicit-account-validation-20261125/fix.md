# Bug Fix: bridge 显式出站账号校验与 replay 租户反查 seam

- **Slug**: outbound-explicit-account-validation-20261125
- **Assessment**: ./assessment.md
- **Status**: applied（actual worktree 定点修复完成，scoped 本地门禁通过，待主智能体独立复验）
- **Started**: 2026-11-25T02:30:00+08:00
- **Applied**: 2026-11-25T02:42:30+08:00

## Approved Scope

1. `ChannelDispatchService.ResolveExplicitOutboundAccount(ctx, tenantID, channel, accountID)`：
   - 非 bridge：保持旧语义；
   - bridge 空账号：`PickAccount`；
   - bridge 显式账号：数值 ID + `accRepo.GetByID` + `Status==1` + `channelRepo.GetByID` + channel 精确匹配；
   - 所有无效/运行错误返回可见 error，绝不 auto-pick。
2. `outbound_registry.go` 新增 registry-owned `isBridgeOutboundChannel` 与账号解析 helper；在 webhook/registry 两文件范围内，`bridgeOutboxCapable` 只由该 helper 调用；helper 只调 service，不写 DB/GORM。
3. 新建 `repository.BridgeAccountTenantRepository`：
   - `ResolveTenantID(ctx, channel, accountID)`；
   - `model.BridgeAccount` + system scope + `Distinct/Limit(2)`；
   - 0/单租户/歧义/运行错误语义分离；
   - `WebhookService` 接口字段 + `DBReady()` 构造装配。
4. replay：
   - 反查错误/歧义立即 abandon；
   - 仅未命中回退 `rec.TenantID`；
   - 仍无租户则 abandon。
5. 迁移 10 个既有 capability 夹具，不放宽 capability 门。

## Actual Test File Inventory

10 个目标用例实际位于以下 4 个文件：

- `user-server/internal/service/webhook_batchc_bridge_sent_test.go`
  - `TestSendOutbound_Bridge_PersistFailureIsNotSent`
  - `TestSendOutbound_Bridge_QueuedIsSent`
- `user-server/internal/service/webhook_outbound_audit_test.go`
  - `TestSendOutbound_Bridge_DuplicateAuditRemainsNonDeliverable`
  - `TestSendOutbound_Bridge_AuditSafetyKeepsRealDBFailureRetry`
- `user-server/internal/service/webhook_outbound_b5_bridge_test.go`
  - `TestSendOutbound_Bridge_SenderIDEqualsAccountID_NineChannels`
  - `TestSendOutbound_Bridge_ReplayKeepsSender`
  - `TestSendOutbound_Bridge_MarkdownAlignedWithManualPath`
- `user-server/internal/service/webhook_outbound_r26_bridge_cards_test.go`
  - `TestSendOutbound_Bridge_CardsFallbackRecordedOnRow`
  - `TestSendOutbound_Bridge_NoCardsNoDroppedKey`
  - `TestSendOutbound_Bridge_CardsFallbackLogsOnce`

## Actual Production Files

- `user-server/internal/service/channel_dispatch.go`
  - 保留 staged A 的 channel/conversation/sticky/preferred 错误传播；
  - 保留 unstaged 外部 `NewChannelDispatchService()` constructor 重构；
  - 新增 `ResolveExplicitOutboundAccount` 与数值 ID/channel/status/edition fail-closed 校验。
- `user-server/internal/service/outbound_registry.go`
  - 新增 registry-owned `isBridgeOutboundChannel` 与 `resolveOutboundAccount`；
  - registry 只调用 service，不写 DB/GORM。
- `user-server/internal/service/webhook_outbound.go`
  - 删除旧 `autoPickOutboundAccount` / `normalizeOutboundAccount` 直查 DB helper；
  - `sendOutbound` 改为 registry/service 账号解析并可见返回错误；
  - replay 改走 `BridgeAccountTenantRepository`，错误/歧义立即 abandon，仅未命中回退行值；
  - 原外部 constructor 一行变化位于被删除的旧 helper 内，替换逻辑使用无参 constructor，未整文件覆盖。
- `user-server/internal/service/webhook.go`
  - 保留 staged leadDiscovery 变更；
  - 新增 `bridgeAccountTenantRepo` 接口字段与 `DBReady()` 装配。
- `user-server/internal/repository/bridge_account_tenant.go`（新文件）
  - `BridgeAccountTenantRepository.ResolveTenantID`；
  - `model.BridgeAccount` + system scope + `Distinct/Limit(2)`；
  - 0/单租户/歧义/运行错误语义分离。

## Actual Test Files

- `user-server/internal/service/outbound_explicit_account_validation_test.go`
  - 有效显式账号、空账号 auto-pick、不存在/禁用/错渠道/跨租户/tenant0/edition 禁止/channel 与 account 仓储错误矩阵。
- `user-server/internal/service/outbound_account_architecture_test.go`
  - 双文件架构闸：`webhook_outbound.go` 与 `outbound_registry.go` 均禁直接 DB/GORM/model 查询；
  - 真实路径即此文件；**不存在** `service/audit_architecture_test.go`。
- `user-server/internal/repository/bridge_account_tenant_test.go`
  - no-row、tenant0、one-tenant、ambiguous、query-error。
- `user-server/internal/service/delayed_replay_tenant_test.go`
  - 已有租户、未命中回退行值、错误/歧义不回退、无归属拒绝；
  - replay 错误/歧义立即 abandon 的集成断言。
- `user-server/internal/service/bridge_outbound_fixture_test.go`
  - canonical Channel/ChannelAccount 夹具与 custom edition 精确能力上下文 helper。
- 既有 4 个 capability 夹具文件（见上）已迁移为 canonical 数值账号 ID，未放宽 capability 门。

## Conflict Safety / Backport Boundary

- 所有修改均直接落在 actual worktree，未从快照整文件回植。
- `channel_dispatch.go` 的 staged A 补丁与 unstaged constructor 重构均保留。
- `webhook.go` 的 staged leadDiscovery 变更均保留。
- 外部 staged `bridge/tenant_binding.go`、`channelgw/registry_parity_test.go` 未修改。
- D 的 edition repository/service/test 改动未修改。
- 未 commit、未 push、未部署、未外发、未 UI。

## Verification

最终 actual 日志：

- `check-e3-targeted-green-actual.log`：rc=0，覆盖 repository reverse-lookup、架构闸、显式账号矩阵、A dispatch、bridge 13 个既有用例、replay 仲裁/abandon。
- `check-e3-build-actual.log`：rc=0（02:42:22）。
- `check-e3-vet-actual.log`：rc=0（02:42:12）。
- `check-e3-layer-boundary-actual.log`：rc=0，1757 files / 0 新增跨层违规。
- `check-e3-architecture-gate-actual.log`：rc=0，仅既有 `wecomCSService` interface warning。

详细 RED/GREEN 与覆盖矩阵见 `./test.md`。本 lane 为 applied，等待主智能体独立复验；不自行关闭 E。
