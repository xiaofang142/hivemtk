# Bug Assessment: 渠道账号读取与选号绕过版本门

- **Slug**: edition-account-gate-20261125
- **Created**: 2026-11-25T01:51:00+08:00
- **Source**: pasted text — requirements.md §9 EDITION-CHANNEL-CONTROL + 主智能体 D 矩阵复核
- **Verdict**: valid
- **Severity**: high

## Report (verbatim or summarized)

> opensource 只能使用国外渠道；custom 可用国内+国外；未知/非法 version_type fail-closed。列表、详情、code 查询、发送与账号选择均遵守版本门。

冻结源码：`934825c2d6b804e79c3bf144cbe25dbcc913007e`。

## Symptom

`ChannelRepository` 已对国内渠道执行 edition gate，但 `ChannelAccountRepository` 的读取方法不校验所属 channel。于是 opensource（以及被归一化为 opensource 的未知版本）仍可：

- `List` / `ListOptions` 列出国内渠道账号；
- `GetByID` / `Get` / `GetDetail` 返回国内渠道账号；
- `GetByIDs` 在权限计算中返回国内渠道账号；
- `GetByIdentifyCode` 通过账号冗余码或权威识别码表解析出国内渠道账号；
- `ChannelDispatchService.PickAccount` 在调用方直传 `ChannelID` 时跳过 `ChannelRepository`，直接选中国内账号。

custom 与国外渠道行为正常。

## Reproduction

1. 在干净 HEAD 快照播种 `douyin`、`telegram`、`wecom` 渠道及对应账号。
2. 用 `edition.WithContext(ctx, "opensource")` 调 repository/service 矩阵。
3. 当前 `GetByID`、`List`、`GetByIDs`、两种 `GetByIdentifyCode` 路径均返回国内账号。
4. `PickAccount` 直传国内 `ChannelID` 返回 `round_robin` 账号。
5. `unknown="bogus"` 同样泄漏；`custom` 与 `telegram` 正常。

证据：`check-d-edition-repo-r2.log`、`check-d-edition-account-dispatch-r3.log`、`check-d-account-repo-red-r2.log`。

## Suspected Code Paths

- `user-server/internal/repository/channel_account.go:74` — `GetByID` 仅按 tenant/id 查账号。
- `user-server/internal/repository/channel_account.go:89` — `List` 仅按 tenant/channel/status 过滤。
- `user-server/internal/repository/channel_account.go:119` — `GetByIDs` 仅按 tenant/id 过滤。
- `user-server/internal/repository/channel_account.go:137` — `GetByIdentifyCode` 两条解析路径均无 channel edition gate。
- `user-server/internal/service/channel_account.go:108` — 统一账号列表口直接委托 repository，无版本过滤。
- `user-server/internal/service/channel_dispatch.go:60` — 仅当 `ChannelID==0` 才经 `ChannelRepository`；直传 ID 绕过版本门。
- `user-server/internal/pkg/edition/edition.go:197` — 国内渠道码判定已有单一策略源，但未导出可供 repository 查询使用的集合。

## Root Cause Hypothesis

高置信度：版本门只落在 `ChannelRepository`，而账号仓储是独立数据面；大量调用方直接使用账号仓储，导致“渠道可见性”与“渠道账号可用性”形成两个不一致权威。`PickAccount` 的直传 `ChannelID` 只是该缺口的显式放大点。

## Proposed Remediation

**Preferred：在 `ChannelAccountRepository` 数据面建立统一 edition gate seam。**

1. 在 `edition` 包增加策略集合出口（例如 `DomesticChannelCodes()` / `BlockedChannelCodes(ctx, feature)`），国内渠道码只保留单一事实源。
2. 在 `channel_account.go` 增加共享 query scope，例如 `applyChannelEditionGate(ctx, q)`：
   - custom 或允许国内渠道时原样返回；
   - opensource/unknown 时用参数化子查询 `channel_id IN (SELECT id FROM channels WHERE code NOT IN ?)` 排除国内渠道；
   - 全部通过 GORM 参数绑定，不写 PostgreSQL 方言 SQL，不在 repository 重抄国内码列表。
