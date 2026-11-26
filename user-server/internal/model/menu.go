package model

import "time"

// SystemMenuNode 系统菜单树节点（RBAC 菜单权限的权威注册表）
//
// GET /api/menus 下发本注册表，经模块与角色授权裁剪后供前端直接渲染：
//   - Key   节点唯一键（前端 i18n 菜单标题用 t("menu."+Key)、图标用 Key→icon 映射）
//   - Path  叶子/带页目录的路由路径（空表示纯目录）
//   - Roles 角色授权上界，祖先限制同时约束后代；为空表示不额外限制
//     （DefaultMenuKeys 用于首次分配，存量分配仍须与 EligibleMenuKeys 取交集）
type SystemMenuNode struct {
	Key      string           `json:"key"`
	Title    string           `json:"title"`
	Icon     string           `json:"icon,omitempty"`
	Path     string           `json:"path,omitempty"`
	Roles    []string         `json:"roles,omitempty"`
	Children []SystemMenuNode `json:"children,omitempty"`
}

// RoleMenuPermission 角色↔菜单分配表（customer_service/staff 两档存行，admin 恒全量不落库）
type RoleMenuPermission struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint      `json:"id" gorm:"primaryKey"`
	RoleCode  string    `json:"role_code" gorm:"size:32;uniqueIndex:idx_role_menu_unique,priority:1"`
	MenuKey   string    `json:"menu_key" gorm:"size:64;uniqueIndex:idx_role_menu_unique,priority:2"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定表名
func (RoleMenuPermission) TableName() string {
	return "role_menu_permissions"
}

// SystemMenuTree 系统菜单树（权威注册表：菜单树已改为后端驱动，GET /api/menus 下发）
//
// 变更时必须同步检查前端 src/router/modules/*.js 的路由 path 字面量：path 对不上时
// 点菜单会进 NotFound、面包屑也不显示，而两边分处两个目录两个语言，没有编译器会替你
// 发现。前端 tests/unit/menuRegistry-helper.js 会读本文件，前端
// tests/unit/router_*.test.js 用它把每个菜单 path 与对应路由模块钉在一起。
var SystemMenuTree = []SystemMenuNode{
	{
		Key: "workspace", Title: "工作台", Icon: "Monitor",
		Roles: []string{"admin", "viewer", "staff"},
		Children: []SystemMenuNode{
			{Key: "inbox", Title: "统一收件箱", Icon: "Box", Path: "/inbox/list"},
			{Key: "approvalTask", Title: "待办中心", Icon: "Tickets", Path: "/approvalTask/list"},
			{Key: "badCase", Title: "Bad Case 队列", Icon: "Warning", Path: "/badCase/list"},
		},
	},
	{
		Key: "customer", Title: "客户与销售", Icon: "UserFilled",
		Roles: []string{"admin", "viewer", "staff"},
		Children: []SystemMenuNode{
			{Key: "customer360", Title: "客户 360", Icon: "UserFilled", Path: "/customer360/list"},
			{Key: "customerConsents", Title: "客户同意与偏好", Icon: "DocumentChecked", Path: "/customer360/consents"},
			{Key: "customerImportJob", Title: "客户导入任务", Icon: "UploadFilled", Path: "/customerImportJob/list", Roles: []string{"admin"}},
			{Key: "aiInsightDraft", Title: "AI洞察草稿", Icon: "MagicStick", Path: "/aiInsightDraft/list", Roles: []string{"admin"}},
			{Key: "crossAccountSearch", Title: "跨账号客户检索", Icon: "Search", Path: "/crossAccountSearch/list", Roles: []string{"admin"}},
			{Key: "customerEvent", Title: "客户事件", Icon: "Bell", Path: "/customerEvent/list"},
			{Key: "tagSegmentation", Title: "标签分层", Icon: "PriceTag", Path: "/tagSegmentation/list", Roles: []string{"admin", "viewer"}},
			{Key: "userSegment", Title: "用户分层 RFM", Icon: "PieChart", Path: "/userSegment/list", Roles: []string{"admin", "viewer"}},
			{Key: "crmMaster", Title: "客户主数据", Icon: "User", Path: "/crm/list", Roles: []string{"admin"}},
			{Key: "salesCockpit", Title: "销售驾驶舱", Icon: "DataLine", Path: "/sales-cockpit", Roles: []string{"admin"}},
			{Key: "orderDraft", Title: "订单草稿", Icon: "Document", Path: "/dashboard/drafts", Roles: []string{"admin"}},
			{Key: "salesWorkbench", Title: "销售工作台", Icon: "Notebook", Path: "/sales-workbench", Roles: []string{"admin"}},
			{Key: "followups", Title: "今日跟进", Icon: "AlarmClock", Path: "/followups/today", Roles: []string{"admin"}},
			{
				Key: "agentSalesIntel", Title: "销售智能", Icon: "Trophy",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "objectionList", Title: "异议处理", Icon: "ChatLineRound", Path: "/objection/list"},
					{Key: "personaList", Title: "销冠画像", Icon: "UserFilled", Path: "/persona/list"},
				},
			},
			{Key: "commerceOperations", Title: "电商操作台", Icon: "Shop", Path: "/commerce/operations", Roles: []string{"admin"}},
			{
				Key: "customerIdentity", Title: "客户身份", Icon: "Connection",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "oneidList", Title: "OneID 列表", Icon: "List", Path: "/oneid/list"},
					{Key: "oneidConflicts", Title: "身份冲突解决", Icon: "Warning", Path: "/oneid/conflicts"},
					{Key: "oneidMergeRules", Title: "合并规则配置", Icon: "Setting", Path: "/oneid/merge-rules"},
				},
			},
		},
	},
	{
		// 线索发现：2026-11-26 从「客户与销售」抽出为独立顶级模块，统一收口
		// 线索发掘、判定、归档、规则、智能体与通知路由。key 与深链保持不变，
		// 仅变更父分组，避免已有角色授权行失效。
		Key: "leadDiscovery", Title: "线索智能体", Icon: "Search",
		Roles: []string{"admin", "viewer", "staff"},
		Children: []SystemMenuNode{
			{Key: "leadMining", Title: "线索挖掘", Icon: "Search", Path: "/lead-mining/index"},
			{Key: "clueList", Title: "线索列表", Icon: "Document", Path: "/clue/list"},
			{Key: "clueStatistics", Title: "线索统计", Icon: "DataAnalysis", Path: "/clue/statistics"},
			{Key: "leadGovernance", Title: "线索治理", Icon: "Aim", Path: "/leads/rules", Roles: []string{"admin"}},
			{Key: "leadMiningAgents", Title: "线索发现智能体", Icon: "Cpu", Path: "/leads/mining-agents", Roles: []string{"admin"}},
			{Key: "leadNotifyRoutes", Title: "线索通知路由", Icon: "Bell", Path: "/leads/notify-routes", Roles: []string{"admin"}},
			{Key: "leadEvidence", Title: "判定与证据", Icon: "Document", Path: "/leads/evidence", Roles: []string{"admin"}},
			{Key: "leadArchive", Title: "线索归档", Icon: "FolderChecked", Path: "/leads/archive", Roles: []string{"admin"}},
			{Key: "leadScopedList", Title: "权限内客户与消息", Icon: "List", Path: "/leads/scoped-list", Roles: []string{"admin"}},
		},
	},
	{
		Key: "aiAgent", Title: "客服智能体", Icon: "Cpu",
		Roles: []string{"admin", "customer_service", "staff"},
		Children: []SystemMenuNode{
			{
				Key: "agentManage", Title: "智能体管理", Icon: "Cpu",
				Children: []SystemMenuNode{
					{Key: "aiAgentList", Title: "智能体列表", Icon: "List", Path: "/aiAgent/list"},
					{Key: "aiAgentCreate", Title: "创建智能体", Icon: "Plus", Path: "/aiAgent/create", Roles: []string{"admin"}},
				},
			},
			{
				Key: "agentPassive", Title: "被动应答", Icon: "ChatDotRound",
				Children: []SystemMenuNode{
					{Key: "customerSession", Title: "客服会话", Icon: "Service", Path: "/customerSession/list"},
					{Key: "intentRecognition", Title: "意图识别", Icon: "Aim", Path: "/intentRecognition/list"},
					{Key: "intentToolFlow", Title: "意图工具流", Icon: "Operation", Path: "/intentRecognition/tool-flow", Roles: []string{"admin"}},
					{Key: "dialogueMemory", Title: "对话记忆", Icon: "ChatDotRound", Path: "/dialogueMemory/list"},
					{Key: "llmRouting", Title: "LLM 路由", Icon: "Cpu", Path: "/llmRouting/list"},
					{Key: "unknownQuestions", Title: "未知问题运营", Icon: "QuestionFilled", Path: "/unknown-questions", Roles: []string{"admin"}},
					{Key: "confidencePanel", Title: "置信度看板", Icon: "CircleCheck", Path: "/confidence/panel", Roles: []string{"admin", "viewer"}},
					{Key: "humanizePanel", Title: "拟人度看板", Icon: "MagicStick", Path: "/humanize/panel", Roles: []string{"admin", "viewer"}},
					{Key: "feedbackLoopPanel", Title: "反馈闭环", Icon: "Refresh", Path: "/feedbackLoop/panel", Roles: []string{"admin", "viewer"}},
				},
			},
			{
				Key: "agentSupport", Title: "工具与 SOP", Icon: "Connection",
				Children: []SystemMenuNode{
					{Key: "sopAgent", Title: "SOP 智能体", Icon: "Connection", Path: "/sopAgent/list", Roles: []string{"admin"}},
					{Key: "scriptTemplate", Title: "销冠话术库", Icon: "ChatLineSquare", Path: "/scriptTemplate/list"},
					{Key: "scriptTemplateAbStats", Title: "话术 AB 统计", Icon: "DataLine", Path: "/scriptTemplate/ab-stats"},
					{Key: "aiToolManagement", Title: "AI 工具管理", Icon: "Tools", Path: "/aiAgent/tools", Roles: []string{"admin"}},
					{Key: "aiCustomIntegration", Title: "工具接入", Icon: "Link", Path: "/aiAgent/integrations", Roles: []string{"admin"}},
					{Key: "mcpCredentials", Title: "MCP 凭证", Icon: "Key", Path: "/mcp/credentials", Roles: []string{"admin"}},
				},
			},
			{
				Key: "agentWorkbench", Title: "客服工作台", Icon: "Headset",
				Roles: []string{"admin", "customer_service"},
				Children: []SystemMenuNode{
					{Key: "agentStatus", Title: "坐席状态", Icon: "Headset", Path: "/customerService/agentStatus"},
					{Key: "csatDashboard", Title: "CSAT 看板", Icon: "Star", Path: "/customerService/csat"},
					{Key: "quickReply", Title: "快捷回复", Icon: "ChatLineSquare", Path: "/customerService/quickReply"},
					{Key: "sessionTag", Title: "会话标签", Icon: "CollectionTag", Path: "/customerService/sessionTag"},
					{Key: "aiSuggestion", Title: "AI 建议", Icon: "MagicStick", Path: "/customerService/aiSuggestion"},
					{Key: "automationCenter", Title: "客服自动化", Icon: "Operation", Path: "/system/automation-hub", Roles: []string{"admin"}},
				},
			},
		},
	},
	{
		Key: "reach", Title: "触达运营", Icon: "Promotion",
		Roles: []string{"admin", "viewer", "staff"},
		Children: []SystemMenuNode{
			{
				Key: "agentActive", Title: "主动触达", Icon: "Promotion",
				Children: []SystemMenuNode{
					{Key: "reachPipeline", Title: "触达 Pipeline", Icon: "Promotion", Path: "/reachPipeline/list", Roles: []string{"admin"}},
					{Key: "reachDeferredOutbound", Title: "延迟出站管理", Icon: "Timer", Path: "/reachPipeline/deferred-outbound", Roles: []string{"admin"}},
					{Key: "marketingFlow", Title: "营销流程", Icon: "SetUp", Path: "/marketingFlow/list", Roles: []string{"admin"}},
					{Key: "batchOperation", Title: "批量运营", Icon: "Operation", Path: "/batchOperation/list", Roles: []string{"admin"}},
				},
			},
			{Key: "systemMaterialLibrary", Title: "素材库", Icon: "Picture", Path: "/system/material-library", Roles: []string{"admin"}},
			{
				Key: "emailReach", Title: "邮件触达", Icon: "Message",
				Children: []SystemMenuNode{
					{Key: "emailList", Title: "邮件列表", Icon: "ChatSquare", Path: "/email", Roles: []string{"admin"}},
					{Key: "emailDrafts", Title: "我的草稿", Icon: "Document", Path: "/email/drafts", Roles: []string{"admin"}},
					{Key: "emailJobs", Title: "我的任务", Icon: "Document", Path: "/email/jobs", Roles: []string{"admin"}},
					{Key: "emailSmtp", Title: "发件账号", Icon: "Setting", Path: "/email/smtp", Roles: []string{"admin"}},
					{Key: "emailInfo", Title: "邮箱服务商参考", Icon: "Setting", Path: "/email/info"},
					{Key: "emailDeliverability", Title: "送达分析", Icon: "DataAnalysis", Path: "/email/deliverability", Roles: []string{"admin"}},
					{Key: "emailGuide", Title: "邮件使用指南", Icon: "Document", Path: "/email/guide"},
				},
			},
			{
				Key: "smsReach", Title: "短信触达", Icon: "ChatDotSquare",
				Children: []SystemMenuNode{
					{Key: "smsList", Title: "短信列表", Icon: "ChatDotSquare", Path: "/sms/list"},
					{Key: "smsDrafts", Title: "短信草稿", Icon: "Document", Path: "/sms/drafts"},
					{Key: "smsJobs", Title: "短信任务", Icon: "List", Path: "/sms/jobs"},
					{Key: "smsConfig", Title: "短信配置", Icon: "Setting", Path: "/sms/config", Roles: []string{"admin"}},
				},
			},
			{
				Key: "douyin", Title: "抖音", Icon: "VideoPlay",
				Children: []SystemMenuNode{
					{Key: "douyinCard", Title: "抖音卡片", Icon: "VideoPlay", Path: "/douyinCard", Roles: []string{"admin"}},
					{Key: "douyinStats", Title: "抖音统计", Icon: "DataAnalysis", Path: "/douyin/stats"},
				},
			},
			{
				Key: "kuaishou", Title: "快手", Icon: "Bell",
				Children: []SystemMenuNode{
					{Key: "kuaishouCard", Title: "快手卡片", Icon: "ChatDotRound", Path: "/kuaishouCard", Roles: []string{"admin"}},
					{Key: "kuaishouStats", Title: "快手统计", Icon: "DataAnalysis", Path: "/kuaishou/stats"},
				},
			},
			{
				Key: "xiaohongshu", Title: "小红书", Icon: "Picture",
				Children: []SystemMenuNode{
					{Key: "xiaohongshuCard", Title: "小红书卡片", Icon: "Picture", Path: "/xiaohongshuCard", Roles: []string{"admin"}},
					{Key: "xiaohongshuStats", Title: "小红书统计", Icon: "DataAnalysis", Path: "/xiaohongshu/stats"},
				},
			},
			{
				Key: "xianyu", Title: "闲鱼", Icon: "ShoppingBag",
				Children: []SystemMenuNode{
					{Key: "xianyuCard", Title: "闲鱼卡片", Icon: "ShoppingBag", Path: "/xianyuCard", Roles: []string{"admin"}},
					{Key: "xianyuStats", Title: "闲鱼统计", Icon: "DataAnalysis", Path: "/xianyu/stats"},
				},
			},
			{
				Key: "tiktok", Title: "TikTok", Icon: "VideoPlay",
				Children: []SystemMenuNode{
					{Key: "tiktokList", Title: "TikTok卡片", Icon: "VideoPlay", Path: "/tiktok/list", Roles: []string{"admin"}},
					{Key: "tiktokStats", Title: "TikTok统计", Icon: "DataAnalysis", Path: "/tiktok/stats"},
				},
			},
			{
				Key: "whatsapp", Title: "WhatsApp", Icon: "ChatDotRound",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "whatsappAccount", Title: "账号管理", Icon: "Cpu", Path: "/whatsapp/account"},
					{Key: "whatsappDrafts", Title: "草稿箱", Icon: "Document", Path: "/whatsapp/drafts"},
					{Key: "whatsappJobs", Title: "群发任务", Icon: "Promotion", Path: "/whatsapp/jobs"},
					{Key: "whatsappLeadGroup", Title: "群发线索分组", Icon: "UserFilled", Path: "/whatsapp/lead-group-selection"},
					{Key: "whatsappBulkMatrix", Title: "模板群发", Icon: "Grid", Path: "/whatsapp/bulk-matrix"},
					{Key: "whatsappGroupMessaging", Title: "群组发送", Icon: "Grid", Path: "/whatsapp/group-messaging"},
				},
			},
			{
				Key: "telegram", Title: "电报社群", Icon: "ChatDotRound",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "telegramAccount", Title: "机器人", Icon: "Cpu", Path: "/telegram/account"},
					{Key: "telegramGate", Title: "入群管控", Icon: "Lock", Path: "/telegram/gate"},
				},
			},
			{
				Key: "qq", Title: "QQ 机器人", Icon: "ChatDotRound", Path: "/qq/account",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "qqAccount", Title: "机器人账号", Icon: "Cpu", Path: "/qq/account"},
				},
			},
			{
				Key: "feishu", Title: "飞书", Icon: "ChatDotRound", Path: "/feishu/account",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "feishuAccount", Title: "飞书账号", Icon: "Cpu", Path: "/feishu/account"},
				},
			},
			{Key: "community", Title: "社群管理", Icon: "UserFilled", Path: "/community/list", Roles: []string{"admin"}},
			{
				Key: "shortLink", Title: "短链管理", Icon: "Link",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "shortLinkList", Title: "短链列表", Icon: "Link", Path: "/shortLink"},
					{Key: "shortLinkStats", Title: "短链统计", Icon: "DataAnalysis", Path: "/shortLink/stats"},
				},
			},
			{Key: "livecode", Title: "活码管理", Icon: "Postcard", Path: "/livecode", Roles: []string{"admin"}},
			{Key: "workflowOrchestrator", Title: "工作流编排", Icon: "Share", Path: "/workflow-orchestrator/list", Roles: []string{"admin"}},
		},
	},
	{
		Key: "knowledge", Title: "知识中心", Icon: "Collection",
		Roles: []string{"admin", "viewer", "staff"},
		Children: []SystemMenuNode{
			{Key: "faqKb", Title: "FAQ 知识库", Icon: "Notebook", Path: "/faq/list", Roles: []string{"admin", "customer_service", "staff"}},
			{Key: "knowledgeBase", Title: "知识库管理", Icon: "Files", Path: "/knowledgeBase", Roles: []string{"admin", "customer_service", "staff"}},
			{Key: "sopTemplate", Title: "SOP 模板库", Icon: "Tickets", Path: "/sop-template/list", Roles: []string{"admin"}},
			{Key: "sopTemplateMarket", Title: "SOP 模板市场", Icon: "ShoppingCart", Path: "/sop-template/market", Roles: []string{"admin"}},
			{
				Key: "knowledgeWorkspace", Title: "知识库工作台", Icon: "Files",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "knowledgeManagement", Title: "文档管理", Icon: "Document", Path: "/knowledge/management"},
					{Key: "knowledgeBatchImport", Title: "批量导入", Icon: "UploadFilled", Path: "/knowledge/batch-import"},
					{Key: "knowledgePlayground", Title: "检索 Playground", Icon: "Aim", Path: "/knowledge/playground"},
					{Key: "knowledgeFeedbacks", Title: "反馈管理", Icon: "ChatLineRound", Path: "/knowledge/feedbacks"},
					{Key: "knowledgeTokens", Title: "API Token", Icon: "Key", Path: "/knowledge/tokens"},
					{Key: "knowledgeExternal", Title: "外部系统接入", Icon: "Connection", Path: "/knowledge/external"},
					{Key: "knowledgeStatistics", Title: "知识库统计", Icon: "DataAnalysis", Path: "/knowledge/statistics"},
					{Key: "knowledgeRagEval", Title: "RAG 评测", Icon: "CircleCheck", Path: "/knowledge/rag-eval"},
					{Key: "knowledgeOpenAPI", Title: "OpenAPI 集成", Icon: "Connection", Path: "/knowledge/openapi"},
					{Key: "knowledgeConnectors", Title: "连接器配置", Icon: "Link", Path: "/knowledge/connectors"},
					{Key: "knowledgeCrawlerSources", Title: "网站爬取", Icon: "Connection", Path: "/knowledge/crawler-sources", Roles: []string{"admin"}},
					{Key: "knowledgeQaCandidates", Title: "QA 审核队列", Icon: "CircleCheck", Path: "/knowledge/qa-candidates", Roles: []string{"admin"}},
					{Key: "kbImportConnectors", Title: "第三方知识库导入", Icon: "Connection", Path: "/kb/connectors", Roles: []string{"admin"}},
				},
			},
			{
				Key: "ragProductConfig", Title: "RAG 知识库", Icon: "ChatLineRound",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "ragAccount", Title: "RAG 账号配置", Icon: "Key", Path: "/system/rag-account"},
					{Key: "ragProduct", Title: "RAG 产品管理", Icon: "Goods", Path: "/system/rag-product"},
				},
			},
			{Key: "ragOverview", Title: "RAG 概览", Icon: "Monitor", Path: "/system/rag-overview"},
			{
				Key: "i18nManage", Title: "多语言管理", Icon: "Coin",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "glossary", Title: "术语表管理", Icon: "Collection", Path: "/glossary", Roles: []string{"admin"}},
					{Key: "i18nDashboard", Title: "多语言监控", Icon: "DataLine", Path: "/i18n/dashboard", Roles: []string{"admin"}},
				},
			},
		},
	},
	{
		Key: "geoTools", Title: "GEO 增长", Icon: "MagicStick", Path: "/geo-tools/keyword-mining",
		Roles: []string{"admin"},
		Children: []SystemMenuNode{
			{
				Key: "geoDiagnose", Title: "诊断", Icon: "Aim", Path: "/geo-tools/audit-center",
				Children: []SystemMenuNode{
					{Key: "geoAuditCenter", Title: "审计中心", Icon: "Aim", Path: "/geo-tools/audit-center"},
				},
			},
			{
				Key: "geoConfigGroup", Title: "配置", Icon: "Setting",
				Children: []SystemMenuNode{
					{Key: "geoBrandConfig", Title: "AI 爬虫访问文件", Icon: "OfficeBuilding", Path: "/geo-tools/brand-config"},
					{Key: "geoSeoInfra", Title: "SEO 基础设施", Icon: "DocumentChecked", Path: "/geo-tools/seo-infra"},
					{Key: "geoPusherConfig", Title: "蜘蛛推送配置", Icon: "Key", Path: "/geo-tools/pusher-config"},
					{Key: "geoSiteConfig", Title: "静态站部署", Icon: "Monitor", Path: "/geo-tools/site-config"},
					{Key: "geoSchemaTemplates", Title: "Schema 模板", Icon: "Collection", Path: "/geo-tools/schema-templates"},
					{Key: "geoCompetitors", Title: "竞品管理", Icon: "UserFilled", Path: "/geo-tools/competitors"},
					{Key: "geoConfig", Title: "品牌与站点资料", Icon: "Setting", Path: "/geo-tools/config"},
				},
			},
			{
				Key: "geoProd", Title: "生产", Icon: "EditPen",
				Children: []SystemMenuNode{
					{Key: "geoAutopilot", Title: "一键自动驾驶", Icon: "MagicStick", Path: "/geo-tools/autopilot"},
					{Key: "geoKeywordMining", Title: "关键词蒸馏", Icon: "Search", Path: "/geo-tools/keyword-mining"},
					{Key: "geoContentCreation", Title: "内容创作", Icon: "EditPen", Path: "/geo-tools/content-creation"},
					{Key: "geoContentOptimize", Title: "文章优化", Icon: "Document", Path: "/geo-tools/content-optimize"},
					{Key: "geoKnowledgeBase", Title: "GEO 知识库", Icon: "FolderOpened", Path: "/geo-tools/knowledge-base"},
					{Key: "geoEntityGraph", Title: "实体图谱", Icon: "Share", Path: "/geo-tools/entity-graph"},
					{Key: "geoDecisionReport", Title: "信源补位任务", Icon: "Document", Path: "/geo/decision-report"},
				},
			},
			{
				Key: "geoPublish", Title: "发布", Icon: "Connection",
				Children: []SystemMenuNode{
					{Key: "geoSitePublish", Title: "发布到官网", Icon: "Upload", Path: "/geo-tools/site-publish"},
					{Key: "geoPushCenter", Title: "蜘蛛推送", Icon: "Promotion", Path: "/geo-tools/push-center"},
					{Key: "geoPlatformPublish", Title: "平台发布", Icon: "Connection", Path: "/geo-tools/platform-publish"},
					{Key: "geoWorkflow", Title: "工作流", Icon: "Setting", Path: "/geo-tools/workflow"},
				},
			},
			{
				Key: "geoCapture", Title: "承接", Icon: "ChatDotRound", Path: "/geo-tools/capture-links",
				Children: []SystemMenuNode{
					{Key: "geoCaptureLinks", Title: "承接链接", Icon: "Link", Path: "/geo-tools/capture-links"},
				},
			},
			{
				Key: "geoMonitor", Title: "监控", Icon: "Monitor",
				Children: []SystemMenuNode{
					{Key: "geoFunnelDashboard", Title: "漏斗总览", Icon: "DataAnalysis", Path: "/geo-tools/funnel-dashboard"},
					{Key: "geoIndexTracking", Title: "收录与引用", Icon: "CircleCheck", Path: "/geo-tools/index-tracking"},
					{Key: "geoVisibilityBoard", Title: "可见性观测", Icon: "TrendCharts", Path: "/geo-tools/visibility"},
					{Key: "geoSovBoard", Title: "竞品 SOV", Icon: "DataLine", Path: "/geo-tools/sov-board"},
					{Key: "geoCrawlerStats", Title: "UA 探测记录", Icon: "Monitor", Path: "/geo-tools/crawler-stats"},
					{Key: "geoVerification", Title: "多模型验证", Icon: "CircleCheck", Path: "/geo-tools/verification"},
					{Key: "geoAlerts", Title: "预警中心", Icon: "Bell", Path: "/geo-tools/alerts"},
					{Key: "geoReports", Title: "数据报表", Icon: "DataAnalysis", Path: "/geo-tools/reports"},
					{Key: "geoDualEngine", Title: "双端监测", Icon: "DataBoard", Path: "/geo-tools/dual-engine"},
				},
			},
		},
	},
	{
		Key: "analytics", Title: "数据分析", Icon: "DataAnalysis",
		Roles: []string{"admin", "viewer", "staff"},
		Children: []SystemMenuNode{
			{Key: "dashboardScreen", Title: "数据大屏", Icon: "DataBoard", Path: "/dashboardScreen/list"},
			{Key: "conversionFunnel", Title: "转化漏斗", Icon: "Filter", Path: "/conversionFunnel/list"},
			{Key: "llmUsageReport", Title: "LLM 用量报表", Icon: "TrendCharts", Path: "/reports/llm", Roles: []string{"admin", "viewer"}},
			{Key: "aiProductivity", Title: "AI 产能分析", Icon: "DataAnalysis", Path: "/aiProductivity/list", Roles: []string{"admin", "viewer"}},
			{Key: "customReport", Title: "自定义报表", Icon: "Document", Path: "/customReport/list"},
			{Key: "abExperiment", Title: "A/B 实验", Icon: "DataLine", Path: "/abExperiment/list", Roles: []string{"admin", "viewer"}},
			{Key: "churnPrediction", Title: "流失预警", Icon: "Warning", Path: "/churnPrediction/list", Roles: []string{"admin", "viewer"}},
			{Key: "customerJourney", Title: "客户旅程大屏", Icon: "TrendCharts", Path: "/customerJourney/dashboard", Roles: []string{"admin", "viewer"}},
			{Key: "cohortPath", Title: "同期群路径分析", Icon: "Guide", Path: "/analytics/cohort-path", Roles: []string{"admin", "viewer"}},
		},
	},
	{
		Key: "system", Title: "系统管理", Icon: "Setting",
		Roles: []string{"admin", "viewer", "staff"},
		Children: []SystemMenuNode{
			{
				Key: "channels", Title: "渠道", Icon: "Connection",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					// 保留权限键和深链；前端投影为一个入口，模块内部切换管理面。
					{Key: "channelManage", Title: "渠道", Icon: "SetUp", Path: "/channels/manage", Roles: []string{"admin"}},
					{Key: "channelAccount", Title: "渠道账号", Icon: "Connection", Path: "/channels/accounts", Roles: []string{"admin"}},
					{Key: "channelIdentifyCode", Title: "识别码", Icon: "Key", Path: "/channels/identify-codes", Roles: []string{"admin"}},
				},
			},
			{
				Key: "siteConfig", Title: "系统配置与运维", Icon: "Tools",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "systemConfig", Title: "站点设置", Icon: "Tools", Path: "/system/config"},
					{Key: "systemObsConfig", Title: "存储配置", Icon: "Box", Path: "/system/obs-config"},
					{Key: "systemMonitor", Title: "监控", Icon: "Cpu", Path: "/system/monitor"},
					{Key: "systemTrace", Title: "链路追踪", Icon: "Connection", Path: "/system/trace"},
					{Key: "systemGuide", Title: "系统使用指南", Icon: "Document", Path: "/system/guide"},
					{Key: "domainPool", Title: "域名池", Icon: "Link", Path: "/domainPool"},
					{Key: "backupList", Title: "备份恢复", Icon: "FolderOpened", Path: "/backup/list"},
					{Key: "dynamicThreshold", Title: "动态阈值参数", Icon: "DataLine", Path: "/system/config-params", Roles: []string{"admin"}},
					{Key: "systemDictionaries", Title: "业务字典", Icon: "Collection", Path: "/system/dictionaries", Roles: []string{"admin"}},
					{Key: "systemFeatureFlags", Title: "功能开关", Icon: "Switch", Path: "/system/feature-flags", Roles: []string{"admin"}},
					{Key: "ltcConfig", Title: "LTC 运营开关", Icon: "Open", Path: "/system/ltc-config", Roles: []string{"admin"}},
					{Key: "opsOverview", Title: "运维总览", Icon: "Monitor", Path: "/ops-overview", Roles: []string{"admin"}},
					{Key: "observabilityDashboard", Title: "AI 观测与渠道健康", Icon: "Odometer", Path: "/observability-dashboard", Roles: []string{"admin"}},
					{Key: "capabilityMatrix", Title: "能力矩阵", Icon: "Grid", Path: "/cap/matrix", Roles: []string{"admin"}},
					{Key: "messageHub", Title: "消息中台 MQ", Icon: "MessageBox", Path: "/messageHub/list", Roles: []string{"admin"}},
					{Key: "messageHubDashboard", Title: "消息中台看板", Icon: "DataBoard", Path: "/messageHub/dashboard", Roles: []string{"admin"}},
					{Key: "operationLog", Title: "操作日志", Icon: "Tickets", Path: "/operationLog/list"},
					{Key: "securityAudit", Title: "安全审计", Icon: "Lock", Path: "/securityAudit/list"},
				},
			},
			{
				Key: "teamGroup", Title: "团队与权限", Icon: "UserFilled",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "systemUser", Title: "人员管理", Icon: "UserFilled", Path: "/system/users"},
					{Key: "roleManage", Title: "角色管理", Icon: "UserFilled", Path: "/system/roles"},
					{Key: "permissionManage", Title: "授权管理", Icon: "Lock", Path: "/system/permissions"},
					{Key: "businessAccountAuth", Title: "业务账号授权", Icon: "Lock", Path: "/businessAccountAuth/list", Roles: []string{"admin"}},
				},
			},
			{
				Key: "permissionGroup", Title: "渠道与集成", Icon: "Connection",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "platformAccount", Title: "平台账号", Icon: "Platform", Path: "/platformAccount/list"},
					{Key: "integration", Title: "第三方对接", Icon: "Connection", Path: "/integration/list"},
					{Key: "bridgeExtension", Title: "扩展安装包", Icon: "Download", Path: "/bridge/extension"},
					{Key: "wecomAccount", Title: "多账号聚合", Icon: "Connection", Path: "/wecomAccount/list", Roles: []string{"admin"}},
					{
						Key: "agentChannel", Title: "客服渠道", Icon: "Connection",
						Roles: []string{"admin"},
						Children: []SystemMenuNode{
							{Key: "chatChannelList", Title: "网页客服渠道", Icon: "Connection", Path: "/chatChannel/list"},
							{Key: "whatsappCloud", Title: "WhatsApp Cloud", Icon: "Connection", Path: "/whatsapp-cloud"},
							{Key: "dingtalkApp", Title: "钉钉应用账号", Icon: "Connection", Path: "/dingtalk-app"},
						},
					},
				},
			},
			{
				Key: "browserAutomation", Title: "浏览器自动化", Icon: "Monitor", Path: "/browser-automation/tasks",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "baTasks", Title: "任务列表", Icon: "Monitor", Path: "/browser-automation/tasks"},
					{Key: "baCreate", Title: "新建任务", Icon: "Plus", Path: "/browser-automation/tasks/create"},
					{Key: "baCron", Title: "定时触发器", Icon: "Timer", Path: "/browser-automation/cron"},
					{Key: "baStatus", Title: "Host 状态", Icon: "CircleCheck", Path: "/browser-automation/status"},
					{Key: "egoFlows", Title: "我的流程", Icon: "Document", Path: "/browser-automation/ego/flows"},
					{Key: "egoSessions", Title: "通用会话", Icon: "Connection", Path: "/browser-automation/ego/sessions"},
					{Key: "egoExecutions", Title: "执行监控", Icon: "DataLine", Path: "/browser-automation/ego/executions"},
					{Key: "egoRunner", Title: "Runner 状态", Icon: "Cpu", Path: "/browser-automation/ego/runner"},
					{Key: "egoAssets", Title: "资产中心", Icon: "Shop", Path: "/browser-automation/ego/assets"},
					{Key: "egoAssetAdmin", Title: "资产发布台", Icon: "Upload", Path: "/browser-automation/ego/assets/admin", Roles: []string{"admin"}},
					{Key: "egoTelemetry", Title: "遥测看板", Icon: "DataBoard", Path: "/browser-automation/ego/telemetry", Roles: []string{"admin"}},
				},
			},
			{Key: "capabilityDiagnostics", Title: "能力运行诊断", Icon: "DataAnalysis", Path: "/cap/diagnostics", Roles: []string{"admin"}},
			// 开通入口归基础模块，不能被待购买能力的开关关闭。
			{
				Key: "moduleCenterGroup", Title: "功能模块", Icon: "Shop",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "moduleCenter", Title: "功能模块市场", Icon: "Shop", Path: "/module-market"},
					{Key: "myModules", Title: "我的模块", Icon: "FolderOpened", Path: "/module-market/my"},
					{Key: "moduleOrders", Title: "模块订单", Icon: "Tickets", Path: "/module-market/orders"},
				},
			},
			{
				Key: "templateMarketGroup", Title: "模板/技能", Icon: "Shop",
				Roles: []string{"admin"},
				Children: []SystemMenuNode{
					{Key: "templateMarket", Title: "模板/技能市场", Icon: "Shop", Path: "/template-market"},
					{Key: "templateMarketMy", Title: "我的发布/订单", Icon: "Tickets", Path: "/template-market/my"},
					{Key: "templateMarketReview", Title: "发布审核", Icon: "Operation", Path: "/template-market/review", Roles: []string{"admin"}},
				},
			},
			{
				Key: "assetBundle", Title: "资产包", Icon: "Box",
				Roles: []string{"admin", "viewer", "staff"},
				Children: []SystemMenuNode{
					{Key: "assetMarket", Title: "资产市场", Icon: "ShoppingCart", Path: "/asset-market"},
					{Key: "myAssets", Title: "我的资产", Icon: "Files", Path: "/asset-market/my-assets"},
					{Key: "assetBundleList", Title: "资产包管理", Icon: "List", Path: "/asset-bundle/list", Roles: []string{"admin"}},
					{Key: "assetBundlePlayground", Title: "开发者 Playground", Icon: "EditPen", Path: "/asset-bundle/playground", Roles: []string{"admin"}},
					{Key: "assetBundleMerchant", Title: "商户话术包", Icon: "ChatDotRound", Path: "/asset-bundle/merchant-new", Roles: []string{"admin"}},
					{Key: "syncLog", Title: "同步日志", Icon: "Refresh", Path: "/asset-market/sync-log"},
				},
			},
		},
	},
}

// FlattenMenuKeys 深度优先收集树中全部节点 key
func FlattenMenuKeys(tree []SystemMenuNode) []string {
	keys := make([]string, 0, 64)
	var walk func(nodes []SystemMenuNode)
	walk = func(nodes []SystemMenuNode) {
		for _, n := range nodes {
			keys = append(keys, n.Key)
			walk(n.Children)
		}
	}
	walk(tree)
	return keys
}

// ExistsMenuKey 校验 key 是否为注册表中的合法菜单键
func ExistsMenuKey(key string) bool {
	if key == "" {
		return false
	}
	var walk func(nodes []SystemMenuNode) bool
	walk = func(nodes []SystemMenuNode) bool {
		for _, n := range nodes {
			if n.Key == key {
				return true
			}
			if walk(n.Children) {
				return true
			}
		}
		return false
	}
	return walk(SystemMenuTree)
}

// roleAllowsMenu 把注册表上的角色词表（manager/sales/viewer…）折算到后端 3 档角色：
// 这个角色**有没有资格**拥有该节点。
//
// 它是授权的上界，不是初值：库里的 role_menu_permissions 只能在它圈定的范围内再收窄，
// 不能把注册表留给超管的节点发出去（见 EligibleMenuKeys）。
// 首空表 seed（DefaultMenuKeys）也用它推导出厂分配。
//
//	admin             —— 全部放行（调用方单独处理，这里仅兜底）
//	customer_service  —— 需命中 customer_service 或 staff（客服视角词表）
//	staff             —— 需命中 staff 或 viewer（员工视角词表）
//	其余（含非法角色）—— 一律不合格
func roleAllowsMenu(role string, roles []string) bool {
	if len(roles) == 0 {
		return true
	}
	has := func(candidates ...string) bool {
		for _, r := range roles {
			for _, c := range candidates {
				if r == c {
					return true
				}
			}
		}
		return false
	}
	switch role {
	case SystemRoleCodeAdmin:
		return true
	case SystemRoleCodeCustomerService:
		return has(SystemRoleCodeCustomerService, SystemRoleCodeStaff)
	case SystemRoleCodeStaff:
		return has(SystemRoleCodeStaff, "viewer")
	default:
		return false
	}
}

// DefaultMenuKeys 推导某角色首空表时的默认可见菜单 key 集合
//
// admin 恒返回全部 key（实际不落库，直接走全树）；
// 其余角色按注册表 Roles 词表折算。仅在 role_menu_permissions 首次为空时 seed 使用。
func DefaultMenuKeys(role string) []string {
	keys := make([]string, 0, 64)
	var walk func(nodes []SystemMenuNode)
	walk = func(nodes []SystemMenuNode) {
		for _, n := range nodes {
			if roleAllowsMenu(role, n.Roles) {
				keys = append(keys, n.Key)
				walk(n.Children)
			}
		}
	}
	walk(SystemMenuTree)
	return keys
}

// EligibleMenuKeys 返回注册表允许某角色拥有的 key 集合（授权上界）。
//
// 与 DefaultMenuKeys 用同一棵折算口径，差别只在用途：那个只在首空表时推导初始分配，
// 这个是**每次读**都要过的上界——库里的分配行只能在它之内再收窄。
// 于是"注册表把某节点留给超管"这句话对老库同样生效，不必先迁数据。
// 反过来"注册表重新放开一个节点"则**必须**重新授权：库里那些曾经越界的分配行
// 已由 v3.55.0 的收敛迁移摘掉，改一行 Roles 词表不会把历史授权静默放回来
// （留着那些行的话，改注册表就等于改权限，而没人点过授权、也就没有操作日志）。
//
// 目录节点不合格时，其后代一并剔除（父节点不在侧栏，子路由就不该能被调到）。
// admin 恒为全树。非法/空角色为空集（调用方向拒的方向收）。
func EligibleMenuKeys(role string) map[string]struct{} {
	set := make(map[string]struct{}, 64)
	var walk func(nodes []SystemMenuNode, parentOK bool)
	walk = func(nodes []SystemMenuNode, parentOK bool) {
		for _, n := range nodes {
			ok := parentOK && roleAllowsMenu(role, n.Roles)
			if ok {
				set[n.Key] = struct{}{}
			}
			walk(n.Children, ok)
		}
	}
	walk(SystemMenuTree, true)
	return set
}

// FilterMenuTree 按 key 集合过滤菜单树：
//   - 节点自身 key 命中，或任一后代命中 → 保留
//   - 无 Path 且过滤后无子节点的纯目录 → 剪除（避免空目录壳）
//
// keys 为 nil 时返回全树（admin 全量）；非 nil（含空 map）时严格按集合过滤，
// 空 map 结果为空树——避免非 admin 空权限误放行。
func FilterMenuTree(tree []SystemMenuNode, keys map[string]struct{}) []SystemMenuNode {
	if keys == nil {
		return tree
	}
	var walk func(nodes []SystemMenuNode) []SystemMenuNode
	walk = func(nodes []SystemMenuNode) []SystemMenuNode {
		out := make([]SystemMenuNode, 0, len(nodes))
		for _, n := range nodes {
			children := walk(n.Children)
			_, self := keys[n.Key]
			if !self && len(children) == 0 {
				continue
			}
			// key 命中但无后代且无路径的死目录壳 → 也剪掉
			if self && n.Path == "" && len(children) == 0 {
				continue
			}
			n.Children = children
			out = append(out, n)
		}
		return out
	}
	return walk(tree)
}
