# user-web 功能清单

> **规则级别**: ⭐⭐ 项目级开发文档

本清单基于 `user-web/src/api/`、`src/views/`、`src/router/modules/` 实际文件梳理，按业务域分组列出所有页面与对应 API。所有功能名、路由路径、API 文件名均来自代码实际文件，未做臆测。

## 总览统计

| 维度 | 数量 |
| --- | --- |
| API 模块文件（`src/api/*.js`） | **79** |
| 路由模块文件（`src/router/modules/*.js`） | **62** |
| 业务视图目录（`src/views/*/`） | **65** |
| 顶层菜单分组 | **8**（工作台 / 客户 / AI 智能体 / 知识库 / 触达 / 社媒运营 / 分析洞察 / 系统管理） |
| 独立路由（非主菜单） | **7**（登录 / 初始化 / 个人资料 / 通知 / OneID / 嵌入聊天 / 404） |
| 国际化语言 | **4**（zh / en / ja / ar） |
| Pinia store | **4**（user / permission / app / material） |
| WebSocket 通道 | **2**（agent / visitor） |

## 主菜单结构

参考 [../../MENU_SPEC.md](../ui-inventory/MENU_SPEC.md)：

1. **工作台**（workspace）：统一收件箱、客服会话、统一消息、消息中台、企微账号管理
2. **客户管理**（customer）：客户360、客户事件、线索、标签分层、用户分层RFM、客服渠道
3. **AI 智能体**（aiAgent）：智能体列表、对话记忆、意图识别、异议处理、销冠画像、SOP智能体、话术库、LLM路由、置信度/拟人度/反馈学习、资产市场
4. **知识库**（knowledge）：知识库管理、API Token、Playground、分段编辑、外部接入、OpenAPI集成、统计、反馈、批量导入、模板市场、RAG产品配置
5. **营销触达**（reach）：营销自动化、触达Pipeline、短信、邮件、TikTok/抖音/快手/小红书/闲鱼卡片、批量操作、活码、短链
6. **社媒运营**（community）：社群管理、Telegram、飞书、WhatsApp、域名池、客服子功能（坐席状态/AI建议/快捷回复/会话标签）
7. **分析洞察**（analytics/dataAnalysis）：AI产能分析、转化漏斗、客户旅程大屏、数据大屏、自定义报表、A/B实验、流失预警
8. **系统管理**（system，仅 admin）：平台账号、团队成员、角色权限、站点设置、素材库、监控、存储配置、第三方对接、操作日志、安全审计、备份恢复、使用引导

## 一、工作台（workspace）

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 统一收件箱 | `/unifiedInbox/list` | `unifiedInbox.js` | `views/unifiedInbox/List.vue` | 跨平台会话统一收件与处理；6 张统计卡 + 平台分布条形图 + 会话表格 |
| 客服会话 | `/customerSession/list` | `customerSession.js` | `views/customerSession/List.vue` | 三栏看板（会话排队/消息流/客户信息），集成坐席状态/快捷回复/标签/AI建议 |
| 统一消息 | `/unifiedMessage/list` | `unifiedMessage.js` | `views/unifiedMessage/List.vue` | 全渠道消息汇总检索；按消息类型/状态筛选 |
| 消息中台 MQ | `/messageHub/list` | `messageHub.js` | `views/messageHub/List.vue` | 消息队列吞吐监控与手动推送；6 张统计卡 + 平台消息分布 |
| 企微账号管理 | `/wecomAccount/list` | `wecomAccount.js` | `views/wecomAccount/List.vue` + `Data.vue` | 企业微信账号健康度与风控管理；4 张健康度概览卡 |

## 二、客户管理（customer）

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 客户 360 | `/customer360/list` | `customer360.js` | `views/customer360/List.vue` | 客户全景画像；左搜索列表 + 右详情 Tabs |
| 客户事件追踪 | `/customerEvent/list` | `customerEvent.js` | `views/customerEvent/List.vue` | 客户行为事件统一时间线；4 张统计卡 + 按类型筛选 |
| 线索列表 | `/clue/list` | `clue.js` | `views/clue/List.vue` | 批量导入与管理营销线索；含导入弹窗 |
| 线索统计 | `/clue/statistics` | `clue.js` | `views/clue/Statistics.vue` | 按线索类型统计总量与验证量 |
| 标签分层 | `/tagSegmentation/list` | `tagSegmentation.js` | `views/tagSegmentation/List.vue` | 标签管理 + 自动规则 + RFM 分层 + 统计四 Tab |
| 用户分群 RFM | `/userSegment/list` | `userSegment.js` | `views/userSegment/List.vue` | 基于 RFM 模型分群；4 张概览卡 + 矩阵 + 分群列表 |
| 客服 Web Widget 渠道 | `/chatChannel/list` | `chatChannel.js` | `views/chatChannel/List.vue` | 嵌入企业网站的客服浮标管理；每渠道 = 1 AppKey + 白名单 origin |
| 新建客服渠道 | `/chatChannel/create` | `chatChannel.js` | `views/chatChannel/Create.vue` | 创建渠道 |
| 编辑客服渠道 | `/chatChannel/edit/:id` | `chatChannel.js` | `views/chatChannel/Edit.vue` | 编辑渠道 |
| Widget 安装引导 | `/chatChannel/install-guide/:id?` | `chatChannel.js` | `views/chatChannel/InstallGuide.vue` | 嵌入代码安装引导（仅 admin） |

