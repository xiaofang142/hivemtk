package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

func TestCardDelivery_PromptRespectsActualTransport(t *testing.T) {
	engine := &SalesEngine{}
	for _, platform := range []string{"douyin", "xiaohongshu", "unknown"} {
		t.Run(platform, func(t *testing.T) {
			prompt := engine.plainDispatchSystemPrompt(context.Background(), &SalesRequest{
				Platform: platform, Config: &SalesEngineConfig{Persona: "合成销售助手"},
			}, "zh", nil)
			if !strings.Contains(prompt, "文字") || !strings.Contains(prompt, "不得声称") {
				t.Fatalf("无卡片载体必须在生成前说明文字和承诺边界：%q", prompt)
			}
		})
	}
	prompt := buildAgentSystemPrompt("合成销售助手", nil, nil, nil, nil, nil, "")
	if strings.Contains(prompt, "必须调用 card.show") || strings.Contains(prompt, "禁止只在文本") {
		t.Fatalf("工具提示不得跨渠道强制展示未确认的卡片：%q", prompt)
	}
}

func TestCardDelivery_TextPreservesFactsAndReplayIsIdempotent(t *testing.T) {
	cards := []model.RichCard{{
		Title: "合成部署方案", Subtitle: "适用范围需确认", Description: strings.Repeat("必要事实", 100),
		Fields:  map[string]string{"B费用": "以正式报价为准", "A内存": "32GB"},
		Buttons: []model.CardButton{{Text: "详情", URL: "https://docs.example.com/product"}, {Text: "支付", Action: "pay"}},
	}}
	text, err := cardTextDelivery("内存需求还取决于模型大小。", cards)
	if err != nil {
		t.Fatalf("可读卡片不能丢弃：%v", err)
	}
	for _, fact := range []string{"内存需求还取决于模型大小", cards[0].Description, "适用范围需确认", "32GB", "以正式报价为准", "https://docs.example.com/product", "无法执行此操作"} {
		if !strings.Contains(text, fact) {
			t.Errorf("文字替代缺少事实 %q", fact)
		}
	}
	if strings.Index(text, "A内存") > strings.Index(text, "B费用") {
		t.Fatal("字段次序必须确定，不能随 map 遍历波动导致重复身份")
	}
	if again, err := cardTextDelivery(text, cards); err != nil || again != text {
		t.Fatalf("重放不能重复附加卡片信息：err=%v", err)
	}
	if _, err := cardTextDelivery("已经展示", []model.RichCard{{}}); err == nil {
		t.Fatal("不可表达的空卡片必须明确失败而不是发送空内容")
	}
}

func TestCardDelivery_LinksAreStaticHTTPSOnly(t *testing.T) {
	for _, raw := range []string{
		"http://docs.example.com", "javascript:alert(1)", "https://user:pass@docs.example.com",
		"https://127.0.0.1/a", "https://[::1]/a", "https://10.0.0.1/a", "https://169.254.169.254/a",
		"https://100.64.0.1/a", "https://localhost/a", "https://app.local/a", "https://app.internal/a",
		"https://2130706433/a", "https://0x7f000001/a", "https://127.1/a", "https://docs.example.com:8443/a",
		"https://docs.example.com/?access_token=secret", "https://docs.example.com/\nunsafe", "https://a..com",
	} {
		t.Run(raw, func(t *testing.T) {
			if got := cardPublicHTTPSLink(raw); got != "" {
				t.Fatalf("不应呈现危险或内部链接：%q", got)
			}
		})
	}
	for _, raw := range []string{"https://docs.example.com/product?id=1", "https://docs.example.com:443/product"} {
		if got := cardPublicHTTPSLink(raw); got != raw {
			t.Fatalf("合法 HTTPS 链接应原样保留：%q", got)
		}
	}
}

func TestCardDelivery_CapabilityUsesActualSender(t *testing.T) {
	for _, channel := range []WebhookChannel{ChannelFeishu, ChannelTelegram} {
		if !outboundSupportsRichCards(channel) || !strings.Contains(channelCardPrompt(string(channel)), "支持富卡") {
			t.Fatalf("原有富卡能力不能被取消：%s", channel)
		}
	}
	for _, channel := range []WebhookChannel{ChannelDouyin, ChannelXiaohongshu, WebhookChannel("unknown")} {
		if outboundSupportsRichCards(channel) {
			t.Fatalf("未知或无载体通道不能宣称卡片可见：%s", channel)
		}
	}
}
