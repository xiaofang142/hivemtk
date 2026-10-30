# 前端页面路由清单（任务四逐页点验 + 任务六新手引导文案）

- 来源：`user-web/src/router/index.js` + `src/router/modules/*.js`（86 个模块文件）
- 规模：**257 条页面路由**
- 生成方式：`scripts/gen_router_list.py`（只做索引，不开浏览器；重跑保留既有勾选状态）
- 校验口径：本地起 user-server + user-web，逐条路由**拟人点击**页面所有元素/表单/交互，
  比对**浏览器控制台报错**与 **Network 里的非 2xx API 响应**；发现错误即修复

## 测试环境（本轮）

| 形态 | 后端 | 前端 | 标识 |
|---|---|---|---|
| SaaS 多租户 | `127.0.0.1:8315`（`TENANT_MODE=on`） | `127.0.0.1:8321` | `tenant_mode=true` / `edition=saas` |
| 私域独立部署 | `127.0.0.1:8314`（`TENANT_MODE=off`） | `127.0.0.1:8320` | `tenant_mode=false` / `edition=opensource` |

- 浏览器：ego lite（`ego-browser`，复用登录态，脚本逐页拟人点击；控制台 + Network 全量采集）
- 登录账号：`uit_admin` / `Hive@Test123`（id=26，tenant_id=1）
- 两种形态**都要过**：SaaS 页出现的租户/商务属性，独立部署页必须不出现且不报错

## 状态图例

| 标记 | 含义 |
|---|---|
| `[x]` | 已逐页打开并点击验证：控制台无报错、API 无非 2xx |
| `[~]` | 已开页，响应属设计内拒绝（如参数必填校验、无权限），非缺陷 |
| `[!]` | 已点出缺陷，待修复 |
| `[ ]` | 未测 |

## 任务六 · 新手引导（页面导览）状态图例

「引导」列记的是**这一页的新手引导文案与步骤**写没写、写没写对：
读懂该页的功能/业务/使用/配置逻辑 → 写这一页的定位说明与关键操作说明 →
把说明挂到该页**真实存在**的 DOM 元素上 → 浏览器里点开确认高亮正确、× 能关掉。

| 标记 | 含义 |
|---|---|
| `G` | 已写导览：文案讲的是这一页的真实功能/操作/配置，高亮元素在浏览器里点过，关闭按钮生效 |
| `-` | 本页不需要导览（公共页/重定向/无业务语义），或尚未开写 |

## 清单

