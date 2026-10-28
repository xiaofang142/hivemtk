package model

import "sort"

// ModuleMenuBindings 功能模块 ↔ 菜单节点归属表（功能模块化改造的事实源）
//
// 为什么放在 user-server 而不是平台端：SystemMenuTree 在这里，"某个 menu_key 是不是
// 目录节点 / 它在第几层"只有这里知道。平台端拿到的只是一串字符串，让它自己判断归属
// 会把一份事实抄成两份并各自漂移。
//
// 平台端 seed 由 scripts/gen-module-bindings.sh 从本文件导出
// （platform-server/seed/module_menu_bindings.json），两端各有一条测试钉死
// "208 个 menu_key 全覆盖且无重复"，任何一侧漏绑或双绑都会红。
//
// 归属粒度是**菜单节点（key）级**，不是顶级分组级：一个顶级分组下常混着免费与收费
// 节点（system 顶级全是基础；reach 顶级下 6 个目录全是收费；analytics 顶级下
// dashboardScreen 属 P11 而 confidencePanel 属 P2）。
//
// 归属原则：
//  1. category=base 的模块不可关闭，因此不能承载「关掉会让外部系统挂掉」的资源 ——
//     chatChannelList 是 embed-sdk 在客户网站上的凭据来源，归 core 而非 inbox。
//  2. 同一节点只能属于一个模块（跨模块共享用 depends_on 表达，不重复绑定）。
//  3. 目录节点也要绑定：目录被过滤掉时其子路由才不会被调到（FilterMenuTree 的剪枝
//     语义依赖父节点存在）。

// ModuleKey 功能模块标识。与 platform-server 的 module_catalog.module_key 一一对应。
type ModuleKey = string

// 基础模块（免费常开，不可关闭）
const (
	ModuleCore         = "core"
	ModuleCustomer     = "customer"
	ModuleInbox        = "inbox"
	ModuleKnowledge    = "knowledge"
	ModuleAsset        = "asset"
	ModuleLLMSelf      = "llm-self"
	ModulePlatformLink = "platform-link"
)

// 收费模块（需开通；price_cents=0 表示需开通但不收费）
const (
	ModuleAIAgent           = "ai-agent"
	ModuleAISales           = "ai-sales"
	ModuleLLMCloud          = "llm-cloud"
	ModuleReachAuto         = "reach-auto"
	ModuleReachSMS          = "reach-sms"
	ModuleReachEmail        = "reach-email"
	ModuleReachSocial       = "reach-social"
	ModuleReachLink         = "reach-link"
	ModuleIMWecom           = "im-wecom"
	ModuleIMIntl            = "im-intl"
	ModuleAnalytics         = "analytics"
	ModuleExperiment        = "experiment"
	ModuleBrowserAutomation = "browser-automation"
	ModuleGEO               = "geo"
	ModuleSalesCRM          = "sales-crm"
	ModuleApproval          = "approval"
	ModuleI18N              = "i18n"
)

// ModuleCategory 模块分类
const (
	ModuleCategoryBase = "base"
	ModuleCategoryPaid = "paid"
)

// BillingCycle 计费周期（与平台端 model.BillingCycle 同名同义）。
// 平台端枚举必须与本表同批扩展，否则 gen-module-bindings 重生成的种子
// 会在平台端枚举校验处被拒（003 T004）。
const (
	BillingCycleYearly  = "yearly"  // 年费
	BillingCycleMetered = "metered" // 计量（token 钱包，按量扣减）
	BillingCycleMonthly = "monthly" // 月订阅（软件订阅 ¥149/月，003 R3）
	BillingCycleOnce    = "once"    // 买断（永不写 ExpiresAt，003 R3）
)

// ModuleMeta 模块元数据（名称/分类/默认定价/描述）。展示文案以 i18n 为准，
// 这里的 Summary 是「平台端市场卡片 + 用户端市场卡片」共用的兜底文案。
type ModuleMeta struct {
	Key          string
	Name         string
	Category     string
	PriceCents   int64
	BillingCycle string // BillingCycleYearly | BillingCycleMetered | BillingCycleMonthly | BillingCycleOnce
	Icon         string
	Summary      string
	DependsOn    []string
}

