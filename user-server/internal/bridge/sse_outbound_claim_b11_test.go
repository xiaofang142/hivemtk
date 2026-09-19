package bridge

// 批11 §3.7-2 契约锁：SSE 下推与 SSE 补拉都必须先过同一把服务端认领门。
//
// 断的是两条实际后果，不是形状：
//   - 不认领就推 → 同一条 pending 行 SSE 与轮询各拿一次 → 同一句话打给真实客户两遍（不可逆）
//   - 补拉按 id 游标 → 推送后发送失败的行落在游标之下 → 永不再投 → 客户静默收不到回复

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

type fakeClaimer struct {
	refuse map[uint64]bool
	fail   bool

	calls    []uint64
	timeouts []time.Duration
}

func (c *fakeClaimer) ClaimOutboundForPush(_ context.Context, id uint64, timeout time.Duration) (bool, error) {
	c.calls = append(c.calls, id)
	c.timeouts = append(c.timeouts, timeout)
	if c.fail {
		// 故意「报错的同时还声称抢到了」：真实实现不会这样（条件更新只回一种结论），
		// 但门的职责恰恰是——报错即结论未知，未知就不推。若门写成「先看 claimed 再看 err」，
		// 这条 fake 就会让消息照投，双投重新打开（变异 S4 靠这个才杀得掉）。
		return true, errors.New("认领查询炸了")
	}
	if c.refuse[id] {
		return false, nil
	}
	return true, nil
}

func b11Event(hubID uint64, conv string) SSEEvent {
	return BuildOutboundSSEEvent(OutboundEventData{
		HubID:          hubID,
		MsgID:          "mh:b11-" + conv,
		Platform:       "douyin",
		AccountID:      "acc_b11",
		ConversationID: conv,
		Content:        "批11内容 " + conv,
		MsgType:        "text",
		ReceiverID:     conv,
		CreatedAt:      time.Now(),
	})
}

// withBus 每个用例一套独立总线：GlobalSSEBus 是进程级单例，
// 用例改完必须还原，否则认领器泄漏给同包其它 SSE 用例会互相判成「抢锁失败」。
func withBus(t *testing.T) *SSEBus {
	t.Helper()
	prev := GlobalSSEBus
	bus := NewSSEBus()
	GlobalSSEBus = bus
	t.Cleanup(func() { GlobalSSEBus = prev })
	return bus
}

func TestOutboundPushIsClaimedBeforeDelivery(t *testing.T) {
	bus := withBus(t)
	cl := &fakeClaimer{}
	bus.SetOutboundClaimer(cl)

	ch, cancel := bus.Subscribe("douyin", "acc_b11")
	defer cancel()

	bus.Publish(b11Event(77, "conv_1"))

	if len(cl.calls) != 1 || cl.calls[0] != 77 {
		t.Fatalf("推送前必须以 hub_id=77 认领一次，实际 calls=%v", cl.calls)
	}
	if cl.timeouts[0] != service.InboxOutboundClaimTimeout {
		t.Errorf("认领超时必须与轮询侧同源（两条路径两套超时=重投节奏对不上）: %v", cl.timeouts[0])
	}
	select {
	case ev := <-ch:
		if ev.Data["hub_id"] != uint64(77) || ev.Data["msg_id"] != "mh:b11-conv_1" {
			t.Errorf("投递的事件字段不对: %+v", ev.Data)
		}
	default:
		t.Fatal("认领成功后事件没投给订阅者")
	}
}

func TestOutboundPushDoesNotClaimWithoutSubscriber(t *testing.T) {
	bus := withBus(t)
	cl := &fakeClaimer{}
	bus.SetOutboundClaimer(cl)

	// 没人在线（浏览器关了 / 尚未连上 SSE）
	bus.Publish(b11Event(88, "conv_offline"))

	if len(cl.calls) != 0 {
		t.Fatalf("无人在线还认领 = 把行压进 inflight 等超时，白白给一条本可被轮询立刻取走的消息加一轮延迟；calls=%v", cl.calls)
	}
}

