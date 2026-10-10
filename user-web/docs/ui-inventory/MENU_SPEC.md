# HiveMTK 用户端 · 前端菜单页面规格清单

> 生成日期：2026-07-22  ｜  技术栈：Vue 3 + Vue Router + Element Plus + Vite
>
> ⚠️ **本文档是 2026-07-22 的人工快照，不是运行期事实源。** 侧边栏已改为从路由表自动生成：
> 页面归属由 `src/router/modules/*.js` 的 `meta.menuSection` / `menuParents` / `menuOrder` / `menuKey`
> 决定，分区骨架见 `src/layout/menuRegistry.js`，构建器见 `src/layout/menu.js`；
> `Layout.vue` 里不再有硬编码 `topMenus` 菜单树。校验用 `npm run menu:check`
> （`user-web/scripts/check-menu-autogen.cjs`）。
>
> 正文中的路径、图标与角色标注均为当时快照，可能与现状不符 —— 以路由 meta 为准。

## 一、侧边栏菜单结构

- **仪表盘** (Odometer)
- **工作台** (Monitor)
  - **统一收件箱** (Inbox) `/unifiedInbox/list` → `@/views/unifiedInbox/List.vue`
  - **客服会话** (Service) `/customerSession/list` → `@/views/customerSession/List.vue`
  - **统一消息** (Message) `/unifiedMessage/list` → `@/views/unifiedMessage/List.vue`
  - **消息中台** (MessageBox) `/messageHub/list` → `@/views/messageHub/List.vue`
  - **企微账号管理** (Connection) `/wecomAccount/list` → `@/views/wecomAccount/List.vue`
- **客户** (UserFilled)
  - **客户360** (UserFilled) `/customer360/list` → `@/views/customer360/List.vue`
  - **客户事件** (Bell) `/customerEvent/list` → `@/views/customerEvent/List.vue`
  - **线索列表** (Document) `/clue/list` → `@/views/clue/List.vue`
  - **线索统计** (DataAnalysis) `/clue/statistics` → `@/views/clue/Statistics.vue`
  - **标签分层** (PriceTag) `/tagSegmentation/list` → `@/views/tagSegmentation/List.vue`
  - **用户分层RFM** (PieChart) `/userSegment/list` → `@/views/userSegment/List.vue`
  - **客服渠道** (Connection) `/chatChannel/list` → `@/views/chatChannel/List.vue`
  - **渠道安装引导** (Guide) `/chatChannel/install-guide` `[角色:merchant]`
- **AI 智能体** (Cpu)
  - **智能体列表** (Cpu) `/aiAgent/list` → `@/views/aiAgent/List.vue`
  - **创建智能体** (Plus) `/aiAgent/create` → `@/views/aiAgent/Edit.vue`
  - **对话记忆** (ChatDotRound) `/dialogueMemory/list` → `@/views/dialogueMemory/List.vue`
  - **意图识别** (Aim) `/intentRecognition/list` → `@/views/intentRecognition/List.vue`
  - **异议处理** (ChatLineRound) `/objection/list` → `@/views/objection/List.vue`
  - **销冠画像** (UserFilled) `/persona/list` → `@/views/persona/List.vue`
  - **销冠SOP智能体** (Connection) `/sopAgent/list` → `@/views/sopAgent/List.vue`
  - **话术库** (ChatLineSquare) `/scriptTemplate/list` → `@/views/scriptTemplate/List.vue`
  - **LLM路由** (Cpu) `/llmRouting/list` → `@/views/llmRouting/List.vue`
  - **置信度运营** (TrendCharts) `/confidence/panel` → `@/views/confidence/Panel.vue`
  - **拟人度评估** (UserFilled) `/humanize/panel` → `@/views/humanize/Panel.vue`
  - **反馈学习闭环** (Connection) `/feedbackLoop/panel` → `@/views/feedbackLoop/Panel.vue`
  - **资产市场** (Shop) `/asset-market` → `@/views/assetMarket/Market.vue`
  - **我的资产** (FolderOpened) `/asset-market/my-assets` → `@/views/assetMarket/MyAssets.vue`
- **知识库** (Files)
  - **知识库管理** (Files) `/knowledge/management` → `@/views/KnowledgeWorkspace/KnowledgeManagement.vue`
  - **API Token** (Key) `/knowledge/tokens` → `@/views/KnowledgeWorkspace/ApiToken.vue`
  - **检索Playground** (Aim) `/knowledge/playground` → `@/views/KnowledgeWorkspace/Playground.vue`
  - **分段编辑** (Files) `/knowledge/chunks` → `@/views/KnowledgeWorkspace/ChunkManagement.vue`
  - **外部系统接入** (Connection) `/knowledge/external` → `@/views/KnowledgeWorkspace/ExternalImport.vue`
  - **OpenAPI集成** (Connection) `/knowledge/openapi` → `@/views/KnowledgeWorkspace/OpenAPIIntegration.vue`
  - **知识库统计** (DataAnalysis) `/knowledge/statistics` → `@/views/KnowledgeWorkspace/KnowledgeStatistics.vue`
  - **反馈管理** (ChatLineRound) `/knowledge/feedbacks` → `@/views/KnowledgeWorkspace/FeedbackList.vue`
  - **批量导入** (UploadFilled) `/knowledge/batch-import` → `@/views/KnowledgeWorkspace/BatchImport.vue`
  - **模板市场** (Grid) `/templateMarket/list` → `@/views/templateMarket/List.vue`
  - **RAG概览** (Monitor) `/system/rag-overview` → `@/views/system/RagOverview.vue`
  - **RAG主配置** (ChatLineRound) `/system/rag-product-config` → `@/views/RagProductConfig/index.vue`
  - **RAG产品管理** (Goods) `/system/rag-product` → `@/views/RagProductConfig/RagProductManagement.vue`
  - **RAG账号配置** (Key) `/system/rag-account` → `@/views/RagProductConfig/AccountConfig.vue`
- **触达** (Promotion)
  - **营销自动化** (SetUp) `/marketingFlow/list` → `@/views/marketingFlow/List.vue`
  - **触达Pipeline** (Promotion) `/reachPipeline/list` → `@/views/reachPipeline/List.vue`
  - **短信列表** (ChatDotSquare) `/sms/list` → `@/views/sms/List.vue`
  - **短信草稿** (Document) `/sms/drafts` → `@/views/sms/Drafts.vue`
  - **短信任务** (List) `/sms/jobs` → `@/views/sms/Jobs.vue`
  - **短信配置** (Setting) `/sms/config` → `@/views/sms/Config.vue`
  - **邮件列表** (ChatSquare) `/email` → `@/views/email/EmailList.vue`
  - **我的草稿** (Document) `/email/drafts` → `@/views/email/Drafts.vue`
  - **我的任务** (Document) `/email/jobs` → `@/views/email/Jobs.vue`
  - **邮件账号** (Setting) `/email/smtp` → `@/views/email/Smtp.vue`
  - **邮件代理** (Setting) `/email/info` → `@/views/email/Info.vue`
  - **使用引导** (Setting) `/email/guide` → `@/views/email/Guide.vue`
  - **TikTok** (VideoPlay) `/tiktok` → `@/views/tiktokCard/List.vue`
  - **TikTok卡片** (VideoPlay) `/tiktok/list`
  - **TikTok统计** (DataAnalysis) `/stats` → `@/views/tiktokCard/Stats.vue`
  - **TikTok卡片统计** (DataAnalysis) `/card-stats/:id` → `@/views/tiktokCard/CardStats.vue`
  - **TikTok自动回复** (ChatDotRound) `/auto-reply` → `@/views/tiktokCard/AutoReply.vue`
  - **抖音卡片** (VideoPlay) `/douyinCard` → `@/views/douyinCard/List.vue`
  - **抖音卡片统计** (DataAnalysis) `/douyin/stats` → `@/views/douyinCard/Stats.vue`
  - **抖音卡片详情统计** (DataAnalysis) `/douyin-card-stats/:id` → `@/views/douyinCard/CardStats.vue`
  - **抖音自动回复** (ChatDotRound) `/douyin/auto-reply` → `@/views/douyinCard/AutoReply.vue`
  - **快手卡片** (ChatDotRound) `/kuaishouCard` → `@/views/kuaishouCard/List.vue`
  - **快手卡片统计** (DataAnalysis) `/kuaishou/stats` → `@/views/kuaishouCard/Stats.vue`
  - **快手卡片详情统计** (DataAnalysis) `/kuaishou-card-stats/:id` → `@/views/kuaishouCard/CardStats.vue`
  - **快手自动回复** (ChatDotRound) `/kuaishou/auto-reply` → `@/views/kuaishouCard/AutoReply.vue`
  - **小红书卡片** (Picture) `/xiaohongshuCard` → `@/views/xiaohongshuCard/List.vue`
  - **小红书卡片统计** (DataAnalysis) `/xiaohongshu/stats` → `@/views/xiaohongshuCard/Stats.vue`
  - **小红书卡片详情统计** (DataAnalysis) `/xiaohongshu-card-stats/:id` → `@/views/xiaohongshuCard/CardStats.vue`
  - **小红书自动回复** (ChatDotRound) `/xiaohongshu/auto-reply` → `@/views/xiaohongshuCard/AutoReply.vue`
  - **闲鱼卡片** (ShoppingBag) `/xianyuCard` → `@/views/xianyuCard/List.vue`
  - **闲鱼卡片统计** (DataAnalysis) `/xianyu/stats` → `@/views/xianyuCard/Stats.vue`
  - **闲鱼卡片详情统计** (DataAnalysis) `/xianyu-card-stats/:id` → `@/views/xianyuCard/CardStats.vue`
  - **闲鱼自动回复** (ChatDotRound) `/xianyu/auto-reply` → `@/views/xianyuCard/AutoReply.vue`
  - **批量操作** (Operation) `/batchOperation/list` → `@/views/batchOperation/List.vue`
  - **活码管理** `/livecode` → `@/views/livecode/LiveCodeManagement.vue`
  - **短链管理** (Link) `/shortLink` → `@/views/shortLink/List.vue`
  - **短链统计** (DataAnalysis) `/shortLink/stats` → `@/views/shortLink/Stats.vue`
