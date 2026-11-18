package channelcaps

import "hivemtk-user/internal/model"

// manifests.go 渠道消息级能力登记（单一事实源）。
// 新增渠道/消息形态：在此登记一行 + 在落点实现，然后 UPDATE_GOLDEN=1 重新生成
// docs/channel-capabilities.md。
func init() {
	// ── 官方 webhook / API 渠道 ─────────────────────────────────────────────
	Register(Manifest{
		Channel:  model.ChannelTelegram,
		Label:    "Telegram",
		Via:      "webhook",
		Inbound:  []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeLocation, model.MsgTypeCard},
		Outbound: []string{model.MsgTypeText, model.MsgTypeCard},
		Events: []string{
			model.EventTypeMessageReceived, model.EventTypeMessageEdited,
			model.EventTypeGroupMemberJoined, model.EventTypeGroupMemberLeft,
			model.EventTypeInteractionCallback,
		},
		// FeatureInteraction：callback_query 能 answerCallbackQuery（T210 已落地）。
		Features: []Feature{FeatureRichCard, FeatureGroup, FeatureMention, FeatureInteraction},
		Note:     "群聊 @ 提及走 TelegramReplyMeta；卡片为真载体（outboundSupportsRichCards）",
		// Telegram Bot API：sendMessage 的 parse_mode=HTML（群回复 @mention 是 <a> 标签）。
		OutboundFormat:     OutboundFormatHTML,
		MaxTextLen:         4096,
		FormatEvidence:     "webhook_outbound.buildTelegramGroupReply：ParseMode=HTML + 自身 HTML 转义",
		MaxAttachments:     10,
		MaxAttachmentBytes: 20 * 1024 * 1024,
		AttachmentEvidence: "Bot API sendMediaGroup 单组 1–10 个 media；getFile 下载 ≤20MB",
	})
	Register(Manifest{
		Channel:            model.ChannelWhatsApp,
		Label:              "WhatsApp Cloud",
		Via:                "webhook",
		Inbound:            []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeLocation},
		Outbound:           []string{model.MsgTypeText},
		Features:           []Feature{FeatureGroup},
		Note:               "24 小时窗口外用模板消息；出站当前只提交文字载荷",
		OutboundFormat:     OutboundFormatPlain,
		MaxTextLen:         4096,
		FormatEvidence:     "WhatsApp Cloud API text.body 为纯文本，4096 字上限",
		MaxAttachments:     1,
		MaxAttachmentBytes: 5 * 1024 * 1024,
		AttachmentEvidence: "Cloud API 每条消息 1 个 media；image 5MB / video 16MB / document 100MB（取最严 5MB）",
	})
	Register(Manifest{
		Channel:  model.ChannelFeishu,
		Label:    "飞书",
		Via:      "webhook",
		Inbound:  []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeCard, model.MsgTypeLocation},
		Outbound: []string{model.MsgTypeText, model.MsgTypeCard},
		// 撤回与成员变更事件走 dispatchFeishu 的平台事件分支（specs/024 T205）。
		Events: []string{
			model.EventTypeMessageReceived, model.EventTypeMessageDeleted,
			model.EventTypeGroupMemberJoined, model.EventTypeGroupMemberLeft,
		},
		Features:           []Feature{FeatureRichCard, FeatureGroup, FeatureMention},
		Note:               "卡片转 interactive JSON；媒体文件异步回填 media_url",
		OutboundFormat:     OutboundFormatPlain,
		FormatEvidence:     "feishu SendInteractiveCard 走 interactive 载体；text 消息为纯文本",
		MaxAttachments:     1,
		MaxAttachmentBytes: 10 * 1024 * 1024,
		AttachmentEvidence: "飞书消息图片 ≤10MB、文件 ≤30MB（取最严 10MB）；一条消息一个附件，媒体异步回填 media_url",
	})
	Register(Manifest{
		Channel:            model.ChannelDingTalk,
		Label:              "钉钉",
		Via:                "webhook",
		Inbound:            []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeLink},
		Outbound:           []string{model.MsgTypeText},
		Features:           []Feature{FeatureGroup, FeatureMention},
		Note:               "出站 webhook 域名受 allowlist 约束（loadDingtalkWebhookHostAllowed）",
		OutboundFormat:     OutboundFormatPlain,
		MaxTextLen:         20000,
		FormatEvidence:     "outbound_impl_dingtalk：`msgtype=text`，纯文本正文",
		MaxAttachments:     1,
		MaxAttachmentBytes: 20 * 1024 * 1024,
		AttachmentEvidence: "钉钉消息媒体 ≤20MB，一条消息一个 media；媒体异步回填 media_url",
	})
	Register(Manifest{
		Channel:            model.ChannelWeCom,
		Label:              "企业微信（自建应用）",
		Via:                "webhook",
		Inbound:            []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeLocation},
		Outbound:           []string{model.MsgTypeText},
		Features:           []Feature{FeatureGroup, FeatureMention},
		Note:               "XML 信封 msg_signature 恒时比较 + 时间窗验签",
		OutboundFormat:     OutboundFormatPlain,
		MaxTextLen:         2048,
		FormatEvidence:     "outbound_impl_wecom：MsgType=`text`，企微 text 消息为纯文本",
		MaxAttachments:     1,
		MaxAttachmentBytes: 20 * 1024 * 1024,
		AttachmentEvidence: "企微应用消息 media 素材 ≤20MB；一条消息一个 media_id",
	})
	Register(Manifest{
		Channel:            "wechat",
		Label:              "微信公众号",
		Via:                "webhook",
		Inbound:            []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeLocation},
		Outbound:           []string{model.MsgTypeText},
		Note:               "客服消息通道；公众号无群聊能力",
		OutboundFormat:     OutboundFormatPlain,
		MaxTextLen:         2048,
		FormatEvidence:     "outbound_impl_wechat：SendCustomMessage(msgtype=text)",
		MaxAttachments:     1,
		MaxAttachmentBytes: 10 * 1024 * 1024,
		AttachmentEvidence: "公众号客服消息 media_id ≤10MB；一条消息一个媒体",
	})
	Register(Manifest{
		Channel:  model.ChannelQQ,
		Label:    "QQ 官方机器人",
		Via:      "webhook",
		Inbound:  []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeCard},
		Outbound: []string{model.MsgTypeText},
		// 机器人入群/退群（GROUP_ADD_ROBOT / GROUP_DEL_ROBOT）决定群还能不能收消息。
		Events:             []string{model.EventTypeMessageReceived, model.EventTypeBotInvited, model.EventTypeBotRemoved},
		Features:           []Feature{FeatureGroup, FeatureMention},
		Note:               "bot.q.qq.com；Ed25519 验签，被动回复带 channel_msg_id",
		OutboundFormat:     OutboundFormatPlain,
		MaxTextLen:         2000,
		FormatEvidence:     "channelbot/qq splitQQMessage 自行按官方上限切分，渲染层不再切",
		MaxAttachments:     1,
		MaxAttachmentBytes: 0,
		AttachmentEvidence: "官方机器人媒体以 URL 下发，无统一字节上限；服务端落单值 media_url",
	})

	// ── 025 平台批：企业微信客服 / OneBot / Slack / 企微群机器人 ─────────
	Register(Manifest{
		Channel:  model.ChannelWeComCS,
		Label:    "企业微信客服",
		Via:      "webhook",
		Inbound:  []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeFile, model.MsgTypeLink, model.MsgTypeLocation, model.MsgTypeCard},
		Outbound: []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeLink, model.MsgTypeCard},
		// 回调只负责通知「有新消息」，真正的消息由 sync_msg 增量拉取；非消息事件写 channel_event_audits，不进事件总线。
		Events:         []string{model.EventTypeMessageReceived},
		Features:       []Feature{FeatureMention},
		Note:           "回调验签后按 cursor 增量拉取；send_msg 受 48 小时会话窗口约束（95021/95026/95019）",
		OutboundFormat: OutboundFormatPlain,
		MaxTextLen:     2048,
		FormatEvidence: "channelbot/wecomcs SendMsg(msgtype=text)：企微 text.content ≤2048 字节",
		MaxAttachments: 1, MaxAttachmentBytes: 10 * 1024 * 1024,
		AttachmentEvidence: "微信客服 send_msg image 走临时素材 media_id，图片 ≤10MB；一条消息一个 media_id",
	})
	Register(Manifest{
		Channel:  model.ChannelOneBot,
		Label:    "OneBot v11",
		Via:      "webhook",
		Inbound:  []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeFile},
		Outbound: []string{model.MsgTypeText},
		Events: []string{
			model.EventTypeMessageReceived, model.EventTypeMessageDeleted,
			model.EventTypeGroupMemberJoined, model.EventTypeGroupMemberLeft,
		},
		Features:       []Feature{FeatureGroup, FeatureMention},
		Note:           "HTTP 上报 + 反向 WS 双通道；出站优先 WS 回落 HTTP；request 事件只入总线不自动同意",
		OutboundFormat: OutboundFormatPlain,
		MaxTextLen:     2000,
		FormatEvidence: "channelbot/onebot SplitText 默认 2000 字优先换行切块（CQ 码另计）",
		MaxAttachments: 1, MaxAttachmentBytes: 0,
		AttachmentEvidence: "媒体段只透传 URL，服务端不下载，无统一字节上限；单条消息落单值 media_url",
	})
	Register(Manifest{
		Channel:  model.ChannelSlack,
		Label:    "Slack",
		Via:      "webhook",
		Inbound:  []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeLink},
		Outbound: []string{model.MsgTypeText},
		Events: []string{
			model.EventTypeMessageReceived, model.EventTypeMessageEdited, model.EventTypeMessageDeleted,
		},
		Features:       []Feature{FeatureGroup, FeatureMention},
		Note:           "Events API 签名 + 5 分钟时间窗；忽略 bot_id/subtype=bot_message；回复沿用 thread_ts",
		OutboundFormat: OutboundFormatPlain,
		MaxTextLen:     40000,
		FormatEvidence: "Slack chat.postMessage text 上限 40000 字符；本仓按纯文本投递，不依赖 mrkdwn 语法",
		MaxAttachments: 1, MaxAttachmentBytes: 0,
		AttachmentEvidence: "入站文件只透传 Slack 私有 URL，不下载；出站只投文本（原生文件上传不在 sender 范围）",
	})
	Register(Manifest{
		Channel:  model.ChannelWeCombBot,
		Label:    "企业微信群机器人",
		Via:      "webhook",
		Inbound:  []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeLink, model.MsgTypeCard},
		Outbound: []string{model.MsgTypeText},
		// enter_chat / template_card_event / feedback 只落事件审计，不映射业务事件。
		Events:         []string{model.EventTypeMessageReceived},
		Features:       []Feature{FeatureGroup, FeatureMention},
		Note:           "回调 AES-CBC 解密 + 签名校验；出站走群机器人 webhook（markdown）",
		OutboundFormat: OutboundFormatMarkdown,
		MaxTextLen:     4096,
		FormatEvidence: "企微群机器人 markdown.content ≤4096 字节，原生支持 Markdown",
		MaxAttachments: 1, MaxAttachmentBytes: 0,
		AttachmentEvidence: "markdown 载荷不携带附件；入站图片只透传 URL，服务端不下载",
	})

	// ── 网页控件（站内）──────────────────────────────────────────────────
	Register(Manifest{
		Channel:            model.ChannelWeb,
		Label:              "站内网页控件",
		Via:                "widget",
		Inbound:            []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile},
		Outbound:           []string{model.MsgTypeText},
		Note:               "embed-sdk 直连，无第三方验签",
		OutboundFormat:     OutboundFormatMarkdown,
		FormatEvidence:     "embed-sdk 前端按 Markdown 渲染气泡正文",
		MaxAttachments:     1,
		MaxAttachmentBytes: 20 * 1024 * 1024,
		AttachmentEvidence: "user-web/src/views/chat/embed/components/ChatInput.vue:186 拦截 file.size > 20MB；单次选择 1 个文件",
	})

	// ── 蜂桥网页私信渠道（扩展巡检上报 + outbox 下发）────────────────────
	bridge := func(channel, label, note string) Manifest {
		return Manifest{
			Channel:        channel,
			Label:          label,
			Via:            "bridge",
			Inbound:        []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeVideo, model.MsgTypeLink},
			Outbound:       []string{model.MsgTypeText, model.MsgTypeImage},
			OutboundFormat: OutboundFormatPlain,
			FormatEvidence: "网页私信输入框为纯文本，出站前由 StripMarkdownForDM 剥语法",
			// 单条消息一个媒体（服务端落单值 media_url）；URL 直传不下载，故无字节上限。
			MaxAttachments:     1,
			MaxAttachmentBytes: 0,
			AttachmentEvidence: "网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url）",
			Note:               note,
		}
	}
	Register(bridge(model.ChannelDouyin, "抖音私信", "扩展巡检上报；自声/去重由 inbox_ingress 兜底"))
	Register(bridge(model.ChannelXHS, "小红书私信", "同抖音桥接链路"))
	Register(bridge(model.ChannelKuaishou, "快手私信", "Experimental"))
	Register(bridge(model.ChannelTikTok, "TikTok 私信", "海外链路"))
	Register(bridge(model.ChannelXianyu, "闲鱼", ""))
	Register(bridge(model.ChannelJD, "京东（京麦/咚咚）", "Experimental，选择器待真机"))
	Register(bridge(model.ChannelBilibili, "B 站私信", "Experimental"))
	Register(bridge(model.ChannelWeibo, "微博私信", "Experimental"))
	Register(bridge(model.ChannelPersonalWX, "个人微信（桌面伴生）", "伴生为外部工程，服务端协议已就绪"))
	Register(bridge(model.ChannelPDD, "拼多多商家", "Experimental，选择器待真机"))
	// 2026-11-18 电商店铺五渠道骨架（CH-15/18/19/28/35，二次审核补齐）：桥接链路同源，
	// fail-closed（SEL 零证据拒启）。缺此登记 RenderForChannel 直接报「出站不支持该渠道」。
	Register(bridge(model.ChannelTaobao, "千牛网页接待中心", "Experimental，选择器待真机"))
	Register(bridge(model.ChannelDouyinShop, "抖店飞鸽客服", "Experimental，选择器待真机"))
	Register(bridge(model.ChannelKuaishouShop, "快手小店快语客服", "Experimental，选择器待真机"))
	Register(bridge(model.ChannelShopee, "Shopee Seller Centre Webchat", "Experimental，选择器待真机"))
	Register(bridge(model.ChannelWalmartUS, "Walmart Seller Center Inbox", "Experimental，选择器待真机"))

	// ── 通用触达 / 自定义 ────────────────────────────────────────────────
	Register(Manifest{
		Channel:            model.ChannelEmail,
		Label:              "邮件",
		Via:                "smtp",
		Inbound:            []string{model.MsgTypeText, model.MsgTypeFile, model.MsgTypeImage},
		Outbound:           []string{model.MsgTypeText},
		Note:               "SMTP 需后台配置授权码",
		OutboundFormat:     OutboundFormatHTML,
		FormatEvidence:     "SMTP 正文按 HTML 投递（邮件客户端渲染）",
		MaxAttachments:     0,
		MaxAttachmentBytes: 0,
		AttachmentEvidence: "SMTP 单封大小由服务商决定，本仓不校验；入站附件在 inbox_ingress_persist 压成单值 media_url",
	})
	Register(Manifest{
		Channel:            "sms",
		Label:              "短信",
		Via:                "aliyun-sms",
		Outbound:           []string{model.MsgTypeText},
		Note:               "仅触达，无入站回调",
		OutboundFormat:     OutboundFormatPlain,
		MaxTextLen:         500,
		FormatEvidence:     "阿里云短信单条 500 字，纯文本",
		MaxAttachments:     0,
		MaxAttachmentBytes: 0,
		AttachmentEvidence: "短信无附件通道（Outbound 仅 text）",
	})
	Register(Manifest{
		Channel:            "custom",
		Label:              "自定义 webhook",
		Via:                "webhook",
		Inbound:            []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeCard},
		Outbound:           []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile},
		Note:               "自有系统对接：固定 JSON 契约 + X-Hive-Signature（t=unix,v1=HMAC-SHA256）",
		OutboundFormat:     OutboundFormatPlain,
		FormatEvidence:     "custom 出站 POST callback_url 固定 JSON，content 为纯文本",
		MaxAttachments:     1,
		MaxAttachmentBytes: 0,
		AttachmentEvidence: "契约仅一个 media_url 字段，URL 由自有系统托管，服务端不校验字节数",
	})
}
