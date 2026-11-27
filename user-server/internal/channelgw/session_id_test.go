package channelgw

import "testing"

// TestSessionIDForInboundMapping 闸 13（G-05）：session_id 是稳定的渠道会话键，
// 缺失 conversation_id 时按群/发送者回退，跨消息可复现。
func TestSessionIDForInboundMapping(t *testing.T) {
	cases := []struct {
		name string
		msg  *IngestMessage
		want string
	}{
		{"conversation", &IngestMessage{Channel: "douyin", AccountID: "a1", ConversationID: "c1", SenderID: "u1"}, "douyin:a1:c1"},
		{"group fallback", &IngestMessage{Channel: "wecom", AccountID: "a2", GroupID: "g1", SenderID: "u2", IsGroup: true}, "wecom:a2:g1"},
		{"sender fallback", &IngestMessage{Channel: "xhs", AccountID: "a3", SenderID: "u3"}, "xhs:a3:u3"},
		{"name fallback rejected", &IngestMessage{Channel: "xhs", AccountID: "a3", SenderName: "小李"}, ""},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SessionIDForInbound(tc.msg); got != tc.want {
				t.Errorf("SessionIDForInbound=%q，期望 %q", got, tc.want)
			}
		})
	}

	// 同一会话不同消息必须得到同一 session_id。
	a := &IngestMessage{Channel: "douyin", AccountID: "a1", ConversationID: "c1", SenderID: "u1"}
	b := &IngestMessage{Channel: "douyin", AccountID: "a1", ConversationID: "c1", SenderID: "u1"}
	if SessionIDForInbound(a) != SessionIDForInbound(b) {
		t.Error("同会话两条消息的 session_id 必须一致")
	}
}
