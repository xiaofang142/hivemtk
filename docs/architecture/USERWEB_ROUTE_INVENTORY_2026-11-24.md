# user-web 全页面路由清单与模块归属（2026-11-24 重整）

> 来源：`user-web/src/router/index.js` + `src/router/modules/*.js`（93 模块），按 vue-router 绝对路径展开。
> 后端下发权威注册表：`user-server/internal/model/menu.go` 的 `SystemMenuTree`（GET /api/menus）。
> `✅`=已在后端菜单树登记；`⚠️`=有页面/路由但无菜单入口（详情/编辑等深链除外，按「头脑风暴决策」处理）。

## 其他/公开（53）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/abExperiment/list` | A/B 实验 | `views/abExperiment/List.vue` | ✅ |
| `/aiInsightDraft/list` | AI洞察草稿 | `views/aiInsightDraft/List.vue` | ✅ |
| `/aiProductivity/list` | AI产能分析 | `views/aiProductivity/List.vue` | ✅ |
| `/approvalTask/list` | 待办中心 | `views/approvalTask/List.vue` | ✅ |
| `/backup/list` | 备份恢复 | `views/backup/List.vue` | ✅ |
| `/badCase/list` | Bad Case 队列 | `views/badCase/List.vue` | ✅ |
| `/batchOperation/list` | 批量操作 | `views/batchOperation/List.vue` | ✅ |
| `/bridge/extension` | 扩展安装包 | `views/bridge/ExtensionPackage.vue` | ✅ |
| `/bridge/token` | 桥接凭证 | `views/bridge/TokenManagement.vue` | ⚠️ 无菜单 |
| `/businessAccountAuth/list` | 业务账号授权 | `views/businessAccountAuth/List.vue` | ✅ |
| `/churnPrediction/list` | 流失预警 | `views/churnPrediction/List.vue` | ✅ |
| `/community/list` | 社群管理 | `views/community/List.vue` | ✅ |
| `/conversionFunnel/list` | 转化漏斗 | `views/conversionFunnel/List.vue` | ✅ |
| `/crossAccountSearch/list` | 跨账号客户检索 | `views/crossAccountSearch/List.vue` | ✅ |
| `/customReport/list` | 自定义报表 | `views/customReport/List.vue` | ✅ |
| `/dashboard/drafts` | 订单草稿 | `views/orderDraft/List.vue` | ✅ |
| `/dashboard/drafts/:id` | 草稿详情 | `views/orderDraft/Detail.vue` | ⚠️ 无菜单 |
| `/dialogueMemory/list` | 对话记忆 | `views/dialogueMemory/List.vue` | ✅ |
| `/dingtalk-app` | 钉钉应用 | `views/dingtalkApp/DingtalkAppAccount.vue` | ✅ |
| `/domainPool` | 域名池管理 | `views/domainPool/List.vue` | ✅ |
| `/followups/:id` | 跟进详情 | `views/followups/Today.vue` | ⚠️ 无菜单 |
| `/followups/today` | 今日跟进 | `views/followups/Today.vue` | ✅ |
| `/glossary` | 术语表管理 | `views/glossary/List.vue` | ✅ |
| `/i18n/dashboard` | 多语言监控 | `views/i18n/Dashboard.vue` | ✅ |
| `/inbox/list` | 统一收件箱 | `views/inbox/List.vue` | ✅ |
| `/integration/list` | 第三方对接 | `views/integration/List.vue` | ✅ |
| `/intentRecognition/list` | 意图识别 | `views/intentRecognition/List.vue` | ✅ |
| `/intentRecognition/tool-flow` | 意图工具流 | `views/intentRecognition/ToolFlow.vue` | ✅ |
| `/livecode` | 活码管理 | `views/livecode/LiveCodeManagement.vue` | ✅ |
| `/mcp/credentials` | MCP凭证 | `views/mcp/Credentials.vue` | ✅ |
| `/messageHub/dashboard` | 消息总览 | `views/messageHub/Dashboard.vue` | ✅ |
| `/messageHub/list` | 消息中台 | `views/messageHub/List.vue` | ✅ |
| `/objection/list` | 异议处理 | `views/objection/List.vue` | ✅ |
| `/observability-dashboard` | AI 观测与渠道健康 | `views/observabilityDashboard/Index.vue` | ✅ |
| `/operationLog/enhanced` | 操作日志(增强) | `views/operationLog/Enhanced.vue` | ⚠️ 无菜单 |
| `/operationLog/list` | 操作日志 | `views/operationLog/List.vue` | ✅ |
| `/ops-overview` | 运维总览 | `views/OpsOverview/Index.vue` | ✅ |
| `/persona/list` | 销冠画像 | `views/persona/List.vue` | ✅ |
| `/platformAccount/list` | 平台账号 | `views/platformAccount/List.vue` | ✅ |
| `/register` | 商户注册 | `views/register/Register.vue` | ⚠️ 无菜单 |
| `/sales-cockpit` | AI 销冠驾驶舱 | `views/SalesCockpit/Index.vue` | ✅ |
| `/sales-workbench` | 销售工作台 | `views/salesWorkbench/Index.vue` | ✅ |
| `/securityAudit/list` | 安全审计 | `views/securityAudit/List.vue` | ✅ |
| `/shortLink` | 短链管理 | `views/shortLink/List.vue` | ✅ |
| `/shortLink/stats` | 短链统计 | `views/shortLink/Stats.vue` | ✅ |
| `/subscription-wall` | 订阅续费 | `views/subscription/Wall.vue` | ⚠️ 无菜单 |
| `/tagSegmentation/list` | 标签分层 | `views/tagSegmentation/List.vue` | ✅ |
| `/unknown-questions` | 未知问题运营 | `views/unknownQuestion/List.vue` | ✅ |
| `/wecomAccount/data` | 企微数据看板 | `views/wecomAccount/Data.vue` | ⚠️ 无菜单 |
| `/wecomAccount/list` | 企微账号管理 | `views/wecomAccount/List.vue` | ✅ |
| `/workflow-orchestrator/editor/:workflow_id` | 工作流编辑 | `views/workflowOrchestrator/Editor.vue` | ⚠️ 无菜单 |
| `/workflow-orchestrator/execution/:id` | 执行详情 | `views/workflowOrchestrator/Execution.vue` | ⚠️ 无菜单 |
| `/workflow-orchestrator/list` | 工作流编排 | `views/workflowOrchestrator/List.vue` | ✅ |

