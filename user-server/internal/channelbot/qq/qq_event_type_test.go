package qq

import (
	"testing"

	"hivemtk-user/internal/model"
)

// TestQQ_RobotGroupEvents_ToInbound 机器人入群/退群必须映射成事件而不是被丢弃。
func TestQQ_RobotGroupEvents_ToInbound(t *testing.T) {
	cases := []struct {
		t         string
		eventType string
	}{
		{"GROUP_ADD_ROBOT", model.EventTypeBotInvited},
		{"GROUP_DEL_ROBOT", model.EventTypeBotRemoved},
	}
	for _, c := range cases {
		e := &Event{
			ID: "evt-1",
			T:  c.t,
			D:  []byte(`{"group_openid":"g1","op_member_openid":"u1"}`),
		}
		in := e.ToInbound("42")
		if in == nil {
			t.Fatalf("%s 不应被丢弃", c.t)
		}
		if in.EventType != c.eventType {
			t.Fatalf("%s 事件类型应为 %q，实际 %q", c.t, c.eventType, in.EventType)
		}
		if in.ConversationID != "g1" || in.GroupID != "g1" {
			t.Fatalf("%s 会话/群键丢失：%+v", c.t, in)
		}
		if in.MessageID == "" {
			t.Fatalf("%s 缺幂等键：事件重投会重复入账", c.t)
		}
	}
}