## 三、AI 智能体（aiAgent）

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 智能体列表 | `/aiAgent/list` | `aiAgent.js` | `views/aiAgent/List.vue` | 销售/客服/混合智能体管理 |
| 创建智能体 | `/aiAgent/create` | `aiAgent.js` | `views/aiAgent/Edit.vue` | 配置智能体人设/知识库/SOP/话术/LLM参数 |
| 编辑智能体 | `/aiAgent/edit/:id` | `aiAgent.js` | `views/aiAgent/Edit.vue` | 编辑现有智能体 |
| 对话记忆 | `/dialogueMemory/list` | `dialogueMemory.js` | `views/dialogueMemory/List.vue` | 短期+长期双层记忆；为 SOP 智能体提供上下文 |
| 意图识别 | `/intentRecognition/list` | `intentRecognition.js` | `views/intentRecognition/List.vue` | 规则 + LLM 双引擎识别客户意图 |
| 异议处理 | `/objection/list` | `objection.js` | `views/objection/List.vue` | 识别客户异议并匹配应对话术 |
| 销冠画像 | `/persona/list` | `persona.js` | `views/persona/List.vue` | 员工 5 维度能力评估、趋势与团队对比 |
| 销冠 SOP 智能体 | `/sopAgent/list` | `sopAgent.js` | `views/sopAgent/List.vue` | 基于意图+记忆自动推进销售 SOP |
| 话术模板 | `/scriptTemplate/list` | `scriptTemplate.js` | `views/scriptTemplate/List.vue` | 营销/销售话术模板管理；按场景筛选 |
| LLM 多模型路由 | `/llmRouting/list` | `llmRouting.js` | `views/llmRouting/List.vue` | 多模型接入、场景路由、Fallback、成本统计 |
| 置信度运营 | `/confidence/panel` | `tuning.js` | `views/confidence/Panel.vue` | 置信度信号分布、动态阈值、转人工规则 |
| 拟人度评估 | `/humanize/panel` | `tuning.js` | `views/humanize/Panel.vue` | AI 回复拟人度评分、销冠基线、低质样本 |
| 反馈学习闭环 | `/feedbackLoop/panel` | `tuning.js` | `views/feedbackLoop/Panel.vue` | 销冠对话聚类 → Prompt 候选迭代 → MAB A/B 自适应 |
| 资产市场 | `/asset-market` | `assetMarket.js` | `views/assetMarket/Market.vue` | 浏览并试用平台资产（智能体角色/话术/AB方案/工作流/行业SOP） |
| 我的资产 | `/asset-market/my-assets` | `assetMarket.js` | `views/assetMarket/MyAssets.vue` | 管理已购买/自建资产 |
| 资产详情 | `/asset-market/detail/:id` | `assetMarket.js` | `views/assetMarket/Detail.vue` | 非菜单；查看单个资产详情与数据预览 |
| 资产同步日志 | `/asset-market/sync-log` | `assetMarket.js` | `views/assetMarket/SyncLog.vue` | 非菜单；资产同步记录 |
| 渠道智能体绑定 | —（对话框组件） | `channelAgentBinding.js` | `components/AgentBindingDialog.vue` | 智能体 ↔ 渠道绑定 |
| 客服智能体挂载 | —（对话框组件） | `customerServiceAgent.js` | `components/AgentMountDialog.vue` | 智能体挂载到客服坐席 |
| 资产包 Playground | `/asset-bundle/playground` | `assetBundle.js` | `views/assetBundle/Playground.vue` | 低代码 Playground |
| 资产包商户编辑器 | `/asset-bundle/editor` | `assetBundle.js` | `views/assetBundle/MerchantEditor.vue` | 商户编辑器 |
| 资产包列表 | `/asset-bundle/list` | `assetBundle.js` | `views/assetBundle/List.vue` | 资产包列表 |

