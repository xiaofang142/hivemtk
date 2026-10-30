package model

// 表 → 模块 的归属登记表（任务一「功能模块涉及的…数据表」的可校验形态）
//
// ============================================================
// 为什么单独一张表，而不是把表名塞进 module_bindings.go 的注释里
//
// 需求原文要求「整理当前所有的功能模块，功能模块涉及的页面**以及数据表**」。
// 页面归属（ModuleMenuBindings）有测试钉死「208+ 个菜单 key 全覆盖」，
// 而表归属此前只写在文档里 —— 于是实测 270 张表里有 **32 张是无主的孤儿表**
// （wechat_accounts / webhook_events / notifications / bridge_accounts …）。
//
// 孤儿表的危害不是文档不好看：一张表无主意味着**没人知道它随哪个模块开关**，
// 关掉某模块后它可能仍在被写入（定时任务/请求路径），数据继续增长 ——
// 也就是"关了模块不省钱也不干净"。
//
// 本表把归属变成**可校验的数据**，并由
// TestEveryModelTableHasModuleOwner 钉死「一张表都不许漏」。
// ============================================================

// 平台/系统内部表：不属于任何可售模块，随基座常在。
//
// 归 core 而不是"无主"：它们确实有主人（系统基座），只是不可关闭。
var coreOwnedTables = []string{
	// 认证 / 账号 / 审计
	"system_users", "user_mfa", "login_events", "password_history",
	"password_reset_tokens", "sso_identities", "role_menu_permissions",
	"security_audits", "security_audit_items", "security_alerts",
	"operation_logs", "user_blacklist",
	// R39 面板模板（仪表盘模板）：管理面板的展示配置，随基座常在，不属可售模块
	"dashboard_templates",
	// 告警规则：阈值/静默/通知策略，属系统基座而非某个业务模块
	"alert_rules", "alert_histories", "mcp_credentials",
	"layer_decision_logs", "tool_call_audits", "sync_logs", "saved_views",
	// 配置 / 字典 / 开关
	"system_config", "system_config_kv", "config_params", "config_param_groups",
	"config_param_audit_logs", "dictionaries", "feature_flags",
	"feature_flag_audit_logs", "feature_flag_eval_logs", "threshold_policies",
	// R5 租户化的三张：租户覆盖值与它的审计流、平台面 KV（跨租户，随基座常在）
	"tenant_config_params", "tenant_config_param_audit_logs", "platform_config_kv",
	// 存储 / 素材 / 集成 / 平台账号
	"obs_config", "platform_accounts", "platform_account_configs",
	"integration_accounts", "integration_templates", "webhook_events",
	"webhook_subscriptions", "bridge_accounts", "api_logs",
	// bridge_metrics：追加式运维指标时间序列（(metric_name, labels, ts) 维度、无
	// tenant_id、租户豁免表）——看板维度是系统健康而不是任何业务面，挂到业务模块
	// 下会让"关模块 ⇒ 表停写"说假话。与 bridge_selector_health 同口径归 core。
	"bridge_metrics",
	// email_accounts：一租户多账号的 SMTP 发信凭据。它**不是** reach-email 模块的
	// 运行数据（那是 email_jobs / email_list 那六张——reach-email 自己的发信走
	// email_smtp），而是被跨切面服务共用的账号配置源：EmailService（密码重置、
	// 注册/换绑通知、proactive 触达、告警通知经 EmailSender 抽象、customer_service_plus）
	// 全读它，channel_overview 数它的行、backup 在导出白名单里。关掉 reach-email
	// 不该导致这张配置表失主，也不存在「随某个可售模块停写」的边界。
	"email_accounts",
	// B1-a：桥接选择器健康度计数（每渠道×字段一行）。随基座常在而不是随某个可售模块：
	// 它观测的是**扩展活着与否**，任何用到桥接渠道的模块都依赖它，且它没有自己的
	// 页面/接口面（读数是运营排障用）。挂到某个模块下就意味着"关掉那个模块后
	// 这张表不再被清理"，而写入方（扩展上报口）根本不受模块闸管。
	"bridge_selector_health",
	// 租户与模块化本体
	"tenants", "tenant_modules", "module_sync_logs", "module_orders",
	"tenant_subscription_mirror",
	// 模板/技能市场写侧闭环（specs/013 S-22）：目录条目是**平台级共享橱窗**
	//（无 tenant_id，锚点见 router/cross_tenant_scan_test.go 对 detail/:id 的判词），
	// 订单与 module_orders 同一条线下收款形态。市场是 platform-link 基础模块的
	// 橱窗/购买入口——购买入口不能被购买条件关闭，两表随基座常在，归 core。
	"market_catalog_entries", "market_entry_orders",
	"llm_providers", "llm_routing_rules", "llm_routing_logs", "llm_usage_records",
	"llm_usage_daily", "trace_events",
	// 升级 / 迁移 / 运维
	"upgrade_tasks", "migration_checkpoints", "migration_records",
	"backups", "restore_records", "system_metrics", "visit_logs",
	"aggregation_watermarks", "macros",
	// 消息基础（所有 IM/工作台共用）
	"unified_messages", "unified_replies", "message_hub", "message",
	"session_messages", "message_trace", "msg_hourly_summary",
	"notifications", "notification_reads", "web_vital_records", "help_center_test_records",
	"cron_job_leases", "account", "user", "order",
}

