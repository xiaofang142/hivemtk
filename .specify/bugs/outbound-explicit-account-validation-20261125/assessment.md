# Bug Assessment: bridge 显式出站账号校验绕过仓储 seam 且回退到其他账号

- **Slug**: outbound-explicit-account-validation-20261125
- **Created**: 2026-11-25T02:28:30+08:00
- **Source**: `requirements.md` §13 第 3 条 + 主智能体 2026-11-25 纠正口径
- **Verdict**: valid
- **Severity**: high
- **Mode**: assess only（未修生产、未回植、未 commit/push、未外发、未 UI）

## 1. 结论

当前 `normalizeOutboundAccount` 把“空账号”与“无效显式账号”混为一谈：显式账号不存在、禁用、错渠道或查询失败时，会退化为 `autoPickOutboundAccount`，可能把回复写到另一账号名下。这直接违反 `requirements.md` §13：

> 缺账号/错渠道/禁用账号零外发。

同时，该 helper 在 `service/webhook_outbound.go` 内直接使用 `dbstate.Get()`、GORM `WithContext/Where/First` 和 `model.ChannelAccount/model.Channel`，绕过 `ChannelAccountRepository` / `ChannelRepository` 的租户与 edition 门禁。显式账号校验必须改为 service + repository seam，且错误必须可见，不能返回空账号后让 capability 门掩盖根因。

## 2. 当前代码事实

### 2.1 行为缺口

`user-server/internal/service/webhook_outbound.go`：

- `autoPickOutboundAccount`（约 601 行）：空账号时经 `ChannelDispatchService.PickAccount` 自动分派。
- `normalizeOutboundAccount`（约 633 行）：
  - `accountID == ""` 才应 auto-pick；
  - 但当前显式 ID 解析失败、状态非 1、渠道不匹配、DB nil 或查询错误时也调用 auto-pick；
  - 返回值只有 `string`，没有 error，仓储运行错误会退化成空账号，再由 capability 门返回 `capability_mapping_incomplete`。
- `sendOutbound`（约 695 行）直接调用该 helper，随后才做 capability 授权。

### 2.2 五层 / edition 缺口

`normalizeOutboundAccount` 当前直接执行：

- `dbstate.Get()`；
- `db.WithContext(ctx).Where("tenant_id = ? AND id = ? AND status = ?", ...).First(&acc)`；
- `db.WithContext(ctx).Select("id, code").Where("id = ? AND tenant_id = ?", ...).First(&chModel)`。

因此：

- 绕过 `ChannelRepository.GetByID` 的 `edition.AllowsContext` 国内渠道门禁；
- 绕过 repository 的统一错误与存储不可用语义；
- service 层重新长出第二份账号/渠道查询真值。

另有一处独立但会被双文件架构闸命中的直接 DB 访问：`replayDelayedOutbound` 约 508 行通过 `tenant.SingleTenantByKey(ctx, dbstate.Get(), "bridge_accounts", ...)` 反查租户。若按主智能体要求“`webhook_outbound.go` 与 `outbound_registry.go` 都不得直接 DB/GORM 查询”，该处也必须同步下沉，不能只修显式账号 helper。

### 2.3 registry 归属缺口

`webhook_outbound.go` 仍直接调用 `bridgeOutboxCapable`。主智能体要求该能力判断移到 `outbound_registry.go` 的 registry-owned helper；webhook 只调用 registry/service，不直接判断能力位，也不搬 DB helper。

## 3. 可复用 seam 评估