## 四、知识库（knowledge）

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 知识库管理 | `/knowledge/management` | `knowledge.js` + `knowledgeBase.js` | `views/KnowledgeWorkspace/KnowledgeManagement.vue` | 文档全生命周期管理；6 张统计卡 |
| API Token | `/knowledge/tokens` | `knowledge.js` | `views/KnowledgeWorkspace/ApiToken.vue` | 为外部系统创建 API Token |
| 检索 Playground | `/knowledge/playground` | `knowledge.js` | `views/KnowledgeWorkspace/Playground.vue` | 验证 topK/相似度阈值/过滤条件 |
| 分段编辑 | `/knowledge/chunks` | `knowledge.js` | `views/KnowledgeWorkspace/ChunkManagement.vue` + `ChunkEditor.vue` | 查看/编辑/拆分/删除/重嵌分段 |
| 外部系统接入 | `/knowledge/external` | `knowledgeMerchant.js` | `views/KnowledgeWorkspace/ExternalImport.vue` | 飞书/Notion/钉钉/CRM 文档接入 |
| OpenAPI 集成 | `/knowledge/openapi` | `knowledge.js` | `views/KnowledgeWorkspace/OpenAPIIntegration.vue` | 配置外部 OpenAPI 接口自动拉取 |
| 知识库统计 | `/knowledge/statistics` | `knowledge.js` | `views/KnowledgeWorkspace/KnowledgeStatistics.vue` | 文档/检索质量数据分析 |
| 反馈管理 | `/knowledge/feedbacks` | `knowledge.js` | `views/KnowledgeWorkspace/FeedbackList.vue` | 汇集 Playground 用户反馈 |
| 批量导入 | `/knowledge/batch-import` | `knowledge.js` | `views/KnowledgeWorkspace/BatchImport.vue` | 文件上传/文本批量粘贴 |
| 模板市场 | `/templateMarket/list` | —（独立模块） | `views/templateMarket/List.vue` | 营销模板市场，开箱即用 |
| RAG 概览 | `/system/rag-overview` | `ragProductConfig.js` | `views/system/RagOverview.vue` | RAG 产品运行概览 |
| RAG 主配置 | `/system/rag-product-config` | `ragProductConfig.js` | `views/RagProductConfig/index.vue` | 容器页（Tab） |
| RAG 产品管理 | `/system/rag-product` | `ragProductConfig.js` | `views/RagProductConfig/RagProductManagement.vue` | RAG 产品增删改查 |
| RAG 账号配置 | `/system/rag-account` | `ragProductConfig.js` | `views/RagProductConfig/AccountConfig.vue` | 平台账号 + 密钥 |
| 公开聊天 API | —（嵌入页后端接口） | `chatPublic.js` | — | 嵌入聊天窗的公开 API（无需登录） |

## 五、营销触达（reach）

### 5.1 通用触达

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 营销自动化 | `/marketingFlow/list` | `marketingFlow.js` | `views/marketingFlow/List.vue` | 可视化编排营销自动化流程 |
| 触达 Pipeline | `/reachPipeline/list` | `reachPipeline.js` | `views/reachPipeline/List.vue` | 多通道触达 Pipeline 编排与执行监控 |
| 批量操作 | `/batchOperation/list` | `batchOperation.js` | `views/batchOperation/List.vue` | 批量处理线索/客户/消息；工具卡片网格 + 操作历史 |
| 活码管理 | `/livecode` | `livecode.js` | `views/livecode/LiveCodeManagement.vue` | 短链 + 轮询二维码 + 统计 |
| 短链管理 | `/shortLink` | `shortLink.js` | `views/shortLink/List.vue` | 生成与管理营销短链 |
| 短链统计 | `/shortLink/stats` | `shortLink.js` | `views/shortLink/Stats.vue` | 短链访问统计 |

### 5.2 短信

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 短信列表 | `/sms/list` | `sms.js` | `views/sms/List.vue` | 短信发送记录管理 |
| 短信草稿 | `/sms/drafts` | `sms.js` | `views/sms/Drafts.vue` | 草稿箱 |
| 短信任务 | `/sms/jobs` | `sms.js` | `views/sms/Jobs.vue` | 发送任务 |
| 短信配置 | `/sms/config` | `sms.js` | `views/sms/Config.vue` | 短信网关配置 |

### 5.3 邮件

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 邮件列表 | `/email` | `email.js` | `views/email/EmailList.vue` | 邮件发送/阅读追踪 |
| 我的草稿 | `/email/drafts` | `email.js` | `views/email/Drafts.vue` | 草稿箱 |
| 我的任务 | `/email/jobs` | `email.js` | `views/email/Jobs.vue` | 发送任务 |
| 邮件账号 | `/email/smtp` | `email.js` | `views/email/Smtp.vue` | SMTP 配置 |
| 邮件代理 | `/email/info` | `email.js` | `views/email/Info.vue` | 代理配置 |
| 使用引导 | `/email/guide` | `email.js` | `views/email/Guide.vue` | 配置引导 |

### 5.4 渠道卡片（5 平台同构）

