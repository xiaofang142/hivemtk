# user-web 路由清单（router-list.md）

> 生成时间：2026-10-27T16:26:39.083Z　来源：`user-web/src/router/index.js` + `user-web/src/router/modules/*.js`
> 解析方式：逐文件 AST 解析路由对象，父/子路由已拼接为完整路径；与 `user-web/ROUTE_AUDIT_LIST.md` 互为参照。

- 模块文件：86 个
- 静态路由：9 条
- 模块路由：236 条

## 静态路由

| # | 路由 | name | 标题 | 公开 | 备注 |
|---|---|---|---|---|---|
| 1 | `/` | Layout | 布局（重定向 → /messageHub/list） | 否 |  |
| 2 | `/setup` | InitSetup | 系统初始化 | 是 |  |
| 3 | `/login` | Login | 登录 | 是 |  |
| 4 | `/help-center` | HelpCenter | 帮助中心 | 是 |  |
| 5 | `/chat/embed/default` | ChatEmbed | 在线客服嵌入 | 是 |  |
| 6 | `/chat/embed/:channel_ref` | ChatEmbedChannel | 在线客服嵌入(渠道) | 是 |  |
| 7 | `/profile` | Profile | 个人资料 | 否 |  |
| 8 | `/notifications` | Notifications | 通知中心 | 否 |  |
| 9 | `/:pathMatch(.*)*` | NotFound | 404 页面不存在 | 是 | 404 兜底 |

## 模块路由

### 1. abExperiment（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 10 | `/abExperiment/list` | AbExperimentList | A/B 实验 | dataAnalysis | 需登录 | - |

### 2. aiAgent（6 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 11 | `/aiAgent` | AIAgent | 智能体 | aiAgent | 需登录 | - |
| 12 | `/aiAgent/list` | AIAgentList | 智能体列表 | aiAgent | 需登录 | - |
| 13 | `/aiAgent/create` | AIAgentCreate | 创建智能体 | aiAgent | 需登录 | - |
| 14 | `/aiAgent/edit/:id` | AIAgentEdit | 编辑智能体 | aiAgent | 需登录 | - |
| 15 | `/aiAgent/integrations` | AICustomIntegration | 工具接入 | aiAgent | 需登录 | - |
| 16 | `/aiAgent/tools` | AIToolManagement | AI 工具管理 | aiAgent | 需登录 | - |

### 3. aiProductivity（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 17 | `/aiProductivity/list` | AiProductivityList | AI产能分析 | analytics | 需登录 | - |

### 4. analytics（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 18 | `/analytics/cohort-path` | CohortPath | 留存与路径 | analytics | 需登录 | - |

### 5. approvalTask（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 19 | `/approvalTask/list` | ApprovalTaskList | 待办中心 | workspace | 需登录 | - |

### 6. assetBundle（5 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 20 | `/asset-bundle/list` | AssetBundleList | 资产包管理 | aiAgent | 需登录 | - |
| 21 | `/asset-bundle/playground` | AssetBundlePlayground | 开发者 Playground | aiAgent | 需登录 | - |
| 22 | `/asset-bundle/playground/:aid` | AssetBundlePlaygroundEdit | 开发者 Playground | aiAgent | 需登录 | - |
| 23 | `/asset-bundle/merchant-new` | AssetBundleMerchantNew | 商户新建话术包 | aiAgent | 需登录 | - |
| 24 | `/asset-bundle/merchant/:aid` | AssetBundleMerchantEdit | 商户配置话术包 | aiAgent | 需登录 | - |

### 7. assetMarket（4 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 25 | `/asset-market` | AssetMarket | 资产市场 | aiAgent | 需登录 | - |
| 26 | `/asset-market/detail/:id` | AssetMarketDetail | 资产详情 | aiAgent | 需登录 | - |
| 27 | `/asset-market/my-assets` | AssetMyAssets | 我的资产 | aiAgent | 需登录 | - |
| 28 | `/asset-market/sync-log` | AssetSyncLog | 同步日志 | aiAgent | 需登录 | - |

### 8. backup（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 29 | `/backup/list` | BackupList | 备份恢复 | system | admin | - |
| 30 | `/backup/enhanced` | BackupEnhanced | 备份管理 | system | admin | - |

### 9. badCase（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 31 | `/badCase/list` | BadCaseList | Bad Case 队列 | workspace | 需登录 | - |

### 10. batchOperation（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 32 | `/batchOperation/list` | BatchOperationList | 批量操作 | reach | 需登录 | - |

### 11. bridgeToken（4 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 33 | `/bridge` | BridgeRoot | 渠道接入 | - | admin | /bridge/token |
| 34 | `/bridge/token` | BridgeTokenManagement | 桥接凭证 | - | admin | - |
| 35 | `/bridge/extension` | BridgeExtensionPackage | 扩展安装包 | - | 需登录 | - |
| 36 | `/token` | - | - | - | 需登录 | /bridge/token |

### 12. browserAutomation（13 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 37 | `/browser-automation` | BrowserAutomation | 浏览器自动化 | automation | 需登录 | /browser-automation/tasks |
| 38 | `/browser-automation/tasks` | BrowserAutomationList | 任务列表 | - | 需登录 | - |
| 39 | `/browser-automation/tasks/create` | BrowserAutomationCreate | 新建任务 | - | 需登录 | - |
| 40 | `/browser-automation/tasks/:id/edit` | BrowserAutomationEditor | 编辑任务 | - | 需登录 | - |
| 41 | `/browser-automation/tasks/:id` | BrowserAutomationDetail | 任务详情 | - | 需登录 | - |
| 42 | `/browser-automation/sessions/:id` | BrowserAutomationMonitor | 执行监控 | - | 需登录 | - |
| 43 | `/browser-automation/cron` | BrowserAutomationCron | 定时触发器 | - | 需登录 | - |
| 44 | `/browser-automation/status` | BrowserAutomationStatus | Host 状态 | - | 需登录 | - |
| 45 | `/browser-automation/ego/flows` | EgoFlows | 我的流程 | - | 需登录 | - |
| 46 | `/browser-automation/ego/executions` | EgoExecutions | 执行监控 | - | 需登录 | - |
| 47 | `/browser-automation/ego/runner` | EgoRunner | Runner 状态 | - | 需登录 | - |
| 48 | `/browser-automation/ego/assets` | EgoAssets | 资产中心 | - | 需登录 | - |
| 49 | `/browser-automation/ego/assets/admin` | EgoAssetAdmin | 资产发布台 | - | 需登录 | - |

