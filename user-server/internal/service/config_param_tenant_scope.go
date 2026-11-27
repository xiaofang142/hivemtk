package service

import (
	"fmt"

	"hivemtk-user/internal/model"
)

// 参数的租户可见性。
//
// 「这个键能不能按租户改」不是运维填一个值就能定的：它取决于**读取点有没有把请求的租户
// 带进参数中心**。库里存了一条租户覆盖行、而读取点用 context.Background() 解析平台域时，
// 那次写入就是不生效——页面按「保存成功」回执，行为却照旧，这是最难查的一类不一致
// （本仓对同一形状的处理先例是 `未接线` 与 `Deprecated`：宁可显式承认不生效，
// 也不留一个看起来能用的旋钮）。
//
// 三档：
//   不在下面两张表里   scope=inherit   读取点带请求 ctx ⇒ 租户覆盖真生效（默认档）；
//   platformScopedParams               本就该全平台一个值（进程资源、上游/渠道硬限制、
//                                      全站统一的安全与并发窗口），租户写一律拒绝；
//   tenantPendingParams                业务口径确实该按租户走，但读取点目前解析平台域，
//                                      接线之前同样拒绝，接线之后必须从本表删掉。
//
// 判档的事实源是代码里的读取形状，不是这里的字：
// scripts/check-config-param-readpoints.py 会重算「所有用 context.Background()／装配层 bg
// 读取的键」并与这两张表逐键对账——表里多一个键（读点已接 ctx 却还拦着租户写）
// 和少一个键（改了不生效却允许写）都判红。

