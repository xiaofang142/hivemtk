package channelgw

import (
	"encoding/json"
	"testing"
	"time"
)

// TestIngestMessage_ToEvent 基础字段映射 + SessionID 规则 + Extra 可观测字段。
func TestIngestMessage_ToEvent(t *testing.T) {
	now := time.Now().UnixMilli()
	m := &IngestMessage{
		EventID:        "ev-1",
		Channel:        "douyin",
		AccountID:      "acc-1",
		ConversationID: "conv-1",
		SenderID:       "user-9",
		SenderName:     "客户",
		SenderType:     "customer",
		MsgType:        "text",
		Content:        "你好",
		Timestamp:      now,
		ContentHash:    "mh:abc",
	}
	ev := m.ToEvent(string(TransportHTTP))
	if ev == nil {
		t.Fatal("ToEvent 返回 nil")
	}
	if ev.SessionID != "douyin:acc-1:conv-1" {
		t.Errorf("SessionID = %q, want douyin:acc-1:conv-1", ev.SessionID)
	}
	if ev.EventID != "ev-1" || ev.Content != "你好" || ev.SenderID != "user-9" {
		t.Errorf("基础字段映射错误: %+v", ev)
	}
	if !ev.Timestamp.Equal(time.UnixMilli(now)) {
		t.Errorf("Timestamp 未按毫秒解析: %v", ev.Timestamp)
	}
	if ev.Extra["transport"] != "http" {
		t.Errorf("Extra[transport] = %v, want http", ev.Extra["transport"])
	}
	if ev.Extra["account_id"] != "acc-1" || ev.Extra["bridge"] != true {
		t.Errorf("Extra 缺 account_id/bridge: %+v", ev.Extra)
	}
	if ev.Extra["content_hash"] != "mh:abc" {
		t.Errorf("Extra[content_hash] = %v, want mh:abc", ev.Extra["content_hash"])
	}
}

// TestIngestMessage_ToEvent_ZeroTimestamp 时间戳缺省回退当前时间。
func TestIngestMessage_ToEvent_ZeroTimestamp(t *testing.T) {
	m := &IngestMessage{Channel: "douyin", AccountID: "a", ConversationID: "c"}
	ev := m.ToEvent("")
	if _, ok := ev.Extra["transport"]; ok {
		t.Error("transport 为空时不应写入 Extra")
	}
	if time.Since(ev.Timestamp) > time.Minute {
		t.Errorf("Timestamp=0 应回退当前时间, got %v", ev.Timestamp)
	}
}

// TestIngestMessage_ToEvent_Group 群聊扩展字段。
func TestIngestMessage_ToEvent_Group(t *testing.T) {
	m := &IngestMessage{
		Channel: "kuaishou", AccountID: "a", ConversationID: "c",
		IsGroup: true, GroupID: "g-1", GroupName: "测试群",
	}
	ev := m.ToEvent(string(TransportWebSocket))
	if !ev.IsGroup || ev.GroupID != "g-1" {
		t.Errorf("群聊字段映射错误: %+v", ev)
	}
	if ev.Extra["is_group"] != true || ev.Extra["group_id"] != "g-1" || ev.Extra["group_name"] != "测试群" {
		t.Errorf("群聊 Extra 缺失: %+v", ev.Extra)
	}
}

// TestIngestMessage_ToEventFull History 拷贝与 Extra 冗余。
func TestIngestMessage_ToEventFull(t *testing.T) {
	m := &IngestMessage{
		Channel: "xianyu", AccountID: "a", ConversationID: "c",
		History: []*HistoryItem{
			{EventID: "h-1", Content: "轮次1", SenderType: "customer", Timestamp: 1700000000000},
			nil,
			{EventID: "h-2", Content: "轮次2", SenderType: "self", Direction: "outbound"},
		},
	}
	ev := m.ToEventFull(string(TransportHTTP))
	if len(ev.History) != 2 {
		t.Fatalf("History 长度 = %d, want 2（nil 跳过）", len(ev.History))
	}
	if ev.History[0].EventID != "h-1" || ev.History[1].Direction != "outbound" {
		t.Errorf("History 拷贝错误: %+v", ev.History)
	}
	if _, ok := ev.Extra["history"]; !ok {
		t.Error("Extra[history] 冗余缺失")
	}

	empty := &IngestMessage{Channel: "xianyu", AccountID: "a", ConversationID: "c"}
	if full := empty.ToEventFull("http"); len(full.History) != 0 {
		t.Errorf("无 History 时不应产生历史: %+v", full.History)
	}
}