### 13. chatChannel（4 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 50 | `/chatChannel/list` | ChatChannelList | 客服渠道 | customer | 需登录 | - |
| 51 | `/chatChannel/create` | ChatChannelCreate | 新建客服渠道 | customer | 需登录 | - |
| 52 | `/chatChannel/edit/:id` | ChatChannelEdit | 编辑客服渠道 | customer | 需登录 | - |
| 53 | `/chatChannel/install-guide/:id?` | ChatChannelInstallGuide | Widget 安装引导 | customer | 需登录 | - |

### 14. churnPrediction（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 54 | `/churnPrediction/list` | ChurnPredictionList | 流失预警 | dataAnalysis | 需登录 | - |

### 15. clue（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 55 | `/clue/list` | ClueList | 线索列表 | customer | 需登录 | - |
| 56 | `/clue/statistics` | ClueStatistics | 线索统计 | customer | 需登录 | - |

### 16. community（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 57 | `/community/list` | CommunityList | 社群管理 | community | 需登录 | - |

### 17. conversionFunnel（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 58 | `/conversionFunnel/list` | ConversionFunnelList | 转化漏斗 | analytics | 需登录 | - |

### 18. customReport（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 59 | `/customReport/list` | CustomReportList | 自定义报表 | dataAnalysis | 需登录 | - |

### 19. customer360（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 60 | `/customer360/list` | Customer360List | 客户 360 | customer | 需登录 | - |

### 20. customerEvent（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 61 | `/customerEvent/list` | CustomerEventList | 客户事件 | customer | 需登录 | - |

### 21. customerJourney（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 62 | `/customerJourney/dashboard` | CustomerJourneyDashboard | 客户旅程大屏 | analytics | 需登录 | - |

### 22. customerService（5 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 63 | `/customerService/agentStatus` | AgentStatus | 坐席状态 | community | 需登录 | - |
| 64 | `/customerService/quickReply` | QuickReply | 快捷回复 | community | 需登录 | - |
| 65 | `/customerService/sessionTag` | SessionTag | 会话标签 | community | 需登录 | - |
| 66 | `/customerService/aiSuggestion` | AISuggestion | AI 建议 | community | 需登录 | - |
| 67 | `/customerService/csat` | CsatDashboard | CSAT 看板 | community | 需登录 | - |

### 23. customerSession（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 68 | `/customerSession/list` | CustomerSessionList | 客服会话 | customer | 需登录 | - |

### 24. dashboardScreen（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 69 | `/dashboardScreen/list` | DashboardScreenList | 数据大屏 | dataAnalysis | 需登录 | - |
| 70 | `/dashboardScreen/builder` | DashboardScreenBuilder | 大屏构建 | dataAnalysis | 需登录 | - |

### 25. dialogueMemory（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 71 | `/dialogueMemory/list` | DialogueMemoryList | 对话记忆 | aiAgent | 需登录 | - |

### 26. dingtalkApp（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 72 | `/dingtalk-app` | DingtalkApp | 钉钉应用 | community | 需登录 | - |

### 27. domainPool（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 73 | `/domainPool` | DomainPool | 域名池管理 | system | admin | - |

### 28. douyinCard（3 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 74 | `/douyinCard` | DouyinCard | 抖音卡片 | community | 需登录 | - |
| 75 | `/douyin/stats` | DouyinStats | 抖音卡片统计 | community | 需登录 | - |
| 76 | `/douyin-card-stats/:id` | DouyinCardStats | 抖音卡片详情统计 | community | 需登录 | - |

### 29. email（8 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 77 | `/email` | Email | 邮件列表 | reach | 需登录 | - |
| 78 | `/email/drafts` | EmailDrafts | 我的草稿 | reach | 需登录 | - |
| 79 | `/email/jobs` | EmailJobs | 我的任务 | reach | 需登录 | - |
| 80 | `/email/smtp` | EmailSmtp | 邮件账号 | reach | 需登录 | - |
| 81 | `/email/info` | EmailInfo | 邮件代理 | reach | 需登录 | - |
| 82 | `/email/guide` | EmailGuide | 使用引导 | reach | 需登录 | - |
| 83 | `/email/deliverability` | EmailDeliverability | 送达分析 | email | 需登录 | - |
| 84 | `/email/drag-editor` | EmailDragEditor | 拖拽编辑器 | reach | 需登录 | - |

### 30. faq（4 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 85 | `/faq` | FAQ | FAQ 知识库 | aiAgent | 需登录 | - |
| 86 | `/faq/list` | FAQList | FAQ 列表 | aiAgent | 需登录 | - |
| 87 | `/faq/editor` | FAQEditor | FAQ 编辑 | aiAgent | 需登录 | - |
| 88 | `/faq/editor/:id` | FAQEditorEdit | FAQ 编辑 | aiAgent | 需登录 | - |

### 31. feishu（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 89 | `/feishu` | Feishu | 飞书 | community | 需登录 | - |
| 90 | `/feishu/account` | FeishuAccount | 飞书账号 | community | 需登录 | - |

### 32. followups（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 91 | `/followups/today` | FollowUpToday | 今日跟进 | workspace | 需登录 | - |
| 92 | `/followups/:id` | FollowUpDetail | 跟进详情 | workspace | 需登录 | - |