> TikTok / 抖音 / 快手 / 小红书 / 闲鱼 五平台卡片页面结构完全一致：`List.vue` / `Stats.vue` / `CardStats.vue` / `AutoReply.vue`，仅平台标识与 API 路径不同。

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| TikTok 卡片 | `/tiktok` → `/tiktok/list` | `tiktokCard.js` | `views/tiktokCard/List.vue` | TikTok 引流卡片管理 |
| TikTok 统计 | `/stats` | `tiktokCard.js` | `views/tiktokCard/Stats.vue` | 卡片统计 |
| TikTok 卡片统计 | `/card-stats/:id` | `tiktokCard.js` | `views/tiktokCard/CardStats.vue` | 单卡统计 |
| TikTok 自动回复 | `/auto-reply` | `tiktokAutoReply.js` | `views/tiktokCard/AutoReply.vue` | 浏览器自动化接管私信 |
| 抖音卡片 | `/douyinCard` | `douyinCard.js` | `views/douyinCard/List.vue` | 抖音引流卡片 |
| 抖音卡片统计 | `/douyin/stats` | `douyinCard.js` | `views/douyinCard/Stats.vue` | 统计 |
| 抖音卡片详情统计 | `/douyin-card-stats/:id` | `douyinCard.js` | `views/douyinCard/CardStats.vue` | 单卡统计 |
| 抖音自动回复 | `/douyin/auto-reply` | `autoReply.js` | `views/douyinCard/AutoReply.vue` | 自动回复 |
| 快手卡片 | `/kuaishouCard` | `kuaishouCard.js` | `views/kuaishouCard/List.vue` | 快手引流卡片 |
| 快手卡片统计 | `/kuaishou/stats` | `kuaishouCard.js` | `views/kuaishouCard/Stats.vue` | 统计 |
| 快手卡片详情统计 | `/kuaishou-card-stats/:id` | `kuaishouCard.js` | `views/kuaishouCard/CardStats.vue` | 单卡统计 |
| 快手自动回复 | `/kuaishou/auto-reply` | `autoReply.js` | `views/kuaishouCard/AutoReply.vue` | 自动回复 |
| 小红书卡片 | `/xiaohongshuCard` | `xiaohongshuCard.js` | `views/xiaohongshuCard/List.vue` | 小红书引流卡片 |
| 小红书卡片统计 | `/xiaohongshu/stats` | `xiaohongshuCard.js` | `views/xiaohongshuCard/Stats.vue` | 统计 |
| 小红书卡片详情统计 | `/xiaohongshu-card-stats/:id` | `xiaohongshuCard.js` | `views/xiaohongshuCard/CardStats.vue` | 单卡统计 |
| 小红书自动回复 | `/xiaohongshu/auto-reply` | `autoReply.js` | `views/xiaohongshuCard/AutoReply.vue` | 自动回复 |
| 闲鱼卡片 | `/xianyuCard` | `xianyuCard.js` | `views/xianyuCard/List.vue` | 闲鱼引流卡片 |
| 闲鱼卡片统计 | `/xianyu/stats` | `xianyuCard.js` | `views/xianyuCard/Stats.vue` | 统计 |
| 闲鱼卡片详情统计 | `/xianyu-card-stats/:id` | `xianyuCard.js` | `views/xianyuCard/CardStats.vue` | 单卡统计 |
| 闲鱼自动回复 | `/xianyu/auto-reply` | `xianyuAutoReply.js` | `views/xianyuCard/AutoReply.vue` | 自动回复（独立 API） |

## 六、社媒运营（community）

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 社群管理 | `/community/list` | `community.js` | `views/community/List.vue` | 管理社群分组及成员 |
| TG 机器人 | `/telegram` → `/telegram/account` | `telegram.js` | `views/telegram/account.vue` | TG 机器人账号 + Webhook + AI 绑定 |
| 飞书账号 | `/feishu` → `/feishu/account` | `feishu.js` | `views/feishu/FeishuAccount.vue` | 飞书应用账号与 Webhook |
| WhatsApp 社群 | `/whatsapp` → `/whatsapp/account` | `whatsapp.js` | `views/whatsapp/WhatsappAccount.vue` | WhatsApp 账号 + AI 绑定 |
| WhatsApp 草稿箱 | `/whatsapp/drafts` | `whatsapp.js` | `views/whatsapp/WhatsappDrafts.vue` | 草稿 |
| WhatsApp 群发 | `/whatsapp/jobs` | `whatsapp.js` | `views/whatsapp/WhatsappJobs.vue` | 发送任务 |
| WhatsApp 批量消息 | `/whatsapp/group-messaging` | `bulkMessaging.js` | `views/whatsappBot/BulkMessaging.vue` | 基于模板+账号批量发送 |
| 从线索库选择群体 | `/whatsapp/lead-group-selection` | `bulkMessaging.js` | `views/whatsappBot/LeadGroupSelection.vue` | 群体选择 |
| WhatsApp Cloud (Meta) | `/whatsapp-cloud/account` | `whatsappCloud.js` | `views/whatsappCloud/WhatsappCloudAccount.vue` | Meta 商业 API 接入 |
| 钉钉应用 | `/dingtalk-app/account` | `dingtalkApp.js` | `views/dingtalkApp/DingtalkAppAccount.vue` | 企业内部应用，支持回调收消息 |
| 域名池管理 | `/domainPool` | `domainPool.js` | `views/domainPool/List.vue` | 短链/落地页可用域名检测 |
| 坐席状态 | `/customerService/agentStatus` | `customerService.js` | `views/customerService/AgentStatus.vue` | 客服在线状态/会话数/接待上限 |
| AI 建议 | `/customerService/aiSuggestion` | `customerService.js` | `views/customerService/AISuggestion.vue` | AI 对客服的回复建议 |
| 快捷回复 | `/customerService/quickReply` | `customerService.js` | `views/customerService/QuickReply.vue` | 按分类维护常用话术 |
| 会话标签 | `/customerService/sessionTag` | `customerService.js` | `views/customerService/SessionTag.vue` | 维护会话标签体系 |