// ModuleCatalogMeta 24 个模块的元数据。顺序即平台端市场默认排序。
//
// 定价口径：
// 7 个基础模块 0 元免费常开（不可关闭）；17 个收费模块统一 ¥1（100 分，
// 需开通、按 order_type=module 下单开通；浏览器自动化是 BillingCycleOnce
// 买断，不设 ExpiresAt，本地无支付通道 → 保持不可购不可用；llm-cloud 同为
// ¥1 开通 + metered 按量预付费）。平台端为运行时定价权威，改价经模块同步下发
// （MarketList/MarketDetail 读镜像优先），本表只是编译期种子与离线兜底。
// 改价后必须同步：运行 go run ./cmd/gen-module-bindings 重生成平台端 seed
// （config/ 与 internal/service/ 两份），并更新 website/src/config/content.js。
var ModuleCatalogMeta = []ModuleMeta{
	// ---- 基础模块（7）----
	{Key: ModuleCore, Name: "系统基座", Category: ModuleCategoryBase, Icon: "Setting",
		Summary: "账号权限、站点配置、审计与备份，系统可运行的底线"},
	{Key: ModuleCustomer, Name: "客户中心", Category: ModuleCategoryBase, Icon: "UserFilled",
		Summary: "线索、客户 360、标签分层、RFM 分群与 OneID 身份归一", DependsOn: []string{ModuleCore}},
	{Key: ModuleInbox, Name: "会话工作台", Category: ModuleCategoryBase, Icon: "Monitor",
		Summary: "统一收件箱、客服会话、坐席状态与快捷回复", DependsOn: []string{ModuleCore, ModuleCustomer}},
	{Key: ModuleKnowledge, Name: "知识库", Category: ModuleCategoryBase, Icon: "Collection",
		Summary: "文档入库、分段检索、RAG 评测与对外 API Token", DependsOn: []string{ModuleCore}},
	{Key: ModuleAsset, Name: "资产与话术", Category: ModuleCategoryBase, Icon: "Box",
		Summary: "资产市场、资产包 Playground 与销冠话术库", DependsOn: []string{ModuleCore, ModuleKnowledge}},
	{Key: ModuleLLMSelf, Name: "自有模型接入", Category: ModuleCategoryBase, Icon: "Cpu",
		Summary: "用自己的 API Key 接入大模型，用量与成本可视化，不额外计费", DependsOn: []string{ModuleCore}},
	{Key: ModulePlatformLink, Name: "平台对接", Category: ModuleCategoryBase, Icon: "Connection",
		Summary: "平台心跳、功能模块下发与开通状态同步；离线部署时全部基础能力直接可用", DependsOn: []string{ModuleCore}},

	// ---- 收费模块（17）----
	{Key: ModuleAIAgent, Name: "AI 智能体", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "ChatDotRound",
		Summary:   "7×24 自动应答真实客户消息，可绑定不同模型、知识库与 SOP",
		DependsOn: []string{ModuleCore, ModuleCustomer, ModuleInbox, ModuleKnowledge, ModuleLLMSelf}},
	{Key: ModuleAISales, Name: "AI 销冠", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "Trophy",
		Summary:   "意图识别、异议应对、销冠画像与 SOP 自动推进成交",
		DependsOn: []string{ModuleCore, ModuleCustomer, ModuleInbox, ModuleLLMSelf, ModuleAIAgent}},
	{Key: ModuleLLMCloud, Name: "平台模型算力", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleMetered, Icon: "Cpu",
		Summary:   "不用自己申请 Key，用平台垫资的大模型；按 token 预付费，输入输出分别计价",
		DependsOn: []string{ModuleCore, ModuleLLMSelf}},
	{Key: ModuleReachAuto, Name: "营销自动化", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "Operation",
		Summary: "线索进来自动跑多轮触达与工作流编排", DependsOn: []string{ModuleCore, ModuleCustomer}},
	{Key: ModuleReachSMS, Name: "短信触达", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "ChatDotSquare",
		Summary: "国内短信发送、任务调度与退订管理", DependsOn: []string{ModuleCore, ModuleCustomer, ModuleReachAuto}},
	{Key: ModuleReachEmail, Name: "邮件触达", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "Message",
		Summary: "邮件列表、SMTP、草稿任务与送达率追踪", DependsOn: []string{ModuleCore, ModuleCustomer, ModuleReachAuto}},
	{Key: ModuleReachSocial, Name: "社媒引流卡片", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "VideoPlay",
		Summary:   "抖音 / 快手 / 小红书 / 闲鱼 / TikTok 引流卡片与自动私信回复",
		DependsOn: []string{ModuleCore, ModuleCustomer, ModuleReachAuto}},
	{Key: ModuleReachLink, Name: "短链活码", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "Link",
		Summary: "可追踪短链、活码投放与域名池健康检测", DependsOn: []string{ModuleCore}},
	{Key: ModuleIMWecom, Name: "企业微信", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "Promotion",
		Summary: "企微账号托管、客户群运营与健康度风控", DependsOn: []string{ModuleCore, ModuleCustomer, ModuleInbox}},
	{Key: ModuleIMIntl, Name: "海外与协作 IM", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "ChatDotRound",
		Summary:   "WhatsApp / Telegram / QQ / 飞书 / 钉钉 / 社群 多通道收发",
		DependsOn: []string{ModuleCore, ModuleCustomer, ModuleInbox}},
	{Key: ModuleAnalytics, Name: "数据分析", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "DataAnalysis",
		Summary: "数据大屏、转化漏斗、AI 产能与自定义报表", DependsOn: []string{ModuleCore, ModuleCustomer, ModuleInbox}},
	{Key: ModuleExperiment, Name: "实验与预测", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "TrendCharts",
		Summary: "A/B 实验（含多臂老虎机）与流失预警", DependsOn: []string{ModuleCore, ModuleCustomer, ModuleAnalytics}},
	// 浏览器自动化是唯一有价模块：¥99 买断（003 R3 用户裁决）。离线同锁。
	{Key: ModuleBrowserAutomation, Name: "浏览器自动化", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleOnce, Icon: "Cpu",
		Summary: "常驻浏览器接管真实客户端做私信与评论", DependsOn: []string{ModuleCore, ModuleCustomer}},
	{Key: ModuleGEO, Name: "GEO 智能优化", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "MagicStick",
		Summary:   "AI 搜索可见性优化：关键词蒸馏、内容生成、收录与 SOV 监控",
		DependsOn: []string{ModuleCore, ModuleKnowledge}},
	{Key: ModuleSalesCRM, Name: "销售工作台", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "Notebook",
		Summary: "销售驾驶舱、跟进、商机报价与回款闭环", DependsOn: []string{ModuleCore, ModuleCustomer, ModuleAISales}},
	{Key: ModuleApproval, Name: "审批与待办", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "Tickets",
		Summary: "待办中心、Bad Case 队列与人工任务分派", DependsOn: []string{ModuleCore}},
	{Key: ModuleI18N, Name: "多语言管理", Category: ModuleCategoryPaid, PriceCents: 100, BillingCycle: BillingCycleYearly, Icon: "Coin",
		Summary: "术语表与多语言监控看板", DependsOn: []string{ModuleCore}},
}

