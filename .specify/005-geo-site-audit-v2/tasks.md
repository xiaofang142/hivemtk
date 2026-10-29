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

## 审计轮（2026-10-30，真站烟雾驱动）

- [x] T011 **textLen 灌水修复**：可见文本改从 DOM 文本节点收集（跳过 script/style/noscript/
  template 子树），字数按非空白 rune 计——修前整页 HTML 当文本，标签堆出来的页面白过
  CONTENT-LENGTH、品牌/G-SD-VISIBLE 匹配打到属性值上（回归：TestSiteAuditTagInflation）
- [x] T012 **markdown 协商误判修复**：判定改为前 4KB 无 HTML 骨架标记（<!doctype html/
  <html/<body）——修前只看前缀，`<!-- 注释 --><!DOCTYPE html>` 形态漏判（回归：
  TestSiteAuditMarkdownNegotiation 三形态）
- [x] T013 **G-LINKS 页内锚误伤修复**：仅 `#/`、`/#` SPA 路由计数违规，`#section`
  合法页内锚放行（回归：TestSiteAuditHashLinks）
- [x] T014 **非 200 正文短路**：WAF 挑战页/登录墙时 schema/content/brand/meta/signals
  短路为 PAGE-STATUS 一项，不再把错误页当正文审计出"title 不含品牌名"类误导结论；
  robots/llms/ai_discovery 走独立资源不受影响（回归：TestSiteAuditBodyBlockedShortCircuit；
  真站烟雾 https://xapptool.cn 实证：审计 UA 被 WAF 拦 403，报告如实指出）
- [x] T015 真站端到端烟雾（xapptool.cn）：抓取→解析→九类评分→修复清单全链路实证通过
