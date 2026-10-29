---
description: "GEO 站点审计 v2 实施任务清单"
---

# Tasks: GEO 站点审计 v2

**Input**: `.specify/005-geo-site-audit-v2/`（spec.md / plan.md）

## Phase 1: 评分引擎（纯函数）

- [x] T001 `service/site_audit.go` 类型与分值表：`SiteResources` / `SiteAuditCheck` /
  `SiteAuditCategory` / `GeoSiteAuditReport`；rubric v2 九类分值常量
- [x] T002 google_ai 八检查（G-INDEX/G-SNIPPET/G-CANONICAL/G-DATES/G-BYLINE/G-LINKS/G-VIEWPORT/G-SITEMAP，source 链接随检查项输出）
- [x] T003 robots 类（存在性 + search 层三 bot + sitemap 声明；training 层只提示不打分）
- [x] T004 schema / content / brand_entity / meta / llms / signals / ai_discovery 七类
- [x] T005 汇总：分类得分合计 = 总分；`buildSiteFixes` 按分值降序出修复清单
- [x] T006 `fetchSiteResources`：≤8 请求、10s 单请求超时、2MB body 上限、只 GET 目标域资源

## Phase 2: 接线

- [x] T007 `controller/tech_metrics.go` 加 `SiteAudit`（URL 校验、400 语义错误）
- [x] T008 `router/geo_routes.go` 挂 `POST /geo/techconfig/site-audit`

## Phase 3: 测试

- [x] T009 `site_audit_test.go`：满分配置夹具全绿；逐类缺陷夹具断言扣分与 fixes 排序；
  robots 三层分级语义；llms.txt 结构校验；恶意输入（超长 body、坏 JSON-LD）不 panic
- [x] T010 `go build ./...` + `go test ./internal/geo/...` 全绿
