// core_test.go channelbot/core 纯函数测试：SecureEqual 常量时比较 + ToMessageEvent 归一化映射
package core

import (
	"testing"
	"time"
)

func TestSecureEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"both empty", "", "", true},
		{"a empty", "", "x", false},
		{"b empty", "x", "", false},
		{"equal", "secret-token", "secret-token", false && true || ("secret-token" == "secret-token")},
		{"differ", "abc", "abd", false},
		{"prefix", "abc", "abcd", false},
	}
	// 简化：直接断言语义
	if !SecureEqual("s3cret", "s3cret") {
		t.Error("SecureEqual(相同值) 应为 true")
	}
	if SecureEqual("s3cret", "s3cret2") {
		t.Error("SecureEqual(前缀不同长) 应为 false")
	}
	if SecureEqual("s3cret", "other!") {
		t.Error("SecureEqual(不同值) 应为 false")
	}
	if SecureEqual("", "") != true {
		t.Error("SecureEqual(双空) 应为 true（a==b）")
	}
	if SecureEqual("", "x") != false {
		t.Error("SecureEqual(空,非空) 应为 false")
	}
	_ = cases
}

func TestInboundMessage_ToMessageEvent(t *testing.T) {
	ts := int64(1700000000)
	m := InboundMessage{
		Platform:       "telegram",
		MessageID:      "msg-1",
		ConversationID: "chat-9",
		SenderID:       "u-1",
		SenderName:     "张三",
		Content:        "hello",
		MsgType:        "text",
		IsGroup:        true,
		GroupID:        "chat-9",
		GroupName:      "三人行",
		Timestamp:      ts,
	}
	ev := m.ToMessageEvent("acc-7")

	if ev.EventID != "msg-1" {
		t.Errorf("EventID = %q, want msg-1", ev.EventID)
	}
	if ev.Channel != "telegram" {
		t.Errorf("Channel = %q, want telegram", ev.Channel)
	}
	if ev.SenderID != "u-1" || ev.SenderName != "张三" {
		t.Errorf("Sender 映射错误: %q/%q", ev.SenderID, ev.SenderName)
	}
	if ev.ReceiverID != "acc-7" {
		t.Errorf("ReceiverID = %q, want acc-7", ev.ReceiverID)
	}
	if ev.SessionID != "telegram:chat-9" {
		t.Errorf("SessionID = %q, want telegram:chat-9", ev.SessionID)
	}
	if ev.Extra["account_id"] != "acc-7" || ev.Extra["channel_msg_id"] != "msg-1" || ev.Extra["group_name"] != "三人行" {
		t.Errorf("Extra 映射错误: %v", ev.Extra)
	}
	if ev.Timestamp.IsZero() || ev.Timestamp.Unix() != ts {
		t.Errorf("Timestamp = %v, want unix %d", ev.Timestamp, ts)
	}
}

func TestInboundMessage_ToMessageEvent_EmptyOptional(t *testing.T) {
	m := InboundMessage{Platform: "qq", MessageID: "m2", SenderID: "u2", Content: "hi", MsgType: "text"}
	ev := m.ToMessageEvent("acc")

	if ev.SessionID != "" {
		t.Errorf("无 ConversationID 时 SessionID 应为空, got %q", ev.SessionID)
	}
	if !ev.Timestamp.IsZero() {
		t.Errorf("无 Timestamp 时应零值, got %v", ev.Timestamp)
	}
	if ev.IsGroup {
		t.Error("默认应非群消息")
	}
}

func TestClientOptions(t *testing.T) {
	c := NewBaseClient(
		WithTimeout(3 * time.Second),
		WithBaseURL("https://example.com"),
	)
	if c.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
}
