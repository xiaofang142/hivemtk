package service

import (
	"net"
	"net/url"
	"strings"
	"unicode"

	"hivemtk-user/internal/channelcaps"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/urlguard"
)

// outboundSupportsRichCards 是渲染层的兼容入口（specs/024 T403）。
//
// 事实源已迁到 channelcaps.FeatureRichCard：这里保留旧签名只为不惊动调用方与存量测试，
// 新代码请直接用 channelcaps.SupportsRichCards。
func outboundSupportsRichCards(channel WebhookChannel) bool {
	return channelcaps.SupportsRichCards(string(channel))
}

func channelCardPrompt(platform string) string {
	if outboundSupportsRichCards(WebhookChannel(platform)) {
		return "\n\n# 当前渠道交付能力\n当前渠道支持富卡。card.show 仅构造卡片，不代表已送达或客户已点击；正文仍须直接回答问题，不得声称操作已执行。"
	}
	return "\n\n# 当前渠道交付能力\n当前渠道仅支持文字。必须用完整文字直接回答问题；card.show 如有调用仅提供结构化事实，系统会转换为同一条文字。不得声称已经展示卡片、让客户点击卡片或声称按钮操作已执行。链接只使用可信来源，不得编造。"
}

// cardTextDelivery 是 foldCardsToText 的兼容入口（specs/024 T403）。
//
// 旧签名保留：延迟重放/回放测试都在用它；实现已搬到 outbound_render.go，
// 那里与 RenderForChannel 共用同一段折叠逻辑（一处实现，两处调用）。
func cardTextDelivery(content string, cards []model.RichCard) (string, error) {
	return foldCardsToText(content, cards)
}

// cardPublicHTTPSLink is a static presentation guard, not a trust/DNS guarantee.
// Rendering a model-provided link must not itself trigger a network request.
func cardPublicHTTPSLink(raw string) string {
	if raw == "" || strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Opaque != "" {
		return ""
	}
	if port := u.Port(); port != "" && port != "443" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil {
		if urlguard.CheckIP(ip) != nil || ip.IsMulticast() {
			return ""
		}
	} else {
		labels := strings.Split(host, ".")
		if len(labels) < 2 || len(host) > 253 {
			return ""
		}
		for _, suffix := range []string{"localhost", "local", "internal", "lan", "home", "test", "invalid"} {
			if labels[len(labels)-1] == suffix {
				return ""
			}
		}
		// Numeric/hex alternative IP encodings are not public domain names.
		last := labels[len(labels)-1]
		if !strings.ContainsAny(last, "abcdefghijklmnopqrstuvwxyz") || strings.HasPrefix(last, "0x") {
			return ""
		}
		for _, label := range labels {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return ""
			}
			for _, r := range label {
				if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
					return ""
				}
			}
		}
	}
	for key := range u.Query() {
		switch strings.ToLower(key) {
		case "password", "access_token", "authorization", "cookie", "session", "secret":
			return ""
		}
	}
	return raw
}
