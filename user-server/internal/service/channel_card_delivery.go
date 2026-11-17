package service

import (
	"errors"
	"net"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/urlguard"
)

// outboundSupportsRichCards describes this sender, not unrelated web UI renderers.
func outboundSupportsRichCards(channel WebhookChannel) bool {
	return channel == ChannelFeishu || channel == ChannelTelegram
}

func channelCardPrompt(platform string) string {
	if outboundSupportsRichCards(WebhookChannel(platform)) {
		return "\n\n# 当前渠道交付能力\n当前渠道支持富卡。card.show 仅构造卡片，不代表已送达或客户已点击；正文仍须直接回答问题，不得声称操作已执行。"
	}
	return "\n\n# 当前渠道交付能力\n当前渠道仅支持文字。必须用完整文字直接回答问题；card.show 如有调用仅提供结构化事实，系统会转换为同一条文字。不得声称已经展示卡片、让客户点击卡片或声称按钮操作已执行。链接只使用可信来源，不得编造。"
}

// cardTextDelivery keeps the candidate separate from the actual text payload.
// It is deterministic and idempotent, so a durable replay cannot append facts twice.
func cardTextDelivery(content string, cards []model.RichCard) (string, error) {
	blocks := make([]string, 0, len(cards))
	for _, card := range cards {
		lines := make([]string, 0, 4+len(card.Fields)+len(card.Buttons))
		for _, fact := range []string{card.Title, card.Subtitle, card.Description} {
			if fact = strings.TrimSpace(fact); fact != "" {
				lines = append(lines, fact)
			}
		}
		keys := make([]string, 0, len(card.Fields))
		for key := range card.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := strings.TrimSpace(card.Fields[key])
			if value != "" {
				lines = append(lines, strings.TrimSpace(key)+"："+value)
			}
		}
		for _, button := range card.Buttons {
			label := strings.TrimSpace(button.Text)
			if label == "" {
				label = "详情"
			}
			if link := cardPublicHTTPSLink(button.URL); link != "" {
				lines = append(lines, label+"："+link)
			} else if button.URL != "" || button.Action != "" {
				lines = append(lines, label+"（当前渠道无法执行此操作，请联系人工确认）")
			} else if button.Text != "" {
				lines = append(lines, label)
			}
		}
		for _, image := range []string{card.ImageURL, card.ThumbURL} {
			if link := cardPublicHTTPSLink(image); link != "" {
				lines = append(lines, "图片链接："+link)
			}
		}
		if len(lines) > 0 {
			blocks = append(blocks, strings.Join(lines, "\n"))
		}
	}
	if len(blocks) == 0 {
		return "", errors.New("卡片无法形成可读文字载荷")
	}
	// Neutralize only known presentation claims; preserve the rest of the answer's facts.
	text := strings.NewReplacer(
		"我已经为你展示了卡片", "以下是文字版信息",
		"已经为您展示了卡片", "以下是文字版信息",
		"请点击卡片查看详情", "请查看下方文字详情",
		"点击卡片查看详情", "查看下方文字详情",
		"请点击卡片", "请查看下方文字信息",
		"请查看卡片", "请查看下方文字信息",
	).Replace(strings.TrimSpace(content))
	for _, block := range blocks {
		if strings.Contains(text, block) {
			continue
		}
		if text != "" {
			text += "\n\n"
		}
		text += block
	}
	return text, nil
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