## 客户中心（23）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/clue/list` | 线索列表 | `views/clue/List.vue` | ✅ |
| `/clue/statistics` | 线索统计 | `views/clue/Statistics.vue` | ✅ |
| `/customer360/consents` | 客户同意与偏好 | `views/customer360/Consents.vue` | ✅ |
| `/customer360/list` | 客户 360 | `views/customer360/List.vue` | ✅ |
| `/customerEvent/list` | 客户事件 | `views/customerEvent/List.vue` | ✅ |
| `/customerImportJob/list` | 客户导入任务 | `views/customerImportJob/List.vue` | ✅ |
| `/customerJourney/dashboard` | 客户旅程大屏 | `views/customerJourney/Dashboard.vue` | ✅ |
| `/customerService/agentStatus` | 坐席状态 | `views/customerService/AgentStatus.vue` | ✅ |
| `/customerService/aiSuggestion` | AI 建议 | `views/customerService/AISuggestion.vue` | ✅ |
| `/customerService/csat` | CSAT 看板 | `views/customerService/CsatDashboard.vue` | ✅ |
| `/customerService/quickReply` | 快捷回复 | `views/customerService/QuickReply.vue` | ✅ |
| `/customerService/sessionTag` | 会话标签 | `views/customerService/SessionTag.vue` | ✅ |
| `/customerSession/list` | 客服会话 | `views/customerSession/List.vue` | ✅ |
| `/lead-mining/index` | 线索发掘 | `views/leadMining/Index.vue` | ✅ |
| `/leads/archive` | 线索归档 | `views/leads/Archive.vue` | ⚠️ 无菜单 |
| `/leads/evidence` | 判定证据回放 | `views/leads/Evidence.vue` | ⚠️ 无菜单 |
| `/leads/mining-agents` | 线索发现智能体 | `views/leads/MiningAgents.vue` | ⚠️ 无菜单 |
| `/leads/notify-routes` | 线索通知路由 | `views/leads/NotifyRoutes.vue` | ⚠️ 无菜单 |
| `/leads/rules` | 线索三粒度规则 | `views/leads/Rules.vue` | ⚠️ 无菜单 |
| `/leads/scoped-list` | 权限内客户与消息 | `views/leads/ScopedList.vue` | ⚠️ 无菜单 |
| `/userSegment/builder` | 分层规则构建 | `views/userSegment/Builder.vue` | ⚠️ 无菜单 |
| `/userSegment/list` | 用户分层 RFM | `views/userSegment/List.vue` | ✅ |
| `/userSegment/rfm-matrix` | RFM 矩阵 | `views/userSegment/RfmMatrix.vue` | ⚠️ 无菜单 |