// BaseModuleKeys 全部基础模块 key（顺序即开通顺序依赖）。
func BaseModuleKeys() []string {
	out := make([]string, 0, 8)
	for _, m := range ModuleCatalogMeta {
		if m.Category == ModuleCategoryBase {
			out = append(out, m.Key)
		}
	}
	return out
}

// PaidModuleKeys 全部收费模块 key。
func PaidModuleKeys() []string {
	out := make([]string, 0, 24)
	for _, m := range ModuleCatalogMeta {
		if m.Category == ModuleCategoryPaid {
			out = append(out, m.Key)
		}
	}
	return out
}

// ModuleDependsOn 取模块声明的直接依赖。
func ModuleDependsOn(key string) []string {
	for _, m := range ModuleCatalogMeta {
		if m.Key == key {
			return m.DependsOn
		}
	}
	return nil
}

// ModuleMenuBindings 模块 → 菜单节点清单。**手工维护，测试钉死全覆盖**。
//
// 键的排序按 SystemMenuTree 的先序遍历顺序书写，方便人工比对；测试不依赖顺序。
var ModuleMenuBindings = map[string][]string{
	// ========== core：系统基座（28 节点）==========
	ModuleCore: {
		// system 顶级目录及其下的站点配置
		"system", "siteConfig",
		"systemConfig", "systemObsConfig", "systemMaterialLibrary", "systemMonitor",
		"systemTrace", "systemGuide", "automationCenter", "dynamicThreshold",
		"systemDictionaries", "systemFeatureFlags", "ltcConfig", "opsOverview",
		// 团队
		"teamGroup", "systemUser", "roleManage", "permissionManage",
		// 权限设置
		"permissionGroup", "platformAccount", "integration", "bridgeToken",
		// bridgeExtension（扩展安装包）与 bridgeToken 同属「桥接」这一摊，
		// 同为系统基座能力（core）。漏绑会让它落进 enabledForNode 的
		// 「未绑定 ⇒ 永远放行」分支 —— 模块闸对它完全失效。
		"bridgeExtension",
		"operationLog", "securityAudit",
		// AI 观测与渠道健康：admin 只读看板，与 systemMonitor/systemTrace 同族，归 core
		"observabilityDashboard",
		// 备份
		"backupList", "backupEnhanced",
		// 客服 Web Widget 渠道（embed-sdk 在客户网站上的凭据来源，见文件头原则 1）
		"chatChannelList",
	},

	// ========== customer：客户中心（12 节点）==========
	ModuleCustomer: {
		"customer", "clueList", "clueStatistics", "leadMining",
		"customer360", "customerEvent", "tagSegmentation", "userSegment", "unifiedMessage",
		"customerIdentity", "oneidList", "oneidConflicts", "oneidMergeRules",
	},

	// ========== inbox：会话工作台（12 节点）==========
	ModuleInbox: {
		"workspace",
		"inbox", "messageHub", "messageHubDashboard",
		"customerSession",
		"agentWorkbench", "agentStatus", "csatDashboard", "quickReply", "sessionTag", "aiSuggestion",
	},

	// ========== knowledge：知识库（16 节点）==========
	ModuleKnowledge: {
		"knowledge", "knowledgeWorkspace",
		"knowledgeManagement", "knowledgeBatchImport", "knowledgePlayground",
		"knowledgeFeedbacks", "knowledgeTokens", "knowledgeExternal", "knowledgeStatistics",
		"knowledgeRagEval", "knowledgeOpenAPI", "knowledgeConnectors",
		"ragProductConfig", "ragMainConfig", "ragAccount", "ragProduct", "ragOverview",
		"faqKb",
	},

	// ========== asset：资产与话术（6 节点）==========
	ModuleAsset: {
		"assetBundle", "assetMarket", "myAssets", "syncLog",
		"assetBundleList", "assetBundlePlayground", "assetBundleMerchant",
		"scriptTemplate", "scriptTemplateAbStats",
	},

	// ========== llm-self：自有模型接入（1 节点）==========
	ModuleLLMSelf: {
		"llmRouting",
	},

	// ========== platform-link：平台对接（3 菜单节点）==========
	// 功能模块市场 / 我的模块 / 模块订单。
	//
	// 它们归 platform-link（基础模块，恒开）而不是任何付费模块，理由是
	// **购买入口不能被购买条件关闭**：关掉某模块的闸，用户就找不到「去开通」的地方，
	// 而未开通模块的人恰恰最需要这个入口。这与「菜单绑定用于过闸」的常规用法反着来，
	// 注释写在这里以免后人"顺手修正"成绑到某个付费模块。
	ModulePlatformLink: {
		"moduleCenterGroup", "moduleCenter", "myModules", "moduleOrders",
		// 模板/技能市场（F2，specs/013）：目录是编译期种子、接口是公开橱窗
		// （/api/template-market/* 无需登录），页面本身不绑定任何付费权益。
		// 归 platform-link（基础模块，恒开）与上面的市场入口同一条理由：
		// 绑定到某个付费模块后，未开通的用户会连带看不到「有哪些现成能力可用」，
		// 而这份目录恰恰是他们决定买什么的前置信息。
		"templateMarketGroup", "templateMarket",
	},

	// ========== llm-cloud：平台模型算力（0 菜单节点）==========
	// llm-cloud 没有独立菜单页：它的入口是 LLM 页面的「平台套餐」Tab 与顶部余额条，
	// 而 llmRouting 节点归 llm-self。本模块只管钱包与计量，不挡任何菜单节点，
	// 但**接口层仍过闸**（/api/llm/wallet、/api/llm/plans 等按本 key 拦截）。
	ModuleLLMCloud: {},

	// ========== ai-agent：AI 智能体（5 节点）==========
	ModuleAIAgent: {
		"aiAgent", "agentManage", "aiAgentList", "aiAgentCreate", "aiToolManagement",
		// aiCustomIntegration（工具接入）：P1-3 的自定义工具/MCP server 配置面，
		// 与 aiToolManagement 同一装配链（"AI 能调用什么"），随 ai-agent 开关。
		"aiCustomIntegration",
	},

	// ========== ai-sales：AI 销冠（16 节点）==========
	ModuleAISales: {
		"agentPassive", "intentRecognition", "dialogueMemory",
		"agentSupport", "sopAgent", "sopTemplate", "sopTemplateMarket", "knowledgeBase",
		"agentSalesIntel", "objectionList", "personaList",
		"confidencePanel", "humanizePanel", "feedbackLoopPanel",
	},

	// ========== reach-auto：营销自动化（5 节点）==========
	ModuleReachAuto: {
		"reach", // 顶级目录：由全部触达/IM 收费模块共同持有（目录节点允许多绑定）
		"agentActive", "reachPipeline", "marketingFlow", "batchOperation",
		"workflowOrchestrator",
	},

	// ========== reach-sms：短信触达（5 节点）==========
	ModuleReachSMS: {
		"smsReach", "smsList", "smsDrafts", "smsJobs", "smsConfig",
	},

	// ========== reach-email：邮件触达（8 节点）==========
	ModuleReachEmail: {
		"emailReach", "emailList", "emailDrafts", "emailJobs",
		"emailSmtp", "emailInfo", "emailDeliverability", "emailGuide",
	},

	// ========== reach-social：社媒引流卡片（15 节点）==========
	ModuleReachSocial: {
		"douyin", "douyinCard", "douyinStats",
		"kuaishou", "kuaishouCard", "kuaishouStats",
		"xiaohongshu", "xiaohongshuCard", "xiaohongshuStats",
		"xianyu", "xianyuCard", "xianyuStats",
		"tiktok", "tiktokList", "tiktokStats",
	},

	// ========== reach-link：短链活码（4 节点）==========
	ModuleReachLink: {
		"shortLink", "shortLinkList", "shortLinkStats", "livecode",
		"domainPool",
	},

	// ========== im-wecom：企业微信（1 节点）==========
	ModuleIMWecom: {
		"wecomAccount",
	},

	// ========== im-intl：海外与协作 IM（12 节点）==========
	ModuleIMIntl: {
		"agentChannel", "whatsappCloud", "dingtalkApp",
		"telegram", "telegramAccount",
		"qq", "qqAccount",
		"feishu", "feishuAccount",
		"whatsapp", "whatsappAccount", "whatsappDrafts", "whatsappJobs",
		"whatsappLeadGroup", "whatsappBulkMatrix", "whatsappGroupMessaging",
		"community",
	},

	// ========== analytics：数据分析（7 节点）==========
	ModuleAnalytics: {
		"analytics", "dashboardScreen", "conversionFunnel", "aiProductivity",
		"customReport", "customerJourney", "cohortPath",
	},

	// ========== experiment：实验与预测（2 节点）==========
	ModuleExperiment: {
		"abExperiment", "churnPrediction",
	},

	// ========== browser-automation：浏览器自动化（10 节点）==========
	ModuleBrowserAutomation: {
		"browserAutomation", "baTasks", "baCreate", "baCron", "baStatus",
		"egoFlows", "egoSessions", "egoExecutions", "egoRunner", "egoAssets", "egoAssetAdmin",
	},

	// ========== geo：GEO 智能优化（30 节点）==========
	ModuleGEO: {
		"geoTools", "geoConfigGroup",
		"geoBrandConfig", "geoSeoInfra", "geoPusherConfig", "geoSiteConfig",
		"geoSchemaTemplates", "geoCompetitors", "geoConfig",
		"geoProd", "geoKeywordMining", "geoContentCreation", "geoContentOptimize",
		"geoKnowledgeBase", "geoEntityGraph",
		"geoPublish", "geoSitePublish", "geoPushCenter", "geoPlatformPublish", "geoWorkflow",
		"geoMonitor", "geoFunnelDashboard", "geoIndexTracking", "geoVisibilityBoard",
		"geoSovBoard", "geoCrawlerStats", "geoVerification", "geoAlerts", "geoReports",
		"geoDecisionReport",
	},

	// ========== sales-crm：销售工作台（4 节点）==========
	ModuleSalesCRM: {
		"salesCockpit", "orderDraft", "salesWorkbench", "followups",
	},

	// ========== approval：审批与待办（2 节点）==========
	ModuleApproval: {
		"approvalTask", "badCase",
	},

	// ========== i18n：多语言管理（3 节点）==========
	ModuleI18N: {
		"i18nManage", "glossary", "i18nDashboard",
	},
}