- **社媒运营** (UserFilled)
  - **社群管理** (UserFilled) `/community/list` → `@/views/community/List.vue`
  - **TG机器人** (ChatDotRound) `/telegram` → `@/views/telegram/account.vue`
  - **机器人账号** (Cpu) `/telegram/account` → `@/views/telegram/account.vue`
  - **飞书** (ChatLineRound) `/feishu` → `@/views/feishu/FeishuAccount.vue`
  - **飞书账号** (Cpu) `/feishu/account` → `@/views/feishu/FeishuAccount.vue`
  - **WhatsApp社群** (ChatDotRound) `/whatsapp` → `@/views/whatsapp/WhatsappAccount.vue`
  - **账号管理** (Cpu) `/whatsapp/account` → `@/views/whatsapp/WhatsappAccount.vue`
  - **草稿箱** (Document) `/whatsapp/drafts` → `@/views/whatsapp/WhatsappDrafts.vue`
  - **批量消息发送** (ChatLineRound) `/whatsapp/group-messaging` → `@/views/whatsappBot/BulkMessaging.vue`
  - **群发** (Promotion) `/whatsapp/jobs` → `@/views/whatsapp/WhatsappJobs.vue`
  - **从线索库选择群体** (User) `/whatsapp/lead-group-selection` → `@/views/whatsappBot/LeadGroupSelection.vue`
  - **域名池管理** (Link) `/domainPool` → `@/views/domainPool/List.vue`
  - **坐席状态** (Headset) `/customerService/agentStatus` → `@/views/customerService/AgentStatus.vue`
  - **AI建议** (MagicStick) `/customerService/aiSuggestion` → `@/views/customerService/AISuggestion.vue`
  - **快捷回复** (ChatLineSquare) `/customerService/quickReply` → `@/views/customerService/QuickReply.vue`
  - **会话标签** (CollectionTag) `/customerService/sessionTag` → `@/views/customerService/SessionTag.vue`
- **分析洞察** (TrendCharts)
  - **AI产能分析** (DataAnalysis) `/aiProductivity/list` → `@/views/aiProductivity/List.vue`
  - **转化漏斗** (Filter) `/conversionFunnel/list` → `@/views/conversionFunnel/List.vue`
  - **客户旅程大屏** (TrendCharts) `/customerJourney/dashboard` → `@/views/customerJourney/Dashboard.vue`
  - **数据大屏** (DataBoard) `/dashboardScreen/list` → `@/views/dashboardScreen/List.vue`
  - **自定义报表** (Document) `/customReport/list` → `@/views/customReport/List.vue`
  - **A/B实验** (DataLine) `/abExperiment/list` → `@/views/abExperiment/List.vue`
  - **流失预警** (Warning) `/churnPrediction/list` → `@/views/churnPrediction/List.vue`
- **系统管理** (Tools) `[角色:merchant]`
  - **平台账号** (Platform) `/platformAccount/list` → `@/views/platformAccount/List.vue`
  - **团队成员** (UserFilled) `/teamUser/list` → `@/views/teamUser/List.vue`
  - **角色权限** (Lock) `/teamUser/role` → `@/views/teamUser/Role.vue`
  - **站点设置** (Tools) `/system/config` → `@/views/system/Config.vue`
  - **素材库** (Picture) `/system/material-library` → `@/views/system/MaterialLibrary.vue`
  - **监控** (Cpu) `/system/monitor` → `@/views/system/Monitor.vue`
  - **存储配置** (Cloud) `/system/obs-config` → `@/views/system/ObsConfig.vue`
  - **第三方对接** (Connection) `/integration/list` → `@/views/integration/List.vue`
  - **操作日志** (Tickets) `/operationLog/list` → `@/views/operationLog/List.vue`
  - **安全审计** (Shield) `/securityAudit/list` → `@/views/securityAudit/List.vue`
  - **备份恢复** (FolderOpened) `/backup/list` → `@/views/backup/List.vue`
  - **使用引导** (Document) `/system/guide` → `@/views/system/Guide.vue`

## 二、完整页面清单（全部路由）

