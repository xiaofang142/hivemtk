package channelgw

import (
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// 2026-11-10 审计 F2：媒体 URL 在协议转换层（ToEvent / HistoryToEvent）统一截到列宽。
// 上游线路消息 media_url 无长度保障，越界会让 message_hub INSERT 22001 整条失败。
func TestToEvent_MediaURLClamped(t *testing.T) {
	long := "https://p3-sign.douyinpic.com/img/" + strings.Repeat("x", 600)
	m := &IngestMessage{
		Channel:   "douyin",
		MsgType:   "image",
		Content:   "[图片]",
		MediaURL:  long,
		Timestamp: time.Now().UnixMilli(),
	}
	ev := m.ToEvent("http")
	if ev == nil {
		t.Fatal("ToEvent 返回 nil")
	}
	if got := len([]rune(ev.MediaURL)); got > model.MaxMediaURLLen {
		t.Fatalf("ToEvent 未截断 media_url: %d 字符", got)
	}
	if !strings.HasPrefix(ev.MediaURL, "https://p3-sign.douyinpic.com/img/") {
		t.Fatalf("截断应保留前缀: %q", ev.MediaURL[:40])
	}
}

func TestHistoryToEvent_MediaURLClamped(t *testing.T) {
	long := "https://f/" + strings.Repeat("y", 600)
	parent := &IngestMessage{Channel: "xhs", ConversationID: "c1"}
	it := &HistoryItem{MsgType: "image", Content: "[图片]", MediaURL: long, Timestamp: time.Now().UnixMilli()}
	ev := HistoryToEvent(parent, it)
	if ev == nil {
		t.Fatal("HistoryToEvent 返回 nil")
	}
	if got := len([]rune(ev.MediaURL)); got > model.MaxMediaURLLen {
		t.Fatalf("HistoryToEvent 未截断 media_url: %d 字符", got)
	}
}

// 短 URL 不受影响（逐字节不变）
func TestToEvent_MediaURLShortUnchanged(t *testing.T) {
	const u = "https://f/x1.png"
	m := &IngestMessage{Channel: "douyin", MsgType: "image", Content: "[图片]", MediaURL: u, Timestamp: time.Now().UnixMilli()}
	if got := m.ToEvent("http").MediaURL; got != u {
		t.Fatalf("短 URL 必须逐字节不变: got %q", got)
	}
}
