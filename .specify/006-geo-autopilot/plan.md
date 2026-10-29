# Implementation Plan: GEO 一键 autopilot

**Branch**: `006-geo-autopilot` | **Date**: 2026-10-29 | **Spec**: [spec.md](./spec.md)

## Summary

新增 GEO 域第五个业务子域 `autopilot`：单关键词驱动的即时全链路管线。
复用既有 service 能力（KeywordService.SemanticExpand / KeywordMiningService.CrawlSuggest、
CombineLongtail、SaveMiningResults / ContentService.GenerateContent、ScoreContent、
GenerateSchema / PushService.PushURLs / TechConfigService.RunSiteAudit），
新增的只有**编排**（步骤状态机 + 运行记录）。

## Technical Context

**Language/Version**: Go 1.26（user-server）；Vue 3 + Element Plus（user-web）
**Storage**: 新表 `geo_autopilot_runs`（AutoMigrate 登记于 `internal/pkg/db/migrate.go`）
**Testing**: `go test ./internal/geo/...`（PG 测试库）+ vitest（前端）

## Architecture（五层）

```
Router: geo.POST("/autopilot/runs", ctrl.Create) / geo.GET("/autopilot/runs", ctrl.List)
        geo.GET("/autopilot/runs/:id", ctrl.Get)
Controller: dto 绑定 + 调 service（409 语义由 service 返回 sentinel error 映射）
Service: AutopilotService
  - Create(ctx, req) → 落库 running 行 → go execute(detachedCtx)
  - execute: 步骤状态机，每步写回 steps/progress/current_step（无 LLM 依赖的步骤可测）
  - List / Get
Repository: GeoAutopilotRunRepository（Create/Update/Get/List/HasRunning）
Model: GeoAutopilotRun（uuid PK、tenant_id、steps JSON 文本、GetSteps/SetSteps 助手）
```

步骤装配全部走**既有构造器**（与 keyword_pipeline.go 的 `NewXxxRepository()`/
`NewXxxService(...)` 同形态），不重复建仓。

## Steps JSON 契约

```json
[{"name":"brand_check","label":"品牌配置检查","status":"success","detail":"...","duration_ms":12,"result":{...}}]
```

status ∈ running|success|skipped|failed；progress = 完成步数/总步数 × 100。

## Files

| File | Change |
|---|---|
| `internal/geo/model/autopilot.go` | 新增模型 + steps JSON 助手 |
| `internal/geo/repository/autopilot.go` | 新增仓储（5 方法） |
| `internal/geo/service/autopilot.go` | 新增编排服务（步骤机 + 复用既有服务） |
| `internal/geo/dto/autopilot.go` | 请求/响应 DTO |
| `internal/geo/controller/autopilot.go` | 3 个 handler |
| `internal/router/geo_routes.go` | 3 行映射 |
| `internal/pkg/db/migrate.go` | 登记模型 |
| `internal/geo/service/autopilot_test.go` | 编排单测（mock 不可行处走降级路径 + repo 集成） |
| `user-web/src/api/geoAutopilot.js` | API 出口 |
| `user-web/src/views/geo/Autopilot.vue` | 页面 |
| `user-web/src/router/modules/geoTools.js` | 路由（execute 组 order 0） |

## Verification

`cd user-server && go build ./... && go test ./internal/geo/...`；
`cd user-web && npm run test`（既有用例不回归）。