### 33. geoTools（25 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 93 | `/geo-tools/brand-config` | GeoBrandConfig | 品牌与域名 | config | 需登录 | - |
| 94 | `/geo-tools/seo-infra` | GeoSeoInfra | SEO 基础设施 | config | 需登录 | - |
| 95 | `/geo-tools/pusher-config` | GeoPusherConfig | 蜘蛛推送配置 | config | 需登录 | - |
| 96 | `/geo-tools/site-config` | GeoSiteConfig | 静态站部署 | config | 需登录 | - |
| 97 | `/geo-tools/schema-templates` | GeoSchemaTemplates | Schema 模板 | config | 需登录 | - |
| 98 | `/geo-tools/competitors` | GeoCompetitors | 竞品管理 | config | 需登录 | - |
| 99 | `/geo-tools/config` | GeoConfigOptimizer | 配置优化 | config | 需登录 | - |
| 100 | `/geo-tools/keyword-mining` | GeoKeywordMining | 关键词蒸馏 | execute | 需登录 | - |
| 101 | `/geo-tools/content-creation` | GeoContentCreation | 内容创作 | execute | 需登录 | - |
| 102 | `/geo-tools/content-optimize` | GeoContentOptimize | 文章优化 | execute | 需登录 | - |
| 103 | `/geo-tools/site-publish` | GeoSitePublish | 发布到官网 | execute | 需登录 | - |
| 104 | `/geo-tools/push-center` | GeoPushCenter | 蜘蛛推送 | execute | 需登录 | - |
| 105 | `/geo-tools/platform-publish` | GeoPlatformPublish | 多平台发布 | execute | 需登录 | - |
| 106 | `/geo-tools/workflow` | GeoWorkflow | 工作流 | execute | 需登录 | - |
| 107 | `/geo-tools/knowledge-base` | GeoKnowledgeBase | GEO 知识库 | execute | 需登录 | - |
| 108 | `/geo-tools/entity-graph` | GeoEntityGraph | 实体图谱 | execute | 需登录 | - |
| 109 | `/geo-tools/funnel-dashboard` | GeoFunnelDashboard | 漏斗总览 | observe | 需登录 | - |
| 110 | `/geo-tools/index-tracking` | GeoIndexTracking | 收录与引用 | observe | 需登录 | - |
| 111 | `/geo-tools/visibility` | GeoVisibilityBoard | 可见性趋势 | observe | 需登录 | - |
| 112 | `/geo-tools/sov-board` | GeoSovBoard | 竞品 SOV | observe | 需登录 | - |
| 113 | `/geo-tools/crawler-stats` | GeoCrawlerStats | 爬虫统计 | observe | 需登录 | - |
| 114 | `/geo-tools/verification` | GeoVerification | 多模型验证 | observe | 需登录 | - |
| 115 | `/geo/decision-report` | GeoDecisionReport | 决策链报表 | observe | 需登录 | - |
| 116 | `/geo-tools/reports` | GeoReports | 成本报表 | observe | 需登录 | - |
| 117 | `/geo-tools/alerts` | GeoAlertCenter | 告警中心 | observe | 需登录 | - |

### 34. glossary（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 118 | `/glossary` | GlossaryList | 术语表管理 | i18n | 需登录 | - |

### 35. i18nStats（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 119 | `/i18n/dashboard` | I18nDashboard | 多语言监控 | i18n | 需登录 | - |

### 36. inbox（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 120 | `/inbox/list` | InboxList | 统一收件箱 | workspace | 需登录 | - |

### 37. integration（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 121 | `/integration/list` | IntegrationList | 第三方对接 | system | admin | - |

### 38. intentRecognition（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 122 | `/intentRecognition/list` | IntentRecognitionList | 意图识别 | aiAgent | 需登录 | - |

### 39. kbConnector（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 123 | `/kb/connectors` | KbConnectors | 知识库连接器 | knowledge | 需登录 | - |
| 124 | `/cap/matrix` | CapabilityMatrix | 能力矩阵 | system | 需登录 | - |

### 40. knowledge（12 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 125 | `/knowledge/management` | KnowledgeManagement | 知识库管理 | knowledge | 需登录 | - |
| 126 | `/knowledge/batch-import` | KnowledgeBatchImport | 批量导入 | knowledge | 需登录 | - |
| 127 | `/knowledge/playground` | KnowledgePlayground | 检索 Playground | knowledge | 需登录 | - |
| 128 | `/knowledge/connectors` | KnowledgeConnectors | 外部连接器 | knowledge | 需登录 | - |
| 129 | `/knowledge/crawler-sources` | KnowledgeCrawlerSources | 网站爬取 | knowledge | 需登录 | - |
| 130 | `/knowledge/qa-candidates` | KnowledgeQACandidates | QA 审核队列 | knowledge | 需登录 | - |
| 131 | `/knowledge/feedbacks` | KnowledgeFeedbacks | 反馈管理 | knowledge | 需登录 | - |
| 132 | `/knowledge/tokens` | KnowledgeTokens | API Token | knowledge | 需登录 | - |
| 133 | `/knowledge/external` | KnowledgeExternal | 外部系统接入 | knowledge | 需登录 | - |
| 134 | `/knowledge/statistics` | KnowledgeStatistics | 知识库统计 | knowledge | 需登录 | - |
| 135 | `/knowledge/openapi` | KnowledgeOpenAPI | OpenAPI 集成 | knowledge | 需登录 | - |
| 136 | `/knowledge/rag-eval` | RagEvaluation | RAG 评测 | knowledge | 需登录 | - |

### 41. knowledgeBase（4 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 137 | `/knowledgeBase` | KnowledgeBase | 知识库管理 | knowledge | 需登录 | - |
| 138 | `/knowledgeBase/list` | KnowledgeBaseList | 知识库列表 | knowledge | 需登录 | - |
| 139 | `/knowledgeBase/create` | KnowledgeBaseCreate | 新建知识库 | knowledge | 需登录 | - |
| 140 | `/knowledgeBase/edit/:id` | KnowledgeBaseEdit | 编辑知识库 | knowledge | 需登录 | - |

