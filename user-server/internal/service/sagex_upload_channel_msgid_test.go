package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// SageX 单事件构造函数必须写顶层 channel_msg_id：值取带 installation 隔离的源消息身份
// （与 EventID 同值），入站 channelMsgIDOf 据此跳过内容窗口去重；Extra.sagex.event_id
// 仍保留原始上报 eventID，不影响该字段既有语义。
func TestSageXBuildMessageEvent_ChannelMsgID(t *testing.T) {
	inst := &model.SageXInstallation{ID: 7, TenantID: 1, AccountID: "acc"}
	const (
		installationID = "inst-abc"
		sourceEventID  = "e-123"
	)
	ev := &SageXEventItem{
		EventID:      sourceEventID,
		Conversation: SageXConversation{Type: "private", Title: "张三"},
		Message:      SageXMessage{Sender: "张三", Text: "你好，我想咨询"},
	}
	got, err := (&SageXUploadService{}).buildMessageEvent(inst, installationID, "batch-1", "qq", ev)
	if err != nil {
		t.Fatalf("构造单事件失败: %v", err)
	}

	want := "sagex:" + installationID + ":" + sourceEventID
	if got.EventID != want {
		t.Errorf("单事件 EventID = %q，期望带 installation 隔离的源身份 %q", got.EventID, want)
	}
	raw, ok := got.Extra["channel_msg_id"].(string)
	if !ok || raw == "" {
		t.Fatalf("单事件顶层 channel_msg_id 必须是非空字符串，实际 Extra=%v", got.Extra)
	}
	if raw != want {
		t.Errorf("单事件 channel_msg_id = %q，期望 %q（与源消息身份一致）", raw, want)
	}
	if inner, ok := got.Extra["sagex"].(map[string]any); !ok || inner["event_id"] != sourceEventID {
		t.Errorf("Extra.sagex.event_id 应保留原始上报值 %q，实际 Extra=%v", sourceEventID, got.Extra)
	}
	if seen := channelMsgIDOf(got); seen != want {
		t.Errorf("channelMsgIDOf 应读到 %q，实际 %q ⇒ 入站仍会走内容窗口去重", want, seen)
	}
}

// 数组规范事件逐条都要有顶层 channel_msg_id：与各自源消息级 EventID 同值、
// 带 installation 隔离且重发稳定，channelMsgIDOf 逐条可读。
func TestSageXBuildNormalizedMessageEvents_ChannelMsgID(t *testing.T) {
	inst := &model.SageXInstallation{ID: 7, TenantID: 1, AccountID: "acc"}
	const installationID = "inst-abc"
	primary := &model.MessageEvent{
		EventID:        "sagex:" + installationID + ":e1",
		SessionID:      "s1",
		Channel:        "qq",
		Content:        "通知正文",
		ConversationID: "conv",
		Timestamp:      time.UnixMilli(1000),
		Extra:          map[string]any{},
	}
	ev := &SageXEventItem{
		EventID: "e1",
		RawNotification: SageXRawNotification{
			Messages: []SageXStyleMessage{
				{Text: "甲：我要买", Timestamp: 1000, SenderKey: "member-a", SenderName: "甲"},
				{Text: "通知正文", Timestamp: 1000, SenderKey: "member-a"}, // 与正文重叠，应跳过
			},
			HistoricMessages: []SageXStyleMessage{
				{Text: "历史消息", Timestamp: 900, SenderKey: "member-b", SenderName: "乙"},
			},
		},
	}
	svc := &SageXUploadService{}
	events := svc.buildNormalizedMessageEvents(inst, installationID, "batch-1", "qq", ev, primary)
	if len(events) != 2 {
		t.Fatalf("应生成 2 条规范事件，实际 %d", len(events))
	}
	prefix := "sagex:" + installationID + ":src:"
	for i, event := range events {
		if !strings.HasPrefix(event.EventID, prefix) {
			t.Errorf("第 %d 条事件 EventID = %q，期望前缀 %q", i+1, event.EventID, prefix)
		}
		raw, ok := event.Extra["channel_msg_id"].(string)
		if !ok || raw == "" {
			t.Fatalf("第 %d 条规范事件顶层 channel_msg_id 必须是非空字符串，实际 Extra=%v", i+1, event.Extra)
		}
		if raw != event.EventID {
			t.Errorf("第 %d 条 channel_msg_id = %q，期望与源消息身份 EventID %q 一致", i+1, raw, event.EventID)
		}
		if seen := channelMsgIDOf(event); seen != raw {
			t.Errorf("第 %d 条 channelMsgIDOf = %q，期望 %q", i+1, seen, raw)
		}
	}

	// 同一通知重发身份不变：既有按 msg_id 幂等的语义不受影响。
	again := svc.buildNormalizedMessageEvents(inst, installationID, "batch-1", "qq", ev, primary)
	if len(again) != len(events) {
		t.Fatalf("重发应生成同样条数，实际 %d vs %d", len(again), len(events))
	}
	for i := range events {
		if again[i].Extra["channel_msg_id"] != events[i].Extra["channel_msg_id"] {
			t.Errorf("第 %d 条重发后 channel_msg_id 漂移: %v -> %v",
				i+1, events[i].Extra["channel_msg_id"], again[i].Extra["channel_msg_id"])
		}
	}
}