## 客户中心/客户身份（3）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/oneid/conflicts` | 身份冲突解决 | `views/oneid/Conflicts.vue` | ✅ |
| `/oneid/list` | OneID 列表 | `views/oneid/List.vue` | ✅ |
| `/oneid/merge-rules` | 合并规则配置 | `views/oneid/MergeRuleConfig.vue` | ✅ |

## 客户中心（待接线）（8）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/crm/companies` | 公司主数据 | `views/crm/Companies.vue` | ⚠️ 无菜单 |
| `/crm/contacts` | 客户联系人 | `views/crm/Entities.vue` | ⚠️ 无菜单 |
| `/crm/dedupe` | 客户去重合并 | `views/crm/Dedupe.vue` | ⚠️ 无菜单 |
| `/crm/import-export` | 客户导入导出 | `views/crm/ImportExport.vue` | ⚠️ 无菜单 |
| `/crm/list` | 客户主数据 | `views/crm/List.vue` | ⚠️ 无菜单 |
| `/crm/profile360/:id?` | 客户 360 | `views/crm/Profile360.vue` | ⚠️ 无菜单 |
| `/crm/service-providers` | 客户服务商 | `views/crm/Entities.vue` | ⚠️ 无菜单 |
| `/crm/suppliers` | 客户供应商 | `views/crm/Entities.vue` | ⚠️ 无菜单 |

## 数据分析（7）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/analytics/cohort-path` | 留存与路径 | `views/analytics/CohortPath.vue` | ✅ |
| `/confidence/panel` | 置信度运营 | `views/confidence/Panel.vue` | ✅ |
| `/dashboardScreen/builder` | 大屏构建 | `views/dashboardScreen/Builder.vue` | ⚠️ 无菜单 |
| `/dashboardScreen/list` | 数据大屏 | `views/dashboardScreen/List.vue` | ✅ |
| `/feedbackLoop/panel` | 反馈学习闭环 | `views/feedbackLoop/Panel.vue` | ✅ |
| `/humanize/panel` | 拟人度评估 | `views/humanize/Panel.vue` | ✅ |
| `/reports/llm` | LLM用量报表 | `views/reports/LlmUsageDashboard.vue` | ✅ |

