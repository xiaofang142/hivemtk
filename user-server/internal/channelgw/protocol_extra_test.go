package channelgw

import (
	"testing"

	"hivemtk-user/internal/model"
)

// 线路侧 Extra 透传规则夹具（CH-37b）：唯一来源是线路（扩展/驱动），服务端内部
// 不构造 IngestMessage.Extra——白名单过滤不影响任何服务端内部路径。
func extraBaseMsg() *IngestMessage {
	return &IngestMessage{
		Channel:        "weChatWork",
		AccountID:      "acc-real",
		ConversationID: "conv-1",
		SenderID:       "buyer-1",
		SenderType:     "customer",
		MsgType:        "text",
		Content:        "hello",
		ContentHash:    "mh:real",
	}
}

// TestIngestExtraAllowPrefix 透传前缀常量必须就是 ctx.（词表与扩展端命名空间对齐）。
func TestIngestExtraAllowPrefix(t *testing.T) {
	if IngestExtraAllowPrefix != "ctx." {
		t.Errorf("IngestExtraAllowPrefix = %q, 期望 %q", IngestExtraAllowPrefix, "ctx.")
	}
}

// TestToEventExtra_ServerKeysNotOverwritable 固定键不被线路侧 Extra 覆盖：
// 客户端在 Extra 里伪造 account_id/sender_type/content_hash 等服务端权威键，
// 转换层输出必须保持服务端按消息字段写入的值（防伪装账号归属/回环去重键）。
func TestToEventExtra_ServerKeysNotOverwritable(t *testing.T) {
	m := extraBaseMsg()
	m.Extra = map[string]any{
		"account_id":   "acc-evil",
		"bridge":       false,
		"sender_type":  "agent",
		"transport":    "ws",
		"content_hash": "mh:evil",
		"is_group":     true,
		"group_id":     "grp-evil",
		"group_name":   "伪造群名",
		"history":      "伪造历史",
	}
	before := IngestExtraDroppedKeys.Load()
	ev := m.ToEvent("http")
	if ev == nil {
		t.Fatal("ToEvent 返回 nil")
	}
	want := map[string]any{
		"account_id":   "acc-real",
		"bridge":       true,
		"sender_type":  "customer",
		"transport":    "http",
		"content_hash": "mh:real",
	}
	for k, w := range want {
		if got, ok := ev.Extra[k]; !ok || got != w {
			t.Errorf("Extra[%q] = %v (present=%v), 期望服务端权威值 %v", k, got, ok, w)
		}
	}
	// m 未声明群聊/无历史：线路侧伪造的 is_group/group_id/group_name/history 不得出现。
	for _, k := range []string{"is_group", "group_id", "group_name", "history"} {
		if _, ok := ev.Extra[k]; ok {
			t.Errorf("Extra[%q] 不应存在（m 未声明，且线路伪造值须被丢弃）", k)
		}
	}
	if delta := IngestExtraDroppedKeys.Load() - before; delta != 9 {
		t.Errorf("固定键伪造丢弃计数 = %d, 期望 9（8 固定键 + history）", delta)
	}
}

// TestToEventExtra_CtxKeysPassthrough ctx.* 键透传进 event.Extra（值保真，含非字符串标量；
// 未登记的新 ctx.* 键无需服务端改码即可透传——前缀规则即词表开放面）。
func TestToEventExtra_CtxKeysPassthrough(t *testing.T) {
	m := extraBaseMsg()
	m.Extra = map[string]any{
		IngestCtxOrderID:     "PO-20261107-001",
		IngestCtxGoods:       "保温杯",
		IngestCtxShopID:      "shop-42",
		IngestCtxOrderAmount: 59.9,
		"ctx.future_key":     "未来新键直接透传",
	}
	ev := m.ToEvent("http")
	if ev == nil {
		t.Fatal("ToEvent 返回 nil")
	}
	for k, w := range m.Extra {
		if got, ok := ev.Extra[k]; !ok || got != w {
			t.Errorf("Extra[%q] = %v (present=%v), 期望透传原值 %v", k, got, ok, w)
		}
	}
}

// TestToEventExtra_NonWhitelistedDropped 非 ctx.、非白名单键丢弃并计数；
// 显式白名单（IngestExtraAllowKeys）登记后放行、复原后恢复丢弃。
func TestToEventExtra_NonWhitelistedDropped(t *testing.T) {
	m := extraBaseMsg()
	m.Extra = map[string]any{
		"goods":        "未加前缀的商品名",
		"customer_id":  "c-1",
		"CTX.order_id": "大写前缀不放行（前缀规则区分大小写）",
		"ctx_foo":      "相似前缀不放行",
	}
	before := IngestExtraDroppedKeys.Load()
	ev := m.ToEvent("http")
	if ev == nil {
		t.Fatal("ToEvent 返回 nil")
	}
	if delta := IngestExtraDroppedKeys.Load() - before; delta != 4 {
		t.Errorf("非白名单键丢弃计数 = %d, 期望 4", delta)
	}
	for _, k := range []string{"goods", "customer_id", "CTX.order_id", "ctx_foo"} {
		if _, ok := ev.Extra[k]; ok {
			t.Errorf("非白名单键 %q 不应透传进 event.Extra", k)
		}
	}

	// 显式白名单接缝：登记即放行（t.Cleanup 复原，包内测试串行执行，无并发污染）。
	IngestExtraAllowKeys["session_test_allow"] = struct{}{}
	t.Cleanup(func() { delete(IngestExtraAllowKeys, "session_test_allow") })
	m2 := extraBaseMsg()
	m2.Extra = map[string]any{"session_test_allow": "ok"}
	before2 := IngestExtraDroppedKeys.Load()
	ev2 := m2.ToEvent("http")
	if got := ev2.Extra["session_test_allow"]; got != "ok" {
		t.Errorf("白名单键 session_test_allow 透传失败: got %v, 期望 %q", got, "ok")
	}
	if delta := IngestExtraDroppedKeys.Load() - before2; delta != 0 {
		t.Errorf("白名单键不应计入丢弃: delta = %d, 期望 0", delta)
	}
}