### 42. kuaishouCard（3 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 141 | `/kuaishouCard` | KuaishouCard | 快手卡片 | community | 需登录 | - |
| 142 | `/kuaishou/stats` | KuaishouStats | 快手卡片统计 | community | 需登录 | - |
| 143 | `/kuaishou-card-stats/:id` | KuaishouCardStats | 快手卡片详情统计 | community | 需登录 | - |

### 43. leadMining（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 144 | `/lead-mining/index` | LeadMining | 线索发掘 | 线索发掘 | 需登录 | - |

### 44. livecode（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 145 | `/livecode` | LiveCodeManagement | 活码管理 | - | 需登录 | - |

### 45. llmReport（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 146 | `/reports/llm` | LlmUsageDashboard | LLM用量报表 | analytics | 需登录 | - |

### 46. llmRouting（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 147 | `/llmRouting/list` | LlmRoutingList | LLM路由 | aiAgent | 需登录 | - |
| 148 | `/llmRouting/cost` | LlmRoutingCostDashboard | 成本看板 | aiAgent | 需登录 | - |

### 47. marketingFlow（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 149 | `/marketingFlow/list` | MarketingFlowList | 营销自动化 | reach | 需登录 | - |

### 48. mcpCredential（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 150 | `/mcp/credentials` | McpCredentials | MCP凭证 | aiAgent | 需登录 | - |

### 49. messageHub（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 151 | `/messageHub/list` | MessageHubList | 消息中台 | workspace | 需登录 | - |
| 152 | `/messageHub/dashboard` | MessageHubDashboard | 消息总览 | workspace | 需登录 | - |

### 50. moduleMarket（6 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 153 | `/module-market` | ModuleMarket | 功能模块市场 | - | 公开 | - |
| 154 | `/module-market/detail/:key` | ModuleMarketDetail | 模块详情 | - | 公开 | - |
| 155 | `/module-market/my` | ModuleMy | 我的模块 | - | 需登录 | - |
| 156 | `/module-market/orders` | ModuleOrders | 我的订单 | - | 需登录 | - |
| 157 | `/subscription-wall` | SubscriptionWall | 订阅续费 | - | 需登录 | - |
| 158 | `/register` | Register | 商户注册 | - | 公开 | - |

### 51. objection（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 159 | `/objection/list` | ObjectionList | 异议处理 | sales | 需登录 | - |

### 52. observabilityDashboard（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 160 | `/observability-dashboard` | ObservabilityDashboard | AI 观测与渠道健康 | - | admin | - |

### 53. oneid（3 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 161 | `/oneid/list` | OneIDList | OneID 列表 | - | 需登录 | - |
| 162 | `/oneid/conflicts` | OneIDConflicts | 身份冲突解决 | - | 需登录 | - |
| 163 | `/oneid/merge-rules` | OneIDMergeRules | 合并规则配置 | - | 需登录 | - |

### 54. operationLog（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 164 | `/operationLog/list` | OperationLogList | 操作日志 | system | admin | - |
| 165 | `/operationLog/enhanced` | OperationLogEnhanced | 操作日志(增强) | system | admin | - |

### 55. ops（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 166 | `/ops-overview` | OpsOverview | 运维总览 | - | admin | - |
| 167 | `/sales-cockpit` | SalesCockpitIndex | AI 销冠驾驶舱 | - | 需登录 | - |

### 56. orderDraft（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 168 | `/dashboard/drafts` | OrderDraftList | 订单草稿 | workspace | 需登录 | - |
| 169 | `/dashboard/drafts/:id` | OrderDraftDetail | 草稿详情 | workspace | 需登录 | - |

### 57. permission（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 170 | `/system/permissions` | PermissionPanel | permission.title | system | admin | - |

### 58. persona（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 171 | `/persona/list` | PersonaList | 销冠画像 | sales | 需登录 | - |

### 59. platformAccount（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 172 | `/platformAccount/list` | PlatformAccountList | 平台账号 | system | admin | - |

### 60. qq（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 173 | `/qq` | QQBot | QQ 机器人 | community | 需登录 | - |
| 174 | `/qq/account` | QQAccount | 机器人账号 | community | 需登录 | - |

### 61. ragProductConfig（3 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 175 | `/system/rag-product-config` | RagProductConfigIndex | RAG 主配置 | knowledge | 需登录 | - |
| 176 | `/system/rag-account` | RagProductAccountConfig | RAG 账号配置 | knowledge | 需登录 | - |
| 177 | `/system/rag-product` | RagProductManagement | RAG 产品管理 | knowledge | 需登录 | - |

### 62. reachPipeline（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 178 | `/reachPipeline/list` | ReachPipelineList | 触达Pipeline | reach | 需登录 | - |
| 179 | `/reachPipeline/editor/:id?` | ReachPipelineEditor | Pipeline 编辑 | reach | 需登录 | - |

### 63. role（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 180 | `/system/roles` | RoleList | role.title | system | admin | - |

### 64. salesWorkbench（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 181 | `/sales-workbench` | SalesWorkbenchIndex | 销售工作台 | workspace | 需登录 | - |

### 65. scriptTemplate（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 182 | `/scriptTemplate/list` | ScriptTemplateList | 话术库 | aiAgent | 需登录 | - |
| 183 | `/scriptTemplate/ab-stats` | ScriptTemplateAbStats | 话术AB测试 | aiAgent | 需登录 | - |

### 66. securityAudit（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 184 | `/securityAudit/list` | SecurityAuditList | 安全审计 | system | admin | - |

### 67. shortLink（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 185 | `/shortLink` | ShortLink | 短链管理 | community | 需登录 | - |
| 186 | `/shortLink/stats` | ShortLinkStats | 短链统计 | community | 需登录 | - |