| 路径 | 页面标题 | 视图组件 | 分组 | 图标 | 隐藏 | 重定向 |
| --- | --- | --- | --- | --- | --- | --- |
| `/` | 个人资料 | `@/views/Profile.vue` | — | — |  | /unifiedInbox/list |
| `/:pathMatch(.*)*` | 页面不存在 | `@/views/NotFound.vue` | — | — |  | — |
| `/chat/embed` | — | `(redirect)` | — | — |  | /chat/embed/default |
| `/chat/embed/` | — | `(redirect)` | — | — |  | /chat/embed/default |
| `/chat/embed/:channel_ref` | 在线客服 | `@/views/chat/embed/Index.vue` | — | — |  | /chat/embed/default |
| `/chat/embed/default` | 在线客服 | `@/views/chat/embed/Index.vue` | — | — |  | /chat/embed/default |
| `/douyin-card-stats/:id` | 抖音卡片详情统计 | `@/views/douyinCard/CardStats.vue` | community | DataAnalysis |  | — |
| `/douyin/auto-reply` | 抖音自动回复 | `@/views/douyinCard/AutoReply.vue` | community | ChatDotRound |  | — |
| `/douyin/stats` | 抖音卡片统计 | `@/views/douyinCard/Stats.vue` | community | DataAnalysis |  | — |
| `/douyinCard` | 抖音卡片 | `@/views/douyinCard/List.vue` | community | VideoPlay |  | — |
| `/kuaishou-card-stats/:id` | 快手卡片详情统计 | `@/views/kuaishouCard/CardStats.vue` | community | DataAnalysis |  | — |
| `/kuaishou/auto-reply` | 快手自动回复 | `@/views/kuaishouCard/AutoReply.vue` | community | ChatDotRound |  | — |
| `/kuaishou/stats` | 快手卡片统计 | `@/views/kuaishouCard/Stats.vue` | community | DataAnalysis |  | — |
| `/kuaishouCard` | 快手卡片 | `@/views/kuaishouCard/List.vue` | community | ChatDotRound |  | — |
| `/livecode` | 活码管理 | `@/views/livecode/LiveCodeManagement.vue` | — | — |  | — |
| `/login` | 登录 | `@/views/Login.vue` | — | — |  | /chat/embed/default |
| `/oneid` | 页面不存在 | `@/views/NotFound.vue` | — | — |  | /oneid/list |
| `/setup` | 系统初始化 | `@/views/setup/InitSetup.vue` | — | — |  | /chat/embed/default |
| `/telegram/group` | 页面不存在 | `@/views/NotFound.vue` | — | — |  | /telegram/account |
| `/xianyu-card-stats/:id` | 闲鱼卡片详情统计 | `@/views/xianyuCard/CardStats.vue` | community | DataAnalysis |  | — |
| `/xianyu/auto-reply` | 闲鱼自动回复 | `@/views/xianyuCard/AutoReply.vue` | community | ChatDotRound |  | — |
| `/xianyu/stats` | 闲鱼卡片统计 | `@/views/xianyuCard/Stats.vue` | community | DataAnalysis |  | — |
| `/xianyuCard` | 闲鱼卡片 | `@/views/xianyuCard/List.vue` | community | ShoppingBag |  | — |
| `/xiaohongshu-card-stats/:id` | 小红书卡片详情统计 | `@/views/xiaohongshuCard/CardStats.vue` | community | DataAnalysis |  | — |
| `/xiaohongshu/auto-reply` | 小红书自动回复 | `@/views/xiaohongshuCard/AutoReply.vue` | community | ChatDotRound |  | — |
| `/xiaohongshu/stats` | 小红书卡片统计 | `@/views/xiaohongshuCard/Stats.vue` | community | DataAnalysis |  | — |
| `/xiaohongshuCard` | 小红书卡片 | `@/views/xiaohongshuCard/List.vue` | community | Picture |  | — |
| `abExperiment/list` | A/B 实验 | `@/views/abExperiment/List.vue` | dataAnalysis | DataLine |  | — |
| `aiAgent` | 智能体 | `@/views/aiAgent/List.vue` | aiAgent | Cpu |  | — |
| `aiAgent/create` | 创建智能体 | `@/views/aiAgent/Edit.vue` | aiAgent | Plus |  | — |
| `aiAgent/edit/:id` | 编辑智能体 | `@/views/aiAgent/Edit.vue` | aiAgent | Edit |  | — |
| `aiAgent/list` | 智能体列表 | `@/views/aiAgent/List.vue` | aiAgent | List |  | — |
| `aiContent/list` | AI 内容创作 | `@/views/aiContent/List.vue` | knowledge | MagicStick |  | — |
| `aiProductivity/list` | AI产能分析 | `@/views/aiProductivity/List.vue` | analytics | DataAnalysis |  | — |
| `asset-market` | 资产市场 | `@/views/assetMarket/Market.vue` | aiAgent | Shop |  | — |
| `asset-market/detail/:id` | 资产详情 | `@/views/assetMarket/Detail.vue` | aiAgent | — | 是 | — |
| `asset-market/my-assets` | 我的资产 | `@/views/assetMarket/MyAssets.vue` | aiAgent | FolderOpened |  | — |
| `asset-market/sync-log` | 同步日志 | `@/views/assetMarket/SyncLog.vue` | aiAgent | — | 是 | — |
| `auto-reply` | TikTok自动回复 | `@/views/tiktokCard/AutoReply.vue` | reach | ChatDotRound |  | — |
| `backup/list` | 备份恢复 | `@/views/backup/List.vue` | system | FolderOpened |  | — |
| `batchOperation/list` | 批量操作 | `@/views/batchOperation/List.vue` | reach | Operation |  | — |
| `card-stats/:id` | TikTok卡片统计 | `@/views/tiktokCard/CardStats.vue` | reach | DataAnalysis |  | — |
| `chatChannel/create` | 新建客服渠道 | `@/views/chatChannel/Create.vue` | customer | Plus |  | — |
| `chatChannel/edit/:id` | 编辑客服渠道 | `@/views/chatChannel/Edit.vue` | customer | Edit |  | — |
| `chatChannel/install-guide/:id?` | Widget 安装引导 | `@/views/chatChannel/InstallGuide.vue` | customer | Guide |  | — |
| `chatChannel/list` | 客服渠道 | `@/views/chatChannel/List.vue` | customer | Connection |  | — |
| `churnPrediction/list` | 流失预警 | `@/views/churnPrediction/List.vue` | dataAnalysis | Warning |  | — |
| `clue/list` | 线索列表 | `@/views/clue/List.vue` | customer | Document |  | — |
| `clue/statistics` | 线索统计 | `@/views/clue/Statistics.vue` | customer | DataAnalysis |  | — |
| `community/list` | 社群管理 | `@/views/community/List.vue` | community | UserFilled |  | — |
| `confidence/panel` | 置信度运营 | `@/views/confidence/Panel.vue` | aiAgent | TrendCharts |  | — |
| `conversionFunnel/list` | 转化漏斗 | `@/views/conversionFunnel/List.vue` | analytics | Filter |  | — |
| `customer360/list` | 客户 360 | `@/views/customer360/List.vue` | customer | UserFilled |  | — |
| `customerEvent/list` | 客户事件 | `@/views/customerEvent/List.vue` | customer | Bell |  | — |
| `customerJourney/dashboard` | 客户旅程大屏 | `@/views/customerJourney/Dashboard.vue` | analytics | TrendCharts |  | — |
| `customerService/agentStatus` | 坐席状态 | `@/views/customerService/AgentStatus.vue` | community | Headset |  | — |
| `customerService/aiSuggestion` | AI 建议 | `@/views/customerService/AISuggestion.vue` | community | MagicStick |  | — |
| `customerService/quickReply` | 快捷回复 | `@/views/customerService/QuickReply.vue` | community | ChatLineSquare |  | — |
| `customerService/sessionTag` | 会话标签 | `@/views/customerService/SessionTag.vue` | community | CollectionTag |  | — |
| `customerSession/list` | 客服会话 | `@/views/customerSession/List.vue` | customer | Service |  | — |
| `customReport/list` | 自定义报表 | `@/views/customReport/List.vue` | dataAnalysis | Document |  | — |
| `dashboardScreen/list` | 数据大屏 | `@/views/dashboardScreen/List.vue` | dataAnalysis | DataBoard |  | — |
| `dialogueMemory/list` | 对话记忆 | `@/views/dialogueMemory/List.vue` | aiAgent | ChatDotRound |  | — |
| `domainPool` | 域名池管理 | `@/views/domainPool/List.vue` | system | Link |  | — |
| `email` | 邮件列表 | `@/views/email/EmailList.vue` | reach | ChatSquare |  | — |
| `email/drafts` | 我的草稿 | `@/views/email/Drafts.vue` | reach | Document |  | — |
| `email/guide` | 使用引导 | `@/views/email/Guide.vue` | reach | Setting |  | — |
| `email/info` | 邮件代理 | `@/views/email/Info.vue` | reach | Setting |  | — |
| `email/jobs` | 我的任务 | `@/views/email/Jobs.vue` | reach | Document |  | — |
| `email/smtp` | 邮件账号 | `@/views/email/Smtp.vue` | reach | Setting |  | — |
| `feedbackLoop/panel` | 反馈学习闭环 | `@/views/feedbackLoop/Panel.vue` | aiAgent | Connection |  | — |
| `feishu` | 飞书 | `@/views/feishu/FeishuAccount.vue` | community | ChatLineRound |  | — |
| `feishu/account` | 飞书账号 | `@/views/feishu/FeishuAccount.vue` | community | Cpu |  | — |
| `humanize/panel` | 拟人度评估 | `@/views/humanize/Panel.vue` | aiAgent | UserFilled |  | — |
| `integration/list` | 第三方对接 | `@/views/integration/List.vue` | system | Connection |  | — |
| `intentRecognition/list` | 意图识别 | `@/views/intentRecognition/List.vue` | aiAgent | Aim |  | — |
| `knowledge/batch-import` | 批量导入 | `@/views/KnowledgeWorkspace/BatchImport.vue` | knowledge | UploadFilled |  | — |
| `knowledge/chunks` | 分段编辑 | `@/views/KnowledgeWorkspace/ChunkManagement.vue` | knowledge | Files |  | — |
| `knowledge/external` | 外部系统接入 | `@/views/KnowledgeWorkspace/ExternalImport.vue` | knowledge | Connection |  | — |
| `knowledge/feedbacks` | 反馈管理 | `@/views/KnowledgeWorkspace/FeedbackList.vue` | knowledge | ChatLineRound |  | — |
| `knowledge/management` | 知识库管理 | `@/views/KnowledgeWorkspace/KnowledgeManagement.vue` | knowledge | Files |  | — |
| `knowledge/openapi` | OpenAPI 集成 | `@/views/KnowledgeWorkspace/OpenAPIIntegration.vue` | knowledge | Connection |  | — |
| `knowledge/playground` | 检索 Playground | `@/views/KnowledgeWorkspace/Playground.vue` | knowledge | Aim |  | — |
| `knowledge/statistics` | 知识库统计 | `@/views/KnowledgeWorkspace/KnowledgeStatistics.vue` | knowledge | DataAnalysis |  | — |
| `knowledge/tokens` | API Token | `@/views/KnowledgeWorkspace/ApiToken.vue` | knowledge | Key |  | — |
| `list` | TikTok卡片 | `@/views/tiktokCard/List.vue` | reach | VideoPlay |  | — |
| `llmRouting/list` | LLM路由 | `@/views/llmRouting/List.vue` | aiAgent | Cpu |  | — |
| `marketingFlow/list` | 营销自动化 | `@/views/marketingFlow/List.vue` | reach | SetUp |  | — |
| `messageHub/list` | 消息中台 | `@/views/messageHub/List.vue` | workspace | MessageBox |  | — |
| `notifications` | 通知中心 | `@/views/Notifications.vue` | — | — |  | /telegram/account |
| `objection/list` | 异议处理 | `@/views/objection/List.vue` | sales | ChatLineRound |  | — |
| `oneid/conflicts` | 身份冲突解决 | `@/views/oneid/Conflicts.vue` | — | Warning |  | — |
| `oneid/list` | OneID 列表 | `@/views/oneid/List.vue` | — | List |  | — |
| `operationLog/list` | 操作日志 | `@/views/operationLog/List.vue` | system | Tickets |  | — |
| `persona/list` | 销冠画像 | `@/views/persona/List.vue` | sales | UserFilled |  | — |
| `platformAccount/list` | 平台账号 | `@/views/platformAccount/List.vue` | system | Platform |  | — |
| `profile` | 个人资料 | `@/views/Profile.vue` | — | — |  | /telegram/account |
| `reachPipeline/list` | 触达Pipeline | `@/views/reachPipeline/List.vue` | reach | Promotion |  | — |
| `scriptTemplate/list` | 话术库 | `@/views/scriptTemplate/List.vue` | aiAgent | ChatLineSquare |  | — |
| `securityAudit/list` | 安全审计 | `@/views/securityAudit/List.vue` | system | Shield |  | — |
| `shortLink` | 短链管理 | `@/views/shortLink/List.vue` | community | Link |  | — |
| `shortLink/stats` | 短链统计 | `@/views/shortLink/Stats.vue` | community | DataAnalysis |  | — |
| `sms/config` | 短信配置 | `@/views/sms/Config.vue` | reach | Setting |  | — |
| `sms/drafts` | 短信草稿 | `@/views/sms/Drafts.vue` | reach | Document |  | — |
| `sms/jobs` | 短信任务 | `@/views/sms/Jobs.vue` | reach | List |  | — |
| `sms/list` | 短信列表 | `@/views/sms/List.vue` | reach | ChatDotSquare |  | — |
| `sopAgent/list` | 销冠 SOP 智能体 | `@/views/sopAgent/List.vue` | aiAgent | Connection |  | — |
| `stats` | TikTok统计 | `@/views/tiktokCard/Stats.vue` | reach | DataAnalysis |  | — |
| `system/config` | 站点设置 | `@/views/system/Config.vue` | system | Tools |  | — |
| `system/guide` | 使用引导 | `@/views/system/Guide.vue` | system | Document |  | — |
| `system/material-library` | 素材库 | `@/views/system/MaterialLibrary.vue` | system | Picture |  | — |
| `system/monitor` | 监控 | `@/views/system/Monitor.vue` | system | Cpu |  | — |
| `system/obs-config` | 存储配置 | `@/views/system/ObsConfig.vue` | system | Cloud |  | — |
| `system/rag-account` | RAG 账号配置 | `@/views/RagProductConfig/AccountConfig.vue` | knowledge | Key |  | — |
| `system/rag-overview` | RAG概览 | `@/views/system/RagOverview.vue` | knowledge | Monitor |  | — |
| `system/rag-product` | RAG 产品管理 | `@/views/RagProductConfig/RagProductManagement.vue` | knowledge | Goods |  | — |
| `system/rag-product-config` | RAG 主配置 | `@/views/RagProductConfig/index.vue` | knowledge | ChatLineRound |  | — |
| `tagSegmentation/list` | 标签分层 | `@/views/tagSegmentation/List.vue` | customer | PriceTag |  | — |
| `teamUser/list` | 团队成员 | `@/views/teamUser/List.vue` | system | UserFilled |  | — |
| `teamUser/role` | 角色权限 | `@/views/teamUser/Role.vue` | system | Lock |  | — |
| `telegram` | TG 机器人 | `@/views/telegram/account.vue` | community | ChatDotRound |  | — |
| `telegram/account` | 机器人账号 | `@/views/telegram/account.vue` | community | Cpu |  | — |
| `templateMarket/list` | 模板市场 | `@/views/templateMarket/List.vue` | knowledge | Grid |  | — |
| `tiktok` | TikTok | `@/views/tiktokCard/List.vue` | reach | VideoPlay |  | /tiktok/list |
| `unifiedInbox/list` | 统一收件箱 | `@/views/unifiedInbox/List.vue` | workspace | Inbox |  | — |
| `unifiedMessage/list` | 统一消息 | `@/views/unifiedMessage/List.vue` | customer | Message |  | — |
| `userSegment/list` | 用户分层 RFM | `@/views/userSegment/List.vue` | customer | PieChart |  | — |
| `wecomAccount/list` | 企微账号管理 | `@/views/wecomAccount/List.vue` | workspace | Connection |  | — |
| `whatsapp` | WhatsApp 社群 | `@/views/whatsapp/WhatsappAccount.vue` | community | ChatDotRound |  | — |
| `whatsapp/account` | 账号管理 | `@/views/whatsapp/WhatsappAccount.vue` | community | Cpu |  | — |
| `whatsapp/drafts` | 草稿箱 | `@/views/whatsapp/WhatsappDrafts.vue` | community | Document |  | — |
| `whatsapp/group-messaging` | 批量消息发送 | `@/views/whatsappBot/BulkMessaging.vue` | community | ChatLineRound |  | — |
| `whatsapp/jobs` | 群发 | `@/views/whatsapp/WhatsappJobs.vue` | community | Promotion |  | — |
| `whatsapp/lead-group-selection` | 从线索库选择群体 | `@/views/whatsappBot/LeadGroupSelection.vue` | community | User |  | — |