| 需求 | 现有能力 | 评估 |
|------|----------|------|
| tenant + accountID 查账号 | `ChannelAccountRepository.GetByID(ctx, tenantID, id)` | 可直接复用；service 检查 `Status == 1` |
| tenant + channelID 查渠道 | `ChannelRepository.GetByID(ctx, tenantID, id)` | 可直接复用；已含 edition 门禁 |
| channel code 查 channelID | `ChannelRepository.GetByTenantAndCode(ctx, tenantID, code)` | 可直接复用；已含 edition 门禁 |
| 空账号自动分派 | `ChannelDispatchService.PickAccount` | 保留；A staged 补丁已修复 channel/conversation/sticky/preferred 错误传播 |
| service 编排位置 | `ChannelDispatchService` 已同时持有 `channelRepo` 与 `accRepo` | 推荐在其上新增 `ResolveExplicitOutboundAccount` 或等价方法，无需为 canonical 账号校验新增 repository 方法 |
| bridge_accounts 反查租户 | 未找到合适的现成 repository 方法 | 若双文件架构闸保持全域禁 DB，需要新增最小 repository 方法（tenant + channel + accountID → tenantID），错误/歧义语义须与现 `tenant.SingleTenantByKey` 一致 |

**不需要**把 SQL 搬进 service；**也不建议**为了过静态闸把 DB helper 原样搬到 `outbound_registry.go`。

## 4. 建议修复设计（待审批，不在本轮实施）

### 4.1 Service 方法

在 `ChannelDispatchService` 或等价 service 中新增显式账号解析方法，建议输入：

- `ctx`
- 有效 `tenantID`（ctx / 入站 hub 既有归属推导结果；最终为 0 必须拒绝）
- `channel`（code 或 channelID）
- `accountID`

处理顺序：

1. 非 bridge 由 registry-owned helper 直接保持旧语义，不进入账号校验。
2. bridge 且 `accountID` 为空：调用 `PickAccount`；失败返回可见错误。
3. bridge 且 `accountID` 非空：
   - 必须可解析为数值账号 ID；
   - `accRepo.GetByID(ctx, tenantID, id)`：运行错误原样上抛；未命中拒绝；
   - `Status != 1`：拒绝；
   - `channelRepo.GetByID(ctx, tenantID, acc.ChannelID)`：运行错误原样上抛；未命中/edition 禁止拒绝；
   - 请求 channel 与账号 channel 必须精确匹配（code 大小写归一化或 channelID 精确匹配）；
   - 通过后精确返回原显式账号，不再 auto-pick。

### 4.2 Registry-owned helper

`outbound_registry.go` 新增/收口一个出站账号解析 helper：

- 只有该 helper 调用 `bridgeOutboxCapable`；
- bridge 空账号 → `PickAccount`；
- bridge 显式账号 → `ResolveExplicitOutboundAccount`；
- 非 bridge → 原样返回；
- 返回 `(accountID string, err error)`。

`webhook_outbound.go` 只调用该 helper，并在授权、ClaimReply、渲染、sender 之前处理错误；无效显式账号必须 `sent=false`、可见 error、outbox 0 行。

### 4.3 延迟重放直查 DB（主智能体已仲裁）

在 `internal/repository` 新建只读 `BridgeAccountTenantRepository`，接口方法：

```go
ResolveTenantID(ctx context.Context, channel, accountID string) (uint, error)
```

实现要求：

- 使用 `model.BridgeAccount` + `tenant.WithSystemScope`；
- `Distinct("tenant_id")` + `Limit(2)`；
- 0 行返回 `(0, nil)`；
- 1 行返回唯一 tenantID；
- 2 行返回 `tenant.ErrAmbiguousTenantKey`；
- 查询运行错误原样上抛；
- 不放进 `ChannelAccountRepository`，service 不 import bridge，避免成环。

`WebhookService` 新增接口字段，`NewWebhookService` 在 `DBReady()` 时装配；同包测试可注入替身。

replay 语义：

- ctx 已有非零租户：保持原租户；
- 反查运行错误或歧义：立即 `abandonReplay`，**不得回退 `rec.TenantID`**；
- 仅 `(0, nil)` 未命中才回退 `rec.TenantID`；
- 行值也为 0：`abandonReplay`。

## 5. RED 回归（仅 clean snapshot）

快照：`user-head-934825c2d-e/`。生产 `webhook_outbound.go` / `outbound_registry.go` 已恢复 clean HEAD；`channel_dispatch.go` 与 `channel_dispatch_query_errors_test.go` 叠加 actual staged A 补丁。

### 5.1 架构 RED

新增 `internal/service/outbound_account_architecture_test.go`，同时扫描：