// MenuKeyOwner 反查：menu_key → module_key。启动时构建，请求路径零扫描。
var MenuKeyOwner = buildMenuKeyOwner()

func buildMenuKeyOwner() map[string]string {
	owner := make(map[string]string, 256)
	for mk, keys := range ModuleMenuBindings {
		for _, k := range keys {
			owner[k] = mk
		}
	}
	return owner
}

// OwnerOfMenuKey 返回该菜单节点所属模块；未绑定返回空串。
func OwnerOfMenuKey(menuKey string) string {
	return MenuKeyOwner[menuKey]
}

// ModuleEnabledFilter 是菜单过滤用的「已开通模块」判定函数签名。
//
// 注入而非直连：model 包不能依赖 repository/service（会被 middleware 反向 import
// 成环）。cmd/api/main.go 在装配阶段注入真实实现；未注入时返回 nil，
// FilterMenuTreeByModule 视为「无限制」，保证测试与离线首启不被空实现卡死。
var ModuleEnabledFilter func(moduleKey string) bool

// SetModuleEnabledFilter 注入模块启用判定（装配期调用一次）。
func SetModuleEnabledFilter(fn func(moduleKey string) bool) { ModuleEnabledFilter = fn }

// FilterMenuTreeByModule 先按「模块是否开通」裁菜单，再交给角色授权裁剪。
//
// 为什么必须先裁模块：FilterMenuTree 对未命中 key 的节点会剪掉整条分支。若把
// 「未开通模块的菜单 key」当作普通未授权处理，admin（恒全量，不落库）就绕过了模块闸 ——
// 菜单还看得见、点进去才 403，正是 ADR 里点名要避免的症状。
func FilterMenuTreeByModule(tree []SystemMenuNode, enabled func(moduleKey string) bool) []SystemMenuNode {
	if enabled == nil {
		enabled = ModuleEnabledFilter
	}
	if enabled == nil {
		return tree
	}
	var walk func(nodes []SystemMenuNode, parentOK bool) []SystemMenuNode
	walk = func(nodes []SystemMenuNode, parentOK bool) []SystemMenuNode {
		out := make([]SystemMenuNode, 0, len(nodes))
		for _, n := range nodes {
			ok := parentOK && (n.Path == "" || enabledForNode(n, enabled))
			children := walk(n.Children, ok)
			if !ok && len(children) == 0 {
				continue
			}
			n.Children = children
			out = append(out, n)
		}
		return out
	}
	return walk(tree, true)
}