// customer 模块独占
var customerOwnedTables = []string{
	"customers", "customer_channels", "customer_events", "customer_tags",
	"customer_tag_assignments", "customer_segments", "customer_rfm",
	"customer_rfms", "rfm_rules", "customer_do_not_contact", "customer_long_term_memory",
	"clues", "clue_scores", "clue_engagement_events",
	"user_tags", "user_rfms", "smlist", "lead_mining_configs",
}

// inbox 模块独占
var inboxOwnedTables = []string{
	"inbox_conversations", "inbox_assignments", "customer_sessions",
	"session_tags", "session_ai_summaries", "quick_replies", "quick_reply_folders",
	"agent_statuses", "customer_service_agents", "csat_surveys", "ai_suggestions",
	"sla_policies", "sla_first_response_at", "sla_escalate_at", "sla_decide_at",
	"sla_violations", "chat_channels",
}

// knowledge 模块独占
var knowledgeOwnedTables = []string{
	"knowledge_bases", "knowledge_documents", "knowledge_chunks",
	"knowledge_api_tokens", "knowledge_feedbacks", "knowledge_import_logs",
	"knowledge_openapi_sources", "knowledge_search_logs", "kb_documents",
	"kb_releases", "kb_change_audit_logs", "kb_change_requests",
	"external_kb_connectors", "external_kb_sync_items", "external_import_jobs",
	"faq_entries", "rag_products", "rag_metrics_daily", "rag_query_logs",
	// T1 低置信回流三表：写回 faq_entries，归 knowledge 模块（随知识库开关）
	"unknown_questions", "unknown_question_clusters", "unknown_question_review_logs",
	"rag_eval_runs", "rag_eval_questions", "rag_recall_monitor_snapshots",
	"rag_answer_cache",
	// kb_item_bindings：知识库成员表（P0-2/P0-3/D-8 方案 A），
	// 归 knowledge 模块：读写方只有 controller/service/repository 的 kb_item_binding 三件
	"kb_item_bindings",
	// T4 知识库冷启动三表：爬取源/爬取批次/QA 候选，随知识库模块开关
	"kb_crawler_sources", "kb_crawl_batches", "qa_candidates",
	// 父子分段的父块表（P1-1 项0，对标 Dify parent-child chunk）：检索命中小块后
	// 回拼父块上下文，读写方只有 knowledge 分片/检索两处，随知识库模块开关。
	"knowledge_chunk_parents",
	// R39-K7 知识库模板：目录橱窗的开箱模板，读写方只有 kb_template 三件套，
	// 随知识库模块开关。
	"kb_templates",
}

// asset 模块独占
var assetOwnedTables = []string{
	"local_assets", "local_asset_data", "local_asset_sync_log",
	"asset_bundles", "asset_bundle_version_logs", "script_library",
	"script_versions", "script_exposure_logs", "prompt_ab_tests",
	"prompt_candidates",
	// T5 资产市场商店化五表：上架单/版本/举报/评分/扩展用量月账，随 asset 模块开关
	"asset_listings", "asset_versions", "asset_reports", "asset_ratings", "extension_usage_monthly",
}

// ai-agent 模块独占
var aiAgentOwnedTables = []string{
	"ai_agents", "channel_agent_bindings", "agent_kb_bindings",
	"ai_tool_configs", "ai_tool_account_bindings",
	// custom_tools：用户经 OpenAPI 导入的自定义工具 Provider——与 ai_tool_configs
	// 同一条工具装配链（都是"AI 能调用什么"的登记面），随 ai-agent 开关。
	"custom_tools",
	// mcp_servers：外部 MCP server 接入配置（P1-3 客户端侧配置面），同属工具面。
	"mcp_servers",
}

