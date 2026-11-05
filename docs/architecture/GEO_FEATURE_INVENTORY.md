# GEO 模块全景清单 (Feature Inventory)

> 业务域：GEO（Generative Engine Optimization，生成式引擎优化）
> 代码位置：`user-server/internal/geo/`
> 路由挂载：`user-server/internal/router/geo_routes.go` → `SetupGeoRoutes(auth, gormDB)`
> 架构约束：多租户（所有 GEO 表带 `tenant_id`，由 GORM 隔离 callback 自动填充/过滤；公开落地页按 code 自证租户），config 写入/优化与 job/字典写入设 AdminAuth

---

## 1. 目录结构

```
internal/geo/
├── doc.go                          模块设计声明 + 全局设计决策锚点
├── controller/                     35 个 Controller 构造器
├── service/                        39 个 Service（含探针/审计/承接/发布适配器）
├── repository/                     35 个 Repository 接口
├── model/                          37 个 GORM Model
└── dto/                            11 个 DTO 文件（请求/响应契约）
```

---

## 2. 五层架构清单

### 2.1 Model 层（37 个模型）

| 文件 | 模型 | 核心字段 | 备注 |
|------|------|---------|------|
| `model/config.go` | `GeoConfig` | brand_name, advantages, competitors, domain | 单例（固定 ID=1），全局品牌配置 |
| `model/keyword.go` | `GeoKeyword` | keyword, category, intent, cluster, search_volume | 关键词池，cluster 做语义分组 |
| `model/content.go` | `GeoArticle` | title, content, keyword, word_count, score, status | 生成文章主表 |
| `model/knowledge.go` | `GeoKnowledgeDocument` | title, content, doc_type, metadata(JSON) | 品牌知识库，待演进 pgvector 语义检索 |
| `model/query_chain.go` | `GeoQueryChain` | chain_id, seq, query, intent, brand_position, cited_urls, one_id | v3 决策链化 Phase1 用户查询思维链 |
| `model/verification.go` | `GeoVerifyResult` | brand_name, query, response, brand_mentioned, sentiment, position | AI 搜索验证结果 |
| `model/crawler_visit.go` | `GeoCrawlerVisit` | user_agent, path, engine(GPTBot/PerplexityBot), ip | v3 A6 爬虫访问记录 |
| `model/workflow.go` | `GeoWorkflow` + `GeoWorkflowExecution` | steps(JSON), conditions(JSON), schedule | 工作流定义 + 执行记录 |

> 2026-11 新增：`GeoAuditRun` / `GeoAuditItem`（体检历史与逐项问题）、`GeoSearchQuery`（GSC 性能数据，`(tenant_id,row_key)` 唯一）、`GeoCaptureLink`（公开承接码/点击/线索/每日配额）。完整 37 个模型见 `internal/geo/model/`。

### 2.2 Repository 层（35 个接口）

| 文件 | 接口 | 核心方法 |
|------|------|---------|
| `repository/config.go` | `GeoConfigRepository` | Get() / Update() |
| `repository/keyword.go` | `GeoKeywordRepository` | Create / BatchCreate / GetList / Delete / GetStatistics |
| `repository/content.go` | `GeoArticleRepository` | Create / GetByID / GetList / Update / Delete |
| `repository/knowledge.go` | `GeoKnowledgeDocumentRepository` | Create / Update / GetByID / GetList / Delete |
| `repository/query_chain.go` | `GeoQueryChainRepository` | Append / ListByChain / ListByOneID / CountToday |
| `repository/verification.go` | `GeoVerifyResultRepository` | Create / GetByArticleID / GetByBrandName / ListAllForSOV / GetStatistics |
| `repository/crawler_visit.go` | `GeoCrawlerVisitRepository` | Create / StatsByEngine |
| `repository/workflow.go` | `GeoWorkflowRepository` + `GeoWorkflowExecutionRepository` + `GeoWorkflowTemplateRepository` | CRUD + Run + ListExecutions |
| `repository/geo_test.go` | — | test helper `setupGeoTestDB` |

### 2.3 Service 层（39 个 Service 类型）