## 七、分析洞察（analytics / dataAnalysis）

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| AI 产能分析 | `/aiProductivity/list` | `aiProductivity.js` | `views/aiProductivity/List.vue` | 对话量/转化率/响应时长/销冠画像多维统计 |
| 转化漏斗 | `/conversionFunnel/list` | `conversionFunnel.js` | `views/conversionFunnel/List.vue` | 定义漏斗阶段并分析转化率、流失与时间趋势 |
| 客户旅程大屏 | `/customerJourney/dashboard` | `customerJourney.js` | `views/customerJourney/Dashboard.vue` | 9 阶段客户旅程实时监控 + 转化漏斗 + 沉睡客户检测 |
| 数据大屏 | `/dashboardScreen/list` | `dashboardScreen.js` | `views/dashboardScreen/List.vue` | 可全屏展示的营销 KPI 数据大屏（SSE 实时推送） |
| 自定义报表 | `/customReport/list` | `customReport.js` | `views/customReport/List.vue` | 自定义维度/指标生成业务报表 |
| A/B 实验 | `/abExperiment/list` | `abExperiment.js` | `views/abExperiment/List.vue` | 创建并管理 A/B 实验，对比方案效果（含多臂老虎机流量分配） |
| 流失预警 | `/churnPrediction/list` | `churnPrediction.js` | `views/churnPrediction/List.vue` | 基于机器学习预测用户流失风险并提前干预 |
| 客户 RFM（统计） | —（API 在 userSegment.js） | `userSegment.js` | — | RFM 分群统计接口（用户分层页面调用） |
| 自定义报表导出 | —（按钮触发） | `customReport.js` | `views/customReport/List.vue` | Blob 下载 |

## 八、系统管理（system，仅 admin）

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 平台账号 | `/platformAccount/list` | `platformAccount.js` + `platform.js` | `views/platformAccount/List.vue` | 各平台授权账号管理 |
| 团队成员 | `/teamUser/list` | `systemUser.js` + `users.js` | `views/system/UserList.vue` + `views/teamUser/List.vue` | 团队成员管理 |
| 角色权限 | `/teamUser/role` | `role.js` + `permission.js` | `views/system/RoleList.vue` + `views/teamUser/Role.vue` | RBAC 角色与权限点 |
| 站点设置 | `/system/config` | `system.js` | `views/system/Config.vue` | 站点基础信息、SEO、客服备案 |
| 素材库 | `/system/material-library` | `material.js` | `views/system/MaterialLibrary.vue` | 图片/视频/音频/文档素材集中管理 |
| 监控 | `/system/monitor` | `system.js` | `views/system/Monitor.vue` | 服务器实时资源指标 |
| 存储配置 | `/system/obs-config` | `obs.js` | `views/system/ObsConfig.vue` | 对象存储（OSS/COS/七牛）配置 |
| 第三方对接 | `/integration/list` | `integration.js` | `views/integration/List.vue` | 第三方系统集成与 API 对接 |
| 操作日志 | `/operationLog/list` | `operationLog.js` | `views/operationLog/List.vue` | 审计追踪所有用户操作 |
| 安全审计 | `/securityAudit/list` | `securityAudit.js` | `views/securityAudit/List.vue` | 安全检查、风险评估、异常告警 |
| 备份恢复 | `/backup/list` | `backup.js` | `views/backup/List.vue` | 系统全量备份、自动调度、文件归档 |
| 使用引导 | `/system/guide` | `system.js` | `views/system/Guide.vue` | 新用户功能引导 |
| 权限面板 | —（系统子页） | `permission.js` | `views/system/PermissionPanel.vue` | 权限管理面板 |
| 授权许可 | —（嵌入页脚显示） | `license.js` | `layout/Layout.vue` | 授权到期信息展示 |

## 九、独立页 / 非主菜单

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 登录 | `/login` | `users.js` | `views/Login.vue` | 用户端登录入口 |
| 系统初始化 | `/setup` | `system.js` | `views/setup/InitSetup.vue` | 首次部署时初始化系统；3 步向导 |
| 个人资料 | `/profile` | `users.js` | `views/Profile.vue` | 当前登录账号信息 |
| 通知中心 | `/notifications` | —（独立模块） | `views/Notifications.vue` | 平台系统通知、版本公告 |
| OneID 列表 | `/oneid/list` | `oneid.js` | `views/oneid/List.vue` | 客户多渠道身份归一化 |
| OneID 冲突解决 | `/oneid/conflicts` | `oneid.js` | `views/oneid/Conflicts.vue` | 处理身份冲突 |
| 在线客服嵌入页 | `/chat/embed/:channel_ref` | `chat.js` + `chatPublic.js` | `views/chat/embed/Index.vue` → `ChatWindow.vue` | 第三方网站 iframe 加载；含子组件 ChatInput / ChatMessages / VisitorHeader |
| 404 兜底 | `/:pathMatch(.*)*` | — | `views/NotFound.vue` | 未匹配路由兜底；支持 `?status=403` 显示无权访问 |

