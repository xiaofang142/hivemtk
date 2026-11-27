package service

import (
	"context"
	"testing"
)

// 统一渠道消息总线：cron/polling 任务回投 webhook 时必须能在编排侧携带渠道识别信息。
// 这里只钉住 URL 构造这一纯函数；租户/账号的 fail-closed 校验由
// bindCallbackTenant + ChannelResolver 的既有测试覆盖。
func TestBuildTelegramPollingWebhookURL(t *testing.T) {
	cases := []struct {
		name         string
		base         string
		accountID    uint
		identifyCode string
		want         string
	}{
		{
			name:      "无识别码保持历史地址",
			base:      "http://localhost:8204/",
			accountID: 9,
			want:      "http://localhost:8204/api/webhook/telegram/9",
		},
		{
			name:         "识别码走query且路径不变",
			base:         "https://chat.example.com",
			accountID:    42,
			identifyCode: "cid_tg_abc123",
			want:         "https://chat.example.com/api/webhook/telegram/42?identify_code=cid_tg_abc123",
		},
		{
			name:         "识别码特殊字符被正确转义",
			base:         "https://chat.example.com/",
			accountID:    7,
			identifyCode: "cid with space&x=1",
			want:         "https://chat.example.com/api/webhook/telegram/7?identify_code=cid+with+space%26x%3D1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildTelegramPollingWebhookURL(c.base, c.accountID, c.identifyCode); got != c.want {
				t.Fatalf("buildTelegramPollingWebhookURL() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTelegramPollingIdentifyCodeZeroAccount(t *testing.T) {
	if got := telegramPollingIdentifyCode(context.Background(), 0); got != "" {
		t.Fatalf("零账号不得解析出识别码，got %q", got)
	}
}
