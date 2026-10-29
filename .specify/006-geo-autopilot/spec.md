# Feature Specification: GEO 一键 autopilot（傻瓜式单关键词全链路）

**Feature Branch**: `006-geo-autopilot`
**Created**: 2026-10-29
**Status**: Draft
**Input**: 用户任务「geo 模块太复杂，用户只想输入一个关键词，剩下由 hivemtk 预设流程自动完成关键词、下拉词等完整 geo 功能配置并使用（文章上传可不用），实现自动营销获客」

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 输入一个关键词跑通全链路 (Priority: P1)

已配置品牌（GeoConfig.brand_name 非空）的用户在「GEO 自动驾驶」页输入**一个关键词**
（可选：站点 URL、生成文章数），点击启动后系统按预设顺序自动执行：

1. **brand_check**：读取品牌配置（缺失即失败并给出去哪配置的指引）；
2. **keyword_expand**：LLM 语义扩展种子词（失败降级为只用原词，不断链）；
3. **suggest_crawl**：五引擎下拉词抓取（best-effort，失败仅记录）；
4. **longtail_expand**：23 模板长尾离线展开（零成本）；
5. **save_keywords**：挖掘结果落库 geo_keywords + 启发式意图分类；
6. **content_generate**：取扩展词前 N 个（默认 3、上限 5）逐词 生成 → 评分 → Schema（**不发布**）；
7. **push_urls**（可选）：配置了蜘蛛推送且给了 site_url 时推送新文章 URL；
8. **site_audit**（可选）：给了 site_url 时跑 rubric v2 站点审计（005 特性）；
9. **summary**：汇总各步产出（关键词数/下拉词数/文章数/平均分/审计分）。

每步状态实时落库，前端轮询可视化时间线；单步失败不中断（best-effort），
仅 brand_check 与 save_keywords 属硬依赖。

**Independent Test**: POST `/api/geo/autopilot/runs` `{"keyword":"客服系统"}` → 200 返回 run id；
轮询 GET `/api/geo/autopilot/runs/:id` 至终态，steps 里 9 个步骤各带 status/detail，
geo_keywords 新增该种子词派生行，geo_articles 新增 ≤N 篇。

**Acceptance Scenarios**:

1. Given 品牌已配置，When 用新关键词启动，Then 终态 success/partial 且关键词、下拉词、
   长尾、文章四类产出计数 > 0。
2. Given 品牌未配置，When 启动，Then run 状态 failed，error 指引「先到 品牌与域名 页配置品牌名」，
   且不产生任何 LLM/出站调用。
3. Given LLM 不可用，When 启动，Then keyword_expand 记录降级、流程继续（下拉/长尾/落库仍产出）。
4. Given 同租户已有 running 的 run，When 再次启动，Then 返回 409 语义（进行中不可并发）。
5. Given 参数非法（空关键词、article_count 超界），Then 400 且不入库。

### User Story 2 - 过程透明可追溯 (Priority: P2)

运行历史可查（列表 + 详情，含每步耗时与结果摘要），复跑不残留（每次 run 独立记录）。

**Acceptance Scenarios**:

1. Given 历史上跑过 ≥1 次，When 打开列表，Then 按创建时间倒序展示关键字/状态/进度/耗时。
2. Given 某次 run 详情，Then 每步有 status(running/success/skipped/failed)/detail/duration_ms。

### User Story 3 - 零出域与既有纪律不回退 (Priority: P0)

- 出站请求与既有 GEO 能力同源同量：下拉词抓取（CrawlSuggest 既定行为）、可选蜘蛛推送、
  可选站点审计（005 纪律）；不新增任何第三方调用。
- 文章只生成不发布（PlatformPublish 完全不触碰）。
- LLM 成本记账走既有 recordAPICall 口径。

## Requirements

- 新表 `geo_autopilot_runs`（单行=一次运行，steps 为 JSON 文本列），AutoMigrate 登记。
- 并发闸：同租户同时只允许一个 running（DB 状态检查 + 提示 409 语义）。
- 运行体脱离请求 ctx（context.WithoutCancel + 15 分钟超时），前端轮询驱动。
- 前端：`user-web` 新增「自动驾驶」页（输入框 + 步骤时间线 + 结果摘要），路由挂
  execute 组 order 0；API 出口 `src/api/geoAutopilot.js`。

## Out of Scope

- 文章上传/多平台发布（用户明确可不用）。
- 定时化（autopilot 是用户触发的即时流程；批量自动化走既有 keyword_pipeline 作业）。
- 多关键词批量、工作流编排自定义（保持傻瓜式：零配置只有一个输入框）。