## 十、实时功能（WebSocket / SSE）

| 功能 | 通道类型 | 端点 | API 文件 / 工具 | 使用场景 |
| --- | --- | --- | --- | --- |
| 坐席工作台 WebSocket | WebSocket | `/api/ws/agent?agent_id=&agent_name=&token=` | `utils/agentSocket.js` | 客服工作台、统一收件箱实时刷新；事件：`new_session` / `new_message` / `session_update` / `ai_suggestion` |
| 访客 WebSocket | WebSocket | `/api/ws/visitor?session_id=&visitor_id=&channel_id=&since_seq=` | `utils/chatSocket.js` | 嵌入聊天窗；事件：`welcome` / `offline_messages` / `message` / `agent_joined` / `session_closed` / `ai_typing` / `missed_ack` |
| 大屏 SSE 推送 | SSE（EventSource） | `/api/sse/*` | `views/dashboardScreen/List.vue` | 营销 KPI 数据大屏实时推送 |
| 消息中心 | —（HTTP 轮询/通知） | `/api/notifications/*` | `components/MessageNotification.vue` | 顶栏铃铛宿主；展示未读数与列表 |

### WebSocket 鲁棒性设计

| 维度 | AgentSocket | ChatSocket |
| --- | --- | --- |
| 重连策略 | 指数退避 2s→30s，封顶 10 次 | 指数退避 1s→30s，无最大次数（私域长连接） |
| seq 持久化 | `localStorage: agentSocket:lastSeq:{agentId}` | `sessionStorage: chatSocket:lastSeq:{sessionId}:{visitorId}` |
| 离线补发 | `lastSeq` 跟踪 + `seenSeqs` 去重 | `onopen` 自动 `resume(since_seq)` + `missed_ack` 帧 |
| ack 策略 | 批量合并 200ms 窗口 | 批量合并 200ms 窗口 |
| 心跳 | 25s `ping` | 25s `ping` |

## 十一、公域素材

| 功能 | 路由路径 | API 文件 | 主要视图 | 说明 |
| --- | --- | --- | --- | --- |
| 素材库（系统内） | `/system/material-library` | `material.js` | `views/system/MaterialLibrary.vue` | 图片/视频/音频/文档素材集中管理 |
| 模板市场 | `/templateMarket/list` | —（独立） | `views/templateMarket/List.vue` | 营销模板市场，可订阅行业模板 |
| 素材选择对话框 | —（组件） | `material.js` | `components/dialogs/MaterialSelectDialog.vue` | 跨页面素材选择器 |
| 公域素材管理 | —（嵌入页面） | `material.js` | — | 由 `useMaterialStore` 缓存素材/话题/分类列表 |

## 十二、API 模块完整清单

> 以下为 `src/api/` 下全部 79 个文件，按字母顺序排列。

