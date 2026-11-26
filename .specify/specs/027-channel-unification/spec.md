# Feature Specification: 渠道与渠道账号统一配置（027-channel-unification）

**Created**: 2026-11-26
**Status**: Draft（本轮实现口径由用户 question 工具确认：完整实现，含迁移验证）
**Input**: 用户原始需求（原样保留）

```
3 统一的渠道管理和数据
  1 平台设置商户类型 开源版还是 定制版 平台端哪里设置、哪里控制 默认开源版 
  2 用户端 渠道管理  1 开关渠道（只有开关） 2 给渠道配置账号 一个渠道可以多个账号 每个账号唯一识别码
  3 封装统一api  返回渠道列表  渠道下账号列表  寻找系统 所有 渠道使用 1 使用下拉（已开启的渠道）不允许输入   如 线索、客户、授权等 全部找出来修改 不仅仅 user-web 的ui交互 数据，历史数据，均需要修改 
   4  user-web 列表等涉及显示  渠道、账号的  的地方 不要编码 而是关联显示对应汉字 且统一维护 不允许四处定义 
```

## User Scenarios & Testing

### US1 — 平台端商户类型默认为开源版（P1）
平台管理员创建商户时不选版本类型即落 `opensource`；编辑下拉可改 `custom`；存量空值在 UI 与下发链路按 `opensource` 处理。
**Independent Test**：创建商户不传 version_type，查询返回 opensource；DB 默认值与存量更新 SQL 可重复执行。

### US2 — 用户端渠道开关与账号配置（P1）
租户管理员在渠道管理页只能开关渠道（预设渠道走租户 KV，非预设走租户行）；账号管理页可一个渠道配置多个账号，每个账号有唯一识别码，可停用/启用并找回。
**Independent Test**：关停渠道后统一 API 不再返回该渠道；停用账号后业务下拉不再返回该账号，管理列表按「全部/停用」可找回；账号级识别码可重置且仅影响该账号。

### US3 — 统一 API 与全站下拉收口（P1）
系统提供单一 API 返回「已启用渠道列表 + 渠道下已启用账号列表」；线索、客户、授权、触达等所有渠道/账号选择点一律下拉，禁止自由输入；历史数据中的旧渠道码归一到规范码。
**Independent Test**：构造两套租户分别关停不同渠道，统一 API 返回互不影响的列表；前端选择项中不存在自由文本输入；迁移后历史旧码归一并被下拉/中文标签正确识别。

### US4 — 中文显示统一维护（P2）
user-web 中所有显示渠道/账号的地方不再显示编码，统一通过同一份字典/API 映射为汉字；禁止各视图重复定义渠道字典。
**Independent Test**：grep 校验新增后仅一处渠道标签映射；列表页显示汉字而非 code。

## Functional Requirements

- **FR-001** 平台商户 `version_type` 的 DB 默认、GORM 默认、创建请求缺省均为 `opensource`；存量空值一次性更新。
- **FR-002** 平台商户创建/编辑表单含版本类型下拉（开源版/定制版），编辑时空值显示开源版。
- **FR-003** 用户端提供租户级渠道开关 API（预设渠道 KV，非预设渠道行状态），缺租户 fail-closed。
- **FR-004** 提供统一 catalog API：一次返回已启用渠道（含 id/code/中文名）与按渠道分组的已启用账号（含 id/名称/识别码）。
- **FR-005** 提供管理面账号 API：含停用账号的分页列表、详情、编辑、启停、账号授权。
- **FR-006** 识别码支持账号级重置，且不影响同渠道其他账号。
- **FR-007** user-web 所有渠道选择统一走 catalog API（只读已启用），禁止 el-input 自由输入渠道/账号。
- **FR-008** user-web 所有渠道/账号显示统一走单一标签映射（catalog 返回值 + 唯一字典文件），不显示裸 code。
- **FR-009** 历史数据迁移：旧渠道码归一为规范码；渠道主数据补齐规范渠道；迁移可重复执行并有验证证据。

## Success Criteria

- SC-001 统一 API 的渠道列表在开源版下不含国内渠道，定制版含；两租户开关互不影响。
- SC-002 前端渠道/账号选择点零自由输入；显示零裸编码。
- SC-003 `go build ./... && go vet ./...`、前端 lint、迁移验证全部通过并留证。

## Key Entities

- Merchant（version_type）— Channel（预设/自定义、enabled）— ChannelAccount（channel_id、identify_code、status）— ChannelIdentifyCode（channel_id/account_id、version）— ChannelAccountAuthorization。
- 历史业务表 channel code 字符串（归一对象）。
