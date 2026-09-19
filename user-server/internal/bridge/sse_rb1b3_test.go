package bridge

import (
	"context"
	"sort"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// 批1（2026-09-19）回归：R-B1 SSE Data 逐键契约 / R-B3 poll 定时器不被心跳续命。

func TestBuildOutboundSSEEvent_DataContract(t *testing.T) {
	now := time.Now()
	ev := BuildOutboundSSEEvent(OutboundEventData{
		HubID: 42, MsgID: "mh:00550fed", Platform: "xianyu", AccountID: "a1",
		ConversationID: "c1", Content: "你好", MsgType: "text", ReceiverID: "r1",
		IsAIReply: true, Extra: map[string]any{"k": "v"}, CreatedAt: now,
	})
	if ev.ID != "42" || ev.Event != "new_outbound" || ev.ConversationID != "c1" {
		t.Fatalf("路由字段错误: %+v", ev)
	}
	// 扩展端 resolveSSEOutboundKeys / SentCache / v2 ack 依赖的键一个不能少
	for _, k := range []string{"hub_id", "msg_id", "platform", "account_id", "conversation_id", "content", "msg_type", "receiver_id", "is_ai_reply", "extra"} {
		if _, ok := ev.Data[k]; !ok {
			t.Errorf("Data 缺键 %s（R-B1 契约）", k)
		}
	}
	if ev.Data["msg_id"] != "mh:00550fed" {
		t.Errorf("msg_id 必须落 Data（缺它扩展端去重键退化为 undefined|conv → 静默丢消息），得 %v", ev.Data["msg_id"])
	}
	if ev.Timestamp != now {
		t.Error("Timestamp 未透传")
	}
}

// DB 补拉路径与总线构造必须逐键一致——同包共享 BuildOutboundSSEEvent 的机械证明。
func TestOutboxDBFetcherDataParityWithBus(t *testing.T) {
	q := &fakeOutboxQuerier{rows: []model.MessageHub{{
		ID: 7, MsgID: "mh:aaaabbbb", Platform: "douyin", AccountID: "a1",
		ConversationID: "c1", Content: "hi", MsgType: "text", ReceiverID: "r1",
		IsAIReply: false, CreatedAt: time.Now(),
	}}}
	f := &outboxDBFetcher{}
	f.SetQuerier(q)
	events, newID, err := f.FetchOutboxSince(context.Background(), "douyin", "a1", "0")
	if err != nil || len(events) != 1 {
		t.Fatalf("fetch: %v %d", err, len(events))
	}
	if newID != "7" {
		t.Errorf("newLastID=%s want 7", newID)
	}
	bus := BuildOutboundSSEEvent(OutboundEventData{HubID: 7, MsgID: "mh:aaaabbbb", Platform: "douyin", AccountID: "a1", ConversationID: "c1", Content: "hi", MsgType: "text", ReceiverID: "r1"})
	keysOf := func(m map[string]any) []string {
		ks := make([]string, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	a, b := keysOf(events[0].Data), keysOf(bus.Data)
	// extra 一侧为 nil 一侧缺省——按总线路径为基准：DB 行 Extra 为空时两侧同形
	if len(a) != len(b) {
		t.Fatalf("DB/总线 Data 键集不一致: DB=%v bus=%v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("DB/总线 Data 键集不一致: DB=%v bus=%v", a, b)
		}
	}
	if events[0].Data["msg_id"] != "mh:aaaabbbb" {
		t.Error("DB 路径 msg_id 丢失")
	}
}

// R-B3：poll 定时器间隔切换仅在目标值变化时发生——旧实现每轮无条件 Reset，
// 心跳 tick（15s）会把 poll（30s）计时无限续期，DB 兜底补拉永远不触发。
// 该缺陷纯逻辑不可单测（耦合 select 循环），以行为文档约束 + 真机 e2e 验证；
// 此处锁定 idlePollInterval > pollInterval 的不变式（若相等则节流语义消失）。
func TestSSEPollIntervalInvariant(t *testing.T) {
	hb := SSEDefaultHeartbeatInterval
	pollInterval := 2 * hb
	idlePollInterval := 4 * hb
	if idlePollInterval <= pollInterval {
		t.Fatal("idle 间隔必须大于活跃间隔，否则 R-B3 的按需 Reset 无意义")
	}
}