| 序号 | API 文件 | 业务域 | 主要功能 |
| --- | --- | --- | --- |
| 1 | `abExperiment.js` | dataAnalysis | A/B 实验 |
| 2 | `aiAgent.js` | aiAgent | AI 智能体 |
| 3 | `aiProductivity.js` | analytics | AI 产能分析 |
| 4 | `assetBundle.js` | aiAgent | 资产包（低代码 Playground） |
| 5 | `assetMarket.js` | aiAgent | 资产市场 |
| 6 | `autoReply.js` | reach | 自动回复（抖音/快手/小红书） |
| 7 | `backup.js` | system | 备份恢复 |
| 8 | `batchOperation.js` | reach | 批量操作 |
| 9 | `bulkMessaging.js` | community | WhatsApp 批量消息 |
| 10 | `channelAgentBinding.js` | aiAgent | 渠道智能体绑定 |
| 11 | `chat.js` | customer | 客服会话与消息 |
| 12 | `chatChannel.js` | customer | 客服 Web Widget 渠道 |
| 13 | `chatPublic.js` | knowledge | 公开聊天 API（嵌入页） |
| 14 | `churnPrediction.js` | dataAnalysis | 流失预测 |
| 15 | `clue.js` | customer | 线索管理 |
| 16 | `community.js` | community | 社群管理 |
| 17 | `conversionFunnel.js` | analytics | 转化漏斗 |
| 18 | `customReport.js` | dataAnalysis | 自定义报表 |
| 19 | `customer360.js` | customer | 客户 360 |
| 20 | `customerEvent.js` | customer | 客户事件 |
| 21 | `customerJourney.js` | analytics | 客户旅程大屏 |
| 22 | `customerService.js` | community | 客服子功能（坐席/快捷回复/标签/AI建议） |
| 23 | `customerServiceAgent.js` | aiAgent | 客服智能体挂载 |
| 24 | `customerSession.js` | workspace | 客服会话 |
| 25 | `dashboardScreen.js` | dataAnalysis | 数据大屏 |
| 26 | `dialogueMemory.js` | aiAgent | 对话记忆 |
| 27 | `dingtalkApp.js` | community | 钉钉应用 |
| 28 | `domainPool.js` | system | 域名池管理 |
| 29 | `douyinCard.js` | reach | 抖音卡片 |
| 30 | `email.js` | reach | 邮件触达 |
| 31 | `feishu.js` | community | 飞书账号 |
| 32 | `glossary.js` | i18n | 术语表管理 |
| 33 | `i18nStats.js` | i18n | 多语言监控看板 |
| 34 | `integration.js` | system | 第三方对接 |
| 35 | `intentRecognition.js` | aiAgent | 意图识别 |
| 36 | `knowledge.js` | knowledge | 知识库（文档/检索/OpenAPI/统计） |
| 37 | `knowledgeBase.js` | knowledge | 知识库基础接口 |
| 38 | `knowledgeMerchant.js` | knowledge | 知识库商户接入（外部导入） |
| 39 | `kuaishouCard.js` | reach | 快手卡片 |
| 40 | `license.js` | system | 授权许可 |
| 41 | `livecode.js` | reach | 活码管理 |
| 42 | `llmRouting.js` | aiAgent | LLM 多模型路由 |
| 43 | `marketingFlow.js` | reach | 营销自动化 |
| 44 | `material.js` | system | 素材库 |
| 45 | `messageHub.js` | workspace | 消息中台 |
| 46 | `objection.js` | sales | 异议处理 |
| 47 | `obs.js` | system | 对象存储配置 |
| 48 | `oneid.js` | customer | OneID 身份归一化 |
| 49 | `operationLog.js` | system | 操作日志 |
| 50 | `permission.js` | system | 权限管理 |
| 51 | `persona.js` | sales | 销冠画像 |
| 52 | `platform.js` | system | 平台账号基础 |
| 53 | `platformAccount.js` | system | 平台账号管理 |
| 54 | `ragProductConfig.js` | knowledge | RAG 产品配置 |
| 55 | `reachPipeline.js` | reach | 触达 Pipeline |
| 56 | `role.js` | system | 角色管理 |
| 57 | `scriptTemplate.js` | aiAgent | 话术模板 |
| 58 | `securityAudit.js` | system | 安全审计 |
| 59 | `shortLink.js` | reach | 短链管理 |
| 60 | `sms.js` | reach | 短信触达 |
| 61 | `sopAgent.js` | aiAgent | 销冠 SOP 智能体 |
| 62 | `stats.js` | dataAnalysis | 通用统计 |
| 63 | `system.js` | system | 系统配置 |
| 64 | `systemUser.js` | system | 团队成员 |
| 65 | `tagSegmentation.js` | customer | 标签分层 |
| 66 | `telegram.js` | community | Telegram 机器人 |
| 67 | `tiktokAutoReply.js` | reach | TikTok 自动回复 |
| 68 | `tiktokCard.js` | reach | TikTok 卡片 |
| 69 | `tuning.js` | aiAgent | 置信度/拟人度/反馈学习 |
| 70 | `unifiedInbox.js` | workspace | 统一收件箱 |
| 71 | `unifiedMessage.js` | customer | 统一消息 |
| 72 | `userSegment.js` | customer | 用户分群 RFM |
| 73 | `users.js` | system | 用户（登录/资料/找回密码） |
| 74 | `wecomAccount.js` | workspace | 企微账号管理 |
| 75 | `whatsapp.js` | community | WhatsApp 账号 |
| 76 | `whatsappCloud.js` | community | WhatsApp Cloud (Meta) |
| 77 | `xianyuAutoReply.js` | reach | 闲鱼自动回复 |
| 78 | `xianyuCard.js` | reach | 闲鱼卡片 |
| 79 | `xiaohongshuCard.js` | reach | 小红书卡片 |

## 十三、高级功能模块说明

### 13.1 资产市场

- **入口**：`/asset-market` → `views/assetMarket/Market.vue`
- **能力**：浏览并试用平台资产（智能体角色 / 话术 / AB方案 / 工作流 / 行业SOP）
- **API**：`assetMarket.js`（list / detail / purchaseAsset / syncLog）
- **关联**：`assetBundle.js` 提供低代码 Playground 与商户编辑器

### 13.2 AB 实验

- **入口**：`/abExperiment/list` → `views/abExperiment/List.vue`
- **能力**：创建并管理 A/B 实验，对比方案效果，含多臂老虎机（MAB）流量分配
- **API**：`abExperiment.js`
- **联动**：与 `feedbackLoop` 面板共享 MAB 自适应分流逻辑

### 13.3 客户 RFM 分群

- **入口**：`/userSegment/list` → `views/userSegment/List.vue`
- **能力**：基于 RFM 模型分群，识别高价值客户；4 张概览卡（总用户/高价值/活跃/流失风险）+ RFM 矩阵 + 分群列表
- **API**：`userSegment.js`

### 13.4 流失预测

- **入口**：`/churnPrediction/list` → `views/churnPrediction/List.vue`
- **能力**：基于机器学习预测用户流失风险并提前干预；风险统计卡（高/中/低）+ 风险用户列表
- **API**：`churnPrediction.js`

### 13.5 客户旅程大屏