## 数据分析/GEO（29）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/geo-tools/alerts` | 告警中心 | `views/geo/AlertCenter.vue` | ✅ |
| `/geo-tools/audit-center` | 审计中心 | `views/geo/AuditCenter.vue` | ✅ |
| `/geo-tools/autopilot` | 自动驾驶（一键） | `views/geo/Autopilot.vue` | ✅ |
| `/geo-tools/brand-config` | 品牌与域名 | `views/geo/BrandConfig.vue` | ✅ |
| `/geo-tools/capture-links` | 承接链接 | `views/geo/CaptureLinks.vue` | ✅ |
| `/geo-tools/competitors` | 竞品管理 | `views/geo/CompetitorManage.vue` | ✅ |
| `/geo-tools/config` | 配置优化 | `views/geo/ConfigOptimizer.vue` | ✅ |
| `/geo-tools/content-creation` | 内容创作 | `views/geo/ContentCreation.vue` | ✅ |
| `/geo-tools/content-optimize` | 文章优化 | `views/geo/ContentOptimize.vue` | ✅ |
| `/geo-tools/crawler-stats` | 爬虫统计 | `views/geo/CrawlerStats.vue` | ✅ |
| `/geo-tools/dual-engine` | 双端监测 | `views/geo/DualEngineBoard.vue` | ✅ |
| `/geo-tools/entity-graph` | 实体图谱 | `views/geo/EntityGraph.vue` | ✅ |
| `/geo-tools/funnel-dashboard` | 漏斗总览 | `views/geo/FunnelDashboard.vue` | ✅ |
| `/geo-tools/index-tracking` | 收录与引用 | `views/geo/IndexTracking.vue` | ✅ |
| `/geo-tools/keyword-mining` | 关键词蒸馏 | `views/geo/KeywordMining.vue` | ✅ |
| `/geo-tools/knowledge-base` | GEO 知识库 | `views/geo/KnowledgeBase.vue` | ✅ |
| `/geo-tools/platform-publish` | 多平台发布 | `views/geo/PlatformPublish.vue` | ✅ |
| `/geo-tools/push-center` | 蜘蛛推送 | `views/geo/PushCenter.vue` | ✅ |
| `/geo-tools/pusher-config` | 蜘蛛推送配置 | `views/geo/PusherConfig.vue` | ✅ |
| `/geo-tools/reports` | 成本报表 | `views/geo/Reports.vue` | ✅ |
| `/geo-tools/schema-templates` | Schema 模板 | `views/geo/SchemaTemplates.vue` | ✅ |
| `/geo-tools/seo-infra` | SEO 基础设施 | `views/geo/SeoInfra.vue` | ✅ |
| `/geo-tools/site-config` | 静态站部署 | `views/geo/SiteConfig.vue` | ✅ |
| `/geo-tools/site-publish` | 发布到官网 | `views/geo/SitePublish.vue` | ✅ |
| `/geo-tools/sov-board` | 竞品 SOV | `views/geo/SovBoard.vue` | ✅ |
| `/geo-tools/verification` | 多模型验证 | `views/geo/Verification.vue` | ✅ |
| `/geo-tools/visibility` | 可见性趋势 | `views/geo/VisibilityBoard.vue` | ✅ |
| `/geo-tools/workflow` | 工作流 | `views/geo/WorkflowEditor.vue` | ✅ |
| `/geo/decision-report` | 决策链报表 | `views/geo/DecisionReport.vue` | ✅ |

## 智能体（6）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/aiAgent` | 智能体 | `loadList` | ⚠️ 无菜单 |
| `/aiAgent/create` | 创建智能体 | `named` | ✅ |
| `/aiAgent/edit/:id` | 编辑智能体 | `named` | ⚠️ 无菜单 |
| `/aiAgent/integrations` | 工具接入 | `views/aiAgent/CustomIntegration.vue` | ✅ |
| `/aiAgent/list` | 智能体列表 | `loadList` | ✅ |
| `/aiAgent/tools` | AI 工具管理 | `views/aiAgent/ToolManagement/ToolList.vue` | ✅ |

## 智能体/主动触达（4）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/marketingFlow/list` | 营销自动化 | `views/marketingFlow/List.vue` | ✅ |
| `/reachPipeline/deferred-outbound` | 延迟出站管理 | `views/reachPipeline/DeferredOutbound.vue` | ✅ |
| `/reachPipeline/editor/:id?` | Pipeline 编辑 | `views/reachPipeline/Editor.vue` | ⚠️ 无菜单 |
| `/reachPipeline/list` | 触达Pipeline | `views/reachPipeline/List.vue` | ✅ |

