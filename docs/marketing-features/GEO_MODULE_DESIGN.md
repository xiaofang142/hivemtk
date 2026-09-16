# GEO 模块完整设计文档（SEO + GEO 全链路）

> **版本**: v1.0  
> **日期**: 2026-09-15  
> **范围**: Geo 模块从关键词蒸馏到 AI 引擎引用的完整产品链路、技术规格、菜单组织、数据模型、监控可视化、实施计划  
> **依赖**: user-server/internal/geo/ 五层架构、manage 前端 Vue3+ElementPlus  

> **所属系统**: user-server（`internal/geo/`）+ user-web（manage 前端）
> **功能 slug**: geo-module
> **代码位置**: `hivemtk/user-server/internal/geo/`、`hivemtk/user-web/src/router/modules/geoTools.js`

---

## §一 功能完成状态

> 本节为 [`docs/standards/FEATURE_DOCUMENTATION_TEMPLATE.md`](../standards/FEATURE_DOCUMENTATION_TEMPLATE.md)
> 要求的**首节**。目的：先如实交代完成度，再展开设计——避免"文档很长但功能没做"的误读，
> 也避免"功能做了但没勾"的漏记（两类问题在 2026-09-15 的任务清单审计中都实际出现过）。
>
> 状态口径：✅ 已实现 / 🟡 部分实现 / 🔵 骨架已建 / ⬜ 规划中

| 子能力 | 状态 | 证据 |
|--------|------|------|
| 五层架构骨架（Router→Handler→Service→Repository→Model） | ✅ | `internal/geo/` 下 **107** 个非测试 Go 文件，`dto/`、`repository/`、`controller/`、`service/` 分层齐备 |
| 关键词蒸馏 / 分组 | ✅ | `repository/keyword.go`、`repository/keyword_group.go`、`dto/keyword.go`、`dto/keyword_enhance.go` |
| 内容生产与知识接入 | ✅ | `repository/content.go`、`repository/knowledge.go`、`dto/content.go`、`dto/knowledge.go` |
| 蜘蛛推送 / 索引追踪 | ✅ | `repository/geo_push_record.go`、`repository/geo_index_tracking.go`、`repository/geo_pusher_config.go` |
| 站点与 Schema 模板 | ✅ | `repository/geo_site.go`、`repository/geo_schema_template.go` |
| 竞品与探测（probe） | ✅ | `repository/competitor.go`、`repository/probe.go`、`repository/crawler_visit.go` |
| 监控 / 日报 / 告警 | ✅ | `repository/daily_stats.go`、`repository/alert.go`、`model.GeoAlert` 等已登记进 `AutoMigrate` |
| 作业编排 | ✅ | `repository/job_run.go`、`repository/workflow.go`、`dto/workflow.go` |
| 前端菜单（Config → Execute → Observe） | ✅ | `router/modules/geoTools.js` 共 **24** 条路由 |
| 数据表 | ✅ | 由 GORM `AutoMigrate` 维护（见 `user-server/internal/pkg/db/migrate.go` 的 `geomodel.*` 列表），**无**独立 SQL 迁移 |
| 端到端验收 | 🟡 | 见 §八 测试策略；`scripts/api_verify_full.py` 覆盖部分 geo 端点 |

> **说明**：本文其余章节是该模块的**设计蓝图**（含 §十一 的 15 个工作日实施计划）。
> 代码已落地不等于蓝图全部完成——如需权威完成度，请以代码与本表为准，勿以设计章节的存在推断功能已完成。

---

## §二 核心原理

GEO（Generative Engine Optimization）与 SEO 的根本差异：**SEO 争的是「排名」，GEO 争的是「被引用」**。
大模型回答时只会引用它能抓到、能解析、且信源可信的内容，因此本模块同时跑两条链路：

| 链路 | 目标 | 关键手段 | 本文对应章节 |
|------|------|----------|--------------|
| 蜘蛛链路 | 让内容被**收录** | llms.txt v2、IndexNow、6 引擎主动推送 | 七、蜘蛛推送 6 引擎完整方案 |
| AI 引用链路 | 让内容被**引用** | 结构化正文、倒金字塔首段、可机读 Markdown 孪生 | 3.5 GEO 文章格式规范、10.4 Markdown 孪生 |

关键词侧采用 **4 层漏斗模型**（见 3.2）与**长尾词意图分类 × 文章类型**（见 3.3），
站位策略要求**每个关键词命中 3+ AI 引擎**（见 3.4）——单点命中不足以对抗引擎侧的随机性。

## §三 设计标准

以下规范为本模块的硬性依据，实现偏离即视为不合规（调研细节见二、技术规格调研）：

| 标准 | 版本 / 状态 | 约束 |
|------|-------------|------|
| llms.txt | v2（2026-08-10 发布） | 站点根路径需可访问；推荐以 HTTP `Link` 头暴露（见 10.3） |
| IndexNow | 现行 | 一次提交覆盖 Bing / Yandex / Naver / Seznam / Yep / Amazon（见 7.1） |
| Google Indexing API | 现行 | 仅对 JobPosting / BroadcastEvent 类内容生效，凭据需加密存储（见 7.3） |
| 蜘蛛推送 | 6 引擎 | 百度主动推送 + Google + IndexNow + 其余引擎，详见 2.4 |
| AI 爬虫识别 | 2026 分类 | 按 User-Agent 分流统计，避免把 AI 抓取混入人类流量（见 2.5） |

## §四 架构与模块关系

沿用仓库五层架构（Router → Handler → Service → Repository → Model），GEO 位于 `internal/geo/`，
与既有 content / knowledge / competitor 模块为**并列消费关系**，不反向依赖它们。

| 层 | 现状 | 本文对应章节 |
|----|------|--------------|
| 前端 | 16 个页面、`router/modules/geoTools.js` 24 条路由 | 四、菜单组织 |
| Service | 17 个（含 4 个本模块新增，见 5.2~5.5） | 五、后端 Service 架构 |
| 数据 | 17 张既有表 + 本次 ALTER 2 张 / 新增 6 张 | 六、数据模型 |
| 调度 | 每日 Upsert 与定时任务 | 附录 C：每日定时任务清单 |

## §五 数据模型

- 既有：**17 张表**（盘点见 1.2）。
- 本次变更：ALTER **2 张**（见 6.1）、新增 **6 张**（见 6.2）。
- 维护方式：由 GORM `AutoMigrate` 驱动（`user-server/internal/pkg/db/migrate.go` 的 `geomodel.*` 列表），
  **无独立 SQL 迁移文件**——改 schema 必须同时改 Model tag，否则会被 `AutoMigrate` 改回。
- 监控统一走 `geo_daily_stats` 一张表驱动（见 9.3）。

## §六 业务流程

主链路（自左向右）：

```
关键词蒸馏 → 关键词分组 → 内容生成 → 站点部署 → 蜘蛛推送 → 收录追踪 → 日报 / 告警
   (5.2)        (5.2)        3.5     十、部署流水线   七、推送   8.2 收录   附录 C
```

- **产出物**：Hugo 静态站 + Cloudflare Pages（见 10.1），同时输出可公开访问的 Markdown 孪生（见 10.4）。
- **反馈环**：收录与 AI 引用结果回写 `geo_daily_stats`，驱动次日选题（见 9.1、八、监控可视化设计）。
- **失败处理**：推送失败进入重试链与告警，凭据加密存储（见 7.2、7.3）。

## §七 前端交互

- 菜单按 `Config → Execute → Observe` 三层组织，共 24 条路由（见 4.2、4.3）。
- 关键页面：漏斗总览 `FunnelDashboard.vue`（见 8.1）、收录追踪与 AI 引用验证 `IndexTracking.vue`（见 8.2）、
  数据血缘（见 8.3）、SOV 公式升级（见 8.4）。
- 交互约束：所有长任务（挖掘、推送、部署）为异步作业，前端轮询 `job_run` 状态，
  不得在请求线程内同步等待（见 5.1、附录 A）。

---

## 模板 8 节对照

本文早于 `FEATURE_DOCUMENTATION_TEMPLATE.md` 建立，沿用自身的十二章结构。
为便于按模板导航，映射如下：