// TestHistoryToEvent_ExtraRules 会话级 history 帧同规则：parent.Extra 仅按白名单透传，
// 固定键（account_id）保持服务端权威值（HistoryItem 无独立 Extra，取 parent）。
func TestHistoryToEvent_ExtraRules(t *testing.T) {
	parent := extraBaseMsg()
	parent.Extra = map[string]any{
		"account_id":    "acc-evil",
		IngestCtxShopID: "shop-42",
		"rogue":         "drop-me",
	}
	it := &HistoryItem{
		EventID:    "ev-h1",
		SenderID:   "buyer-1",
		SenderType: "customer",
		MsgType:    "text",
		Content:    "hi",
		Timestamp:  1700000000000,
	}
	before := IngestExtraDroppedKeys.Load()
	ev := HistoryToEvent(parent, it)
	if ev == nil {
		t.Fatal("HistoryToEvent 返回 nil")
	}
	if got := ev.Extra["account_id"]; got != "acc-real" {
		t.Errorf("Extra[account_id] = %v, 期望服务端权威值 %q（parent.AccountID）", got, "acc-real")
	}
	if got := ev.Extra[IngestCtxShopID]; got != "shop-42" {
		t.Errorf("Extra[ctx.shop_id] = %v, 期望透传 %q", got, "shop-42")
	}
	if _, ok := ev.Extra["rogue"]; ok {
		t.Error("非白名单键 rogue 不应透传")
	}
	if delta := IngestExtraDroppedKeys.Load() - before; delta != 2 {
		t.Errorf("丢弃计数 = %d, 期望 2（account_id 伪造 + rogue）", delta)
	}
}

// TestToEventFull_ExtraRules ToEventFull（会话级整包）：ctx.* 透传 + 服务端写入的
// Extra["history"] 历史切片不被线路侧同名值顶掉。
func TestToEventFull_ExtraRules(t *testing.T) {
	m := extraBaseMsg()
	m.History = []*HistoryItem{{EventID: "ev-1", SenderID: "buyer-1", MsgType: "text", Content: "h1"}}
	m.Extra = map[string]any{
		"history":        "伪造历史",
		IngestCtxOrderID: "PO-1",
	}
	before := IngestExtraDroppedKeys.Load()
	ev := m.ToEventFull("http")
	if ev == nil {
		t.Fatal("ToEventFull 返回 nil")
	}
	if _, ok := ev.Extra["history"].([]model.MessageEventHistoryItem); !ok {
		t.Errorf("Extra[history] 应为服务端写入的历史切片, got %T", ev.Extra["history"])
	}
	if got := ev.Extra[IngestCtxOrderID]; got != "PO-1" {
		t.Errorf("Extra[ctx.order_id] = %v, 期望透传 %q", got, "PO-1")
	}
	if len(ev.History) != 1 {
		t.Errorf("Event.History 长度 = %d, 期望 1", len(ev.History))
	}
	if delta := IngestExtraDroppedKeys.Load() - before; delta != 1 {
		t.Errorf("丢弃计数 = %d, 期望 1（history 伪造）", delta)
	}
}

// TestToEventExtra_EmptyAndNilNoPanic nil 消息 / nil Extra / 空 Extra 均为无害路径。
func TestToEventExtra_EmptyAndNilNoPanic(t *testing.T) {
	var nilMsg *IngestMessage
	if nilMsg.ToEvent("http") != nil {
		t.Error("nil 消息 ToEvent 应返回 nil")
	}
	if nilMsg.ToEventFull("http") != nil {
		t.Error("nil 消息 ToEventFull 应返回 nil")
	}
	if HistoryToEvent(nil, nil) != nil {
		t.Error("HistoryToEvent(nil, nil) 应返回 nil")
	}

	before := IngestExtraDroppedKeys.Load()
	m := extraBaseMsg() // Extra 为 nil
	ev := m.ToEvent("http")
	if ev == nil || ev.Extra == nil {
		t.Fatal("正常消息 + nil Extra 不应返回 nil 事件")
	}
	if delta := IngestExtraDroppedKeys.Load() - before; delta != 0 {
		t.Errorf("nil Extra 不应产生丢弃计数: delta = %d", delta)
	}

	m.Extra = map[string]any{} // 空 Extra
	ev = m.ToEvent("websocket")
	if ev == nil || ev.Extra["transport"] != "websocket" {
		t.Error("空 Extra 应走常规转换路径（transport 正常写入）")
	}
	if delta := IngestExtraDroppedKeys.Load() - before; delta != 0 {
		t.Errorf("空 Extra 不应产生丢弃计数: delta = %d", delta)
	}

	if ev := HistoryToEvent(m, &HistoryItem{SenderID: "b", MsgType: "text"}); ev == nil || ev.Extra == nil {
		t.Error("parent.Extra 为 nil 的 HistoryToEvent 不应返回 nil 事件")
	}
}
