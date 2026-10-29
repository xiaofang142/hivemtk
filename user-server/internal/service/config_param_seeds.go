package service

// ParamDef 单个参数定义（用于 Seed 初始化）
type ParamDef struct {
	Group        string
	Key          string
	Name         string
	Description  string
	ValueType    string
	DefaultValue string
	Min          *string
	Max          *string
	Step         *string
	// Unit 数字的单位（秒/条/MB/%/倍…），只用于 UI 展示。见 model.ConfigParam.Unit。
	Unit string
	// Risk 危险等级 ""/"warning"/"danger"，驱动 UI 的二次确认。见 model.ConfigParam.Risk。
	Risk string
	// Deprecated 下架标记。
	//
	// 「登记了但没人读」有两种截然不同的成因：一种是接线漏了（债），一种是那个功能
	// 压根没上线（不是债，比如整条路由从未注册、死代码被 //nolint:unused 标着）。
	// 这两种混在同一张表里、只靠 Description 里一句「未接线」区分，结果是页面给用户
	// 挂着一个改了不生效的旋钮——用户改完排查半天，问题却出在代码里而不是配置里。
	//
	// 下架才是正确处置：值与审计留痕可查，但默认不再出现在管理端列表。
	// DeprecatedReason 必须写清「为什么接不上」与「要接上得先做什么」，
	// 否则它就是一笔新的糊涂账。
	Deprecated       bool
	DeprecatedReason string
	ReadOnly         bool
	Restart          bool
	Category         string
}

func strPtr(s string) *string { return &s }