### 68. sms（4 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 187 | `/sms/list` | SmsList | 短信列表 | reach | 需登录 | - |
| 188 | `/sms/drafts` | SmsDrafts | 短信草稿 | reach | 需登录 | - |
| 189 | `/sms/jobs` | SmsJobs | 短信任务 | reach | 需登录 | - |
| 190 | `/sms/config` | SmsConfig | 短信配置 | reach | 需登录 | - |

### 69. sopAgent（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 191 | `/sopAgent/list` | SopAgentList | 销冠 SOP 智能体 | aiAgent | 需登录 | - |

### 70. sopTemplateKb（5 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 192 | `/sop-template` | SOPTemplate | SOP 模板库 | aiAgent | 需登录 | - |
| 193 | `/sop-template/list` | SOPTemplateList | SOP 模板列表 | aiAgent | 需登录 | - |
| 194 | `/sop-template/market` | SOPTemplateMarket | SOP 模板市场 | aiAgent | 需登录 | - |
| 195 | `/sop-template/editor` | SOPTemplateEditor | SOP 模板编辑 | aiAgent | 需登录 | - |
| 196 | `/sop-template/editor/:id` | SOPTemplateEditorEdit | SOP 模板编辑 | aiAgent | 需登录 | - |

### 71. system（12 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 197 | `/system/config` | SystemConfig | 站点设置 | system | admin | - |
| 198 | `/system/obs-config` | SystemObsConfig | 存储配置 | system | admin | - |
| 199 | `/system/material-library` | SystemMaterialLibrary | 素材库 | system | admin | - |
| 200 | `/system/monitor` | SystemMonitor | 监控 | system | admin | - |
| 201 | `/system/trace` | TraceMonitor | 链路追踪 | system | admin | - |
| 202 | `/system/guide` | SystemGuide | 使用引导 | system | admin | - |
| 203 | `/system/rag-overview` | RagOverview | RAG概览 | knowledge | 需登录 | - |
| 204 | `/system/automation-hub` | AutomationHub | 自动化中心 | system | admin | - |
| 205 | `/system/config-params` | ConfigParams | 动态阈值参数 | system | admin | - |
| 206 | `/system/dictionaries` | SystemDictionaries | 业务字典 | system | admin | - |
| 207 | `/system/feature-flags` | SystemFeatureFlags | 功能开关 | system | admin | - |
| 208 | `/system/ltc-config` | LTCConfig | LTC 运营开关 | system | admin | - |

### 72. systemUser（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 209 | `/system/users` | SystemUserList | systemUser.title | system | admin | - |

### 73. tagSegmentation（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 210 | `/tagSegmentation/list` | TagSegmentationList | 标签分层 | customer | 需登录 | - |

### 74. telegram（3 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 211 | `/telegram` | Telegram | TG 机器人 | community | 需登录 | - |
| 212 | `/telegram/account` | TelegramAccount | 机器人账号 | community | 需登录 | - |
| 213 | `/telegram/gate` | TelegramGate | 入群管控 | community | 需登录 | - |

### 75. templateMarket（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 214 | `/template-market` | TemplateMarket | 模板/技能市场 | - | 公开 | - |

### 76. tiktok（4 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 215 | `/tiktok` | TikTok | TikTok | reach | 需登录 | /tiktok/list |
| 216 | `/tiktok/list` | TikTokList | TikTok卡片 | reach | 需登录 | - |
| 217 | `/tiktok/stats` | TikTokStats | TikTok统计 | reach | 需登录 | - |
| 218 | `/tiktok-card-stats/:id` | TikTokCardStats | TikTok卡片统计 | reach | 需登录 | - |

### 77. tuning（3 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 219 | `/confidence/panel` | ConfidencePanel | 置信度运营 | aiAgent | 需登录 | - |
| 220 | `/humanize/panel` | HumanizePanel | 拟人度评估 | aiAgent | 需登录 | - |
| 221 | `/feedbackLoop/panel` | FeedbackLoopPanel | 反馈学习闭环 | aiAgent | 需登录 | - |

### 78. unifiedMessage（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 222 | `/unifiedMessage/list` | UnifiedMessageList | 统一消息 | customer | 需登录 | - |

### 79. unknownQuestion（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 223 | `/unknown-questions` | UnknownQuestionList | 未知问题运营 | customerservice | 需登录 | - |

### 80. userSegment（3 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 224 | `/userSegment/list` | UserSegmentList | 用户分层 RFM | customer | 需登录 | - |
| 225 | `/userSegment/rfm-matrix` | RfmMatrix | RFM 矩阵 | analytics | 需登录 | - |
| 226 | `/userSegment/builder` | UserSegmentBuilder | 分层规则构建 | customer | 需登录 | - |

### 81. wecomAccount（2 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 227 | `/wecomAccount/list` | WecomAccountList | 企微账号管理 | workspace | 需登录 | - |
| 228 | `/wecomAccount/data` | WecomAccountData | 企微数据看板 | workspace | 需登录 | - |

### 82. whatsapp（7 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 229 | `/whatsapp` | Whatsapp | WhatsApp 社群 | community | 需登录 | - |
| 230 | `/whatsapp/account` | WhatsappAccount | 账号管理 | community | 需登录 | - |
| 231 | `/whatsapp/drafts` | WhatsappDrafts | 草稿箱 | community | 需登录 | - |
| 232 | `/whatsapp/jobs` | WhatsappJobs | 群发 | community | 需登录 | - |
| 233 | `/whatsapp/lead-group-selection` | LeadGroupSelection | 从线索库选择群体 | community | 需登录 | - |
| 234 | `/whatsapp/group-messaging` | BulkMessaging | 批量消息发送 | community | 需登录 | - |
| 235 | `/whatsapp/bulk-matrix` | WhatsappBulkMatrix | 群发矩阵 | community | 需登录 | - |

