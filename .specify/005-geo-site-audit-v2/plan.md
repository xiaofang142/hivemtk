# Implementation Plan: GEO 站点审计 v2

**Branch**: `005-geo-site-audit-v2` | **Date**: 2026-10-29 | **Spec**: [spec.md](./spec.md)

## Summary

把 geo-optimizer-skill 的 rubric v2 站点审计引擎以 **Go 原生移植**的方式纳入
`user-server/internal/geo/service/`：活体抓取（目标页 + robots + llms + ai.txt +
summary.json + sitemap）→ 纯函数评分（9 类 100 分）→ 修复清单。
Python 侧 18K 行不引入运行时依赖，只移植「检查项判定语义 + 分值表」；
hivemtk 已有面（内容质量 12 指标 / 25 因子 / index-tracking）不重复建设。

## Technical Context

**Language/Version**: Go 1.26（user-server）
**Dependencies**: golang.org/x/net/html（已在 go.mod）、标准库 net/http
**Storage**: 无新表（审计不落库）
**Testing**: `go test ./internal/geo/...`（真实 PG 测试库仅仓储层需要；本特性纯函数为主）

## Architecture

五层落位（ constitution I）：

```
Router(geo_routes.go 追加一行映射)
  → Controller(tech_metrics.go 追加 SiteAudit handler：绑定 dto → 调 service)
    → Service(site_audit.go：FetchSiteResources + RunSiteAuditFromResources 纯函数)
      → Repository(无)
        → Model(无)
```

- `SiteAuditInput{URL, Timeout}` → `fetchSiteResources(ctx, url)`（≤8 请求、10s/请求、2MB body 上限）
  → `SiteResources{Page, Robots, LLMs, LLMsFull, AITxt, SummaryJSON, SitemapURL, FinalURL}`
  → `RunSiteAuditFromResources(res, brand)` 纯函数 → `*GeoSiteAuditReport`。
- 抓取与评分分离 = spec US3（可离线复用）；`brand` 只用于**页内匹配**（brand_entity 类），
  绝不出站（US2）。
- 复用现有 `AuditGrade` 分档；`llms.txt` 校验语义与 `renderLLMsTxt`（生成端）对齐。

## Checks → 分值映射（与上游 SKILL.md 逐项对齐）

| 类 | 检查 | 分 |
|----|------|----|
| google_ai(20) | G-INDEX 5 / G-SNIPPET 5 / G-CANONICAL 3 / G-DATES 2 / G-BYLINE 2 / G-LINKS 1 / G-VIEWPORT 1 / G-SITEMAP 1 | 20 |
| robots(14) | robots 存在 4 / search 层三 bot 放行 3×2 / sitemap 声明 4 | 14 |
| schema(14) | JSON-LD 存在且可解析 4 / 有价值类型 4 / G-SD-VISIBLE 2 / Organization 可达 4 | 14 |
| content(14) | 标题层级 2 / 字数 3 / 量化证据 3 / 外部权威链接 2 / 内链 2 / 图 alt 覆盖 2 | 14 |
| brand_entity(12) | 品牌在 title 3 / 品牌在正文 3 / about|contact 链接 2 / sameAs|Organization schema 4 | 12 |
| meta(11) | title 长度 3 / meta description 4 / OG 标签 4 | 11 |
| llms(6) | /llms.txt 3 / 结构合法(H1+blockquote+H2) 2 / llms-full.txt 1 | 6 |
| signals(6) | html lang 2 / RSS|Atom 2 / 时效(近两年年份) 2 | 6 |
| ai_discovery(3) | /ai.txt 1 / summary.json 1 / markdown 协商 1 | 3 |

## Files

| File | Change |
|---|---|
| `user-server/internal/geo/service/site_audit.go` | 新增：资源抓取 + 九类评分 + 修复清单 |
| `user-server/internal/geo/service/site_audit_test.go` | 新增：夹具化单测（全类覆盖 + 边界） |
| `user-server/internal/geo/controller/tech_metrics.go` | 追加 `SiteAudit` handler |
| `user-server/internal/router/geo_routes.go` | 追加 `POST /geo/techconfig/site-audit` |

## Verification

`cd user-server && go build ./... && go test ./internal/geo/...`（3.5 口径）。