- `webhook_outbound.go`
- `outbound_registry.go`

禁止直接 GORM/model 查询与 `dbstate.Get().WithContext` 类访问；仅允许 constructor 调用。

结果：预期 FAIL，rc=1。命中：

- `webhook_outbound.go` 直接 `bridgeOutboxCapable`；
- `webhook_outbound.go:508` 直接 `dbstate.Get()`；
- `webhook_outbound.go:652-664` 直接 model/GORM 查询；
- `outbound_registry.go` 当前无直接 DB，双文件闸覆盖成立。

日志：`check-e2-architecture-red-r2.log`。

### 5.2 行为 RED

新增 `internal/service/outbound_explicit_account_validation_test.go`。

| 场景 | clean HEAD + staged A 结果 | 期望 |
|------|-----------------------------|------|
| 有效显式账号精确使用 | PASS | 精确出库，不替换为其他账号 |
| 空账号 auto-pick | PASS | 自动分派后出库 |
| 不存在 | FAIL：`sent=true outbox=1` | 零 outbox + 可见错误 |
| 禁用 | FAIL：`sent=true outbox=1` | 零 outbox + 可见错误 |
| 错渠道 | FAIL：`sent=true outbox=1` | 零 outbox + 可见错误 |
| 跨租户 | FAIL：`sent=true outbox=1` | 零 outbox + 可见错误 |
| tenant=0 | PASS（当前由 capability 门拒绝） | 零 outbox + 可见错误 |
| edition 禁止 | FAIL：`sent=true outbox=1` | 零 outbox + 可见错误 |
| channel repository 运行错误 | FAIL：`sent=true outbox=1` | 零 outbox + 原始仓储错误可见 |
| account repository 运行错误 | FAIL：错误被吞成 `capability_mapping_incomplete` | 零 outbox + 原始仓储错误可见 |

日志：`check-e2-explicit-account-red-final.log`。

A staged 基底回归：`TestChannelDispatchService_*` 全部 PASS，日志 `check-e2-a-staged-overlay-regression.log`；staged `channel_dispatch.go` SHA256 与快照一致：`615a63b8f7244828e55fc38bd09db31c3fbee0ddf358d0c5e79e5bde6c3518bf`。

## 6. 10 个 capability 失败分类（不混入本修复）

在真正未修改的 `head-snapshot-934825c2d/`（`channel_dispatch.go` SHA256 与 HEAD 一致）重跑全部 `^TestSendOutbound_Bridge_`：

- 13 个用例：10 FAIL / 3 PASS；
- 10 个失败全部为 `capability_mapping_incomplete`；
- 本次 clean HEAD 运行未出现 `capability_storage_unavailable`；
- 3 个 PASS：`HubMsgNilIsNotSent`、`UndeliverableIsNotSent`、`UnknownChannelRejected`。

10 个失败：

1. `TestSendOutbound_Bridge_PersistFailureIsNotSent`
2. `TestSendOutbound_Bridge_QueuedIsSent`
3. `TestSendOutbound_Bridge_DuplicateAuditRemainsNonDeliverable`
4. `TestSendOutbound_Bridge_AuditSafetyKeepsRealDBFailureRetry`
5. `TestSendOutbound_Bridge_SenderIDEqualsAccountID_NineChannels`
6. `TestSendOutbound_Bridge_ReplayKeepsSender`
7. `TestSendOutbound_Bridge_MarkdownAlignedWithManualPath`
8. `TestSendOutbound_Bridge_CardsFallbackRecordedOnRow`
9. `TestSendOutbound_Bridge_NoCardsNoDroppedKey`
10. `TestSendOutbound_Bridge_CardsFallbackLogsOnce`

**分类：测试夹具缺失/漂移，不是 capability 生产缺口。**

共同根因：这些用例使用 `acct-*` 等外部账号字符串，却没有播种 canonical `Channel` / `ChannelAccount` 行。当前 `normalizeOutboundAccount` 显式查询失败后 auto-pick；测试库没有 canonical 账号，最终 `accountID=""`，capability 门才返回 `capability_mapping_incomplete`。新增的“有效显式账号”回归播种 canonical 行后同链路 PASS，进一步支持该分类。