## 智能体/客服渠道（4）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/chatChannel/create` | 新建客服渠道 | `named` | ⚠️ 无菜单 |
| `/chatChannel/edit/:id` | 编辑客服渠道 | `named` | ⚠️ 无菜单 |
| `/chatChannel/install-guide/:id?` | Widget 安装引导 | `views/chatChannel/InstallGuide.vue` | ⚠️ 无菜单 |
| `/chatChannel/list` | 客服渠道 | `loadList` | ✅ |

## 智能体/能力支撑（12）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/faq` | FAQ 知识库 | `views/faq/Index.vue` | ⚠️ 无菜单 |
| `/faq/editor` | FAQ 编辑 | `views/faq/Editor.vue` | ⚠️ 无菜单 |
| `/faq/editor/:id` | FAQ 编辑 | `views/faq/Editor.vue` | ⚠️ 无菜单 |
| `/faq/list` | FAQ 列表 | `views/faq/List.vue` | ✅ |
| `/scriptTemplate/ab-stats` | 话术AB测试 | `views/scriptTemplate/AbStats.vue` | ✅ |
| `/scriptTemplate/list` | 话术库 | `views/scriptTemplate/List.vue` | ✅ |
| `/sop-template` | SOP 模板库 | `views/sopTemplate/Index.vue` | ⚠️ 无菜单 |
| `/sop-template/editor` | SOP 模板编辑 | `views/sopTemplate/Editor.vue` | ⚠️ 无菜单 |
| `/sop-template/editor/:id` | SOP 模板编辑 | `views/sopTemplate/Editor.vue` | ⚠️ 无菜单 |
| `/sop-template/list` | SOP 模板列表 | `views/sopTemplate/List.vue` | ✅ |
| `/sop-template/market` | SOP 模板市场 | `views/sopTemplate/Market.vue` | ✅ |
| `/sopAgent/list` | 销冠 SOP 智能体 | `views/sopAgent/List.vue` | ✅ |

## 智能体/被动应答（2）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/llmRouting/cost` | 成本看板 | `views/llmRouting/CostDashboard.vue` | ⚠️ 无菜单 |
| `/llmRouting/list` | LLM路由 | `views/llmRouting/List.vue` | ✅ |

## 知识中心（17）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/kb/connectors` | 知识库连接器 | `views/kb/Connectors.vue` | ✅ |
| `/knowledge/batch-import` | 批量导入 | `named` | ✅ |
| `/knowledge/connectors` | 外部连接器 | `views/KnowledgeWorkspace/Connectors.vue` | ✅ |
| `/knowledge/crawler-sources` | 网站爬取 | `views/KnowledgeWorkspace/CrawlerSources.vue` | ⚠️ 无菜单 |
| `/knowledge/external` | 外部系统接入 | `views/KnowledgeWorkspace/ExternalImport.vue` | ✅ |
| `/knowledge/feedbacks` | 反馈管理 | `views/KnowledgeWorkspace/FeedbackList.vue` | ✅ |
| `/knowledge/management` | 知识库管理 | `loadList` | ✅ |
| `/knowledge/openapi` | OpenAPI 集成 | `views/KnowledgeWorkspace/OpenAPIIntegration.vue` | ✅ |
| `/knowledge/playground` | 检索 Playground | `views/KnowledgeWorkspace/Playground.vue` | ✅ |
| `/knowledge/qa-candidates` | QA 审核队列 | `views/KnowledgeWorkspace/QACandidates.vue` | ⚠️ 无菜单 |
| `/knowledge/rag-eval` | RAG 评测 | `views/knowledge/RagEvaluation.vue` | ✅ |
| `/knowledge/statistics` | 知识库统计 | `views/KnowledgeWorkspace/KnowledgeStatistics.vue` | ✅ |
| `/knowledge/tokens` | API Token | `views/KnowledgeWorkspace/ApiToken.vue` | ✅ |
| `/knowledgeBase` | 知识库管理 | `views/knowledgeBase/List.vue` | ✅ |
| `/knowledgeBase/create` | 新建知识库 | `views/knowledgeBase/List.vue` | ⚠️ 无菜单 |
| `/knowledgeBase/edit/:id` | 编辑知识库 | `views/knowledgeBase/List.vue` | ⚠️ 无菜单 |
| `/knowledgeBase/list` | 知识库列表 | `views/knowledgeBase/List.vue` | ⚠️ 无菜单 |