### 83. whatsappCloud（1 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 236 | `/whatsapp-cloud` | WhatsappCloud | WhatsApp Cloud | community | 需登录 | - |

### 84. workflowOrchestrator（3 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 237 | `/workflow-orchestrator/list` | WorkflowOrchestratorList | 工作流编排 | reach | 需登录 | - |
| 238 | `/workflow-orchestrator/editor/:workflow_id` | WorkflowOrchestratorEditor | 工作流编辑 | reach | 需登录 | - |
| 239 | `/workflow-orchestrator/execution/:id` | WorkflowOrchestratorExecution | 执行详情 | reach | 需登录 | - |

### 85. xianyuCard（3 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 240 | `/xianyuCard` | XianyuCard | 闲鱼卡片 | community | 需登录 | - |
| 241 | `/xianyu/stats` | XianyuStats | 闲鱼卡片统计 | community | 需登录 | - |
| 242 | `/xianyu-card-stats/:id` | XianyuCardStats | 闲鱼卡片详情统计 | community | 需登录 | - |

### 86. xiaohongshuCard（3 条）

| # | 路由 | name | 标题 | group | 权限 | 跳转 |
|---|---|---|---|---|---|---|
| 243 | `/xiaohongshuCard` | XiaohongshuCard | 小红书卡片 | community | 需登录 | - |
| 244 | `/xiaohongshu/stats` | XiaohongshuStats | 小红书卡片统计 | community | 需登录 | - |
| 245 | `/xiaohongshu-card-stats/:id` | XiaohongshuCardStats | 小红书卡片详情统计 | community | 需登录 | - |

## 逐路由实测状态（2026-10-28）

> 测试方式：ego-browser 真浏览器逐路由人工式点击（每次一个路由），采集 console/error/API 4xx-5xx/页面告警；动态路由用真实 ID 抽样验证。

