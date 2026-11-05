package bridge

import (
	"testing"

	"hivemtk-user/internal/model"
)

// TestNormalizeBridgeChannel_NewChannels 2026-11 六渠道统一接待：
// 新增渠道的别名必须收敛到与 model 常量完全一致的规范名，且注册表认识它们。
func TestNormalizeBridgeChannel_NewChannels(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"jd", model.ChannelJD},
		{"jd_web", model.ChannelJD},
		{"jingdong", model.ChannelJD},
		{"jd_chat", model.ChannelJD},
		{"bilibili", model.ChannelBilibili},
		{"bili", model.ChannelBilibili},
		{"bilibili_web", model.ChannelBilibili},
		{"b站", model.ChannelBilibili},
		{"weibo", model.ChannelWeibo},
		{"wb", model.ChannelWeibo},
		{"weibo_web", model.ChannelWeibo},
		{"personal_wx", model.ChannelPersonalWX},
		{"wx_personal", model.ChannelPersonalWX},
		{"wechat_personal", model.ChannelPersonalWX},
		{"personal_wechat", model.ChannelPersonalWX},
	}
	for _, tc := range cases {
		if got := NormalizeBridgeChannel(tc.in); got != tc.want {
			t.Errorf("NormalizeBridgeChannel(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if !IsBridgeChannel(tc.in) {
			t.Errorf("IsBridgeChannel(%q) = false, want true", tc.in)
		}
	}

	// 未知渠道不得被误认为桥接渠道。
	if IsBridgeChannel("unknown-channel") {
		t.Error("unknown-channel 不应是桥接渠道")
	}
}