## 系统设置（17）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/system/automation-hub` | 自动化中心 | `views/system/AutomationHub.vue` | ✅ |
| `/system/config` | 站点设置 | `views/system/Config.vue` | ✅ |
| `/system/config-params` | 动态阈值参数 | `views/system/ConfigParams.vue` | ✅ |
| `/system/dictionaries` | 业务字典 | `views/system/DictionaryManager.vue` | ✅ |
| `/system/feature-flags` | 功能开关 | `views/system/FeatureFlags.vue` | ✅ |
| `/system/guide` | 使用引导 | `views/system/Guide.vue` | ✅ |
| `/system/ltc-config` | LTC 运营开关 | `views/system/LTCConfig.vue` | ✅ |
| `/system/material-library` | 素材库 | `views/system/MaterialLibrary.vue` | ✅ |
| `/system/monitor` | 监控 | `views/system/Monitor.vue` | ✅ |
| `/system/obs-config` | 存储配置 | `views/system/ObsConfig.vue` | ✅ |
| `/system/permissions` | permission.title | `views/system/PermissionPanel.vue` | ✅ |
| `/system/rag-account` | RAG 账号配置 | `views/RagProductConfig/AccountConfig.vue` | ✅ |
| `/system/rag-overview` | RAG概览 | `views/system/RagOverview.vue` | ✅ |
| `/system/rag-product` | RAG 产品管理 | `views/RagProductConfig/RagProductManagement.vue` | ✅ |
| `/system/roles` | role.title | `views/system/RoleList.vue` | ✅ |
| `/system/trace` | 链路追踪 | `views/system/TraceMonitor.vue` | ✅ |
| `/system/users` | systemUser.title | `views/system/UserList.vue` | ✅ |

## 系统设置/功能模块（4）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/module-market` | 功能模块市场 | `views/moduleMarket/Market.vue` | ✅ |
| `/module-market/detail/:key` | 模块详情 | `views/moduleMarket/Detail.vue` | ⚠️ 无菜单 |
| `/module-market/my` | 我的模块 | `views/moduleMarket/MyModules.vue` | ✅ |
| `/module-market/orders` | 我的订单 | `views/moduleMarket/Orders.vue` | ✅ |

## 系统设置/权限设置（待接线）（3）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/channels/accounts` | 渠道账号管理 | `views/channels/account-manage.vue` | ⚠️ 无菜单 |
| `/channels/identify-codes` | 渠道识别码管理 | `views/channels/identify-code-manage.vue` | ✅ |
| `/channels/manage` | 渠道管理 | `views/channels/channel-manage.vue` | ⚠️ 无菜单 |

## 系统设置/模板技能（3）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/template-market` | 模板/技能市场 | `views/templateMarket/Market.vue` | ✅ |
| `/template-market/my` | 我的发布/订单 | `views/templateMarket/MyWorkspace.vue` | ✅ |
| `/template-market/review` | 发布审核 | `views/templateMarket/ReviewConsole.vue` | ✅ |