| 文件 | Service | 核心功能 | 依赖 |
|------|---------|---------|------|
| `service/config.go` | `ConfigService` | 获取/更新 GEO 全局配置 + LLM 智能优化 | ConfigRepo + LLMAdapter |
| `service/keyword.go` | `KeywordService` | LLM 关键词挖掘 / 语义扩展 / 主题聚类 | KeywordRepo + APICallRepo + LLMAdapter |
| `service/content.go` | `ContentService` | 文章生成 / 优化 / EEAT 增强 / Schema 生成 / 原创度检测 | ArticleRepo + OptimizationRepo + APICallRepo + LLMAdapter |
| `service/verification.go` | `VerificationService` | AI 搜索验证 / 负面监控（v3 自动回写思维链 + 补位任务） | VerifyRepo + APICallRepo + ChainRepo + TaskRepo + LLMAdapter |
| `service/report.go` | `ReportService` | 汇总报表 / ROI 分析 / API 成本统计 | ArticleRepo + KeywordRepo + OptimizationRepo + VerifyRepo + APICallRepo |
| `service/platform.go` | `PlatformService` | 平台账号管理 / AES-256-GCM 加密凭据 / 一键发布 | AccountRepo + PublishRecordRepo + ArticleRepo |
| `service/kb.go` | `KBService` | 知识库 CRUD + 搜索 + 品牌问答（Ask） | KBDocRepo + LLMAdapter |
| `service/workflow.go` | `WorkflowService` | 工作流定义 / 模板 / 执行引擎（StepExecutor 接口 + ProgressCallback） | WfRepo + ExecRepo + TplRepo + LLMAdapter |
| `service/resource.go` | `ResourceService` | 静态 GEO 资源库（AI Agent / 工具 / 论文 / 社区） | 无 DB 依赖，内存常量 |
| `service/techconfig.go` | `TechConfigService` | robots.txt / sitemap.xml / llms.txt 生成 + `RunGEOAudit`（25 因子 GEO 审计） | 无 DB 依赖 |
| `service/metrics.go` | `MetricsService` | 内容质量指标：权威分 / 信源密度 / 品牌提及率 / 结构元素 | 无 DB 依赖，纯文本分析 |
| `service/keyword_enhance.go` | `KeywordEnhanceService` | 从历史验证数据反哺高价值关键词 | KeywordRepo + VerifyRepo + LLMAdapter |
| `service/search_probe.go` | `SearchProbe`（接口） | AI 搜索探针——可插拔的 `noopProbe` / `httpProbe`（`GEO_SEARCH_PROBE_URL` 环境变量） | HTTP client + LLMAdapter |
| `service/llm.go` | `LLMAdapter` | GEO 统一 LLM 访问层，封装 hivemtk 全局 Dispatcher + 定价估算（`EstimateCostUSD`） | `internal/aiagent/llm.Dispatcher` |
| `service/prompts.go` | — | 关键词挖掘 / 验证 / EEAT 增强 Prompt 模板（迁移自 AIGEOTOOLS） | 无依赖 |
| `service/intent_matrix.go` | — | 6 大搜索意图分类矩阵（疑问/对比/推荐/教程/评测/信息），`EnhancePromptWithIntent` 为内容生成加意图适配 | 无依赖 |
| `service/decision_executors.go` | 决策链执行器 | query_probe / source_attribution / content_gap_fill / capture_lead 四类步骤 | SearchProbe + ChainRepo + TaskRepo + LeadCapturePort（portcontract） |
| `service/decision_analytics.go` | `GeoDecisionAnalyticsService` | v3 竞品对齐：A1 SOV 品牌声量 / A6 爬虫统计 / A7 不准确声明检测 | VerifyRepo + TaskRepo + ChainRepo + CrawlerRepo + LLMAdapter |

### 2.4 Controller 层（35 个构造器）

