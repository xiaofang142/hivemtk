# Feature Specification: 模板/技能市场（F2 · 最小可用版）

**Feature Branch**: `013-template-skill-market`

**Created**: 2026-10-24

**Status**: Approved

**Input**: 用户指令「F2 模板/技能市场（最小可用版）—— 只读浏览 + 元数据 + 分类，不含支付/发布/审核闭环」。
本 spec 对应《开发计划-改进清单-2026-10-23.md》P2 项 **F2 模板/技能市场**
（原评估：「无模板/技能市场；只有 `internal/template/`（内容模板）」，重定级 P2，季度级项）。

> **S-22 范围补记（2026-10-29）**：第一轮按上方口径只交付只读橱窗；写侧
> （发布-审核-支付闭环 + controller/router/menu 接线 + i18n 9 语种）由未完成清单 S-22
> 立项收口——表 `market_catalog_entries`/`market_entry_orders`、双状态机、审核台与
> 订单接线、菜单两节点、`templateMarketPage` 9 语种段。详见本目录 delivery.md 顶部
> 「S-22 收口补记」与 tasks.md Phase 5。本节之前的正文保持第一轮口径不动（历史锚点）。

## 一、现状调研结论（Argumentation · 回仓取证 2026-10-24）

### 1.1 「内容模板」是什么（F2 唯一的相关现状）

仓库里叫「模板」的东西分两类，**不要混为一谈**：

| 类别 | 位置 | 形态 | 存储 |
|---|---|---|---|
| 内容模板（HTML） | `user-server/internal/template/`（`card_chat.html`、`dummy.html`）与 `user-server/internal/reach/card/template/`（`douyin/xiaohongshu/kuaishou/xianyu_card.html`、`live_code.html`、`wechat_share.html`） | 静态 HTML 文件，运行时由 Go `html/template` 解析执行 | 文件系统，**不是数据库表** |
| 模板渲染服务 | `user-server/internal/reach/card/template/template.go` | `TemplateService`：`GenerateDouyinCardPage` / `GenerateXiaohongshuCardPage` / `GenerateKuaishouCardPage` / `RenderXianyuCard` / `GenerateLiveCodePage` / `GenerateCardChatPage` | 无状态，`templateDir` 相对路径 `"internal/template"` |
| 业务模板（DB 表） | `model/sop_templates`、`model/integration_templates`、`model/whatsapp_message_template` | 回复话术 / 对接映射 / 渠道消息模板 | Postgres（GORM） |

取证位置：4 个渠道卡片服务与 `controller/live_code.go:301` 均以
`template.NewTemplateService("internal/template")` 构造渲染服务。**没有任何一个**是「市场」——
没有目录列表、没有分类、没有元数据查询接口，用户无法浏览「系统里都有哪些模板/技能」。

### 1.2 已存在的「市场」先例（设计对标，不重造）

| 先例 | 后端 | 前端 | 数据源 |
|---|---|---|---|
| 资产市场 | `service/asset_market.go` + `controller/asset_market.go` | `views/assetMarket/Market.vue` | 平台 API（`repository.PlatformAPIClient`） |
| 功能模块市场 | `service/module_market.go` + `service/module_catalog.go` + `controller/module_market.go` | `views/moduleMarket/Market.vue` | **进程内目录** `model.ModuleCatalogMeta` + DB 镜像（owned） |
| SOP 模板 | `service/sop_template.go` | `views/sopTemplateKb/` | DB 表 `sop_templates` |

**结论**：本仓已有「进程内目录 + 只读浏览」的成熟先例（模块市场 `ModuleCatalogMeta`）。
F2 复用这一形态——**不建表、先立契约**，用进程内注册的目录数据源演示。

### 1.3 技能（skill）的现状

仓库无「技能」统一概念，但有可被目录化的技能形态：`internal/middleware/mcp_guard.go`（MCP 工具接入）、
`internal/service/smart_router.go`（智能路由）、`internal/service/assignment.go`（会话智能分配）、
`internal/middleware/ltc_gate*.go`（Agentic LTC，开发计划 D1：骨架已存、闭环未实挂）。
它们与模板一样：**有实现、无目录**。

## 二、设计：目录层的最小数据面

### 2.1 元数据契约（前后端一致）

目录条目 `TemplateMarketEntry`（后端 `repository` 包，API 面字段名一致）：

| 字段 | 含义 | 取值/示例 |
|---|---|---|
| `id` | 目录唯一标识 | `card-douyin`、`skill-mcp-tool` |
| `kind` | 模板 or 技能 | `template` \| `skill` |
| `name` | 展示名 | `抖音卡片页` |
| `category` | 分类（筛选维度） | `channel_card`、`mcp`… |
| `summary` | 一句话简介 | `抖音渠道的落地卡片页模板…` |
| `source` | **事实源**（指向既有实现，不复制内容） | `internal/reach/card/template/douyin_card.html` |
| `availability` | 可用性 | `builtin`（内置可用）\| `needs_module`（需开通模块）\| `coming_soon`（占位） |
| `tags` | 辅助标签 | `["douyin","卡片"]` |

