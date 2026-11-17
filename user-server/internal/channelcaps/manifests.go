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
		OutboundFormat: OutboundFormatHTML,
		MaxTextLen:     4096,
		FormatEvidence: "webhook_outbound.buildTelegramGroupReply：ParseMode=HTML + 自身 HTML 转义",
	})
	Register(Manifest{
		Channel:        model.ChannelWhatsApp,
		Label:          "WhatsApp Cloud",
		Via:            "webhook",
		Inbound:        []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeLocation},
		Outbound:       []string{model.MsgTypeText},
		Features:       []Feature{FeatureGroup},
		Note:           "24 小时窗口外用模板消息；出站当前只提交文字载荷",
		OutboundFormat: OutboundFormatPlain,
		MaxTextLen:     4096,
		FormatEvidence: "WhatsApp Cloud API text.body 为纯文本，4096 字上限",
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
		Features:       []Feature{FeatureRichCard, FeatureGroup, FeatureMention},
		Note:           "卡片转 interactive JSON；媒体文件异步回填 media_url",
		OutboundFormat: OutboundFormatPlain,
		FormatEvidence: "feishu SendInteractiveCard 走 interactive 载体；text 消息为纯文本",
	})
	Register(Manifest{
		Channel:        model.ChannelDingTalk,
		Label:          "钉钉",
		Via:            "webhook",
		Inbound:        []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeLink},
		Outbound:       []string{model.MsgTypeText},
		Features:       []Feature{FeatureGroup, FeatureMention},
		Note:           "出站 webhook 域名受 allowlist 约束（loadDingtalkWebhookHostAllowed）",
		OutboundFormat: OutboundFormatPlain,
		MaxTextLen:     20000,
		FormatEvidence: "outbound_impl_dingtalk：`msgtype=text`，纯文本正文",
	})
	Register(Manifest{
		Channel:        model.ChannelWeCom,
		Label:          "企业微信（自建应用）",
		Via:            "webhook",
		Inbound:        []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeLocation},
		Outbound:       []string{model.MsgTypeText},
		Features:       []Feature{FeatureGroup, FeatureMention},
		Note:           "XML 信封 msg_signature 恒时比较 + 时间窗验签",
		OutboundFormat: OutboundFormatPlain,
		MaxTextLen:     2048,
		FormatEvidence: "outbound_impl_wecom：MsgType=`text`，企微 text 消息为纯文本",
	})
	Register(Manifest{
		Channel:        "wechat",
		Label:          "微信公众号",
		Via:            "webhook",
		Inbound:        []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeLocation},
		Outbound:       []string{model.MsgTypeText},
		Note:           "客服消息通道；公众号无群聊能力",
		OutboundFormat: OutboundFormatPlain,
		MaxTextLen:     2048,
		FormatEvidence: "outbound_impl_wechat：SendCustomMessage(msgtype=text)",
	})
	Register(Manifest{
		Channel:  model.ChannelQQ,
		Label:    "QQ 官方机器人",
		Via:      "webhook",
		Inbound:  []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile, model.MsgTypeAudio, model.MsgTypeVideo, model.MsgTypeLink, model.MsgTypeCard},
		Outbound: []string{model.MsgTypeText},
		// 机器人入群/退群（GROUP_ADD_ROBOT / GROUP_DEL_ROBOT）决定群还能不能收消息。
		Events:         []string{model.EventTypeMessageReceived, model.EventTypeBotInvited, model.EventTypeBotRemoved},
		Features:       []Feature{FeatureGroup, FeatureMention},
		Note:           "bot.q.qq.com；Ed25519 验签，被动回复带 channel_msg_id",
		OutboundFormat: OutboundFormatPlain,
		MaxTextLen:     2000,
		FormatEvidence: "channelbot/qq splitQQMessage 自行按官方上限切分，渲染层不再切",
	})

	// ── 网页控件（站内）──────────────────────────────────────────────────
	Register(Manifest{
		Channel:        model.ChannelWeb,
		Label:          "站内网页控件",
		Via:            "widget",
		Inbound:        []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile},
		Outbound:       []string{model.MsgTypeText},
		Note:           "embed-sdk 直连，无第三方验签",
		OutboundFormat: OutboundFormatMarkdown,
		FormatEvidence: "embed-sdk 前端按 Markdown 渲染气泡正文",
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
			Note:           note,
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

	// ── 通用触达 / 自定义 ────────────────────────────────────────────────
	Register(Manifest{
		Channel:        model.ChannelEmail,
		Label:          "邮件",
		Via:            "smtp",
		Inbound:        []string{model.MsgTypeText, model.MsgTypeFile, model.MsgTypeImage},
		Outbound:       []string{model.MsgTypeText},
		Note:           "SMTP 需后台配置授权码",
		OutboundFormat: OutboundFormatHTML,
		FormatEvidence: "SMTP 正文按 HTML 投递（邮件客户端渲染）",
	})
	Register(Manifest{
		Channel:        "sms",
		Label:          "短信",
		Via:            "aliyun-sms",
		Outbound:       []string{model.MsgTypeText},
		Note:           "仅触达，无入站回调",
		OutboundFormat: OutboundFormatPlain,
		MaxTextLen:     500,
		FormatEvidence: "阿里云短信单条 500 字，纯文本",
	})
	Register(Manifest{
		Channel:        "custom",
		Label:          "自定义 webhook",
		Via:            "webhook",
		Inbound:        []string{model.MsgTypeText, model.MsgTypeImage, model.MsgTypeFile},
		Outbound:       []string{model.MsgTypeText},
		Note:           "自有系统对接，载荷按 WebhookService 约定",
		OutboundFormat: OutboundFormatPlain,
		FormatEvidence: "自有系统对接，载荷按 WebhookService 约定（纯文本）",
	})
}