- [x] `/` ✅ 通过
- [x] `/setup` ✅ 通过
- [x] `/login` ✅ 通过
- [x] `/help-center` ✅ 通过
- [x] `/chat/embed/default` ✅ 通过
- [ ] `/chat/embed/:channel_ref` — 未验证（需动态参数/数据）
- [x] `/profile` ✅ 通过
- [x] `/notifications` ✅ 通过
- [x] `/:pathMatch(.*)*` ✅ 通过
- [x] `/abExperiment/list` ✅ 通过
- [x] `/aiAgent` ✅ 通过
- [x] `/aiAgent/list` ✅ 通过
- [x] `/aiAgent/create` ✅ 通过
- [x] `/aiAgent/edit/:id` ✅ 通过
- [x] `/aiAgent/integrations` ✅ 通过
- [x] `/aiAgent/tools` ✅ 通过
- [x] `/aiProductivity/list` ✅ 通过
- [x] `/analytics/cohort-path` ✅ 通过
- [x] `/approvalTask/list` ✅ 通过
- [x] `/asset-bundle/list` ✅ 通过
- [x] `/asset-bundle/playground` ✅ 通过
- [x] `/asset-bundle/playground/:aid` ✅ 通过
- [x] `/asset-bundle/merchant-new` ✅ 通过
- [x] `/asset-bundle/merchant/:aid` ✅ 通过
- [x] `/asset-market` ✅ 通过
- [ ] `/asset-market/detail/:id` — 未验证（需动态参数/数据）
- [x] `/asset-market/my-assets` ✅ 通过
- [x] `/asset-market/sync-log` ✅ 通过
- [x] `/backup/list` ✅ 通过
- [x] `/backup/enhanced` ✅ 通过
- [x] `/badCase/list` ✅ 通过
- [x] `/batchOperation/list` ✅ 通过
- [x] `/bridge` ✅ 通过
- [x] `/bridge/token` ✅ 通过
- [x] `/bridge/extension` ✅ 通过
- [x] `/token` ✅ 通过
- [x] `/browser-automation` ✅ 通过
- [x] `/browser-automation/tasks` ✅ 通过
- [x] `/browser-automation/tasks/create` ✅ 通过
- [x] `/browser-automation/tasks/:id/edit` ✅ 通过
- [x] `/browser-automation/tasks/:id` ✅ 通过
- [ ] `/browser-automation/sessions/:id` — 未验证（需动态参数/数据）
- [x] `/browser-automation/cron` ✅ 通过
- [x] `/browser-automation/status` ✅ 通过
- [x] `/browser-automation/ego/flows` ✅ 通过
- [x] `/browser-automation/ego/executions` ✅ 通过
- [x] `/browser-automation/ego/runner` ✅ 通过
- [x] `/browser-automation/ego/assets` ✅ 通过
- [x] `/browser-automation/ego/assets/admin` ✅ 通过
- [x] `/chatChannel/list` ✅ 通过
- [x] `/chatChannel/create` ✅ 通过
- [x] `/chatChannel/edit/:id` ✅ 通过
- [x] `/chatChannel/install-guide/:id?` ✅ 通过
- [x] `/churnPrediction/list` ✅ 通过
- [x] `/clue/list` ✅ 通过
- [x] `/clue/statistics` ✅ 通过
- [x] `/community/list` ✅ 通过
- [x] `/conversionFunnel/list` ✅ 通过
- [x] `/customReport/list` ✅ 通过
- [x] `/customer360/list` ✅ 通过
- [x] `/customerEvent/list` ✅ 通过
- [x] `/customerJourney/dashboard` ✅ 通过
- [x] `/customerService/agentStatus` ✅ 通过
- [x] `/customerService/quickReply` ✅ 通过
- [x] `/customerService/sessionTag` ✅ 通过
- [x] `/customerService/aiSuggestion` ✅ 通过
- [x] `/customerService/csat` ✅ 通过
- [x] `/customerSession/list` ✅ 通过
- [x] `/dashboardScreen/list` ✅ 通过
- [x] `/dashboardScreen/builder` ✅ 通过
- [x] `/dialogueMemory/list` ✅ 通过
- [x] `/dingtalk-app` ✅ 通过
- [x] `/domainPool` ✅ 通过
- [x] `/douyinCard` ✅ 通过
- [x] `/douyin/stats` ✅ 通过
- [x] `/douyin-card-stats/:id` ✅ 通过
- [x] `/email` ✅ 通过
- [x] `/email/drafts` ✅ 通过
- [x] `/email/jobs` ✅ 通过
- [x] `/email/smtp` ✅ 通过
- [x] `/email/info` ✅ 通过
- [x] `/email/guide` ✅ 通过
- [x] `/email/deliverability` ✅ 通过
- [x] `/email/drag-editor` ✅ 通过
- [x] `/faq` ✅ 通过
- [x] `/faq/list` ✅ 通过
- [x] `/faq/editor` ✅ 通过
- [x] `/faq/editor/:id` ✅ 通过
- [x] `/feishu` ✅ 通过
- [x] `/feishu/account` ✅ 已修复并回归（刷新 Token：500→502 + 单一可操作提示）
- [x] `/followups/today` ✅ 通过
- [ ] `/followups/:id` — 未验证（需动态参数/数据）
- [x] `/geo-tools/brand-config` ✅ 通过
- [x] `/geo-tools/seo-infra` ✅ 通过
- [x] `/geo-tools/pusher-config` ✅ 通过
- [x] `/geo-tools/site-config` ✅ 通过
- [x] `/geo-tools/schema-templates` ✅ 通过
- [x] `/geo-tools/competitors` ✅ 通过
- [x] `/geo-tools/config` ✅ 通过
- [x] `/geo-tools/keyword-mining` ✅ 通过
- [x] `/geo-tools/content-creation` ✅ 通过
- [x] `/geo-tools/content-optimize` ✅ 通过
- [x] `/geo-tools/site-publish` ✅ 通过
- [x] `/geo-tools/push-center` ✅ 通过
- [x] `/geo-tools/platform-publish` ✅ 通过
- [x] `/geo-tools/workflow` ✅ 通过
- [x] `/geo-tools/knowledge-base` ✅ 通过
- [x] `/geo-tools/entity-graph` ✅ 通过
- [x] `/geo-tools/funnel-dashboard` ✅ 通过
- [x] `/geo-tools/index-tracking` ✅ 通过
- [x] `/geo-tools/visibility` ✅ 通过
- [x] `/geo-tools/sov-board` ✅ 通过
- [x] `/geo-tools/crawler-stats` ✅ 通过
- [x] `/geo-tools/verification` ✅ 通过
- [x] `/geo/decision-report` ✅ 通过
- [x] `/geo-tools/reports` ✅ 通过
- [x] `/geo-tools/alerts` ✅ 通过
- [x] `/glossary` ✅ 通过
- [x] `/i18n/dashboard` ✅ 通过
- [x] `/inbox/list` ✅ 通过
- [x] `/integration/list` ✅ 通过
- [x] `/intentRecognition/list` ✅ 通过
- [x] `/kb/connectors` ✅ 已修复并回归（同步 HTML 清单：500→400 + 可操作提示）
- [x] `/cap/matrix` ✅ 通过
- [x] `/knowledge/management` ✅ 通过
- [x] `/knowledge/batch-import` ✅ 通过
- [x] `/knowledge/playground` ✅ 通过
- [x] `/knowledge/connectors` ✅ 通过
- [x] `/knowledge/crawler-sources` ✅ 通过
- [x] `/knowledge/qa-candidates` ✅ 通过
- [x] `/knowledge/feedbacks` ✅ 通过
- [x] `/knowledge/tokens` ✅ 通过
- [x] `/knowledge/external` ✅ 通过
- [x] `/knowledge/statistics` ✅ 通过
- [x] `/knowledge/openapi` ✅ 通过
- [x] `/knowledge/rag-eval` ✅ 通过
- [x] `/knowledgeBase` ✅ 通过
- [x] `/knowledgeBase/list` ✅ 通过
- [x] `/knowledgeBase/create` ✅ 通过
- [x] `/knowledgeBase/edit/:id` ✅ 通过
- [x] `/kuaishouCard` ✅ 通过
- [x] `/kuaishou/stats` ✅ 通过
- [ ] `/kuaishou-card-stats/:id` — 未验证（需动态参数/数据）
- [x] `/lead-mining/index` ✅ 通过
- [x] `/livecode` ✅ 通过
- [x] `/reports/llm` ✅ 通过
- [x] `/llmRouting/list` ✅ 通过
- [x] `/llmRouting/cost` ✅ 通过
- [x] `/marketingFlow/list` ✅ 通过
- [x] `/mcp/credentials` ✅ 通过
- [x] `/messageHub/list` ✅ 通过
- [x] `/messageHub/dashboard` ✅ 通过
- [x] `/module-market` ✅ 通过
- [ ] `/module-market/detail/:key` — 未验证（需动态参数/数据）
- [x] `/module-market/my` ✅ 通过
- [x] `/module-market/orders` ✅ 通过
- [x] `/subscription-wall` ✅ 通过
- [x] `/register` ✅ 通过
- [x] `/objection/list` ✅ 通过
- [x] `/observability-dashboard` ✅ 通过
- [x] `/oneid/list` ✅ 通过
- [x] `/oneid/conflicts` ✅ 通过
- [x] `/oneid/merge-rules` ✅ 通过
- [x] `/operationLog/list` ✅ 通过
- [x] `/operationLog/enhanced` ✅ 通过
- [x] `/ops-overview` ✅ 通过
- [x] `/sales-cockpit` ✅ 通过
- [x] `/dashboard/drafts` ✅ 通过
- [ ] `/dashboard/drafts/:id` — 未验证（需动态参数/数据）
- [x] `/system/permissions` ✅ 通过
- [x] `/persona/list` ✅ 通过
- [x] `/platformAccount/list` ✅ 通过
- [x] `/qq` ✅ 通过
- [x] `/qq/account` ✅ 通过
- [x] `/system/rag-product-config` ✅ 通过
- [x] `/system/rag-account` ✅ 通过
- [x] `/system/rag-product` ✅ 通过
- [x] `/reachPipeline/list` ✅ 通过
- [x] `/reachPipeline/editor/:id?` ✅ 通过
- [x] `/system/roles` ✅ 通过
- [x] `/sales-workbench` ✅ 通过
- [x] `/scriptTemplate/list` ✅ 通过
- [x] `/scriptTemplate/ab-stats` ✅ 通过
- [x] `/securityAudit/list` ✅ 通过
- [x] `/shortLink` ✅ 通过
- [x] `/shortLink/stats` ✅ 通过
- [x] `/sms/list` ✅ 通过
- [x] `/sms/drafts` ✅ 通过
- [x] `/sms/jobs` ✅ 通过
- [x] `/sms/config` ✅ 通过
- [x] `/sopAgent/list` ✅ 通过
- [x] `/sop-template` ✅ 通过
- [x] `/sop-template/list` ✅ 通过
- [x] `/sop-template/market` ✅ 通过
- [x] `/sop-template/editor` ✅ 通过
- [x] `/sop-template/editor/:id` ✅ 通过
- [x] `/system/config` ✅ 通过
- [x] `/system/obs-config` ✅ 通过
- [x] `/system/material-library` ✅ 通过
- [x] `/system/monitor` ✅ 通过
- [x] `/system/trace` ✅ 通过
- [x] `/system/guide` ✅ 通过
- [x] `/system/rag-overview` ✅ 通过
- [x] `/system/automation-hub` ✅ 通过
- [x] `/system/config-params` ✅ 通过
- [x] `/system/dictionaries` ✅ 通过
- [x] `/system/feature-flags` ✅ 通过
- [x] `/system/ltc-config` ✅ 通过
- [x] `/system/users` ✅ 通过
- [x] `/tagSegmentation/list` ✅ 通过
- [x] `/telegram` ✅ 通过
- [x] `/telegram/account` ✅ 通过
- [x] `/telegram/gate` ✅ 通过
- [x] `/template-market` ✅ 通过
- [x] `/tiktok` ✅ 通过
- [x] `/tiktok/list` ✅ 通过
- [x] `/tiktok/stats` ✅ 通过
- [ ] `/tiktok-card-stats/:id` — 未验证（需动态参数/数据）
- [x] `/confidence/panel` ✅ 通过
- [x] `/humanize/panel` ✅ 通过
- [x] `/feedbackLoop/panel` ✅ 通过
- [x] `/unifiedMessage/list` ✅ 通过
- [x] `/unknown-questions` ✅ 通过
- [x] `/userSegment/list` ✅ 通过
- [x] `/userSegment/rfm-matrix` ✅ 通过
- [x] `/userSegment/builder` ✅ 通过
- [x] `/wecomAccount/list` ✅ 通过
- [x] `/wecomAccount/data` ✅ 通过
- [x] `/whatsapp` ✅ 通过
- [x] `/whatsapp/account` ✅ 通过
- [x] `/whatsapp/drafts` ✅ 通过
- [x] `/whatsapp/jobs` ✅ 通过
- [x] `/whatsapp/lead-group-selection` ✅ 通过
- [x] `/whatsapp/group-messaging` ✅ 通过
- [x] `/whatsapp/bulk-matrix` ✅ 通过
- [x] `/whatsapp-cloud` ✅ 通过
- [x] `/workflow-orchestrator/list` ✅ 通过
- [x] `/workflow-orchestrator/editor/:workflow_id` ✅ 通过
- [x] `/workflow-orchestrator/execution/:id` ✅ 通过
- [x] `/xianyuCard` ✅ 通过
- [x] `/xianyu/stats` ✅ 通过
- [ ] `/xianyu-card-stats/:id` — 未验证（需动态参数/数据）
- [x] `/xiaohongshuCard` ✅ 通过
- [x] `/xiaohongshu/stats` ✅ 通过
- [ ] `/xiaohongshu-card-stats/:id` — 未验证（需动态参数/数据）