- **入口**：`/customerJourney/dashboard` → `views/customerJourney/Dashboard.vue`
- **能力**：9 阶段客户旅程实时监控 + 转化漏斗 + 沉睡客户检测；支持自动刷新
- **API**：`customerJourney.js`

### 13.6 OneID 客户身份归一化

- **入口**：`/oneid/list` + `/oneid/conflicts`
- **能力**：将客户多渠道身份（手机/邮箱/微信/抖音/小红书）归一为统一 ID；冲突解决
- **API**：`oneid.js`

### 13.7 多语言监控

- **入口**：`/i18n/dashboard`（`i18nStats` 模块）+ `/glossary/list`
- **能力**：术语表管理 + 多语言监控看板
- **API**：`glossary.js` + `i18nStats.js`

## 十四、视图目录完整清单（`src/views/`）

```text
src/views/
├── KnowledgeWorkspace/        # 知识库工作台（10 文件）
├── RagProductConfig/           # RAG 产品配置容器页（3 文件）
├── abExperiment/               # A/B 实验
├── aiAgent/                    # AI 智能体（List + Edit）
├── aiProductivity/             # AI 产能分析
├── assetBundle/                # 资产包（List + MerchantEditor + Playground）
├── assetMarket/                # 资产市场（Market + MyAssets + Detail + SyncLog）
├── backup/                     # 备份恢复
├── batchOperation/             # 批量操作
├── chat/                       # 嵌入聊天窗（embed/ 子目录：Index + ChatWindow + 子组件）
├── chatChannel/               # 客服 Web Widget 渠道（List + Create + Edit + InstallGuide）
├── churnPrediction/           # 流失预测
├── clue/                       # 线索（List + Statistics）
├── community/                  # 社群管理
├── confidence/                 # 置信度运营
├── conversionFunnel/          # 转化漏斗
├── customReport/              # 自定义报表
├── customer360/               # 客户 360
├── customerEvent/             # 客户事件
├── customerJourney/           # 客户旅程大屏
├── customerService/           # 客服子功能（4 文件）
├── customerSession/           # 客服会话
├── dashboardScreen/           # 数据大屏
├── dialogueMemory/            # 对话记忆
├── dingtalkApp/               # 钉钉应用
├── domainPool/                # 域名池
├── douyinCard/                # 抖音卡片（List + Stats + CardStats + AutoReply）
├── email/                      # 邮件（6 文件）
├── feedbackLoop/              # 反馈学习闭环
├── feishu/                    # 飞书账号
├── glossary/                  # 术语表
├── humanize/                  # 拟人度评估
├── i18n/                      # 多语言看板
├── integration/               # 第三方对接
├── intentRecognition/         # 意图识别
├── kuaishouCard/              # 快手卡片（4 文件）
├── livecode/                  # 活码管理
├── llmRouting/                # LLM 路由
├── marketingFlow/            # 营销自动化
├── messageHub/               # 消息中台
├── objection/                # 异议处理
├── oneid/                    # OneID 身份归一化（List + Conflicts）
├── operationLog/             # 操作日志
├── persona/                  # 销冠画像
├── platformAccount/          # 平台账号
├── reachPipeline/            # 触达 Pipeline
├── scriptTemplate/          # 话术模板
├── securityAudit/            # 安全审计
├── setup/                    # 系统系统初始化向导
├── shortLink/                # 短链（List + Stats）
├── sms/                      # 短信（4 文件）
├── sopAgent/                 # 销冠 SOP 智能体
├── system/                   # 系统管理（9 文件：Config/Guide/MaterialLibrary/Monitor/ObsConfig/PermissionPanel/RagOverview/RoleList/UserList）
├── tagSegmentation/          # 标签分层
├── telegram/                 # Telegram 机器人
├── tiktokCard/               # TikTok 卡片（4 文件）
├── unifiedInbox/             # 统一收件箱
├── unifiedMessage/           # 统一消息
├── userSegment/              # 用户分群 RFM
├── wecomAccount/             # 企微账号（List + Data）
├── whatsapp/                 # WhatsApp（3 文件）
├── whatsappBot/              # WhatsApp 批量消息（2 文件）
├── whatsappCloud/            # WhatsApp Cloud
├── xianyuCard/               # 闲鱼卡片（4 文件）
├── xiaohongshuCard/          # 小红书卡片（4 文件）
├── ForgotPassword.vue        # 忘记密码（独立）
├── Login.vue                 # 登录（独立）
├── NotFound.vue              # 404 兜底（独立）
├── Notifications.vue         # 通知中心（独立）
└── Profile.vue               # 个人资料（独立）
```

## 关联文档

- [ARCHITECTURE.md](./ARCHITECTURE.md) - 架构图
- [DEVELOPMENT.md](./DEVELOPMENT.md) - 代码开发手册
- [CONVENTIONS.md](./CONVENTIONS.md) - 代码规范
- [../../MENU_SPEC.md](../ui-inventory/MENU_SPEC.md) - 菜单页面规格清单（含每页字段/操作详细规格）
- [../ui-inventory/](../ui-inventory/) - UI 清单文件（按分组 JSON）

---

最近更新日期: 2026-07-26