// enabledForNode 判定单节点：目录节点看子节点是否还有活的（子节点在 walk 里已算），
// 有 Path 的叶子看自己归属的模块。
//
// 未绑定任何模块的节点默认放行 —— 它们是「不属于任何可售模块」的系统骨架节点
// （例如未来新增的内部页）。漏绑由 TestModuleMenuBindingsCoverAllMenuKeys 在 CI 兜住。
func enabledForNode(n SystemMenuNode, enabled func(string) bool) bool {
	mk := MenuKeyOwner[n.Key]
	if mk == "" {
		return true
	}
	return enabled(mk)
}

// ClosureOfModules 返回某模块的依赖闭包（含自身）。
//
// 一层层展开即可：ModuleCatalogMeta 的 DependsOn 已手工排过序，
// 递归反而让"环"这条防线更难写（虽然写入侧本就不允许改）。
func ClosureOfModules(key string) []string {
	seen := map[string]bool{}
	out := []string{}
	var walk func(string)
	walk = func(k string) {
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, k)
		for _, d := range ModuleDependsOn(k) {
			walk(d)
		}
	}
	walk(key)
	sort.Strings(out)
	return out
}

// ModuleOptionRow 子选项（平台端下发；本地只展示，不定价）
type ModuleOptionRow struct {
	OptionKey       string `json:"option_key"`
	OptionName      string `json:"option_name"`
	PriceDeltaCents int64  `json:"price_delta_cents"`
	Enabled         bool   `json:"enabled"`
}

// OwnerModuleCategory 返回模块分类；未知模块返回空串。
func OwnerModuleCategory(key string) string {
	for _, m := range ModuleCatalogMeta {
		if m.Key == key {
			return m.Category
		}
	}
	return ""
}

// OwnerModuleMeta 返回模块元数据指针；不存在返回 nil。
func OwnerModuleMeta(key string) *ModuleMeta {
	for i := range ModuleCatalogMeta {
		if ModuleCatalogMeta[i].Key == key {
			return &ModuleCatalogMeta[i]
		}
	}
	return nil
}
