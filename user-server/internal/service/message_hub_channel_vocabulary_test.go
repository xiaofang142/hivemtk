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

// TestMessageHubChannelVocabularyWechatWeixinPersonalWXSeparation 公众号双写法与个人微信的三词分离：
// wechat（入站控制器写）与 weixin（触达链路写）是公众号的两个真实落库写法，personal_wx 是
// 独立渠道「个人微信」。词表层只做"接受"，不做改写合并——统计按存储原值 GROUP BY platform，
// 三个词必须同时被接受且是三个不同的词值；显示归一（都叫「微信公众号」/「个人微信」）
// 是前端 src/constants/channel.js 的职责，不在这里替它收口。
func TestMessageHubChannelVocabularyWechatWeixinPersonalWXSeparation(t *testing.T) {
	// 企微/小红书的 canonical 常量同场核对，契约表每行都有落点。
	for _, ch := range []string{
		"wechat", "weixin",
		model.ChannelPersonalWX, // personal_wx
		model.ChannelWeCom,      // wecom（企微）
		model.ChannelXHS,        // xiaohongshu（小红书；前端旧值 xhs 由前端别名表归一）
	} {
		if !messageHubPlatforms[ch] {
			t.Errorf("message_hub 平台词表缺少渠道 %q", ch)
		}
	}

	// 三词分离：公众号双写法与个人微信互为不同词值，谁也不许是谁的别名
	// （别名归一是纯显示层概念；存储层一个词就是一行分桶）。
	if "wechat" == "weixin" || "wechat" == string(model.ChannelPersonalWX) ||
		"weixin" == string(model.ChannelPersonalWX) {
		t.Fatal("公众号双写法与个人微信的词值必须互不相同")
	}
	if messageHubPlatforms["personal_wx"] != true || messageHubPlatforms["wechat"] != true ||
		messageHubPlatforms["weixin"] != true {
		t.Fatal("三词必须同时被词表接受")
	}
}