| # | 状态 | 路径 | 名称 | 标题 | 需登录 | 组件 | 源文件 | 引导 |
|---|---|---|---|---|---|---|---|---|
| 1 | [x] | `/setup` | InitSetup | 系统初始化 | - | `views/setup/InitSetup.vue` | `src/router/index.js` | - |
| 2 | [x] | `/login` | Login | 登录 | - | `views/Login.vue` | `src/router/index.js` | - |
| 3 | [x] | `/help-center` | HelpCenter | 帮助中心 | 否 | `views/public/HelpCenter.vue` | `src/router/index.js` | - |
| 4 | [x] | `/chat/embed/default` | ChatEmbed | 在线客服 | 否 | `views/chat/embed/Index.vue` | `src/router/index.js` | - |
| 5 | [x] | `/chat/embed/:channel_ref` | ChatEmbedChannel | 在线客服 | 否 | `views/chat/embed/Index.vue` | `src/router/index.js` | - |
| 6 | [x] | `profile` | Profile | 个人资料 | 是 | `views/Profile.vue` | `src/router/index.js` | - |
| 7 | [x] | `notifications` | Notifications | 通知中心 | 是 | `views/Notifications.vue` | `src/router/index.js` | - |
| 8 | [x] | `/:pathMatch(.*)*` | NotFound | 页面不存在 | - | `views/NotFound.vue` | `src/router/index.js` | - |
| 9 | [x] | `/` | Layout |  | - | `layout/Layout.vue` | `src/router/index.js` | - |
| 10 | [x] | `/chat/embed` | - |  | - | `-` | `src/router/index.js` | - |
| 11 | [x] | `/chat/embed/` | - |  | - | `-` | `src/router/index.js` | - |
| 12 | [x] | `/telegram/group` | - |  | - | `-` | `src/router/index.js` | - |
| 13 | [x] | `/oneid` | - |  | - | `-` | `src/router/index.js` | - |
| 14 | [x] | `/abExperiment/list` | AbExperimentList | A/B 实验 | 是 | `views/abExperiment/List.vue` | `src/router/modules/abExperiment.js` | - |
| 15 | [x] | `/aiAgent` | AIAgent | 智能体 | - | `views/aiAgent/List.vue` | `src/router/modules/aiAgent.js` | - |
| 16 | [x] | `/aiAgent/list` | AIAgentList | 智能体列表 | - | `views/aiAgent/List.vue` | `src/router/modules/aiAgent.js` | - |
| 17 | [x] | `/aiAgent/create` | AIAgentCreate | 创建智能体 | - | `views/aiAgent/Edit.vue` | `src/router/modules/aiAgent.js` | - |
| 18 | [x] | `/aiAgent/edit/:id` | AIAgentEdit | 编辑智能体 | - | `views/aiAgent/Edit.vue` | `src/router/modules/aiAgent.js` | - |
| 19 | [x] | `/aiAgent/integrations` | AICustomIntegration | 工具接入 | - | `views/aiAgent/CustomIntegration.vue` | `src/router/modules/aiAgent.js` | - |
| 20 | [x] | `/aiAgent/tools` | AIToolManagement | AI 工具管理 | - | `views/aiAgent/ToolManagement/ToolList.vue` | `src/router/modules/aiAgent.js` | - |
| 21 | [x] | `/aiProductivity/list` | AiProductivityList | AI产能分析 | 是 | `views/aiProductivity/List.vue` | `src/router/modules/aiProductivity.js` | - |
| 22 | [x] | `/analytics/cohort-path` | CohortPath | 留存与路径 | 是 | `views/analytics/CohortPath.vue` | `src/router/modules/analytics.js` | - |
| 23 | [x] | `/approvalTask/list` | ApprovalTaskList | 待办中心 | - | `views/approvalTask/List.vue` | `src/router/modules/approvalTask.js` | - |
| 24 | [x] | `/asset-bundle/list` | AssetBundleList | 资产包管理 | 是 | `views/assetBundle/List.vue` | `src/router/modules/assetBundle.js` | - |
| 25 | [x] | `/asset-bundle/playground` | AssetBundlePlayground | 开发者 Playground | 是 | `views/assetBundle/Playground.vue` | `src/router/modules/assetBundle.js` | - |
| 26 | [x] | `/asset-bundle/playground/:aid` | AssetBundlePlaygroundEdit | 开发者 Playground | 是 | `views/assetBundle/Playground.vue` | `src/router/modules/assetBundle.js` | - |
| 27 | [x] | `/asset-bundle/merchant-new` | AssetBundleMerchantNew | 商户新建话术包 | 是 | `views/assetBundle/MerchantEditor.vue` | `src/router/modules/assetBundle.js` | - |
| 28 | [x] | `/asset-bundle/merchant/:aid` | AssetBundleMerchantEdit | 商户配置话术包 | 是 | `views/assetBundle/MerchantEditor.vue` | `src/router/modules/assetBundle.js` | - |
| 29 | [x] | `/asset-market` | AssetMarket | 资产市场 | 是 | `views/assetMarket/Market.vue` | `src/router/modules/assetMarket.js` | - |
| 30 | [x] | `/asset-market/detail/:id` | AssetMarketDetail | 资产详情 | 是 | `views/assetMarket/Detail.vue` | `src/router/modules/assetMarket.js` | - |
| 31 | [~] | `/asset-market/my-assets` | AssetMyAssets | 我的资产 | 是 | `views/assetMarket/MyAssets.vue` | `src/router/modules/assetMarket.js` | - |
| 32 | [x] | `/asset-market/sync-log` | AssetSyncLog | 同步日志 | 是 | `views/assetMarket/SyncLog.vue` | `src/router/modules/assetMarket.js` | - |
| 33 | [x] | `/backup/list` | BackupList | 备份恢复 | 是 | `views/backup/List.vue` | `src/router/modules/backup.js` | - |
| 34 | [x] | `/backup/enhanced` | BackupEnhanced | 备份管理 | 是 | `views/backup/Enhanced.vue` | `src/router/modules/backup.js` | - |
| 35 | [x] | `/badCase/list` | BadCaseList | Bad Case 队列 | - | `views/badCase/List.vue` | `src/router/modules/badCase.js` | - |
| 36 | [x] | `/batchOperation/list` | BatchOperationList | 批量操作 | 是 | `views/batchOperation/List.vue` | `src/router/modules/batchOperation.js` | - |
| 37 | [x] | `/bridge/token` | BridgeTokenManagement | 桥接凭证 | - | `views/bridge/TokenManagement.vue` | `src/router/modules/bridgeToken.js` | - |
| 38 | [~] | `/bridge/extension` | BridgeExtensionPackage | 扩展安装包 | 是 | `views/bridge/ExtensionPackage.vue` | `src/router/modules/bridgeToken.js` | - |
| 39 | [x] | `/bridge` | BridgeRoot | 渠道接入 | - | `-` | `src/router/modules/bridgeToken.js` | - |
| 40 | [x] | `/token` | - |  | - | `-` | `src/router/modules/bridgeToken.js` | - |
| 41 | [x] | `/browser-automation` | BrowserAutomation | 浏览器自动化 | 是 | `-` | `src/router/modules/browserAutomation.js` | - |
| 42 | [x] | `/browser-automation/tasks` | BrowserAutomationList | 任务列表 | 是 | `views/browserAutomation/List.vue` | `src/router/modules/browserAutomation.js` | - |
| 43 | [x] | `/browser-automation/tasks/create` | BrowserAutomationCreate | 新建任务 | 是 | `views/browserAutomation/Editor.vue` | `src/router/modules/browserAutomation.js` | - |
| 44 | [x] | `/browser-automation/tasks/:id/edit` | BrowserAutomationEditor | 编辑任务 | 是 | `views/browserAutomation/Editor.vue` | `src/router/modules/browserAutomation.js` | - |
| 45 | [x] | `/browser-automation/tasks/:id` | BrowserAutomationDetail | 任务详情 | 是 | `views/browserAutomation/Detail.vue` | `src/router/modules/browserAutomation.js` | - |
| 46 | [x] | `/browser-automation/sessions/:id` | BrowserAutomationMonitor | 执行监控 | 是 | `views/browserAutomation/Monitor.vue` | `src/router/modules/browserAutomation.js` | - |
| 47 | [x] | `/browser-automation/cron` | BrowserAutomationCron | 定时触发器 | 是 | `views/browserAutomation/Cron.vue` | `src/router/modules/browserAutomation.js` | - |
| 48 | [x] | `/browser-automation/status` | BrowserAutomationStatus | Host 状态 | 是 | `views/browserAutomation/Status.vue` | `src/router/modules/browserAutomation.js` | - |
| 49 | [x] | `/browser-automation/ego/flows` | EgoFlows | 我的流程 | 是 | `views/browserAutomation/ego/Flows.vue` | `src/router/modules/browserAutomation.js` | - |
| 50 | [x] | `/browser-automation/ego/sessions` | EgoSessions | 通用会话 | 是 | `views/browserAutomation/ego/Sessions.vue` | `src/router/modules/browserAutomation.js` | - |
| 51 | [x] | `/browser-automation/ego/executions` | EgoExecutions | 执行监控 | 是 | `views/browserAutomation/ego/Executions.vue` | `src/router/modules/browserAutomation.js` | - |
| 52 | [x] | `/browser-automation/ego/runner` | EgoRunner | Runner 状态 | 是 | `views/browserAutomation/ego/Runner.vue` | `src/router/modules/browserAutomation.js` | - |
| 53 | [x] | `/browser-automation/ego/assets` | EgoAssets | 资产中心 | 是 | `views/browserAutomation/ego/Assets.vue` | `src/router/modules/browserAutomation.js` | - |
| 54 | [x] | `/browser-automation/ego/assets/admin` | EgoAssetAdmin | 资产发布台 | 是 | `views/browserAutomation/ego/AssetAdmin.vue` | `src/router/modules/browserAutomation.js` | - |
| 55 | [x] | `/browser-automation/ego/telemetry` | EgoTelemetry | 遥测看板 | 是 | `views/browserAutomation/ego/Telemetry.vue` | `src/router/modules/browserAutomation.js` | - |
| 56 | [x] | `/chatChannel/list` | ChatChannelList | 客服渠道 | 是 | `views/chatChannel/List.vue` | `src/router/modules/chatChannel.js` | - |
| 57 | [x] | `/chatChannel/create` | ChatChannelCreate | 新建客服渠道 | 是 | `views/chatChannel/Create.vue` | `src/router/modules/chatChannel.js` | - |
| 58 | [x] | `/chatChannel/edit/:id` | ChatChannelEdit | 编辑客服渠道 | 是 | `views/chatChannel/Edit.vue` | `src/router/modules/chatChannel.js` | - |
| 59 | [x] | `/chatChannel/install-guide/:id?` | ChatChannelInstallGuide | Widget 安装引导 | 是 | `views/chatChannel/InstallGuide.vue` | `src/router/modules/chatChannel.js` | - |
| 60 | [x] | `/churnPrediction/list` | ChurnPredictionList | 流失预警 | 是 | `views/churnPrediction/List.vue` | `src/router/modules/churnPrediction.js` | - |
| 61 | [x] | `/clue/list` | ClueList | 线索列表 | - | `views/clue/List.vue` | `src/router/modules/clue.js` | - |
| 62 | [x] | `/clue/statistics` | ClueStatistics | 线索统计 | - | `views/clue/Statistics.vue` | `src/router/modules/clue.js` | - |
| 63 | [x] | `/community/list` | CommunityList | 社群管理 | - | `views/community/List.vue` | `src/router/modules/community.js` | - |
| 64 | [x] | `/conversionFunnel/list` | ConversionFunnelList | 转化漏斗 | 是 | `views/conversionFunnel/List.vue` | `src/router/modules/conversionFunnel.js` | - |
| 65 | [x] | `/customReport/list` | CustomReportList | 自定义报表 | 是 | `views/customReport/List.vue` | `src/router/modules/customReport.js` | - |
| 66 | [x] | `/customer360/list` | Customer360List | 客户 360 | 是 | `views/customer360/List.vue` | `src/router/modules/customer360.js` | - |
| 67 | [x] | `/customer360/consents` | CustomerConsents | 客户同意与偏好 | 是 | `views/customer360/Consents.vue` | `src/router/modules/customer360.js` | - |
| 68 | [x] | `/customerEvent/list` | CustomerEventList | 客户事件 | 是 | `views/customerEvent/List.vue` | `src/router/modules/customerEvent.js` | - |
| 69 | [x] | `/customerJourney/dashboard` | CustomerJourneyDashboard | 客户旅程大屏 | 是 | `views/customerJourney/Dashboard.vue` | `src/router/modules/customerJourney.js` | - |
| 70 | [x] | `/customerService/agentStatus` | AgentStatus | 坐席状态 | 是 | `views/customerService/AgentStatus.vue` | `src/router/modules/customerService.js` | - |
| 71 | [x] | `/customerService/quickReply` | QuickReply | 快捷回复 | 是 | `views/customerService/QuickReply.vue` | `src/router/modules/customerService.js` | - |
| 72 | [x] | `/customerService/sessionTag` | SessionTag | 会话标签 | 是 | `views/customerService/SessionTag.vue` | `src/router/modules/customerService.js` | - |
| 73 | [x] | `/customerService/aiSuggestion` | AISuggestion | AI 建议 | 是 | `views/customerService/AISuggestion.vue` | `src/router/modules/customerService.js` | - |
| 74 | [x] | `/customerService/csat` | CsatDashboard | CSAT 看板 | 是 | `views/customerService/CsatDashboard.vue` | `src/router/modules/customerService.js` | - |
| 75 | [x] | `/customerSession/list` | CustomerSessionList | 客服会话 | 是 | `views/customerSession/List.vue` | `src/router/modules/customerSession.js` | - |
| 76 | [x] | `/dashboardScreen/list` | DashboardScreenList | 数据大屏 | 是 | `views/dashboardScreen/List.vue` | `src/router/modules/dashboardScreen.js` | - |
| 77 | [x] | `/dashboardScreen/builder` | DashboardScreenBuilder | 大屏构建 | 是 | `views/dashboardScreen/Builder.vue` | `src/router/modules/dashboardScreen.js` | - |
| 78 | [x] | `/dialogueMemory/list` | DialogueMemoryList | 对话记忆 | 是 | `views/dialogueMemory/List.vue` | `src/router/modules/dialogueMemory.js` | - |
| 79 | [x] | `/dingtalk-app` | DingtalkApp | 钉钉应用 | - | `views/dingtalkApp/DingtalkAppAccount.vue` | `src/router/modules/dingtalkApp.js` | - |
| 80 | [x] | `/domainPool` | DomainPool | 域名池管理 | 是 | `views/domainPool/List.vue` | `src/router/modules/domainPool.js` | - |
| 81 | [x] | `/douyinCard` | DouyinCard | 抖音卡片 | - | `views/douyinCard/List.vue` | `src/router/modules/douyinCard.js` | - |
| 82 | [x] | `/douyin/stats` | DouyinStats | 抖音卡片统计 | - | `views/douyinCard/Stats.vue` | `src/router/modules/douyinCard.js` | - |
| 83 | [x] | `/douyin-card-stats/:id` | DouyinCardStats | 抖音卡片详情统计 | - | `views/douyinCard/CardStats.vue` | `src/router/modules/douyinCard.js` | - |
| 84 | [x] | `/email` | Email | 邮件列表 | - | `views/email/EmailList.vue` | `src/router/modules/email.js` | - |
| 85 | [x] | `/email/drafts` | EmailDrafts | 我的草稿 | - | `views/email/Drafts.vue` | `src/router/modules/email.js` | - |
| 86 | [x] | `/email/jobs` | EmailJobs | 我的任务 | - | `views/email/Jobs.vue` | `src/router/modules/email.js` | - |
| 87 | [x] | `/email/smtp` | EmailSmtp | 邮件账号 | - | `views/email/Smtp.vue` | `src/router/modules/email.js` | - |
| 88 | [x] | `/email/info` | EmailInfo | 邮件代理 | - | `views/email/Info.vue` | `src/router/modules/email.js` | - |
| 89 | [x] | `/email/guide` | EmailGuide | 使用引导 | - | `views/email/Guide.vue` | `src/router/modules/email.js` | - |
| 90 | [x] | `/email/deliverability` | EmailDeliverability | 送达分析 | 是 | `views/email/Deliverability.vue` | `src/router/modules/email.js` | - |
| 91 | [x] | `/email/drag-editor` | EmailDragEditor | 拖拽编辑器 | 是 | `views/email/DragEditor.vue` | `src/router/modules/email.js` | - |
| 92 | [x] | `/faq` | FAQ | FAQ 知识库 | - | `views/faq/Index.vue` | `src/router/modules/faq.js` | - |
| 93 | [x] | `/faq/list` | FAQList | FAQ 列表 | - | `views/faq/List.vue` | `src/router/modules/faq.js` | - |
| 94 | [x] | `/faq/editor` | FAQEditor | FAQ 编辑 | - | `views/faq/Editor.vue` | `src/router/modules/faq.js` | - |
| 95 | [x] | `/faq/editor/:id` | FAQEditorEdit | FAQ 编辑 | - | `views/faq/Editor.vue` | `src/router/modules/faq.js` | - |
| 96 | [x] | `/feishu` | Feishu | 飞书 | - | `views/feishu/FeishuAccount.vue` | `src/router/modules/feishu.js` | - |
| 97 | [x] | `/feishu/account` | FeishuAccount | 飞书账号 | - | `views/feishu/FeishuAccount.vue` | `src/router/modules/feishu.js` | - |
| 98 | [~] | `/followups/today` | FollowUpToday | 今日跟进 | 是 | `views/followups/Today.vue` | `src/router/modules/followups.js` | - |
| 99 | [~] | `/followups/:id` | FollowUpDetail | 跟进详情 | 是 | `views/followups/Today.vue` | `src/router/modules/followups.js` | - |
| 100 | [x] | `/geo-tools/brand-config` | GeoBrandConfig | 品牌与域名 | 是 | `views/geo/BrandConfig.vue` | `src/router/modules/geoTools.js` | - |
| 101 | [x] | `/geo-tools/seo-infra` | GeoSeoInfra | SEO 基础设施 | 是 | `views/geo/SeoInfra.vue` | `src/router/modules/geoTools.js` | - |
| 102 | [x] | `/geo-tools/pusher-config` | GeoPusherConfig | 蜘蛛推送配置 | 是 | `views/geo/PusherConfig.vue` | `src/router/modules/geoTools.js` | - |
| 103 | [x] | `/geo-tools/site-config` | GeoSiteConfig | 静态站部署 | 是 | `views/geo/SiteConfig.vue` | `src/router/modules/geoTools.js` | - |
| 104 | [x] | `/geo-tools/schema-templates` | GeoSchemaTemplates | Schema 模板 | 是 | `views/geo/SchemaTemplates.vue` | `src/router/modules/geoTools.js` | - |
| 105 | [x] | `/geo-tools/competitors` | GeoCompetitors | 竞品管理 | 是 | `views/geo/CompetitorManage.vue` | `src/router/modules/geoTools.js` | - |
| 106 | [x] | `/geo-tools/config` | GeoConfigOptimizer | 配置优化 | 是 | `views/geo/ConfigOptimizer.vue` | `src/router/modules/geoTools.js` | - |
| 107 | [x] | `/geo-tools/autopilot` | GeoAutopilot | 自动驾驶（一键） | 是 | `views/geo/Autopilot.vue` | `src/router/modules/geoTools.js` | - |
| 108 | [x] | `/geo-tools/keyword-mining` | GeoKeywordMining | 关键词蒸馏 | 是 | `views/geo/KeywordMining.vue` | `src/router/modules/geoTools.js` | - |
| 109 | [x] | `/geo-tools/content-creation` | GeoContentCreation | 内容创作 | 是 | `views/geo/ContentCreation.vue` | `src/router/modules/geoTools.js` | - |
| 110 | [x] | `/geo-tools/content-optimize` | GeoContentOptimize | 文章优化 | 是 | `views/geo/ContentOptimize.vue` | `src/router/modules/geoTools.js` | - |
| 111 | [x] | `/geo-tools/site-publish` | GeoSitePublish | 发布到官网 | 是 | `views/geo/SitePublish.vue` | `src/router/modules/geoTools.js` | - |
| 112 | [x] | `/geo-tools/push-center` | GeoPushCenter | 蜘蛛推送 | 是 | `views/geo/PushCenter.vue` | `src/router/modules/geoTools.js` | - |
| 113 | [x] | `/geo-tools/platform-publish` | GeoPlatformPublish | 多平台发布 | 是 | `views/geo/PlatformPublish.vue` | `src/router/modules/geoTools.js` | - |
| 114 | [x] | `/geo-tools/workflow` | GeoWorkflow | 工作流 | 是 | `views/geo/WorkflowEditor.vue` | `src/router/modules/geoTools.js` | - |
| 115 | [x] | `/geo-tools/knowledge-base` | GeoKnowledgeBase | GEO 知识库 | 是 | `views/geo/KnowledgeBase.vue` | `src/router/modules/geoTools.js` | - |
| 116 | [x] | `/geo-tools/entity-graph` | GeoEntityGraph | 实体图谱 | 是 | `views/geo/EntityGraph.vue` | `src/router/modules/geoTools.js` | - |
| 117 | [x] | `/geo-tools/funnel-dashboard` | GeoFunnelDashboard | 漏斗总览 | 是 | `views/geo/FunnelDashboard.vue` | `src/router/modules/geoTools.js` | - |
| 118 | [x] | `/geo-tools/index-tracking` | GeoIndexTracking | 收录与引用 | 是 | `views/geo/IndexTracking.vue` | `src/router/modules/geoTools.js` | - |
| 119 | [x] | `/geo-tools/visibility` | GeoVisibilityBoard | 可见性趋势 | 是 | `views/geo/VisibilityBoard.vue` | `src/router/modules/geoTools.js` | - |
| 120 | [x] | `/geo-tools/sov-board` | GeoSovBoard | 竞品 SOV | 是 | `views/geo/SovBoard.vue` | `src/router/modules/geoTools.js` | - |
| 121 | [x] | `/geo-tools/crawler-stats` | GeoCrawlerStats | 爬虫统计 | 是 | `views/geo/CrawlerStats.vue` | `src/router/modules/geoTools.js` | - |
| 122 | [x] | `/geo-tools/verification` | GeoVerification | 多模型验证 | 是 | `views/geo/Verification.vue` | `src/router/modules/geoTools.js` | - |
| 123 | [x] | `/geo/decision-report` | GeoDecisionReport | 决策链报表 | 是 | `views/geo/DecisionReport.vue` | `src/router/modules/geoTools.js` | - |
| 124 | [x] | `/geo-tools/reports` | GeoReports | 成本报表 | 是 | `views/geo/Reports.vue` | `src/router/modules/geoTools.js` | - |
| 125 | [x] | `/geo-tools/alerts` | GeoAlertCenter | 告警中心 | 是 | `views/geo/AlertCenter.vue` | `src/router/modules/geoTools.js` | - |
| 126 | [x] | `/glossary` | GlossaryList | 术语表管理 | 是 | `views/glossary/List.vue` | `src/router/modules/glossary.js` | - |
| 127 | [x] | `/i18n/dashboard` | I18nDashboard | 多语言监控 | 是 | `views/i18n/Dashboard.vue` | `src/router/modules/i18nStats.js` | - |
| 128 | [x] | `/inbox/list` | InboxList | 统一收件箱 | - | `views/inbox/List.vue` | `src/router/modules/inbox.js` | - |
| 129 | [x] | `/integration/list` | IntegrationList | 第三方对接 | 是 | `views/integration/List.vue` | `src/router/modules/integration.js` | - |
| 130 | [x] | `/intentRecognition/list` | IntentRecognitionList | 意图识别 | 是 | `views/intentRecognition/List.vue` | `src/router/modules/intentRecognition.js` | - |
| 131 | [x] | `/kb/connectors` | KbConnectors | 知识库连接器 | 是 | `views/kb/Connectors.vue` | `src/router/modules/kbConnector.js` | - |
| 132 | [x] | `/cap/matrix` | CapabilityMatrix | 能力矩阵 | 是 | `views/cap/Matrix.vue` | `src/router/modules/kbConnector.js` | - |
| 133 | [x] | `/cap/diagnostics` | CapabilityDiagnostics | 能力运行诊断 | 是 | `views/cap/Diagnostics.vue` | `src/router/modules/kbConnector.js` | - |
| 134 | [x] | `/knowledge/management` | KnowledgeManagement | 知识库管理 | 是 | `views/KnowledgeWorkspace/KnowledgeManagement.vue` | `src/router/modules/knowledge.js` | - |
| 135 | [x] | `/knowledge/batch-import` | KnowledgeBatchImport | 批量导入 | 是 | `views/KnowledgeWorkspace/BatchImport.vue` | `src/router/modules/knowledge.js` | - |
| 136 | [x] | `/knowledge/playground` | KnowledgePlayground | 检索 Playground | 是 | `views/KnowledgeWorkspace/Playground.vue` | `src/router/modules/knowledge.js` | - |
| 137 | [x] | `/knowledge/connectors` | KnowledgeConnectors | 外部连接器 | 是 | `views/KnowledgeWorkspace/Connectors.vue` | `src/router/modules/knowledge.js` | - |
| 138 | [x] | `/knowledge/crawler-sources` | KnowledgeCrawlerSources | 网站爬取 | 是 | `views/KnowledgeWorkspace/CrawlerSources.vue` | `src/router/modules/knowledge.js` | - |
| 139 | [x] | `/knowledge/qa-candidates` | KnowledgeQACandidates | QA 审核队列 | 是 | `views/KnowledgeWorkspace/QACandidates.vue` | `src/router/modules/knowledge.js` | - |
| 140 | [x] | `/knowledge/feedbacks` | KnowledgeFeedbacks | 反馈管理 | 是 | `views/KnowledgeWorkspace/FeedbackList.vue` | `src/router/modules/knowledge.js` | - |
| 141 | [x] | `/knowledge/tokens` | KnowledgeTokens | API Token | 是 | `views/KnowledgeWorkspace/ApiToken.vue` | `src/router/modules/knowledge.js` | - |
| 142 | [x] | `/knowledge/external` | KnowledgeExternal | 外部系统接入 | 是 | `views/KnowledgeWorkspace/ExternalImport.vue` | `src/router/modules/knowledge.js` | - |
| 143 | [x] | `/knowledge/statistics` | KnowledgeStatistics | 知识库统计 | 是 | `views/KnowledgeWorkspace/KnowledgeStatistics.vue` | `src/router/modules/knowledge.js` | - |
| 144 | [x] | `/knowledge/openapi` | KnowledgeOpenAPI | OpenAPI 集成 | 是 | `views/KnowledgeWorkspace/OpenAPIIntegration.vue` | `src/router/modules/knowledge.js` | - |
| 145 | [x] | `/knowledge/rag-eval` | RagEvaluation | RAG 评测 | 是 | `views/knowledge/RagEvaluation.vue` | `src/router/modules/knowledge.js` | - |
| 146 | [x] | `/knowledgeBase` | KnowledgeBase | 知识库管理 | - | `views/knowledgeBase/List.vue` | `src/router/modules/knowledgeBase.js` | - |
| 147 | [x] | `/knowledgeBase/list` | KnowledgeBaseList | 知识库列表 | - | `views/knowledgeBase/List.vue` | `src/router/modules/knowledgeBase.js` | - |
| 148 | [x] | `/knowledgeBase/create` | KnowledgeBaseCreate | 新建知识库 | - | `views/knowledgeBase/List.vue` | `src/router/modules/knowledgeBase.js` | - |
| 149 | [x] | `/knowledgeBase/edit/:id` | KnowledgeBaseEdit | 编辑知识库 | - | `views/knowledgeBase/List.vue` | `src/router/modules/knowledgeBase.js` | - |
| 150 | [x] | `/kuaishouCard` | KuaishouCard | 快手卡片 | - | `views/kuaishouCard/List.vue` | `src/router/modules/kuaishouCard.js` | - |
| 151 | [x] | `/kuaishou/stats` | KuaishouStats | 快手卡片统计 | - | `views/kuaishouCard/Stats.vue` | `src/router/modules/kuaishouCard.js` | - |
| 152 | [x] | `/kuaishou-card-stats/:id` | KuaishouCardStats | 快手卡片详情统计 | - | `views/kuaishouCard/CardStats.vue` | `src/router/modules/kuaishouCard.js` | - |
| 153 | [x] | `/lead-mining/index` | LeadMining | 线索发掘 | - | `views/leadMining/Index.vue` | `src/router/modules/leadMining.js` | - |
| 154 | [x] | `/livecode` | LiveCodeManagement | 活码管理 | - | `views/livecode/LiveCodeManagement.vue` | `src/router/modules/livecode.js` | - |
| 155 | [x] | `/reports/llm` | LlmUsageDashboard | LLM用量报表 | 是 | `views/reports/LlmUsageDashboard.vue` | `src/router/modules/llmReport.js` | - |
| 156 | [x] | `/llmRouting/list` | LlmRoutingList | LLM路由 | 是 | `views/llmRouting/List.vue` | `src/router/modules/llmRouting.js` | - |
| 157 | [x] | `/llmRouting/cost` | LlmRoutingCostDashboard | 成本看板 | 是 | `views/llmRouting/CostDashboard.vue` | `src/router/modules/llmRouting.js` | - |
| 158 | [x] | `/marketingFlow/list` | MarketingFlowList | 营销自动化 | 是 | `views/marketingFlow/List.vue` | `src/router/modules/marketingFlow.js` | - |
| 159 | [x] | `/mcp/credentials` | McpCredentials | MCP凭证 | 是 | `views/mcp/Credentials.vue` | `src/router/modules/mcpCredential.js` | - |
| 160 | [x] | `/messageHub/list` | MessageHubList | 消息中台 | - | `views/messageHub/List.vue` | `src/router/modules/messageHub.js` | - |
| 161 | [x] | `/messageHub/dashboard` | MessageHubDashboard | 消息总览 | - | `views/messageHub/Dashboard.vue` | `src/router/modules/messageHub.js` | - |
| 162 | [x] | `/module-market` | ModuleMarket | 功能模块市场 | 否 | `views/moduleMarket/Market.vue` | `src/router/modules/moduleMarket.js` | - |
| 163 | [x] | `/module-market/detail/:key` | ModuleMarketDetail | 模块详情 | 否 | `views/moduleMarket/Detail.vue` | `src/router/modules/moduleMarket.js` | - |
| 164 | [x] | `/module-market/my` | ModuleMy | 我的模块 | 是 | `views/moduleMarket/MyModules.vue` | `src/router/modules/moduleMarket.js` | - |
| 165 | [x] | `/module-market/orders` | ModuleOrders | 我的订单 | 是 | `views/moduleMarket/Orders.vue` | `src/router/modules/moduleMarket.js` | - |
| 166 | [x] | `/subscription-wall` | SubscriptionWall | 订阅续费 | 是 | `views/subscription/Wall.vue` | `src/router/modules/moduleMarket.js` | - |
| 167 | [x] | `/register` | Register | 商户注册 | 否 | `views/register/Register.vue` | `src/router/modules/moduleMarket.js` | - |
| 168 | [x] | `/objection/list` | ObjectionList | 异议处理 | 是 | `views/objection/List.vue` | `src/router/modules/objection.js` | - |
| 169 | [x] | `/observability-dashboard` | ObservabilityDashboard | AI 观测与渠道健康 | - | `views/observabilityDashboard/Index.vue` | `src/router/modules/observabilityDashboard.js` | - |
| 170 | [x] | `/oneid/list` | OneIDList | OneID 列表 | 是 | `views/oneid/List.vue` | `src/router/modules/oneid.js` | - |
| 171 | [x] | `/oneid/conflicts` | OneIDConflicts | 身份冲突解决 | 是 | `views/oneid/Conflicts.vue` | `src/router/modules/oneid.js` | - |
| 172 | [x] | `/oneid/merge-rules` | OneIDMergeRules | 合并规则配置 | 是 | `views/oneid/MergeRuleConfig.vue` | `src/router/modules/oneid.js` | - |
| 173 | [x] | `/operationLog/list` | OperationLogList | 操作日志 | 是 | `views/operationLog/List.vue` | `src/router/modules/operationLog.js` | - |
| 174 | [x] | `/operationLog/enhanced` | OperationLogEnhanced | 操作日志(增强) | 是 | `views/operationLog/Enhanced.vue` | `src/router/modules/operationLog.js` | - |
| 175 | [x] | `/ops-overview` | OpsOverview | 运维总览 | - | `views/OpsOverview/Index.vue` | `src/router/modules/ops.js` | - |
| 176 | [x] | `/sales-cockpit` | SalesCockpitIndex | AI 销冠驾驶舱 | - | `views/SalesCockpit/Index.vue` | `src/router/modules/ops.js` | - |
| 177 | [x] | `/dashboard/drafts` | OrderDraftList | 订单草稿 | 是 | `views/orderDraft/List.vue` | `src/router/modules/orderDraft.js` | - |
| 178 | [x] | `/dashboard/drafts/:id` | OrderDraftDetail | 草稿详情 | 是 | `views/orderDraft/Detail.vue` | `src/router/modules/orderDraft.js` | - |
| 179 | [x] | `/system/permissions` | PermissionPanel | permission.title | 是 | `views/system/PermissionPanel.vue` | `src/router/modules/permission.js` | - |
| 180 | [x] | `/persona/list` | PersonaList | 销冠画像 | 是 | `views/persona/List.vue` | `src/router/modules/persona.js` | - |
| 181 | [x] | `/platformAccount/list` | PlatformAccountList | 平台账号 | 是 | `views/platformAccount/List.vue` | `src/router/modules/platformAccount.js` | - |
| 182 | [x] | `/qq` | QQBot | QQ 机器人 | - | `views/qq/account.vue` | `src/router/modules/qq.js` | - |
| 183 | [x] | `/qq/account` | QQAccount | 机器人账号 | - | `views/qq/account.vue` | `src/router/modules/qq.js` | - |
| 184 | [x] | `/system/rag-product-config` | RagProductConfigIndex | RAG 主配置 | 是 | `views/RagProductConfig/index.vue` | `src/router/modules/ragProductConfig.js` | - |
| 185 | [x] | `/system/rag-account` | RagProductAccountConfig | RAG 账号配置 | 是 | `views/RagProductConfig/AccountConfig.vue` | `src/router/modules/ragProductConfig.js` | - |
| 186 | [x] | `/system/rag-product` | RagProductManagement | RAG 产品管理 | 是 | `views/RagProductConfig/RagProductManagement.vue` | `src/router/modules/ragProductConfig.js` | - |
| 187 | [x] | `/reachPipeline/list` | ReachPipelineList | 触达Pipeline | - | `views/reachPipeline/List.vue` | `src/router/modules/reachPipeline.js` | - |
| 188 | [x] | `/reachPipeline/editor/:id?` | ReachPipelineEditor | Pipeline 编辑 | 是 | `views/reachPipeline/Editor.vue` | `src/router/modules/reachPipeline.js` | - |
| 189 | [x] | `/reachPipeline/deferred-outbound` | ReachDeferredOutbound | 延迟出站管理 | 是 | `views/reachPipeline/DeferredOutbound.vue` | `src/router/modules/reachPipeline.js` | - |
| 190 | [x] | `/system/roles` | RoleList | role.title | 是 | `views/system/RoleList.vue` | `src/router/modules/role.js` | - |
| 191 | [x] | `/sales-workbench` | SalesWorkbenchIndex | 销售工作台 | 是 | `views/salesWorkbench/Index.vue` | `src/router/modules/salesWorkbench.js` | - |
| 192 | [x] | `/scriptTemplate/list` | ScriptTemplateList | 话术库 | 是 | `views/scriptTemplate/List.vue` | `src/router/modules/scriptTemplate.js` | - |
| 193 | [x] | `/scriptTemplate/ab-stats` | ScriptTemplateAbStats | 话术AB测试 | 是 | `views/scriptTemplate/AbStats.vue` | `src/router/modules/scriptTemplate.js` | - |
| 194 | [x] | `/securityAudit/list` | SecurityAuditList | 安全审计 | 是 | `views/securityAudit/List.vue` | `src/router/modules/securityAudit.js` | - |
| 195 | [x] | `/shortLink` | ShortLink | 短链管理 | - | `views/shortLink/List.vue` | `src/router/modules/shortLink.js` | - |
| 196 | [x] | `/shortLink/stats` | ShortLinkStats | 短链统计 | - | `views/shortLink/Stats.vue` | `src/router/modules/shortLink.js` | - |
| 197 | [x] | `/sms/list` | SmsList | 短信列表 | - | `views/sms/List.vue` | `src/router/modules/sms.js` | - |
| 198 | [x] | `/sms/drafts` | SmsDrafts | 短信草稿 | - | `views/sms/Drafts.vue` | `src/router/modules/sms.js` | - |
| 199 | [x] | `/sms/jobs` | SmsJobs | 短信任务 | - | `views/sms/Jobs.vue` | `src/router/modules/sms.js` | - |
| 200 | [x] | `/sms/config` | SmsConfig | 短信配置 | - | `views/sms/Config.vue` | `src/router/modules/sms.js` | - |
| 201 | [x] | `/sopAgent/list` | SopAgentList | 销冠 SOP 智能体 | 是 | `views/sopAgent/List.vue` | `src/router/modules/sopAgent.js` | - |
| 202 | [x] | `/sop-template` | SOPTemplate | SOP 模板库 | - | `views/sopTemplate/Index.vue` | `src/router/modules/sopTemplateKb.js` | - |
| 203 | [x] | `/sop-template/list` | SOPTemplateList | SOP 模板列表 | - | `views/sopTemplate/List.vue` | `src/router/modules/sopTemplateKb.js` | - |
| 204 | [x] | `/sop-template/market` | SOPTemplateMarket | SOP 模板市场 | - | `views/sopTemplate/Market.vue` | `src/router/modules/sopTemplateKb.js` | - |
| 205 | [x] | `/sop-template/editor` | SOPTemplateEditor | SOP 模板编辑 | - | `views/sopTemplate/Editor.vue` | `src/router/modules/sopTemplateKb.js` | - |
| 206 | [x] | `/sop-template/editor/:id` | SOPTemplateEditorEdit | SOP 模板编辑 | - | `views/sopTemplate/Editor.vue` | `src/router/modules/sopTemplateKb.js` | - |
| 207 | [x] | `/system/config` | SystemConfig | 站点设置 | 是 | `views/system/Config.vue` | `src/router/modules/system.js` | - |
| 208 | [x] | `/system/obs-config` | SystemObsConfig | 存储配置 | 是 | `views/system/ObsConfig.vue` | `src/router/modules/system.js` | - |
| 209 | [x] | `/system/material-library` | SystemMaterialLibrary | 素材库 | 是 | `views/system/MaterialLibrary.vue` | `src/router/modules/system.js` | - |
| 210 | [x] | `/system/monitor` | SystemMonitor | 监控 | 是 | `views/system/Monitor.vue` | `src/router/modules/system.js` | - |
| 211 | [x] | `/system/trace` | TraceMonitor | 链路追踪 | 是 | `views/system/TraceMonitor.vue` | `src/router/modules/system.js` | - |
| 212 | [x] | `/system/guide` | SystemGuide | 使用引导 | 是 | `views/system/Guide.vue` | `src/router/modules/system.js` | - |
| 213 | [x] | `/system/rag-overview` | RagOverview | RAG概览 | 是 | `views/system/RagOverview.vue` | `src/router/modules/system.js` | - |
| 214 | [x] | `/system/automation-hub` | AutomationHub | 自动化中心 | 是 | `views/system/AutomationHub.vue` | `src/router/modules/system.js` | - |
| 215 | [x] | `/system/config-params` | ConfigParams | 动态阈值参数 | 是 | `views/system/ConfigParams.vue` | `src/router/modules/system.js` | - |
| 216 | [x] | `/system/dictionaries` | SystemDictionaries | 业务字典 | 是 | `views/system/DictionaryManager.vue` | `src/router/modules/system.js` | - |
| 217 | [x] | `/system/feature-flags` | SystemFeatureFlags | 功能开关 | 是 | `views/system/FeatureFlags.vue` | `src/router/modules/system.js` | - |
| 218 | [x] | `/system/ltc-config` | LTCConfig | LTC 运营开关 | 是 | `views/system/LTCConfig.vue` | `src/router/modules/system.js` | - |
| 219 | [x] | `/system/users` | SystemUserList | systemUser.title | 是 | `views/system/UserList.vue` | `src/router/modules/systemUser.js` | - |
| 220 | [x] | `/tagSegmentation/list` | TagSegmentationList | 标签分层 | 是 | `views/tagSegmentation/List.vue` | `src/router/modules/tagSegmentation.js` | - |
| 221 | [x] | `/telegram` | Telegram | TG 机器人 | - | `views/telegram/account.vue` | `src/router/modules/telegram.js` | - |
| 222 | [x] | `/telegram/account` | TelegramAccount | 机器人账号 | - | `views/telegram/account.vue` | `src/router/modules/telegram.js` | - |
| 223 | [x] | `/telegram/gate` | TelegramGate | 入群管控 | - | `views/telegram/gate.vue` | `src/router/modules/telegram.js` | - |
| 224 | [x] | `/template-market` | TemplateMarket | 模板/技能市场 | 否 | `views/templateMarket/Market.vue` | `src/router/modules/templateMarket.js` | - |
| 225 | [x] | `/template-market/my` | TemplateMarketMy | 我的发布/订单 | 是 | `views/templateMarket/MyWorkspace.vue` | `src/router/modules/templateMarket.js` | - |
| 226 | [x] | `/template-market/review` | TemplateMarketReview | 发布审核 | 是 | `views/templateMarket/ReviewConsole.vue` | `src/router/modules/templateMarket.js` | - |
| 227 | [x] | `/tiktok/list` | TikTokList | TikTok卡片 | 是 | `views/tiktokCard/List.vue` | `src/router/modules/tiktok.js` | - |
| 228 | [x] | `/tiktok/stats` | TikTokStats | TikTok统计 | 是 | `views/tiktokCard/Stats.vue` | `src/router/modules/tiktok.js` | - |
| 229 | [x] | `/tiktok-card-stats/:id` | TikTokCardStats | TikTok卡片统计 | 是 | `views/tiktokCard/CardStats.vue` | `src/router/modules/tiktok.js` | - |
| 230 | [x] | `/tiktok` | TikTok | TikTok | - | `-` | `src/router/modules/tiktok.js` | - |
| 231 | [x] | `/confidence/panel` | ConfidencePanel | 置信度运营 | 是 | `views/confidence/Panel.vue` | `src/router/modules/tuning.js` | - |
| 232 | [x] | `/humanize/panel` | HumanizePanel | 拟人度评估 | 是 | `views/humanize/Panel.vue` | `src/router/modules/tuning.js` | - |
| 233 | [x] | `/feedbackLoop/panel` | FeedbackLoopPanel | 反馈学习闭环 | 是 | `views/feedbackLoop/Panel.vue` | `src/router/modules/tuning.js` | - |
| 234 | [x] | `/unifiedMessage/list` | UnifiedMessageList | 统一消息 | - | `views/unifiedMessage/List.vue` | `src/router/modules/unifiedMessage.js` | - |
| 235 | [x] | `/unknown-questions` | UnknownQuestionList | 未知问题运营 | 是 | `views/unknownQuestion/List.vue` | `src/router/modules/unknownQuestion.js` | - |
| 236 | [x] | `/userSegment/list` | UserSegmentList | 用户分层 RFM | 是 | `views/userSegment/List.vue` | `src/router/modules/userSegment.js` | - |
| 237 | [x] | `/userSegment/rfm-matrix` | RfmMatrix | RFM 矩阵 | 是 | `views/userSegment/RfmMatrix.vue` | `src/router/modules/userSegment.js` | - |
| 238 | [x] | `/userSegment/builder` | UserSegmentBuilder | 分层规则构建 | 是 | `views/userSegment/Builder.vue` | `src/router/modules/userSegment.js` | - |
| 239 | [x] | `/wecomAccount/list` | WecomAccountList | 企微账号管理 | - | `views/wecomAccount/List.vue` | `src/router/modules/wecomAccount.js` | - |
| 240 | [x] | `/wecomAccount/data` | WecomAccountData | 企微数据看板 | - | `views/wecomAccount/Data.vue` | `src/router/modules/wecomAccount.js` | - |
| 241 | [x] | `/whatsapp` | Whatsapp | WhatsApp 社群 | - | `views/whatsapp/WhatsappAccount.vue` | `src/router/modules/whatsapp.js` | - |
| 242 | [x] | `/whatsapp/account` | WhatsappAccount | 账号管理 | - | `views/whatsapp/WhatsappAccount.vue` | `src/router/modules/whatsapp.js` | - |
| 243 | [x] | `/whatsapp/drafts` | WhatsappDrafts | 草稿箱 | - | `views/whatsapp/WhatsappDrafts.vue` | `src/router/modules/whatsapp.js` | - |
| 244 | [x] | `/whatsapp/jobs` | WhatsappJobs | 群发 | - | `views/whatsapp/WhatsappJobs.vue` | `src/router/modules/whatsapp.js` | - |
| 245 | [x] | `/whatsapp/lead-group-selection` | LeadGroupSelection | 从线索库选择群体 | 是 | `views/whatsappBot/LeadGroupSelection.vue` | `src/router/modules/whatsapp.js` | - |
| 246 | [x] | `/whatsapp/group-messaging` | BulkMessaging | 批量消息发送 | 是 | `views/whatsappBot/BulkMessaging.vue` | `src/router/modules/whatsapp.js` | - |
| 247 | [x] | `/whatsapp/bulk-matrix` | WhatsappBulkMatrix | 群发矩阵 | 是 | `views/whatsapp/BulkMatrix.vue` | `src/router/modules/whatsapp.js` | - |
| 248 | [x] | `/whatsapp-cloud` | WhatsappCloud | WhatsApp Cloud | - | `views/whatsappCloud/WhatsappCloudAccount.vue` | `src/router/modules/whatsappCloud.js` | - |
| 249 | [x] | `/workflow-orchestrator/list` | WorkflowOrchestratorList | 工作流编排 | 是 | `views/workflowOrchestrator/List.vue` | `src/router/modules/workflowOrchestrator.js` | - |
| 250 | [x] | `/workflow-orchestrator/editor/:workflow_id` | WorkflowOrchestratorEditor | 工作流编辑 | 是 | `views/workflowOrchestrator/Editor.vue` | `src/router/modules/workflowOrchestrator.js` | - |
| 251 | [x] | `/workflow-orchestrator/execution/:id` | WorkflowOrchestratorExecution | 执行详情 | 是 | `views/workflowOrchestrator/Execution.vue` | `src/router/modules/workflowOrchestrator.js` | - |
| 252 | [x] | `/xianyuCard` | XianyuCard | 闲鱼卡片 | - | `views/xianyuCard/List.vue` | `src/router/modules/xianyuCard.js` | - |
| 253 | [x] | `/xianyu/stats` | XianyuStats | 闲鱼卡片统计 | - | `views/xianyuCard/Stats.vue` | `src/router/modules/xianyuCard.js` | - |
| 254 | [x] | `/xianyu-card-stats/:id` | XianyuCardStats | 闲鱼卡片详情统计 | - | `views/xianyuCard/CardStats.vue` | `src/router/modules/xianyuCard.js` | - |
| 255 | [x] | `/xiaohongshuCard` | XiaohongshuCard | 小红书卡片 | - | `views/xiaohongshuCard/List.vue` | `src/router/modules/xiaohongshuCard.js` | - |
| 256 | [x] | `/xiaohongshu/stats` | XiaohongshuStats | 小红书卡片统计 | - | `views/xiaohongshuCard/Stats.vue` | `src/router/modules/xiaohongshuCard.js` | - |
| 257 | [x] | `/xiaohongshu-card-stats/:id` | XiaohongshuCardStats | 小红书卡片详情统计 | - | `views/xiaohongshuCard/CardStats.vue` | `src/router/modules/xiaohongshuCard.js` | - |

## 进度

- 已验证无错误 `[x]`：**253** 条
- 已验证·设计内拒绝 `[~]`：**4** 条（`/asset-market/my-assets`、`/bridge/extension`、`/followups/today`、`/followups/:id`）
- 待点验 `[ ]`：**0** 条
- 已点出缺陷 `[!]`：**0** 条（逐页点出的缺陷已当轮修复并回归，详见 commit message）
- 任务六已写页面导览 `G`：**0** 条
