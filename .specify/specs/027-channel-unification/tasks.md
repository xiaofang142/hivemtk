# Tasks: 027-channel-unification

**Input**: `.specify/specs/027-channel-unification/`
**Format**: `[ID] [P?] [Story] Description`

## Phase 1: Setup
- [x] T001 建立 spec/plan/research/data-model/contracts/quickstart 文档
- [x] T002 平台端商户类型默认 opensource（独立仓，子代理执行；证据 `hivemtk-platform/docs/channel-unification-merchant-type.md`）

## Phase 2: Foundational（后端）
- [x] T003 [US3] 新增 DTO：`dto/channel_catalog.go`（ChannelCatalogItem/AccountCatalogItem/ChannelCatalogResponse）
- [x] T004 [US3] Service：`service/channel_catalog.go` 组合 EnabledOptions + ListOptions，按 channel_id 分组，补中文标签
- [x] T005 [US3] Controller + Router：`GET /api/channels/catalog`
- [x] T006 [US2] Controller + Router：租户渠道开关 `PUT /api/channels/:id/tenant-enabled`
- [x] T007 [US2] Controller + Router：管理面账号 `GET /api/channel-accounts/managed`、`PUT /:id/managed`、`PUT /:id/status`
- [x] T008 [US2] Controller + Router：账号授权 `GET /api/channel-accounts/authorization-options`、`GET/POST /:id/authorizations`、`DELETE /:id/authorizations/:account_id`（复用 AccountAuthorizationService）
- [x] T009 [US2] 前端 API 补 `rotate-for-account` 与上述端点（授权仍走既有 businessAccountAuth API；目录/开关/管理端点已补）

## Phase 3: 前端收口
- [x] T010 [US4] Pinia store `stores/channelCatalog.js`（唯一运行时标签源 + 单次加载）
- [x] T011 [US3] 公共组件 `components/ChannelSelect.vue`、`ChannelAccountSelect.vue`（仅已启用、禁输入）
- [x] T012 [US3] 渠道/账号管理页改造：开关-only、账号停用找回、账号级识别码
- [x] T013 [US3] 线索/客户/授权/触达等视图下拉替换（逐页列出于 progress）
- [x] T014 [US4] 列表显示中文标签替换，清理视图内硬编码字典

## Phase 4: 数据迁移与验证
- [x] T015 [US3] SQL `migrations/100_channel_code_unify.sql` + Go `v3_116_0` 注册（规范码归一 + 预设补齐）
- [x] T016 验证：go build/vet、前端 lint、迁移幂等验证；converge 复盘

## Dependencies
US2 → US3 → US4；T015 与 T003-T014 可并行（不同文件）；T016 最后。