func TestOutboundPushSkippedWhenClaimLost(t *testing.T) {
	bus := withBus(t)
	cl := &fakeClaimer{refuse: map[uint64]bool{99: true}}
	bus.SetOutboundClaimer(cl)

	ch, cancel := bus.Subscribe("douyin", "acc_b11")
	defer cancel()
	bus.Publish(b11Event(99, "conv_taken"))

	if len(cl.calls) != 1 {
		t.Fatalf("应当尝试认领一次: %v", cl.calls)
	}
	select {
	case ev := <-ch:
		t.Fatalf("行已被别的消费者认领（轮询正在发这句），SSE 又推一遍就是双投: %+v", ev)
	default:
	}
}

func TestOutboundPushFailClosedWhenClaimErrors(t *testing.T) {
	bus := withBus(t)
	cl := &fakeClaimer{fail: true}
	bus.SetOutboundClaimer(cl)

	ch, cancel := bus.Subscribe("douyin", "acc_b11")
	defer cancel()
	bus.Publish(b11Event(101, "conv_err"))

	select {
	case ev := <-ch:
		t.Fatalf("认领结果未知时不得推送（未知=可能已被投递）: %+v", ev)
	default:
	}
}

func TestConversationOnlySubscriberStillGetsPush(t *testing.T) {
	bus := withBus(t)
	cl := &fakeClaimer{}
	bus.SetOutboundClaimer(cl)

	// 只订阅会话维度（账号级键无人订阅）：认领门两个维度都要看，否则这类订阅者被饿死
	ch, cancel := bus.SubscribeByConversation("conv_2")
	defer cancel()
	bus.Publish(b11Event(123, "conv_2"))

	if len(cl.calls) != 1 {
		t.Fatalf("纯会话级订阅者在线时应照常认领+推送: calls=%v", cl.calls)
	}
	select {
	case <-ch:
	default:
		t.Fatal("会话级订阅者没收到")
	}
}

func TestNonOutboundEventsBypassClaimGate(t *testing.T) {
	bus := withBus(t)
	cl := &fakeClaimer{}
	bus.SetOutboundClaimer(cl)

	ch, cancel := bus.Subscribe("douyin", "acc_b11")
	defer cancel()
	bus.Publish(SSEEvent{ID: "x", Event: "inbound_message", Data: map[string]any{
		"platform": "douyin", "account_id": "acc_b11",
	}})

	if len(cl.calls) != 0 {
		t.Errorf("认领门只管 new_outbound（入站事件没有出站行可认领）: %v", cl.calls)
	}
	select {
	case <-ch:
	default:
		t.Error("非出站事件被误拦，说明门的事件名判据写错了")
	}
}

func TestClaimGateKeysOnBuilderEventName(t *testing.T) {
	// 门按事件名放行/拦截，而事件名唯一来自 BuildOutboundSSEEvent。
	// 两处任一处改名而另一处不动 = 认领被静默绕过（或所有事件都被拦）。
	if got := b11Event(1, "c").Event; got != EventNewOutbound {
		t.Fatalf("构造器事件名 %q ≠ 认领门判据 %q", got, EventNewOutbound)
	}
	if EventNewOutbound != "new_outbound" {
		t.Errorf("事件名字面量是扩展端 addEventListener 的契约，不得随意改: %q", EventNewOutbound)
	}
}

func TestClaimGateAllowsThroughWithoutClaimer(t *testing.T) {
	bus := withBus(t)
	// 未注入认领器（装配缺件/mock 环境）：退回旧行为放行，不因为缺件把消息全吞掉
	ch, cancel := bus.Subscribe("douyin", "acc_b11")
	defer cancel()
	bus.Publish(b11Event(5, "conv_x"))
	select {
	case <-ch:
	default:
		t.Error("认领器缺件时事件被吞：缺件应当只降级、不致丢消息")
	}
}

