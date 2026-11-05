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
}

// AllMenuCapabilities 返回补充件 + 生成件。补充件在前：同名形状以补充件的
// 归属为准，避免人工判定被生成件的旧推导覆盖。
func AllMenuCapabilities() []MenuCapability {
	out := make([]MenuCapability, 0, len(MenuCapabilitySupplement)+len(MenuCapabilities))
	out = append(out, MenuCapabilitySupplement...)
	out = append(out, MenuCapabilities...)
	return out
}
