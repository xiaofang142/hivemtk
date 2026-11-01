package service

// 契约：收件箱删消息必须同时命中「会话 + 消息」两个键。
//
// 坑（2026-11-02 对抗性复核实测）：DELETE /api/inbox/conversations/{A}/messages/{B的消息}
// 只要 A 可见就回 200「删除成功」——消息 id 全局自增，同租户内可以借一个可见会话
// 删掉另一个会话的消息。前端把 conversation 与 message 一起传正是为了限定范围。

import (
	"testing"

	"hivemtk-user/internal/model"
)

func TestHubBelongsToConversation(t *testing.T) {
	conv := &model.InboxConversation{Platform: "wecom", AccountID: "9", CustomerID: "cust-A", ConversationID: "conv-A"}

	cases := []struct {
		name string
		msg  *model.MessageHub
		want bool
	}{
		{"同会话消息", &model.MessageHub{Platform: "wecom", AccountID: "9", ConversationID: "conv-A"}, true},
		{"跨会话消息", &model.MessageHub{Platform: "wecom", AccountID: "9", ConversationID: "conv-B"}, false},
		{"跨账号消息", &model.MessageHub{Platform: "wecom", AccountID: "10", ConversationID: "conv-A"}, false},
		{"跨平台消息", &model.MessageHub{Platform: "telegram", AccountID: "9", ConversationID: "conv-A"}, false},
	}
	for _, c := range cases {
		if got := hubBelongsToConversation(c.msg, conv); got != c.want {
			t.Errorf("%s: want %v, got %v", c.name, c.want, got)
		}
	}

	// 会话没有 conversation_id 时回落客户在收发双方之一
	noConv := &model.InboxConversation{Platform: "wecom", AccountID: "9", CustomerID: "cust-A"}
	if !hubBelongsToConversation(&model.MessageHub{Platform: "wecom", AccountID: "9", SenderID: "cust-A"}, noConv) {
		t.Error("无会话键时应按客户回落判定（发送方）")
	}
	if hubBelongsToConversation(&model.MessageHub{Platform: "wecom", AccountID: "9", SenderID: "cust-B"}, noConv) {
		t.Error("无会话键时别的客户的消息不应通过")
	}
}
