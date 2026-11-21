package capregistry

// M01 §7.3.5 的商户流程声明，独立于渠道运行诊断。版本标识本地职责契约。
var merchantWorkflowCells = []CellDefinition{
	{Product: "jd_pop", Region: "CN", Action: "aftersale.write", Carrier: ModeOfficialUI, ExecutionMode: ModeOfficialUI,
		ContractVersion: "m01-workflow-v1", DocumentationVersion: "m01-7.3.5", StateLimits: []string{"approved_scope_required", "native_action_available"}},
	{Product: "vipshop", Region: "CN", Action: "refund.observe", Carrier: ModePlatformManaged, ExecutionMode: ModePlatformManaged,
		ContractVersion: "m01-workflow-v1", DocumentationVersion: "m01-7.3.5", StateLimits: []string{"native_case_required"}},
}

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
	Register(Entry{Platform: "pdd", Cap: CapDMReceive, Via: "bridge", Note: "拼多多商家网页（mms.pinduoduo.com/chat-merchant）；Experimental，选择器待真机"})
	Register(Entry{Platform: "pdd", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental"})

	// 2026-11-18 电商店铺五渠道骨架（CH-15/18/19/28/35，二次审核补齐）：桥接收发链路
	// 与既有网页渠道同源；fail-closed（SEL 零证据拒启拒发），真机校准后开闸。
	Register(Entry{Platform: "taobao", Cap: CapDMReceive, Via: "bridge", Note: "千牛网页接待中心（work.taobao.com）；Experimental，选择器待真机"})
	Register(Entry{Platform: "taobao", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发；Experimental"})
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
