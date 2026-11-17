package channelcaps

import (
	"testing"

	"hivemtk-user/internal/model"
)

// TestManifestEventsAreKnownEvents 事件表只能取 T201 的词表：
// 写错字面量（如 "message.recieve"）会让 T203 分流把它当未知类型，静默按新消息处理。
func TestManifestEventsAreKnownEvents(t *testing.T) {
	for _, m := range All() {
		if len(m.Events) == 0 {
			t.Fatalf("%s 未声明任何事件（至少要有 message.received）", m.Channel)
		}
		for _, ev := range m.Events {
			if !model.IsKnownEventType(ev) {
				t.Fatalf("%s 声明了未登记的事件类型 %q", m.Channel, ev)
			}
		}
	}
}

// TestManifestInteractionFeatureMatchesAnswerer 互动特征必须与实际能力一致：
// 声明了 interaction 却没实现 InteractionAnswerer，客户点按钮就永远转圈。
func TestManifestInteractionFeatureMatchesAnswerer(t *testing.T) {
	asserted := map[string]bool{model.ChannelTelegram: true}
	for _, m := range All() {
		has := false
		for _, f := range m.Features {
			if f == FeatureInteraction {
				has = true
			}
		}
		if has != asserted[m.Channel] {
			t.Fatalf("%s 的 FeatureInteraction=%v，但实际实现状态=%v", m.Channel, has, asserted[m.Channel])
		}
	}
}
