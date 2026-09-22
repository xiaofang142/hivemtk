package service

import (
	"context"
	"strings"
)

// TelegramLeadAdapter Telegram 渠道线索适配器
type TelegramLeadAdapter struct{}

func (TelegramLeadAdapter) Channel() string       { return "telegram" }
func (TelegramLeadAdapter) ClueType() int64       { return ClueTypeTelegram }
func (TelegramLeadAdapter) DescPrefix() string    { return "[TG]" }
func (TelegramLeadAdapter) OutreachMinScore() int { return tgDMOutreachMinScore }
func (TelegramLeadAdapter) LeadTag(isOpportunity bool) string {
	if isOpportunity {
		return "群发言商机"
	}
	return "群发言线索"
}
func (TelegramLeadAdapter) ChatLabel(groupTitle string) string {
	g := strings.TrimSpace(groupTitle)
	if g == "" {
		return "私聊"
	}
	return g
}
func (TelegramLeadAdapter) AccountKey(fromID, fromName, username string) string {
	return telegramLeadAccountKey(username, fromID)
}
func (TelegramLeadAdapter) DisplayName(fromName, username, accountKey string) string {
	name := strings.TrimSpace(fromName)
	if name == "" {
		name = strings.TrimSpace(username)
	}
	if name == "" {
		return accountKey
	}
	return name
}
func (TelegramLeadAdapter) ExtraKeywords() (high, medium []string) { return nil, nil }
func (a TelegramLeadAdapter) TriggerOutreach(ctx context.Context, s *WebhookService, accountID, fromID, groupID, groupTitle string, score int, originalText string) {
	s.triggerTGDMOutreach(ctx, accountID, fromID, groupID, groupTitle, score, originalText)
}

// DouyinLeadAdapter 抖音渠道线索适配器
type DouyinLeadAdapter struct{}

func (DouyinLeadAdapter) Channel() string       { return "douyin" }
func (DouyinLeadAdapter) ClueType() int64       { return ClueTypeDouyin }
func (DouyinLeadAdapter) DescPrefix() string    { return "[Douyin]" }
func (DouyinLeadAdapter) OutreachMinScore() int { return dyDMOutreachMinScore }
func (DouyinLeadAdapter) LeadTag(isOpportunity bool) string {
	if isOpportunity {
		return "抖音商机"
	}
	return "抖音线索"
}
func (DouyinLeadAdapter) ChatLabel(groupTitle string) string {
	g := strings.TrimSpace(groupTitle)
	if g == "" {
		return "抖音直播间"
	}
	return g
}
func (DouyinLeadAdapter) AccountKey(fromID, fromName, username string) string {
	account := "@" + strings.TrimLeft(fromName, "@")
	if account == "@" || account == "" {
		account = "dy:" + strings.TrimSpace(fromID)
	}
	if account == "dy:" || account == "" {
		return ""
	}
	return account
}
func (DouyinLeadAdapter) DisplayName(fromName, username, accountKey string) string {
	name := strings.TrimSpace(fromName)
	if name == "" {
		return accountKey
	}
	return name
}
func (DouyinLeadAdapter) ExtraKeywords() (high, medium []string) {
	return []string{"直播间", "小黄车", "链接", "橱窗"}, []string{"推荐", "介绍", "分享"}
}
func (a DouyinLeadAdapter) TriggerOutreach(ctx context.Context, s *WebhookService, accountID, fromID, groupID, groupTitle string, score int, originalText string) {
	s.triggerBridgeDMOutreach(ctx, string(ChannelDouyin), accountID, fromID, groupID, groupTitle, score, originalText)
}

// BridgeLeadAdapter Bridge 网页渠道通用适配器（小红书/TikTok/快手/闲鱼等）
type BridgeLeadAdapter struct {
	ChannelName string
	Type        int64
	Prefix      string
	DefaultChat string
}

func (a BridgeLeadAdapter) Channel() string       { return a.ChannelName }
func (a BridgeLeadAdapter) ClueType() int64       { return a.Type }
func (a BridgeLeadAdapter) DescPrefix() string    { return a.Prefix }
func (a BridgeLeadAdapter) OutreachMinScore() int { return dyDMOutreachMinScore }
func (a BridgeLeadAdapter) LeadTag(isOpportunity bool) string {
	if isOpportunity {
		return "群发言商机"
	}
	return "群发言线索"
}
func (a BridgeLeadAdapter) ChatLabel(groupTitle string) string {
	g := strings.TrimSpace(groupTitle)
	if g == "" {
		return a.DefaultChat
	}
	return g
}
func (a BridgeLeadAdapter) AccountKey(fromID, fromName, username string) string {
	account := "@" + strings.TrimLeft(fromName, "@")
	if account == "@" || account == "" {
		account = a.ChannelName + ":" + strings.TrimSpace(fromID)
	}
	if account == a.ChannelName+":" || account == "" {
		return ""
	}
	return account
}
func (a BridgeLeadAdapter) DisplayName(fromName, username, accountKey string) string {
	name := strings.TrimSpace(fromName)
	if name == "" {
		return accountKey
	}
	return name
}
func (a BridgeLeadAdapter) ExtraKeywords() (high, medium []string) { return nil, nil }
func (a BridgeLeadAdapter) TriggerOutreach(ctx context.Context, s *WebhookService, accountID, fromID, groupID, groupTitle string, score int, originalText string) {
	s.triggerBridgeDMOutreach(ctx, a.ChannelName, accountID, fromID, groupID, groupTitle, score, originalText)
}

