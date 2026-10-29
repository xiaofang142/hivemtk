---
description: "GEO 一键 autopilot 实施任务清单"
---

# Tasks: GEO 一键 autopilot

**Input**: `.specify/006-geo-autopilot/`（spec.md / plan.md）

## Phase 1: 数据与模型

- [x] T001 `model/autopilot.go`：GeoAutopilotRun（uuid PK + tenant_id + steps JSON 文本列 + GetSteps/SetSteps）
- [x] T002 `repository/autopilot.go`：Create/Update/Get/GetByID/List/HasRunning
- [x] T003 `internal/pkg/db/migrate.go` 登记

## Phase 2: 编排服务

- [x] T004 `service/autopilot.go`：Create（校验/并发闸/落库/异步启动）+ execute 步骤机（9 步）
- [x] T005 brand_check 硬失败路径；keyword_expand LLM 降级路径；suggest/longtail/save 复用 mining
- [x] T006 content_generate ×N（逐篇容错）；push/audit 可选步；summary 汇总
- [x] T007 进度落库（progress/current_step/steps 每步写回）+ WithoutCancel/15min 超时

## Phase 3: 接口与前端

- [x] T008 `dto/autopilot.go` + `controller/autopilot.go`（Create/List/Get，409/400 语义）
- [x] T009 `router/geo_routes.go` 挂 3 条路由
- [x] T010 `user-web` api/geoAutopilot.js + views/geo/Autopilot.vue + 路由 order 0

## Phase 4: 测试

- [x] T011 `autopilot_test.go`：repo CRUD + steps JSON 往返；品牌缺失硬失败；步骤状态机
  （无 LLM 降级不 panic、终态一致性、进度单调）；HasRunning 并发闸
- [x] T012 `go build ./...` + `go test ./internal/geo/...` 全绿；前端 vitest 不回归

## 审计轮（2026-10-30）

- [x] T013 **TOCTOU 修复**：HasRunning 与 INSERT 之间的并发窗口由 v3.87.0 部分唯一索引
  `(tenant_id) WHERE status='running'` 兜底，Create 把 23505 唯一冲突映射为
  ErrAutopilotRunning（409 语义）；回归 TestAutopilotUniqueIndexBackstop
- [x] T014 **Create 数据竞争修复**：goroutine 独占 run 指针后，响应返回浅拷贝，
  消除「goroutine 边写 HTTP 序列化边读」的竞争（-race 全绿）
- [x] T015 **评分提取修复**：ScoreContent 的 scores 字段启发式路径是 map[string]float64、
  LLM 路径是匿名 struct，修前类型断言两条全打不中 → 均分恒 0；改 JSON 往返
  （autopilotScoreTotal）对两种载体同构；回归 TestAutopilotScoreTotal
- [x] T016 **扩展词口径对齐**：Layer=related / Source=semantic_expand（与
  SemanticExpand 产出一致），修前留空落 GORM default 'seed' 灌水漏斗种子层
- [x] T017 **push_urls Domain 语义防御**：cfg.Domain 约定为裸主机名（site.go 同口径），
  配置误存完整 URL 时剥 scheme 后再拼，不再产出 https://https:// 坏链