> 说明：上表含 137 条路由（含重定向页、404、嵌入客服页）。菜单中未直接列出的页面（如 `/profile` 个人资料、`/notifications` 通知中心、`/setup` 初始化、`/login` 登录、`/chat/embed/*` 在线客服嵌入页、`/oneid/*` 身份归一化、`/asset-market/detail/:id` 资产详情、`/asset-market/sync-log` 同步日志）为独立路由，不挂在主菜单下。

---

## 三、各页面内容规格（按菜单分组）

> 每页含：用途 / 主要区块 / 关键字段（表格列或表单）/ 关键操作 / 数据来源。字段与接口均取自各视图 `.vue` 组件真实实现。

### 分组 A · 工作台（workspace）

#### 统一收件箱 `/unifiedInbox/list` — `@/views/unifiedInbox/List.vue`
- 用途：跨平台会话统一收件与处理。
- 主要区块：页头（刷新统计）+ 6 张统计卡（会话总数/未读/待处理/已分配/已关闭/超时未响应）+ 平台会话分布条形图 + 搜索表单 + 会话表格 + 分页。
- 关键字段：按平台/状态/关键字筛选；会话列表含客户、平台、状态、分配坐席、最近消息时间。
- 关键操作：刷新统计、搜索、分配/处理会话。
- 数据来源：会话列表接口 + `loadStats`（会话统计，含 `by_platform`）。

#### 客户会话管理 `/customerSession/list` — `@/views/customerSession/List.vue`
- 用途：多渠道客户对话统一处理台（集成坐席状态/快捷回复/标签/AI建议）。
- 主要区块：三栏看板 = 左（会话排队列表，可按进行中/已结束筛选）+ 中（对话消息流）+ 右（客户信息/标签/AI建议）；页头含「我的状态」在线/忙碌/离线切换、「新建会话」。
- 关键字段：会话项含客户名、最近消息、时间、`handlerType`（人工/AI）标签。
- 关键操作：切换坐席状态、新建会话、选择会话、发送消息、打标签、采纳 AI 建议、快捷回复。
- 数据来源：会话/消息接口 + 坐席状态 + AI 建议 + 快捷回复 + 会话标签接口。

#### 统一消息 `/unifiedMessage/list` — `@/views/unifiedMessage/List.vue`
- 用途：全渠道消息汇总检索。
- 主要区块：搜索表单（关键字/消息类型 系统·用户·通知/状态 未读·已读）+ 消息表格 + 分页。
- 关键字段（表格列）：ID、消息ID、内容、发送者、会话、类型、状态标签、时间、操作。
- 关键操作：搜索、重置、查看详情。
- 数据来源：统一消息列表接口。

#### 消息中台 MQ `/messageHub/list` — `@/views/messageHub/List.vue`
- 用途：消息队列吞吐监控与手动推送。
- 主要区块：页头（推送消息/刷新统计）+ 6 张统计卡（消息总数/接收/发送/未读/近24h新增/活跃平台数）+ 平台消息分布条形图 + 消息表格 + 分页。
- 关键操作：推送消息（弹窗）、刷新统计、查看消息。
- 数据来源：消息列表 + `loadStats`（含 `by_platform`）。

#### 企微账号管理 `/wecomAccount/list` — `@/views/wecomAccount/List.vue`
- 用途：企业微信账号健康度与风控管理。
- 主要区块：4 张健康度概览卡（账号总数/平均健康分/配额使用/风险账号）+ 账号列表表格。
- 关键字段（表格列）：企业ID、应用ID、登录状态标签、风险等级标签、健康分、配额使用等。
- 关键操作：刷新、查看/处置风险账号。
- 数据来源：账号列表接口 + 健康度汇总接口（`summary`）。

### 分组 B · 客户（customer）

