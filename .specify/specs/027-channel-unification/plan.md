# Implementation Plan: 027-channel-unification

## Technical Context
- 后端 Go（Gin + GORM + PostgreSQL），五层 Router→Controller→Service→Repository→Model，禁跨层。
- 前端 Vue 3 + Element Plus + Pinia，api 层只放请求函数，named import http。
- 运行期 schema 事实源：GORM AutoMigrate + `internal/migration/migrations` Go 迁移；顶层 `.sql` 为离线对照。

## Constitution Check
- 复用既有 Channel/Account/IdentifyCode 主数据，不另起渠道系统。
- 租户隔离 fail-closed：tenantID=0 一律拒绝。
- 不新增业务错误码，复用既有 domain error。
- 新增错误处理遵循 `%w` 包装；Controller 只做参数绑定与响应。

## 设计决策
1. **统一 API 采用组合端点** `GET /api/channels/catalog`，一次返回 `{channels:[...], accounts:[{...channel_id}]}`；现有 `/channels/options`、`/channel-accounts/options` 保持兼容并补充中文标签字段。
2. **渠道开关**：预设渠道用 `SetPresetEnabledForTenant`（KV），非预设用租户行 Enabled；Controller 以 channel id 定位 code 后分派。
3. **前端单一标签源**：新增 `stores/channelCatalog.js` Pinia store 作为运行时唯一标签/选项来源；`constants/channel.js` 收敛为纯静态降级字典（一次维护），各视图不得再自定义数组。
4. **历史数据**：新增顶层 SQL `100_channel_code_unify.sql` + Go 注册迁移 `v3_116_0`：
   - 补齐全局预设渠道行（TenantID=0，规范 code + 中文名）；
   - 对已识别业务表的旧码做规范映射 UPDATE（幂等）；
   - 不新增 FK 列（避免 GORM 模型大范围改动），标签映射由统一字典承担。
5. **识别码**：账号级重置走既有 `POST /api/identify-codes/:channel_id/rotate-for-account`，前端补 API 封装与按钮。

## Phases
- Phase 0 research：`research.md`（已调研结论固化）。
- Phase 1 design：`data-model.md`、`contracts/channel-catalog.md`、`quickstart.md`。
- Phase 2 后端：DTO/Service/Controller/Router + 迁移。
- Phase 3 前端：API/store/组件 + 各视图收口。
- Phase 4 验证：build/vet/lint/迁移验证；converge 复盘。

## 触碰点（文件）
- `user-server/internal/dto/channel.go`、`internal/service/channel*.go`、`internal/controller/channel*.go`、`internal/router/channel_routes.go`
- `user-server/internal/migration/migrations/v3_116_0_*`、`internal/migration/migrations/initial_schema.go`、`migrations/100_channel_code_unify.sql`
- `user-web/src/api/channelCatalog.js`、`src/stores/channelCatalog.js`、`src/components/ChannelSelect.vue`、`src/components/ChannelAccountSelect.vue`、各业务视图
