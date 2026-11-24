package capregistry

// M01 §7.3.5 的商户流程声明，独立于渠道运行诊断。版本标识本地职责契约。
var merchantWorkflowCells = []CellDefinition{
	{Product: "jd_pop", Region: "CN", Action: "aftersale.write", Carrier: ModeOfficialUI, ExecutionMode: ModeOfficialUI,
		ContractVersion: "m01-workflow-v1", DocumentationVersion: "m01-7.3.5", StateLimits: []string{"approved_scope_required", "native_action_available"}},
	{Product: "vipshop", Region: "CN", Action: "refund.observe", Carrier: ModePlatformManaged, ExecutionMode: ModePlatformManaged,
		ContractVersion: "m01-workflow-v1", DocumentationVersion: "m01-7.3.5", StateLimits: []string{"native_case_required"}},
}

// EC-19 (M19, 未实现 / 未 live 关闭，见 docs/未来任务清单.md §7.1.8、§7.3.5 与 §7.4):
// 以下 §7.3.5 目标流程在本仓库内尚无可用的动作级/工作流声明与执行闭环（仅设计/缺口记录）：
//   - Amazon 自履约/平台托管（FBA 等）：退款与取消、平台托管与卖家操作不得混同；需按 §7.3.5 区分「观察/平台原生」与「卖家侧」流程、角色步骤证据超期升级。当前无 amazon/amazon_fba 商务 cells、无订单改单/售后执行适配。
//   - 1688 / 速卖通（AliExpress）：卖家/官方工作台执行流程缺失；1688 卖家视角不得复用买家查询等约束须在实现中落地。
//   - Etsy：官方 Shop Manager 退款向导、取消另建动作等未实现（见 M19 设计要点）；无 etsy commerce cells。
//   - 得物（Poizon/DeWu）：官方工作台流程缺失。
//   - 唯品会（Vipshop）：merchantWorkflowCells 中仅有 refund.observe（platform_managed）声明，表示平台托管观察类记录，
//     与「可使用的案件流程、指定角色/步骤/证据/超期升级」的完整 M19 交付不等价；即时零售分业态、退款与取消的完整闭环仍缺。
//   - 苏宁官方工作台：缺失。
//   - 即时零售（分业态，如部分退款/整单取消/配送中协商、医药专员等）：缺失。
// 完成标准（M19）：§7.3.5 每行有可使用的案件流程、指定角色/步骤/证据/超期升级；退款与取消、平台托管与卖家操作不混同。
// 本注释仅作代码内事实锚点，不构成功能完成证据。
//
// EC-29 / EC-30 (M29/M30, 未实现 / 未 live 关闭，见 docs/未来任务清单.md §7.1.8、§7.2.6 A14、§7.3.5):
// Amazon 买家消息不是通用 IM / 营销出口，不得进入营销封装或营销模板：
//   - Amazon 无 DM send/receive 渠道登记（本文件 init() 中无 "amazon" 条目），因此 reach_send_pipeline
//     与 marketing_flow 的 email/sms 发送器不得被当作 Amazon 买家消息出口使用；
//   - Amazon 买家消息仅按 getMessagingActionsForOrder 返回的**规定动作**处理，必须保留平台证据
//     （action 名、订单/消息 ID、平台受理回执）；
//   - FBA 由 Amazon 处理退货与资金：系统只做观察与解释，不创建 refund/return 写入；
//   - 卖家自履约按 Manage Returns 处理并记录 RMA/退款，原生自动回复责任与本系统/AI/人工三者
//     必须有唯一回复责任切换记录，不能互相覆盖；
//   - 配置 API（Messaging Automations v2026-04-20）只证明配置契约，不产生任意营销权限，
//     也不能用 Important 绕过买家选择；
//   - 营销促销禁止：Amazon 出口不得套用营销模板、频控、优惠权益或主动获客流程。
//
// EC-30 边界（平台原生营销配置 vs 现有营销链路，消息派发复用 23-A，不复制营销管线）：
//   - 「平台原生营销」（含 Amazon 原生自动回复/规定动作、及其他平台原生营销配置）不得直接
//     操作 HiveMtk 消息派发（23-A 统一发送闸门/ProactiveReachService），也不得绕过 DNC、
//     审批（W-1）、合规（E1）与退订判定链；
//   - HiveMtk 营销链路（marketing_flow / ReachByCustomer / 旅程与分群）不得封装/转译平台
//     原生营销配置为营销模板、频控、优惠权益或主动获客流程，不得把平台原生营销动作当作
//     HiveMtk 营销出口；
//   - 平台原生营销需要消息触达时，只能经 23-A 统一发送入口并在消息内容/责任上保留平台
//     证据，不得新增第二条营销管线或旁路 23-A；
//   - HiveMtk 营销链路如需复用平台能力，仅可作为「观察/解释/人工接续」，不得把平台原生
//     营销的配置/状态当作 HiveMtk 营销的执行结果或验收证据。
// 本注释仅作代码内事实锚点，不构成功能完成证据。

