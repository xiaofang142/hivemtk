# Feature Specification: GEO 站点审计 v2（rubric v2 九类百分类，geo-optimizer-skill 吸纳）

**Feature Branch**: `005-geo-site-audit-v2`
**Created**: 2026-10-29
**Status**: Draft
**Input**: 用户任务「调研论证 https://github.com/Auriti-Labs/geo-optimizer-skill 技术细节，制定计划吸纳融入 hivemtk geo 模块」

## 调研结论（输入论证）

**geo-optimizer-skill v4.19.8（MIT）**：Python CLI + MCP（`geo` 命令，core 约 1.8 万行），
审计「站点能否被 AI 搜索引擎（ChatGPT Search / Perplexity / Claude / Google AI Overviews）找到并引用」。
核心资产：

1. **rubric v2 评分**：9 类 100 分 —— google_ai 20 / robots 14 / schema 14 / content 14 /
   brand_entity 12 / meta 11 / llms 6 / signals 6 / ai_discovery 3；分档 0-35 critical /
   36-67 foundation / 68-85 good / 86-100 excellent。
2. **Google AI 就绪 10 检查**（G-INDEX / G-SNIPPET / G-CANONICAL / G-DATES / G-BYLINE /
   G-LINKS / G-VIEWPORT / G-SITEMAP / G-SD-VISIBLE / G-GENAI-CONTROL），每项附 Google 官方文档出处。
3. **AI 爬虫三层分级**（training / search / user-triggered）：OAI-SearchBot、PerplexityBot、
   Claude-SearchBot、Googlebot 属 search 层必须放行；GPTBot/ClaudeBot/Google-Extended 属
   training 层可策略性禁止而不影响 AI 搜索可见性。
4. **llms.txt / llms-full.txt 校验**（H1 → blockquote → H2 sections 结构）。
5. **ai_discovery**：ai.txt、summary.json、markdown 协商（Accept: text/markdown）。
6. 47 项 citability 内容检查（Princeton GEO 9 法）与 8 种输出格式（本仓**不吸纳**——
   hivemtk 已有 metrics.go 12 指标 + 25 因子内容审计覆盖该面）。

**hivemtk 现状**：`TechConfigService.RunGEOAudit`（25 因子）是**纯文本内容审计**
（输入 url/title/content/metaDesc/schema 字符串），不做活体抓取；robots/llms 两个因子
硬编码 pass；品牌名硬编码 "HiveMtk"。与 rubric v2 的差距 = 本特性的范围。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 输入网址一键拿到 AI 可见性评分与修复清单 (Priority: P1)

用户在 GEO 工具里输入自己站点的一个 URL，系统**活体抓取**该页 + robots.txt +
llms.txt + ai.txt + summary.json，按九类 rubric v2 打出 0-100 分，给出逐类得分、
逐检查项 pass/fail 明细与按分值排序的修复建议。

**Independent Test**: POST `/api/geo/techconfig/site-audit` `{"url":"https://example.com"}`，
返回 `{"score":0-100,"grade":"...","categories":[...],"fixes":[...]}`，各 category 分值合计 = score。

**Acceptance Scenarios**:

1. Given 一个放行了 OAI-SearchBot/PerplexityBot/Claude-SearchBot 且声明 sitemap 的站点，
   When 审计，Then robots 类得满分且各 bot 逐项可见。
2. Given robots.txt 禁止了 PerplexityBot，When 审计，Then robots 类扣分且 fixes 给出放行建议。
3. Given 页面带合法 JSON-LD（WebSite/Organization），When 审计，Then schema 类按类型加分，
   且 JSON-LD name/headline 出现在可见文本时 G-SD-VISIBLE 过。
4. Given 站点部署了 /llms.txt（H1+blockquote+H2 结构），When 审计，Then llms 类得分；
   缺失时 fixes 提示可用 POST /geo/techconfig/llms-txt 生成。
5. Given 目标 URL 不可达（超时/DNS 失败/5xx），When 审计，Then 返回 400 语义错误而非 500，
   message 说明原因。

### User Story 2 - 审计不外发任何品牌数据 (Priority: P0)

审计的出站请求只带用户显式提供的 URL 本身（GET 目标页 + 其 robots/llms/ai.txt/sitemap），
不携带品牌名、优势、验证结果等任何 GEO 配置数据。未显式发起审计时不产生任何出站请求
（无定时任务、无后台抓取）。

**Acceptance Scenarios**:

1. Given 任意品牌配置内容，When 发起一次审计，Then 出站 HTTP 请求 URL 集合 ⊆ {目标页,
   目标域 robots.txt, /llms.txt, /llms-full.txt, /ai.txt, /summary.json, /sitemap.xml}，请求头不含品牌信息。
2. Given 无用户触发，Then 不存在周期性站点抓取行为。

### User Story 3 - 审计引擎可脱离网络复用 (Priority: P2)

评分与检查逻辑为纯函数（输入=已抓取的资源包），单测用 HTML/robots/llms 文本夹具即可
全量覆盖，不依赖真实外网。活体抓取只是薄封装。

**Acceptance Scenarios**:

1. Given 夹具 HTML（含/缺各要素），When 跑 `RunSiteAuditFromResources`，Then 各检查项
   判定与 rubric 一致且可断言。
2. Given 恶意 HTML（超大 body / 深嵌套），When 解析，Then 不 panic、有大小上限保护。

## Requirements

- 九类分值与 geo-optimizer rubric v2 完全一致（google_ai 20 / robots 14 / schema 14 /
  content 14 / brand_entity 12 / meta 11 / llms 6 / signals 6 / ai_discovery 3）。
- 分档沿用仓内 `AuditGrade`（86/68/36 —— 与 Auriti 分档一致）。
- 每个检查项输出：`{check, category, score, max, pass, detail, source?}`；`source` 为
  Google 官方文档 URL（仅 google_ai 类检查携带，与上游一致）。
- 输出兼容现有响应契约 `{"code":0,"data":...,"message":"ok"}`。
- 五层架构：新增逻辑全部在 service 层（`internal/geo/service/site_audit.go`），
  controller 仅参数绑定；无新表（审计不落库，v1 与前端展示直连）。
- 活体抓取超时单请求 10s、body 上限 2MB、最多 8 个请求；抓取失败按类降级为 0 分并给 detail。
- 旧 25 因子 `RunGEOAudit`（内容审计）保持不变、不删除。

## Out of Scope

- 47 项 citability 全集、8 种输出格式（html/pdf/sarif）、MCP server、plugin entry points、
  diff/history/monitor（hivemtk 已有 index-tracking/citation drift 覆盖）。
- 审计结果落库与历史对比（后续特性）。
- Python sidecar / 子进程调用 geo CLI（引入 Python 运行时依赖，违背私域一键部署约束）。
