package service

import (
	"testing"
)

// T04（2026-11-26）：tg/qq/whatsapp/飞书/企微这类「机器人」不是独立事实源，
// 而是渠道类型。判据落在统一目录的下拉源上：它们必须全部在 PresetChannelCodes
// 里（启动期幂等播种 → GET /api/channels/catalog 下发给全系统 ChannelSelect），
// 任何页面都不该再自建这几类的码表或自由输入框。
func TestT04BotChannelsAreInUnifiedDropdownSource(t *testing.T) {
	want := map[string]string{
		"telegram": "Telegram",
		"qq":       "QQ",
		"whatsapp": "WhatsApp",
		"feishu":   "飞书",
		"wecom":    "企业微信",
	}
	got := make(map[string]string, len(PresetChannelCodes))
	for _, c := range PresetChannelCodes {
		if _, dup := got[c.Code]; dup {
			t.Fatalf("预设渠道码重复：%s（下拉会出现两个同名选项）", c.Code)
		}
		got[c.Code] = c.Name
	}
	for code, name := range want {
		if got[code] == "" {
			t.Errorf("机器人渠道 %s 未进入统一目录下拉源 PresetChannelCodes，引用处将无法用 ChannelSelect 关联", code)
			continue
		}
		if got[code] != name {
			t.Errorf("渠道 %s 的显示名 = %q，want %q（下拉文案与业务称呼要一致）", code, got[code], name)
		}
	}
}