## 系统设置/浏览器自动化（14）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/browser-automation/cron` | 定时触发器 | `views/browserAutomation/Cron.vue` | ✅ |
| `/browser-automation/ego/assets` | 资产中心 | `views/browserAutomation/ego/Assets.vue` | ✅ |
| `/browser-automation/ego/assets/admin` | 资产发布台 | `views/browserAutomation/ego/AssetAdmin.vue` | ✅ |
| `/browser-automation/ego/executions` | 执行监控 | `views/browserAutomation/ego/Executions.vue` | ✅ |
| `/browser-automation/ego/flows` | 我的流程 | `views/browserAutomation/ego/Flows.vue` | ✅ |
| `/browser-automation/ego/runner` | Runner 状态 | `views/browserAutomation/ego/Runner.vue` | ✅ |
| `/browser-automation/ego/sessions` | 通用会话 | `views/browserAutomation/ego/Sessions.vue` | ✅ |
| `/browser-automation/ego/telemetry` | 遥测看板 | `views/browserAutomation/ego/Telemetry.vue` | ✅ |
| `/browser-automation/sessions/:id` | 执行监控 | `views/browserAutomation/Monitor.vue` | ⚠️ 无菜单 |
| `/browser-automation/status` | Host 状态 | `views/browserAutomation/Status.vue` | ✅ |
| `/browser-automation/tasks` | 任务列表 | `loadList` | ✅ |
| `/browser-automation/tasks/:id` | 任务详情 | `views/browserAutomation/Detail.vue` | ⚠️ 无菜单 |
| `/browser-automation/tasks/:id/edit` | 编辑任务 | `named` | ⚠️ 无菜单 |
| `/browser-automation/tasks/create` | 新建任务 | `named` | ✅ |

## 系统设置/能力（2）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/cap/diagnostics` | 能力运行诊断 | `views/cap/Diagnostics.vue` | ✅ |
| `/cap/matrix` | 能力矩阵 | `views/cap/Matrix.vue` | ⚠️ 无菜单 |

## 系统设置/资产包（9）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/asset-bundle/list` | 资产包管理 | `loadList` | ✅ |
| `/asset-bundle/merchant-new` | 商户新建话术包 | `named` | ✅ |
| `/asset-bundle/merchant/:aid` | 商户配置话术包 | `named` | ⚠️ 无菜单 |
| `/asset-bundle/playground` | 开发者 Playground | `views/assetBundle/Playground.vue` | ✅ |
| `/asset-bundle/playground/:aid` | 开发者 Playground | `views/assetBundle/Playground.vue` | ⚠️ 无菜单 |
| `/asset-market` | 资产市场 | `views/assetMarket/Market.vue` | ✅ |
| `/asset-market/detail/:id` | 资产详情 | `views/assetMarket/Detail.vue` | ⚠️ 无菜单 |
| `/asset-market/my-assets` | 我的资产 | `views/assetMarket/MyAssets.vue` | ✅ |
| `/asset-market/sync-log` | 同步日志 | `views/assetMarket/SyncLog.vue` | ✅ |