var platformScopedParams = map[string]string{
	// —— 进程内的共享资源：一个进程只有一份，按租户配会互相挤兑
	"cache.faq_decay_max_batch":             "FAQ 衰减任务单轮批量",
	"cache.max_keys":                        "内存缓存注册表的键数上限",
	"cache.memo_ttl":                        "安装 memo 的存活时长",
	"cache.platform_cache_ttl":              "平台信息缓存的存活时长",
	"cache.translation_cache_max_entries":   "翻译缓存条目上限",
	"middleware.audit_flush_interval":       "审计日志刷盘节拍",
	"middleware.max_json_body_mb":           "单请求 JSON 体上限（进程解析内存的闸门）",
	"misc.agentloop_history_max_candidates": "Agent loop 候选历史条数（token 成本闸门）",
	"misc.backup_page_size":                 "备份分页大小（后台任务节奏）",
	"misc.batch_import_max_file_mb":         "批量导入文件体积上限",
	"misc.csv_export_max_rows":              "导出行数上限（进程内存与超时闸门）",
	"misc.domain_check_concurrency":         "域名检查并发度",
	"misc.ownership_cache_ttl":              "归属校验结果缓存时长",
	"misc.polling_lock_stale_threshold":     "轮询实例失联判定",
	"misc.sop_scheduler_interval":           "SOP 调度扫描节拍",
	"misc.text_truncate_max_bytes":          "文本截断字节上限（日志与正文体积闸门）",
	"telemetry.feature_flag_poll_interval":  "功能开关拉取节拍",
	"telemetry.geo_position_window":         "地理位置去重窗口",
	"telemetry.node_health_window":          "节点健康统计窗口",
	"telemetry.trace_sink_buffer":           "链路写入缓冲深度",
	"workflow.max_executed_node_trace":      "单次执行留痕节点数上限",
	"workflow.max_running_per_sop":          "每个 SOP 的并行执行上限",
	"workflow.max_subflow_depth":            "子流程嵌套深度上限",
	"workflow.max_workflow_steps":           "单次流程步数上限",

	// —— 上游服务或渠道协议的硬限制：租户改成更大的值只会让调用失败
	"agent_llm.db_sink_stop_deadline":          "链路落库协程退出等待",
	"agent_llm.default_circuit_open_duration":  "provider 熔断打开时长",
	"agent_llm.default_failure_threshold":      "provider 判失败的连续次数",
	"agent_llm.default_health_check_interval":  "provider 健康检查节拍",
	"agent_llm.default_health_check_timeout":   "provider 健康检查超时",
	"agent_llm.default_http_timeout":           "LLM provider 默认请求超时",
	"agent_llm.geo_max_tokens_per_call":        "地理推断单次 token 上限",
	"agent_tool.cooldown_duration":             "工具熔断冷却时长",
	"agent_tool.fail_threshold":                "工具熔断失败次数",
	"agent_tool.max_concurrent":                "工具执行并发度",
	"agent_tool.max_content_len":               "工具回内容长度上限",
	"channelbot.http_timeout":                  "渠道 HTTP 请求超时",
	"channelbot.qq_message_max_len":            "QQ 单条消息长度上限",
	"channelbot.tg_inline_buttons_per_row_max": "Telegram 内联键盘每行按钮上限",
	"channelbot.tg_inline_rows_max":            "Telegram 内联键盘行数上限",
	"channelbot.tg_message_max_length":         "Telegram 单条消息长度上限",
	"confidence.emb_retry_cooldown":            "向量服务重试冷却",
	"frontend_ui.material_max_size_mb":         "素材上传体积闸门",
	"knowledge.async_processing_timeout":       "后台入库任务预算",
	"knowledge.embedding_dimension":            "向量维度：改动等于换向量空间，已入库向量全废",
	"knowledge.external_import_timeout":        "外部导入任务预算",
	"knowledge.max_upload_file_size":           "知识库上传体积闸门",
	"knowledge.merchant_batch_max_file_size":   "商品批量上传体积闸门",
	"knowledge.ssrf_check_timeout":             "SSRF 校验超时（安全闸门）",
	"llm.global_override":                      "全局 LLM 覆盖（平台级，管理端写平台默认行）",
	"embedding.global_override":                "全局向量覆盖（平台级，管理端写平台默认行）",
	"misc.deepl_timeout":                       "DeepL 请求超时",
	"misc.sms_max_retry":                       "短信通道重试次数（上游配额行为）",

	// —— 全站统一的安全窗口与并发锁窗口：与身份体系绑定，不按租户分叉
	"lock.ai_processing_ttl":               "AI 处理占锁时长",
	"lock.inbox_content_dedup_ttl":         "收件内容去重窗口",
	"lock.inbox_human_ttl":                 "人工接管占锁时长",
	"lock.inbox_pending_ttl":               "待处理占锁时长",
	"lock.ingest_lock_ttl":                 "入库锁时长",
	"lock.message_hub_idem_ttl":            "消息幂等窗口",
	"middleware.mfa_recent_verify_ttl":     "MFA 近期验证窗口",
	"misc.sso_cookie_ttl":                  "SSO Cookie 存活时长",
	"misc.visitor_token_ttl":               "访客令牌存活时长",
	"ratelimit.global_bucket":              "全局限流桶深",
	"ratelimit.global_rps":                 "全局限流速率",
	"ratelimit.visitor_per_channel_bucket": "按渠道限流桶深",
	"ratelimit.visitor_per_channel_rps":    "按渠道限流速率",
	"ratelimit.visitor_per_ip_bucket":      "按 IP 限流桶深",
	"ratelimit.visitor_per_ip_rps":         "按 IP 限流速率",
	"sales.identity_max_attempts":          "身份解析尝试次数上限",

	// —— 平台端通信地址：这台部署连哪个平台，是**部署级**事实不是租户级事实。
	// 按租户分叉会让「一个租户填错地址 ⇒ 全租户的资产市场/订阅/钱包一起拉空」，
	// 而它们本该互不相干。SaaS 模式下所有租户连同一个平台。
	"platform.api_host": "平台端 API 地址（部署级，租户写一律拒绝）",

	// —— SaaS 配置面改造（2026-11-17）env→参数中心迁移批次：读取点带
	//    context.Background()（进程级装配/异步链），租户写会被门禁对账判红。
	"webhook.recovery_enabled":             "恢复扫描器总开关（构造期读）",
	"webhook.recovery_interval_seconds":    "恢复扫描间隔（构造期读）",
	"webhook.recovery_cooldown_seconds":    "恢复重放冷却（构造期读）",
	"webhook.recovery_batch":               "恢复单批容量（构造期读）",
	"agent_llm.ai_quiet_hours_disabled":    "AI 勿扰停用（异步排水链无请求 ctx）",
	"agent_llm.humanize_eval_disabled":     "拟人评估停用（启动期读一次）",
	"agent_llm.humanize_eval_threshold":    "拟人达标阈值（启动期读一次）",
	"misc.brain_token_budget":              "浏览器自动化 token 预算（进程成本闸）",
	"misc.file_url_ttl":                    "签名 URL 时效（进程级签名器）",
	"etl.parser_sidecar_url":               "外挂解析端点（进程级，索引一致性）",
	"etl.image_describe_url":               "图片描述端点（进程级推理服务）",
	"etl.image_describe_model":             "图片描述模型名（随端点）",
	"rag_health.embedding_critical":        "健康探活关键标记（实例级）",
	"knowledge.contextual_enhance_enabled": "上下文增强开关（装配期读）",
	"reach.quiet_hours_disabled":           "触达免打扰停用（装配期读）",
	"alerting.email_enabled":               "告警邮件开关（实例级运维面）",
	"alerting.email_recipients":            "告警邮件收件人（实例级运维面）",
	"alerting.webhook_url":                 "告警 Webhook（实例级运维面）",
}