#### 客户 360 `/customer360/list` — `@/views/customer360/List.vue`
- 用途：客户全景画像（左搜索列表 + 右详情）。
- 主要区块：左栏客户搜索表格（姓名/手机/来源）；右栏基础信息卡（头像+标签+描述项）+ 详情 Tabs（基本信息/行为/标签等）。
- 关键字段：手机、邮箱、微信、来源、注册时间、最后活跃、客户价值、消费次数、状态；基本信息含年龄/性别/职业/地区/生日/备注。
- 关键操作：搜索客户、选择客户、联系客户、添加标签。
- 数据来源：客户列表 + 客户详情接口。

#### 客户事件追踪 `/customerEvent/list` — `@/views/customerEvent/List.vue`
- 用途：追踪客户在系统内的所有行为事件。
- 主要区块：页头（创建事件）+ 4 张统计卡（今日/本周事件、活跃用户、事件类型）+ 事件列表（按类型 浏览/点击/注册/购买/分享 筛选）。
- 关键操作：创建事件、按类型筛选、查看事件。
- 数据来源：事件列表 + 事件统计接口。

#### 线索列表 `/clue/list` — `@/views/clue/List.vue`
- 用途：批量导入与管理营销线索。
- 主要区块：导入线索按钮 + 导入弹窗（线索类型下拉 + 多行文本"名称,账号,城市,地址"）+ 线索表格。
- 关键字段（表格列）：名称、账号、城市、地址、类型、是否验证、操作。
- 关键操作：导入线索、删除线索。
- 数据来源：`clueApi`（列表/导入/删除）。

#### 线索统计 `/clue/statistics` — `@/views/clue/Statistics.vue`
- 用途：按线索类型统计总量与验证量。
- 主要区块：卡片网格，每种线索类型一卡（类型名/总数/已验证）。
- 数据来源：`clueApi.statistics()`。

#### 标签分层 `/tagSegmentation/list` — `@/views/tagSegmentation/List.vue`
- 用途：管理用户标签、自动标签规则、RFM 分层策略与统计。
- 主要区块：页头（新增标签/刷新）+ Tabs：标签列表（搜索+类型筛选 手动/自动/系统）、自动标签规则、RFM 分层、标签统计。
- 关键字段（标签表列）：标签名称、类型标签、分类、用户数、描述、创建时间、操作。
- 关键操作：新增/编辑/删除标签、配置自动规则、刷新。
- 数据来源：标签/规则/分层/统计接口。

#### 用户分群 RFM `/userSegment/list` — `@/views/userSegment/List.vue`
- 用途：基于 RFM 模型分群，识别高价值客户。
- 主要区块：页头（创建分群/刷新）+ 4 张 RFM 概览卡（总用户/高价值/活跃/流失风险）+ RFM 客户分群矩阵 + 分群列表。
- 关键操作：创建分群、刷新、查看分群明细。
- 数据来源：分群统计 + 分群列表接口。

#### 客服 Web Widget 渠道 `/chatChannel/list` — `@/views/chatChannel/List.vue`
- 用途：管理嵌入企业网站的客服浮标入口（每渠道 = 1 AppKey + 白名单 origin）。
- 主要区块：页头（刷新/新建渠道）+ 搜索栏（关键词、状态 启用/禁用）+ 渠道表格。
- 关键操作：新建渠道（`/chatChannel/create`）、编辑（`/chatChannel/edit/:id`）、安装引导（`/chatChannel/install/:id`）、搜索。
- 数据来源：渠道列表/增删改接口。
- 关联页：`Create.vue` 新建、`Edit.vue` 编辑、`InstallGuide.vue` 嵌入代码安装引导。

### 分组 C · AI 智能体（aiAgent）

#### AI 智能体管理 `/aiAgent/list` — `@/views/aiAgent/List.vue`
- 用途：管理销售/客服/混合智能体。
- 主要区块：页头（刷新/新建智能体）+ 搜索栏（类型 销售·客服·混合 / 状态 / 关键词）+ 智能体表格。
- 关键操作：新建（`/aiAgent/create`）、编辑（`/aiAgent/edit/:id`）、启停、刷新。
- 数据来源：智能体列表/增删改接口。

#### 创建/编辑智能体 `/aiAgent/create`、`/aiAgent/edit/:id` — `@/views/aiAgent/Edit.vue`
- 用途：配置智能体人设、知识库、SOP、话术与 LLM 参数。
- 主要区块：分区表单（基本信息 / 人设 / 知识库绑定 / SOP / 话术 / LLM 参数）+ 顶部「测试」对话弹窗。
- 关键字段：智能体编码（唯一，编辑禁改）、名称、类型（销售/客服/混合）等。
- 关键操作：保存、测试对话、返回列表。
- 数据来源：智能体详情/保存/测试接口。

#### 对话记忆 `/dialogueMemory/list` — `@/views/dialogueMemory/List.vue`
- 用途：短期+长期双层记忆，为 SOP 智能体提供客户上下文。
- 主要区块：页头（刷新）+ 4 张统计卡（客户记忆数/高意向/中意向/累计异议记录）+ 记忆列表。
- 数据来源：记忆列表/统计接口。

#### 销售意图识别 `/intentRecognition/list` — `@/views/intentRecognition/List.vue`
- 用途：规则 + LLM 双引擎识别客户意图。
- 主要区块：意图识别测试区（客户ID/平台/对话文本 → 识别结果）+ 意图记录/规则列表。
- 关键操作：填入示例、执行识别、维护规则。
- 数据来源：意图识别/记录接口。

#### 异议处理中心 `/objection/list` — `@/views/objection/List.vue`
- 用途：识别客户异议类型并匹配最佳应对话术。
- 主要区块：页头（智能处理）+ 4 张统计卡（异议类别/已匹配模板/置信度/使用记录）+ 智能处理区 + 模板列表。
- 关键操作：智能处理（输入异议→匹配模板）、维护异议模板。
- 数据来源：异议类别/模板/处理接口。

#### 销冠能力画像 `/persona/list` — `@/views/persona/List.vue`
- 用途：员工 5 维度能力评估、趋势与团队对比。
- 主要区块：左员工列表（搜索、综合分标签）+ 右能力雷达图/趋势/对比。
- 关键操作：选择员工、刷新。
- 数据来源：员工列表 + 能力画像接口。

#### 销冠 SOP 智能体 `/sopAgent/list` — `@/views/sopAgent/List.vue`
- 用途：基于意图+记忆自动推进销售 SOP。
- 主要区块：Tabs（SOP 管理 / 执行监控 等）；SOP 管理含 4 统计卡（SOP 总数/已激活/已停用/进行中执行）+ SOP 列表。
- 关键操作：新建/激活/停用 SOP、查看执行。
- 数据来源：SOP 列表/统计/执行接口。

#### 话术模板 `/scriptTemplate/list` — `@/views/scriptTemplate/List.vue`
- 用途：管理营销/销售话术模板。
- 主要区块：页头（新增话术）+ 场景筛选（开场白/跟进/异议处理/促成成交）+ 搜索 + 话术表格。
- 关键字段（表格列）：标题、场景标签、内容、使用次数、效果评分(评星)、更新时间、操作。
- 关键操作：新增/编辑/删除话术。
- 数据来源：话术模板增删改查接口。

#### LLM 多模型路由 `/llmRouting/list` — `@/views/llmRouting/List.vue`
- 用途：多模型接入、场景路由、Fallback 策略与成本统计。
- 主要区块：页头（新增模型/刷新）+ Tabs：模型列表 / 场景路由 / 成本统计。
- 关键字段（模型表列）：模型名称、厂商、状态、优先级、配额(次/日)、已用、接入地址、操作。
- 关键操作：新增/编辑模型、测试模型、配置路由与 Fallback。
- 数据来源：模型/路由/成本接口。

#### 置信度运营面板 `/confidence` — `@/views/confidence/Panel.vue`
- 用途：置信度信号分布、动态阈值、转人工规则。
- 主要区块：4 统计卡（信号总数24h/平均置信度/转人工触发/高置信自动回复）+ Tabs（分布 / 阈值策略 / 转人工规则）。
- 数据来源：置信度统计/策略接口。

#### 拟人度评估 `/humanize` — `@/views/humanize/Panel.vue`
- 用途：AI 回复拟人度评分、销冠话术基线、低质样本收集。
- 主要区块：4 统计卡（评估总数24h/平均拟人度/低质样本/销冠基线）+ Tabs。
- 数据来源：拟人度评估接口。

#### 反馈学习闭环 `/feedbackLoop` — `@/views/feedbackLoop/Panel.vue`
- 用途：销冠对话聚类 → Prompt 候选迭代 → MAB A/B 自适应分流。
- 主要区块：4 统计卡（反馈事件24h/销冠对话/Prompt候选/Bandit探索中）+ Tabs。
- 数据来源：反馈/聚类/候选/Bandit 接口。