## 数据正确性复测（2026-10-28 第二轮）

> 不只点交互，还抓 `/api/*` 响应体核对页面数字/列表/表单值；证据在 `evidence-data-20261028/`（240 条路由）。

- 已修复：`/notifications` 总数 20→1163、`/system/automation-hub` Webhook 5 条不显示、`/observability-dashboard` 全部显示“-”、`/geo-tools/workflow` 请求 undefined + 不自动选流程、`/kuaishou/stats` 统计跳 404、`/aiProductivity/list` 平均响应 3220s、`/geo-tools/site-publish` 500/重复提示、`/scriptTemplate/ab-stats` 话术下拉空、`/lead-mining/index` 配置不显示、`/oneid/conflicts` 列表恒空、知识连接器测试/拉取结果错、会话 AI 摘要恒为空。
- 仍缺业务数据无法核对：`/chat/embed/:channel_ref`、`/asset-market/detail/:id`、`/module-market/detail/:key`、`/followups/:id`、`/dashboard/drafts/:id`、会话监控 `sessions/:id`、404 参数路由。
- 其余路由的统计卡/表格/总数与接口响应一致，未发现新的数据错位。

## 动态路由补全（第三轮 2026-10-28）

- 已用真实数据验证：`/chat/embed/:channel_ref`（真实 app_key）、`/asset-market/detail/:id`（本地资产回落）、`/module-market/detail/:key`（ai-agent）、`/browser-automation/sessions/:id`（session 647）、404 参数路由。
- `/followups/:id`、`/dashboard/drafts/:id`：组件已实现，数据依赖 `FF_LTC_ORDER_DRAFT_DB` 草稿/旅程运行时；当前 off 档页面给出明确降级说明，不是缺页。