var tenantPendingParams = map[string]string{
	// —— 检索与生成的业务口径：租户本可以各自调，但读取点现在解析平台域（接线待做）
	"agent_llm.agent_loop_drift_factor":    "Agent 循环成本漂移系数",
	"agent_llm.max_retries":                "生成重试次数",
	"agent_llm.max_tokens":                 "生成 token 上限",
	"agent_llm.minor_llm_threshold":        "小模型分流阈值",
	"agent_llm.request_timeout":            "生成请求超时",
	"agent_llm.temperature":                "生成温度",
	"agent_llm.top_p":                      "核采样参数",
	"agent_llm.vote_agreement_threshold":   "多路投票一致阈值",
	"knowledge.bm25_scan_limit":            "BM25 扫描上限",
	"knowledge.chunk_preview_max_len":      "分片预览长度",
	"knowledge.default_top_k":              "检索默认返回条数",
	"knowledge.max_search_list_size":       "检索列表上限",
	"knowledge.merchant_knowledge_max_len": "商品知识正文长度",
	"knowledge.similarity_threshold":       "RAG 相似度阈值",

	// —— 会话与线索判定的业务口径：同上
	"inbox_sales.geo_lead_preview_max_len":      "地理线索预览长度",
	"inbox_sales.preview_sample_limit":          "预览样本条数",
	"inbox_sales.tg_lead_opportunity_threshold": "Telegram 线索成机阈值",
	"inbox_sales.unified_miner_lead_threshold":  "统一挖掘成机阈值",
	"memory.l1_ttl_hours":                       "短期记忆 TTL",
	"pagination.cursor_page_size":               "游标分页每页条数",
	"sales.audience_default_limit":              "受众默认拉取条数",
	"sales.audience_max_limit":                  "受众最大拉取条数",
	"session.active_ttl":                        "会话活跃时长",
	"session.idle_ttl":                          "会话闲置判定",
	"session.max_delay_seconds":                 "会话最大延迟",
	"smart_cs.confidence_threshold":             "智能客服置信度阈值",
	"smart_cs.enable_auto_reply":                "智能客服自动回复开关",
	"smart_cs.max_ai_consecutive":               "智能客服连续 AI 回复上限",

	// —— 拟人度与置信度调参：同上
	"confidence.humanize_boundary_high":     "拟人度边界上界",
	"confidence.humanize_boundary_low":      "拟人度边界下界",
	"confidence.humanize_default_threshold": "拟人度达标阈值",
	"confidence.humanize_max_retry":         "拟人度重生成次数",
	"confidence.humanize_sample_rate":       "拟人度采样率",
	"confidence.intent_fewshot_min_cos":     "意图少样本命中余弦阈值",
	"confidence.veto_low_rag_threshold":     "低 RAG 否决阈值",
	"confidence.weak_truth_min_confidence":  "弱真值最低置信度",

	// —— 编辑/摘要类行为窗口：同上
	"misc.edit_lock_ttl":           "编辑锁时长",
	"misc.preview_max_len":         "正文预览长度",
	"misc.reply_sem_timeout":       "回复语义分析超时",
	"misc.sop_max_wait":            "SOP 定时器最长等待",
	"misc.summary_stale_threshold": "摘要过期判定阈值",
}

// 三档的取值。管理端拿到 tenant_scope 后决定「租户覆盖」这一列是可用、
// 禁用（平台级）还是标成待接线。
const (
	tenantScopeInherit  = "inherit"
	tenantScopePlatform = "platform"
	tenantScopePending  = "pending"
)

// fullParamKey 把参数中心的两段坐标拼成表里用的单段键。
func fullParamKey(group, key string) string { return group + "." + key }

// paramTenantScope 返回该键的租户可见性与人类可读的原因。
// 第一返回值为空串表示 inherit（默认档，不在任何表里）。
func paramTenantScope(group, key string) (scope, reason string) {
	full := fullParamKey(group, key)
	if r, ok := platformScopedParams[full]; ok {
		return tenantScopePlatform, r
	}
	if r, ok := tenantPendingParams[full]; ok {
		return tenantScopePending, r
	}
	return "", ""
}

// stampTenantScope 给读出的一批定义行盖上租户可见性。
//
// 必须早于任何提前返回：平台管理员（无租户上下文）也要看到「这个旋钮租户能不能改」，
// 否则页面只对租户说话、对平台只字不提，而这两批人恰恰是需要一起决定接线顺序的人。
func stampTenantScope(params []model.ConfigParam) {
	for i := range params {
		scope, reason := paramTenantScope(params[i].Group, params[i].Key)
		if scope == "" {
			scope = tenantScopeInherit
		}
		params[i].TenantScope = scope
		params[i].TenantScopeReason = reason
	}
}

// tenantScopedWriteErr 判断一次租户写该不该被拦下；不该拦时返回 nil。
//
// 平台级与待接线给的是两句不同的话：前者是「这个旋钮本来就不该按租户分叉」，
// 后者是「应该按租户分叉但还没接线，暂时别指望它」——把后者说成前者会把债说成设计。
func tenantScopedWriteErr(group, key string) error {
	switch scope, reason := paramTenantScope(group, key); scope {
	case tenantScopePlatform:
		return platformScopedWriteErr(fullParamKey(group, key), reason)
	case tenantScopePending:
		return tenantPendingWriteErr(fullParamKey(group, key), reason)
	}
	return nil
}

func platformScopedWriteErr(full, reason string) error {
	return fmt.Errorf("参数 %s 是平台级参数（%s），不接受租户覆盖；需要调整请在平台参数里改", full, reason)
}

func tenantPendingWriteErr(full, reason string) error {
	return fmt.Errorf("参数 %s（%s）的租户覆盖还没接线：读取点目前按平台域解析，"+
		"现在保存只会写出一条不生效的覆盖行。要按租户生效请先把它接进请求上下文", full, reason)
}