#### 资产市场 `/asset-market` — `@/views/assetMarket/Market.vue`
- 用途：浏览并试用平台资产（智能体角色/话术/AB方案/工作流/行业SOP）。
- 主要区块：筛选栏（类型/行业）+ 资产卡片网格（封面/标题/行业·类型标签/描述）。
- 关键操作：查询、进入「我的资产」、点击卡片进详情。
- 数据来源：`assetMarket` 列表接口。

#### 我的资产 `/asset-market/my-assets` — `@/views/assetMarket/MyAssets.vue`
- 用途：管理已购买/自建资产。
- 主要区块：页头（去市场/自建资产/同步日志）+ 筛选（类型/来源 平台购买·自建/关键词）+ 资产表格。
- 关键操作：自建资产、查询、同步。
- 数据来源：我的资产列表接口。

#### 资产详情 `/asset-market/detail/:id` — `@/views/assetMarket/Detail.vue`（非菜单）
- 用途：查看单个资产详情与数据预览。
- 主要区块：页头返回 + 资产卡（名称/行业·类型·版本标签/描述）+ 数据预览(JSON)。
- 关键操作：免费试用（`purchaseAsset`）。
- 数据来源：`assetDetail`、`purchaseAsset`。

### 分组 D · 知识库 / RAG（knowledge）

#### 知识库管理 `/knowledge/management` — `@/views/KnowledgeWorkspace/KnowledgeManagement.vue`
- 用途：知识库文档全生命周期管理（导入/编辑/索引）。
- 主要区块：6 张统计卡（文档总数/分段总数/总Token/今日导入/索引就绪率/检索命中率）+ 工具栏 + 文档表格。
- 关键操作：导入/新建文档、编辑、重建索引、删除。
- 数据来源：知识库 `overview` + 文档列表接口。

#### API Token 管理 `/knowledge/api-token` — `@/views/KnowledgeWorkspace/ApiToken.vue`
- 用途：为外部系统(CRM/ERP/Helpdesk)创建 API Token，用于 `/api/knowledge-merchant/external/import` 推送文档。
- 主要区块：左 Token 列表（按产品筛选）+ 右创建面板；明文仅创建时显示一次。
- 关键字段（表格列）：ID、名称、产品、权限(scopes)、状态、操作。
- 关键操作：创建/吊销 Token、按产品筛选、刷新。
- 数据来源：Token 列表/创建/吊销接口。

#### 检索 Playground `/knowledge/playground` — `@/views/KnowledgeWorkspace/Playground.vue`
- 用途：生产前验证 topK、相似度阈值、过滤条件对检索质量的影响。
- 主要区块：左检索参数（产品/查询文本/Top K 滑块/相似度阈值滑块/过滤条件）+ 右命中分段结果（可提交"相关/不相关"反馈）。
- 关键操作：执行检索、对命中分段打反馈。
- 数据来源：检索接口 + 反馈接口。

#### 分段编辑工作台 `/knowledge/chunks` — `@/views/KnowledgeWorkspace/ChunkManagement.vue`
- 用途：查看/编辑/拆分/删除/重新嵌入文档分段。
- 主要区块：`ChunkEditor` 组件（按 `docId` 加载）；编辑后向量索引清空、下次构建自动重嵌。
- 数据来源：分段编辑接口（ChunkEditor 内）。

#### 外部系统接入 `/knowledge/external-import` — `@/views/KnowledgeWorkspace/ExternalImport.vue`
- 用途：通过 API Token 接入飞书/Notion/钉钉/自有 CRM 文档，支持异步(job_no)与同步(sync=true)两种模式。
- 主要区块：左导入测试表单（数据源单选 通用JSON/飞书/Notion/钉钉 + 产品 + JSON 调试）+ 右结果。
- 关键操作：切换数据源、提交导入、查看结果。
- 数据来源：外部导入接口。

#### OpenAPI 数据源集成 `/knowledge/openapi` — `@/views/KnowledgeWorkspace/OpenAPIIntegration.vue`
- 用途：配置外部 OpenAPI 接口自动拉取数据入库（支持 GET/POST/RESTful，Bearer/API Key/HMAC/Basic 认证）。
- 主要区块：筛选栏（产品）+ 新建数据源按钮 + 数据源表格。
- 关键字段（表格列）：ID、名称、类型、方法(GET/POST)、状态、操作。
- 关键操作：新建/编辑/触发同步数据源。
- 数据来源：OpenAPI 数据源列表/增删改/同步接口。

#### 知识库统计 `/knowledge/statistics` — `@/views/KnowledgeWorkspace/KnowledgeStatistics.vue`
- 用途：知识库文档/检索质量数据分析。
- 主要区块：筛选（产品/时间范围 7·30·90 天）+ 概览卡（文档总数/分段总数等）+ 图表。
- 数据来源：知识库统计接口。

#### 检索反馈管理 `/knowledge/feedback` — `@/views/KnowledgeWorkspace/FeedbackList.vue`
- 用途：汇集 Playground 用户反馈，指导优化文档/阈值。
- 主要区块：筛选（产品/评价 相关·一般·不相关/关键词）+ 统计卡（总反馈/相关/不相关等）+ 反馈表格。
- 数据来源：反馈列表/统计接口。

#### 批量导入 `/knowledge/batch-import` — `@/views/KnowledgeWorkspace/BatchImport.vue`
- 用途：批量导入知识库文档。
- 主要区块：Tabs = 文件上传(CSV/JSON/Excel) / 文本批量粘贴(JSON 数组)；右侧预览。
- 关键字段：每条记录 title、content(必填)、category、tags。
- 关键操作：选择产品/格式、上传或粘贴、预览、提交导入。
- 数据来源：批量导入接口。

#### 模板市场 `/templateMarket/list` — `@/views/templateMarket/List.vue`
- 用途：营销模板市场，开箱即用。
- 主要区块：页头（提交模板）+ 筛选（搜索/分类 邮件·短信·WhatsApp·落地页·海报·话术/标签 热门·新品·免费·付费/排序 最新·最热·评分）+ 模板卡片网格。
- 关键操作：提交模板、查看/使用模板。
- 数据来源：模板列表接口。

#### RAG 智能体概览 `/system/rag-overview` — `@/views/system/RagOverview.vue`
- 用途：RAG 产品运行概览与快速入口。
- 主要区块：3 统计卡（RAG产品数量/活跃产品/已启用账号）+ 快速操作卡片。
- 数据来源：RAG 概览统计接口。

#### RAG 产品配置 `/ragProductConfig` — `@/views/RagProductConfig/index.vue`
- 用途：RAG 产品与账号配置容器页（card 型 Tabs）。
- 主要区块：Tab「RAG 产品管理」→ `RagProductManagement.vue`；Tab「账号配置」→ `AccountConfig.vue`。
  - RAG 产品管理：产品表格（产品名称/类别/LLM模型/温度值/最大Token/Top-P/操作）+ 新增产品。
  - 账号配置：表单（平台 抖音·快手·小红书·闲鱼 + 账号ID + 密钥等）。
- 数据来源：RAG 产品/账号增删改查接口。

#### AI 内容生成 `/aiContent/list` — `@/views/aiContent/List.vue`
- 用途：AI 生成营销文案、产品描述、话术、短视频脚本。
- 主要区块：左内容生成器表单 + 右生成结果/历史。
- 关键字段：内容类型（营销文案/产品描述/社交媒体/邮件主题/话术/短视频脚本）、产品主题、目标人群、风格、字数限制、关键词。
- 关键操作：生成内容、复制、保存。
- 数据来源：AI 内容生成接口。

### 分组 E · 触达（reach）

#### 营销流程 `/marketingFlow/list` — `@/views/marketingFlow/List.vue`
- 用途：可视化编排营销自动化流程。
- 主要区块：页头（新建流程）+ 流程表格 + 编辑弹窗（900px 画布）。
- 关键字段（表格列）：流程名称、触发条件、步骤数、执行次数、成功率(进度条)、状态(开关)、更新时间、操作。
- 关键操作：新建/编辑/执行/查看日志/删除、启停开关。
- 数据来源：营销流程增删改查/执行/日志接口。

#### 触达 Pipeline `/reachPipeline/list` — `@/views/reachPipeline/List.vue`
- 用途：多通道触达 Pipeline 编排与执行监控。
- 主要区块：Tabs = Pipeline 管理 / 执行监控 等；Pipeline 管理含 4 统计卡（总数/运行中/已暂停/已归档）+ 列表。
- 关键操作：新建/启停/归档 Pipeline、查看执行。
- 数据来源：Pipeline 列表/统计/执行接口。

#### 短信列表 `/sms/list` — `@/views/sms/List.vue`
- 用途：短信发送记录管理。
- 主要区块：页头（发送短信）+ 搜索表单（手机号/状态 待发送·发送中·已发送·失败/发送时间范围）+ 短信表格。
- 关键操作：发送短信、搜索、重置。
- 数据来源：短信列表接口。
- 关联页：`/sms/drafts` 草稿、`/sms/jobs` 发送任务、`/sms/config` 短信配置。