// DefaultParamDefs 返回全部默认参数定义
//
// 来源：项目内 ~60 处硬编码 const（bridge/handler_http.go、
// knowledge/service/constants.go、smart_cs_orchestrator.go、
// customer_session.go、cache/memory.go 等）。
//
// 设计原则：
//  1. 每一条对应一个现存的硬编码 const
//  2. Group 按业务域划分：bridge / knowledge / agent_llm / smart_cs /
//     session / cache / pagination / wecom / telemetry / sales / misc
//  3. Min/Max/Step 基于参数语义的合理范围（用于 UI slider/number input）
//
// 「登记」不等于「接通」：本清单会被参数中心整份渲染成可编辑控件，而只有被生产代码
// 读走的那些改了才真生效。没读取点的条目一律在 Name 上挂「（未接线）」、Description 上挂
// 「【当前不生效，改了也没人读】」，让页面自己说清这条能不能用；将来把它接线之后，
// 必须把这两句撤掉，否则门会报 STALE。
// 判据与当前读数（有读取点／已声明未接线 各多少条）由 scripts/check-config-param-readpoints.py
// 现测打印，本注释不复述数字（复述的那个数在代码一改之后就会漂）。
func DefaultParamDefs() []ParamDef {
	return []ParamDef{

		{Group: "permission", Key: "role_permissions_json", Name: "角色权限矩阵",
			Description: "JSON: {角色码: [权限项]}；权限项支持 module.* 通配；admin 恒全权不走此配置",
			ValueType:   "json", Risk: "danger", DefaultValue: `{"customer_service":["cards.*","shortlinks.*","clues.*","autoreply.*"],"staff":["cards.view","shortlinks.view","clues.view"]}`, Category: "权限"},

		{Group: "reach", Key: "global_per_user_daily_limit", Name: "单客户全管线日触达上限",
			Description: "同一客户被全部触达管线共享的每日（CST 自然日）触达上限；0=禁用该层",
			ValueType:   "int", Unit: "次/天", Risk: "warning", DefaultValue: "3", Min: strPtr("0"), Max: strPtr("50"), Category: "触达"},

		{Group: "reach", Key: "compliance_policy_json", Name: "主动触达合规策略（地区×行业）",
			Description: "JSON 文档，按地区/行业收紧主动触达：{enabled, default_region, unknown_region_deny, " +
				"regions{US:{allowed_channels,denied_channels,quiet_hours_start,quiet_hours_end,daily_limit,require_opt_in}}, " +
				"industries{medical:{...同一组字段}}}。空对象或 enabled=false 即不启用（行为与升级前一致）。" +
				"判定优先级：渠道拒绝>渠道白名单>缺同意>静默时段(延后)>日上限；未命中地区规则且 unknown_region_deny=false 时放行并留痕 region_unruled",
			ValueType: "json", Risk: "danger", DefaultValue: "{}", Category: "触达"},

		{Group: "bridge", Key: "polling_max_timeout", Name: "轮询最大超时（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】服务端没有 HTTP 长轮询实现：ingest 立即返回，回复由扩展走 SSE 或 /outbox 轮询。internal/bridge/handler_http.go 的 HTTPPollingMaxTimeout 全树只有 defaults_test.go 引用。要启用请先实现长轮询分支，并把该常量改成本参数的读取口",
			ValueType:   "duration", Deprecated: true, DeprecatedReason: "服务端没有 HTTP 长轮询实现：ingest 立即返回，回复由扩展走 SSE 或 /outbox 轮询。两个兜底常量 HTTPPollingMaxTimeout / HTTPPollingDefaultTimeout 已随本次清理删除（它们全树只有默认值测试引用）。要重新提供这个旋钮，先在 internal/bridge/handler_http.go 实现长轮询分支并给等待加 ctx.Done() 退出，再把新的兜底常量登记回本条", DefaultValue: "500", Min: strPtr("10"), Max: strPtr("3600"), Step: strPtr("5")},

		{Group: "bridge", Key: "polling_default_timeout", Name: "轮询默认超时（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】同上，长轮询未实现。bridge/handler_http.go 的 HTTPPollingDefaultTimeout 全树只有 defaults_test.go 引用",
			ValueType:   "duration", Deprecated: true, DeprecatedReason: "同上，长轮询未实现，两个兜底常量已删除。要重新提供这个旋钮，先实现长轮询分支，再登记回本条", DefaultValue: "30", Min: strPtr("5"), Max: strPtr("120"), Step: strPtr("1")},

		{Group: "bridge", Key: "ingest_max_body_bytes", Name: "Ingest 请求体上限",
			Description: "bridge/ingest 单次请求体最大字节数",
			ValueType:   "int", Unit: "字节", Risk: "danger", DefaultValue: "4194304", Min: strPtr("1048576"), Max: strPtr("67108864")},
		{Group: "bridge", Key: "ingest_max_messages", Name: "Ingest 单次消息数上限",
			Description: "单次上报最多携带的消息条数",
			ValueType:   "int", Unit: "条", DefaultValue: "200", Min: strPtr("10"), Max: strPtr("2000"), Step: strPtr("10")},
		{Group: "bridge", Key: "sse_heartbeat_interval", Name: "SSE 心跳间隔",
			Description: "SSE 空闲时服务端向客户端发送心跳包的间隔（秒）",
			ValueType:   "duration", DefaultValue: "15", Min: strPtr("5"), Max: strPtr("60"), Step: strPtr("1")},
		{Group: "bridge", Key: "max_reply_content_bytes", Name: "回复内容截断上限（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】internal/bridge/bridge_helpers.go 的 maxReplyContentBytes 标注了 nolint:unused——生产路径没有任何回复内容截断逻辑。要启用请在出网前对该字段做截断，并把它接到本参数",
			ValueType:   "int", Unit: "字节", Risk: "danger", Deprecated: true, DeprecatedReason: "internal/bridge/bridge_helpers.go 的 maxReplyContentBytes 常量已随本次清理删除——它带 //nolint:unused，生产路径从未做过任何回复内容截断，留下一个只被测试引用的常量与一个改了不生效的旋钮是双份糊涂账。要重新提供这个旋钮，先在出网路径上对该字段做真实截断，再登记回本条", DefaultValue: "4096", Min: strPtr("1024"), Max: strPtr("65536"), Step: strPtr("256")},

		{Group: "bridge", Key: "max_ack_msg_ids", Name: "ACK 单次消息ID数上限",
			Description: "客户端一次 ACK 最多携带的消息 ID 数",
			ValueType:   "int", Unit: "个", DefaultValue: "500", Min: strPtr("50"), Max: strPtr("5000"), Step: strPtr("50")},
		{Group: "bridge", Key: "online_grace_window", Name: "在线状态宽限窗口",
			Description: "渠道账号心跳丢失后仍判定为在线的宽限时间（秒）",
			ValueType:   "duration", DefaultValue: "30", Min: strPtr("10"), Max: strPtr("300"), Step: strPtr("5")},
		{Group: "bridge", Key: "outbound_orphan_ttl", Name: "出站孤儿行结算阈值",
			Description: "桥接渠道出站消息在「账号未注册或已超过此时长未同步」时，行本身超过此时长即落 failed（原因 outbound_orphan_expired）；0 表示停用该结算",
			ValueType:   "duration", DefaultValue: "604800", Min: strPtr("0"), Max: strPtr("7776000"), Step: strPtr("3600")},
		{Group: "bridge", Key: "outbound_orphan_dry_run", Name: "出站孤儿行结算只报数",
			Description: "true（缺省）＝后台回扫只统计并打印候选行数，不写库；确认过报数后改 false 才真正把孤儿行落 failed。落 failed 不可逆（内容不动，但状态离开待办集合），所以开关默认停在报数一侧",
			ValueType:   "bool", Risk: "warning", DefaultValue: "true"},

		{Group: "knowledge", Key: "default_top_k", Name: "默认 Top-K 检索数",
			Description: "向量检索返回的候选文档数",
			ValueType:   "int", Unit: "条", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("50"), Step: strPtr("1")},
		{Group: "knowledge", Key: "similarity_threshold", Name: "相似度阈值",
			Description: "向量检索相似度低于此值视为无相关内容（0-1）",
			ValueType:   "float", DefaultValue: "0.5", Min: strPtr("0.1"), Max: strPtr("0.95"), Step: strPtr("0.05")},
		{Group: "knowledge", Key: "chunk_preview_max_len", Name: "分片内容预览长度",
			Description: "检索结果 Content 字段截断长度",
			ValueType:   "int", Unit: "字符", DefaultValue: "500", Min: strPtr("100"), Max: strPtr("5000"), Step: strPtr("50")},
		{Group: "knowledge", Key: "max_search_list_size", Name: "检索扫描上限",
			Description: "BM25-lite 兜底路径扫描的最大条目数",
			ValueType:   "int", Unit: "条", DefaultValue: "1000", Min: strPtr("100"), Max: strPtr("100000"), Step: strPtr("100")},
		{Group: "knowledge", Key: "bm25_scan_limit", Name: "BM25 文本匹配上限",
			Description: "BM25 关键词匹配扫描的最大分片数",
			ValueType:   "int", Unit: "条", DefaultValue: "10000", Min: strPtr("1000"), Max: strPtr("500000"), Step: strPtr("1000")},
		{Group: "knowledge", Key: "embedding_dimension", Name: "Embedding 维度",
			Description: "向量嵌入维度（bge-m3 默认 1024）。改它必须把所有已入库的向量按新维度重新生成并重建索引，" +
				"不是改个数字就生效的参数——维度与存量向量不一致时检索结果全是噪声，而且错得很安静（不报错，只是答非所问）。" +
				"所以这里只读：要换维度，走「换嵌入模型 + 全量重建索引」的运维流程。",
			ValueType: "int", Unit: "维", DefaultValue: "1024", Min: strPtr("128"), Max: strPtr("8192"), ReadOnly: true},
		{Group: "knowledge", Key: "async_processing_timeout", Name: "异步处理超时",
			Description: "文档异步处理（分片+embedding+索引）总超时（秒）",
			ValueType:   "duration", DefaultValue: "900", Min: strPtr("60"), Max: strPtr("7200"), Step: strPtr("60")},
		{Group: "knowledge", Key: "external_import_timeout", Name: "外部导入超时",
			Description: "飞书/Notion 外部导入总超时（秒）",
			ValueType:   "duration", DefaultValue: "1800", Min: strPtr("60"), Max: strPtr("7200"), Step: strPtr("60")},
		{Group: "knowledge", Key: "ssrf_check_timeout", Name: "SSRF 防护超时",
			Description: "外部 URL SSRF 校验的单次 HTTP 超时（秒）",
			ValueType:   "duration", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("60"), Step: strPtr("1")},
		{Group: "knowledge", Key: "embedding_max_batch", Name: "Embedding 最大批大小",
			Description: "单次 embedding 向量化调用的最大文本批数",
			ValueType:   "int", Unit: "条", DefaultValue: "64", Min: strPtr("1"), Max: strPtr("1024"), Step: strPtr("8")},

		{Group: "agent_llm", Key: "temperature", Name: "LLM Temperature",
			Description: "采样温度，越大输出越发散（0-2）",
			ValueType:   "float", Risk: "warning", DefaultValue: "0.7", Min: strPtr("0.1"), Max: strPtr("2.0"), Step: strPtr("0.1")},
		{Group: "agent_llm", Key: "max_tokens", Name: "LLM Max Tokens",
			Description: "单次 LLM 调用最大生成 token 数",
			ValueType:   "int", Unit: "token", DefaultValue: "1000", Min: strPtr("64"), Max: strPtr("8192"), Step: strPtr("64")},
		{Group: "agent_llm", Key: "top_p", Name: "LLM Top-P",
			Description: "核采样概率阈值（0-1）",
			ValueType:   "float", Risk: "warning", DefaultValue: "0.9", Min: strPtr("0.5"), Max: strPtr("1.0"), Step: strPtr("0.05")},
		{Group: "agent_llm", Key: "request_timeout", Name: "LLM 请求超时",
			Description: "单次 LLM HTTP 请求超时（秒）",
			ValueType:   "duration", DefaultValue: "60", Min: strPtr("5"), Max: strPtr("600"), Step: strPtr("5")},
		{Group: "agent_llm", Key: "max_retries", Name: "LLM 最大重试",
			Description: "LLM 调用失败后自动重试次数",
			ValueType:   "int", Unit: "次", DefaultValue: "3", Min: strPtr("0"), Max: strPtr("10"), Step: strPtr("1")},
		{Group: "agent_llm", Key: "agent_loop_drift_factor", Name: "AgentLoop 漂移因子",
			Description: "智能体循环步数相对阈值的放宽倍数（超过阈值×factor 触发降级）",
			ValueType:   "float", Unit: "倍", DefaultValue: "2.5", Min: strPtr("1.0"), Max: strPtr("10.0"), Step: strPtr("0.1")},
		{Group: "agent_llm", Key: "minor_llm_threshold", Name: "轻量 LLM 意图阈值",
			Description: "意图识别置信度低于此值时只调用轻量模型（非完整 LLM）",
			ValueType:   "float", Risk: "warning", DefaultValue: "0.6", Min: strPtr("0.3"), Max: strPtr("0.95"), Step: strPtr("0.05")},
		{Group: "agent_llm", Key: "geo_max_tokens_per_call", Name: "Geo LLM 单调 Token 上限",
			Description: "内容分析 Geo 场景单次 LLM 调用的最大 token",
			ValueType:   "int", Unit: "token", DefaultValue: "4000", Min: strPtr("512"), Max: strPtr("32768"), Step: strPtr("256")},

		{Group: "smart_cs", Key: "confidence_threshold", Name: "智能客服置信度阈值",
			Description: "置信度高于此值才走自动 AI 回复，否则转人工",
			ValueType:   "float", Risk: "warning", DefaultValue: "0.7", Min: strPtr("0.3"), Max: strPtr("0.99"), Step: strPtr("0.05")},
		{Group: "smart_cs", Key: "enable_auto_reply", Name: "开启自动回复",
			Description: "是否允许智能体自动回复（false 时全部转人工）",
			ValueType:   "bool", Risk: "warning", DefaultValue: "true"},
		{Group: "smart_cs", Key: "max_ai_consecutive", Name: "AI 连续回复上限",
			Description: "同一会话内 AI 连续回复超过此次数后强制转人工",
			ValueType:   "int", Unit: "次", DefaultValue: "10", Min: strPtr("2"), Max: strPtr("50"), Step: strPtr("1")},

		// human_task.handoff_first_response_minutes：键名这里是一串字面量、
		// 服务侧读的是 service.HumanTaskConfigGroup / HumanTaskHandoffSlaKey 两个常量，
		// 两边打错任意一处都**不会报错**，只会让运维改参数永远不生效（GetInt 安静回退默认值）。
		// 所以一致性由 TestHumanTaskSeedKeyMatchesServiceConstants 逐字锁住。
		{Group: "human_task", Key: "handoff_first_response_minutes", Name: "转人工首响时限（分钟）",
			Description: "会话转人工后坐席首次响应的时限，超时即计入人工待办逾期读数；1~1440 之外一律回退默认 5 分钟",
			ValueType:   "int", Unit: "分钟", Risk: "warning", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("1440"), Step: strPtr("1")},

		{Group: "session", Key: "active_ttl", Name: "会话活跃 TTL",
			Description: "无任何消息交互超过此时间后会话自动关闭（秒）",
			ValueType:   "duration", DefaultValue: "86400", Min: strPtr("300"), Max: strPtr("2592000"), Step: strPtr("3600")},
		{Group: "session", Key: "idle_ttl", Name: "MCP 会话空闲 TTL",
			Description: "MCP Server 会话无活动超过此时间后过期（秒）",
			ValueType:   "duration", DefaultValue: "1800", Min: strPtr("60"), Max: strPtr("86400"), Step: strPtr("60")},
		{Group: "session", Key: "max_delay_seconds", Name: "营销流程 delay 节点等待上限（秒）",
			Description: "营销流程 delay 节点单次等待时长的上限（秒），超过则截断（不是放弃）。注意这条不是触达排程的「超期放弃」——代码里没有那个概念",
			ValueType:   "duration", DefaultValue: "300", Min: strPtr("30"), Max: strPtr("3600"), Step: strPtr("10")},

		{Group: "cache", Key: "max_keys", Name: "内存缓存最大键数",
			Description: "进程内内存缓存的最大条目数（超过会触发 LRU 淘汰）",
			ValueType:   "int", Risk: "danger", DefaultValue: "10000", Min: strPtr("1000"), Max: strPtr("1000000"), Step: strPtr("1000")},
		{Group: "cache", Key: "faq_answer_enabled", Name: "开启 FAQ 语义答案缓存",
			Description: "默认关。打开后编排器每次入会话先做一次 embedding 并查 rag_answer_cache，命中即直接回缓存答案、跳过本轮生成（代价：多一次向化调用，以及相近问题会复用同一条答案）；装配点在 app/faq_cache_wiring.go。生效范围还受下面两格约束",
			ValueType:   "bool", Risk: "warning", DefaultValue: "false"},
		{Group: "cache", Key: "faq_ttl", Name: "FAQ 答案缓存 TTL",
			Description: "缓存条目最长可用时间（秒）：命中校验时条目已超龄就删掉该行并按未命中处理。只在 cache.faq_answer_enabled 为真时有人读（app/faq_cache_wiring.go）",
			ValueType:   "duration", DefaultValue: "300", Min: strPtr("60"), Max: strPtr("86400"), Step: strPtr("60")},
		{Group: "cache", Key: "faq_decay_max_batch", Name: "FAQ 衰减批量",
			Description: "后台 FAQ 分数衰减任务每轮处理的最大条目",
			ValueType:   "int", Unit: "条", DefaultValue: "1000", Min: strPtr("100"), Max: strPtr("100000"), Step: strPtr("100")},

		{Group: "pagination", Key: "page_max_size", Name: "单页最大条数（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】internal/pkg/utils/pagination.go 的 defaultMaxPageSize=200 只被 newDefaultConfig() 用，而 newDefaultConfig 只被 ParsePagination/ParsePaginationOffset 用——这两个函数全树只有 pagination_test.go 调用，零生产调用方。另注：种子写 100，代码写 200，两者本就不一致",
			ValueType:   "int", Unit: "条", Deprecated: true, DeprecatedReason: "internal/pkg/utils/pagination.go 的 ParsePagination / ParsePaginationOffset 全树只有 pagination_test.go 调用，零生产调用方，所以 defaultMaxPageSize 读不到任何消费点；另注种子曾写 100 而代码写 200，两者本就不一致。ParsePagination 本身作为公共工具保留（它不是债，只是待启用的工具）。要重新提供这两个旋钮，先让至少一个 controller 真的调 ParsePagination，再把它的默认值改成读本参数", DefaultValue: "100", Min: strPtr("10"), Max: strPtr("10000"), Step: strPtr("10")},

		{Group: "pagination", Key: "page_default_size", Name: "默认分页大小（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】internal/pkg/utils/pagination.go 的 defaultDefaultPageSize 有两处消费：一处是上面那条不可达的 newDefaultConfig，另一处是 ParseCursorParams 的 limit 未指定兜底分支——但 operation_log / security_audit / customer 三个调用方都传了正数 defaultLimit，把那条分支遮蔽了",
			ValueType:   "int", Unit: "条", Deprecated: true, DeprecatedReason: "同上：defaultDefaultPageSize 的两处消费一处不可达、另一处被 operation_log / security_audit / customer 三个调用方的正数 defaultLimit 遮蔽。要重新提供这个旋钮，先接上 ParsePagination 的调用方", DefaultValue: "20", Min: strPtr("5"), Max: strPtr("500"), Step: strPtr("5")},

		{Group: "pagination", Key: "cursor_page_size", Name: "游标分页大小",
			Description: "游标分页（Cursor）每批拉取的数量",
			ValueType:   "int", Unit: "条", DefaultValue: "100", Min: strPtr("10"), Max: strPtr("1000"), Step: strPtr("10")},

		{Group: "wecom", Key: "quota_min_rate", Name: "配额最低扣分阈值",
			Description: "企微消息使用率超过此比例（0-1）开始扣健康分（-5 档）。三档（-5 / -15 / -25）分别对应 quota_min_rate / quota_degrade / quota_critical_rate。写小会过早扣分让所有账号进黄区；写大则配额快用满了健康分仍然满分",
			ValueType:   "float", DefaultValue: "0.7", Min: strPtr("0.1"), Max: strPtr("0.99"), Step: strPtr("0.05")},
		{Group: "wecom", Key: "quota_degrade", Name: "配额降级阈值",
			Description: "企微消息使用率超过此比例（0-1）扣健康分（-15 档）并把健康账号标黄。写小会让配额正常波动的账号长期挂黄；写大则真到限流边缘仍显示健康",
			ValueType:   "float", DefaultValue: "0.9", Min: strPtr("0.1"), Max: strPtr("1.0"), Step: strPtr("0.05")},
		{Group: "wecom", Key: "quota_critical_rate", Name: "配额近乎耗尽阈值",
			Description: "企微消息使用率超过此比例（0-1）扣健康分（-25 档）。三档里最高的一档，配低等于几乎到限流才体现。系统会保证它不低于 quota_degrade，避免档位交叉后扣分方向反过来",
			ValueType:   "float", DefaultValue: "0.95", Min: strPtr("0.1"), Max: strPtr("1.0"), Step: strPtr("0.05")},
		{Group: "wecom", Key: "success_rate_critical", Name: "成功率严重不达标阈值（百分数）",
			Description: "账号发送成功率低于此值扣健康分（-30 档），是成功率三档里最严的一档。注意单位是百分数不是 0~1 比例（与 SuccessRate 字段同口径）：填 0.8 会被当成 0.8% 成功率，等于把账号直接打到最严档。配高等于允许大量丢消息仍算健康",
			ValueType:   "float", Unit: "%", Risk: "warning", DefaultValue: "50", Min: strPtr("0"), Max: strPtr("100"), Step: strPtr("1")},
		{Group: "wecom", Key: "success_rate_degrade", Name: "成功率降级阈值（百分数）",
			Description: "账号发送成功率低于此值扣健康分（-15 档）。单位是百分数。系统会保证它严格低于 success_rate_warn 且不高于 success_rate_critical，避免档位乱填后「成功率更低的反而扣得更少」",
			ValueType:   "float", Unit: "%", Risk: "warning", DefaultValue: "80", Min: strPtr("0"), Max: strPtr("100"), Step: strPtr("1")},
		{Group: "wecom", Key: "success_rate_warn", Name: "成功率轻微不达标阈值（百分数）",
			Description: "账号发送成功率低于此值扣健康分（-5 档），是三档里最宽松的一档；成功率恰好等于此值不扣分（刚好算达标）。单位是百分数。配低等于稍微有失败就扣分",
			ValueType:   "float", Unit: "%", Risk: "warning", DefaultValue: "95", Min: strPtr("0"), Max: strPtr("100"), Step: strPtr("1")},

		{Group: "telemetry", Key: "node_health_window", Name: "节点健康检查窗口",
			Description: "节点健康心跳统计窗口（秒）。0 会让聚合恒为空列表，界面像是所有渠道都没心跳。",
			ValueType:   "duration", DefaultValue: "86400", Min: strPtr("3600"), Max: strPtr("604800"), Step: strPtr("3600")},
		{Group: "telemetry", Key: "trace_sink_buffer", Name: "Trace Sink 缓冲",
			Description: "追踪采集缓冲队列容量（超过丢弃低优先级事件）",
			ValueType:   "int", Unit: "条", DefaultValue: "2048", Min: strPtr("256"), Max: strPtr("524288"), Step: strPtr("256")},
		// D6（specs/007 §3.6）：看板窗口默认值与渠道在线率降级阈值接参数中心。
		// 读点：internal/service/observability_dashboard.go 的 defaultWindow / onlineDegradedBelow
		// （每次快照热读；兜底常量 DashboardDefaultWindowHours / ChannelOnlineDegradedBelow 与 DefaultValue 一致）。
		{Group: "telemetry", Key: "dashboard_default_window_hours",
			Name:        "看板默认统计窗口（小时）",
			Description: "运营看板在调用方没给 window（或给了非法值）时用的统计窗口，单位小时。写大＝样本更多、曲线更平滑，但「刚出问题」会被历史稀释；写小＝灵敏，代价是样本少时读数抖动。必须能被 24 整除才显示成「N 天」形态，否则显示成「N 小时」；0 或 >8760 一律回落兜底 168（7 天）——填 0 的直觉是「只看此刻」，而那样的看板会把空读数显示成绿色",
			ValueType:   "int", Unit: "小时", DefaultValue: "168", Min: strPtr("1"), Max: strPtr("8760"), Step: strPtr("1"), Restart: false},
		{Group: "telemetry", Key: "channel_online_degraded_below",
			Name:        "渠道在线率降级阈值",
			Description: "渠道在线账号占比低于该百分比即判 degraded（0–100）。在线率不是二元量：3 个账号掉 1 个就是 66.7%，对这个租户来说已经「有一条消息发不出去」。写 100＝偶发重连就飘红；写低＝只有大面积掉线才看得见，中间那一段会与「一切正常」同色。全离线另有 offline 档，不受此阈值影响。非正数或 >100 一律回落兜底 90",
			ValueType:   "int", Unit: "%", DefaultValue: "90", Min: strPtr("1"), Max: strPtr("100"), Step: strPtr("1"), Restart: false},
		{Group: "telemetry", Key: "geo_position_window", Name: "品牌词命中去重窗口",
			Description: "Geo 品牌词提及量统计时，相邻两处命中的字符间隔小于该值即算同一处提及（如「Apple 发布会」只计一次）。单位是字符偏移，不是条数。",
			ValueType:   "int", Unit: "字符", DefaultValue: "20", Min: strPtr("5"), Max: strPtr("100"), Step: strPtr("1")},
		{Group: "telemetry", Key: "feature_flag_poll_interval", Name: "Feature Flag 轮询间隔",
			Description: "pkg/featureflag 从环境变量热加载开关的轮询周期（秒）。改小会让每次轮询都读一遍环境变量；配 0 会被回落成代码兜底值，因为 time.NewTicker(0) 直接 panic。",
			ValueType:   "duration", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("300"), Step: strPtr("1")},

		{Group: "sales", Key: "insight_limit", Name: "销售洞察条数（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】原指向 trace_learning.TopInsights 的 limit，而那个函数没有任何生产调用方（学习洞察目前只写不读），已随之删除；要恢复这个旋钮，先有生产调用方按行业读洞察",
			ValueType:   "int", Unit: "条", Deprecated: true, DeprecatedReason: "对应的读取函数 trace_learning.TopInsights 全仓无生产调用方，已删除：学习洞察写入后没有任何地方回读。给一个不存在的读取路径配默认值本身就是错的抽象。要重新提供这个旋钮，先让某个生产链路把行业与 limit 传进一个真实的读口", DefaultValue: "3", Min: strPtr("1"), Max: strPtr("20"), Step: strPtr("1")},

		{Group: "sales", Key: "identity_max_attempts", Name: "客户身份合并重试上限",
			Description: "CustomerIdentity 身份合并的最大重试次数",
			ValueType:   "int", Unit: "次", DefaultValue: "8", Min: strPtr("1"), Max: strPtr("50"), Step: strPtr("1")},
		{Group: "sales", Key: "audience_default_limit", Name: "圈选默认人数",
			Description: "运营在 SOP 圈选条件里没写 limit 时，一轮圈多少客户。调度器每人一条 SOPExecution，没有这一道默认值，未填条件的轮次会拿到空名单，而空名单在 Reasons 上表现为 no_match，看起来像「条件太严」而不是默认值被改坏",
			ValueType:   "int", Unit: "人", DefaultValue: "200", Min: strPtr("1"), Max: strPtr("1000"), Step: strPtr("1")},
		{Group: "sales", Key: "audience_max_limit", Name: "圈选硬上限",
			Description: "圈选人数的硬上限，条件里写得再大也夹到这里。这是并发闸：超了会把一轮变成上千条并发 SOPExecution。一般不应比 audience_default_limit 大太多，否则「夹上限」会成为常态而不是异常信号",
			ValueType:   "int", Unit: "人", DefaultValue: "500", Min: strPtr("1"), Max: strPtr("10000"), Step: strPtr("1")},

		{Group: "misc", Key: "upload_max_size_mb", Name: "上传文件大小上限",
			Description: "单次上传的文件大小上限（MB）。0 表示不覆盖、沿用下游：优先级为 env UPLOAD_MAX_SIZE > 本参数 > 「系统配置」页的兼容列 > 代码兜底 10MB。默认 0 是为了让存量站点保持现状——填 10 会把运维已在系统配置页调好的 200MB 又压回 10MB。覆盖面边界：本参数只被 `internal/controller` 的上传链路读取。素材库那条是另一个切片" +
				"（`internal/content/controller/material.go`），它读 frontend_ui.material_max_size_mb 且前后端同源，改本参数**不影响**它——" +
				"当初没把素材口接进本链，是因为这条链的兼容列默认 50MB，接过去等于给一个一直按 10MB 拦的端点静默放宽 5 倍",
			ValueType: "int", Unit: "MB", Risk: "danger", DefaultValue: "0", Min: strPtr("0"), Max: strPtr("512"), Step: strPtr("1")},
		{Group: "misc", Key: "visitor_token_ttl", Name: "访客 Token TTL",
			Description: "未登录访客的身份 Token 有效期（秒）",
			ValueType:   "duration", DefaultValue: "604800", Min: strPtr("3600"), Max: strPtr("2592000"), Step: strPtr("3600")},
		{Group: "misc", Key: "sso_cookie_ttl", Name: "SSO Cookie TTL",
			Description: "SSO 单点登录 cookie 的有效期（秒）",
			ValueType:   "duration", DefaultValue: "300", Min: strPtr("60"), Max: strPtr("86400"), Step: strPtr("60")},
		{Group: "misc", Key: "heartbeat_interval", Name: "平台心跳间隔",
			Description: "向平台发送心跳的周期（秒）",
			ValueType:   "duration", DefaultValue: "180", Min: strPtr("30"), Max: strPtr("3600"), Step: strPtr("10")},
		{Group: "misc", Key: "polling_lock_stale_threshold", Name: "轮询锁过期阈值",
			Description: "Telegram 等渠道 polling lock 超过此时间未更新视为失效（秒）",
			ValueType:   "duration", DefaultValue: "60", Min: strPtr("10"), Max: strPtr("600"), Step: strPtr("5")},
		{Group: "misc", Key: "domain_check_concurrency", Name: "域名检测并发",
			Description: "域名可用性检查的并发 goroutine 数",
			ValueType:   "int", DefaultValue: "16", Min: strPtr("1"), Max: strPtr("128"), Step: strPtr("1")},
		{Group: "misc", Key: "ab_exposure_buffer", Name: "AB 曝光缓冲",
			Description: "AB 实验曝光事件的批量缓冲大小",
			ValueType:   "int", Unit: "条", DefaultValue: "1024", Min: strPtr("64"), Max: strPtr("65536"), Step: strPtr("64")},
		{Group: "misc", Key: "sms_max_retry", Name: "短信最大重试次数",
			Description: "短信发送失败后的重试次数",
			ValueType:   "int", Unit: "次", DefaultValue: "3", Min: strPtr("0"), Max: strPtr("10"), Step: strPtr("1")},
		{Group: "misc", Key: "edit_lock_ttl", Name: "会话编辑锁 TTL",
			Description: "会话编辑锁（协作碰撞检测）的存活秒数。它同时是持锁人异常退出后的自愈时间——锁表没有后台清理线程，全靠 ExpiresAt 过期。调小让碰撞检测更灵敏但坐席更容易被锁在门外，调大相反；非正数会被回落成代码兜底 300s",
			ValueType:   "duration", Risk: "danger", DefaultValue: "300", Min: strPtr("30"), Max: strPtr("3600"), Step: strPtr("30")},
		{Group: "misc", Key: "csv_export_max_rows", Name: "CSV 导出最大行数",
			Description: "自定义报表 CSV 导出的单行上限（防止 OOM）",
			ValueType:   "int", Unit: "行", DefaultValue: "30000", Min: strPtr("1000"), Max: strPtr("500000"), Step: strPtr("1000")},
		{Group: "misc", Key: "batch_import_max_file_mb", Name: "批量导入文件大小上限",
			Description: "批量导入口（admin POST /api/batch/import，线索/客户 CSV）的单个文件大小上限（MB）。" +
				"这条**只有后端读**（internal/content/controller/batch_operation.go 的 batchImportMaxUploadSize）：" +
				"导入页面用的是裸 el-upload，既没有本地拦截也没有尺寸提示，所以不存在「前端放过、后端拦死」的错位，" +
				"也就不必把值下发给前端多养一份会漂移的副本。" +
				"调大要连带看 middleware.max_json_body_mb 之外的那道 multipart 缓冲——超出 8MB 缓冲的部分 gin 落临时文件，" +
				"上限本身不改变内存曲线，只改变一次请求能塞进磁盘/解析器的体积。默认 50 = 接线前写死的那个数",
			ValueType: "int", Unit: "MB", Risk: "danger", DefaultValue: "50", Min: strPtr("1"), Max: strPtr("512"), Step: strPtr("1")},
		{Group: "misc", Key: "backup_page_size", Name: "备份列表单页上限",
			Description: "备份列表单页最多返回多少条（超出按此截断）。注意这是上限不是默认分页大小：默认每页仍是 10 条，由 controller/backup.go 的 defaultBackupPageSize 决定",
			ValueType:   "int", Unit: "条", DefaultValue: "1000", Min: strPtr("100"), Max: strPtr("100000"), Step: strPtr("100")},
		{Group: "misc", Key: "text_truncate_max_bytes", Name: "文本截断上限",
			Description: "文本工具通用截断的最大字节数",
			ValueType:   "int", Unit: "字节", DefaultValue: "8192", Min: strPtr("512"), Max: strPtr("1048576"), Step: strPtr("256")},
		{Group: "misc", Key: "preview_max_len", Name: "消息预览最大长度",
			Description: "消息列表 preview 字段的最大字符长度",
			ValueType:   "int", Unit: "字符", DefaultValue: "500", Min: strPtr("50"), Max: strPtr("5000"), Step: strPtr("50")},
		{Group: "misc", Key: "sop_scheduler_interval", Name: "SOP Scheduler 扫描间隔",
			Description: "SOP 调度器扫描待触发任务的周期（秒）",
			ValueType:   "duration", DefaultValue: "60", Min: strPtr("5"), Max: strPtr("600"), Step: strPtr("5")},
		{Group: "misc", Key: "sop_max_wait", Name: "SOP Timer 最大等待",
			Description: "SOP 定时器的最大等待时间（超过视为异常）",
			ValueType:   "duration", DefaultValue: "86400", Min: strPtr("3600"), Max: strPtr("2592000"), Step: strPtr("3600")},

		{Group: "channelgw", Key: "ws_register_timeout", Name: "WS 注册超时",
			Description: "Channel Gateway WebSocket 注册握手超时（秒）",
			ValueType:   "duration", DefaultValue: "15", Min: strPtr("5"), Max: strPtr("60"), Step: strPtr("1")},
		{Group: "channelgw", Key: "ws_read_idle_timeout", Name: "WS 读空闲超时",
			Description: "WebSocket 读取空闲超时（秒），超过触发重连",
			ValueType:   "duration", DefaultValue: "90", Min: strPtr("10"), Max: strPtr("600"), Step: strPtr("5")},
		{Group: "channelgw", Key: "ws_write_timeout", Name: "WS 写超时",
			Description: "WebSocket 单次写超时（秒）",
			ValueType:   "duration", DefaultValue: "10", Min: strPtr("1"), Max: strPtr("60"), Step: strPtr("1")},
		{Group: "channelgw", Key: "ws_push_interval", Name: "WS 推送间隔",
			Description: "心跳空闲时 WS 推送消息的最小间隔（秒）",
			ValueType:   "duration", DefaultValue: "2", Min: strPtr("1"), Max: strPtr("10"), Step: strPtr("1")},
		{Group: "channelgw", Key: "ws_pipeline_timeout", Name: "WS Pipeline 超时",
			Description: "WS pipeline 单次批量处理超时（秒）",
			ValueType:   "duration", DefaultValue: "30", Min: strPtr("5"), Max: strPtr("300"), Step: strPtr("5")},

		{Group: "agent_tool", Key: "max_concurrent", Name: "工具最大并发",
			Description: "Agent 工具执行器同时运行的最大并发数",
			ValueType:   "int", Risk: "danger", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("50"), Step: strPtr("1")},
		{Group: "agent_tool", Key: "max_content_len", Name: "工具输出截断上限",
			Description: "工具返回内容超过此长度时截断",
			ValueType:   "int", Unit: "字符", DefaultValue: "4000", Min: strPtr("512"), Max: strPtr("65536"), Step: strPtr("256")},
		{Group: "agent_tool", Key: "result_cache_ttl", Name: "工具结果缓存 TTL（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】ResultCache 的实现在 tooluse/result_cache.go，但 NewResultCache 全仓只有测试调用，生产链路从未构造过它——没有构造点就接不上 TTL。要启用需先在 executor 装饰链上挂 ResultCache，再把本条接进去",
			ValueType:   "duration", Deprecated: true, DeprecatedReason: "ResultCache 的实现在 internal/aiagent/agent/tooluse/result_cache.go，但 NewResultCache 全仓只有 p2_test.go 调用，生产链路从未构造过它——没有构造点就接不上 TTL。要重新提供这个旋钮，先在 executor 装饰链上挂 ResultCache，再把 TTL 接到本条", DefaultValue: "60", Min: strPtr("10"), Max: strPtr("3600"), Step: strPtr("5")},

		{Group: "agent_tool", Key: "cooldown_duration", Name: "工具冷却时间",
			Description: "工具失败后自动冷却时间（秒）",
			ValueType:   "duration", DefaultValue: "30", Min: strPtr("5"), Max: strPtr("300"), Step: strPtr("5")},
		{Group: "agent_tool", Key: "fail_threshold", Name: "工具熔断阈值",
			Description: "连续失败多少次后触发熔断",
			ValueType:   "int", Unit: "次", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("50"), Step: strPtr("1")},

		// 下面五条是 **executor 装饰链上按工具熔断**（tooluse.CircuitBreakerRegistry）的参数，
		// 与同组 `fail_threshold`（ToolRouter 内部「失败即换同类工具」的短路）不是同一个熔断：
		// 两者独立计量、互不共享状态，改错对象不会动到想改的那条。
		// 五条的 DefaultValue 与 tooluse.DefaultCircuitBreakerConfig() 逐项相等，
		// 所以「接上但没人改过」必须是无行为变更的——这条不变量由
		// internal/app/tool_circuit_breaker_wiring_test.go 断言。
		{Group: "agent_tool", Key: "circuit_failure_threshold", Name: "工具执行链熔断阈值",
			Description: "executor 装饰链按工具熔断的连续失败次数阈值：同一工具连败到此数即转 open 态、进入退避冷却。写大＝坏工具持续被调用、把上游 LLM 的上下文塞满失败噪声；写小＝一次瞬时抖动就把工具摘掉一段时间。仅在环境变量 FF_TOOL_CIRCUIT_BREAKER=shadow|enforce 时才有意义，默认 off 时熔断器根本不构造，本条改了不生效。env TOOL_CIRCUIT_FAILURE_THRESHOLD 优先级高于本条。装配期读一次，改完需重启才生效；装配点 app/tool_circuit_breaker_wiring.go",
			ValueType:   "int", Unit: "次", Risk: "danger", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("1000"), Step: strPtr("1"), Restart: true},
		{Group: "agent_tool", Key: "circuit_base_cooldown", Name: "工具执行链熔断基础冷却",
			Description: "第一次转 open 后的冷却时长（秒），后续按 circuit_backoff_multiplier 指数放大。写大＝坏工具被摘过久，上游 LLM 看到的是「工具不可用」；写小＝刚冷却完就被重试，等于没有熔断。与 circuit_max_cooldown 同时看：本值大于上限时上限会被抬到本值（否则指数退避被反向夹住）。仅在 FF_TOOL_CIRCUIT_BREAKER=shadow|enforce 时才有意义；env TOOL_CIRCUIT_BASE_COOLDOWN 优先。装配期读一次，改完需重启才生效",
			ValueType:   "duration", DefaultValue: "30", Min: strPtr("1"), Max: strPtr("3600"), Step: strPtr("5"), Restart: true},
		{Group: "agent_tool", Key: "circuit_max_cooldown", Name: "工具执行链熔断冷却上限",
			Description: "指数退避的封顶（秒）：第 n 次 open 的冷却 = base × multiplier^(n-1)，超过本值按本值取。写大＝一个长期坏掉的工具可以几分钟不被碰；写小＝退避失去意义，坏工具被反复重试。低于 circuit_base_cooldown 时会被抬到 base 并告警。仅在 FF_TOOL_CIRCUIT_BREAKER=shadow|enforce 时才有意义；env TOOL_CIRCUIT_MAX_COOLDOWN 优先。装配期读一次，改完需重启才生效",
			ValueType:   "duration", DefaultValue: "300", Min: strPtr("5"), Max: strPtr("86400"), Step: strPtr("5"), Restart: true},
		{Group: "agent_tool", Key: "circuit_backoff_multiplier", Name: "工具执行链熔断退避倍数",
			Description: "每多一次 open，冷却时长的放大倍数（1 = 不退避，固定冷却）。写大＝第三次失败起基本不会再碰这个工具；写小＝退避曲线拉平，等同于只配一个固定冷却。小于 1 会被拒并回落代码兜底 2.0（退避反而变短是配错，不是「更保守」）。仅在 FF_TOOL_CIRCUIT_BREAKER=shadow|enforce 时才有意义；env TOOL_CIRCUIT_BACKOFF_MULTIPLIER 优先。装配期读一次，改完需重启才生效",
			ValueType:   "float", Unit: "倍", DefaultValue: "2.0", Min: strPtr("1"), Max: strPtr("100"), Step: strPtr("0.5"), Restart: true},
		{Group: "agent_tool", Key: "circuit_half_open_max_attempts", Name: "工具执行链熔断半开配额",
			Description: "半开态允许放行的试探请求数：冷却到期后转 half_open，放这么多请求出去，全成功即回 closed，出现失败立刻回 open 并重新计时。写大＝坏工具在恢复期被打满（一次放行一批）；写小＝恢复慢，好工具也要多等一个冷却周期。仅在 FF_TOOL_CIRCUIT_BREAKER=shadow|enforce 时才有意义；env TOOL_CIRCUIT_HALF_OPEN_ATTEMPTS 优先。装配期读一次，改完需重启才生效",
			ValueType:   "int", Unit: "次", DefaultValue: "1", Min: strPtr("1"), Max: strPtr("100"), Step: strPtr("1"), Restart: true},

		{Group: "middleware", Key: "audit_flush_interval", Name: "审计刷盘间隔",
			Description: "审计中间件批量刷盘到 DB 的时间间隔（秒）",
			ValueType:   "duration", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("300"), Step: strPtr("1")},
		{Group: "middleware", Key: "mfa_recent_verify_ttl", Name: "MFA 近验有效期",
			Description: "MFA 二次验证后的宽限期（秒）",
			ValueType:   "duration", DefaultValue: "300", Min: strPtr("60"), Max: strPtr("3600"), Step: strPtr("60")},
		{Group: "middleware", Key: "max_json_body_mb", Name: "全局请求体上限",
			Description: "JSON / 表单请求体的全局封顶（MB）。必须高于各端点既有上界（webhook 可调顶格 4MB），调小会把已放行的高段请求截成半份；env MAX_JSON_BODY_MB 仍可覆盖。临时放开请用 env 设 0（不限制），本参数最小为 1。改动需重启",
			ValueType:   "int", Unit: "MB", Risk: "danger", DefaultValue: "8", Min: strPtr("1"), Max: strPtr("100"), Step: strPtr("1"), Restart: true},

		{Group: "confidence", Key: "humanize_default_threshold", Name: "人性化默认阈值",
			Description: "文本人性化判定阈值（0-1），落库到 humanize_scores.threshold；装配点 app/confidence_params_wiring.go",
			ValueType:   "float", DefaultValue: "0.85", Min: strPtr("0.1"), Max: strPtr("1.0"), Step: strPtr("0.05")},
		{Group: "confidence", Key: "humanize_boundary_low", Name: "人性化边界样本下界",
			Description: "拟人度判定的边界样本区间下界（含，0-1）。低于它的样本判「明显达标」、不送 LLM；落在 [low,high) 的算边缘样本，再按 humanize_sample_rate 抽样送 LLM。调大到接近阈值会让边缘样本消失（LLM 复核形同关闭），调小会让几乎所有样本都变成边缘样本（费用与延迟同时失控）；装配点 app/p1_params_wiring.go",
			ValueType:   "float", DefaultValue: "0.70", Min: strPtr("0"), Max: strPtr("1.0"), Step: strPtr("0.05")},
		{Group: "confidence", Key: "humanize_boundary_high", Name: "人性化边界样本上界",
			Description: "拟人度判定的边界样本区间上界（不含，0-1）。调小到低于 humanize_boundary_low 时区间为空，等于关掉 LLM 复核；调大超过 1 无效。它与 humanize_default_threshold / boundary_low 是同一套判据的三个边界，改动前请把三者一起看",
			ValueType:   "float", DefaultValue: "0.85", Min: strPtr("0.05"), Max: strPtr("1.0"), Step: strPtr("0.05")},
		{Group: "confidence", Key: "humanize_sample_rate", Name: "人性化 LLM 采样率",
			Description: "边缘样本里送 LLM 复核的比例（0-1）。0 是合法值，语义是「一个都不送、纯规则打分」；1 是全部送。调大直接等比放大 LLM 调用量与响应延迟；装配点 app/p1_params_wiring.go",
			ValueType:   "float", DefaultValue: "0.10", Min: strPtr("0"), Max: strPtr("1.0"), Step: strPtr("0.05")},
		{Group: "confidence", Key: "humanize_max_retry", Name: "人性化最大重生成次数",
			Description: "判定不达标后的最大重写轮数。填 1 以下会被回落成代码兜底 3：调度侧按次数预算 LLM 调用，静默变 0 会让预算模型与真实行为脱节。显式调用 WithMaxRetry 仍优先于本参数",
			ValueType:   "int", Unit: "次", DefaultValue: "3", Min: strPtr("1"), Max: strPtr("10"), Step: strPtr("1")},
		{Group: "confidence", Key: "intent_fewshot_min_cos", Name: "意图 Few-Shot 最小余弦",
			Description: "意图识别 Few-Shot 检索最小余弦相似度（0-1），低于它的样本不进 prompt；装配点 app/confidence_params_wiring.go",
			ValueType:   "float", DefaultValue: "0.7", Min: strPtr("0.1"), Max: strPtr("1.0"), Step: strPtr("0.05")},
		{Group: "confidence", Key: "weak_truth_min_confidence", Name: "弱真值最小置信度",
			Description: "意图指标弱真值判定的最小置信度（0-1），低于它的预测进低置信桶、不入混淆矩阵；装配点 app/confidence_params_wiring.go",
			ValueType:   "float", Risk: "warning", DefaultValue: "0.9", Min: strPtr("0.1"), Max: strPtr("1.0"), Step: strPtr("0.05")},
		{Group: "confidence", Key: "emb_retry_cooldown", Name: "Embedding 重试冷却",
			Description: "Embedding 锚点预计算失败后的重试冷却间隔（秒），冷却期内不重复触发后台重算；装配点 app/confidence_params_wiring.go",
			ValueType:   "duration", DefaultValue: "60", Min: strPtr("5"), Max: strPtr("300"), Step: strPtr("5")},
		{Group: "confidence", Key: "veto_low_rag_threshold", Name: "否决低 RAG 阈值",
			Description: "一票否决规则的最低 RAG 得分阈值（0-1），RAGQual 低于它判定知识库无覆盖、必须转人工或兜底；装配点 app/confidence_params_wiring.go",
			ValueType:   "float", Risk: "warning", DefaultValue: "0.1", Min: strPtr("0"), Max: strPtr("0.5"), Step: strPtr("0.01")},

		{Group: "workflow", Key: "max_subflow_depth", Name: "子流程最大深度",
			Description: "子流程嵌套最大层级，防止无限递归",
			ValueType:   "int", Unit: "层", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("30"), Step: strPtr("1")},
		{Group: "workflow", Key: "max_workflow_steps", Name: "工作流最大步数",
			Description: "单个工作流最多可执行的节点数",
			ValueType:   "int", Unit: "步", DefaultValue: "1000", Min: strPtr("10"), Max: strPtr("100000"), Step: strPtr("10")},
		{Group: "workflow", Key: "max_running_per_sop", Name: "单 SOP 最大并发",
			Description: "同一个 SOP 模板同时运行的最大实例数",
			ValueType:   "int", DefaultValue: "50", Min: strPtr("1"), Max: strPtr("500"), Step: strPtr("1")},
		{Group: "workflow", Key: "max_executed_node_trace", Name: "执行轨迹最大节点",
			Description: "SOP 执行历史记录最多保留的节点数",
			ValueType:   "int", Unit: "个", DefaultValue: "200", Min: strPtr("10"), Max: strPtr("10000"), Step: strPtr("10")},

		{Group: "session", Key: "inbox_overdue_threshold", Name: "收件箱超时阈值",
			Description: "收件箱会话超过此时间无客服响应标记为 overdue（秒）",
			ValueType:   "duration", DefaultValue: "1800", Min: strPtr("300"), Max: strPtr("172800"), Step: strPtr("60")},
		{Group: "session", Key: "inbox_default_staff_load_limit", Name: "客服默认负载上限",
			Description: "收件箱分配给单个客服的默认会话负载上限",
			ValueType:   "int", Risk: "danger", DefaultValue: "30", Min: strPtr("1"), Max: strPtr("200"), Step: strPtr("5")},

		{Group: "cache", Key: "memo_ttl", Name: "安装 memo TTL",
			Description: "系统安装信息缓存的有效期（秒）",
			ValueType:   "duration", DefaultValue: "2", Min: strPtr("1"), Max: strPtr("300"), Step: strPtr("1")},
		{Group: "cache", Key: "platform_cache_ttl", Name: "平台缓存 TTL",
			Description: "平台信息 API 结果缓存有效期（秒）",
			ValueType:   "duration", DefaultValue: "20", Min: strPtr("1"), Max: strPtr("3600"), Step: strPtr("1")},
		{Group: "cache", Key: "translation_cache_max_entries", Name: "翻译缓存最大条目",
			Description: "翻译结果内存缓存的最大条目数",
			ValueType:   "int", Unit: "条", DefaultValue: "100000", Min: strPtr("1000"), Max: strPtr("10000000"), Step: strPtr("1000")},

		{Group: "agent_llm", Key: "vote_agreement_threshold", Name: "投票一致阈值",
			Description: "多 LLM 投票一致性判断阈值（0-1）",
			ValueType:   "float", DefaultValue: "0.80", Min: strPtr("0.5"), Max: strPtr("1.0"), Step: strPtr("0.05")},
		{Group: "agent_llm", Key: "default_health_check_interval", Name: "健康检查间隔",
			Description: "LLM Provider 熔断器健康检查周期（秒）",
			ValueType:   "duration", DefaultValue: "30", Min: strPtr("5"), Max: strPtr("600"), Step: strPtr("5")},
		{Group: "agent_llm", Key: "default_circuit_open_duration", Name: "熔断器熔断时长",
			Description: "LLM Provider 熔断后保持 open 状态的时长（秒）",
			ValueType:   "duration", DefaultValue: "60", Min: strPtr("10"), Max: strPtr("3600"), Step: strPtr("10")},
		{Group: "agent_llm", Key: "default_health_check_timeout", Name: "健康检查超时",
			Description: "LLM Provider 健康检查请求的超时（秒）",
			ValueType:   "duration", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("60"), Step: strPtr("1")},
		{Group: "agent_llm", Key: "default_failure_threshold", Name: "熔断失败阈值",
			Description: "LLM Provider 连续失败多少次后触发熔断",
			ValueType:   "int", Unit: "次", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("50"), Step: strPtr("1")},
		{Group: "agent_llm", Key: "default_http_timeout", Name: "默认 HTTP 超时",
			Description: "LLM 客户端 HTTP 请求默认超时（秒）",
			ValueType:   "duration", DefaultValue: "180", Min: strPtr("5"), Max: strPtr("600"), Step: strPtr("5")},
		{Group: "agent_llm", Key: "db_sink_stop_deadline", Name: "DB Sink 停止截止",
			Description: "Trace DB Sink 停止时等待的最大时间（秒）",
			ValueType:   "duration", DefaultValue: "3", Min: strPtr("1"), Max: strPtr("60"), Step: strPtr("1")},
		{Group: "agent_llm", Key: "provider_failover_policy", Name: "LLM 降级策略（整份 JSON）",
			Description: "按部署覆盖 ProviderFailover 的整份策略：config 里 >0/非空的字段逐项覆盖上面四条 default_* 旋钮；scenarios 是整表替换，少写一个场景不会报错，那个意图改由调度路由给候选。读路径在 internal/aiagent/llm/provider_failover.go 的 LoadPolicy，每 tick 走参数中心 60s 缓存；JSON 非法时整份回落内置默认并打 Warn",
			ValueType:   "json", Risk: "danger", DefaultValue: `{"config":{"health_check_interval":30,"failure_threshold":5,"circuit_open_duration":60,"degraded_latency_ms":3000,"local_fallback_provider":"default","template_reply":"抱歉，当前服务暂时繁忙，请稍后再试或联系人工客服。","health_check_path":"/health"},"scenarios":{"intent_recognize":["default","deepseek","qwen"],"sop_reply":["default","gpt-4o","glm-4"],"objection":["default","gpt-4o","glm-4"],"friendly_chat":["default","deepseek"],"long_summary":["default","kimi","qwen"],"high_quality":["default","gpt-4o","glm-4"],"low_cost":["default","deepseek"]}}`,
			Category: "LLM"},
		{Group: "agent_llm", Key: "default_semantic_threshold", Name: "语义缓存阈值",
			Description: "FAQ 语义缓存 Tier2 命中相似度阈值（0-1），只在 cache.faq_answer_enabled 为真时有人读。契约只允许调紧：小于 0.95 的值会被构造器抬回 0.95，因此填 0.8 与填 0.95 效果相同",
			ValueType:   "float", DefaultValue: "0.95", Min: strPtr("0.5"), Max: strPtr("1.0"), Step: strPtr("0.05")},

		{Group: "embedding", Key: "global_override", Name: "全局 Embedding 提供商覆盖（整份 JSON）",
			Description: "整份 JSON：{enabled,base_url,api_key,model}。enabled 为真且 base_url 非空时，EmbeddingService.DefaultConfig 用它顶掉 config.yaml/env 那一层，指向云端 OpenAI 兼容端点；关掉只需把 enabled 写成 false。维度硬约束 1024（pgvector 兼容），非 1024 维模型在调用侧强制回落并告警。api_key 落库前已按 llm_providers 同一套加密，页面上看到的是密文。读路径在 internal/aiagent/llm/embedding_global_config.go，写路径由管理端 PUT /api/llm/embedding-config 走本条（带操作者审计）",
			ValueType:   "json", Risk: "danger", DefaultValue: `{"enabled":false,"base_url":"","api_key":"","model":""}`,
			Category: "LLM"},

		{Group: "knowledge", Key: "max_upload_file_size", Name: "知识库上传大小上限",
			Description: "知识库文档上传的最大文件大小（字节）。前端 KnowledgeManagement.vue 的本地拦截与提示文案经 " +
				"GET /api/config/runtime 读同一条（跨组放行），所以改这里会同时改前后端，不再出现「运维调到 100MB、页面照样拦 50MB」",
			ValueType: "int", Unit: "字节", Risk: "danger", DefaultValue: "52428800", Min: strPtr("1048576"), Max: strPtr("524288000"), Step: strPtr("1048576")},
		{Group: "knowledge", Key: "merchant_batch_max_file_size", Name: "商户知识批量导入文件大小上限",
			Description: "商户知识库批量导入（/api/knowledge-merchant/batch/upload）单个文件的字节上限，整个文件会一次读进内存解析。" +
				"与 max_upload_file_size 不是同一个概念，所以不并成一条：那条管整篇文档上传（默认 50MB），这条管结构化行数据导入（默认 20MB，正是前端 BatchImport.vue 一直在标的数）。" +
				"接线前这条链路服务端零校验、上限只写在页面上（绕过页面直接调接口就能喂任意大的文件）；现在前后端读同一条（前端经 GET /api/config/runtime 的跨组放行拿到）。" +
				"改大它会真的把更大的文件读进内存，改小它同时收紧前后端",
			ValueType: "int", Unit: "字节", Risk: "danger", DefaultValue: "20971520", Min: strPtr("1048576"), Max: strPtr("209715200"), Step: strPtr("1048576")},
		{Group: "knowledge", Key: "merchant_knowledge_max_len", Name: "商户知识库内容上限",
			Description: "外部知识库导入单条内容的最大字符数",
			ValueType:   "int", Unit: "字符", DefaultValue: "2000", Min: strPtr("100"), Max: strPtr("10000"), Step: strPtr("100")},

		// D-7（2026-10-23）：chat_ws_ping_period / chat_ws_pong_wait / chat_ws_write_wait
		// 三条已下架种子随死代码一起移除——承载它们的 ChatWS 控制器与 /ws/chat 路由
		// 从未接线，整体删除（决策：两周内无 /ws/chat 产品计划 ⇒ 删除而非接线）。
		// 将来若上真坐席 WS，需先补 JWT 鉴权 + Origin 白名单 + 租户绑定
		//（参照 /api/ws/visitor 的五层防护），再把旋钮登记回来。

		{Group: "inbox_sales", Key: "tg_lead_opportunity_threshold", Name: "Telegram 线索机会阈值",
			Description: "Telegram 线索识别判定为有机会的最低分数",
			ValueType:   "int", Unit: "分", DefaultValue: "40", Min: strPtr("5"), Max: strPtr("200"), Step: strPtr("1")},
		{Group: "lead", Key: "llm_refine_enabled", Name: "线索LLM复核开关",
			Description: "开启后线索/商机判定走 LLM 行业无关判别，关键词打分仅作兜底",
			ValueType:   "bool", Risk: "warning", DefaultValue: "true", Category: "线索挖掘"},
		{Group: "lead", Key: "industry_profile", Name: "行业画像",
			Description: "一段自由文本描述本站业务，LLM 线索判别按此理解行业语义",
			ValueType:   "string", DefaultValue: "通用零售/服务咨询：客户咨询产品或服务的价格、功能与购买流程", Category: "线索挖掘"},
		{Group: "inbox_sales", Key: "unified_miner_lead_threshold", Name: "统一挖掘线索阈值",
			Description: "统一线索挖掘判定为有效线索的最低分数",
			ValueType:   "int", Unit: "分", DefaultValue: "40", Min: strPtr("5"), Max: strPtr("200"), Step: strPtr("1")},
		{Group: "inbox_sales", Key: "preview_sample_limit", Name: "合并预览采样上限",
			Description: "OneID 合并预览的最大采样数",
			ValueType:   "int", Unit: "条", DefaultValue: "20", Min: strPtr("1"), Max: strPtr("100"), Step: strPtr("1")},
		{Group: "inbox_sales", Key: "geo_lead_preview_max_len", Name: "Geo 线索洞察上限",
			Description: "Trace Learning 洞察返回的最大字符数",
			ValueType:   "int", Unit: "字符", DefaultValue: "200", Min: strPtr("10"), Max: strPtr("5000"), Step: strPtr("50")},

		{Group: "misc", Key: "deepl_timeout", Name: "DeepL 翻译超时",
			Description: "DeepL 翻译 API 请求超时（秒）",
			ValueType:   "duration", DefaultValue: "30", Min: strPtr("5"), Max: strPtr("300"), Step: strPtr("5")},
		{Group: "misc", Key: "reply_sem_timeout", Name: "回复语义超时",
			Description: "Webhook AI 回复语义分析超时（秒）",
			ValueType:   "duration", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("60"), Step: strPtr("1")},
		{Group: "misc", Key: "ownership_cache_ttl", Name: "归属缓存 TTL",
			Description: "权限归属检查的结果缓存有效期（秒）",
			ValueType:   "duration", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("300"), Step: strPtr("1")},
		{Group: "misc", Key: "summary_stale_threshold", Name: "Dashboard 摘要过期阈值",
			Description: "Dashboard SSE 摘要数据超过此时间视为过期需刷新（秒）",
			ValueType:   "duration", DefaultValue: "600", Min: strPtr("60"), Max: strPtr("3600"), Step: strPtr("30")},
		{Group: "misc", Key: "agentloop_history_max_candidates", Name: "AgentLoop 历史最大候选",
			Description: "AgentLoop 召回的历史对话最大候选数",
			ValueType:   "int", Unit: "条", DefaultValue: "200", Min: strPtr("10"), Max: strPtr("10000"), Step: strPtr("10")},

		// ---- lock 组：锁与幂等 TTL（阶段 2c）----
		// 这一组与其它组性质不同：这些 TTL 是正确性参数而不是调优项，写错的
		// 结果是死锁、并发重复落库或重复回复，所以每条 Description 都写清
		// 「改小/改大会出什么事」，而不是只写「是什么」。
		{Group: "lock", Key: "inbox_human_ttl", Name: "人工接管锁 TTL",
			Description: "收件箱人工接管锁的存活时间（秒）。锁到期后该会话的入站消息会被 AI 继续处理——写小＝人工还没说完就被交回给 AI，写大＝人工忘记点「结束接管」后长时间无法自动恢复",
			ValueType:   "duration", Risk: "danger", DefaultValue: "86400", Min: strPtr("60"), Max: strPtr("604800"), Step: strPtr("60"), Restart: false},
		{Group: "lock", Key: "inbox_pending_ttl", Name: "入站待处理队列 TTL",
			Description: "AI 防抖期间暂存入站消息的存活时间（秒）。写小＝防抖窗口还没走完消息就消失了，客户那句话直接丢失且无投递痕迹",
			ValueType:   "duration", Risk: "danger", DefaultValue: "300", Min: strPtr("30"), Max: strPtr("86400"), Step: strPtr("30"), Restart: false},
		{Group: "lock", Key: "inbox_content_dedup_ttl", Name: "入站内容去重窗口",
			Description: "入口「渠道+发送者+内容」去重键的存活时间（秒）。写小＝平台重发与出站回显这类正常重复会漏过，写大＝客户隔很久把同一句话又说了一遍会被当成重复而吞掉",
			ValueType:   "duration", DefaultValue: "300", Min: strPtr("10"), Max: strPtr("86400"), Step: strPtr("30"), Restart: false},
		{Group: "lock", Key: "ingest_lock_ttl", Name: "入站落库锁 TTL",
			Description: "同一会话入站落库互斥锁的存活时间（秒）。锁只在落库函数执行期间持有，写小＝长事务没跑完锁就被别人抢走导致并发重复落库，写大＝进程崩溃后该会话被挡住直到 TTL 自然过期",
			ValueType:   "duration", Risk: "danger", DefaultValue: "25", Min: strPtr("5"), Max: strPtr("600"), Step: strPtr("5"), Restart: false},
		{Group: "lock", Key: "ai_processing_ttl", Name: "AI 处理中标记 TTL",
			Description: "「AI 正在处理」排他标记的存活时间（秒），推理期间到达的新消息靠它避免被补触发第二次。写小＝重复回复，写大＝AI 进程崩了之后该会话长时间不再响应",
			ValueType:   "duration", Risk: "danger", DefaultValue: "120", Min: strPtr("10"), Max: strPtr("3600"), Step: strPtr("10"), Restart: false},
		{Group: "lock", Key: "message_hub_idem_ttl", Name: "消息中台幂等 TTL",
			Description: "消息中台投递幂等键的存活时间（秒），同一 msgID 在此窗口内重复投递会被判重丢弃。注意它在账号服务构造期读取，改动要重启才对已启动的账号生效",
			ValueType:   "duration", Risk: "danger", DefaultValue: "86400", Min: strPtr("60"), Max: strPtr("604800"), Step: strPtr("60"), Restart: true},
		{Group: "lock", Key: "ai_lock_ttl", Name: "AI 会话锁 TTL（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】InboxAILockKey 那把锁的兜底 TTL（秒）。tryAcquireAILock / ReleaseAILock / IsSessionAIBusy 三个函数整条生产链路都没人调用（源文件自带 //nolint:unused 标注），真正在跑的并发闸是上面 ai_processing_ttl 那条；要启用这套锁得先接 callers，再接本参数",
			ValueType:   "duration", Risk: "danger", Deprecated: true, DeprecatedReason: "InboxAILockKey 那把锁的兜底 TTL。tryAcquireAILock / ReleaseAILock / IsSessionAIBusy 三个函数整条生产链路都没人调用（源文件自带 //nolint:unused 标注），真正在跑的并发闸是同组的 ai_processing_ttl。要重新提供这个旋钮，先给这三个函数接上生产调用方，再把 TTL 接到本条", DefaultValue: "15", Min: strPtr("5"), Max: strPtr("300"), Step: strPtr("5"), Restart: false},

		// ── channelbot 组：渠道侧的长度/条数上限与外部依赖超时 ──────────────
		// 平台自身也有硬上限（QQ 2000 / TG 4096 / inline keyboard 100 行 8 钮），
		// 调大超过平台限制只会让平台侧报错，调小则提前分批发送。

		{Group: "channelbot", Key: "qq_message_max_len", Name: "QQ 单条消息长度上限",
			Description: "QQ 渠道单条文本消息的最大长度（字符）。超出即自动分段发送，返回首段 msg_id。写大超过平台 2000 上限会被平台直接拒收而本地毫不知情，写小则一条回复被拆成很多条、观感碎",
			ValueType:   "int", Unit: "字符", DefaultValue: "2000", Min: strPtr("1"), Max: strPtr("2000"), Step: strPtr("1"), Restart: false},
		{Group: "channelbot", Key: "tg_message_max_length", Name: "TG 单条消息长度上限",
			Description: "Telegram 渠道单条文本消息的最大长度（字符）。超出即自动分段发送，返回首段 message_id。写大超过平台 4096 上限会被平台直接拒收，写小则一条回复被拆成很多条",
			ValueType:   "int", Unit: "字符", DefaultValue: "4096", Min: strPtr("1"), Max: strPtr("4096"), Step: strPtr("1"), Restart: false},
		{Group: "channelbot", Key: "tg_inline_rows_max", Name: "TG inline 键盘行数上限",
			Description: "Telegram inline keyboard 的最大行数。超出即截断（多出来的按钮直接消失）。写大超过平台 100 行上限会让整个键盘被平台拒收导致按钮全部消失，比截断更糟",
			ValueType:   "int", Unit: "行", Risk: "danger", DefaultValue: "100", Min: strPtr("1"), Max: strPtr("100"), Step: strPtr("1"), Restart: false},
		{Group: "channelbot", Key: "tg_inline_buttons_per_row_max", Name: "TG inline 键盘每行按钮数上限",
			Description: "Telegram inline keyboard 每一行的最大按钮数。超出即截断。写大超过平台 8 个上限会让整个键盘被拒收导致按钮全部消失",
			ValueType:   "int", Unit: "个", Risk: "danger", DefaultValue: "8", Min: strPtr("1"), Max: strPtr("8"), Step: strPtr("1"), Restart: false},
		{Group: "channelbot", Key: "http_timeout", Name: "渠道外部依赖 HTTP 超时",
			Description: "channelbot 各子包（QQ/TG/WhatsApp）访问平台 API 的默认 HTTP 超时（秒）。构造期读一次，改完需重启。写大＝外部依赖挂起时请求 goroutine 长时间占着不放，写小＝平台侧慢一点就误判超时（Telegram 的长轮询 getUpdates 天然需要几十秒，别把它调小）",
			ValueType:   "duration", DefaultValue: "30", Min: strPtr("1"), Max: strPtr("120"), Step: strPtr("1"), Restart: true},

		// ── sse 组：业务侧 SSE 连接参数 ───────────────────────────────────
		// 注意与 bridge.sse_* 区分：那两条管 bridge 长连接，这一组管业务 SSE Hub。
		{Group: "sse", Key: "heartbeat_interval", Name: "SSE 心跳间隔",
			Description: "业务 SSE 连接发送心跳注释帧的间隔（秒）。写大＝反代/CDN 的 proxy_read_timeout（通常 60s）会切断长连接，写小＝空连接也持续占用带宽。0 值会让 time.NewTicker(0) 直接 panic，故非法值一律回落兜底",
			ValueType:   "duration", DefaultValue: "15", Min: strPtr("1"), Max: strPtr("60"), Step: strPtr("1"), Restart: false},
		{Group: "sse", Key: "max_conn_per_ip", Name: "SSE 单 IP 连接数上限",
			Description: "同一个来源 IP 允许同时打开的 SSE 连接数。这是防单点刷连接的闸。写大＝一个 IP 能把连接池占满挤掉正常用户，写小＝NAT/办公室出口共用 IP 的场景下多名正常用户会互相挤掉。0 会把所有访客一次性挡在门外",
			ValueType:   "int", Risk: "danger", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("200"), Step: strPtr("1"), Restart: false},
		{Group: "sse", Key: "client_buffer_size", Name: "SSE 客户端事件缓冲",
			Description: "每个 SSE 连接本地事件队列的容量（条）。写大＝每个连接按上限常驻内存，写小＝事件突发时更早触发丢弃/断链。0 会让缓冲退化为同步信道，一个慢客户端就能堵死整个广播。构造期读一次，改完需重启",
			ValueType:   "int", Risk: "danger", DefaultValue: "100", Min: strPtr("1"), Max: strPtr("10000"), Step: strPtr("1"), Restart: true},

		// ── bridge 组补充：回放 backlog 与总线缓冲 ────────────────────────
		// bridge.sse_heartbeat_interval / sse_max_stream_duration 更早就已 DB 驱动，
		// 这里补的是同文件里另外两个此前写死的。
		{Group: "bridge", Key: "sse_max_backlog_events", Name: "bridge SSE 回放 backlog 上限",
			Description: "每个 bridge SSE 连接为断线重连保留的回放事件数（条）。写大＝每个长连接按上限常驻内存，写小＝客户端重连后更早开始丢事件（表现为「中间几条消息没收到」）",
			ValueType:   "int", Risk: "danger", DefaultValue: "1000", Min: strPtr("1"), Max: strPtr("100000"), Step: strPtr("1"), Restart: false},
		{Group: "bridge", Key: "sse_bus_buffer_size", Name: "bridge SSE 总线缓冲",
			Description: "bridge SSE 事件总线的 channel 容量（条）。写大＝一次事件洪峰更不容易阻塞发布方，代价是常驻内存；写小＝洪峰时发布方被阻塞。0 会让总线退化为同步信道，一个慢消费者就能堵死所有出站投递。构造期读一次，改完需重启",
			ValueType:   "int", Risk: "danger", DefaultValue: "100", Min: strPtr("1"), Max: strPtr("100000"), Step: strPtr("1"), Restart: true},
		// B1-a：桥接选择器健康度的降级阈值。
		// 读点：internal/service/selector_health.go 的 SelectorHealthService.Threshold
		// （每请求热读，改完即时生效；兜底常量 SelectorHealthDegradedBelow 与 DefaultValue 一致）。
		{Group: "bridge", Key: "selector_health_degraded_below",
			Name:        "桥接选择器命中率降级阈值",
			Description: "选择器命中率低于该百分比即判 degraded（0–100，样本不足 20 次的先归 warming 不算降级）。平台改版后桥接扩展仍在跑、但读不到消息，此前的表现是「这个账号今天没客户」；有了它，运营面会直接看到哪个渠道的哪个字段命中了率掉了。写高＝更容易报警（改版刚发生时就能看到，代价是页面加载慢的正常页面也可能短暂飘红），写低＝更稳但可能漏掉真实失效。非正数或 >100 一律回落兜底 60",
			ValueType:   "int", Unit: "%", DefaultValue: "60", Min: strPtr("1"), Max: strPtr("100"), Step: strPtr("1"), Restart: false},
		{Group: "memory", Key: "l1_ttl_hours", Name: "短期记忆 TTL",
			Description: "L1 短期记忆（MemoryLayerShortTerm）的存活秒数。它同时是召回窗口：调大让多轮对话上下文更连贯，调小让旧消息更快沉到 L4 长期层。默认 24h 与单会话的常见跨度相当。调 0 会让刚写入的短期消息立即过期、短期召回永远空手，症状是「AI 突然不记得上一句」；非正数一律回落代码兜底",
			ValueType:   "duration", DefaultValue: "86400", Min: strPtr("60"), Max: strPtr("604800"), Step: strPtr("60")},
		{Group: "memory", Key: "l1_window_size", Name: "短期记忆窗口大小",
			Description: "单个会话保留的 L1 短期记忆条数（超出即淘汰最旧）。它同时是多轮召回的上下文宽度：写大＝上下文更连贯，但每轮召回喂给 LLM 的 token 变多、成本上升；写小＝更早丢历史，「AI 突然不记得上一句」。0 或负值会让 l1Trim 把整个会话的短期记忆删光，故一律回落兜底",
			ValueType:   "int", Unit: "条", DefaultValue: "10", Min: strPtr("1"), Max: strPtr("1000"), Step: strPtr("1"), Restart: false},
		{Group: "geo", Key: "default_visibility_days", Name: "可见性趋势默认天数",
			Description: "AI 可见性趋势查询在调用方没给 days 时的默认时间窗（天）。注意它不是上限：days 大于 365 仍会被夹到 365（一次要回 365 行按引擎的聚合，是查询成本闸，不随默认值一起放大）。改小会让环比对比的样本不足（不足 2 天直接无环比）",
			ValueType:   "int", Unit: "天", DefaultValue: "30", Min: strPtr("1"), Max: strPtr("365"), Step: strPtr("1")},
		{Group: "reporting", Key: "default_window_days", Name: "报表默认窗口天数",
			Description: "报表/列表类接口在调用方没给 days（或给了非法值）时的默认统计窗口（天）。收口原先散在 CSAT 趋势、i18n 质量趋势、邮件可达性、销售工作台、AI 产能、知识库统计等十几处各自写死的 30。每请求热读，改完立即生效；不是各接口的上限（>365 或 >90 的夹取上限仍按各点位自己的口径）。非正数一律回落代码兜底 30",
			ValueType:   "int", Unit: "天", DefaultValue: "30", Min: strPtr("1"), Max: strPtr("365"), Step: strPtr("1")},
		// ── webhook 组：回调处理的并发、限流与重试 ────────────────────────
		// 七个常量原先散在 webhook_dedup.go / webhook.go / webhook_ai.go 三处。
		// 优先级链：env > 本组 > 常量兜底；env 保留为部署层应急开关。
		// 生效时机分两类：dedup_ttl / rate_limit / rate_burst / max_retries 是每请求读，
		// 改完立即生效；worker_count / queue_size / reply_concurrency 在
		// NewWebhookService 里构造，标 Restart=true 只对下次启动生效。
		{Group: "webhook", Key: "dedup_ttl", Name: "webhook 事件去重窗口",
			Description: "同一 event_id 的 webhook 回调在多长时间内视为重复事件并直接丢弃（秒）。平台重试/客户端重发都靠它兜住，写小会出现「同一条消息进来两次」的重复处理，写大＝一次内容相同的重复回放被压掉的时间更长（配错时的症状是消息莫名丢失，且只在重试时复现）",
			ValueType:   "duration", DefaultValue: "300", Min: strPtr("10"), Max: strPtr("86400"), Step: strPtr("10"), Restart: false},
		{Group: "webhook", Key: "rate_limit", Name: "webhook 每账号令牌桶速率",
			Description: "单个账号（或来源 key）的令牌桶每秒补充令牌数，即持续允许的回调速率（次/秒）。写大＝平台侧抖动/重试风暴直接打到下游处理，写小＝正常批量事件被限流丢弃（客户消息延迟几分钟才出现）。默认 30 次/秒远高于人工消息速率，主要防脚本刷量",
			ValueType:   "int", Unit: "次/秒", Risk: "danger", DefaultValue: "30", Min: strPtr("1"), Max: strPtr("100000"), Step: strPtr("1"), Restart: false},
		{Group: "webhook", Key: "rate_burst", Name: "webhook 每账号令牌桶容量",
			Description: "单个账号令牌桶的桶容量（次），即可容忍的瞬时突发量。写大＝一次洪峰全部放行，下游瞬时压力全吃；写小＝洪峰被削平成限流丢弃（客户连发几条只收到前几条）。要与 rate_limit 搭配看：容量小于速率等于没有缓冲",
			ValueType:   "int", Risk: "danger", DefaultValue: "60", Min: strPtr("1"), Max: strPtr("100000"), Step: strPtr("1"), Restart: false},
		{Group: "webhook", Key: "worker_count", Name: "webhook 处理 worker 数",
			Description: "从回调队列取任务并发处理的 goroutine 数。写大＝CPU/下游依赖被打满（表现为下游超时成串）；写小＝队列堆积、回调处理延迟。构造期读一次，改完需重启才生效",
			ValueType:   "int", DefaultValue: "4", Min: strPtr("1"), Max: strPtr("256"), Step: strPtr("1"), Restart: true},
		{Group: "webhook", Key: "queue_size", Name: "webhook 回调队列容量",
			Description: "回调任务队列的 channel 容量（条）。写大＝洪峰时更多任务在内存里排队（重启全丢）；写小＝队列满时新回调被直接丢弃（客户消息彻底不到达）。构造期读一次，改完需重启才生效",
			ValueType:   "int", Risk: "danger", DefaultValue: "512", Min: strPtr("1"), Max: strPtr("1000000"), Step: strPtr("1"), Restart: true},
		{Group: "webhook", Key: "reply_concurrency", Name: "webhook 回复并发闸",
			Description: "同时允许进行的 AI 回复数（信号量容量）。这是防 LLM 侧被打满的总闸。写大＝LLM 侧并发超限触发限流/降级（表现为回复大面积失败）；写小＝回复排队，客户等待变长。构造期读一次，改完需重启才生效",
			ValueType:   "int", DefaultValue: "32", Min: strPtr("1"), Max: strPtr("10000"), Step: strPtr("1"), Restart: true},
		{Group: "webhook", Key: "max_retries", Name: "webhook 失败重试次数",
			Description: "渠道投递失败后的重试轮数（不含首投）。写大＝下游长时间不可用时会一直重试，占用 worker 与令牌；写小＝一次瞬时失败就放弃（客户收不到回复）。只对「可重试」错误生效，不可重试错误立即放弃。注意与 agent_llm.max_retries 区分：那一条管 LLM 调用本身的重试，本条目管渠道出网投递",
			ValueType:   "int", Unit: "次", DefaultValue: "3", Min: strPtr("0"), Max: strPtr("20"), Step: strPtr("1"), Restart: false},
		// ── sop 组：SOP 调度器并发 与 Saga 补偿参数 ───────────────────────
		// 六条全部是构造期读一次，故全标 Restart=true：调度器的 worker 数/队列容量/
		// LLM 信号量在 NewSOPExecutionDispatcher 里 make(chan,N) 时定型，补偿的
		// 计划表大小决定内存占用。改完只对下次启动的进程生效。
		{Group: "sop", Key: "dispatcher_worker_count", Name: "SOP 调度 worker 数",
			Description: "SOP 执行调度器并发处理节点的 goroutine 数。写大＝下游依赖被打满（表现为节点执行超时成串）；写小＝节点排队、整条 SOP 流程变慢。构造期读一次，改完需重启才生效",
			ValueType:   "int", DefaultValue: "16", Min: strPtr("1"), Max: strPtr("512"), Step: strPtr("1"), Restart: true},
		{Group: "sop", Key: "dispatcher_queue_capacity", Name: "SOP 调度队列容量",
			Description: "SOP 待执行任务的 channel 容量（条）。写大＝洪峰时更多任务在内存里排队（重启全丢）；写小＝队列满时新任务被直接丢弃（流程卡住不动）。构造期读一次，改完需重启才生效",
			ValueType:   "int", Risk: "danger", DefaultValue: "1000", Min: strPtr("1"), Max: strPtr("1000000"), Step: strPtr("1"), Restart: true},
		{Group: "sop", Key: "dispatcher_llm_concurrency", Name: "SOP 节点 LLM 并发闸",
			Description: "同时允许进行的 LLM 类 SOP 节点数（信号量容量）。这是防 LLM 侧被打满的总闸。写大＝LLM 侧并发超限触发限流/降级；写小＝节点排队，流程尾部长时间等待。构造期读一次，改完需重启才生效",
			ValueType:   "int", DefaultValue: "4", Min: strPtr("1"), Max: strPtr("1000"), Step: strPtr("1"), Restart: true},
		{Group: "sop", Key: "compensation_max_attempts", Name: "Saga 补偿单节点重试次数",
			Description: "单个 SOP 节点补偿失败后的重试轮数（不含首偿）。写大＝补偿侧长时间占用资源；写小＝一次瞬时失败就放弃补偿（流程停在失败态，需人工介入）。仅在 FF_LTC_SAGA_COMPENSATION 开关开启时才有意义，开关关闭时整条链路不装配。构造期读一次，改完需重启才生效",
			ValueType:   "int", Unit: "次", DefaultValue: "3", Min: strPtr("0"), Max: strPtr("20"), Step: strPtr("1"), Restart: true},
		{Group: "sop", Key: "compensation_ttl", Name: "Saga 单节点补偿超时",
			Description: "单个节点补偿操作的上下文超时（秒）。写大＝一个卡死的补偿动作长时间占着资源；写小＝慢一点的撤销动作被判超时放弃（流程停在半补偿态）。仅在 FF_LTC_SAGA_COMPENSATION 开关开启时才有意义。构造期读一次，改完需重启才生效",
			ValueType:   "duration", DefaultValue: "30", Min: strPtr("1"), Max: strPtr("600"), Step: strPtr("1"), Restart: true},
		{Group: "sop", Key: "compensation_max_plans_kept", Name: "Saga 补偿计划保留条数",
			Description: "内存中保留的补偿计划条数上界，超出即淘汰最旧者。这是防无界增长的内存闸：写大＝常驻内存上升；写小＝仍在执行中的旧补偿计划可能被提前淘汰（若在读取时被淘汰，返回那一刻的内容仍有效，但后续状态机会丢失依据）。仅在 FF_LTC_SAGA_COMPENSATION 开关开启时才有意义。构造期读一次，改完需重启才生效",
			ValueType:   "int", Unit: "条", DefaultValue: "512", Min: strPtr("1"), Max: strPtr("100000"), Step: strPtr("1"), Restart: true},
		// ── domain_health 组：域名健康度阈值 ──────────────────────────────
		// 四条全是"看一眼分数就做决定"的类型：改错的结果不是性能问题，而是"该切的没切"。
		{Group: "domain_health", Key: "score_healthy", Name: "域名健康分达标线",
			Description: "域名健康分达到该值即视为健康、可参与选优与自动启用（0-100）。写大＝很多域名被判不健康、可用池缩小甚至无域名可选；写小＝带病的域名被当成健康继续承载流量。这是整条健康度判定的主刻度，score_switch_threshold 应低于它",
			ValueType:   "int", Unit: "分", DefaultValue: "80", Min: strPtr("1"), Max: strPtr("100"), Step: strPtr("1"), Restart: false},
		{Group: "domain_health", Key: "score_switch_threshold", Name: "域名自动切换触发线",
			Description: "活跃域名健康分跌破该值即触发自动切换到最优域名（0-100）。写大＝一有抖动就切主域名（表现为域名频繁变动、上下游缓存失效）；写小＝已经明显带病的域名还在承载流量。必须低于 score_healthy，高于它等于「永远不切，因为健康判定先失败」",
			ValueType:   "int", Unit: "分", Risk: "danger", DefaultValue: "30", Min: strPtr("0"), Max: strPtr("100"), Step: strPtr("1"), Restart: false},
		{Group: "domain_health", Key: "consecutive_failure_limit", Name: "域名连续失败次数上限",
			Description: "连续探测失败达到该次数即触发自动切换，是「分数还没掉下来就先切走」的旁路。写大＝域名已经明显连不上还要继续探；写小＝偶发的网络抖动就触发切换（域名频繁变动）",
			ValueType:   "int", Unit: "次", Risk: "warning", DefaultValue: "3", Min: strPtr("1"), Max: strPtr("100"), Step: strPtr("1"), Restart: false},
		{Group: "domain_health", Key: "check_timeout", Name: "域名探测超时",
			Description: "每次域名探测的 HTTP 超时（秒）。写大＝挂起的域名拖长整轮巡检（批量巡检会串行等满超时）；写小＝慢一点的正常域名被误判不可达、健康分下滑。构造期读一次，改完需重启才生效",
			ValueType:   "duration", DefaultValue: "5", Min: strPtr("1"), Max: strPtr("60"), Step: strPtr("1"), Restart: true},

		// ── rag_health 组：RAG 健康度窗口与六维权重 ───────────────────────
		// 六个权重按总和归一化后使用（report.Score 是六个 WeightedScore 直接相加），
		// 所以每个旋钮的语义是干净的"相对比重"，调哪一项都不会把总分推出 0-100。
		{Group: "rag_health", Key: "default_window", Name: "RAG 健康度默认窗口",
			Description: "计算 RAG 健康度时在调用方没给 window 的默认时间窗（秒）。写大＝一次要回更多行的指标按时间聚合，报告更稳但更钝（问题过去很久才从分数上消退）；写小＝样本不足（不足 2 天直接无环比），分数抖动明显",
			ValueType:   "duration", DefaultValue: "3600", Min: strPtr("60"), Max: strPtr("604800"), Step: strPtr("60"), Restart: false},
		{Group: "rag_health", Key: "weight_retrieval", Name: "RAG 健康度·检索可用性权重",
			Description: "六个维度里「检索可用性」的相对比重。这一项衡量最近窗口内有没有检索发生过（TotalQueries>0 即满分）。默认只给 0.10：它更像「链路通不通」的存活信号，不该主导总分。六项会按总和归一化，所以调任一项都不会把总分推出 0-100",
			ValueType:   "float", DefaultValue: "0.10", Min: strPtr("0.01"), Max: strPtr("10"), Step: strPtr("0.01"), Restart: false},
		{Group: "rag_health", Key: "weight_recall", Name: "RAG 健康度·召回质量权重",
			Description: "六个维度里「召回质量」的相对比重（按 AvgRecall 打分）。默认给到 0.30，是六个里最大的一项：召回不到正确内容，后面所有环节做得再好也答不对。写大＝总分几乎等于召回率；写小＝召回烂掉时总分看起来还正常，属于掩盖问题",
			ValueType:   "float", DefaultValue: "0.30", Min: strPtr("0.01"), Max: strPtr("10"), Step: strPtr("0.01"), Restart: false},
		{Group: "rag_health", Key: "weight_embedding", Name: "RAG 健康度·向量化质量权重",
			Description: "六个维度里「向量化质量」的相对比重（失败率/维度异常）。默认 0.15。写大＝向量化一有抖动总分就掉；写小＝批量文档向量化失败只在告警维度反映，总分看不出来",
			ValueType:   "float", DefaultValue: "0.15", Min: strPtr("0.01"), Max: strPtr("10"), Step: strPtr("0.01"), Restart: false},
		{Group: "rag_health", Key: "weight_coverage", Name: "RAG 健康度·知识库覆盖权重",
			Description: "六个维度里「知识库覆盖」的相对比重（有多少知识库真的被检索命中过）。默认 0.15。写大＝新导入的知识库长期没人问会导致总分下滑；写小＝覆盖盲区被掩盖",
			ValueType:   "float", DefaultValue: "0.15", Min: strPtr("0.01"), Max: strPtr("10"), Step: strPtr("0.01"), Restart: false},
		{Group: "rag_health", Key: "weight_performance", Name: "RAG 健康度·性能权重",
			Description: "六个维度里「性能」的相对比重（检索延迟分位）。默认 0.15。写大＝一次慢查询就把总分拉低；写小＝检索已经慢到影响体验，总分仍显示健康",
			ValueType:   "float", DefaultValue: "0.15", Min: strPtr("0.01"), Max: strPtr("10"), Step: strPtr("0.01"), Restart: false},
		{Group: "rag_health", Key: "weight_alerts", Name: "RAG 健康度·告警状态权重",
			Description: "六个维度里「告警状态」的相对比重（未处理告警数）。默认 0.15。写大＝堆积一批未读告警就让总分跌到 C/D；写小＝告警无人处理时总分仍是 A，看板失去预警意义",
			ValueType:   "float", DefaultValue: "0.15", Min: strPtr("0.01"), Max: strPtr("10"), Step: strPtr("0.01"), Restart: false},
		// ── feedback 组：反馈学习闭环（bandit / 采集器 / 销冠分析 / Prompt 迭代 / 自动优化）
		// 22 条全部构造期读一次，故全标 Restart=true：五个组件的配置在
		// InitFeedbackLoopComponents / InitFeedbackCollector 里组装完就固化在各组件的
		// config 字段里，改完只对下次启动的进程生效。
		// 信号权重表（DefaultSignalWeights，12 键）刻意不入库：那是一张双射表，
		// 拆成 12 条参数后运维要看全貌反而更难，要动它应走字典表（阶段四）。
		{Group: "feedback", Key: "bandit_min_samples_for_exploit", Name: "bandit 转入利用的最小样本量（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】BanditAllocator.SelectArm 随 self_learning 模块（b36025e3）失去唯一调用方，2026-10 审计删除整条臂选择路径，BanditConfig 已无对应字段；当前 SOP 变体分流走 SOPAgent.ABTestConfig",
			ValueType:   "int", Unit: "条", Deprecated: true, DeprecatedReason: "SelectArm/SelectPrompt 全树已无生产调用方（git 历史中唯一消费者 self_learning 于 2026-08-08 b36025e3 删除），且 SelectArm 持 b.mu 再调 betaSample 会自锁死。要重新提供这个旋钮，先在生产链路接入臂选择、恢复 BanditConfig.MinSamplesForExploit 并登记回本条", DefaultValue: "30", Min: strPtr("1"), Max: strPtr("100000"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "bandit_exploration_floor", Name: "bandit 探索流量下限（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】该参数在代码里从未有过读取方（SelectArm 也未使用），随臂选择路径一并下架",
			ValueType:   "float", Risk: "warning", Deprecated: true, DeprecatedReason: "全树从未有代码读取 ExplorationFloor：它只出现在 DefaultBanditConfig 与参数装配里，SelectArm 的探索比例也没有引用它。要重新提供这个旋钮，先在臂选择实现里真正使用该下限，再登记回本条", DefaultValue: "0.10", Min: strPtr("0.001"), Max: strPtr("1"), Step: strPtr("0.01"), Restart: true},
		{Group: "feedback", Key: "bandit_traffic_ceiling", Name: "bandit 单 arm 流量上限（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】只被 SelectArm 的 enforceTrafficCeiling 读取，该路径已随 2026-10 审计删除",
			ValueType:   "float", Risk: "warning", Deprecated: true, DeprecatedReason: "enforceTrafficCeiling/pickLowestTrafficArm 只服务 SelectArm，且 SelectArm 全树无生产调用方，已整段删除。要重新提供这个旋钮，先接入臂选择路径并恢复该配置字段，再登记回本条", DefaultValue: "0.60", Min: strPtr("0.01"), Max: strPtr("1"), Step: strPtr("0.01"), Restart: true},
		{Group: "feedback", Key: "bandit_convergence_threshold", Name: "bandit 收敛判定阈值",
			Description: "后验分布判定实验已收敛所需的最低一致度（0-1）。写大＝要更确定的证据才宣布收敛（收敛慢但结论更可信）；写小＝过早宣布收敛，把还在摇摆中的 arm 定为胜者。0 值回落默认 0.95",
			ValueType:   "float", DefaultValue: "0.95", Min: strPtr("0.5"), Max: strPtr("1"), Step: strPtr("0.01"), Restart: true},
		{Group: "feedback", Key: "bandit_min_samples_for_promote", Name: "bandit 晋升胜者的最小样本量",
			Description: "宣布某个 arm 为胜者并晋升前，它至少要拿到的样本数。写小＝样本不足就晋升，胜者可能只是运气好；写大＝迟迟不肯晋升，实验周期被拉长。0 或负值回落默认 100",
			ValueType:   "int", Unit: "条", DefaultValue: "100", Min: strPtr("1"), Max: strPtr("1000000"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "bandit_posterior_samples", Name: "bandit 后验采样数",
			Description: "每次计算后验分布时的蒙特卡洛采样次数。写大＝判定更稳但 CPU 更贵（这是纯计算量旋钮）；写小＝收敛判定噪声变大，同一份数据两次跑可能给出不同结论。0 或负值回落默认 1000",
			ValueType:   "int", Unit: "次", DefaultValue: "1000", Min: strPtr("10"), Max: strPtr("1000000"), Step: strPtr("10"), Restart: true},
		{Group: "feedback", Key: "collector_queue_size", Name: "反馈事件队列容量",
			Description: "反馈事件进内存队列的容量（条）。写大＝突发反馈更多在内存里排队（进程重启全丢）；写小＝队列满时新事件被直接丢弃（ErrQueueFull），表现为「点了赞但学习不到」。0 或负值回落默认 1000",
			ValueType:   "int", Risk: "danger", DefaultValue: "1000", Min: strPtr("1"), Max: strPtr("1000000"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "collector_flush_interval", Name: "反馈刷盘间隔",
			Description: "采集器把内存里的反馈事件批量写入 DB 的间隔（秒）。写小＝几乎实时落库但写放大（每来几条就刷一次盘）；写大＝落库延迟变长，进程崩溃时丢失的窗口更大。0 值回落默认 2s",
			ValueType:   "duration", DefaultValue: "2", Min: strPtr("1"), Max: strPtr("3600"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "collector_batch_size", Name: "反馈刷盘批量",
			Description: "每次刷盘最多写入的反馈事件条数。写大＝单次事务更大、吞吐更好，但一条失败回滚的代价也更大；写小＝小而频繁。要与 collector_flush_interval 搭配看：批量小于到达速率时队列会持续增长。0 或负值回落默认 50",
			ValueType:   "int", Unit: "条", DefaultValue: "50", Min: strPtr("1"), Max: strPtr("100000"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "analyzer_min_reward", Name: "销冠样本最低奖励分",
			Description: "一条对话要被选为「销冠样本」所需的最低奖励分。写大＝只挑顶尖对话，样本少但质量高；写小＝样本多但可能混进一般对话，聚类出来的「销冠模式」被稀释。填 0 表示关掉这道奖励门槛（只按其他条件筛）。0 值回落默认 1.0",
			ValueType:   "float", DefaultValue: "1", Min: strPtr("-100"), Max: strPtr("100"), Step: strPtr("0.1"), Restart: true},
		{Group: "feedback", Key: "analyzer_cluster_sim_threshold", Name: "销冠样本聚类相似度阈值",
			Description: "两条销冠对话被归入同一簇所需的相似度（0-1）。写大＝簇更纯但数量多、每簇样本少；写小＝簇少而杂，「同一类打法」里混进不同模式，提取出的提示词会互相矛盾。0 值回落默认 0.85",
			ValueType:   "float", DefaultValue: "0.85", Min: strPtr("0.1"), Max: strPtr("1"), Step: strPtr("0.01"), Restart: true},
		{Group: "feedback", Key: "analyzer_min_cluster_size", Name: "销冠聚类最小簇规模",
			Description: "一个簇至少要有多少条对话才会被认定为有效模式。写大＝只承认反复出现的打法，模式少但可靠；写小＝偶然的一次好对话也被当成模式，可能把噪声固化成提示词。0 或负值回落默认 3",
			ValueType:   "int", Unit: "条", DefaultValue: "3", Min: strPtr("1"), Max: strPtr("100000"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "analyzer_top_k_per_cluster", Name: "每簇保留的样板条数",
			Description: "每个聚类里最终保留多少条样板对话（取奖励分最高的若干）。写大＝每簇样板更全，但喂给 LLM 的上下文更长、更贵；写小＝提示词更紧凑，但可能丢掉簇内有价值的变体。0 或负值回落默认 3",
			ValueType:   "int", Unit: "条", DefaultValue: "3", Min: strPtr("1"), Max: strPtr("1000"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "analyzer_max_dialogues_per_run", Name: "单轮分析对话数上限",
			Description: "单轮销冠分析最多处理多少条对话。这是单轮的成本闸：写大＝每轮覆盖更全但跑得久、LLM 花费高；写小＝只覆盖最近的少量对话，可能长期错过某类打法。0 或负值回落默认 500",
			ValueType:   "int", Unit: "条", DefaultValue: "500", Min: strPtr("1"), Max: strPtr("1000000"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "iterator_min_samples_for_iteration", Name: "Prompt 迭代最小样本量",
			Description: "触发一轮 Prompt 迭代所需的最小样本数。写小＝样本不足就开始改提示词，改动可能是噪声驱动；写大＝迟迟不迭代，问题提示词长期在线。0 或负值回落默认 50",
			ValueType:   "int", Unit: "条", DefaultValue: "50", Min: strPtr("1"), Max: strPtr("1000000"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "iterator_negative_reward_threshold", Name: "Prompt 迭代负向门限",
			Description: "样本平均奖励分低于该值即认为当前提示词有问题、值得迭代（可为负）。写大（更接近 0）＝一有下滑就改，迭代频繁但可能反复横跳；写小（更负）＝要到明显很差才动手，改得少但每条提示词在线时间长。填 0 表示关掉这道门限（只按样本量触发）。0 值回落默认 -0.5",
			ValueType:   "float", Risk: "warning", DefaultValue: "-0.5", Min: strPtr("-100"), Max: strPtr("0"), Step: strPtr("0.1"), Restart: true},
		{Group: "feedback", Key: "iterator_candidates_per_run", Name: "每轮 Prompt 候选数",
			Description: "每轮迭代生成多少个候选提示词。写大＝一次看更多变体，命中更好版本的机会高，但 LLM 花费线性上涨；写小＝省成本但试错空间小。0 或负值回落默认 3",
			ValueType:   "int", Unit: "个", DefaultValue: "3", Min: strPtr("1"), Max: strPtr("100"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "iterator_auto_approve", Name: "Prompt 候选自动放行",
			Description: "迭代产出的候选提示词是否免人工评审直接生效。默认 false（要人看一眼）。改成 true 等于把「提示词改写」这条产线全自动——出错的候选会直接替换线上提示词，而它影响的是每一通对话。默认关闭是有意取舍",
			ValueType:   "bool", Risk: "warning", DefaultValue: "false", Restart: true},
		{Group: "feedback", Key: "optimizer_auto_apply_priority", Name: "自动优化生效优先级门槛",
			Description: "优化建议至少要达到该优先级才会被自动应用（数字越小优先级越高）。写小＝只有最高优先级的建议才自动落地，其余仍走人工；写大＝大量中低优先级建议也自动应用，覆盖面广但风险面同步放大。0 或负值回落默认 2",
			ValueType:   "int", DefaultValue: "2", Min: strPtr("0"), Max: strPtr("10"), Step: strPtr("1"), Restart: true},
		{Group: "feedback", Key: "optimizer_rollback_drop_threshold", Name: "自动回滚跌幅阈值",
			Description: "应用新方案后指标相对旧方案跌落超过该比例即触发回滚（0-1）。写大＝要跌得明显才回滚，劣化方案在线时间长；写小＝稍有下滑就回滚，好方案可能因正常波动被误杀。0 值回落默认 0.20",
			ValueType:   "float", DefaultValue: "0.20", Min: strPtr("0.001"), Max: strPtr("1"), Step: strPtr("0.01"), Restart: true},
		{Group: "feedback", Key: "optimizer_rollback_complaint_ratio", Name: "自动回滚投诉率阈值",
			Description: "应用新方案后投诉率超过该比例即触发回滚（0-1）。这是比跌幅更硬的一条安全线：投诉是用户用脚投票，宁可误杀也不让它持续。写大＝容忍更高投诉率；写小＝一有投诉苗头就回滚。0 值回落默认 0.50",
			ValueType:   "float", Risk: "warning", DefaultValue: "0.50", Min: strPtr("0.001"), Max: strPtr("1"), Step: strPtr("0.01"), Restart: true},
		{Group: "feedback", Key: "optimizer_ab_test_duration", Name: "自动优化 AB 实验时长",
			Description: "一轮自动优化 AB 实验的持续时间（秒）。写大＝样本充足、结论可信，但劣化方案在线时间也长；写小＝ 快速换方案，但可能测不出真实差异。默认 7 天。0 值回落默认 604800s",
			ValueType:   "duration", DefaultValue: "604800", Min: strPtr("3600"), Max: strPtr("2592000"), Step: strPtr("3600"), Restart: true},
		// ── frontend_ws 组：前端 WS 客户端的重连/心跳/ACK 旋钮 ─────────────
		// 后端没有读取点：这些常量活在 user-web/src/utils/{agentSocket,chatSocket,
		// journeyTracker}.js 里，前端经 GET /api/config/runtime 拉取值。
		// 它们登记进 runtimePublicGroups（controller/config_param_runtime.go），
		// 由 scripts/check-config-param-readpoints.py 按 group.key 粒度核对该份清单。
		{Group: "frontend_ws", Key: "agent_max_reconnect_attempts", Name: "坐席 WS 最大重连次数",
			Description: "坐席工作台 WebSocket 断线后的最大重连尝试次数。写大＝后端长时间不可用时前端持续重试（用户以为还在线，实际消息收发早已中断）；写小＝后端一次抖动就被判死刑，需要手动刷新页面才恢复。默认 10 次配合约 4 分钟的退避窗口",
			ValueType:   "int", Unit: "次", DefaultValue: "10", Min: strPtr("1"), Max: strPtr("200"), Step: strPtr("1"), Restart: false},
		{Group: "frontend_ws", Key: "chat_max_reconnect_attempts", Name: "聊天 WS 最大重连次数",
			Description: "客服聊天窗口 WebSocket 断线后的最大重连尝试次数。默认 50 远高于坐席那条：这个窗口是客户对着看的，断线后多试一会儿比直接显示「已断开体验好。写小＝客户视角频繁出现连接中断提示」",
			ValueType:   "int", Unit: "次", DefaultValue: "50", Min: strPtr("1"), Max: strPtr("500"), Step: strPtr("1"), Restart: false},
		{Group: "frontend_ws", Key: "agent_initial_reconnect_delay_ms", Name: "坐席 WS 首次重连延迟",
			Description: "坐席 WebSocket 第一次重连前等待的毫秒数（之后按指数退避，上限见 agent_max_reconnect_delay_ms）。写大＝断线后恢复更慢；写小＝后端还没起来就反复撞门，退避序列被拉长",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "2000", Min: strPtr("100"), Max: strPtr("60000"), Step: strPtr("100"), Restart: false},
		{Group: "frontend_ws", Key: "chat_initial_reconnect_delay_ms", Name: "聊天 WS 首次重连延迟",
			Description: "客服聊天窗口 WebSocket 第一次重连前等待的毫秒数。默认 1000 比坐席那条短：客户在等回复，早半秒连上就少一分「卡住了的观感」",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "1000", Min: strPtr("100"), Max: strPtr("60000"), Step: strPtr("100"), Restart: false},
		{Group: "frontend_ws", Key: "max_reconnect_delay_ms", Name: "WS 重连退避上限",
			Description: "两个 WS 客户端共用的重连退避上限（毫秒）。默认 30s：再往上等，用户早就关页面了；再往下则退避序列很快触顶、变成固定频率重试，起不到给后端喘息的作用",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "30000", Min: strPtr("1000"), Max: strPtr("300000"), Step: strPtr("1000"), Restart: false},
		{Group: "frontend_ws", Key: "agent_ping_interval_ms", Name: "坐席 WS 心跳间隔",
			Description: "坐席 WebSocket 发送应用层 ping 的间隔（毫秒）。写大＝反代/CDN 的 proxy_read_timeout（通常 60s）会切断长连接；写小＝空连接也持续占用带宽与上行配额。默认 25s 与常见 60s 网关超时留足余量",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "25000", Min: strPtr("1000"), Max: strPtr("120000"), Step: strPtr("1000"), Restart: false},
		{Group: "frontend_ws", Key: "chat_ping_interval_ms", Name: "聊天 WS 心跳间隔",
			Description: "客服聊天窗口 WebSocket 的应用层 ping 间隔（毫秒）。同上：必须显著小于反代读超时，否则客户看到的是「聊到一半不刷新了」",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "25000", Min: strPtr("1000"), Max: strPtr("120000"), Step: strPtr("1000"), Restart: false},
		{Group: "frontend_ws", Key: "agent_ack_batch_interval_ms", Name: "坐席 WS ACK 合批间隔",
			Description: "坐席 WebSocket 把待确认的消息批量上报的间隔（毫秒）。写大＝上行包少但端到端确认延迟高（服务端要更久才知道前端收到了）；写小＝几乎逐条确认，上行放大。默认 200ms 在人眼不可感知的量级",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "200", Min: strPtr("10"), Max: strPtr("5000"), Step: strPtr("10"), Restart: false},
		{Group: "frontend_ws", Key: "chat_ack_batch_interval_ms", Name: "聊天 WS ACK 合批间隔",
			Description: "客服聊天窗口 WebSocket 的 ACK 合批间隔（毫秒）。同坐席那条：这是「消息到底显示出来了没有的确认延迟，调太大会让重复投递窗口变长」",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "200", Min: strPtr("10"), Max: strPtr("5000"), Step: strPtr("10"), Restart: false},
		{Group: "frontend_ws", Key: "journey_history_max", Name: "访客轨迹本地保留条数",
			Description: "journeyTracker 在 localStorage 里保留的访客轨迹条数。写大＝本地存储占用上升（超过约 5MB 会被浏览器静默截断，那之后新轨迹全写不进去）；写小＝回看时更早丢历史",
			ValueType:   "int", Unit: "条", DefaultValue: "50", Min: strPtr("10"), Max: strPtr("2000"), Step: strPtr("10"), Restart: false},
		{Group: "frontend_ws", Key: "journey_queue_max", Name: "访客轨迹上报队列上限",
			Description: "journeyTracker 断网时本地堆积的上报事件条数。写大＝弱网/离线更久后恢复时能补发更多；写小＝离线期间的轨迹被更早丢弃（表现为访客行为路径缺一段）",
			ValueType:   "int", Unit: "条", DefaultValue: "100", Min: strPtr("10"), Max: strPtr("5000"), Step: strPtr("10"), Restart: false},
		{Group: "frontend_ws", Key: "journey_flush_interval_ms", Name: "访客轨迹刷报间隔",
			Description: "journeyTracker 定时把队列里的轨迹事件上报的间隔（毫秒）。默认 30s：写小＝上行请求频繁（移动端流量与电量）；写大＝页面关掉前的轨迹更容易没发出去",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "30000", Min: strPtr("1000"), Max: strPtr("300000"), Step: strPtr("1000"), Restart: false},

		// ── frontend_ui 组：前端视图里的阈值常量 ──────────────────────────
		{Group: "frontend_ui", Key: "approval_counts_poll_ms", Name: "审批待办轮询间隔",
			Description: "审批任务列表未读数轮询的间隔（毫秒）。默认 30s 是「值班换班时看得见新账的量级。写小＝审批接口被打频（列表页常开）；写大＝别人已经办完的事，这边还显示待处理」",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "30000", Min: strPtr("3000"), Max: strPtr("600000"), Step: strPtr("1000"), Restart: false},
		{Group: "frontend_ui", Key: "bad_case_stats_poll_ms", Name: "坏案例统计轮询间隔",
			Description: "坏案例列表统计卡片的轮询间隔（毫秒）。默认 30s。写小＝统计聚合查询被频打；写大＝坏案例新增/修复的反馈变慢",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "30000", Min: strPtr("3000"), Max: strPtr("600000"), Step: strPtr("1000"), Restart: false},
		{Group: "frontend_ui", Key: "browser_poll_failure_limit", Name: "浏览器监控连续失败上限",
			Description: "浏览器自动化监控页判定「任务异常前允许的连续轮询失败次数。写大＝偶发的网络抖动不误报，但真故障要更久才标红；写小＝一次超时就判异常，监控页会出现假警报」",
			ValueType:   "int", Unit: "次", DefaultValue: "3", Min: strPtr("1"), Max: strPtr("50"), Step: strPtr("1"), Restart: false},
		{Group: "frontend_ui", Key: "deep_link_scan_page_size", Name: "深链扫描每页条数",
			Description: "客户会话列表做深链扫描时每页拉取的条数。这是把 N 次请求压成 N/limit 次的旋钮：写大＝请求数少但单页体大；写小＝页数多，接口总耗时上升",
			ValueType:   "int", Unit: "条", DefaultValue: "500", Min: strPtr("50"), Max: strPtr("5000"), Step: strPtr("50"), Restart: false},
		{Group: "frontend_ui", Key: "material_max_size_mb", Name: "素材库单文件上限（MB）",
			Description: "素材库上传单文件的大小上限（MB）。前端弹窗的提示与本地拦截（MaterialLibrary.vue 的 beforeUpload）和素材库的后端拦截" +
				"（internal/content/controller/material.go 的 materialMaxUploadSize）读同一条，报错文案里的数字跟着它走。" +
				"这条以前只有前端读：后端那句写死的 10MB 不认参数，于是运维调到 20MB 后前端放行、后端拦死，用户只看到一条没有解释的 400。" +
				"边界：它与通用上传口用的 misc.upload_max_size_mb 是两条不同的链路（那条走 env > 参数 > 兼容列 > 10MB 兜底），互不影响；" +
				"本条的兜底固定 10MB，读不到库时与接线前的行为逐字一致",
			ValueType: "int", Unit: "MB", Risk: "danger", DefaultValue: "10", Min: strPtr("1"), Max: strPtr("512"), Step: strPtr("1"), Restart: false},
		{Group: "frontend_ui", Key: "websocket_reconnect_max_wait_ms", Name: "WS 重连最大等待（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】前端 WebSocket 重连退避的上限（毫秒）。默认 30s：等更久用户通常已离开页面，等更短则失去给后端的喘息时间。与 frontend_ws.max_reconnect_delay_ms 同源，保留两条是历史形态",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "30000", Min: strPtr("1000"), Max: strPtr("300000"), Step: strPtr("1000"), Restart: false,
			Deprecated: true, DeprecatedReason: "同义重复：坐席/聊天两个 WS 客户端的退避上限都由 frontend_ws.max_reconnect_delay_ms 提供（utils/agentSocket.js、utils/chatSocket.js 各一处 rtDur 读取），本条在前端全树零读取点，页面上改了不生效。要恢复这个旋钮，先让某一类 socket 真的读它，再把另一条合并过来——不要留着两条都能改、只有一条生效的形态"},
		{Group: "frontend_ui", Key: "toast_duration_ms", Name: "全局提示停留时长（已下架）",
			Description: "【已下架：改了不生效，改动期间请勿依赖】全局 toast/消息提示的停留时长（毫秒）。默认 3000：短了用户来不及读完（尤其是较长的错误文案），长了会挡住下面的操作区",
			ValueType:   "int", Unit: "毫秒", DefaultValue: "3000", Min: strPtr("1000"), Max: strPtr("30000"), Step: strPtr("500"), Restart: false,
			Deprecated: true, DeprecatedReason: "前端没有统一的提示出口：全树 196 个文件各自调 ElMessage，只有 3 处显式传 duration，Element Plus 也没有「消息默认时长」的全局配置项可挂。本条零读取点，页面上改了不生效。要接上得先在 utils/ 里收敛一个提示封装（含 ElMessage.success/error/warning 三形）并把 196 处调用改走它，再登记回本条——这是一次前端改造，不属于参数中心能单独完成的范围"},
		// ── G1 补登记：硬闸反向核对抓出的两条孤儿读取点 ──────────────────
		// 两条都在注释里写着「可经 X 调整」，但种子里没有这一条 ⇒ 代码每次读都 miss、
		// 每次都回落 fallback，运维在页面上看不到这个旋钮。这正是 1.8 在
		// channelgw/ws.go 上栽过的形状，只是那次的 key 写错、这次是压根没登记。
		{Group: "agent_llm", Key: "geo_pipeline_batch_per_run", Name: "Geo SEO 管线每轮词数",
			Description: "Geo 关键词管线每轮送 LLM 做 SEO（生成→评分→验证→探针）的词数上限。写大＝单轮更快清零积压但单轮超时风险高（20 词约消耗 60 分钟预算的一半）；写小＝单轮更快返回但积压清得慢。0 或负值关闭该批次（表现为「SEO批次已关闭」）",
			ValueType:   "int", Unit: "词", DefaultValue: "20", Min: strPtr("0"), Max: strPtr("500"), Step: strPtr("1"), Restart: false},
		{Group: "bridge", Key: "sse_max_stream_duration", Name: "bridge SSE 单流最长时长",
			Description: "单个 bridge SSE 连接从建立到被强制关闭的最长时长（秒）。写大＝长连接存活更久（重连少），代价是服务端持有连接的句柄与缓冲占用更久；写小＝客户端需要更频繁地重连重放 backlog。默认 300＝代码兜底 5 分钟（bridge/sse.go 的 SSEDefaultMaxStreamDuration），与 15s 心跳配套：到期由客户端重连并重放 backlog。0 或负值回落兜底",
			ValueType:   "duration", DefaultValue: "300", Min: strPtr("60"), Max: strPtr("86400"), Step: strPtr("60"), Restart: false},

		// ── ratelimit 组：全局限流 + 访客双维限流（R2 用户端第一批）──────
		// 读取点在 internal/router（router.go / chat_routes.go），两处都 import service，
		// 走直连范式。都是**装配期读一次**（router.Setup 跑一次把值塞进限流器构造），
		// 故全标 Restart=true。
		// 注意：全局两条的默认值取的是 router.go:215 生产实际传入的 1000/20000，
		// 而不是 ratelimit.go DefaultRateLimitConfig 里的 10/100 —— 后者只是「无参调用
		// RateLimitMiddleware()」时的兜底（仅测试走这条路），生产恒被 router 显式覆盖。
		// 参数化锚在生产读取点上，故默认值必须等于生产值 1000/20000 才是行为零变更。
		{Group: "ratelimit", Key: "global_rps", Name: "全局限流 RPS",
			Description: "全局限流器每秒放行的请求数（按 IP/APIKey 令牌桶速率，Redis 模式换算成每分钟上限）。装配点 router.go 装配期读一次，改完需重启。写小会把正常流量挡在门外，写大等于放开。注意默认 1000 取自 router 生产值；ratelimit.go 的 DefaultRateLimitConfig(10) 仅测试无参调用时兜底，生产不走它",
			ValueType:   "int", Unit: "次/秒", Risk: "danger", DefaultValue: "1000", Min: strPtr("1"), Max: strPtr("1000000"), Step: strPtr("10"), Restart: true},
		{Group: "ratelimit", Key: "global_bucket", Name: "全局限流桶容量",
			Description: "全局限流器令牌桶容量（允许的瞬时突发量）。装配期读一次，改完需重启。写小＝突发被削平成 429，写大＝一次洪峰全放行。默认 20000 取自 router 生产值",
			ValueType:   "int", Risk: "danger", DefaultValue: "20000", Min: strPtr("1"), Max: strPtr("10000000"), Step: strPtr("100"), Restart: true},
		{Group: "ratelimit", Key: "visitor_per_ip_rps", Name: "访客单 IP RPS",
			Description: "公开聊天接口（/chat/public）按来源 IP 限流的每秒请求数。装配期读一次，改完需重启。写小会误伤 NAT/办公室出口共用 IP 的多名正常访客，写大削弱防爬能力",
			ValueType:   "int", Unit: "次/秒", Risk: "warning", DefaultValue: "20", Min: strPtr("1"), Max: strPtr("10000"), Step: strPtr("1"), Restart: true},
		{Group: "ratelimit", Key: "visitor_per_ip_bucket", Name: "访客单 IP 桶容量",
			Description: "访客单 IP 令牌桶容量（允许的瞬时突发量）。装配期读一次，改完需重启。默认 40（2×RPS）",
			ValueType:   "int", Risk: "warning", DefaultValue: "40", Min: strPtr("1"), Max: strPtr("100000"), Step: strPtr("1"), Restart: true},
		{Group: "ratelimit", Key: "visitor_per_channel_rps", Name: "访客单渠道 RPS",
			Description: "公开聊天接口按渠道（channel app_key）限流的每秒请求数，防脚本滥用单个渠道。装配期读一次，改完需重启。写小会让热门渠道的正常并发被误伤",
			ValueType:   "int", Unit: "次/秒", Risk: "warning", DefaultValue: "100", Min: strPtr("1"), Max: strPtr("100000"), Step: strPtr("1"), Restart: true},
		{Group: "ratelimit", Key: "visitor_per_channel_bucket", Name: "访客单渠道桶容量",
			Description: "访客单渠道令牌桶容量（允许的瞬时突发量）。装配期读一次，改完需重启。默认 200（2×RPS）",
			ValueType:   "int", Risk: "warning", DefaultValue: "200", Min: strPtr("1"), Max: strPtr("1000000"), Step: strPtr("10"), Restart: true},
		// 路径级配额：每请求热读（参数中心 60s 内存缓存），改完不重启即生效——
		// 与上面六条装配期定型的键不同，故不标 Restart。
		// JSON 形状 {path: {rps, bucket_size}}，解析失败/结构非法回落代码默认表
		// （middleware.DefaultPathQuota），默认值与那张表逐字节同源。
		{Group: "ratelimit", Key: "path_quotas", Name: "路径级限流配额（整份 JSON）",
			Description: "按路径覆写限流配额的整份 JSON：{\"/api/path\": {\"rps\": 每秒请求数, \"bucket_size\": 令牌桶容量}}。每客户端（IP）×路径独立计数——一个客户端打满不影响其他客户端；路径必须以 / 开头且 rps/bucket_size 为正，非法条目单条丢弃并打 Warn；整份为空或解析失败回落内置默认四条（external-orders-by-customer / ai-suggestions / trace-eval/trigger / reach）。每请求热读（参数中心 60s 缓存），改表 60s 内生效（已有桶在参数变化时按新值重建），未列入配额表的路径不受此限流影响",
			ValueType:   "json", Risk: "danger", DefaultValue: `{"/api/integrations/external-orders-by-customer":{"rps":5,"bucket_size":20},"/api/ai-suggestions":{"rps":3,"bucket_size":10},"/api/monitor/trace-eval/trigger":{"rps":1,"bucket_size":5},"/api/reach":{"rps":50,"bucket_size":500}}`, Category: "限流"},

		// ── 免打扰窗口：两条独立管道、四条键，禁止合并──────────────
		// reach 组管「全渠道触达管线」的静默时段（原 reach_send_pipeline_quiethours.go
		// 硬编码 22-8）；webhook 组管「AI 回复」的免打扰（原 webhook_outbound.go 硬编码 23-7）。
		// 两处是不同管道、允许不同值，故拆成两组键，Description 写明分工。
		// 形态取分键 int + 跨键校验：每键 0-23 范围校验，越界回落代码默认；每请求读
		// （参数中心 60s TTL 缓存），改完下一条消息生效，故 Restart=false。
		// nextDayFirstSend 恒等于 end（同一参数），不单独设键。
		{Group: "reach", Key: "quiet_hours_start", Name: "触达免打扰开始小时",
			Description: "全渠道触达管线（R-4 静默时段）每天进入免打扰的起始小时（CST，0-23）。scope=全局（config_params 无租户维）。默认 22 与接线前硬编码一致。越界（<0 或 >23）回落代码默认 22。与 webhook.ai_quiet_hours_* 是两条不同管道，禁止合并",
			ValueType:   "int", Unit: "点", Risk: "warning", DefaultValue: "22", Min: strPtr("0"), Max: strPtr("23"), Restart: false},
		{Group: "reach", Key: "quiet_hours_end", Name: "触达免打扰结束小时",
			Description: "全渠道触达管线免打扰窗口的结束小时（CST，0-23），同时是次日首发小时（nextDayFirstSend 恒等于它）。scope=全局。默认 8 与接线前硬编码一致。start>end 表示跨午夜窗口（如 22-8）。越界回落代码默认 8",
			ValueType:   "int", Unit: "点", Risk: "warning", DefaultValue: "8", Min: strPtr("0"), Max: strPtr("23"), Restart: false},
		{Group: "webhook", Key: "ai_quiet_hours_start", Name: "AI 回复免打扰开始小时",
			Description: "AI 回复出站（webhook_outbound）每天免打扰的起始小时（CST，0-23），命中后回复延迟到次日 end 点投递。scope=全局。默认 23 与接线前硬编码一致。env DISABLE_AI_QUIET_HOURS 仍可整体关掉免打扰。与 reach.quiet_hours_* 是两条不同管道，禁止合并",
			ValueType:   "int", Unit: "点", Risk: "warning", DefaultValue: "23", Min: strPtr("0"), Max: strPtr("23"), Restart: false},
		{Group: "webhook", Key: "ai_quiet_hours_end", Name: "AI 回复免打扰结束小时",
			Description: "AI 回复出站免打扰窗口的结束小时（CST，0-23），即重投/首发顺延到的小时。scope=全局。默认 7 与接线前硬编码一致。start>end 表示跨午夜窗口（如 23-7）。越界回落代码默认 7",
			ValueType:   "int", Unit: "点", Risk: "warning", DefaultValue: "7", Min: strPtr("0"), Max: strPtr("23"), Restart: false},

		// ── urls 组：对外链接四件（R2 / R5-3）────────────────────────
		// 都是 env > 参数 > 代码常量 的三级读取（env 保留为应急）。读取口走参数中心
		// 60s TTL 缓存（GetString 经 getString），不会每请求打库。
		// R5-3 起读取口透传请求 ctx：SaaS 模式绑定租户后读到本租户覆盖值，
		// 未绑定租户（启动期 / 平台级调用）回落平台默认值。
		// frontend_url 读取点在 service/password_reset.go（直连）；cloud_url /
		// geo_site_base_url / public_base_url 读取点在 internal/config（config 不能
		// 反向 import service，走 provider 注入，装配点 app/url_params_wiring.go）。
		{Group: "urls", Key: "frontend_url", Name: "前端站点基址",
			Description: "密码重置邮件里拼接 reset 链接用的前端站点基址。优先级 env FRONTEND_URL > 本参数 > 代码兜底 http://localhost:3000。SaaS 模式按租户覆盖（读取点透传重置用户所属租户 ctx；未绑定读平台默认）。读取点 service/password_reset.go 的 getFrontendBaseURL（发信时读，非高频）。默认与接线前代码兜底一致",
			ValueType:   "string", DefaultValue: "http://localhost:3000", Restart: false},
		{Group: "urls", Key: "cloud_url", Name: "云端版对外地址",
			Description: "public config 里展示给前端的云端版对外地址。优先级 平台配置 cloud_url > env PLATFORM_CLOUD_URL > 本参数 > 代码常量 https://work.xapptool.cn。SaaS 模式按租户覆盖（读取点透传请求 ctx；未绑定读平台默认）。读取点 internal/config/platform.go 的 CloudURLFor（provider 注入，60s TTL 缓存）。默认与代码常量一致；参数缺省（空）时回落常量，行为零变更",
			ValueType:   "string", DefaultValue: "https://work.xapptool.cn", Restart: false},
		{Group: "urls", Key: "geo_site_base_url", Name: "官网基址",
			Description: "官网基址（拼接站内路径、判断是否本站 URL）。优先级 env GEO_SITE_BASE_URL > 本参数 > 代码常量 https://xiaofang142.github.io/hivemtk。SaaS 模式按租户覆盖（读取点透传请求 ctx；未绑定读平台默认）。读取点 internal/config/website.go 的 WebsiteBaseURLFor（provider 注入，60s TTL 缓存）。默认与代码常量一致；参数缺省（空）时回落常量，行为零变更",
			ValueType:   "string", DefaultValue: "https://xiaofang142.github.io/hivemtk", Restart: false},
		{Group: "urls", Key: "public_base_url", Name: "公网回调基址",
			Description: "渠道 webhook 回调与短链拼绝对地址用的公网基址。优先级 env PUBLIC_BASE_URL > config.yaml external.public_base_url > 本参数 > 空（调用方回退请求 Host）。SaaS 模式按租户覆盖（读取点透传请求 ctx；未绑定读平台默认）。平台默认留空＝沿用部署配置，行为零变更。读取点 internal/config/server.go 的 GetPublicBaseURLFor（provider 注入，60s TTL 缓存）",
			ValueType:   "string", DefaultValue: "", Restart: false},
		// —— platform 组：平台端通信地址（部署级，不按租户分叉）
		//
		// 与 urls.* 组的方向差异是刻意的：urls.* 是 env 优先（env 是部署契约），
		// 本参数是**参数中心优先** —— 它的存在意义就是"应对平台端迁移"，
		// 用户端改地址必须能压住部署时留在 .env 里的旧值，否则这条参数改了不生效。
		//
		// PLATFORM_ENABLED 仍是 env 总开关，不进参数中心：那是"要不要出网"，
		// 本参数是"出网去哪"，两件事混在一起会让"关闭平台"变成一个需要改库的动作。
		//
		// 默认值留空是刻意的：空 = "没有平台可连"，与关态语义一致；
		// 回落一个具体域名只会让请求打向黑洞、把日志刷成 Error，换不来任何功能。
		{Group: "platform", Key: "api_host", Name: "平台端 API 地址",
			Description: "平台端（platform-server）的对外 API 地址，用户端所有平台能力（商户注册/心跳/资产市场/模块订阅/订单/LLM 钱包/站内信）的出站目标。优先级 本参数 > config/platform.yaml 的 api_url > env PLATFORM_API_HOST/PLATFORM_API_URL > 空串。**参数中心优先于 env 是刻意的**：本参数的存在意义就是应对平台端迁移，改完必须在管理端生效，不能被 .env 里的旧值压住。平台级参数（不按租户分叉，见 config_param_tenant_scope.go 的 platformScopedParams）。读取点 internal/config/platform.go 的 PlatformURLFor（provider 注入，60s TTL 缓存），改完约 60 秒内生效、无需重启。PLATFORM_ENABLED 未开启时本参数不生效（关态地址恒为空串）。",
			ValueType:   "string", DefaultValue: "", Restart: false, Risk: "danger"},
		// —— content_guard 组：内容安全闸（异步审查流水线）
		//
		// 只收"运营真的可能按内容规模调"的那一把：审查长度上限。流水线的并发/批量/
		// 轮询/重试次数留在 env（装配期读一次），因为它们是部署规模参数而非内容口径，
		// 与 worker 数量一起随机器规格走，放进按租户可覆盖的参数中心只会让
		// "同一台进程里不同租户用不同批大小"变成可能，而批大小是进程级资源。
		{Group: "content_guard", Key: "max_scan_runes", Name: "单条内容审查长度上限",
			Description: "单条内容送审时的截断上限（rune）。超过它的部分不参与词库匹配，记录里 Truncated=true，默认按「内容超长」转人工（不判 pass）。兜底常量 internal/contentguard/service/engine.go 的 MaxScanRunes=20000，可用区间是它的 [1/20, ×10]。调大＝长文（商户长文案/KB 文档）被完整审，代价是单条扫描更久（匹配随长度线性增长，且它是防 DoS 的上界，不是越大越好）。读取点 internal/app/content_guard_wiring.go 的 contentGuardMaxScanRunes，**装配期读一次 ⇒ 改完要重启 user-server**；越界一律回落到兜底值并打 warn",
			ValueType:   "int", Unit: "字符", DefaultValue: "20000", Min: strPtr("1000"), Max: strPtr("200000"), Step: strPtr("1000"), Restart: true},
	}
}
