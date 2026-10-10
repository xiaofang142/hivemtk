package capregistry

// channels.go 渠道能力登记（单一事实源）。
// 新增渠道/能力：在此登记一行 + 对应实现，矩阵文档由测试重新生成。
func init() {
	// 网页私聊渠道（蜂桥扩展桥接）
	Register(Entry{Platform: "douyin", Cap: CapDMReceive, Via: "bridge", Note: "蜂桥扩展巡检上报；自声/去重由服务端 inbox_ingress 兜底"})
	Register(Entry{Platform: "douyin", Cap: CapDMSend, Via: "bridge", Note: "outbox 下发，扩展回写输入框发送（会话漂移守卫拒误发）"})
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

	// 官方 API/webhook 渠道
	Register(Entry{Platform: "wecom", Cap: CapWebhook, Via: "webhook", Note: "XML 信封验签（msg_signature 恒时比较 + 时间窗）"})
	Register(Entry{Platform: "wecom", Cap: CapDMSend, Via: "api", Note: ""})
	Register(Entry{Platform: "telegram", Cap: CapWebhook, Via: "webhook", Note: "PUBLIC_BASE_URL 配置时 webhook，否则 polling 单实例"})
	Register(Entry{Platform: "telegram", Cap: CapDMSend, Via: "api", Note: ""})
	Register(Entry{Platform: "feishu", Cap: CapWebhook, Via: "webhook", Note: ""})
	Register(Entry{Platform: "whatsapp", Cap: CapWebhook, Via: "webhook", Note: ""})

	// 触达
	Register(Entry{Platform: "sms", Cap: CapSMS, Via: "aliyun-sms", Note: "需后台配置 AccessKey/签名/模板"})
	Register(Entry{Platform: "email", Cap: CapEmail, Via: "smtp", Note: "需后台配置 SMTP 授权码"})

	// 知识库外部同步
	Register(Entry{Platform: "ima", Cap: CapKBSync, Via: "kbconnector", Note: "腾讯 IMA OpenAPI；笔记类平台不提供导出，仅文件类"})
	Register(Entry{Platform: "webhttp", Cap: CapKBSync, Via: "kbconnector", Note: "通用 HTTP 清单端点"})
}