### 2.2 查询接口（列表 / 详情 / 分类）

```
GET /api/template-market/list?kind=&category=&keyword=&page=&page_size=   → { list, total }
GET /api/template-market/categories                                        → [ "agentic", "channel_card", ... ]（去重升序）
GET /api/template-market/detail/:id                                        → 单条；未知 ID 返回业务码 404 语义
```

过滤语义（service 层，顺序固定）：`kind` 精确匹配 → `category` 精确匹配 → `keyword`
在 name/summary/tags 上做忽略大小写的子串匹配。零值查询 = 全量（目录稳定序）。

### 2.3 与 `internal/template` 的关系（论证）

**F2 市场是现有模板/技能的「目录层」，不是新的内容层。**
- 不改 `internal/template/*.html` 与 `TemplateService` 的内容语义——目录条目的 `source` 字段只是
  **指向**那些实现的事实源，渲染仍走原路径。
- 不新建数据库表：目录是编译期种子（进程内注册），写侧（发布/审核）未立项前建表只会制造
  「一份事实抄两份且无同步路径」的裂脑，与 E1/模块市场先例的论证一致。
- 与模块市场的关系：模块市场是「功能模块」的购买入口（有订单/支付），F2 市场是「模板/技能」的
  **只读橱窗**（无订单），两者并列，互不替代。

## User Scenarios & Testing

### US1 · 浏览目录（Priority: P0）

运营/管理员打开「模板/技能市场」，看到全部条目卡片（名称、类型、分类、可用性、简介），
未登录也可逛（公开橱窗，对齐 `module-market/list`）。

**Independent Test**：`GET /api/template-market/list` 返回 13 条（9 模板 + 4 技能），
每条含契约要求的全部字段；前端页面按卡片渲染、无 500。

### US2 · 分类与关键词过滤（Priority: P0）

按类型（模板/技能）、分类、关键词过滤；分类列表去重升序返回。

**Independent Test**：`kind=skill` 返回 4 条；`category=channel_card` 返回 5 条；
`keyword=SOP`（忽略大小写）命中 `sop-reply`；不存在的分类返回空列表（非 nil）。

### US3 · 详情与未知 ID fail-closed（Priority: P0）

点卡片看详情（ID/类型/分类/可用性/简介/来源）；未知 ID 返回明确的「查无」错误而不是 500。

**Independent Test**：`Get("card-douyin")` 返回完整字段；`Get("no-such")` / `Get("")` /
`Get("   ")` 均返回 `errors.Is(err, ErrMarketEntryNotFound)` 且条目为 nil。

## Requirements

- **FR-1** 目录条目承载于进程内注册的数据源（`repository.MemoryTemplateMarketRepository`），不建数据库表。
- **FR-2** 元数据契约字段：`id/kind/name/category/summary/source/availability/tags`，字段名即前后端契约。
- **FR-3** 仓储接口只暴露 `All` 与 `Get` 两个只读方法；`Get` 未命中返回 `(nil, nil)`（仓库层惯例）。
- **FR-4** service 层过滤顺序：kind → category → keyword；keyword 忽略大小写，命中 name/summary/tags。
- **FR-5** 未知/空 ID 一律 fail-closed 返回哨兵 `ErrMarketEntryNotFound`，不翻译成 500。
- **FR-6** `Categories` 去重 + 升序，供前端筛选器使用。
- **FR-7** 目录种子每条 `source` 必须指向仓库内真实存在的实现；`coming_soon` 条目必须如实标注。
- **FR-8** 全部查询/过滤/未知 ID 行为有纯单元测试覆盖（不依赖数据库）。

## Key Entities

- **TemplateMarketEntry**（值对象）：目录条目（元数据契约）。
- **TemplateMarketRepository**（接口）+ **MemoryTemplateMarketRepository**（进程内实现）：目录数据源。
- **TemplateMarketService**：查询/过滤/分类聚合。
- **MarketQuery**：查询条件（kind/category/keyword）。

## Success Criteria

- **SC-1** `go test ./internal/service/ -run 'TemplateMarketService'` 与
  `go test ./internal/repository/ -run 'Market|MemoryTemplateMarket'` 全绿。
- **SC-2** `gofmt -l` 对新增 4 个 Go 文件干净；`go vet`（service/repository）干净。
- **SC-3** 前端 `npm run build` 通过；新增渲染用例 `tests/unit/templateMarket_render.test.js` 全绿。
- **SC-4** 未知 ID fail-closed 语义有显式用例钉住（errors.Is + nil 条目）。
- **SC-5** 不改动 `internal/template`、`internal/reach/card/template`、`model/module_tables.go`，
  不新建任何数据库迁移。