// channels.go 渠道能力登记（单一事实源）。
// 新增渠道/能力：在此登记一行 + 对应实现，矩阵文档由测试重新生成。
func init() {
	// 网页私聊渠道（蜂桥扩展桥接）
	Register(Entry{Platform: "douyin", Cap: CapDMReceive, Via: "bridge", Note: "蜂桥扩展巡检上报；自声/去重由服务端 inbox_ingress 兜底"})
	Register(Entry{Platform: "douyin", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发，扩展回写输入框发送（会话漂移守卫拒误发）", Cells: []CellDefinition{
		{Product: "douyin", Region: "CN", Action: "message.text.send", Carrier: "bridge", ExecutionMode: ModeBridge, ContractVersion: "bridge-v1", RequiredScopes: []string{"message.text.send"}, DocumentationVersion: "design-cell-v1", Experimental: true},
		{Product: "douyin", Region: "CN", Action: "message.image.send", Carrier: "bridge", ExecutionMode: ModeBridge, ContractVersion: "bridge-v1", RequiredScopes: []string{"message.image.send"}, DocumentationVersion: "design-cell-v1", Experimental: true},
	}})
	Register(Entry{Platform: "douyin", Cap: CapLeadMine, Via: "unified-miner", Note: "关键词打分 + LLM 复核（lead.llm_refine_enabled）"})
	Register(Entry{Platform: "xiaohongshu", Cap: CapDMReceive, Via: "bridge", Note: "同抖音桥接链路"})
	Register(Entry{Platform: "xiaohongshu", Cap: CapDMSend, Via: "bridge", Note: "同抖音桥接链路"})
	Register(Entry{Platform: "xiaohongshu", Cap: CapLeadMine, Via: "unified-miner", Note: ""})
	Register(Entry{Platform: "kuaishou", Cap: CapDMReceive, Via: "bridge", Note: ""})
	Register(Entry{Platform: "kuaishou", Cap: CapDMSend, Via: "bridge", Note: ""})
	Register(Entry{Platform: "tiktok", Cap: CapDMReceive, Via: "bridge", Note: "海外链路"})
	Register(Entry{Platform: "tiktok", Cap: CapDMSend, Via: "bridge", Note: "海外链路"})
	Register(Entry{Platform: "xianyu", Cap: CapDMReceive, Via: "bridge", Note: ""})
	Register(Entry{Platform: "xianyu", Cap: CapDMSend, Via: "bridge", Note: ""})
	// 2026-11 六渠道统一接待：京东/B站/微博为网页私信桥（选择器待真机校准）；
	// personal_wx 为桌面伴生协议（服务端已就绪，伴生为外部工程）。
	Register(Entry{Platform: "jd", Cap: CapDMReceive, Via: "bridge", Note: "京麦/咚咚网页私信；Experimental，选择器待真机"})
	Register(Entry{Platform: "jd", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental"})
	Register(Entry{Platform: "bilibili", Cap: CapDMReceive, Via: "bridge", Note: "message.bilibili.com；Experimental"})
	Register(Entry{Platform: "bilibili", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental"})
	Register(Entry{Platform: "weibo", Cap: CapDMReceive, Via: "bridge", Note: "weibo.com/chat；Experimental"})
	Register(Entry{Platform: "weibo", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental"})
	Register(Entry{Platform: "personal_wx", Cap: CapDMReceive, Via: "bridge", Note: "桌面伴生上报；伴生未交付（外部工程）"})
	Register(Entry{Platform: "personal_wx", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；伴生未交付（外部工程）"})
	// CH-16 首切片（2026-11-09，二次审核补齐）：拼多多商家网页私信，桥接收发链路与
	// 既有网页渠道同源；Experimental（选择器为 X 级历史源码实证，本方真机未校准）。
	// EC-16 (M16): commerce 动作格（订单改单/售后/退款 api_adapter cells）未登记；当前仅消息桥接骨架。
	Register(Entry{Platform: "pdd", Cap: CapDMReceive, Via: "bridge", Note: "拼多多商家网页（mms.pinduoduo.com/chat-merchant）；Experimental，选择器待真机；EC-16 commerce cells 未登记"})
	Register(Entry{Platform: "pdd", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental；EC-16 commerce cells 未登记"})

	// 2026-11-18 电商店铺五渠道骨架（CH-15/18/19/28/35，二次审核补齐）：桥接收发链路
	// 与既有网页渠道同源；fail-closed（SEL 零证据拒启拒发），真机校准后开闸。
	// EC-16 (M16): "淘宝/天猫、PDD、抖店"为核心扩展目标；本表中 taobao/pdd 仅桥接消息能力，尚无
	// commerce 动作格（order.address.change/aftersale.write/refund.write）；douyin_shop 已有上述
	// api_adapter 实验格但仍未形成完整闭环（见 order_change_request / aftersale 服务注释）。不代表已达 M16 验收。
	Register(Entry{Platform: "taobao", Cap: CapDMReceive, Via: "bridge", Note: "千牛网页接待中心（work.taobao.com）；Experimental，选择器待真机；EC-16 commerce cells 未登记"})
	Register(Entry{Platform: "taobao", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental；EC-16 commerce cells 未登记"})
	Register(Entry{Platform: "douyin_shop", Cap: CapDMReceive, Via: "bridge", Note: "抖店飞鸽客服（im.jinritemai.com）；Experimental，选择器待真机"})
	Register(Entry{Platform: "douyin_shop", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental", Cells: []CellDefinition{
		{Product: "douyin_shop", Region: "CN", Action: "message.text.send", Carrier: "bridge", ExecutionMode: ModeBridge, ContractVersion: "bridge-v1", RequiredScopes: []string{"message.text.send"}, DocumentationVersion: "design-cell-v1", Experimental: true},
		{Product: "douyin_shop", Region: "CN", Action: "message.image.send", Carrier: "bridge", ExecutionMode: ModeBridge, ContractVersion: "bridge-v1", RequiredScopes: []string{"message.image.send"}, DocumentationVersion: "design-cell-v1", Experimental: true},
		{Product: "douyin_shop", Region: "CN", Action: "order.address.change", Carrier: "api_adapter", ExecutionMode: ModeAPIAdapter, ContractVersion: "api-v1", RequiredScopes: []string{"order.address.change"}, StateLimits: []string{"platform_preflight_required"}, DocumentationVersion: "design-cell-v1", Experimental: true},
		{Product: "douyin_shop", Region: "CN", Action: "aftersale.write", Carrier: "api_adapter", ExecutionMode: ModeAPIAdapter, ContractVersion: "api-v1", RequiredScopes: []string{"aftersale.write"}, StateLimits: []string{"platform_preflight_required"}, DocumentationVersion: "design-cell-v1", Experimental: true},
		{Product: "douyin_shop", Region: "CN", Action: "refund.write", Carrier: "api_adapter", ExecutionMode: ModeAPIAdapter, ContractVersion: "api-v1", RequiredScopes: []string{"refund.write"}, StateLimits: []string{"platform_preflight_required"}, DocumentationVersion: "design-cell-v1", Experimental: true},
	}})
	Register(Entry{Platform: "kuaishou_shop", Cap: CapDMReceive, Via: "bridge", Note: "快手小店快语客服（im.kwaixiaodian.com）；Experimental，选择器待真机"})
	Register(Entry{Platform: "kuaishou_shop", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental"})
	Register(Entry{Platform: "shopee", Cap: CapDMReceive, Via: "bridge", Note: "Shopee Seller Centre Webchat；Experimental，选择器待真机"})
	Register(Entry{Platform: "shopee", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental"})
	Register(Entry{Platform: "walmart_us", Cap: CapDMReceive, Via: "bridge", Note: "Walmart Seller Center Inbox；Experimental，选择器待真机"})
	Register(Entry{Platform: "walmart_us", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental"})

	// 官方 API/webhook 渠道
	Register(Entry{Platform: "wechat", Cap: CapWebhook, Via: "webhook", Note: "公众号回调验签（/api/webhook/wechat/:account_id）"})
	Register(Entry{Platform: "wechat", Cap: CapDMSend, Via: "api", Note: "客服消息；触达侧渠道码 weixin 同源"})

	// 官方 API/webhook 渠道
	Register(Entry{Platform: "wecom", Cap: CapWebhook, Via: "webhook", Note: "XML 信封验签（msg_signature 恒时比较 + 时间窗）"})
	Register(Entry{Platform: "wecom", Cap: CapDMSend, Via: "api", Note: ""})
	Register(Entry{Platform: "telegram", Cap: CapWebhook, Via: "webhook", Note: "PUBLIC_BASE_URL 配置时 webhook，否则 polling 单实例"})
	Register(Entry{Platform: "telegram", Cap: CapDMSend, Via: "api", Note: ""})
	Register(Entry{Platform: "feishu", Cap: CapWebhook, Via: "webhook", Note: ""})
	Register(Entry{Platform: "whatsapp", Cap: CapWebhook, Via: "webhook", Note: ""})

	// 025 平台批（2026-11-18）：企业微信客服 / OneBot v11 / Slack / 企微群机器人 / 自定义 webhook
	Register(Entry{Platform: "wecomcs", Cap: CapWebhook, Via: "webhook", Note: "回调验签 + sync_msg 游标增量拉取"})
	Register(Entry{Platform: "wecomcs", Cap: CapDMSend, Via: "api", Note: "send_msg；48 小时会话窗口外拒发（window_expired）"})
	Register(Entry{Platform: "onebot", Cap: CapWebhook, Via: "webhook", Note: "HTTP 上报 + 反向 WS；Bearer token 校验"})
	Register(Entry{Platform: "onebot", Cap: CapDMSend, Via: "ws", Note: "出站优先反向 WS，未连接时回落 HTTP /send_private_msg|/send_group_msg"})
	Register(Entry{Platform: "slack", Cap: CapWebhook, Via: "webhook", Note: "Events API v0 签名 + 5 分钟时间窗"})
	Register(Entry{Platform: "slack", Cap: CapDMSend, Via: "api", Note: "chat.postMessage，回复沿用 thread_ts"})
	Register(Entry{Platform: "wecombot", Cap: CapWebhook, Via: "webhook", Note: "AES-CBC 解密 + 签名校验"})
	Register(Entry{Platform: "wecombot", Cap: CapDMSend, Via: "webhook", Note: "群机器人 webhook，markdown 载荷"})
	Register(Entry{Platform: "custom", Cap: CapWebhook, Via: "webhook", Note: "固定 JSON 契约 + X-Hive-Signature"})
	Register(Entry{Platform: "custom", Cap: CapDMSend, Via: "webhook", Note: "POST callback_url 同款签名；4xx 不重试、5xx/超时可重试"})

	// 触达
	Register(Entry{Platform: "sms", Cap: CapSMS, Via: "aliyun-sms", Note: "需后台配置 AccessKey/签名/模板"})
	Register(Entry{Platform: "email", Cap: CapEmail, Via: "smtp", Note: "需后台配置 SMTP 授权码"})

	// 知识库外部同步
	Register(Entry{Platform: "ima", Cap: CapKBSync, Via: "kbconnector", Note: "腾讯 IMA OpenAPI；笔记类平台不提供导出，仅文件类"})
	Register(Entry{Platform: "webhttp", Cap: CapKBSync, Via: "kbconnector", Note: "通用 HTTP 清单端点"})
}
