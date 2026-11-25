# CH-20261125-01~10 修复记录

日期：2026-11-25

## 已实施（核心判据）

### CH-01 预设渠道初始化与租户启用
- `user-server/internal/service/channel.go`
  - `EnsurePresets` 改为在 `channels.code` 全局唯一模板行（TenantID=0）落库，不再按租户重复播种；幂等不覆盖改名/停用。
  - 新增 `EnsureTenantPresets` / `IsEnabledForTenant` / `RequireEnabledForTenant` / `SetPresetEnabledForTenant`：租户启用配置复用 `system_config_kv`（不新建表）写 `channel_preset:<code>`，两租户互不影响；未配置默认启用、脏值 fail-closed。
  - `EnabledOptions` 改为全局模板 + 租户自定义渠道，按租户 KV 过滤；`ensureOnDemand` 经已有 controller/options 调用链接通生产初始化（空库/升级/新租户）。
- `user-server/internal/repository/channel_preset.go`：ChannelPresetStateRepository（Get/Set，租户 KV，密钥构造纯函数）。
- 反例测试：`channel_preset_mgmt_test.go`（空库播种、重复幂等、两租户启停隔离、自定义渠道可见/租户隔离、脏值 fail-closed、ParseEnabledValue 正反例）。

### CH-02 平台账号配置集中管理
- `user-server/internal/service/channel_account_credential.go`：凭据存储契约（system_config_kv，key 含显式 tenantID+accountID，不改 schema）；`MaskCredential` 脱敏、`MergeCredential` 脱敏占位不覆盖真实 secret、`ValidateCredentialForPlatform` 按 TG/QQ/飞书/企微/WhatsApp 校验；`MigratePreview`/`MigrateApply` 稳定映射 + 幂等复用不重建 + 冲突拒绝 + 稳定排序（反向创建顺序一致）。
- `user-server/internal/repository/channel_account_credential.go`：KV 仓储（tenant_id+key 二次隔离，OnConflict 幂等）。
- 生产装配：`NewChannelAccountCredentialServiceDefault`。
- 反例测试：`channel_account_mgmt_test.go`（脱敏/合并/清空/平台校验/迁移稳定与幂等）。

### CH-03 账号管理 CRUD 备注与停用找回
- `user-server/internal/service/channel_account_mgmt.go`：`ChannelAccountManageService.ListManaged` 默认全部（含停用/备注），复用仓储共享过滤的 `CountManaged` 保证 total 口径一致；`UpdateManaged` 指针字段 nil=不改、空串=明确清空，详情加载失败阻止提交；`SetStatus` 仅改状态。管理列表与业务下拉刻意分开，不用业务默认过滤掩盖管理对象。
- `user-server/internal/repository/channel_account_mgmt.go`：`managedBase` 唯一过滤构造口供 List/Count 共用。
- 反例测试：过滤/分页/第二页 total/跨租户、仅改名称保留备注与自他名称/类型、空提交不写库、明确清空、详情失败阻止提交、停用找回与非法状态拒绝。

### CH-04/05 统一查询与状态契约
- `user-server/internal/service/channel_status.go`：CH-05 先交统一状态判定/变更契约 `RequireRunnable`（渠道+账号同时启用、账号归属渠道、查询失败上抛）、`AccountEnabled`、`ChannelEnabled`，供 CH-07/LEAD-03 消费；全入口 worker 退出矩阵列为余项，不形成互等。
- `user-server/internal/service/channel_resolver.go`：CH-07 在 `ResolveTenant` 识别码命中后统一复核可信 ctx/渠道一致性，新增 `ResolveTrusted`（识别码+平台映射+可信 ctx 联合校验，冲突拒绝、显式账号不一致拒换号、渠道/账号停用拒绝）；CH-08 重写 `Identify` 单聊/群聊自他判定，移除识别码/显示名/内部账号 ID 当 self 候选。
- `user-server/internal/service/channel_identity.go`：CH-08 `IdentifySpeaker` 接受平台稳定 self ID + 群成员角色映射，同名/角色冲突歧义拒绝，`IsUnknownIdentity` 供业务闸使用。
- 反例测试：`channel_contract_test.go`（状态双启、停用/错渠道/缺 ID/查询故障；ctx/渠道/账号冲突；稳定 self、unknown 不猜、群角色冲突）。