func bridgeLeadAdapterForChannel(channel string) BridgeLeadAdapter {
	switch channel {
	case "xiaohongshu":
		return BridgeLeadAdapter{ChannelName: "xiaohongshu", Type: ClueTypeXiaohongshu, Prefix: "[Xiaohongshu]", DefaultChat: "小红书笔记/群组"}
	case "tiktok":
		return BridgeLeadAdapter{ChannelName: "tiktok", Type: ClueTypeTikTok, Prefix: "[TikTok]", DefaultChat: "TikTok 直播/群组"}
	case "kuaishou":
		return BridgeLeadAdapter{ChannelName: "kuaishou", Type: ClueTypeKuaishou, Prefix: "[Kuaishou]", DefaultChat: "快手直播间"}
	case "xianyu":
		return BridgeLeadAdapter{ChannelName: "xianyu", Type: ClueTypeXianyu, Prefix: "[Xianyu]", DefaultChat: "闲鱼群组"}
	case "douyin":
		return BridgeLeadAdapter{ChannelName: "douyin", Type: ClueTypeDouyin, Prefix: "[Douyin]", DefaultChat: "抖音直播间"}
	default:
		return BridgeLeadAdapter{ChannelName: channel, Type: ClueTypeCustom, Prefix: "[" + channel + "]", DefaultChat: "群组"}
	}
}

// WhatsAppLeadAdapter WhatsApp 渠道线索适配器（无主动触达，24h 窗口限制）
type WhatsAppLeadAdapter struct{}

func (WhatsAppLeadAdapter) Channel() string       { return "whatsapp" }
func (WhatsAppLeadAdapter) ClueType() int64       { return ClueTypeWhatsapp }
func (WhatsAppLeadAdapter) DescPrefix() string    { return "[WhatsApp]" }
func (WhatsAppLeadAdapter) OutreachMinScore() int { return 0 }
func (WhatsAppLeadAdapter) LeadTag(isOpportunity bool) string {
	if isOpportunity {
		return "私聊商机"
	}
	return "私聊线索"
}
func (WhatsAppLeadAdapter) ChatLabel(groupTitle string) string {
	g := strings.TrimSpace(groupTitle)
	if g == "" {
		return "WhatsApp 私聊"
	}
	return g
}
func (WhatsAppLeadAdapter) AccountKey(fromID, fromName, username string) string {
	id := strings.TrimSpace(fromID)
	if id == "" {
		return ""
	}
	return "wa:" + id
}
func (WhatsAppLeadAdapter) DisplayName(fromName, username, accountKey string) string {
	name := strings.TrimSpace(fromName)
	if name == "" {
		return accountKey
	}
	return name
}
func (WhatsAppLeadAdapter) ExtraKeywords() (high, medium []string) { return nil, nil }
func (WhatsAppLeadAdapter) TriggerOutreach(_ context.Context, _ *WebhookService, _, _, _, _ string, _ int, _ string) {

}

// WeComLeadAdapter 企业微信渠道线索适配器
type WeComLeadAdapter struct{}

func (WeComLeadAdapter) Channel() string       { return "wecom" }
func (WeComLeadAdapter) ClueType() int64       { return ClueTypeWeCom }
func (WeComLeadAdapter) DescPrefix() string    { return "[WeCom]" }
func (WeComLeadAdapter) OutreachMinScore() int { return 0 }
func (WeComLeadAdapter) LeadTag(isOpportunity bool) string {
	if isOpportunity {
		return "企微商机"
	}
	return "企微线索"
}
func (WeComLeadAdapter) ChatLabel(groupTitle string) string {
	g := strings.TrimSpace(groupTitle)
	if g == "" {
		return "企微会话"
	}
	return g
}
func (WeComLeadAdapter) AccountKey(fromID, fromName, username string) string {
	id := strings.TrimSpace(fromID)
	if id == "" {
		return ""
	}
	return "wecom:" + id
}
func (WeComLeadAdapter) DisplayName(fromName, username, accountKey string) string {
	name := strings.TrimSpace(fromName)
	if name == "" {
		return accountKey
	}
	return name
}
func (WeComLeadAdapter) ExtraKeywords() (high, medium []string) { return nil, nil }
func (WeComLeadAdapter) TriggerOutreach(_ context.Context, _ *WebhookService, _, _, _, _ string, _ int, _ string) {
}

// FeishuLeadAdapter 飞书渠道线索适配器
type FeishuLeadAdapter struct{}