func TestOutboundPushRejectsEventWithoutHubID(t *testing.T) {
	bus := withBus(t)
	cl := &fakeClaimer{}
	bus.SetOutboundClaimer(cl)

	ch, cancel := bus.Subscribe("douyin", "acc_b11")
	defer cancel()

	// hub_id=0：不是真实行号（message_hub.id 从 1 起）。拿它去认领必然空转，
	// 更糟的是事件照常投出去 = 一次「服务端从没打算投递」的推送落地。
	// 这条锁的是 Data 契约缺件时的失败方向：宁可不投（超时后按欠交付重投），不可乱投。
	ev := b11Event(0, "conv_noid")
	if ev.Data["hub_id"] != uint64(0) {
		t.Fatalf("夹具不成立：hub_id=%v", ev.Data["hub_id"])
	}
	bus.Publish(ev)

	if len(cl.calls) != 0 {
		t.Errorf("hub_id 非法时不该去问认领门: %v", cl.calls)
	}
	select {
	case got := <-ch:
		t.Fatalf("hub_id 非法的事件被投出去了: %+v", got.Data)
	default:
	}
}

// ---- SSE 补拉侧：按「欠交付」取，且每条逐一行过认领门 ----

type fakeOwedQuerier struct {
	rows []model.MessageHub

	seenTimeout time.Duration
	seenLimit   int
	calls       int
}

func (f *fakeOwedQuerier) FetchOutboundSince(context.Context, string, string, uint64, int) ([]model.MessageHub, error) {
	return nil, nil
}

func (f *fakeOwedQuerier) FetchOutboundUndelivered(_ context.Context, channel, accountID string, claimTimeout time.Duration, limit int) ([]model.MessageHub, error) {
	f.calls++
	f.seenTimeout = claimTimeout
	f.seenLimit = limit
	out := make([]model.MessageHub, 0, len(f.rows))
	for _, r := range f.rows {
		if r.Platform == channel && r.AccountID == accountID {
			out = append(out, r)
		}
	}
	return out, nil
}

func b11Row(id uint, conv string) model.MessageHub {
	return model.MessageHub{ID: id, Platform: "douyin", AccountID: "acc_b11", ConversationID: conv,
		Direction: "outbound", Status: "pending", MsgType: "text", MsgID: "mh:" + conv, Content: "内容 " + conv}
}

func TestBacklogFetchClaimsEveryRow(t *testing.T) {
	bus := withBus(t)
	cl := &fakeClaimer{refuse: map[uint64]bool{2: true}}
	bus.SetOutboundClaimer(cl)

	f := &outboxDBFetcher{}
	q := &fakeOwedQuerier{rows: []model.MessageHub{b11Row(1, "c1"), b11Row(2, "c2"), b11Row(3, "c3")}}
	f.SetQuerier(q)
	f.SetOwedQuerier(q)

	events, newLastID, err := f.FetchOutboxSince(context.Background(), "douyin", "acc_b11", "0")
	if err != nil {
		t.Fatal(err)
	}
	if q.seenTimeout != service.InboxOutboundClaimTimeout {
		t.Errorf("补拉用的超时必须与认领同源: %v", q.seenTimeout)
	}
	if len(events) != 2 || events[0].ID != "1" || events[1].ID != "3" {
		t.Fatalf("认领被拒的行不得出现在补拉结果里（那条正被别人投递）: %v", eventIDs(events))
	}
	if cl.calls[0] != 1 || cl.calls[1] != 2 || cl.calls[2] != 3 {
		t.Errorf("三条都该问一次认领门: %v", cl.calls)
	}
	if newLastID != "3" {
		t.Errorf("游标仍要推进作协议记账, got %q", newLastID)
	}
}