// ai-sales 模块独占
var aiSalesOwnedTables = []string{
	"dialogue_memories", "business_memories", "memory_items",
	"intent_records", "intent_examples", "sales_intent_scores",
	"objection_templates", "sales_personas",
	"sales_champion_profile_snapshots", "champion_baselines",
	"champion_dialogues", "champion_phrases",
	"sop_agents", "sop_templates", "sop_executions", "sop_node_transitions",
	"sop_state_memories", "sop_timers", "sop_outbox", "sop_exec_events",
	"confidence_signals", "confidence_calibrations", "humanize_scores",
	"humanize_dimensions", "feedback_records", "feedback_signals",
	"feedback_events", "learning_insights", "bandit_reflux_log",
	"low_quality_samples", "dialogue_eval_runs", "trace_eval_log",
	// dialogue_eval_sets：导入的对话评测集（P0-1 CSV 批量导入的落点）——
	// 与 dialogue_eval_runs 同一评测域（集 → 跑），随 ai-sales 开关。
	"dialogue_eval_sets",
}

// reach-auto 模块独占
var reachAutoOwnedTables = []string{
	"reach_pipelines", "reach_jobs", "reach_delayed_outbound", "reach_compliance_log",
	// D02/D04/D05（2026-10-28）：延迟出站持久队列、同意事实账与偏好、配额预占账——
	// 均属主动触达域，随 reach-auto 开关。
	"reach_deferred_outbound", "contact_consent_events", "contact_preferences",
	"reach_quota_counters", "reach_quota_reservations",
	"automation_rules", "rule_pending_executions", "optimization_suggestions",
	// 工作流编排器（workflowOrchestrator 菜单节点同属本模块）
	"workflow_versions", "workflow_executions", "workflow_node_executions",
}

// reach-sms / reach-email 模块独占
var reachSmsOwnedTables = []string{
	"sms", "sms_unsubscribes", "sms_tracking", "sms_delivery_statuses",
	"sms_number_portability_logs",
}

var reachEmailOwnedTables = []string{
	"email_list", "email_smtp", "email_jobs", "email_tracking_events",
	"email_unsubscribes", "email_job_metrics",
}

// reach-social 模块独占
var reachSocialOwnedTables = []string{
	"douyin_cards", "douyin_card_activities", "kuaishou_cards",
	"kuaishou_card_activities", "xiaohongshu_cards", "xiaohongshu_card_activities",
	"xianyu_cards", "xianyu_card_activities", "tiktok_cards", "tiktok_card_activities",
	"wechat_accounts", "wechat_messages",
}

// reach-link 模块独占
var reachLinkOwnedTables = []string{
	"short_links", "short_link_accesses", "live_codes", "live_code_qrs",
	"live_code_qr_stats", "livecode_click_log", "qr_code_click_log",
	"domain_pool", "domain_blacklist", "domain_health_log",
}

// im-wecom 模块独占
var imWecomOwnedTables = []string{
	"wecom_accounts", "wecom_account_health", "wecom_groups",
	"wecom_group_members", "wecom_messages", "wecom_tags", "wecom_customers",
}

// im-intl 模块独占
var imIntlOwnedTables = []string{
	"telegram_accounts", "telegram_group_gates", "telegram_group_members",
	"qq_accounts", "feishu_accounts", "feishu_customers", "feishu_messages",
	"dingtalk_app_accounts", "whatsapp_accounts", "whatsapp_sessions",
	"whatsapp_messages", "whatsapp_drafts", "whatsapp_jobs", "whatsapp_job_details",
	"whatsapp_message_queues", "whatsapp_queue_statuses", "whatsapp_group_messages",
	"whatsapp_message_templates", "whatsapp_cloud_accounts",
	"community_groups", "community_members", "community_messages",
}

// analytics / experiment 模块独占
var analyticsOwnedTables = []string{
	"daily_stats", "conversion_funnels", "report_subscriptions",
	"customer_journey", "external_customers",
	"support_metrics",
}

var experimentOwnedTables = []string{
	"churn_scores", "bandit_arms", "recovery_queue",
}

// sales-crm 模块独占
var salesCRMOwnedTables = []string{
	"opportunities", "quotes", "quote_line_items", "order_drafts",
	"bills", "payments", "after_sales", "sales_events", "external_orders",
	"external_products",
}

// geo 模块独占
var geoOwnedTables = []string{
	"geo_brands", "geo_domains", "geo_keywords", "geo_query_chains",
	"geo_contents", "geo_entities", "geo_relations", "geo_competitors",
	"geo_crawler_visits", "geo_index_records", "geo_alerts", "geo_reports",
	"geo_decisions", "geo_publish_jobs", "geo_sites", "geo_push_logs",
	"geo_verifications", "geo_sov_snapshots", "geo_dict", "geo_tasks",
}

// approval / i18n / browser-automation 模块独占
var approvalOwnedTables = []string{
	"approval_requests", "human_tasks", "bad_cases", "handoff_decisions",
}