// sagexCountingCache 记录内容窗口键的 SetNX 次数；同键第二次返回 false 模拟已被占用。
// 内嵌 cache.Cache 只为满足接口，本路径只调用 SetNX。
type sagexCountingCache struct {
	cache.Cache
	setnxCalls int
	seen       map[string]bool
}

func (c *sagexCountingCache) SetNX(_ context.Context, key string, _ any, _ time.Duration) (bool, error) {
	c.setnxCalls++
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	if c.seen[key] {
		return false, nil
	}
	c.seen[key] = true
	return true, nil
}

// 同会话同发送者连发两条相同内容的 SageX 合法消息，第二条不得被入口内容窗口吞掉：
// 修复前 EventID 不同但没有 channel_msg_id，第二条命中同一把内容键被判重复。
func TestSageXChannelMsgID_SkipsContentWindowDedup(t *testing.T) {
	inst := &model.SageXInstallation{ID: 7, TenantID: 1, AccountID: "acc"}
	build := func(eventID string) *model.MessageEvent {
		event, err := (&SageXUploadService{}).buildMessageEvent(inst, "inst-abc", "batch-1", "qq", &SageXEventItem{
			EventID: eventID,
			Message: SageXMessage{Sender: "张三", Text: "同一句话再说一遍"},
		})
		if err != nil {
			t.Fatalf("构造事件 %s 失败: %v", eventID, err)
		}
		// 只验内容窗口那一维；会话维度由 ingress 既有测试覆盖。
		event.ConversationID = ""
		return event
	}
	first, second := build("e-1"), build("e-2")
	if first.Content != second.Content || first.SenderName != second.SenderName {
		t.Fatal("夹具失效：两条消息的发送者与内容必须相同")
	}

	fakeCache := &sagexCountingCache{}
	ingress := &InboxIngressService{hubRepo: &repository.MessageHubRepository{}, cache: fakeCache}
	for i, event := range []*model.MessageEvent{first, second} {
		decision, err := ingress.interceptInbound(context.Background(), event)
		if err != nil {
			t.Fatalf("第 %d 条入站判定失败: %v", i+1, err)
		}
		if decision.Blocked {
			t.Fatalf("第 %d 条合法消息被内容窗口吞掉: %s", i+1, decision.Reason)
		}
	}
	if fakeCache.setnxCalls != 0 {
		t.Errorf("带 channel_msg_id 的事件不应占内容窗口键，实际 SetNX %d 次", fakeCache.setnxCalls)
	}
}
