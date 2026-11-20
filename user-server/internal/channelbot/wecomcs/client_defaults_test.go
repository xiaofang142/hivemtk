package wecomcs

import (
	"testing"

	"hivemtk-user/internal/channelbot/core"
)

func TestNewClient_DefaultAndOverrideURL(t *testing.T) {
	if got := NewClient().BaseURL; got != "https://qyapi.weixin.qq.com" {
		t.Errorf("微信客服默认 API 地址应生效，实际 %q", got)
	}
	if got := NewClient(core.WithBaseURL("http://127.0.0.1:12345")).BaseURL; got != "http://127.0.0.1:12345" {
		t.Errorf("显式测试地址应覆盖默认 API 地址，实际 %q", got)
	}
}