var i18nOwnedTables = []string{"glossaries", "dictionary_transitions"}

// llmCloudOwnedTables 平台模型算力（计量型收费模块）独占
var llmCloudOwnedTables = []string{
	"llm_wallets", "llm_wallet_txns", "platform_llm_usage_records",
}

// browserAutomationOwnedTables 浏览器自动化独占
//
// 常驻 Chromium 会话与任务执行记录。没有专属表时它确实"关了什么都不停"，
// 但那是实现现状而非它不需要数据 —— 后续补表时往这里加即可，归属已经先立住。
//
// 十二张表一张不少（逐表对过 `internal/browser_automation/model`
// 的 TableName 取值与开发库 pg_tables）：
//   - 去掉 browser_profiles：模型已不存在（Profile 概念并入 browser_profile_health），
//     开发库里也没有这张表——登记一张不存在的表等于让「这张表归谁」这件事说假话。
//   - 补上此前漏登的六张：browser_sessions / browser_steps / browser_command_log /
//     browser_cron_triggers / browser_llm_plans / browser_audit_digests /
//     browser_write_claims。漏登的后果不是「读不出来」——本表目前还没有生产消费方
//     （OwnerOfTable/TablesOfModule 全仓零引用），而是**归属这件事没被说清**：
//     以后真按「关模块 ⇒ 这张表跟着停」做开关时，漏登的那张表会继续被后台任务写。
var browserAutomationOwnedTables = []string{
	"browser_tasks", "browser_sessions", "browser_steps",
	"browser_command_log", "browser_cron_triggers", "browser_llm_plans",
	"browser_audit_digests", "browser_audit_prune_runs",
	"browser_write_claims", "browser_outreach_dedupe",
	"browser_outreach_receipts", "browser_profile_health",
}

// egoLiteOwnedTables ego-lite 腿独占（设计文档 §一：四张物理新表，物理独立于
// browser_automation；模块开关上随 browser-automation 一起——双腿属同一收费模块，
// 表归属独立登记只是为了让「这四张表是谁的」这件事说清）
var egoLiteOwnedTables = []string{
	"egolite_executions", "ego_event_log", "egolite_flows", "egolite_write_claims",
	"egolite_assets", "egolite_runner_tokens", "egolite_flow_crons",
	"egolite_sessions", "egolite_rounds",
	// runner 遥测（S-9）：口径白名单在 egolite/telemetry.go，随 ego 腿一起开关。
	"egolite_telemetry",
}

// moduleTableRegistry 模块 → 独占表
var moduleTableRegistry = map[string][]string{
	ModuleCore:              coreOwnedTables,
	ModuleCustomer:          customerOwnedTables,
	ModuleInbox:             inboxOwnedTables,
	ModuleKnowledge:         knowledgeOwnedTables,
	ModuleAsset:             assetOwnedTables,
	ModuleAIAgent:           aiAgentOwnedTables,
	ModuleAISales:           aiSalesOwnedTables,
	ModuleReachAuto:         reachAutoOwnedTables,
	ModuleReachSMS:          reachSmsOwnedTables,
	ModuleReachEmail:        reachEmailOwnedTables,
	ModuleReachSocial:       reachSocialOwnedTables,
	ModuleReachLink:         reachLinkOwnedTables,
	ModuleIMWecom:           imWecomOwnedTables,
	ModuleIMIntl:            imIntlOwnedTables,
	ModuleAnalytics:         analyticsOwnedTables,
	ModuleExperiment:        experimentOwnedTables,
	ModuleSalesCRM:          salesCRMOwnedTables,
	ModuleGEO:               geoOwnedTables,
	ModuleApproval:          approvalOwnedTables,
	ModuleI18N:              i18nOwnedTables,
	ModuleLLMCloud:          llmCloudOwnedTables,
	ModuleBrowserAutomation: append(browserAutomationOwnedTables, egoLiteOwnedTables...),
}

// TableOwners 表 → 模块 反查表（启动时构建）
var TableOwners = buildTableOwners()

func buildTableOwners() map[string]string {
	out := make(map[string]string, 512)
	for mk, tables := range moduleTableRegistry {
		for _, t := range tables {
			out[t] = mk
		}
	}
	return out
}

// OwnerOfTable 返回该表所属模块；无主返回空串。
func OwnerOfTable(table string) string { return TableOwners[table] }

// TablesOfModule 返回某模块独占的表清单
func TablesOfModule(moduleKey string) []string {
	out := append([]string(nil), moduleTableRegistry[moduleKey]...)
	return out
}

// TotalRegisteredTables 登记在册的表总数
func TotalRegisteredTables() int { return len(TableOwners) }