func (FeishuLeadAdapter) Channel() string       { return "feishu" }
func (FeishuLeadAdapter) ClueType() int64       { return ClueTypeFeishu }
func (FeishuLeadAdapter) DescPrefix() string    { return "[Feishu]" }
func (FeishuLeadAdapter) OutreachMinScore() int { return 0 }
func (FeishuLeadAdapter) LeadTag(isOpportunity bool) string {
	if isOpportunity {
		return "飞书商机"
	}
	return "飞书线索"
}
func (FeishuLeadAdapter) ChatLabel(groupTitle string) string {
	g := strings.TrimSpace(groupTitle)
	if g == "" {
		return "飞书会话"
	}
	return g
}
func (FeishuLeadAdapter) AccountKey(fromID, fromName, username string) string {
	id := strings.TrimSpace(fromID)
	if id == "" {
		return ""
	}
	return "fs:" + id
}
func (FeishuLeadAdapter) DisplayName(fromName, username, accountKey string) string {
	name := strings.TrimSpace(fromName)
	if name == "" {
		return accountKey
	}
	return name
}
func (FeishuLeadAdapter) ExtraKeywords() (high, medium []string) { return nil, nil }
func (FeishuLeadAdapter) TriggerOutreach(_ context.Context, _ *WebhookService, _, _, _, _ string, _ int, _ string) {
}

// GenericLeadAdapter 通用渠道适配器（用于未专门实现的渠道，如 Twitter/邮件/短信/QQ/微信/网页组件等）
type GenericLeadAdapter struct {
	ChannelName string
	Type        int64
	Prefix      string
}

func (a GenericLeadAdapter) Channel() string       { return a.ChannelName }
func (a GenericLeadAdapter) ClueType() int64       { return a.Type }
func (a GenericLeadAdapter) DescPrefix() string    { return a.Prefix }
func (a GenericLeadAdapter) OutreachMinScore() int { return 0 }
func (a GenericLeadAdapter) LeadTag(isOpportunity bool) string {
	if isOpportunity {
		return "商机线索"
	}
	return "普通线索"
}
func (a GenericLeadAdapter) ChatLabel(groupTitle string) string {
	g := strings.TrimSpace(groupTitle)
	if g == "" {
		return a.ChannelName + " 会话"
	}
	return g
}
func (a GenericLeadAdapter) AccountKey(fromID, fromName, username string) string {
	id := strings.TrimSpace(fromID)
	if id == "" {
		id = strings.TrimSpace(fromName)
	}
	if id == "" {
		return ""
	}
	return a.ChannelName + ":" + id
}
func (a GenericLeadAdapter) DisplayName(fromName, username, accountKey string) string {
	name := strings.TrimSpace(fromName)
	if name == "" {
		return accountKey
	}
	return name
}
func (a GenericLeadAdapter) ExtraKeywords() (high, medium []string) { return nil, nil }
func (a GenericLeadAdapter) TriggerOutreach(_ context.Context, _ *WebhookService, _, _, _, _ string, _ int, _ string) {
}

// LeadAdapterForPlatform 根据平台名返回对应的线索适配器（统一工厂入口）。
func LeadAdapterForPlatform(platform string) ChannelLeadAdapter {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "telegram", "tg":
		return TelegramLeadAdapter{}
	case "douyin":
		return DouyinLeadAdapter{}
	case "xiaohongshu", "xhs":
		return bridgeLeadAdapterForChannel("xiaohongshu")
	case "tiktok":
		return bridgeLeadAdapterForChannel("tiktok")
	case "kuaishou":
		return bridgeLeadAdapterForChannel("kuaishou")
	case "xianyu":
		return bridgeLeadAdapterForChannel("xianyu")
	case "whatsapp", "wa":
		return WhatsAppLeadAdapter{}
	case "wecom", "wechat_work":
		return WeComLeadAdapter{}
	case "feishu", "lark":
		return FeishuLeadAdapter{}
	case "qq":
		return GenericLeadAdapter{ChannelName: "qq", Type: ClueTypeQQ, Prefix: "[QQ]"}
	case "wechat", "wx":
		return GenericLeadAdapter{ChannelName: "wechat", Type: ClueTypeWeChat, Prefix: "[WeChat]"}
	case "twitter", "x":
		return GenericLeadAdapter{ChannelName: "twitter", Type: ClueTypeTwitter, Prefix: "[Twitter]"}
	case "email", "mail":
		return GenericLeadAdapter{ChannelName: "email", Type: ClueTypeEmail, Prefix: "[Email]"}
	case "sms":
		return GenericLeadAdapter{ChannelName: "sms", Type: ClueTypeSMS, Prefix: "[SMS]"}
	case "web", "web_embed", "widget":
		return GenericLeadAdapter{ChannelName: "web", Type: ClueTypeWebWidget, Prefix: "[Web]"}
	default:
		return GenericLeadAdapter{ChannelName: platform, Type: ClueTypeCustom, Prefix: "[" + platform + "]"}
	}
}
