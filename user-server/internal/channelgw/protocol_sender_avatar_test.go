package channelgw

import (
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// M-2 客户头像链路（2026-11-11）：sender_avatar 与 sender_name 同链路透传 ——
// IngestMessage.SenderAvatar → MessageEvent.SenderAvatar（截到 media_url 同款列宽）；
// HistoryItem.SenderAvatar → MessageEventHistoryItem.SenderAvatar（ToEventFull 拷贝）。
// 空头像（渠道提取不到）保持为空，不报错。

func TestToEvent_SenderAvatarPassthrough(t *testing.T) {
	const u = "https://p3-sign.douyinpic.com/avatar.jpeg?x-expires=1"
	m := &IngestMessage{
		Channel:      "douyin",
		MsgType:      "text",
		Content:      "你好",
		SenderName:   "张三",
		SenderAvatar: u,
		Timestamp:    time.Now().UnixMilli(),
	}
	ev := m.ToEvent("http")
	if ev == nil {
		t.Fatal("ToEvent 返回 nil")
	}
	if ev.SenderAvatar != u {
		t.Fatalf("短头像 URL 必须逐字节透传: got %q want %q", ev.SenderAvatar, u)
	}
}

func TestToEvent_SenderAvatarEmptyStaysEmpty(t *testing.T) {
	m := &IngestMessage{Channel: "jd", MsgType: "text", Content: "hi", Timestamp: time.Now().UnixMilli()}
	ev := m.ToEvent("http")
	if ev == nil {
		t.Fatal("ToEvent 返回 nil")
	}
	if ev.SenderAvatar != "" {
		t.Fatalf("无头像渠道 sender_avatar 必须保持空串: got %q", ev.SenderAvatar)
	}
}

func TestToEvent_SenderAvatarClamped(t *testing.T) {
	long := "https://p3-sign.douyinpic.com/avatar/" + strings.Repeat("a", 600)
	m := &IngestMessage{
		Channel:      "douyin",
		MsgType:      "text",
		Content:      "你好",
		SenderAvatar: long,
		Timestamp:    time.Now().UnixMilli(),
	}
	ev := m.ToEvent("http")
	if got := len([]rune(ev.SenderAvatar)); got != model.MaxMediaURLLen {
		t.Fatalf("sender_avatar 未按 media_url 同款口径截断: %d 字符 (want %d)", got, model.MaxMediaURLLen)
	}
	if !strings.HasPrefix(ev.SenderAvatar, "https://p3-sign.douyinpic.com/avatar/") {
		t.Fatalf("截断应保留前缀: %q", ev.SenderAvatar[:40])
	}
}

func TestHistoryToEvent_SenderAvatarPassthroughAndClamp(t *testing.T) {
	parent := &IngestMessage{Channel: "xhs", ConversationID: "c1"}
	const short = "https://sns-avatar.xhscdn.com/u1.jpg"
	ev := HistoryToEvent(parent, &HistoryItem{MsgType: "text", Content: "hi", SenderAvatar: short, Timestamp: time.Now().UnixMilli()})
	if ev == nil {
		t.Fatal("HistoryToEvent 返回 nil")
	}
	if ev.SenderAvatar != short {
		t.Fatalf("history 轮次头像短 URL 必须逐字节透传: got %q", ev.SenderAvatar)
	}

	long := "https://sns-avatar.xhscdn.com/" + strings.Repeat("b", 600)
	ev2 := HistoryToEvent(parent, &HistoryItem{MsgType: "text", Content: "hi", SenderAvatar: long, Timestamp: time.Now().UnixMilli()})
	if got := len([]rune(ev2.SenderAvatar)); got != model.MaxMediaURLLen {
		t.Fatalf("history 轮次头像未截断: %d 字符 (want %d)", got, model.MaxMediaURLLen)
	}
}

func TestToEventFull_HistorySenderAvatarCopied(t *testing.T) {
	const u = "https://f/av.png"
	m := &IngestMessage{
		Channel:         "xianyu",
		ConversationID:  "c2",
		MsgType:         "text",
		Content:         "hi",
		Timestamp:       time.Now().UnixMilli(),
		SenderAvatar:    "https://f/top.png",
		History: []*HistoryItem{
			{EventID: "e1", MsgType: "text", Content: "hi", SenderAvatar: u, Timestamp: time.Now().UnixMilli()},
		},
	}
	ev := m.ToEventFull("websocket")
	if ev == nil {
		t.Fatal("ToEventFull 返回 nil")
	}
	if len(ev.History) != 1 {
		t.Fatalf("History 拷贝条数错误: %d", len(ev.History))
	}
	if ev.History[0].SenderAvatar != u {
		t.Fatalf("ToEventFull 未拷贝 history 轮次 sender_avatar: got %q", ev.History[0].SenderAvatar)
	}
}