### CH-06 识别码多账号精确定位与原子重置
- `user-server/internal/repository/channel_identify_code.go`：`RotateCAS`（按期望版本条件更新，冲突 ErrIdentifyVersionConflict；首次生成 OnConflict DoNothing），包级 `findValidIdentifyCode` 统一当前码/窗口内旧码判定（闸门与解析一致，不改接口避免破坏既有替身）。
- `user-server/internal/service/channel_identify.go`：`rotate` 先 CAS 写权威码（版本单调、并发受控、失败不半写），冗余列只写给显式指定账号或按渠道清空（消除多账号同码与唯一索引冲突）；新增 `CreateOrRotateForAccount`（缺/错账号拒绝，不取首条）。
- `user-server/internal/repository/channel_account.go`：`GetByIdentifyCode` 权威码路径改用 `findValidIdentifyCode`；多账号返回歧义错误，不取首条。

### CH-09 统一入站归档与业务触发
- `user-server/internal/service/channel_inbound_orchestrator.go`（独立于 `inbound_message.go`，不改既有链）：`DeliverAndArchive` 做去重→乱序→身份→持久化→业务触发，返回显式 `InboundOutcome`（archived/duplicate/out_of_order/rejected/persist_failed + automation 决策），防双写双触发，unknown 跳过猜测自动回复；失败显式 error，不伪装成功。

### CH-10 均衡调度三路径
- `user-server/internal/service/channel_dispatch.go`：粘性命中必须校验 `ChannelID`，错误渠道显式拒绝（不串发、不静默换号）；候选分页循环拉取全部启用账号（不再固定 500），会话哈希/轮转口径不变。
- 反例测试：跨渠道粘性拒绝、600 候选分页覆盖末位账号。

## 余项（未整项关闭，不删清单行）

- CH-01：main.go 显式启动装配不在本批边界；现经 `EnabledOptions` 按需幂等自愈接通，未做真实生产启动/升级演练。
- CH-02：各平台旧页/旧 API 的实际 UI 表单迁移与凭据字段逐平台联调未做（仅服务层契约 + 稳定映射）；未做真实平台凭据收发回归。
- CH-03：Controller 管理 List 仍复用 Options（未改 controller 层），本批提供独立 `ListManaged` 服务/仓储供后续装配；前端详情失败禁用提交未接线（页面改动未涉及）。
- CH-04：telegram/gate.vue、customer360/List.vue 旧消费者未迁移；员工授权选项过滤与旧平台 ID 映射的实际调用位置清单未产出；未做两租户/多员工 UI 验证。
- CH-05：全入口 worker 退出/重启不重启停用账号的运行验收留余项（按二审口径随 CH-07/LEAD-03/09 集成后补）。
- CH-06/07：真实 PG 并发重置 CAS、重置前后全入口（query/header、TG hook/polling、Bridge、API、SageX）端到端矩阵未跑（纯函数/stub 已覆盖核心反例）。
- CH-08：真实消息入口接线（各平台稳定 userID 来源与群成员映射读取）未做，仅契约与判定函数。
- CH-09：`InboundArchiveRepository`/`InboundBusinessTrigger` 与既有 MessageHub/Inbox/双 Agent 的实际适配实现未接线；重放/乱序/解析失败已显式产出 Outcome，真实持久化失败对账未跑。
- CH-10：API 获取下发、hook 收发三路径 ingress→inbox→业务→下发→回执的端到端验证未做；仅选号/粘性/分页核心判据 GREEN。
- DB 集成测试：未在本批运行（主 agent 全量 service 回归窗口 + 测试库槽位冲突风险），核心判据以纯函数/内存替身覆盖；PG 用例列为余项。
