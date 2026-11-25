# CH-20261125-01~10 施工前评估（valid 判定与边界）

日期：2026-11-25
判据源：`../../../../docs/未完成任务清单.md` §3.29 第 481–529 行原文（含二审“状态契约先交、不全入口互等”口径）
施工 agent：渠道域 agent（主 agent 派发）

## 1. valid 判定

判据 valid。十项均指向明确的现行代码路径，且原文与仓库实况可对齐：
- `service/channel.go` 有 `EnsurePresets` 但生产装配点没有调用（grep 全仓仅 controller 持有 `presetSvc` 供 `EnabledOptions` 使用）。
- `service/channel_account.go` 有统一 `ListAccounts/ListOptions`，`GetDetail` 已全字段；但 Controller 管理 List 仍复用只带启用账号+无备注的 Options，total 用返回行数。
- `service/channel_identify.go` 先读 version 再 `Upsert`，仓储 `Upsert` 无版本比较；`SetIdentifyCodeByChannel` 在非事务内独立双写。
- `service/channel_resolver.go` 有 `Identify`，但真实消息入口调用未接线；`ResolveTenant` 在识别码成功后未统一复核渠道与账号状态。
- `service/channel_dispatch.go` 粘性命中只校账号 Status、不校 ChannelID；候选固定 `Limit:500`。
- `service/inbound_message.go` 的 `Deliver` 只补租户/账号并打日志，解析失败 Warn 后仍返 nil，无持久化/业务触发的可查状态。

## 2. 逐项缺口（施工前快照）

| 项 | 缺口摘要 |
|---|---|
| CH-01 | 预设播种没有生产初始化入口；`EnsurePresets` 用全局 code 跳过，租户 2 拿不到预设；新租户启用配置没有落库；自定义渠道可见性未验。 |
| CH-02 | 平台账号配置（TG/QQ/飞书/企微/WhatsApp）仍在各自平台页与 API，未在统一账号管理按类型展示/脱敏/稳定映射；无存量迁移可预览与幂等。 |
| CH-03 | 管理 List 走 `ListOptions`（默认仅启用、total=行数、无备注）；前端详情失败后可继续保存、备注未回填；停用账号在管理列表找不到。 |
| CH-04 | 多处仍走旧平台列表/硬编码来源选项（telegram/gate.vue、customer360/List.vue），`channel_dispatch.go` 直读账号仓储；员工授权过滤与旧平台 ID 映射未接入统一查询。 |
| CH-05 | `RequireEnabled` 无业务调用；Resolver/Dispatch 不统一校渠道 Enabled；状态契约未抽出供 07/LEAD-03 消费；全入口 worker 退出未接。 |
| CH-06 | 识别码多账号时冗余列全局唯一冲突，`SetIdentifyCodeByChannel` 给同渠道所有账号写同码；闸门 `ValidCodes` 认旧码但 `GetByCode` 只认当前码；无 CAS/串行版本。 |
| CH-07 | webhook `bindCallbackTenant` 只换 tenant 不换 account；`ResolveTenant` 已绑定租户直接返回；TG polling 回环走旧数字账号路径；query/header 入口身份一致性未统一。 |
| CH-08 | `Identify` 无真实入口消费；单聊把识别码/账号 ID 当 self 候选；unknown 归客户无显式语义与业务闸。 |
| CH-09 | `Deliver` 不落库、不归档、不触发业务，失败只 Warn；重放/乱序/解析失败不可查。 |
| CH-10 | 粘性不校 ChannelID；候选固定 500 条；三路径（API 获取下发/均衡收发/hook）未贯通到下发与回执。 |

## 3. 施工边界（严格遵守）

- 可改：`service/channel.go`、`service/channel_master*.go`、`service/channel_account.go`（仅 CH-02/03 的 CRUD/备注/停用过滤；LEAD agent 同文件改授权/Agent 绑定，动手前重读）、`service/channel_resolver.go`、`service/channel_dispatch*.go`（仅调用点接线）、`repository/channel*.go`（scoped 查询）、新建对应 `_test.go`、新建本规格目录、必要 `user-web/src/views/channels/` 与 `src/api/channel*.js` 接线。
- 严禁改：`customer_*.go`、`lead_*.go`、`auto_reply.go`、`inbox*.go`、`outbound_*.go`、`webhook_*.go`、`router/*`、任何已存在 `.specify/bugs/` 目录。
- 不改数据库 schema、不 commit/push/发布/外发、不做 UI 真机点验。
- 契约先交：CH-05 的统一状态判定/变更契约先就绪供 CH-07/LEAD-03 消费；不要求 CH-07 等 CH-05 全入口矩阵关闭；不形成互等。
- 并行保护：主 agent 正在跑全量 service 回归（约 10 分钟）；DB 测试延后到该窗口结束，使用 `POSTGRES_TEST_*` 从 `../.env` 注入、端口 8217、单包串行 `-p 1 -parallel 1`。
- 工作树有并行 agent 未提交产物；build 中间态失败若不在本边界内，等待重试并如实记录，不越界修。
