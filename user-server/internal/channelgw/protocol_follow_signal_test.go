package channelgw

import (
	"testing"
)

// M-6（2026-11-11）：关注关系信号键必须在线路 Extra 显式白名单内——扩展端
// parseMessageItem 命中「互相关注/你还不是对方好友/followed you」等系统文案或
// bilibili「未关注人消息」tab 归属时随消息上报，服务端据此登记首触事件。
// 键名与扩展端 user-web/bridge/src/core/follow-signal.js 逐字一致。
func TestIngestExtraAllowKeys_FollowSignalKeys(t *testing.T) {
	for _, key := range []string{"new_user_unfollowed", "new_user_signal"} {
		if _, ok := IngestExtraAllowKeys[key]; !ok {
			t.Errorf("白名单缺 %q：识别信号将被线路侧丢弃，运营观测与自动关注策略全部落空", key)
		}
		if !allowIngestExtraKey(key) {
			t.Errorf("allowIngestExtraKey(%q) = false，期望 true", key)
		}
	}
	// 固定键仍不可覆盖（白名单放行不改变服务端权威键语义）
	for _, key := range []string{"account_id", "bridge", "sender_type"} {
		if allowIngestExtraKey(key) {
			t.Errorf("固定键 %q 仍应被拒（服务端权威写入）", key)
		}
	}
}

// 透传全链路：ToEvent 后 event.Extra 保留信号键原值（布尔 + 来源字符串）。
func TestToEvent_ExtraFollowSignalPassthrough(t *testing.T) {
	m := extraBaseMsg()
	m.Extra = map[string]any{
		"new_user_unfollowed": true,
		"new_user_signal":     "peer_followed_you",
		"rogue":               "drop-me",
	}
	ev := m.ToEvent("http")
	if ev == nil {
		t.Fatal("ToEvent 返回 nil")
	}
	if got, ok := ev.Extra["new_user_unfollowed"]; !ok || got != true {
		t.Errorf("Extra[new_user_unfollowed] = %v (present=%v)，期望 true 原值透传", got, ok)
	}
	if got, ok := ev.Extra["new_user_signal"]; !ok || got != "peer_followed_you" {
		t.Errorf("Extra[new_user_signal] = %v (present=%v)，期望 peer_followed_you 原值透传", got, ok)
	}
	if _, ok := ev.Extra["rogue"]; ok {
		t.Errorf("非白名单键 rogue 不应透传")
	}
}

// 会话级 history 帧：parent.Extra 的信号键同规则透传（与轮询路径一致）。
func TestHistoryToEvent_ExtraFollowSignalPassthrough(t *testing.T) {
	parent := extraBaseMsg()
	parent.Extra = map[string]any{
		"new_user_unfollowed": true,
		"new_user_signal":     "unfollowed_tab",
	}
	it := &HistoryItem{
		EventID:    "ev-m6-h",
		SenderID:   "buyer-m6",
		SenderType: "customer",
		MsgType:    "text",
		Content:    "hi",
		Timestamp:  1700000000000,
	}
	ev := HistoryToEvent(parent, it)
	if ev == nil {
		t.Fatal("HistoryToEvent 返回 nil")
	}
	if got, ok := ev.Extra["new_user_unfollowed"]; !ok || got != true {
		t.Errorf("history 帧 Extra[new_user_unfollowed] = %v (present=%v)，期望透传", got, ok)
	}
	if got, ok := ev.Extra["new_user_signal"]; !ok || got != "unfollowed_tab" {
		t.Errorf("history 帧 Extra[new_user_signal] = %v (present=%v)，期望透传", got, ok)
	}
}
