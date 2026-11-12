package service

// ParamGroupDef 参数分组的展示元数据（中文名 / 说明 / 排序）。
//
// 为什么从前端搬到这里：ConfigParams.vue 里曾硬编码一份 GROUP_LABELS（29 组中文名）。
// 那份清单和 config_params.param_group 是两份事实——新增一个组，前端不知道它叫什么，
// 页面上就显示英文 key（"inbox_sales"），像出了个 bug；改一次中文名要改前端、
// 改前端还要记得和后端组名对得上。
//
// 搬成数据之后还有一个附带收益：排序与说明跟着走。前端那份只能靠对象字面量的
// 书写顺序排组，改个顺序得动前端。
//
// 仍然保留在前端的只有「分组标签名 → 路由/图标」这类纯 UI 资源，与本表无关。
type ParamGroupDef struct {
	Group       string
	Label       string
	Description string
	SortOrder   int
}

// DefaultParamGroupDefs 返回全部分组定义（与种子里的组一一对应；条数以本函数
// 实际返回为准，历史注释曾停留在 35 与真实数量脱节过）。
//
// 与 DefaultParamDefs 的关系：本表管「组怎么显示」，那张表管「组里有哪些旋钮」。
// 两者都必须有对方提到的每一个组名 —— 少一边就是一处漂移，由
// TestParamGroupCoversEverySeedGroup / TestSeedGroupsAreAllDeclared 双向钉死。
func DefaultParamGroupDefs() []ParamGroupDef {
	return []ParamGroupDef{
		{Group: "agent_llm", Label: "AI 大模型", Description: "LLM 调用、供应商降级、熔断与健康检查", SortOrder: 10},
		// llm / rerank 组（2026-11-12）：§3.13 LLM/embedding/rerank 连接配置 env→DB
		// 迁移各带入一条 global_override 种子，组元数据此前漏登记——参数中心侧栏
		// 回落显示英文 key（TestParamGroupCoversEverySeedGroup 钉住）。
		{Group: "llm", Label: "全局 LLM 连接", Description: "全局 LLM API 连接覆盖（base_url/api_key/model，按租户整份 JSON）", SortOrder: 15},
		{Group: "agent_tool", Label: "AI 工具", Description: "工具调用并发、内容截断、失败换路与冷却", SortOrder: 20},
		{Group: "embedding", Label: "向量嵌入", Description: "Embedding 提供商与批量参数", SortOrder: 30},
		{Group: "rerank", Label: "全局 Rerank 连接", Description: "全局 Rerank API 连接覆盖（base_url/api_key/model，按租户整份 JSON）", SortOrder: 35},
		{Group: "cache", Label: "缓存策略", Description: "内存缓存容量与 TTL、FAQ 衰减批次", SortOrder: 40},
		{Group: "rag_health", Label: "RAG 健康度", Description: "RAG 质量分权重与低召回告警线", SortOrder: 45},
		{Group: "feedback", Label: "反馈闭环", Description: "对话反馈驱动的策略迭代与 bandit 分配", SortOrder: 46},
		{Group: "sop", Label: "SOP 执行", Description: "SOP 执行的并发、补偿与调度", SortOrder: 47},
		{Group: "domain_health", Label: "域名健康度", Description: "发送域名的健康分与熔断", SortOrder: 48},
		{Group: "frontend_ws", Label: "前端 WebSocket", Description: "浏览器端重连退避、心跳与批量 ACK（前端消费）", SortOrder: 285},
		{Group: "frontend_ui", Label: "前端界面", Description: "前端轮询节奏与上传体积校验（前端消费）", SortOrder: 286},
		{Group: "knowledge", Label: "知识库", Description: "检索 topK、相似度、切片与导入超时", SortOrder: 50},
		{Group: "memory", Label: "短期记忆", Description: "L1 记忆窗口与 TTL、L4 每客户条数", SortOrder: 60},
		{Group: "bridge", Label: "消息桥接", Description: "扩展侧接入的入站上限、SSE 与在线判定", SortOrder: 70},
		{Group: "sse", Label: "SSE 推送", Description: "服务端推送心跳、每 IP 连接数与写缓冲", SortOrder: 80},
		{Group: "inbox_sales", Label: "私信销售", Description: "线索判定阈值与预览采样", SortOrder: 90},
		{Group: "session", Label: "会话", Description: "会话活跃/空闲窗口与人工负载", SortOrder: 100},
		{Group: "lock", Label: "分布式锁", Description: "收件箱/消息中台的锁与幂等 TTL（正确性参数，改错会死锁或重复处理）", SortOrder: 110},
		{Group: "human_task", Label: "转人工任务", Description: "人工任务分配与交接 SLA", SortOrder: 120},
		{Group: "channelbot", Label: "渠道机器人", Description: "QQ / Telegram 的长度上限与 HTTP 超时", SortOrder: 130},
		{Group: "channelgw", Label: "渠道网关", Description: "渠道网关的连接与推送节奏", SortOrder: 140},
		{Group: "wecom", Label: "企业微信", Description: "账号健康分档、配额降级与接口超时", SortOrder: 150},
		// "wechat" 分组已随 D-7 删除（2026-10-23）：组内唯一的三个旋钮（chat_ws_*）
		// 是 /ws/chat 死代码的下架参数，随端点一起移除——UI 不再渲染空分组。
		{Group: "webhook", Label: "Webhook 回调", Description: "回调验签、去重、限流与重试", SortOrder: 170},
		{Group: "reach", Label: "触达频控", Description: "全管线日触达上限（防打扰）", SortOrder: 180},
		{Group: "sales", Label: "销售", Description: "销售洞察与客户身份合并", SortOrder: 190},
		{Group: "lead", Label: "线索画像", Description: "线索挖掘阈值与行业画像", SortOrder: 200},
		{Group: "permission", Label: "角色权限", Description: "角色权限矩阵（JSON）", SortOrder: 210},
		{Group: "confidence", Label: "置信度阈值", Description: "拟人化/去重/否决/重写阈值（0-1）", SortOrder: 220},
		{Group: "workflow", Label: "工作流", Description: "SOP 工作流步数、并发与子流程深度", SortOrder: 230},
		{Group: "telemetry", Label: "遥测", Description: "指标采集窗口、去重与开关轮询", SortOrder: 240},
		{Group: "geo", Label: "可见性统计", Description: "地域关键词与可见性统计窗口", SortOrder: 250},
		{Group: "pagination", Label: "分页", Description: "分页与游标分页每批条数", SortOrder: 260},
		{Group: "middleware", Label: "中间件", Description: "请求体上限、审计刷盘、MFA 宽限", SortOrder: 270},
		{Group: "ratelimit", Label: "限流", Description: "全局限流与访客双维限流的速率与桶容量", SortOrder: 272},
		{Group: "reporting", Label: "报表统计", Description: "报表与列表的默认时间窗口天数", SortOrder: 274},
		{Group: "misc", Label: "其它", Description: "跨域的零散旋钮（访客令牌、上传上限、心跳等）", SortOrder: 280},
		{Group: "urls", Label: "对外链接", Description: "前端/云端/官网基址（发信与展示用的对外 URL）", SortOrder: 282},
		{Group: "platform", Label: "平台端通信", Description: "平台端 API 地址（应对平台端迁移，平台级参数）", SortOrder: 283},
		// content_guard 组：内容安全闸（异步审查流水线）。SortOrder 放在 smart_cs 前面，
		// 与种子文件里「content_guard 组」的注释块位置对应。
		{Group: "content_guard", Label: "内容安全", Description: "内容审查流水线的长度上限与转人工口径", SortOrder: 284},
		{Group: "smart_cs", Label: "智能客服", Description: "智能客服编排的阈值与开关", SortOrder: 290},
		// retention 组：高写入自观测/审计表保留窗口（CC-04）。SortOrder 随尾巴递增，
		// 与种子文件里「retention 组」的注释块位置对应。
		{Group: "retention", Label: "日志保留", Description: "自观测与审计日志表的保留窗口（后台每 24h 一轮清理）", SortOrder: 291},
	}
}
