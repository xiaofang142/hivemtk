package channelgw

import (
	"testing"

	"hivemtk-user/internal/model"
)

// TestChannelAliasesResolveToKnownChannels 闸 7（G-01）：别名表里的每个 historical
// 写法都必须归一到统一总线的已知渠道（注册表或直连集合），禁止别名指向不存在的
// 渠道导致入站被静默丢弃。
func TestChannelAliasesResolveToKnownChannels(t *testing.T) {
	if len(channelAliases) == 0 {
		t.Fatal("渠道别名表为空：G-01 单源归一未生效")
	}
	for alias, canonical := range channelAliases {
		if alias == "" || canonical == "" {
			t.Errorf("别名表存在空键/空值: %q -> %q", alias, canonical)
			continue
		}
		if !IsKnownChannel(canonical) {
			t.Errorf("别名 %q -> %q 不是已知渠道（registry 与直连集合均未登记）", alias, canonical)
		}
		if got := NormalizeChannel(alias); got != canonical {
			t.Errorf("NormalizeChannel(%q)=%q，期望 %q", alias, got, canonical)
		}
		if got := NormalizeChannel("  " + alias + "  "); got != canonical {
			t.Errorf("NormalizeChannel 未做 trim/lower: %q -> %q", alias, got)
		}
	}
}

// TestCanonicalNamesMatchModelConstants 闸 8（G-01）：协议 canonical 名与 model 常量
// 逐值对齐——四套命名里 DB channels.code / Registry.Name / model.Channel* 必须同值，
// WebhookChannel 只是 string 别名，不再维护第二份取值表。
func TestCanonicalNamesMatchModelConstants(t *testing.T) {
	// web 是通用网页标识（非具体接入渠道），不进入渠道注册表/发掘集合，单独排除。
	modelValues := []string{
		model.ChannelTelegram, model.ChannelWhatsApp, model.ChannelXHS,
		model.ChannelWeCom, model.ChannelXianyu, model.ChannelDouyin, model.ChannelKuaishou,
		model.ChannelTikTok, model.ChannelJD, model.ChannelBilibili, model.ChannelWeibo,
		model.ChannelPersonalWX, model.ChannelPDD, model.ChannelEmail, model.ChannelFeishu,
		model.ChannelDingTalk, model.ChannelQQ, model.ChannelWeComCS, model.ChannelOneBot,
		model.ChannelSlack, model.ChannelWeCombBot, model.ChannelCustom, model.ChannelTaobao,
		model.ChannelDouyinShop, model.ChannelKuaishouShop, model.ChannelShopee,
		model.ChannelWalmartUS,
	}
	for _, v := range modelValues {
		if NormalizeChannel(v) != v {
			t.Errorf("canonical 值 %q 被别名表二次改写为 %q（四套命名漂移）", v, NormalizeChannel(v))
		}
		if !IsKnownChannel(v) {
			t.Errorf("model 渠道常量 %q 不在统一总线已知集合（registry + 直连集合漏登记）", v)
		}
	}
	for _, name := range Default.Names() {
		if NormalizeChannel(name) != name {
			t.Errorf("Registry 名 %q 被别名表改写为 %q", name, NormalizeChannel(name))
		}
	}
}

// TestUnknownChannelStaysUnknown 未登记名不得被悄悄伪装成已知渠道（fail-closed）。
func TestUnknownChannelStaysUnknown(t *testing.T) {
	if IsKnownChannel("totally_unknown_channel") {
		t.Error("未知渠道不应命中统一总线已知集合")
	}
	if got := NormalizeChannel("TOTALLY_UNKNOWN_CHANNEL"); got != "totally_unknown_channel" {
		t.Errorf("未知渠道归一结果应仅小写化，实际=%q", got)
	}
}
