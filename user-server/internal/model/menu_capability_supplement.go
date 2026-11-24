package model

// MenuCapabilitySupplement 手写补充的能力面（**不在生成件里**）。
//
// 生成件 scripts/menu-route-role-audit.py 的入选判据依赖前端调用图与角色可见性，
// 对后来新增、尚未被脚本纳管的 GEO 子路由会整体漏掉。漏掉的后果不是“少一条提示”：
// RoutePolicyGuard 对未命中路径直接放行，未购买 GEO 模块的租户也能调用这些口。
//
// 口径与生成件一致：MenuKeys 是“任一授予即放行”的菜单节点。这里只补
// 生成件缺失、且消费页明确的 GEO 形状；新增路由后由
// router.TestGeoRoutesAllModuleGated 兜底（它同时要求模块归属非空）。
var MenuCapabilitySupplement = []MenuCapability{
	// 预警中心
	{Pattern: "/api/geo/alerts", MenuKeys: []string{"geoAlerts", "geoTools"}},
	{Pattern: "/api/geo/alerts/unread-count", MenuKeys: []string{"geoAlerts", "geoTools"}},
	{Pattern: "/api/geo/alerts/*/ack", MenuKeys: []string{"geoAlerts", "geoTools"}},
	{Pattern: "/api/geo/alerts/*", MenuKeys: []string{"geoAlerts", "geoTools"}},
	// 机会列表（决策报告页消费）
	{Pattern: "/api/geo/opportunities", MenuKeys: []string{"geoDecisionReport", "geoTools"}},
	// 引用域名 / 引用矩阵 / 引用漂移（收录与引用页、数据报表页）
	{Pattern: "/api/geo/citations/domains", MenuKeys: []string{"geoIndexTracking", "geoReports"}},
	{Pattern: "/api/geo/citations/matrix", MenuKeys: []string{"geoIndexTracking", "geoReports"}},
	{Pattern: "/api/geo/citations/drift", MenuKeys: []string{"geoIndexTracking", "geoReports"}},
	// 可见性趋势与提示词扇出
	{Pattern: "/api/geo/visibility/trend", MenuKeys: []string{"geoVisibilityBoard", "geoFunnelDashboard"}},
	{Pattern: "/api/geo/prompt/fanout", MenuKeys: []string{"geoVisibilityBoard", "geoReports"}},
	// SOV 刷新（竞品 SOV / 可见性页）
	{Pattern: "/api/geo/probe/run-sov", MenuKeys: []string{"geoSovBoard", "geoVisibilityBoard", "geoCompetitors"}},
	// 数据报表：内容格式归因
	{Pattern: "/api/geo/reports/content-format", MenuKeys: []string{"geoReports"}},
	// 关键词增强 / 蒸馏流水线
	{Pattern: "/api/geo/keyword-enhance/analyze", MenuKeys: []string{"geoKeywordMining"}},
	{Pattern: "/api/geo/keyword-enhance/enhance", MenuKeys: []string{"geoKeywordMining"}},
	{Pattern: "/api/geo/keyword-mining/pipeline", MenuKeys: []string{"geoKeywordMining"}},
	// 内容指标分析
	{Pattern: "/api/geo/metrics/analyze", MenuKeys: []string{"geoContentCreation", "geoContentOptimize"}},
	// 技术配置（SEO 基础设施 / 审计中心 / 工具页）
	{Pattern: "/api/geo/techconfig/audit", MenuKeys: []string{"geoSeoInfra", "geoAuditCenter", "geoTools"}},
	{Pattern: "/api/geo/techconfig/site-audit", MenuKeys: []string{"geoSeoInfra", "geoAuditCenter", "geoTools"}},
	{Pattern: "/api/geo/techconfig/robots", MenuKeys: []string{"geoSeoInfra", "geoTools"}},
	{Pattern: "/api/geo/techconfig/llms-txt", MenuKeys: []string{"geoSeoInfra", "geoTools"}},
	{Pattern: "/api/geo/techconfig/sitemap", MenuKeys: []string{"geoSeoInfra", "geoTools"}},
	// 配置优化
	{Pattern: "/api/geo/config/optimize", MenuKeys: []string{"geoConfig", "geoBrandConfig", "geoTools"}},
	// 一键自动驾驶（无独立菜单，归到生产/工作流入口）
	{Pattern: "/api/geo/autopilot/runs", MenuKeys: []string{"geoAutopilot", "geoProd", "geoWorkflow", "geoTools"}},
	{Pattern: "/api/geo/autopilot/runs/*", MenuKeys: []string{"geoAutopilot", "geoProd", "geoWorkflow", "geoTools"}},
	// 种子词与字典
	{Pattern: "/api/geo/seed-terms", MenuKeys: []string{"geoKeywordMining", "geoTools"}},
	{Pattern: "/api/geo/seed-terms/reseed", MenuKeys: []string{"geoKeywordMining", "geoTools"}},
	{Pattern: "/api/geo/dicts", MenuKeys: []string{"geoConfig", "geoTools"}},
	{Pattern: "/api/geo/dicts/*/*", MenuKeys: []string{"geoConfig", "geoTools"}},
	// 定时任务控制面（超管专属，能力面只做模块归属兜底）
	{Pattern: "/api/geo/jobs", MenuKeys: []string{"geoTools"}},
	{Pattern: "/api/geo/jobs/runs", MenuKeys: []string{"geoTools"}},
	{Pattern: "/api/geo/jobs/*/trigger", MenuKeys: []string{"geoTools"}},
	{Pattern: "/api/geo/jobs/*/schedule", MenuKeys: []string{"geoTools"}},
	// 平台发布
	{Pattern: "/api/geo/platform/publish", MenuKeys: []string{"geoPlatformPublish"}},
	{Pattern: "/api/geo/platform/accounts/*", MenuKeys: []string{"geoPlatformPublish"}},
	// 2026-11-17 审计 P2-2：短信发送/重发按"能力位"收口（判据来自消费页 sms/List.vue
	// 的 smsList 菜单节点，staff 默认授予；未授予 smsList 的角色不可直调发送面）。
	// 配置读写口 /api/sms/config 走 adminOnlyRoutes（smsConfig 已收归 admin），不在此表。
	{Pattern: "/api/sms/send", MenuKeys: []string{"smsList"}},
	{Pattern: "/api/sms/resend/*", MenuKeys: []string{"smsList"}},
	// 2026-11-18 七审收口：流失预警是租户级经营分析（菜单注册表 Roles=admin,viewer；
	// staff 未列入），登录档裸放 ⇒ staff 可直读全租户客户流失数据。按能力位收口，
	// 正别名与 frontend_aliases 的连字别名都要覆盖。
	// 匹配器只支持单段 *（无 **），正别名/连字别名各一条单段规则
	{Pattern: "/api/churn/*", MenuKeys: []string{"churnPrediction"}},
	{Pattern: "/api/churn-prediction/*", MenuKeys: []string{"churnPrediction"}},
	// 2026-11-24 菜单重整：今日新增的 CRM / 线索治理 / 渠道统一 / 电商操作台
	// 四个业务面此前只有前端路由、侧栏无入口，接口也因此没有 menu_key 可挂——
	// 任何登录角色都能直调全租户主数据/规则/渠道凭据/店铺操作口。这里按新菜单
	// 节点收口：非超管须被授予对应菜单才放行，与侧栏勾选一处生效。
	// 只读口（注册在 auth 组）按能力位收；写口已有 AdminAuthMiddleware 的
	//（/manage/leads、/manage/lead-mining-agents、/lead-mining）由仅超管表拦，
	// 能力表不重复写它们，避免被 TestRouteGuardChain_CapabilityRulesAreNotDeadLines
	// 判成全遮蔽死行。
	// CRM 客户主数据（/customers、/dedupe/*、/merge/*、/360/*、实体 CRUD 等）
	{Pattern: "/api/manage/crm/*", MenuKeys: []string{"crmMaster"}},
	{Pattern: "/api/manage/crm/*/*", MenuKeys: []string{"crmMaster"}},
	// 线索治理读口（规则/通知路由/权限内消息与客户/判定命中）
	{Pattern: "/api/manage/leads/*", MenuKeys: []string{"leadGovernance"}},
	{Pattern: "/api/manage/leads/*/*/*", MenuKeys: []string{"leadGovernance"}},
	// 线索发现智能体读口 / 线索挖掘分析读口（同属线索治理菜单）
	{Pattern: "/api/manage/lead-mining-agents", MenuKeys: []string{"leadGovernance"}},
	{Pattern: "/api/manage/lead-mining-agents/*", MenuKeys: []string{"leadGovernance"}},
	{Pattern: "/api/lead-mining/*", MenuKeys: []string{"leadGovernance"}},
	{Pattern: "/api/lead-mining/*/*", MenuKeys: []string{"leadGovernance"}},
	// 渠道统一治理（预设渠道/账号/共享上下文），归渠道账号管理菜单。
	// /api/identify-codes* 归 channelIdentifyCode（生成件已有，不重复）。
	{Pattern: "/api/channels", MenuKeys: []string{"channelManage", "channelAccount"}},
	{Pattern: "/api/channels/*", MenuKeys: []string{"channelManage", "channelAccount"}},
	{Pattern: "/api/channel-accounts", MenuKeys: []string{"channelAccount"}},
	{Pattern: "/api/channel-accounts/*", MenuKeys: []string{"channelAccount"}},
	{Pattern: "/api/conversation-context", MenuKeys: []string{"channelAccount"}},
	// 二次审核补充：/api/channels/customer/:customer_id 是 4 段形状，上面的
	// /api/channels/*（3 段）接不住；它读的是某客户的渠道列表，与渠道管理同族，
	// 不收口就等于任何登录角色都能按 customer_id 枚举渠道绑定。
	{Pattern: "/api/channels/customer/*", MenuKeys: []string{"channelManage", "channelAccount"}},
	// 电商操作台（订单/操作/证据/复核全链），归 commerceOperations 菜单。
	{Pattern: "/api/v1/commerce/*", MenuKeys: []string{"commerceOperations"}},
	{Pattern: "/api/v1/commerce/*/*", MenuKeys: []string{"commerceOperations"}},
	{Pattern: "/api/v1/commerce/*/*/*", MenuKeys: []string{"commerceOperations"}},
}

// AllMenuCapabilities 返回补充件 + 生成件。补充件在前：同名形状以补充件的
// 归属为准，避免人工判定被生成件的旧推导覆盖。
func AllMenuCapabilities() []MenuCapability {
	out := make([]MenuCapability, 0, len(MenuCapabilitySupplement)+len(MenuCapabilities))
	out = append(out, MenuCapabilitySupplement...)
	out = append(out, MenuCapabilities...)
	return out
}