#### 邮件列表 `/email/list` — `@/views/email/EmailList.vue`
- 用途：邮件发送/阅读追踪记录。
- 主要区块：邮件表格。
- 关键字段（表格列）：主题、收件人、发件人、是否发送、发送时间、是否阅读、阅读时间。
- 数据来源：邮件列表接口。
- 关联页：`/email/drafts` 草稿、`/email/jobs` 发送任务、`/email/smtp` SMTP 配置、`/email/info`、`/email/guide` 配置引导。

#### TikTok 卡片管理 `/tiktokCard/list` — `@/views/tiktokCard/List.vue`
- 用途：管理 TikTok 引流卡片（标题/描述/图片/跳转）。
- 主要区块：页头（统计分析/添加卡片）+ 搜索表单（关键词/状态 激活·禁用）+ 卡片表格。
- 关键字段（表格列）：ID、标题、描述、图片、状态、操作。
- 关键操作：添加/编辑/删除卡片、进入统计。
- 数据来源：卡片列表/增删改接口。
- 关联页：`/tiktokCard/stats` 统计、`/tiktokCard/cardStats` 单卡统计、`/tiktokCard/autoReply` 自动回复。
- **同构平台**：抖音 `douyinCard/*`、快手 `kuaishouCard/*`、小红书 `xiaohongshuCard/*`、闲鱼 `xianyuCard/*` 页面结构与 TikTok 卡片完全一致（列表/统计/自动回复），仅平台不同。

#### 批量操作 `/batchOperation/list` — `@/views/batchOperation/List.vue`
- 用途：批量处理线索/客户/消息等数据。
- 主要区块：工具卡片网格（每个批量工具一卡，含已执行次数）+ 操作历史表格。
- 关键字段（历史列）：工具、目标、总数、成功、失败、状态、执行时间。
- 关键操作：点击工具执行批量任务。
- 数据来源：批量工具/历史接口。

#### 活码管理 `/livecode/management` — `@/views/livecode/LiveCodeManagement.vue`
- 用途：管理可动态跳转的活码（短链+二维码）。
- 主要区块：页头（新增活码）+ 搜索栏（名称/状态 启用·禁用）+ 活码表格。
- 关键字段（表格列）：ID、活码名称、短链、完整短链(可点击)、状态、操作。
- 关键操作：新增/编辑/删除活码。
- 数据来源：活码列表/增删改接口。

#### 短链管理 `/shortLink/list` — `@/views/shortLink/List.vue`
- 用途：生成与管理营销短链。
- 主要区块：页头（添加短链）+ 搜索表单（短码/原始URL/状态 正常·禁用）+ 短链表格 + 分页。
- 关键操作：添加/编辑/删除短链、查看统计。
- 数据来源：短链列表/增删改接口。
- 关联页：`/shortLink/stats` 短链访问统计。

### 分组 F · 社媒运营（community）

#### 社群管理 `/community/list` — `@/views/community/List.vue`
- 用途：管理社群分组及成员。
- 主要区块：页头（导出/导入/新增分组）+ 搜索表单（分组名称/状态 正常·禁用）+ 分组表格。
- 关键操作：新增/编辑/删除分组、导入/导出、搜索。
- 数据来源：社群分组列表/增删改/导入导出接口。

#### Telegram 机器人账号 `/telegram/account` — `@/views/telegram/account.vue`
- 用途：管理 TG 机器人账号与 Webhook、AI 绑定。
- 主要区块：搜索栏 + 账号操作（添加机器人/刷新）+ 账号表格。
- 关键字段（表格列）：账号名称、Bot Token(脱敏)、Webhook URL、Webhook 状态、智能体状态、操作。
- 关键操作：添加机器人、注册 Webhook、绑定 AI、刷新。
- 数据来源：TG 账号列表/增删改接口。

#### 飞书账号 `/feishu/account` — `@/views/feishu/FeishuAccount.vue`
- 用途：管理飞书应用账号与 Webhook。
- 主要区块：搜索栏 + 添加飞书账号/刷新 + 账号表格。
- 关键字段（表格列）：账号名称、App ID、状态(正常/停用)、Webhook(已启用/未启用)、操作。
- 数据来源：飞书账号列表/增删改接口。

#### WhatsApp 账号 `/whatsapp/account` — `@/views/whatsapp/WhatsappAccount.vue`
- 用途：管理 WhatsApp 账号并绑定 AI 智能体。
- 主要区块：添加账号表单（名称/备注）+ 账号表格 + AI 绑定对话框。
- 关键字段（表格列）：名称、备注、状态、操作（登录/状态/绑定AI）。
- 关键操作：添加账号、扫码登录、刷新状态、绑定 AI。
- 数据来源：WhatsApp 账号列表/登录/状态接口。
- 关联页：`/whatsapp/drafts` 草稿、`/whatsapp/jobs` 发送任务。

#### WhatsApp 批量消息 `/whatsappBot/bulk-messaging` — `@/views/whatsappBot/BulkMessaging.vue`
- 用途：基于模板+账号批量发送 WhatsApp 消息。
- 主要区块：Tabs = 发送消息（消息模板/发送账号/收件人）/ 发送记录。
- 关键操作：选模板、选账号、导入收件人、批量发送。
- 数据来源：模板/账号/批量发送接口。
- 关联页：`/whatsappBot/lead-group-selection` 从线索库选择目标群体。

#### 域名池管理 `/domainPool/list` — `@/views/domainPool/List.vue`
- 用途：管理短链/落地页可用域名并检测可用性。
- 主要区块：页头（添加域名/检查所有域名）+ 搜索表单（域名/状态 正常·不可访问）+ 域名表格。
- 关键操作：添加域名、批量检查、搜索。
- 数据来源：域名列表/增删改/检测接口。

#### 客服工作台子模块（customerService）
- **坐席状态 `/customerService/agent-status`** — `AgentStatus.vue`：维护客服在线状态/当前会话数/接待上限；4 统计卡（坐席总数/在线坐席/当前会话/…）+ 坐席表格 + 新增坐席。
- **AI 建议 `/customerService/ai-suggestion`** — `AISuggestion.vue`：展示/管理 AI 对客服的回复建议。
- **快捷回复 `/customerService/quick-reply`** — `QuickReply.vue`：按分类维护常用话术模板；分类筛选 + 搜索 + 新增快捷回复。
- **会话标签 `/customerService/session-tag`** — `SessionTag.vue`：维护会话标签体系。

### 分组 G · 分析洞察（analytics）

#### AI 产能分析 `/aiProductivity/list` — `@/views/aiProductivity/List.vue`
- 用途：对话量/转化率/响应时长/销冠画像多维统计。
- 主要区块：4 概览卡（总对话数/AI转化率/平均响应ms/销冠人数）+ Tabs（对话量/转化/响应/画像）。
- 关键操作：刷新。
- 数据来源：AI 产能概览/明细接口。

#### 转化漏斗 `/conversionFunnel/list` — `@/views/conversionFunnel/List.vue`
- 用途：定义漏斗阶段并分析转化率、流失与时间趋势。
- 主要区块：页头（新增阶段/刷新）+ 4 统计卡（总进入数/最终转化数/整体转化率/…）+ 漏斗图。
- 关键操作：新增/编辑漏斗阶段、刷新。
- 数据来源：漏斗阶段/统计接口。

#### 客户旅程大屏 `/customerJourney/dashboard` — `@/views/customerJourney/Dashboard.vue`
- 用途：9 阶段客户旅程实时监控 + 转化漏斗 + 沉睡客户检测。
- 主要区块：页头（最后更新时间/自动刷新开关/刷新）+ 顶部统计卡（客户总数/成交客户/…）+ 阶段漏斗/趋势图。
- 关键操作：切换自动刷新、手动刷新。
- 数据来源：旅程概览接口。

#### 营销数据大屏 `/dashboardScreen/list` — `@/views/dashboardScreen/List.vue`
- 用途：可全屏展示的营销 KPI 数据大屏。
- 主要区块：顶部标题+时间+全屏按钮 + KPI 卡片行（含较昨日趋势）+ 图表行（营销趋势近30天等 ECharts）。
- 关键操作：全屏切换。
- 数据来源：大屏 KPI/图表接口。

#### 自定义报表 `/customReport/list` — `@/views/customReport/List.vue`
- 用途：自定义维度/指标生成业务报表。
- 主要区块：页头（新建报表）+ 报表表格 + 创建/编辑弹窗。
- 关键字段（表格列）：报表名称、报表类型、维度、指标、创建人、创建时间、操作。
- 关键操作：新建/查看/导出/编辑/删除报表。
- 数据来源：报表列表/增删改/导出接口。