| 文件 | Controller | 路由前缀 | 核心端点 |
|------|-----------|---------|---------|
| `controller/config.go` | `ConfigController` | `/geo/config` | GET /geo/config, PUT /geo/config(Admin), POST /geo/config/optimize(Admin) |
| `controller/keyword.go` | `KeywordController` | `/geo/keywords` | mine / expand / cluster / list / delete |
| `controller/content.go` | `ContentController` | `/geo/content` | generate / optimize / score / eeat / schema / uniqueness / list / :id |
| `controller/verification.go` | `VerificationController` | `/geo/verification` | verify / negative / results |
| `controller/report.go` | `ReportController` | `/geo/reports` | summary / roi / api-costs |
| `controller/platform.go` | `PlatformController` | `/geo/platform` | platforms / accounts / publish / records |
| `controller/kb.go` | `KBController` | `/geo/kb` | documents CRUD / search / ask |
| `controller/workflow.go` | `WorkflowController` | `/geo/workflow` | workflows CRUD + run + executions + templates |
| `controller/resource.go` | `ResourceController` | `/geo/resources` + `/geo/techconfig` + `/geo/metrics` | agents/tools/papers/communities + robots/sitemap/llms-txt + metrics/analyze |
| `controller/keyword_enhance.go` | `KeywordEnhanceController` | `/geo/keyword-enhance` | analyze / enhance |
| `controller/decision_controller.go` | `GeoDecisionController` | `/geo/decision`(直接挂 auth) | report / tasks GET / tasks/:id/done POST |

### 2.5 DTO 层（11 个文件）

| 文件 | 用途 |
|------|------|
| `dto/config.go` | 配置更新请求（Brand / Advantages / Competitors / Domain / VerifyModels） |
| `dto/keyword.go` | 关键词挖掘 / 扩展请求与响应 |
| `dto/keyword_enhance.go` | 关键词增强响应（HistoricalStats / EnhancedKeywords） |
| `dto/content.go` | 内容生成 / 优化 / EEAT / Schema / 原创度检测请求 |
| `dto/verification.go` | AI 搜索验证 / 负面监控请求 |
| `dto/report.go` | 汇总报表 / ROI / API 成本响应 |
| `dto/platform.go` | 平台账号（凭据脱敏）/ 发布请求 |
| `dto/knowledge.go` | 知识库文档 CRUD / 搜索 / 问答请求 |
| `dto/workflow.go` | 工作流定义 / 执行请求 |

---

## 3. 决策表点名 7 个文件详录

| # | 文件 | 核心功能 | 依赖 | 路由挂载点 |
|---|------|---------|------|-----------|
| 1 | `service/geo_audit.go` | 25 因子 GEO 审计，Otterly 对齐。**注意：不是独立文件，是 `TechConfigService` 的方法** `RunGEOAudit(url, title, content, metaDesc, schemaJSONLD) -> GeoAuditReport` | 无 DB 依赖 | 无直接路由，作为 `TechConfigService` 方法供间接调用 |
| 2 | `service/keyword_enhance.go` | 从历史 `GeoVerifyResult` 反哺高价值关键词，AnalyzeHistoricalPerformance -> Enhance 两阶段 | KeywordRepo + VerifyRepo + LLMAdapter | `/geo/keyword-enhance/analyze`、`/geo/keyword-enhance/enhance` |
| 3 | `service/search_probe.go` | AI 搜索探针接口。默认 `noopProbe` 显式报错；配置 `GEO_SEARCH_PROBE_URL` 后启用 `httpProbe` 返回真实引擎回答 + 被引 URL | HTTP client | 无直接路由，被 decision_executors.go 的 `query_probe` 步骤消费 |
| 4 | `service/metrics.go` | 纯文本内容质量分析，12 项指标：AuthorityScore / TrustDensity / BrandMentions / StructureElements | 无 DB 依赖 | `/geo/metrics/analyze` |
| 5 | `service/techconfig.go` | robots.txt / sitemap.xml / llms.txt 静态生成 + 25 因子审计方法 | 无 DB 依赖 | `/geo/techconfig/robots`、`/geo/techconfig/sitemap`、`/geo/techconfig/llms-txt` |
| 6 | `service/kb.go` | 品牌知识库，CRUD + 搜索 + LLM Ask 问答（待演进 pgvector 语义检索） | KBDocRepo + LLMAdapter | `/geo/kb/documents`、`/geo/kb/search`、`/geo/kb/ask` |
| 7 | `service/resource.go` | 静态 GEO 资源库（Agent / Tool / Paper / Community），全部内存常量 | 无依赖 | `/geo/resources/agents`、`/geo/resources/tools`、`/geo/resources/papers`、`/geo/resources/communities` |

