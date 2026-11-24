package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

// P0-c：渠道发掘资格以 channelgw.Default 的 MineMode 投影为唯一真值源，
// 个微/QQ 等 MessageHub 平台必须能进入发掘集合（不再受 5 渠道硬编码白名单限制）。
func TestLeadMiningChannelCapabilityProjection(t *testing.T) {
	SetLeadMiningChannels([]string{"personal_wx", "qq", "douyin"})
	defer SetLeadMiningChannels(nil)
	for _, ch := range []string{"personal_wx", "qq", "douyin"} {
		if !leadMiningChannelCapable(ch) {
			t.Fatalf("渠道 %s 应参与发掘", ch)
		}
	}
	if leadMiningChannelCapable("unknown_channel") {
		t.Fatal("未登记渠道不得参与发掘")
	}
}

// P0-c：notifyLeadMined 只在入站且内容非空时触发发掘出口；出站/空内容不触发。
func TestNotifyLeadMined_Gates(t *testing.T) {
	s := &WebhookService{}
	// 未注入发掘出口时任何调用都应安全返回（不 panic、不隐式构造带 DB 服务）。
	for _, hub := range []*model.MessageHub{
		nil,
		{},
		{Direction: "inbound", Content: ""},
		{Direction: "outbound", Content: "hi"},
		{Direction: "inbound", Content: "有货"},
	} {
		s.notifyLeadMined(context.Background(), hub)
	}
}
