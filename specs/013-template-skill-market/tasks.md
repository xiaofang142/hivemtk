# Tasks: 模板/技能市场（F2 · 最小可用版）

**Feature**: `013-template-skill-market` ｜ 勾选即以「已验证」为准（无验证不算完成）

## Phase 1 目录数据源（`internal/repository/template_market_repo.go`，新建）

- [x] T001 定义契约常量：`EntryKindTemplate/EntryKindSkill`、`AvailabilityBuiltin/NeedsModule/ComingSoon`
- [x] T002 定义契约值对象 `TemplateMarketEntry`（id/kind/name/category/summary/source/availability/tags）
- [x] T003 定义只读接口 `TemplateMarketRepository`（All / Get；Get 未命中返回 nil,nil）
- [x] T004 实现 `MemoryTemplateMarketRepository` + `New...` / `New...With`（测试注入）；All 返回副本
- [x] T005 编写种子目录 `marketCatalogSeed`（9 模板 + 4 技能，source 逐条指向真实实现）
- [x] T006 测试：种子契约完整性（ID 唯一/值域合法/字段非空/双 kind 覆盖）+ All 副本语义 + Get 命中与未命中

## Phase 2 查询服务（`internal/service/template_market.go`，新建）

- [x] T007 定义哨兵 `ErrMarketEntryNotFound` 与 API DTO `MarketItem` / `MarketQuery`
- [x] T008 实现 `List`（kind → category → keyword 过滤，目录稳定序）
- [x] T009 实现 `Categories`（去重 + 升序）
- [x] T010 实现 `Get`（TrimSpace 空 ID → 哨兵；仓储 nil → 哨兵；命中 → DTO）
- [x] T011 测试：表驱动过滤（全量/kind/category/keyword 大小写/tags 命中/叠加/空结果）、
  稳定序、Get 命中、未知/空/空白 ID fail-closed、仓储错误透传

## Phase 3 前端（独立文件，不碰共享 router/i18n）

- [x] T012 新增 `src/api/templateMarket.js`（list/categories/detail + 展示常量）
- [x] T013 新增 `src/views/templateMarket/Market.vue`（卡片网格 + 类型/分类/关键词过滤 + 详情抽屉 + 空态）
- [x] T014 新增 `src/router/modules/templateMarket.js`（懒装配模块文件，登记点见 delivery.md）
- [x] T015 测试 `tests/unit/templateMarket_render.test.js`：渲染/关键词过滤/空态 3 例

## Phase 4 验证与交付

- [x] T016 后端：`gofmt -l` 干净、`go build`（service/repository，含他人在途冲突说明）、`go vet`、
  service 与 repository 定向 `go test`
- [x] T017 前端：`npx vitest run tests/unit/templateMarket_render.test.js` 全绿、`npm run build` 通过
- [x] T018 四件套：`specs/013-template-skill-market/{spec,plan,tasks,delivery}.md`
- [x] T019 delivery.md 写清：改动文件、测试命令与结果、未实现/留待后续决策点、建议接线点（controller 代码 + router 登记 + menu 节点）

## Phase 5 S-22 收口：发布-审核-支付闭环（2026-10-29）

- [x] T020 model：`market_catalog_entries`（平台级、无 tenant_id）+ `market_entry_orders`
  （带 tenant_id）两张表 + 双状态机（目录条目 pending/approved/rejected/disabled、
  订单 pending_payment/paid/cancelled）+ allModels() 登记 + module_tables.go core 归属
- [x] T021 repository：`MarketEntryRepository`（条目/订单存取 + 条件跃迁乐观并发）+ 真库用例 3 条
  （平台级匿名读不受租户闸拦、条件跃迁 0 行语义、订单租户隔离）
- [x] T022 service：`MarketEntryService`（提审/审核/下架复架/下单/撤单/收款确认/我的三表）
  + `TemplateMarketService` 合并 DB approved 条目上橱窗（读失败降级纯种子）+ 真库用例 4 条
  （主线闭环 / 免费直通 / fail-closed 面 / 租户 on 隔离）
- [x] T023 controller/router：`controller/market_entry.go` 9 端点；module_routes.go
  auth 档 6 条 + tenantAdmin(admin) 档 4 条；哨兵→业务码映射（查无/越态/重复/参数不落 500）
- [x] T024 菜单：menu.go templateMarketMy/templateMarketReview 两节点 +
  module_bindings.go platform-link 归属（menu_wiring/menuRegistry 双侧门通过）
- [x] T025 前端：api/templateMarket.js 闭环 API 与展示常量；路由模块补 /template-market/my
  与 /template-market/review；MyWorkspace.vue / ReviewConsole.vue / PublishForm.vue；
  Market.vue 工作台入口（发布弹窗/我的/审核台跳转，v-if 门保证匿名与冻结单测安全）
- [x] T026 i18n：9 语种追加 menu 两键 + templateMarketPage 段 52 键（键值 9 语种 parity）
- [x] T027 验证：go build ./... 干净、gofmt/vet 干净、五个后端包全量真库测试绿、
  router 面全套绿；npm run build 绿 + 四份冻结门用例绿；delivery.md 补 S-22 收口补记