---

## 4. 路由总览

共 **147 条**注册（`geo_routes.go` 内联统计）：144 条鉴权路由（含 3 条直接挂 `auth` 的决策端点）+ 3 条公开承接路由。
鉴权路由挂在 `auth.Group("/geo")` 下（`/api` 前缀由外层 router 提供）；公开路由为
`GET /api/public/geo/l/:code`、`POST .../ask`、`POST .../lead`，由 `PublicCodeTenant` 按 code 绑定租户。
下面保留核心路由示例，完整清单以 `geo_routes.go` 为准：

```
GET    /api/geo/config                               ConfigController.GetConfig
PUT    /api/geo/config                               ConfigController.UpdateConfig         [Admin]
POST   /api/geo/config/optimize                     ConfigController.OptimizeConfig       [Admin]

POST   /api/geo/keywords/mine                       KeywordController.MineKeywords
POST   /api/geo/keywords/expand                     KeywordController.SemanticExpand
POST   /api/geo/keywords/cluster                    KeywordController.TopicCluster
GET    /api/geo/keywords/list                       KeywordController.GetKeywordList
DELETE /api/geo/keywords/:id                        KeywordController.DeleteKeyword

POST   /api/geo/content/generate                    ContentController.GenerateContent
POST   /api/geo/content/optimize                    ContentController.OptimizeContent
POST   /api/geo/content/score                       ContentController.ScoreContent
POST   /api/geo/content/eeat                        ContentController.EnhanceEEAT
POST   /api/geo/content/schema                      ContentController.GenerateSchema
POST   /api/geo/content/uniqueness                  ContentController.CheckUniqueness
GET    /api/geo/content/list                        ContentController.GetArticleList
GET    /api/geo/content/:id                         ContentController.GetArticleByID

POST   /api/geo/verification/verify                 VerificationController.VerifyArticle
POST   /api/geo/verification/negative               VerificationController.MonitorNegative
GET    /api/geo/verification/results/:article_id    VerificationController.GetVerifyResults

GET    /api/geo/reports/summary                     ReportController.GetReport
GET    /api/geo/reports/roi                         ReportController.GetROI
GET    /api/geo/reports/api-costs                   ReportController.GetAPICosts

GET    /api/geo/platform/platforms                  PlatformController.ListPlatforms
GET    /api/geo/platform/accounts                   PlatformController.ListAccounts
POST   /api/geo/platform/accounts                   PlatformController.SaveAccount
DELETE /api/geo/platform/accounts/:id                PlatformController.DeleteAccount
POST   /api/geo/platform/publish                    PlatformController.Publish
GET    /api/geo/platform/records                    PlatformController.ListPublishRecords

GET    /api/geo/kb/documents                         KBController.List
POST   /api/geo/kb/documents                         KBController.Save
GET    /api/geo/kb/documents/:id                    KBController.Get
DELETE /api/geo/kb/documents/:id                    KBController.Delete
GET    /api/geo/kb/search                            KBController.Search
POST   /api/geo/kb/ask                              KBController.Ask

GET    /api/geo/workflow/workflows                  WorkflowController.List
POST   /api/geo/workflow/workflows                  WorkflowController.Create
GET    /api/geo/workflow/workflows/:id               WorkflowController.Get
PUT    /api/geo/workflow/workflows/:id               WorkflowController.Update
DELETE /api/geo/workflow/workflows/:id               WorkflowController.Delete
POST   /api/geo/workflow/workflows/:id/run           WorkflowController.Run
GET    /api/geo/workflow/workflows/:id/executions    WorkflowController.ListExecutions
GET    /api/geo/workflow/templates                   WorkflowController.ListTemplates
POST   /api/geo/workflow/templates                   WorkflowController.CreateTemplate

GET    /api/geo/resources/agents                    ResourceController.GetAgents
GET    /api/geo/resources/tools                      ResourceController.GetTools
GET    /api/geo/resources/papers                     ResourceController.GetPapers
GET    /api/geo/resources/communities                ResourceController.GetCommunities
GET    /api/geo/resources/summary                    ResourceController.GetResourceSummary
GET    /api/geo/resources/search                     ResourceController.SearchResources
POST   /api/geo/techconfig/robots                    ResourceController.GenerateRobots
POST   /api/geo/techconfig/sitemap                   ResourceController.GenerateSitemap
POST   /api/geo/techconfig/llms-txt                  ResourceController.GenerateLLMsTxt
POST   /api/geo/metrics/analyze                      ResourceController.AnalyzeMetrics

GET    /api/geo/keyword-enhance/analyze              KeywordEnhanceController.Analyze
POST   /api/geo/keyword-enhance/enhance              KeywordEnhanceController.Enhance

GET    /api/geo/decision/report                      GeoDecisionController.GetDecisionReport          (直接挂 auth)
GET    /api/geo/decision/tasks                       GeoDecisionController.GetTasks                    (直接挂 auth)
POST   /api/geo/decision/tasks/:id/done              GeoDecisionController.MarkTaskDone                (直接挂 auth)
GET    /api/geo/sov                                  -> GeoDecisionAnalyticsService.GetShareOfVoice     (内联 handler)
GET    /api/geo/crawler-stats                        -> GeoDecisionAnalyticsService.GetCrawlerStats      (内联 handler)
POST   /api/geo/inaccurate-claims                    -> GeoDecisionAnalyticsService.DetectInaccurateClaims (内联 handler)
```