| 模板节 | 本文对应位置 |
|--------|--------------|
| §一 功能完成状态 | 见上（本文新增） |
| §二 核心原理 | [二、技术规格调研](#二技术规格调研202609-最新)、[三、完整产品链路](#三完整产品链路) |
| §三 设计标准 | [二、技术规格调研](#二技术规格调研202609-最新)（llms.txt v2 / IndexNow / Google Indexing API / 6 引擎规范） |
| §四 架构与模块关系 | [五、后端 Service 架构](#五后端-service-架构)、[四、菜单组织](#四菜单组织config--execute--observe) |
| §五 数据模型 | [六、数据模型（ALTER + 新增）](#六数据模型alter--新增) |
| §六 业务流程 | [三、完整产品链路](#三完整产品链路)、[七、蜘蛛推送 6 引擎完整方案](#七蜘蛛推送-6-引擎完整方案) |
| §七 前端交互 | [四、菜单组织](#四菜单组织config--execute--observe)、[八、监控可视化设计](#八监控可视化设计) |
| §八 测试策略 | 见文末（本文新增） |

---

## 目录

- [一、设计背景与问题定义](#一设计背景与问题定义)
- [二、技术规格调研（2026.09 最新）](#二技术规格调研202609-最新)
- [三、完整产品链路](#三完整产品链路)
- [四、菜单组织（Config → Execute → Observe）](#四菜单组织config--execute--observe)
- [五、后端 Service 架构](#五后端-service-架构)
- [六、数据模型（ALTER + 新增）](#六数据模型alter--新增)
- [七、蜘蛛推送 6 引擎完整方案](#七蜘蛛推送-6-引擎完整方案)
- [八、监控可视化设计](#八监控可视化设计)
- [九、SEO × GEO 数据整合](#九seo--geo-数据整合)
- [十、网站部署流水线](#十网站部署流水线)
- [十一、实施计划（15 个工作日）](#十一实施计划15-个工作日)
- [十二、风险与已知限制](#十二风险与已知限制)

---

## 一、设计背景与问题定义

### 1.1 业务目标

私域部署的 AI 智能体客服系统，需要通过 SEO（搜索引擎优化）+ GEO（生成式引擎优化，Generative Engine Optimization）让品牌内容在：

- **传统搜索引擎**（百度、Google、Bing、360、搜狗、神马）中被用户搜到
- **AI 问答引擎**（豆包、文心一言、Kimi、DeepSeek、通义千问、ChatGPT、Copilot）中被 AI 引用并回答用户问题

### 1.2 现有能力盘点

#### 已有 Service（17 个，约 5000 行 Go）

| Service | 方法 | 状态 |
|---------|------|------|
| TechConfigService | GenerateRobots / GenerateSitemap / GenerateLLMsTxt | ✅ 已有，需升级 v2 |
| KeywordService | MineKeywords / SemanticExpand / TopicCluster | ✅ 有，缺下拉词抓取 |
| KeywordEnhanceService | AnalyzeHistoricalPerformance / EnhanceKeywordWithData | ✅ |
| ContentService | GenerateContent / Optimize / Score / EnhanceEEAT / GenerateSchema / CheckUniqueness | ✅ 非常完整 |
| PlatformService | SaveAccount / Publish / publishGitHub / ListPublishRecords | ✅ 多平台发布 |
| ProbeService | ProbeAllEnginesConcurrent / TestSingle / ListRuns | ✅ 5 引擎并发 |
| VisibilityService | GetTrend / GetEngineCompare | ✅ |
| VerificationService | VerifyArticle / MonitorNegative / GetVerifyResults | ✅ |
| WorkflowService | Create / Update / Run / RegisterExecutor | ✅ 工作流引擎 |
| MonitorCrawlerService | RunCrawlerCron / doCrawl | ✅ |
| GeoDecisionAnalytics | GetShareOfVoice / GetCrawlerStats / DetectInaccurateClaims | ✅ SOV |
| ReportService | GetReport / GetAPICosts / GetROI | ✅ |
| EntityExtractor | ExtractFromDocument | ✅ |
| KBService | Save / Ask / Search / GetContextForGeneration | ✅ |
| MetricsService | Analyze / CountTrustSignals / CalculateAuthorityScore | ✅ |
| LLMService | 统一 LLM 调用 | ✅ |
| AlertService | 告警 | ✅ |

#### 已有数据模型（17 张表）

| 表名 | 用途 |
|------|------|
| geo_keywords | 关键词库 |
| geo_keyword_groups | 关键词分组 |
| geo_articles | 生成的文章 |
| geo_optimizations | 文章优化记录 |
| geo_publish_records | 多平台发布记录 |
| geo_probe_runs | 搜索探针结果 |
| geo_verify_results | 验证结果 |
| geo_daily_stats | 每日聚合（engine/intent/funnel_stage/brand_mentioned/citation_count） |
| geo_query_chains | 查询链追踪 |
| geo_content_tasks | 内容缺口任务队列 |
| geo_crawler_visits | 爬虫访问记录 |
| geo_job_runs | 定时任务执行 |
| geo_knowledge_documents | 知识库 |
| geo_entities / geo_entity_relations | 实体图谱 |
| geo_source_catalogs | 来源目录 |
| geo_workflows / geo_workflow_executions / geo_workflow_templates | 工作流 |
| geo_competitors / geo_alert / geo_config / geo_api_calls | 基础配置 |

#### 已有前端页面（16 个）

全部在 `/geo-tools/` 路由下，`group: 'analytics'`，扁平排列：
VisibilityBoard / DecisionReport / KeywordMining / ContentCreation / ContentOptimize / KnowledgeBase / PlatformPublish / WorkflowEditor / SovBoard / CrawlerStats / EntityGraph / Verification / Reports / ConfigOptimizer / CompetitorManage / AlertCenter

### 1.3 核心缺口（对比完整链路）

| # | 缺口 | 影响 |
|---|------|------|
| 1 | **关键词无 4 层漏斗模型** | 扁平存储，无法指导优先级 |
| 2 | **无下拉词 API 抓取** | 只有 LLM 造词，缺真实用户搜索数据 |
| 3 | **静态站完全缺失** | TechConfig 能生成 robots/sitemap/llms.txt，但没地方部署 |
| 4 | **无蜘蛛推送** | sitemap 不会自动推给百度/Google/IndexNow |
| 5 | **无索引追踪** | 不知道哪篇被哪个引擎收录、被哪个 AI 引用 |
| 6 | **无全链路漏斗可视化** | 分散页面，没有一张图从关键词到 AI 引用 |
| 7 | **SEO/GEO 不联动** | geo_daily_stats 有 citation_count 但没和 SOV/Visibility 联动 |
| 8 | **菜单扁平** | 16 个页面按功能散排，用户认知成本高 |

---

## 二、技术规格调研（2026.09 最新）

### 2.1 llms.txt v2 规范（2026.08.10 发布）

| 项目 | 规范 |
|------|------|
| **格式** | H1 站点名 → blockquote 简介 → 可选上下文段 → `##` 板块 → markdown 链接（带说明） |
| **v2 硬新增** | `rel="describedby"` 广播——必须同时在 `<head>` 和 HTTP Link 头声明 |
| **发现协议** | `Link: </llms.txt>; rel="describedby"; type="text/markdown"` 跟着每个响应走 |
| **路径作用域** | `/docs/llms.txt` 只覆盖 `/docs/` 下页面，"最具体的文件获胜" |
| **Markdown 孪生** | 每个页面必须有 `.md` 版本（`page.html` → `page.html.md`） |
| **页面级声明** | `<link rel="alternate" type="text/markdown" href="page.html.md">` |
| **Chrome Lighthouse** | 已加入 agentic-browsing 检查项 |
| **真实状态** | OpenAI/Anthropic/Google 文档站都有；仅 llmstxt.org 完整实现了 v2 relations |

### 2.2 IndexNow 协议（2026 状态）

| 项目 | 规范 |
|------|------|
| **覆盖引擎** | Bing、Yandex、Naver（韩国 55%）、Seznam（捷克 25%）、Yep、**Amazonbot** |
| **❌ 不含** | Google（不采纳）、百度、字节、神马 |
| **2026 数据** | 80M+ 网站，每天 5B+ URL，Bing 22% 点击来自 IndexNow |
| **认证** | 域名根目录 `{UUID}.txt` 文件内容 = key 本身；或 keyLocation 参数 |
| **批量** | JSON POST，一次最多 **10000 URL** |
| **配额** | **无限** |
| **间接收益** | Bing 索引 → ChatGPT Search / Microsoft Copilot / DuckDuckGo / Yahoo |

### 2.3 Google Indexing API

| 项目 | 规范 |
|------|------|
| **官方说** | 只支持 JobPosting / BroadcastEvent 页面类型 |
| **实测** | 任何 URL 都接收推送 |
| **配额** | 默认 **200 次/天**（按 URL 计数） |
| **配额规避** | 多建 GCP Project（每个 200）；batch multipart 每批 100 URL |
| **Go SDK** | `google.golang.org/api/indexing/v3` |
| **JWT 链** | Service Account JSON → 签名 JWT → POST oauth2/token → access_token（缓存 1h） |
| **坑** | Search Console 必须把 Service Account 加为**所有者**；URL 协议/域名必须完全匹配 |

### 2.4 蜘蛛推送 6 引擎完整技术规范

| 引擎 | 方法 | 接口 | 认证 | 配额 | 格式 |
|------|------|------|------|------|------|
| **百度** | 主动推送 API | `POST http://data.zz.baidu.com/urls?site=SITE&token=TOKEN` | token 明文 | 每日几千 | text/plain（每行 URL），一次 2000 |
| **字节/豆包** | 头条站长平台 sitemap | sitemap 为主 | 站长平台 token | - | sitemap |
| **神马/通义** | 神马站长平台 | sitemap + URL 提交 | 站长平台 | - | sitemap |
| **Google** | Indexing API v3 | `POST https://indexing.googleapis.com/v3/urlNotifications:publish` | GCP Service Account JWT | 200 URL/天/Project | JSON `{"url":"...", "type":"URL_UPDATED"}` |
| **Bing/Yandex/Naver/Seznam** | IndexNow（一次覆盖） | `POST https://api.indexnow.org/indexnow` | 域名根 key.txt | **无限** | JSON POST，10000/次 |
| **全引擎兜底** | sitemap.xml | 各站长平台手动提交 | - | 长期有效 | XML |

### 2.5 2026 AI 爬虫 User-Agent 分类

| 类别 | 爬虫 | 策略 |
|------|------|------|
| **AI 搜索索引爬虫** | OAI-SearchBot、Claude-SearchBot、PerplexityBot | ✅ Allow /（决定 AI 可见性） |
| **传统搜索爬虫** | Googlebot、Baiduspider、bingbot、360Spider、Sogou | ✅ Allow / |
| **AI 训练爬虫** | GPTBot、ClaudeBot、DeepSeekBot、Bytespider、QwenBot、Meta-ExternalAgent、CCBot、Applebot-Extended | ✅ Allow /（我们需要 AI 引用） |
| **Amazonbot** | Amazonbot（Alexa + IndexNow） | ✅ Allow / |
| **用户触发型** | ChatGPT-User、Claude-User、Perplexity-User | 不严格遵守 robots.txt，可忽略 |

**国内 AI 爬虫 UA 完整清单（2026.08 验证）**：
```
Bytespider       → 豆包
DeepSeekBot      → DeepSeek
QwenBot          → 通义千问
ErnieBot         → 文心一言
BaiduSpider-AI   → 文心一言补充
TencentAIspider  → 腾讯元宝
MoonshotBot      → Kimi
```

### 2.6 下拉词抓取接口（免费、无注册）

| 引擎 | 接口 | 解析字段 | JSONP 包裹 |
|------|------|----------|-----------|
| **百度** | `https://suggestion.baidu.com/su?wd={kw}&cb=cb` | `s[]` 数组，最多 10 | ✅ |
| **Bing** | `https://api.bing.com/qsonhs.aspx?q={kw}&type=cb&cb=cb` | `AS.Results[].Suggests[].Txt` | ✅ |
| **Google** | `https://suggestqueries.google.com/complete/search?client=firefox&hl=zh-CN&q={kw}&callback=cb` | `[1]` 数组 | ✅ |
| **360** | `https://sug.so.360.cn/suggest?format=json&word={kw}&callback=cb` | `result[].word` | ✅ |
| **搜狗** | `https://sor.html5.qq.com/api/getsug?key={kw}` | 数组第 `[1]` 项 | ❌（直接 JSON） |

**Go JSONP 解析**：
```go
func stripJSONP(s string) string {
    re := regexp.MustCompile(`^\w+\((.*)\);?$`)
    m := re.FindStringSubmatch(s)
    if len(m) == 2 { return m[1] }
    return s
}
```

---

## 三、完整产品链路

### 3.1 全链路架构图

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                         GEO 营销作战台（manage 后台）                         │
│                                                                              │
│  ┌─ 配置层 ─────────┐  ┌─ 执行层 ─────────┐  ┌─ 监控层 ─────────────────┐   │
│  │ BrandConfig      │  │ KeywordMining    │  │ FunnelDashboard        │   │
│  │ SeoInfra         │→ │ ContentCreation  │→ │ IndexTracking          │   │
│  │ PusherConfig     │  │ SitePublish      │  │ VisibilityBoard        │   │
│  │ SiteConfig       │  │ PushCenter       │  │ SovBoard               │   │
│  │ SchemaTemplates  │  │ PlatformPublish  │  │ CrawlerStats           │   │
│  │ CompetitorManage │  │ WorkflowEditor   │  │ Verification           │   │
│  └──────────────────┘  └──────────────────┘  │ DecisionReport         │   │
│                                              │ Reports                │   │
│                                              │ AlertCenter            │   │
│                                              └────────────────────────┘   │
└──────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                         后端 Service 层（internal/geo/）                       │
│                                                                              │
│  KeywordMiningService → ContentService → SiteDeployService → PushService     │
│       ↓                   ↓                  ↓                ↓            │
│  4层漏斗关键词        倒金字塔+Schema     Hugo导出          6引擎蜘蛛推送      │
│                                                                              │
│  IndexTrackerService ←──────────────────────────────────────────────────────┘
│       ↓                                                                     │
│  每日定时 Upsert → geo_daily_stats（SEO × GEO 聚合）                          │
│       ↓                                                                     │
│  FunnelDashboard / SovBoard / VisibilityBoard / AlertCenter                  │
└──────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                         静态站（用户可见）                                     │
│                                                                              │
│  https://yourdomain.com/                                                     │
│  ├── llms.txt                    ← AI 迎宾台（v2 + describedby Link 头）     │
│  ├── llms-full.txt               ← 可选，重点页完整 Markdown 拼接           │
│  ├── robots.txt                  ← AI 爬虫友好（2026 完整白名单）            │
│  ├── sitemap.xml                 ← 自动生成 + 每周全量提交                    │
│  ├── {KEY}.txt                   ← IndexNow 验证文件                         │
│  │                                                                          │
│  ├── article/*.html              ← 文章页（含 Schema JSON-LD）               │
│  ├── article/*.html.md           ← v2 Markdown 孪生                          │
│  ├── faq/*.html                  ← FAQ 页                                    │
│  ├── faq/*.html.md                                                           │
│  ├── guide/*.html                ← 指南/对比页                               │
│  └── product/*.html              ← 产品页                                    │
│                                                                              │
│  部署链: Hugo → GitHub Actions → Cloudflare Pages                            │
└──────────────────────────────────────────────────────────────────────────────┘
```

### 3.2 关键词 4 层漏斗模型

```
┌─────────────────────────────────────────────────────────────┐
│  Layer 1: 种子词 (seed)       10-50 个                        │
│  品牌核心词 / 行业大类词 / 人工精选                            │
│  "CRM"、"客户管理系统"、"SaaS"                                │
└─────────────────────────────────────────────────────────────┘
           │ SemanticExpand + 同义词库
           ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 2: 关联词 (related)    100-500 个                      │
│  LLM 语义扩展 + 上下位词                                      │
│  "CRM系统"、"客户关系管理"、"企业CRM"                          │
└─────────────────────────────────────────────────────────────┘
           │ CrawlSuggest（5 引擎并发）
           ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 3: 下拉词 (suggest)    500-2000 个                     │
│  百度/Bing/Google/360/搜狗 suggest 接口                       │
│  "CRM哪个好用"、"CRM免费版"、"CRM系统排名"                     │
└─────────────────────────────────────────────────────────────┘
           │ CombineLongtail（模板化组合）
           ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 4: 长尾词 (longtail)  2000-10000 个                    │
│  问题模板 × 场景模板 × 年份模板                                │
│  "2026年中小企业CRM选型指南"、"CRM免费版和付费版区别对比"      │
│  每个长尾词 = 一篇文章                                        │
└─────────────────────────────────────────────────────────────┘
```

### 3.3 长尾词意图分类 × 文章类型

| 意图 query_intent | 漏斗阶段 funnel_stage | 文章类型 | 示例标题 |
|-------------------|----------------------|----------|----------|
| how_to | 认知 | How-to 教程 | "如何选择 CRM 系统" |
| problem | 认知 | 问题解决 | "CRM 实施失败的 10 个原因" |
| comparison | 评估 | 对比测评 | "XX CRM vs YY CRM 哪个好" |
| recommendation | 评估 | 排行榜 | "2026 中小企业 CRM 推荐" |
| pricing | 决策 | 选型指南 | "CRM 免费版 vs 付费版怎么选" |
| case_study | 留存 | 案例研究 | "某制造企业 CRM 实施案例" |

### 3.4 站位策略：每个关键词命中 3+ AI 引擎

```
关键词入选门槛 = AI 收录率 > 60% AND 提及率 > 30%

生成文章 → 部署静态站 → 蜘蛛推送 → 等收录 → 探针验证：
  ✅ 百度 + 豆包（字节系）
  ✅ 百度 + 文心一言
  ✅ 360 + 360 AI 助手
  ✅ Bing + Microsoft Copilot
  ✅ Google + Gemini / Perplexity

≥3 个 AI 引擎引用 = 站位成功 → 关键词加权
<3 个 = 调整 Schema 标记 / 加 FAQ 区块 / 重新推送
```

### 3.5 GEO 文章格式规范（AI 最爱）

**倒金字塔结构**：
```
首段 1-2 句 = 结论 + 品牌自然提及
  ↓
核心要点（H2，问题式标题："怎么选"、"哪个好"、"区别是什么"）
  ↓
选型指南 / 对比表格（Schema Product / Comparison）
  ↓
FAQ 区块（Schema FAQPage — AI 问答最高权重）
  ↓
总结 + CTA
```

**必须生成的 Schema 类型**：

| 页面类型 | Schema 类型 | 作用 |
|----------|-------------|------|
| 产品页 | `Product` | AI 提取产品参数、价格 |
| 对比页 | `Comparison` | AI 引用对比数据 |
| FAQ 页 | `FAQPage` | **AI 问答最高权重** |
| 文章页 | `Article` | 标注作者、发布时间 |
| 企业页 | `Organization` | 统一品牌实体 |

**Prompt 升级方向**：
- 强制倒金字塔（首段结论前置）
- 强制问题式 H2（让 AI 容易匹配用户提问）
- 强制 FAQ 区块（至少 3 个 Q&A）
- 强制品牌自然提及（1-2 次/千字，避免堆砌）
- 强制可引用性（给出具体数据点、来源）

---

## 四、菜单组织（Config → Execute → Observe）

### 4.1 设计原则

菜单不是功能分类，是**认知流水线**。SEO/GEO 工作天然分 3 阶段：

```
┌──────────┐    ┌──────────┐    ┌──────────┐
│  配置层   │ →  │  执行层   │ →  │  监控层   │
│  Config   │    │  Execute │    │  Observe  │
└──────────┘    └──────────┘    └──────────┘
```

### 4.2 完整路由表（router/modules/geoTools.js）

#### Layer 1: 配置 (group: 'config')

| 路径 | 组件 | 标题 | 职责 |
|------|------|------|------|
| /geo-tools/brand-config | BrandConfig.vue | 品牌与域名 | 品牌名、域名、竞品列表 |
| /geo-tools/seo-infra | SeoInfra.vue | SEO 基础设施 | robots.txt / sitemap / llms.txt v2 在线预览编辑 |
| /geo-tools/pusher-config | PusherConfig.vue | 蜘蛛推送配置 | 6 引擎推送凭据（AES 加密存储） |
| /geo-tools/site-config | SiteConfig.vue | 静态站部署 | Hugo 路径、Cloudflare、GitHub Actions |
| /geo-tools/schema-templates | SchemaTemplates.vue | Schema 模板 | FAQPage / Product / Comparison / Article 模板库 |
| /geo-tools/competitors | CompetitorManage.vue | 竞品管理 | 竞品列表（现有） |

#### Layer 2: 执行 (group: 'execute')

| 路径 | 组件 | 标题 | 职责 |
|------|------|------|------|
| /geo-tools/keyword-mining | KeywordMining.vue | 关键词蒸馏 | 4 层漏斗 + 下拉词抓取 + 长尾组合（升级） |
| /geo-tools/content-creation | ContentCreation.vue | 内容创作 | 模板选择 + Schema 自动注入（升级） |
| /geo-tools/content-optimize | ContentOptimize.vue | 文章优化 | 现有 |
| /geo-tools/site-publish | SitePublish.vue | 发布到官网 | ExportToHugo → TriggerDeploy → HealthCheck |
| /geo-tools/push-center | PushCenter.vue | 蜘蛛推送 | 6 引擎推送队列 + 配额仪表盘 |
| /geo-tools/platform-publish | PlatformPublish.vue | 多平台发布 | 知乎/CSDN/掘金等（现有） |
| /geo-tools/workflow | WorkflowEditor.vue | 工作流 | 现有 |
| /geo-tools/knowledge-base | KnowledgeBase.vue | GEO 知识库 | 现有 |
| /geo-tools/entity-graph | EntityGraph.vue | 实体图谱 | 现有 |

#### Layer 3: 监控 (group: 'observe')

| 路径 | 组件 | 标题 | 职责 |
|------|------|------|------|
| /geo-tools/funnel-dashboard | FunnelDashboard.vue | 漏斗总览 | **核心可视化**——全链路一张图 |
| /geo-tools/index-tracking | IndexTracking.vue | 收录与引用 | 收录追踪 + AI 引用验证 + 根因分析 |
| /geo-tools/visibility | VisibilityBoard.vue | 可见性趋势 | 现有 |
| /geo-tools/sov-board | SovBoard.vue | 竞品 SOV | 现有 |
| /geo-tools/crawler-stats | CrawlerStats.vue | 爬虫统计 | 现有 |
| /geo-tools/verification | Verification.vue | 多模型验证 | 现有 |
| /geo-tools/decision-report | DecisionReport.vue | 决策链报表 | 现有 |
| /geo-tools/reports | Reports.vue | 成本报表 | 现有 |
| /geo-tools/alerts | AlertCenter.vue | 告警中心 | 现有 |

### 4.3 菜单分组在 Sidebar 中的呈现

```
🏠 GEO 智能优化
├── 🛠️ 配置
│   ├── 品牌与域名
│   ├── SEO 基础设施
│   ├── 蜘蛛推送配置
│   ├── 静态站部署
│   ├── Schema 模板
│   └── 竞品管理
├── 🚀 执行
│   ├── 关键词蒸馏
│   ├── 内容创作
│   ├── 文章优化
│   ├── 发布到官网
│   ├── 蜘蛛推送
│   ├── 多平台发布
│   ├── 工作流
│   ├── GEO 知识库
│   └── 实体图谱
└── 📊 监控
    ├── 漏斗总览
    ├── 收录与引用
    ├── 可见性趋势
    ├── 竞品 SOV
    ├── 爬虫统计
    ├── 多模型验证
    ├── 决策链报表
    ├── 成本报表
    └── 告警中心
```

---

## 五、后端 Service 架构

### 5.1 全景图（现有 + 新增）

```
internal/geo/service/
├── config.go               ConfigOptimizerService       → 现有
├── techconfig.go           TechConfigService            → 现有 + 扩展 v2
├── keyword.go              KeywordService               → 现有
├── keyword_enhance.go      KeywordEnhanceService        → 现有
├── keyword_mining.go       KeywordMiningService         → ★新增
├── content.go              ContentService               → 现有 + 扩展
├── platform.go             PlatformService              → 现有
├── site.go                 SiteDeployService            → ★新增
├── push.go                 PushService                  → ★新增
├── crawler.go              CrawlerService               → 现有
├── monitor_crawler.go      MonitorCrawlerService        → 现有
├── search_probe.go         ProbeService                 → 现有
├── visibility.go           VisibilityService            → 现有
├── verification.go         VerificationService          → 现有
├── index_tracker.go        IndexTrackerService          → ★新增
├── intent_matrix.go        IntentMatrixService          → 现有
├── workflow.go             WorkflowService              → 现有
├── entity_extractor.go     EntityExtractorService       → 现有
├── kb.go                   KBService                    → 现有
├── metrics.go              MetricsService               → 现有
├── decision_analytics.go   GeoDecisionAnalyticsService  → 现有
├── report.go               ReportService                → 现有
├── alert.go                AlertService                 → 现有
├── scheduler.go            SchedulerService             → 现有
├── llm.go                  LLMService                   → 现有
├── prompts.go              PromptManager                → 现有 + 扩展
├── prompt_fanout.go        PromptFanoutService          → 现有
├── geo_audit.go            TechConfigService            → 现有
└── browser_publisher.go    BrowserPublisher             → 现有
```

### 5.2 KeywordMiningService（★新增）

```go
// service/keyword_mining.go

type KeywordMiningService struct {
    kwSvc *KeywordService
    llm   *LLMService
}

// CrawlSuggest 并发抓 5 引擎下拉词
// engines: ["baidu","bing","google","360","sogou"]
// 返回去重后的 GeoKeyword 列表（source="suggest_"+engine, layer="suggest"）
func (s *KeywordMiningService) CrawlSuggest(ctx context.Context, seedWords []string, engines []string) ([]*model.GeoKeyword, error)

// CombineLongtail 模板化长尾词组合
// 模板示例：
//   疑问: ["如何选择{seed}系统", "{seed}系统哪个好用"]
//   对比: ["{seed}免费版和付费版区别", "{seed}和XX对比"]
//   推荐: ["2026年{seed}推荐", "中小企业{seed}选型指南"]
// 自动填 layer="longtail", query_intent, funnel_stage
func (s *KeywordMiningService) CombineLongtail(ctx context.Context, seedWords []string, templates []string) ([]*model.GeoKeyword, error)

// ClassifyIntent LLM 分类 query_intent（how_to/comparison/recommendation/pricing/case_study/problem）
func (s *KeywordMiningService) ClassifyIntent(ctx context.Context, keywords []string) (map[string]string, error)

// BuildFunnel 从 geo_keywords 构建 4 层漏斗统计
type KeywordFunnel struct {
    SeedCount     int
    RelatedCount  int
    SuggestCount  int
    LongtailCount int
    Total         int
    Intents       map[string]int   // query_intent → count
    FunnelStages  map[string]int   // funnel_stage → count
}
func (s *KeywordMiningService) BuildFunnel(ctx context.Context) (*KeywordFunnel, error)
```

### 5.3 PushService（★新增）

```go
// service/push.go

// 统一 Pusher 接口
type Pusher interface {
    Name() string
    Push(ctx context.Context, urls []string) ([]PushResult, error)
}

type PushResult struct {
    URL         string
    Success     bool
    RemainQuota int
    Error       string
}

// 6 个实现
type BaiduPusher struct { Site, Token string }
// POST http://data.zz.baidu.com/urls?site=SITE&token=TOKEN
// Content-Type: text/plain（每行一个 URL）
// 批量 2000 URL/次，返回 {"success":N, "remain":N}

type GooglePusher struct { Projects []GoogleProject }
// Batch multipart/mixed，每批 100 URL
// 配额 200 URL/天/Project，多 Project Round-Robin

type IndexNowPusher struct { Host, Key, KeyLocation string }
// POST https://api.indexnow.org/indexnow
// Content-Type: application/json; charset=utf-8
// body: {"host","key","keyLocation","urlList":[...]}
// 无限额，覆盖 Bing/Yandex/Naver/Seznam/Yep/Amazon

type SitemapPusher struct { SitemapRepo repository.GeoSiteRepository }
// 生成 sitemap.xml → 提交各站长平台 URL

// QuotaManager 配额管理
type QuotaManager struct {
    limits map[string]int  // platform → 每日上限
    used   map[string]int  // platform → 今日已用
}
func (q *QuotaManager) Allow(platform string, n int) bool
func (q *QuotaManager) Consume(platform string, n int)
func (q *QuotaManager) Reset()  // 定时 00:00 调用

// PushService 编排器
type PushService struct {
    pushers  []Pusher
    quota    *QuotaManager
    records  repository.GeoPushRecordRepository
    articles repository.GeoArticleRepository
}

// PushArticle 文章部署后自动推所有启用的引擎
func (s *PushService) PushArticle(ctx context.Context, article *model.GeoArticle) error

// PushManual 手动选引擎推指定 URL 列表
func (s *PushService) PushManual(ctx context.Context, urls []string, platforms []string) ([]*model.GeoPushRecord, error)

// GenerateAndSubmitSitemap 全量 sitemap 生成 + 提交各站长平台（定时调用）
func (s *PushService) GenerateAndSubmitSitemap(ctx context.Context) error
```

### 5.4 SiteDeployService（★新增）

```go
// service/site.go

type SiteDeployService struct {
    siteRepo    repository.GeoSiteRepository
    articleRepo repository.GeoArticleRepository
    techConfig  *TechConfigService
}

// ExportToHugo 把 geo_articles 已发布文章 → Hugo content/posts/{slug}.md
// 同时生成 llms.txt v2 + robots.txt + sitemap.xml 到 Hugo static/
func (s *SiteDeployService) ExportToHugo(ctx context.Context) (int, error)

// TriggerDeploy git add hugo/ → commit → push → GitHub Actions → Cloudflare Pages
func (s *SiteDeployService) TriggerDeploy(ctx context.Context) (string, error)

// HealthCheck 部署后健康检查
// - HTTPS 200
// - llms.txt 可访问 + describedby Link 头存在
// - robots.txt 可访问
// - sitemap.xml 可访问
// - 关键页面响应 < 200ms
// - Schema JSON-LD 存在
func (s *SiteDeployService) HealthCheck(ctx context.Context, site *model.GeoSite) (*SiteHealth, error)
```

### 5.5 IndexTrackerService（★新增）

```go
// service/index_tracker.go

type IndexTrackerService struct {
    articleRepo  repository.GeoArticleRepository
    trackingRepo repository.GeoIndexTrackingRepository
    probeSvc     *ProbeService
    verifySvc    *VerificationService
}

// VerifyArticleFull 单篇文章全链路验证
// 1) ProbeService: 6 引擎（百度/Google/Bing/字节/神马/360）搜索收录 + 排名
// 2) VerificationService: 4 AI（豆包/文心/Kimi/DeepSeek）问答验证引用
// 3) 结果写入 geo_index_tracking
// 4) 计算「站位评分」：被 N 个 AI 引用的占比
func (s *IndexTrackerService) VerifyArticleFull(ctx context.Context, articleID string) (*ArticleStanding, error)

// FunnelStats 全链路漏斗统计（核心监控方法）
// 关键词数 → 文章数 → 已部署数 → 被收录数 → 被 AI 引用数
// 按引擎拆分，按日期聚合（数据源：geo_index_tracking + geo_daily_stats）
type FunnelStats struct {
    SeedKeywords    int
    TotalArticles   int
    DeployedArticles int
    IndexedArticles map[string]int   // engine → count
    AICitedArticles map[string]int   // AI engine → count
    ConversionRates map[string]float64
    Trend30d        []DailyFunnelPoint
}
func (s *IndexTrackerService) FunnelStats(ctx context.Context, dateRange DateRange) (*FunnelStats, error)

// AutoVerifyCron 定时任务入口（每日 02:00 跑）
// 扫描前一天发布的文章 → 全链路验证 → Upsert geo_daily_stats → 告警异常
func (s *IndexTrackerService) AutoVerifyCron(ctx context.Context) error
```

### 5.6 扩展现有 Service

| Service | 扩展点 |
|---------|--------|
| `TechConfigService.GenerateLLMsTxt` | 输出 v2 格式 + 自动从 geo_articles 动态读取 + 生成 describedby Link 头 |
| `TechConfigService.GenerateRobots` | 加 2026 完整 AI 爬虫白名单（8 国内 + 13 海外） |
| `ContentService.GenerateContent` | 加倒金字塔强制 + 问题式 H2 强制 + Schema 自动注入 + FAQ 区块强制 |
| `PromptManager` | 新增 `GeoV2ContentPrompt`（倒金字塔 + 可引用性 + 品牌自然提及） |
| `SchedulerService` | 新增每日 02:00 AutoVerifyCron + 00:05 ResetQuotaCron |

---

## 六、数据模型（ALTER + 新增）

### 6.1 现有表 ALTER（2 张）

```sql
-- geo_keywords: 加 6 个字段
ALTER TABLE geo_keywords ADD COLUMN layer VARCHAR(20) DEFAULT 'seed';
-- 取值: 'seed' | 'related' | 'suggest' | 'longtail'
ALTER TABLE geo_keywords ADD COLUMN parent_id VARCHAR(36) REFERENCES geo_keywords(id);
ALTER TABLE geo_keywords ADD COLUMN query_intent VARCHAR(30);
-- 取值: 'how_to' | 'comparison' | 'recommendation' | 'problem' | 'pricing' | 'case_study'
ALTER TABLE geo_keywords ADD COLUMN suggest_engines TEXT;  -- JSON 数组: ["baidu","bing","google","360","sogou"]
ALTER TABLE geo_keywords ADD COLUMN suggest_count INT DEFAULT 0;
ALTER TABLE geo_keywords ADD COLUMN last_mined_at TIMESTAMP;

-- geo_articles: 加 5 个字段
ALTER TABLE geo_articles ADD COLUMN site_url VARCHAR(500);
-- https://domain.com/article/crm-guide.html
ALTER TABLE geo_articles ADD COLUMN site_path VARCHAR(200);
-- /article/crm-guide.html
ALTER TABLE geo_articles ADD COLUMN schema_json JSONB;
-- 完整 JSON-LD（可能含多个 @type）
ALTER TABLE geo_articles ADD COLUMN markdown_path VARCHAR(200);
-- /article/crm-guide.html.md (v2 Markdown 孪生)
ALTER TABLE geo_articles ADD COLUMN deployed_at TIMESTAMP;
ALTER TABLE geo_articles ADD COLUMN llms_included BOOLEAN DEFAULT FALSE;
```

### 6.2 新增表（6 张）

```sql
-- geo_push_records: 蜘蛛推送记录
CREATE TABLE geo_push_records (
    id            VARCHAR(36) PRIMARY KEY,
    article_id    VARCHAR(36) REFERENCES geo_articles(id) ON DELETE CASCADE,
    platform      VARCHAR(30) NOT NULL,
    -- 'baidu' | 'google' | 'indexnow' | 'toutiao' | 'shenma' | 'sitemap'
    url           TEXT NOT NULL,
    batch_id      VARCHAR(36),
    success_count INT DEFAULT 0,
    fail_count    INT DEFAULT 0,
    remain_quota  INT,
    error_msg     TEXT,
    pushed_at     TIMESTAMP DEFAULT NOW(),
    verified      BOOLEAN DEFAULT FALSE,
    verified_at   TIMESTAMP
);
CREATE INDEX idx_push_records_platform ON geo_push_records(platform);
CREATE INDEX idx_push_records_url ON geo_push_records(url);
CREATE INDEX idx_push_records_pushed_at ON geo_push_records(pushed_at);

-- geo_sites: 站点配置
CREATE TABLE geo_sites (
    id             VARCHAR(36) PRIMARY KEY,
    domain         VARCHAR(255) NOT NULL UNIQUE,
    provider       VARCHAR(50) DEFAULT 'cloudflare',
    hugo_path      VARCHAR(500),
    git_repo       VARCHAR(255),
    deploy_cmd     VARCHAR(255),
    health_score   INT DEFAULT 0,
    last_deploy_at TIMESTAMP,
    active         BOOLEAN DEFAULT TRUE,
    created_at     TIMESTAMP DEFAULT NOW()
);

-- geo_pusher_configs: 蜘蛛平台配置（凭据 AES-256-GCM 加密）
CREATE TABLE geo_pusher_configs (
    id          VARCHAR(36) PRIMARY KEY,
    platform    VARCHAR(30) NOT NULL UNIQUE,
    config_json JSONB NOT NULL,
    -- baidu:   {"site":"...","token":"..."}
    -- google:  {"projects":[{"client_email":"...","private_key":"..."}]}
    -- indexnow: {"host":"...","key":"...","keyLocation":"..."}
    daily_limit INT,
    used_today  INT DEFAULT 0,
    last_reset_at DATE,
    active      BOOLEAN DEFAULT TRUE
);

-- geo_index_tracking: 收录 + AI 引用追踪
CREATE TABLE geo_index_tracking (
    id            VARCHAR(36) PRIMARY KEY,
    article_id    VARCHAR(36) REFERENCES geo_articles(id) ON DELETE CASCADE,
    engine        VARCHAR(30) NOT NULL,
    -- 'baidu' | 'google' | 'bing' | 'toutiao' | 'shenma' | '360' | 'doubao' | 'wenxin' | 'kimi' | 'deepseek'
    keyword       TEXT NOT NULL,
    indexed       BOOLEAN DEFAULT FALSE,
    rank_position INT,
    ai_cited      BOOLEAN DEFAULT FALSE,
    ai_cite_count INT DEFAULT 0,
    last_checked  TIMESTAMP,
    checked_at    TIMESTAMP DEFAULT NOW()
);
CREATE INDEX idx_index_tracking_article ON geo_index_tracking(article_id);
CREATE INDEX idx_index_tracking_engine ON geo_index_tracking(engine);
CREATE INDEX idx_index_tracking_checked_at ON geo_index_tracking(checked_at);

-- geo_schema_templates: Schema 模板库
CREATE TABLE geo_schema_templates (
    id           VARCHAR(36) PRIMARY KEY,
    page_type    VARCHAR(50) NOT NULL,
    -- 'article' | 'faq' | 'guide' | 'product'
    schema_type  VARCHAR(50) NOT NULL,
    -- 'FAQPage' | 'Product' | 'Article' | 'Comparison' | 'Organization'
    template_json JSONB NOT NULL,
    active       BOOLEAN DEFAULT TRUE,
    created_at   TIMESTAMP DEFAULT NOW()
);

-- geo_daily_stats: 已有表，加 AI 引擎扩展（不需要新表，只加 engine 取值）
-- 现有字段已足够：
--   stat_date + engine + intent + funnel_stage (unique)
--   brand_mentioned_count / competitor_mentioned_count / citation_count / negative_count / probe_count
--   engine 取值扩展: 'doubao' | 'wenxin' | 'kimi' | 'deepseek'（AI 引擎）
```

---

## 七、蜘蛛推送 6 引擎完整方案

### 7.1 各引擎技术细节

#### 百度主动推送 API

```
POST http://data.zz.baidu.com/urls?site=yourdomain.com&token=YOUR_TOKEN
Content-Type: text/plain

https://yourdomain.com/article/crm-guide
https://yourdomain.com/article/crm-free-vs-paid
（每行一个 URL，最多 2000 条）
```

响应：
```json
{"success":5, "remain":1995}
```

配额用完（`remain=0`）→ 降级到 sitemap 全量提交（每周一）。

#### Google Indexing API

前置步骤（一次性）：
```
1. console.cloud.google.com 创建 Project
2. 启用 "Web Search Indexing API"
3. 创建 Service Account → 下载 JSON key
4. Search Console 添加站点 + Service Account 加为"所有者"
```

推送请求：
```
POST https://indexing.googleapis.com/v3/urlNotifications:publish
Authorization: Bearer ACCESS_TOKEN
Content-Type: application/json

{"url":"https://domain.com/article/crm-guide.html", "type":"URL_UPDATED"}
```

批量请求（multipart/mixed，每批 100 URL，节省 HTTP 连接）：
```
POST https://indexing.googleapis.com/batch
Content-Type: multipart/mixed; boundary=BOUNDARY
Authorization: Bearer ACCESS_TOKEN

--BOUNDARY
Content-Type: application/http

POST /v3/urlNotifications:publish
Content-Type: application/json

{"url":"...","type":"URL_UPDATED"}
--BOUNDARY
Content-Type: application/http

POST /v3/urlNotifications:publish
Content-Type: application/json

{"url":"...","type":"URL_UPDATED"}
--BOUNDARY--
```

配额管理：
- 默认 200 URL/天/Project
- 多 Project Round-Robin（配置里存 N 个 Service Account JSON）
- `used_today` 字段每日 00:05 重置

#### IndexNow（覆盖 Bing/Yandex/Naver/Seznam/Yep/Amazon）

前置步骤：
```bash
# 1. 生成 key（UUID）
uuidgen  # → a1b2c3d4-5678-9abc-def0-123456789abc

# 2. 在网站根目录放 {key}.txt
# https://yourdomain.com/a1b2c3d4-5678-9abc-def0-123456789abc.txt
# 文件内容 = key 本身（一行纯文本）
```

批量推送（JSON POST，最多 10000 URL/次，**无限额**）：
```
POST https://api.indexnow.org/indexnow
Content-Type: application/json; charset=utf-8

{
  "host": "yourdomain.com",
  "key": "a1b2c3d4-5678-9abc-def0-123456789abc",
  "keyLocation": "https://yourdomain.com/a1b2c3d4-5678-9abc-def0-123456789abc.txt",
  "urlList": [
    "https://yourdomain.com/article/crm-guide",
    "https://yourdomain.com/article/crm-free-vs-paid"
  ]
}
```

单 URL 极简 GET：
```
GET https://api.indexnow.org/indexnow?host=yourdomain.com&key=KEY&url=https://...
```

### 7.2 推送触发链

```
文章发布（geo_articles.status → 'published'）
    ↓
SiteDeployService.ExportToHugo() 写入 Hugo content/
    ↓
SiteDeployService.TriggerDeploy() → git push → Cloudflare Pages 部署
    ↓ （不等部署完成，立即推）
PushService.PushArticle(ctx, article)
    ↓
遍历所有 active 的 Pusher：
    BaiduPusher.Push()        → quota 够就推，不够跳过
    GooglePusher.Push()       → 多个 Project Round-Robin
    IndexNowPusher.Push()     → 无限额，全推
    SitemapPusher.Queue()     → 加入 sitemap 生成队列
    ↓
结果写入 geo_push_records（success_count / remain_quota / error_msg）
    ↓
每日 00:05 QuotaManager.Reset() → geo_pusher_configs.used_today = 0
每日 02:00 Scheduler → PushService.GenerateAndSubmitSitemap()
```

### 7.3 配置凭据加密

```
geo_pusher_configs.config_json 用 AES-256-GCM 加密后存 DB：

加密流程：
  plaintext JSON → AES-256-GCM(GEO_PUSHER_SECRET_KEY from env) → base64 → DB

解密流程：
  DB value → base64 → AES-256-GCM decrypt → plaintext JSON

密钥来源：环境变量 GEO_PUSHER_SECRET_KEY（≥32 字节）
未配置密钥：降级明文 + 记录告警（保证连续性）
接口层一律脱敏（dto 只返回 masked 值，不回显明文）
```

---

## 八、监控可视化设计

### 8.1 漏斗总览（FunnelDashboard.vue）

```
┌─────────────────────────────────────────────────────────────────────────┐
│ 漏斗总览                                                                 │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌─ 双漏斗（SEO × GEO）─────────────────────────────────────────────┐   │
│  │                                                                   │   │
│  │  关键词        文章        已部署        被收录        被 AI 引用   │   │
│  │  ┌──────┐     ┌──────┐     ┌──────┐     ┌──────┐     ┌──────┐   │   │
│  │  │ 4567 │ →   │  312 │ →   │  298 │ →   │  264 │ →   │  111 │   │   │
│  │  │ ████ │     │ ████ │     │ ████ │     │ ████ │     │ ██   │   │   │
│  │  └──────┘     └──────┘     └──────┘     └──────┘     └──────┘   │   │
│  │  100%         6.8%         95.5%        88.6%        42.0%      │   │
│  │                                                                   │   │
│  │  ⚠️ 瓶颈分析：                                                     │   │
│  │    1. 关键词→文章 转化率 6.8%（建议：批量生成 + 自动发布）          │   │
│  │    2. AI 引用率 42%（Kimi/DeepSeek 较低，建议：补 Schema）          │   │
│  └───────────────────────────────────────────────────────────────────┘   │
│                                                                         │
│  ┌─ 引擎对比表 ────────────────────────────────────────────────────┐   │
│  │  引擎     │ 收录率 │ 平均排名 │ AI 引用 │ 30天趋势               │   │
│  │  ─────────┼────────┼──────────┼────────┼────────────────────  │   │
│  │  百度      │  87%   │  #3.4    │ 文心 62% │ 📈 +5% 本周          │   │
│  │  Google    │  72%   │  #8.1    │  —       │ 📈 +3%              │   │
│  │  Bing      │  68%   │  #7.2    │ Copilot  │ 📈                  │   │
│  │  豆包      │  —     │ —        │ 豆包 58% │ 📈 +8%              │   │
│  │  Kimi      │  —     │ —        │ Kimi 21% │ 📉 ↓3%             │   │
│  │  DeepSeek  │  —     │ —        │ DS 17%  │ ⚠️ 首次抓取中       │   │
│  └───────────────────────────────────────────────────────────────────┘   │
│                                                                         │
│  ┌─ 30 天趋势图（4 条折线叠加）────────────────────────────────────┐   │
│  │  · 被收录文章数（蓝色）                                          │   │
│  │  · 品牌提及次数（绿色）                                          │   │
│  │  · AI 引用次数（紫色）                                           │   │
│  │  · 竞品提及次数（红色虚线）                                      │   │
│  │  数据源: geo_daily_stats                                         │   │
│  └───────────────────────────────────────────────────────────────────┘   │
│                                                                         │
│  ┌─ 今日推送状态 ──────────────────────────────────────────────────┐   │
│  │  百度    ████░░░░  2845/5000  ✅                                 │   │
│  │  Google  ██░░░░░░  37/200     ✅                                 │   │
│  │  IndexNow ████████  ∞/∞        ✅                                 │   │
│  │  sitemap 每周一全量提交                                          │   │
│  └───────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
```

### 8.2 收录追踪 + AI 引用验证（IndexTracking.vue）

```
┌─────────────────────────────────────────────────────────────────────────┐
│ 收录与 AI 引用                                                          │
├─────────────────────────────────────────────────────────────────────────┤
│  🔍 URL/关键词 [________]   引擎 [全部▼]   收录状态 [全部▼]   AI引用 [全部▼]│
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌─ 文章列表 ────────────────────────────────────────────────────────┐   │
│  │  文章                             关键词       收录  AI引用  操作  │   │
│  │  ────────────────────────────────────────────────────────────── │   │
│  │  如何选择CRM系统                   CRM选型     ✅百度  ✅豆包     │   │
│  │  https://domain.com/article/crm-guide.html              ✅文心   │   │
│  │                                                          ❌Kimi   │   │
│  │                                                          ❌DS     │   │
│  │  2026年中小企业CRM推荐             CRM推荐     ✅百度  ✅文心  [验证]│   │
│  │                                             ❌Google ❌DS  [详情]│   │
│  └───────────────────────────────────────────────────────────────────┘   │
│                                                                         │
│  ┌─ 详情展开：AI 引用分析 + 根因 ───────────────────────────────────┐   │
│  │                                                                   │   │
│  │  URL: https://domain.com/article/crm-guide.html                  │   │
│  │                                                                   │   │
│  │  ┌─ 豆包 ✅ 引用 ──────────────────────────────────────────┐     │   │
│  │  │  回答摘录: "根据某企业官网的分析，选择CRM时..."          │     │   │
│  │  │  品牌匹配: ✓                                             │     │   │
│  │  └─────────────────────────────────────────────────────────┘     │   │
│  │  ┌─ Kimi ❌ 未引用 ───────────────────────────────────────┐     │   │
│  │  │  根因分析:                                              │     │   │
│  │  │  · Kimi 用 DeepSeek-Spider → 需推 DeepSeekBot          │     │   │
│  │  │  · 缺 FAQPage Schema（AI 问答最高权重）                  │     │   │
│  │  │  💡 建议: 补 Schema + 知乎发同名内容带链接                │     │   │
│  │  └─────────────────────────────────────────────────────────┘     │   │
│  │                                                                   │   │
│  │  [🔄 立即全链路验证]  下次自动: 明天 02:00                        │   │
│  └───────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
```

### 8.3 数据血缘

```
geo_keywords (4567)
    ↓ content 生成
geo_articles (312)
    ↓ site 部署
GeoArticle.SiteURL (298)
    ↓ push 蜘蛛
geo_push_records (N 次)
    ↓ 等待 + 验证
geo_index_tracking (264)
    ↓ 每日定时 02:00 Upsert
geo_daily_stats  ← 已有表！SEO × GEO 统一聚合点

同时 feed 给：
  ├─ FunnelDashboard.vue   → 漏斗可视化（关键词→文章→收录→AI引用）
  ├─ SovBoard.vue          → 竞品 SOV（brand_mentioned_count vs competitor_mentioned_count + citation_count）
  ├─ VisibilityBoard.vue   → 可见性趋势（engine × date 折线）
  ├─ AlertCenter.vue       → 异常告警（连续 3 天 AI 引用下降 > 30%）
  └─ DecisionReport.vue     → 决策链报表
```

### 8.4 SOV 公式升级（SEO + GEO 统一）

```
SOV_score(brand, engine, intent) =
    (BrandMentionedCount × base_weight + CitationCount × ai_weight)
    / (BrandMentionedCount + CompetitorMentionedCount + CitationCount)

// ai_weight 按引擎差异化：
//   百度:   ai_weight = 1.5  （文心一言从百度索引拿数据）
//   豆包:   ai_weight = 2.0  （豆包回答中引用=直接驱动流量）
//   Kimi:   ai_weight = 2.0
//   Google: ai_weight = 1.0  （传统搜索，品牌提及主导）
//   Bing:   ai_weight = 1.3  （Copilot 从 Bing 拿数据）
```

---

## 九、SEO × GEO 数据整合

### 9.1 geo_daily_stats 已有字段的妙用

```go
// 已有表，完全够用！
type GeoDailyStat struct {
    Date                     string    `uniqueIndex: idx_date_engine_intent`
    Engine                   string    `uniqueIndex`
    Intent                   string    `uniqueIndex`
    FunnelStage              string    // cognitive / evaluation / decision / retention
    BrandMentionedCount      int       // ← SEO 指标（品牌被提及次数）
    CompetitorMentionedCount int       // ← 竞品对比
    CitationCount            int       // ← GEO 指标（AI 引擎引用次数）
    NegativeCount            int       // ← 负面声量
    ProbeCount               int       // 当日探针次数
}
```

### 9.2 每日 Upsert 任务

```
定时任务（每日 02:00）:
1. 扫描 geo_index_tracking 最新记录（前一天的文章验证结果）
2. 按 engine + intent + funnel_stage 聚合：
   - indexed_count（收录文章数）
   - ai_cited_count（AI 引用次数）
   - brand_mentioned（从 geo_probe_runs / geo_query_chains 拉）
   - competitor_mentioned（同上）
3. Upsert 到 geo_daily_stats（ON CONFLICT DO UPDATE）
4. 同时计算 SOV_score → 写 DecisionReport
5. 检测异常 → 触发 AlertCenter（连续 3 天 AI 引用下降 > 30%）
```

### 9.3 一个表驱动全部监控

```
geo_daily_stats
  │
  ├── FunnelDashboard.vue     → 漏斗各环节转化率
  │                              (keywords→articles→deployed→indexed→ai_cited)
  │
  ├── SovBoard.vue            → 竞品 SOV
  │                              (brand_mentioned vs competitor_mentioned × engine)
  │
  ├── VisibilityBoard.vue     → 可见性趋势
  │                              (engine × date × 折线)
  │
  ├── AlertCenter.vue         → 异常告警
  │                              (连续 3 天 citation_count ↓ 30%)
  │
  └── DecisionReport.vue      → 决策链报表
                                 (SOV_score 变化 + ROI + 成本)
```

---

## 十、网站部署流水线

### 10.1 Hugo + Cloudflare Pages 架构

```
geo-run [ExportToHugo: 写 Markdown 到 hugo/content/posts/]
    ↓
geo-run [TriggerDeploy: git add hugo/ → commit → push]
    ↓
GitHub Actions (.github/workflows/deploy.yml):
    uses: peaceiris/actions-hugo@v3  # Hugo 扩展版
    run: hugo --minify               # 构建到 public/
    uses: cloudflare/pages-action@v1 # 部署到 Cloudflare Pages
    ↓
Cloudflare Pages: 全球 CDN + 自动 HTTPS + Edge 快
```

### 10.2 Hugo 目录约定

```
hugo/
├── content/
│   ├── posts/
│   │   ├── crm-guide-how-to-choose.md    ← ExportToHugo 写入
│   │   ├── crm-free-vs-paid.md
│   │   └── ...
│   ├── faq/
│   │   ├── crm-pricing.md
│   │   └── ...
│   ├── guides/
│   │   └── top-crm-2026.md
│   └── product/
│       └── feature-list.md
├── static/
│   ├── llms.txt           ← GenerateLlmsTxt() 生成
│   ├── robots.txt         ← GenerateRobots() 生成
│   ├── sitemap.xml        ← GenerateSitemap() 生成（或 Hugo 自带）
│   └── {INDEXNOW_KEY}.txt ← IndexNow 验证文件
├── layouts/
│   └── _default/single.html  ← 模板里注入 Schema JSON-LD + Markdown 孪生 Link 头
├── public/               ← Hugo 构建产物（Cloudflare Pages 从这里部署）
└── config.toml
```

### 10.3 llms.txt v2 HTTP Link 头

**两种方式都要做**（覆盖不同的 AI Agent）：

**方式 1：Cloudflare Pages _headers 文件（每个页面都带上）**
```
# hugo/static/_headers
/*
  Link: </llms.txt>; rel="describedby"; type="text/markdown"
  Link: </sitemap.xml>; rel="sitemap"
```

**方式 2：Go TechConfigService.GenerateLLMsTxt() 动态生成**
```go
func (s *TechConfigService) GenerateLLMsTxt(cfg *LLMsTxtConfig) string {
    // 从 geo_articles 查已部署文章
    // 按 funnel_stage 分组
    // 输出 v2 格式 Markdown:
    // # 企业名
    // > 一句话简介
    // ## Guides
    // - [如何选型](https://domain.com/guide/crm-guide): 选型指南
    // ## FAQ
    // - [CRM 价格](https://domain.com/faq/crm-pricing): 各版本价格说明
}
```

### 10.4 Markdown 孪生（v2 要求）

**方案：Hugo 原生支持**
```toml
# hugo/config.toml
# Hugo 会把 content/posts/xxx.md 构建成 public/posts/xxx/index.html
# 我们同时让 Markdown 源文件也可公开访问：

[[static]]
  source = "content/posts"
  target = "posts"
# 这样 public/posts/xxx.md 就直接可访问了
# 完整 URL: https://domain.com/posts/crm-guide-how-to-choose.md

# 然后在模板里加 Link 头：
# hugo/layouts/_default/single.html:
# <link rel="alternate" type="text/markdown" href="{{ .Site.BaseURL }}posts/{{ .File.BaseFileName }}.md">
```

### 10.5 Hugo GitHub Actions

```yaml
# .github/workflows/deploy.yml
name: deploy
on:
  push:
    branches: [main]
    paths: ['hugo/content/**', 'hugo/static/**']
  workflow_dispatch:

jobs:
  build:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: hugo
    steps:
      - uses: actions/checkout@v4
      - uses: peaceiris/actions-hugo@v3
        with:
          hugo-version: '0.132.0'
          extended: true
      - run: hugo --minify
      - uses: cloudflare/pages-action@v1
        with:
          apiToken: ${{ secrets.CLOUDFLARE_API_TOKEN }}
          accountId: ${{ secrets.CLOUDFLARE_ACCOUNT_ID }}
          projectName: geo-site
          directory: hugo/public
```

---

## 十一、实施计划（15 个工作日）

### Phase 1：基础设施（3d）

| 天 | 任务 | 产出 |
|----|------|------|
| 1 | DB 迁移：ALTER geo_keywords/geo_articles + CREATE 6 新表 | migration SQL |
| 2 | Repo + DTO + Controller 骨架（5 张新表 + 2 ALTER 表） | 代码骨架 + go build |
| 3 | Router 重构：geoTools.js 按 Config/Execute/Observe 分组 | 新路由文件 |

### Phase 2：关键词 + 蜘蛛推送（4d）

| 天 | 任务 | 产出 |
|----|------|------|
| 4 | KeywordMiningService：5 引擎 suggest crawler + JSONP 解析 | 可跑的 CrawlSuggest |
| 5 | CombineLongtail（模板化长尾组合）+ ClassifyIntent（LLM 分类） | 长尾词生成 |
| 6 | PushService：IndexNowPusher + BaiduPusher + QuotaManager | 2 个 Pusher + 配额管理 |
| 7 | PushService：GooglePusher（JWT + batch multipart + 多 Project） | Google 推送完整实现 |

### Phase 3：静态站 + 部署（2d）

| 天 | 任务 | 产出 |
|----|------|------|
| 8 | Hugo 初始化 + SiteDeployService.ExportToHugo + GenerateLlmsTxt v2 | Hugo 项目 + 导出能力 |
| 9 | SiteDeployService.TriggerDeploy + HealthCheck + GitHub Actions + Cloudflare Pages | 部署流水线跑通 |

### Phase 4：内容 + 监控（4d）

| 天 | 任务 | 产出 |
|----|------|------|
| 10 | ContentService 扩展（倒金字塔 + 问题式 H2 + Schema 自动注入） | 生成的文章符合 GEO v2 |
| 11 | IndexTrackerService：VerifyArticleFull（Probe + Verification 联动） | 单篇全链路验证 |
| 12 | IndexTrackerService.FunnelStats + AutoVerifyCron + daily_stats Upsert | 定时跑 + 漏斗数据 |
| 13 | SchedulerService 接入 + AlertCenter 异常检测 | 告警联动 |

### Phase 5：前端页面（并行，后端同步）

| 天 | 页面 | 产出 |
|----|------|------|
| 4-5 | KeywordMining.vue 升级（漏斗 + 下拉词 Tab + 长尾组合器） | 升级 |
| 6 | PushCenter.vue（推送队列 + 配额仪表盘） | 新增 |
| 7 | PusherConfig.vue + SiteConfig.vue | Config 层 |
| 8-9 | SitePublish.vue（一键部署 + HealthCheck） | 新增 |
| 10 | SeoInfra.vue（robots/sitemap/llms.txt 在线编辑） | Config 层 |
| 11 | SchemaTemplates.vue | Config 层 |
| 12-13 | FunnelDashboard.vue（核心漏斗图 + 引擎对比 + 30 天趋势） | **核心可视化** |
| 14 | IndexTracking.vue（收录 + AI 引用 + 根因分析） | 新增 |
| 15 | 全部联调 + Bug 修复 + git push 双远端 | 交付 |

### 并行策略

```
Day 1-3: 后端 DB + Repo + Controller 骨架 + 前端路由重构
Day 4-7: 后端 KeywordMiningService + PushService（4天）
         ║ 同时：前端 PushCenter.vue + PusherConfig.vue + KeywordMining.vue 升级
Day 8-9: 后端 SiteDeployService + Hugo + GitHub Actions（2天）
         ║ 同时：前端 SitePublish.vue + SeoInfra.vue + SchemaTemplates.vue
Day 10-13: 后端 ContentService 扩展 + IndexTrackerService + 定时任务
           ║ 同时：前端 FunnelDashboard.vue + IndexTracking.vue
Day 14-15: 联调 + 修复 + 交付

总工期: ~15 工作日（后端 ~12.5d + 前端 ~10.5d 并行）
```

---

## 十二、风险与已知限制

| 风险 | 影响 | 缓解 |
|------|------|------|
| **Google Indexing API 200 URL/天配额不够** | 新文章推不完 | 多 GCP Project Round-Robin；降级到 sitemap 自然爬 |
| **百度主动推送 token 获取门槛** | 需要百度搜索资源平台账号，首次可能需验证 | 走站长平台人工申请；配额用完降级 sitemap |
| **AI 爬虫可能不遵守 robots.txt** | Bytespider / DeepSeekBot 已知会忽略 | 监控 crawler_visits；必要时 CDN WAF 层拦截 |
| **下拉词 suggest 接口可能封 IP** | 抓取失败 | 加 User-Agent 轮换；Redis 缓存结果 24h；失败降级 LLM 造词 |
| **Google Service Account 必须加为 Owner** | 推送报 403 | 开发阶段人工验证；GSC 老版页面也加一次（Google bug） |
| **Hugo + Cloudflare 部署可能偶发失败** | 文章不在线就推给蜘蛛 | SiteDeployService.HealthCheck 部署后强制检查；失败不触发推送 |
| **AI 引用验证需要调用 AI API** | 成本增加 | 复用 geo_articles.keyword 字段；复用现有 ProbeService；每日只验证前一天发布的 |
| **Schema JSON-LD 注入可能与作者手动写的冲突** | 重复 Schema | 在 GeoArticle 加 schema_json 字段；页面渲染时检查去重 |
| **v2 Markdown 孪生可能影响 Hugo 路由** | 页面 404 | 用 `[[static]]` 配置单独暴露 Markdown 源；测试验证 |

---

## 附录 A：完整 Go Service 接口签名

### KeywordMiningService

```go
func (s *KeywordMiningService) CrawlSuggest(ctx context.Context, seedWords []string, engines []string) ([]*model.GeoKeyword, error)
func (s *KeywordMiningService) CombineLongtail(ctx context.Context, seedWords []string, templates []string) ([]*model.GeoKeyword, error)
func (s *KeywordMiningService) ClassifyIntent(ctx context.Context, keywords []string) (map[string]string, error)
func (s *KeywordMiningService) BuildFunnel(ctx context.Context) (*KeywordFunnel, error)
```

### PushService

```go
func (s *PushService) PushArticle(ctx context.Context, article *model.GeoArticle) error
func (s *PushService) PushManual(ctx context.Context, urls []string, platforms []string) ([]*model.GeoPushRecord, error)
func (s *PushService) GenerateAndSubmitSitemap(ctx context.Context) error
```

### SiteDeployService

```go
func (s *SiteDeployService) ExportToHugo(ctx context.Context) (int, error)
func (s *SiteDeployService) TriggerDeploy(ctx context.Context) (string, error)
func (s *SiteDeployService) HealthCheck(ctx context.Context, site *model.GeoSite) (*SiteHealth, error)
```

### IndexTrackerService

```go
func (s *IndexTrackerService) VerifyArticleFull(ctx context.Context, articleID string) (*ArticleStanding, error)
func (s *IndexTrackerService) FunnelStats(ctx context.Context, dateRange DateRange) (*FunnelStats, error)
func (s *IndexTrackerService) AutoVerifyCron(ctx context.Context) error
```

---

## 附录 B：推荐 Prompt 片段

### GEO v2 内容生成 Prompt（首段倒金字塔）

```
你是一位 GEO（生成式引擎优化）专家。请围绕关键词「{keyword}」创作一篇
面向 AI 引擎引用的文章。

核心原则：
1. 首段 1-2 句必须是结论，包含品牌自然提及（{brand_name}）
2. H2 标题用问题式（"怎么选"、"哪个好"、"区别是什么"）
3. 至少 3 个 H2，每个 H2 下有 2-3 段支撑
4. 结尾必须有 FAQ 区块（至少 3 个 Q&A）
5. 文中给出具体数据点（价格、数量、年份）提升可引用性
6. 自然提及 {brand_name} 1-2 次/千字

文章结构：
  首段: 结论 + {brand_name} 自然提及
  H2: 问题 1
  H2: 问题 2
  H2: 问题 3
  H2: FAQ
  总结 + CTA
```

### llms.txt 动态生成规则

```
从 geo_articles 查 deployed_at IS NOT NULL 的文章，按 funnel_stage 分组：

## 认知层文章（How-to / 问题解决）
- [标题](site_url): 一句说明，包含 keyword

## 评估层文章（对比 / 排行）
- ...

## 决策层文章（选型 / 价格）
- ...

## FAQ
- ...

## 品牌信息
- 企业简介 URL
- 联系方式 URL
- 产品列表 URL
```

---

## 附录 C：每日定时任务清单

| 时间 | 任务 | 来源 | 依赖 |
|------|------|------|------|
| 00:05 | QuotaManager.Reset() | PushService | geo_pusher_configs |
| 02:00 | IndexTrackerService.AutoVerifyCron() | IndexTrackerService | geo_index_tracking → geo_daily_stats |
| 03:00 | PushService.GenerateAndSubmitSitemap() | PushService | geo_articles → Hugo static/ → 各站长平台 |
| 04:00 | MonitorCrawlerService.RunCrawlerCron() | 现有 | geo_keywords |
| 05:00 | AlertService 异常检测（citation_count 下降） | 现有 | geo_daily_stats |
| 06:00 | ReportService.GetReport() 日度聚合 | 现有 | geo_daily_stats + geo_api_calls |

---

## §八 测试策略

> 本节为 `FEATURE_DOCUMENTATION_TEMPLATE.md` 的**推荐节**，2026-09-15 补齐（原文档无测试策略）。

### 8.1 单元测试（Go）

`user-server/internal/geo/` 下现有 **9** 个测试文件：

| 层 | 文件 | 覆盖重点 |
|----|------|----------|
| repository | `geo_test.go` | 仓储读写、查询链 |
| service | `geo_test.go` | 服务层主流程 |
| service | `intent_matrix_test.go` | 意图矩阵映射 |
| service | `decision_executors_test.go` | 决策执行器 |
| service | `job_manager_test.go` | 作业编排 / 调度 |
| service | `visibility_test.go` | AI 引擎可见性计算 |
| service | `alert_test.go` | 告警判定 |
| service | `geo_api_test.go` | 对外 API 契约 |
| service | `zz_edge_test.go` | 边界与异常路径 |

运行方式：

```bash
cd hivemtk/user-server
go test ./internal/geo/... -p 1 -count=1
```

> 注意：仓库统一使用 `-p 1`（串行）执行 Go 测试，原因见 `hivemtk/CLAUDE.md` 的并发限制说明。

### 8.2 接口级验收

- `scripts/api_verify_full.py` — 项目指定的验收入口，覆盖 geo 相关端点；
- 新增/修改 geo 端点后必须同步更新该脚本，否则视为未完成。

### 8.3 前端验证

- 路由注册：`user-web/src/router/modules/geoTools.js`（24 条路由）需与后端端点一一对应；
- `user-web/tests/` 下的页面级 e2e 用于关键路径回归。

### 8.4 覆盖率

geo 模块纳入仓库整体覆盖率统计，CI 门槛见 `.github/workflows/user-server-ci.yml`：
**FLOOR=20%**（阻断回归底线）/ **TARGET=60%**（产品目标，仅告警）。

### 8.5 已知测试缺口

| 缺口 | 影响 | 建议 |
|------|------|------|
| 无端到端「推送→收录→引用」全链路测试 | 引擎侧变更可能静默失效 | 增加 mock 引擎的集成测试 |
| 外部引擎接口无契约测试 | 上游协议变更无法提前发现 | 定期抓取规范快照做 diff |
| 前端 geo 页面无独立组件测试 | 菜单/表单回归依赖手工 | 补 Vitest 组件用例 |

---

*文档版本: v1.1 · 整合 4 轮对话 · 2026-09-15（v1.1 按 FEATURE_DOCUMENTATION_TEMPLATE 补齐 §一 功能完成状态、§八 测试策略与元数据块）*