#### A/B 测试 `/abExperiment/list` — `@/views/abExperiment/List.vue`
- 用途：创建并管理 A/B 实验、对比方案效果。
- 主要区块：页头（创建实验）+ 4 统计卡（进行中/已完成/显著胜出/…）+ 实验列表。
- 关键操作：创建/启停实验、查看结果。
- 数据来源：实验列表/统计接口。

#### 流失预测 `/churnPrediction/list` — `@/views/churnPrediction/List.vue`
- 用途：基于机器学习预测用户流失风险并提前干预。
- 主要区块：页头（运行预测/刷新）+ 风险统计卡（高/中/低风险用户）+ 风险用户列表。
- 关键操作：运行预测、刷新、干预。
- 数据来源：流失预测运行/结果接口。

### 分组 H · 系统管理（system）

#### 平台账号管理 `/platformAccount/list` — `@/views/platformAccount/List.vue`
- 用途：管理各平台（抖音/快手/小红书/闲鱼等）授权账号。
- 主要区块：页头（支持平台/新增账号）+ 搜索表单（平台/账号/状态 正常·异常·未登录）+ 账号表格。
- 关键操作：新增/编辑/删除账号、查看支持平台、搜索。
- 数据来源：平台账号列表/增删改接口。

#### 团队管理 `/teamUser/list` — `@/views/teamUser/List.vue`
- 用途：管理团队成员及其角色。
- 主要区块：页头（添加成员）+ 4 统计卡（团队成员/管理员/活跃成员/…）+ 成员表格。
- 关键操作：添加/编辑/停用成员、分配角色。
- 数据来源：成员列表/统计/增删改接口。

#### 角色权限管理 `/teamUser/role` — `@/views/teamUser/Role.vue`
- 用途：维护角色与权限点的对应关系。
- 主要区块：页头（新建角色，仅管理员可用）+ 3 统计卡（角色数量/权限点数/系统角色）+ 角色表格 + 权限分配。
- 关键操作：新建/编辑角色、勾选权限点。
- 数据来源：角色/权限接口（`roleService.GetPermissions` 等）。

#### 系统配置 `/system/config` — `@/views/system/Config.vue`
- 用途：站点基础信息、SEO、客服备案等全局配置。
- 主要区块：单表单分区（基础信息 站点名/URL/Logo/主题色 → SEO 关键词·描述 → 客服与备案 电话·邮箱·备案号 等）。
- 关键操作：保存配置。
- 数据来源：系统配置读取/保存接口。

#### 素材库管理 `/system/material` — `@/views/system/MaterialLibrary.vue`
- 用途：图片/视频/音频/文档素材集中管理。
- 主要区块：页头（上传素材/新建分类）+ 搜索筛选（分类/类型）+ 素材网格。
- 关键操作：上传、新建分类、删除素材。
- 数据来源：素材列表/分类/上传接口。

#### 服务器监控 `/system/monitor` — `@/views/system/Monitor.vue`
- 用途：查看服务器实时资源指标。
- 主要区块：指标卡片（CPU/内存/磁盘使用率、网络速度等）+ 刷新。
- 数据来源：系统监控指标接口。

#### 云存储配置 `/system/obs` — `@/views/system/ObsConfig.vue`
- 用途：配置对象存储（OSS/COS/七牛等）。
- 主要区块：页头（新增配置）+ 配置表格。
- 关键字段（表格列）：配置名称、服务商、存储桶、节点域名、是否默认、状态、操作。
- 关键操作：新增/编辑/设默认/删除配置。
- 数据来源：存储配置列表/增删改接口。

#### 使用引导 `/system/guide` — `@/views/system/Guide.vue`
- 用途：新用户功能引导与帮助文档入口。
- 主要区块：分步/分模块的引导卡片。

#### 集成管理 `/integration/list` — `@/views/integration/List.vue`
- 用途：管理第三方系统集成与 API 对接。
- 主要区块：页头（添加集成）+ 统计卡（已启用/已禁用/异常/…）+ 集成表格。
- 关键操作：添加/编辑/启停/测试集成。
- 数据来源：集成列表/统计/增删改接口。

#### 操作日志 `/operationLog/list` — `@/views/operationLog/List.vue`
- 用途：审计追踪所有用户操作。
- 主要区块：页头（导出/刷新）+ 筛选栏（关键词/模块 用户·线索·营销·系统/操作类型 创建·更新·删除·登录·登出）+ 日志表格。
- 关键操作：搜索、导出、刷新。
- 数据来源：操作日志列表/导出接口。

#### 安全审计 `/securityAudit/list` — `@/views/securityAudit/List.vue`
- 用途：安全检查、风险评估、异常告警追踪。
- 主要区块：页头（刷新/立即审计）+ 统计卡（审计总数/高风险/…）+ 审计记录表格。
- 关键操作：立即审计、刷新、处理告警。
- 数据来源：安全审计运行/列表/统计接口。


#### 数据备份 `/backup/list` — `@/views/backup/List.vue`
- 用途：系统全量备份、自动调度、文件归档。
- 主要区块：页头（刷新/立即备份）+ 统计卡（备份总数/已完成/…）+ 备份记录表格。
- 关键操作：立即备份、下载/恢复/删除备份、配置自动调度。
- 数据来源：备份列表/创建/恢复接口。

### 分组 I · 独立页 / 非主菜单

#### 个人资料 `/profile` — `@/views/Profile.vue`
- 用途：查看与维护当前登录账号信息。
- 主要区块：左侧头像卡（首字母头像/角色标签/用户名/ID）+ 右侧资料表单（+ 修改密码）。
- 关键操作：刷新、保存资料、修改密码。
- 数据来源：当前用户资料接口。

#### 通知中心 `/notifications` — `@/views/Notifications.vue`
- 用途：查看平台系统通知、版本公告与提醒。
- 主要区块：页头（刷新/全部标记已读）+ 统计卡（总消息/未读/…）+ 通知列表。
- 关键操作：刷新、标记已读。
- 数据来源：通知列表/统计/标记已读接口。

#### OneID 客户身份管理 `/oneid/list` — `@/views/oneid/List.vue`
- 用途：将客户多渠道身份（手机/邮箱/微信/抖音/小红书）归一为统一 ID。
- 主要区块：页头（搜索 UnifiedID/手机/邮箱 + 解析/创建 OneID）+ 说明 alert + 身份表格。
- 关键字段（表格列）：UnifiedID、手机号、邮箱等。
- 关键操作：搜索、解析/创建 OneID。
- 数据来源：OneID 列表/解析接口。

#### OneID 身份冲突解决 `/oneid/conflicts` — `@/views/oneid/Conflicts.vue`
- 用途：处理同一客户被识别为多个 OneID 的冲突。
- 主要区块：说明 alert + 筛选栏 + 冲突表格。
- 关键字段（表格列）：冲突 ID、OneID A、OneID B、冲突类型、冲突详情、检测时间、操作。
- 关键操作：合并、忽略。
- 数据来源：冲突列表/合并/忽略接口。（注：接口未通时菜单可能临时隐藏）

#### 系统初始化向导 `/setup` — `@/views/setup/InitSetup.vue`
- 用途：首次部署时初始化系统。
- 主要区块：3 步向导（阅读使用声明[开源条款+安装上报告知] → 创建超管账号 → 完成进入登录）。
- 关键操作：同意条款、创建超管、完成。
- 数据来源：初始化状态/创建超管接口。

#### 在线客服嵌入页 `/chat/embed` — `@/views/chat/embed/Index.vue`
- 用途：嵌入外部网站的全屏客服对话窗口（iframe/独立页）。
- 主要区块：`ChatWindow` 组件（全屏），加载中显示 spinner。
- URL 参数：`channelId`（默认 default）、`title`、`color`、`source`、`card_id`（卡片来源追踪）。
- 数据来源：客服会话/消息接口（ChatWindow 内）。

#### 登录 `/login` — `@/views/Login.vue`
- 用途：用户端登录入口。表单（用户名/密码），登录后跳工作台。

#### 404 `/:pathMatch(.*)*` — `@/views/NotFound.vue`
- 用途：未匹配路由兜底页。

---

> **规格来源说明**：以上内容规格均基于各 `.vue` 视图组件模板与脚本的真实实现提取（页面标题、区块、表格列、表单字段、按钮/行操作、接口调用）。TikTok/抖音/快手/小红书/闲鱼卡片模块为同构，正文以 TikTok 为代表说明；`sms/*`、`email/*`、`whatsapp/*` 等的草稿/任务/配置子页在各自主页面下以「关联页」列出。

#### 同步日志 `/asset-market/sync-log` — `@/views/assetMarket/SyncLog.vue`（非菜单）
- 用途：查看资产同步记录。
- 主要区块：筛选（资产ID）+ 日志表格（时间/资产/操作/状态/错误信息）。
- 数据来源：`syncLog`。