---

## 5. v3 决策链化关键设计

| 设计点 | 说明 |
|--------|------|
| portcontract | `LeadCapturePort` 接口在 `decision_executors.go` 定义，实际注入 `CaptureLeadFunc` -> `mainClueRepo.Create`，geo 不反向依赖主域 |
| OneID 绑定 | `capture_lead` 执行器写入 clue 后回填 `geo_query_chains.one_id`，inbox 据此定位链 |
| 补位任务消费 | `GeoContentTaskRepository.ListPending` -> `GeoDecisionController.GetTasks` 供内容生产管线轮询 |
| 自动回写 | `VerificationService` 在 LLM 验证成功后自动 Append 思维链 + 写入 `GeoContentTask`（静默失败不阻塞验证主流程） |
| SOV | 基于 `GeoVerifyResult.BrandMentioned` 聚合各 intent 下品牌被提及占比 |

---

## 6. 设计约束（doc.go 声明）

1. **多租户**：所有 GEO 表带 `tenant_id`；写入由隔离 callback 填充，读取自动过滤；公开入口（短链/落地页/活码）按业务码反查唯一租户，反查失败 fail-closed
2. **凭据安全**：`internal/pkg/crypto` AES-256-GCM 加密；`FIELD_ENCRYPTION_KEY >= 32 字节`；接口层一律脱敏
3. **LLM 成本**：`EstimateCostUSD` 返回 USD/CNY 双值，**估算而非账单**
4. **数据来源诚实**：探针 `mode=simulated/real/manual` 全链路透传；GSC 数据标 `gsc_csv`/`gsc_csv_undated`；平台能力只有 GitHub/WordPress/Webhook 标 `real_api`，Medium 标 `manual`；首响无会话时显示“未接入”
5. **审计口径版本化**：站点 rubric v2/v3、内容 rubric content-v1/v2 落库；跨版本 compare 明确“不可比”
6. **已知演进方向**：pgvector 语义检索（知识库）、workflow JSONB 化、geo_api_calls 时间索引

---

## 7. 统计速览

| 维度 | 数量 |
|------|------|
| Model | 37 |
| Repository 接口 | 35 |
| Service 类型 | 39 |
| Controller 构造器 | 35 |
| DTO 文件 | 11 |
| 路由注册 | 147（鉴权 144 + 公开 3） |
| 前端页面 | 30（`user-web/src/views/geo/`） |
