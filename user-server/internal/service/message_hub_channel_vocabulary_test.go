package service

import (
	"testing"

	"hivemtk-user/internal/model"
)

// TestMessageHubPlatformsCoverAllChannelConstants 渠道词表单源守门：
// 每个对外声明的渠道常量都必须被 message_hub 的平台词表接受。
//
// 背景（2026-11 六渠道统一接待）：新增渠道需要在 model/channelgw/bridge/
// message_hub/capregistry 多处登记，漏掉 message_hub 的直接后果是
// "入站能进、出站轨迹落不了库"，线上只表现为一条静默失败。此处把
// 渠道常量全集与词表锁在一起，漏登记即红。
func TestMessageHubPlatformsCoverAllChannelConstants(t *testing.T) {
	channels := []string{
		model.ChannelTelegram,
		model.ChannelWhatsApp,
		model.ChannelXHS,
		model.ChannelWeCom,
		model.ChannelXianyu,
		model.ChannelDouyin,
		model.ChannelKuaishou,
		model.ChannelTikTok,
		model.ChannelEmail,
		model.ChannelFeishu,
		model.ChannelDingTalk,
		model.ChannelQQ,
		model.ChannelJD,
		model.ChannelBilibili,
		model.ChannelWeibo,
		model.ChannelPersonalWX,
	}
	for _, ch := range channels {
		if !messageHubPlatforms[ch] {
			t.Errorf("message_hub 平台词表缺少渠道常量 %q（入站/出站会静默落库失败）", ch)
		}
	}

	// 公众号两个真实存在的写法：入站用 wechat，触达用 weixin；两条都必须能落库。
	for _, ch := range []string{"wechat", "weixin"} {
		if !messageHubPlatforms[ch] {
			t.Errorf("message_hub 平台词表缺少公众号渠道 %q", ch)
		}
	}
}