// TestHistoryToEvent 轮次字段取自 item，会话元数据取自 parent。
func TestHistoryToEvent(t *testing.T) {
	parent := &IngestMessage{
		Channel: "douyin", AccountID: "acc-1", ConversationID: "conv-1",
		SenderType: "customer", IsGroup: true, GroupID: "g-1", GroupName: "群",
		ReceiverID: "recv-parent",
	}
	it := &HistoryItem{
		EventID: "h-9", Content: "出站轮次", Direction: "outbound",
		SenderType: "self", Timestamp: 1700000000000,
	}
	ev := HistoryToEvent(parent, it)
	if ev == nil {
		t.Fatal("HistoryToEvent 返回 nil")
	}
	if ev.Channel != "douyin" || ev.ConversationID != "conv-1" || ev.SessionID != "douyin:acc-1:conv-1" {
		t.Errorf("会话元数据应取自 parent: %+v", ev)
	}
	if ev.EventID != "h-9" || ev.Content != "出站轮次" || ev.Extra["sender_type"] != "self" {
		t.Errorf("轮次字段应取自 item: %+v", ev)
	}
	if ev.ReceiverID != "recv-parent" {
		t.Errorf("ReceiverID = %q, want recv-parent（parent 兜底）", ev.ReceiverID)
	}
	if !ev.IsGroup || ev.Extra["group_name"] != "群" {
		t.Errorf("群元数据应继承 parent: %+v", ev.Extra)
	}

	parent2 := &IngestMessage{Channel: "douyin", AccountID: "a", ConversationID: "conv-2"}
	ev2 := HistoryToEvent(parent2, &HistoryItem{Direction: "outbound"})
	if ev2.ReceiverID != "conv-2" {
		t.Errorf("出站兜底 ReceiverID = %q, want conv-2", ev2.ReceiverID)
	}

	if HistoryToEvent(nil, it) != nil || HistoryToEvent(parent, nil) != nil {
		t.Error("nil 入参应返回 nil")
	}
}

// TestFrame_ProtocolVersionAndEventID 帧协议版本缺省与事件 ID 透传。
func TestFrame_ProtocolVersionAndEventID(t *testing.T) {
	var nilFrame *Frame
	if nilFrame.ProtocolVersion() != ProtocolVersionV1 {
		t.Error("nil 帧应按 v1 处理")
	}
	if (&Frame{}).ProtocolVersion() != ProtocolVersionV1 {
		t.Error("V=0 应按 v1 处理")
	}
	if (&Frame{V: 2}).ProtocolVersion() != ProtocolVersionV2 {
		t.Error("V=2 应透传")
	}
	f := &Frame{Message: &IngestMessage{EventID: "ev-x"}}
	if f.MessageEventID() != "ev-x" {
		t.Errorf("MessageEventID = %q, want ev-x", f.MessageEventID())
	}
	if (&Frame{}).MessageEventID() != "" {
		t.Error("无 Message 时应返回空串")
	}
}

// TestIsDuplicateReason 幂等/拦截原因判定。
// 批15 起判定改成「只认结论前缀」（见 protocol.go 注释），因此原先靠子串命中的两行翻面：
//   - "skip: locked"：人工接管根本不是重复（该分支 Accepted=true 且已落库），
//     判重只是碰巧撞上了 "skip" 这个词。
//   - "record already exists"：不是任何产出方会写的结论短语；真重复走
//     "msg_id already exists…"。留在这儿只会让嗅探表重新变成"任意文案都可能命中"。
func TestIsDuplicateReason(t *testing.T) {
	cases := map[string]bool{
		"":                          false,
		"ok":                        false,
		"msg_id already exists":     true,
		"intercepted by middleware": true,
		"self echo detected":        true,
		"duplicate delivery":        true,
		"skip: locked":              false,
		"record already exists":     false,
	}
	for reason, want := range cases {
		if got := IsDuplicateReason(reason); got != want {
			t.Errorf("IsDuplicateReason(%q) = %v, want %v", reason, got, want)
		}
	}
}

// TestIngestMessage_JSONCompat 线路 JSON tag 与既有前端协议兼容（v2 请求体样例）。
func TestIngestMessage_JSONCompat(t *testing.T) {
	raw := `{
		"event_id": "ev-1", "channel": "douyin", "account_id": "acc-1",
		"conversation_id": "conv-1", "sender_id": "u-9", "sender_type": "customer",
		"msg_type": "text", "content": "hi", "timestamp": 1700000000000,
		"is_group": true, "group_id": "g-1", "content_hash": "mh:x",
		"history": [{"event_id": "h-1", "content": "old", "timestamp": 1700000000000}]
	}`
	var m IngestMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if m.EventID != "ev-1" || m.ContentHash != "mh:x" || !m.IsGroup {
		t.Errorf("JSON tag 不兼容: %+v", m)
	}
	if len(m.History) != 1 || m.History[0].EventID != "h-1" {
		t.Errorf("history JSON tag 不兼容: %+v", m.History)
	}

	out, err := json.Marshal(&m)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	for _, key := range []string{`"event_id"`, `"conversation_id"`, `"content_hash"`, `"is_group"`} {
		if !jsonContains(string(out), key) {
			t.Errorf("序列化结果缺 %s: %s", key, out)
		}
	}
}

// TestFrame_JSONRoundtrip 帧结构序列化/反序列化往返一致。
func TestFrame_JSONRoundtrip(t *testing.T) {
	f := &Frame{
		V: CurrentProtocolVersion, Type: FrameInbound,
		Channel: "douyin", AccountID: "acc-1", TraceID: "t-1",
		Messages: []*IngestMessage{{EventID: "ev-1", ConversationID: "c", MsgType: "text", Content: "x"}},
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("帧序列化失败: %v", err)
	}
	var back Frame
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("帧反序列化失败: %v", err)
	}
	if back.Type != FrameInbound || len(back.Messages) != 1 || back.Messages[0].EventID != "ev-1" {
		t.Errorf("帧往返不一致: %+v", back)
	}
}

func jsonContains(s, key string) bool {
	return len(s) > 0 && len(key) > 0 && indexOf(s, key) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
