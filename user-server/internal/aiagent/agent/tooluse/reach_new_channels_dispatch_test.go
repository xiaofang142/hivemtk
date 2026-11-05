package tooluse

import (
	"context"
	"testing"
)

// TestDispatchToAdapter_NewBridgeChannels 2026-11 六渠道统一接待：
// 新渠道（含别名）必须被出站分派识别，不能落到 default 的 "unknown channel"。
func TestDispatchToAdapter_NewBridgeChannels(t *testing.T) {
	adapter := &mockReachAdapter{}
	cases := []string{
		"jd", "jd_web", "jingdong",
		"bilibili", "bili", "bilibili_web",
		"weibo", "wb", "weibo_web",
		"personal_wx", "wx_personal", "wechat_personal",
	}
	for _, ch := range cases {
		req := &ReachSendRequest{
			Channel:     ch,
			AccountID:   "acc-1",
			RecipientID: "peer-1",
			MsgType:     "text",
			Content:     "hello",
		}
		if _, err := dispatchToAdapter(context.Background(), adapter, req); err != nil {
			t.Errorf("dispatchToAdapter(%q) error = %v, want nil", ch, err)
		}
	}

	if _, err := dispatchToAdapter(context.Background(), adapter, &ReachSendRequest{Channel: "unknown-channel"}); err == nil {
		t.Error("unknown-channel 应返回 unknown channel 错误")
	}
}