修复本 lane 后，这 10 个旧夹具必须改为播种 canonical 渠道/账号并使用数值显式 ID；不能通过放宽 capability 门或恢复 auto-pick 来让它们变绿。无需为 capability 另开生产 bug slug。

日志：`check-e2-capability-clean-head-all.log`。

## 7. 实际工作树冲突预检

- HEAD：`934825c2d6b804e79c3bf144cbe25dbcc913007e`
- 不在本轮 scope、禁止覆盖：
  - `user-server/internal/bridge/tenant_binding.go`：外部 staged；
  - `user-server/internal/channelgw/registry_parity_test.go`：外部 staged。
- 后续修复存在在途冲突：
  - `user-server/internal/service/channel_dispatch.go`：`MM`，staged 为 A 补丁，worktree 还有外部 constructor 重构；
  - `user-server/internal/service/webhook_outbound.go`：外部未提交的一行 constructor 调用变化。
- 本轮未回植任何 actual 产品源码；只创建本 assessment 目录与 snapshot 测试。
- 冻结记录：`e2-freeze.txt`。

## 8. 建议验收门

修复获批后至少跑：

1. 新增显式账号行为矩阵全绿；
2. 双文件架构回归全绿；
3. A staged `TestChannelDispatchService_*` 保持全绿；
4. 10 个旧 capability 夹具迁移后全绿；
5. `go test ./internal/service ./internal/bridge/... ./internal/channelgw/...`；
6. `go build ./...`、`go vet ./...`、layer boundary、architecture 门；
7. bridge vitest 全量；
8. 回植前重新预检所有 scoped 文件，发现外部 diff 即停。

## 9. 风险与边界

- 显式账号 fail-closed 会让旧“外部字符串账号 ID”调用方显式失败；这是正确安全语义，但调用方/夹具必须迁移。
- repository 运行错误应与“账号不存在”分离，不能统一变成 404 或 auto-pick。
- edition 禁止必须经 `ChannelRepository`，不能在 service 重新实现一份渠道名单。
- 延迟重放租户反查按已批准的 `BridgeAccountTenantRepository` seam 实施；运行错误/歧义立即 abandon，只有未命中才回退行值。
- 未做 UI、真实外发、生产验收。

## 10. 主智能体审批结果

**已正式批准 E fix scope。** 批准内容：

1. `ChannelDispatchService.ResolveExplicitOutboundAccount(ctx, tenantID, channel, accountID)`：非 bridge 原样；bridge 空账号走 `PickAccount`；bridge 显式账号必须数值 ID，经 `accRepo.GetByID`、`Status==1`、`channelRepo.GetByID` 与请求 channel 精确匹配；未命中/禁用/错渠道/跨租户/edition 禁止/运行错误均返回可见 error，绝不 auto-pick。
2. `outbound_registry.go` 新增 registry-owned `isBridgeOutboundChannel` / 等价 helper，唯一调用 `bridgeOutboxCapable`；账号解析 helper 收口在此文件，但只调用 service，不写 DB/GORM。`webhook_outbound.go` 删除直接能力判断与 model/GORM 查询。
3. replay 反查租户下沉为新的 `repository.BridgeAccountTenantRepository`，按 §4.3 的仲裁实现；`WebhookService` 增加接口字段并在 `DBReady()` 时装配。
4. 迁移 10 个既有 `TestSendOutbound_Bridge_*` 的 canonical Channel/ChannelAccount 夹具，不放宽 capability 门。
5. 回归覆盖显式账号矩阵、tenant reverse lookup 0/单租户/歧义/运行错误、replay abandon/回退/无归属、双文件架构闸、A staged 回归、10 capability 夹具全绿。

冲突安全要求：actual worktree 定点 edit，禁止整文件覆盖；外部 staged `bridge/tenant_binding.go`、`channelgw/registry_parity_test.go` 绝不改；每次写 scoped 文件前重新检查 diff/SHA/status。