func TestBacklogFetchIgnoresCursorWhenOwedQuerierPresent(t *testing.T) {
	bus := withBus(t)
	bus.SetOutboundClaimer(&fakeClaimer{})

	f := &outboxDBFetcher{}
	q := &fakeOwedQuerier{rows: []model.MessageHub{b11Row(1, "c1"), b11Row(2, "c2")}}
	f.SetQuerier(q)
	f.SetOwedQuerier(q)

	// 客户端游标停在 2（它自认收过 1、2）：本批的立论是「游标不再是读取闸门」——
	// 只要这两条仍欠交付（没 ack），重连必须再拿到它们，否则就是原来那条静默丢消息。
	events, _, err := f.FetchOutboxSince(context.Background(), "douyin", "acc_b11", "2")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("游标之后的欠交付行为 0 条，但欠的是 1、2 两条 → 又回到静默丢消息: %v", eventIDs(events))
	}
}

func TestBacklogFetchLegacyPathWhenOwedMissing(t *testing.T) {
	// 装配缺件（注入方只实现了游标查询）：必须退回旧语义而不是查空即认为没事，
	// 同时 SetOutboxQuerier 已 Error 留痕。这条断的是「缺件时不静默变瞎」。
	bus := withBus(t)

	h := NewBridgeIngestHandler(nil)
	h.SetOutboxQuerier(&fakeOutboxQuerier{rows: b3Rows()})
	if h.outboxFetcher.owed != nil {
		t.Fatal("假查询器没实现 owed，断言环境不成立")
	}
	if bus.claimer != nil {
		t.Error("注入方不实现认领口时总线必须清空认领器（残留旧认领器会把行压进 inflight 却没人投递）")
	}
	events, _, err := h.outboxFetcher.FetchOutboxSince(context.Background(), "douyin", "acc_b3", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("缺件兜底应走游标路径给出 2 条: %v", eventIDs(events))
	}
}

// fullFakeRepo 同时实现三个口，形状对齐 repository.MessageHubRepository。
type fullFakeRepo struct {
	fakeOwedQuerier
}

func (f *fullFakeRepo) ClaimOutboundForPush(_ context.Context, id uint64, _ time.Duration) (bool, error) {
	return true, nil
}

func TestSetOutboxQuerierWiresBusClaimer(t *testing.T) {
	// 装配锁：router 只调 SetOutboxQuerier 这一个入口，认领器是从这里顺带注入总线的。
	// 这一句被删掉后 TestRealRepoSatisfiesOutboundPorts 仍然编译通过（它只做接口赋值），
	// 桥接端也不会报错——只是 SSE 推送又回到「不认领」的旧行为，双投重新打开。
	bus := withBus(t)

	h := NewBridgeIngestHandler(nil)
	q := &fullFakeRepo{}
	q.rows = []model.MessageHub{b11Row(1, "c1")}
	h.SetOutboxQuerier(q)

	if bus.claimer == nil {
		t.Fatal("注入实现认领口的查询器后，总线必须拿到认领器")
	}
	if _, ok := bus.claimer.(*fullFakeRepo); !ok {
		t.Errorf("总线认领器必须是注入的那个实例，got %T", bus.claimer)
	}
	if h.outboxFetcher.owed == nil {
		t.Error("同一个实例实现了 owed，补拉必须走欠交付语义而不是游标")
	}
}

func TestRealRepoSatisfiesOutboundPorts(t *testing.T) {
	// 生产装配锁：router 注给 bridge 的就是 repository.MessageHubRepository。
	// 它一旦少实现任何一个口，SSE 会静默退回「不认领 + 按游标补拉」的旧行为——
	// 那正是本批要收口的两条面重新打开，且没有任何红灯会亮。
	var (
		_ OutboxQuerier       = (*repository.MessageHubRepository)(nil)
		_ OutboxOwedQuerier   = (*repository.MessageHubRepository)(nil)
		_ OutboundPushClaimer = (*repository.MessageHubRepository)(nil)
	)
}

func eventIDs(events []SSEEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.ID)
	}
	return out
}