## 触达运营（22）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/douyin-card-stats/:id` | 抖音卡片详情统计 | `views/douyinCard/CardStats.vue` | ⚠️ 无菜单 |
| `/douyin/stats` | 抖音卡片统计 | `views/douyinCard/Stats.vue` | ✅ |
| `/douyinCard` | 抖音卡片 | `views/douyinCard/List.vue` | ✅ |
| `/feishu` | 飞书 | `views/feishu/FeishuAccount.vue` | ⚠️ 无菜单 |
| `/feishu/account` | 飞书账号 | `views/feishu/FeishuAccount.vue` | ✅ |
| `/kuaishou-card-stats/:id` | 快手卡片详情统计 | `views/kuaishouCard/CardStats.vue` | ⚠️ 无菜单 |
| `/kuaishou/stats` | 快手卡片统计 | `views/kuaishouCard/Stats.vue` | ✅ |
| `/kuaishouCard` | 快手卡片 | `views/kuaishouCard/List.vue` | ✅ |
| `/qq` | QQ 机器人 | `views/qq/account.vue` | ⚠️ 无菜单 |
| `/qq/account` | 机器人账号 | `views/qq/account.vue` | ✅ |
| `/telegram` | TG 机器人 | `views/telegram/account.vue` | ⚠️ 无菜单 |
| `/telegram/account` | 机器人账号 | `views/telegram/account.vue` | ✅ |
| `/telegram/gate` | 入群管控 | `views/telegram/gate.vue` | ✅ |
| `/tiktok-card-stats/:id` | TikTok卡片统计 | `views/tiktokCard/CardStats.vue` | ⚠️ 无菜单 |
| `/tiktok/list` | TikTok卡片 | `views/tiktokCard/List.vue` | ✅ |
| `/tiktok/stats` | TikTok统计 | `views/tiktokCard/Stats.vue` | ✅ |
| `/xianyu-card-stats/:id` | 闲鱼卡片详情统计 | `views/xianyuCard/CardStats.vue` | ⚠️ 无菜单 |
| `/xianyu/stats` | 闲鱼卡片统计 | `views/xianyuCard/Stats.vue` | ✅ |
| `/xianyuCard` | 闲鱼卡片 | `views/xianyuCard/List.vue` | ✅ |
| `/xiaohongshu-card-stats/:id` | 小红书卡片详情统计 | `views/xiaohongshuCard/CardStats.vue` | ⚠️ 无菜单 |
| `/xiaohongshu/stats` | 小红书卡片统计 | `views/xiaohongshuCard/Stats.vue` | ✅ |
| `/xiaohongshuCard` | 小红书卡片 | `views/xiaohongshuCard/List.vue` | ✅ |

## 触达运营/WhatsApp（8）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/whatsapp` | WhatsApp 社群 | `views/whatsapp/WhatsappAccount.vue` | ⚠️ 无菜单 |
| `/whatsapp-cloud` | WhatsApp Cloud | `views/whatsappCloud/WhatsappCloudAccount.vue` | ✅ |
| `/whatsapp/account` | 账号管理 | `views/whatsapp/WhatsappAccount.vue` | ✅ |
| `/whatsapp/bulk-matrix` | 群发矩阵 | `views/whatsapp/BulkMatrix.vue` | ✅ |
| `/whatsapp/drafts` | 草稿箱 | `views/whatsapp/WhatsappDrafts.vue` | ✅ |
| `/whatsapp/group-messaging` | 批量消息发送 | `views/whatsappBot/BulkMessaging.vue` | ✅ |
| `/whatsapp/jobs` | 群发 | `views/whatsapp/WhatsappJobs.vue` | ✅ |
| `/whatsapp/lead-group-selection` | 从线索库选择群体 | `views/whatsappBot/LeadGroupSelection.vue` | ✅ |

## 触达运营/短信（4）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/sms/config` | 短信配置 | `views/sms/Config.vue` | ✅ |
| `/sms/drafts` | 短信草稿 | `views/sms/Drafts.vue` | ✅ |
| `/sms/jobs` | 短信任务 | `views/sms/Jobs.vue` | ✅ |
| `/sms/list` | 短信列表 | `views/sms/List.vue` | ✅ |

## 触达运营/邮件（8）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/email` | 邮件列表 | `views/email/EmailList.vue` | ✅ |
| `/email/deliverability` | 送达分析 | `views/email/Deliverability.vue` | ✅ |
| `/email/drafts` | 我的草稿 | `views/email/Drafts.vue` | ✅ |
| `/email/drag-editor` | 拖拽编辑器 | `views/email/DragEditor.vue` | ⚠️ 无菜单 |
| `/email/guide` | 使用引导 | `views/email/Guide.vue` | ✅ |
| `/email/info` | 邮件代理 | `views/email/Info.vue` | ✅ |
| `/email/jobs` | 我的任务 | `views/email/Jobs.vue` | ✅ |
| `/email/smtp` | 邮件账号 | `views/email/Smtp.vue` | ✅ |

## 触达运营（待接线）（1）

| 路由 | 标题 | 组件 | 菜单 |
|---|---|---|---|
| `/commerce/operations` | 电商操作工作台 | `views/commerce/OperationsPanel.vue` | ⚠️ 无菜单 |