3. 将 gate 应用到：
   - `GetByID`
   - `List`（在分页/limit 之前应用，避免先分页后过滤导致少页/泄漏）
   - `GetByIDs`
   - `GetByIdentifyCode` 的账号冗余码查询与权威码反查账号查询
4. 该 seam 自动覆盖 `ListOptions`、`Get`、`GetDetail`、权限计算、resolver 与 `PickAccount` 的 sticky/preferred/round-robin，因为后者最终都走账号仓储；预计**不需要**在 `channel_dispatch.go` 再加第二套 edition 判断。
5. 保持账号查询既有 `tenant_id` 条件不变；`GetByIdentifyCode` 仍显式 `tenant.WithSystemScope` 跨租户解析，edition 使用原 ctx，不让 system scope 放大版本权限。
6. 国外渠道码不在拒绝集合，opensource/custom 均可用；未知版本继续由 `edition.WithContext` 归一化为 opensource。

**Join 与子查询评估**：

- `JOIN channels` 可实现同样语义且通常走索引，但需验证 GORM 租户 callback 是否会连带约束 joined `channels`，以及全局预设渠道行的 tenant 语义。
- 优先用参数化子查询把 gate 收敛为单行 scope，避免 joined 表的 tenant callback 干扰；若实测 join 不改变 tenant/system scope，可作为性能替代。
- 两者都只传 code 列表，edition 策略仍留在 Go 层，不属于 SQL 方言硬编码。

**Alternatives**：

- service 层逐调用方补判断：拒绝，直接 repository 调用与未来新调用方仍会绕过。
- 查询后在 Go 内存过滤：拒绝，`List` 分页会少页，`GetByIdentifyCode` 可能先触发歧义再过滤，且产生 N+1。
- 数据库 RLS/trigger：拒绝，版本策略会复制到数据库，迁移与测试成本高，违背单一策略源。

**Files likely to change**:

- `user-server/internal/pkg/edition/edition.go`
- `user-server/internal/repository/channel_account.go`
- `user-server/internal/repository/edition_channel_account_repository_matrix_test.go`（新增/回植）
- `user-server/internal/service/edition_channel_account_matrix_test.go`（新增/回植）
- `user-server/internal/repository/edition_channel_matrix_test.go`（保留既有渠道矩阵回归）

**Tests to add or update**:

- opensource/custom/unknown × `GetByID` / `List` / `GetByIDs`。
- `GetByIdentifyCode` 账号冗余码与权威识别码两条路径。
- 国外渠道在 opensource/custom 均可用，不被误伤。
- `PickAccount` 直传国内 `ChannelID` 在 opensource/unknown fail-closed，custom 可用。
- 跨租户 `GetByID` / `GetByIDs` 不泄漏；`GetByIdentifyCode` 仍保留 system scope 解析能力。
- 分页在 gate 后仍保持正确数量与顺序。

## Risks & Considerations

- `Create` / `Update` / `Delete` 不在本轮明确读取/选号范围内；opensource 仍可能通过写接口预置国内账号，需主智能体裁决是否同批收口。
- 子查询会排除孤儿账号（channel 行不存在），这是 fail-closed 方向，但需确认存量数据是否依赖孤儿账号。
- `GetByIdentifyCode` 的歧义判断发生在 gate 之后；国内多渠道账号在 opensource 应直接不可解析，而不是报歧义。
- repository 层 gate 会让 fake repository 单测不自动获得版本保护；服务矩阵必须使用真实 repository/test PG 才有效。
- 实际 `service/channel_account.go` 正被外部 lane 修改，本 assessment 阶段不修生产、不回植。

## Open Questions

- [已裁决：主智能体批准写侧同批收口，覆盖 `Create` / `Update` / `Delete` / `DeleteByChannel` / `SetIdentifyCodeByChannel`。]
- [已裁决：存量孤儿账号在 opensource/unknown fail-closed；custom 保持可读，但新 Create 仍拒绝孤儿渠道。]
