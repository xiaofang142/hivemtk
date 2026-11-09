package channelgw

// MD-03 兼容契约测试：一组共享契约用例分别运行真实传输接缝 adapter（PG 8217
// 受控变量）与 fake adapter（contractFakePipeline，只用本地契约类型实现，
// 编译期即证明「测试 adapter 不 import 根 Service 也能实现 IngressPipeline」）。
//
// 不变量（与 service 侧既有语义逐条对齐）：
//   ① 空批次 → 非 nil 结果 + 空 per_event + triggered_ai=false
//   ② 逐事件混合结果 → len(per_event)==len(events)，accepted 与重复结论逐事件可分辨
//   ③ 错误路径 → err 非 nil 且不伪造成功回执（fake 注入错误；真实走已取消 ctx）
//   ④⑤ pending 认领 + ack 重放：认领行带 claimed_at 租约/inflight；首次 ack 翻转
//      1 行、重放 0 影响/duplicate 分类（重复送达=0 的对账依据）
//
// JSON 语义对照：本地契约类型与 service DTO 逐字段 marshal 后按值相等
// （保留缺省/空数组/null 的现协议区别，不靠整串字段顺序相等）。
// 真实持久化/租约仍用 PG；不拿 fake 证明行锁。

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// contractFakePipeline 契约 fake：实现同一 IngressPipeline 接口，签名零根 Service 类型。
type contractFakePipeline struct {
	ingestErr    error
	ingestResult *IngestBatchResult
	history      []*model.MessageEvent
	claimQue     []*model.MessageHub
	acked        map[string]bool
	detailedAck  map[string]bool
}

func newContractFakePipeline() *contractFakePipeline {
	return &contractFakePipeline{
		acked:        map[string]bool{},
		detailedAck:  map[string]bool{},
		ingestResult: &IngestBatchResult{PerEvent: []*IngestEventResult{}},
	}
}

func (f *contractFakePipeline) IngestBatch(_ context.Context, events []*model.MessageEvent) (*IngestBatchResult, error) {
	if f.ingestErr != nil {
		return nil, f.ingestErr
	}
	return f.ingestResult, nil
}

func (f *contractFakePipeline) PersistHistory(_ context.Context, ev *model.MessageEvent, _ string) error {
	f.history = append(f.history, ev)
	return nil
}

func (f *contractFakePipeline) ClaimOutbound(_ context.Context, _, _ string, limit int) ([]*model.MessageHub, error) {
	if len(f.claimQue) == 0 {
		return nil, nil
	}
	n := min(len(f.claimQue), limit)
	out := make([]*model.MessageHub, 0, n)
	for _, h := range f.claimQue[:n] {
		now := time.Now()
		h.Status = "inflight"
		h.ClaimedAt = &now
		h.PushAttempts = 1
		out = append(out, h)
	}
	f.claimQue = f.claimQue[n:]
	return out, nil
}

func (f *contractFakePipeline) AckOutbound(_ context.Context, _, _ string, msgIDs []string) (int, error) {
	n := 0
	for _, id := range msgIDs {
		if !f.acked[id] {
			f.acked[id] = true
			n++
		}
	}
	return n, nil
}

func (f *contractFakePipeline) AckOutboundDetailed(_ context.Context, _, _ string, msgIDs []string, _, terminalStatus string, _ map[string]OutboundAckInput, _ AckOutboundClaimFence) (*AckOutboundResult, error) {
	res := &AckOutboundResult{Items: make([]OutboundAckItem, 0, len(msgIDs))}
	for _, id := range msgIDs {
		if f.detailedAck[id] || f.acked[id] {
			res.DuplicateCount++
			res.Items = append(res.Items, OutboundAckItem{MsgID: id, Status: model.BridgeAckStatusDuplicate})
			continue
		}
		f.detailedAck[id] = true
		if terminalStatus == model.BridgeAckStatusFailed {
			res.FailedItemsCount++
			res.Items = append(res.Items, OutboundAckItem{MsgID: id, Status: model.BridgeAckStatusFailed})
			continue
		}
		res.AckedItemsCount++
		res.Items = append(res.Items, OutboundAckItem{MsgID: id, Status: model.BridgeAckStatusAcked})
	}
	return res, nil
}

// contractHooks 把 adapter 差异（真实=PG 播种+已取消 ctx，fake=预编结果/错误）
// 收敛成钩子，契约用例本体对两者一视同仁。
type contractHooks struct {
	seedMixedEvents  func(t *testing.T) []*model.MessageEvent
	programMixed     func(t *testing.T, p IngressPipeline)
	invokeErr        func(t *testing.T, p IngressPipeline) (any, error)
	seedPendingEvent func(t *testing.T, p IngressPipeline) string
}

const contractAccountID = "acc-contract"

func runIngressPipelineContract(t *testing.T, p IngressPipeline, h contractHooks) {
	t.Helper()
	ctx := context.Background()

	// ① 空批次：非 nil 结果 + 空 per_event + triggered_ai=false。
	res, err := p.IngestBatch(ctx, nil)
	if err != nil {
		t.Fatalf("空批次 IngestBatch 不应失败: %v", err)
	}
	if res == nil {
		t.Fatal("空批次 IngestBatch 返回 nil result")
	}
	if len(res.PerEvent) != 0 || res.TriggeredAI {
		t.Errorf("空批次契约不符: len(PerEvent)=%d TriggeredAI=%v", len(res.PerEvent), res.TriggeredAI)
	}

	// ② 逐事件混合结果：per_event 与 events 等长；新帧 accepted+queued_for_ai，
	// 重放帧 accepted 但 queued_for_ai=false 且 reason 落重复结论词表
	// （与 cs01 重放负例同口径：accepted=true 是幂等回执，不等于再次触发 AI）。
	evs := h.seedMixedEvents(t)
	h.programMixed(t, p)
	mixRes, err := p.IngestBatch(ctx, evs)
	if err != nil {
		t.Fatalf("混合批次 IngestBatch 不应失败: %v", err)
	}
	var gotPerEvent int
	if mixRes != nil {
		gotPerEvent = len(mixRes.PerEvent)
	}
	if mixRes == nil || gotPerEvent != len(evs) {
		t.Fatalf("per_event 应与 events 等长: res=%+v len=%d want=%d", mixRes, gotPerEvent, len(evs))
	}
	var freshAI, replayDup, unexpected int
	for _, pr := range mixRes.PerEvent {
		if pr == nil {
			t.Fatal("per_event 元素不应为 nil")
		}
		switch {
		case pr.Accepted && pr.QueuedForAI:
			freshAI++
		case pr.Accepted && !pr.QueuedForAI && IsDuplicateReason(pr.Reason):
			replayDup++
		default:
			unexpected++
		}
	}
	if freshAI == 0 || replayDup == 0 || unexpected > 0 {
		t.Errorf("混合结果应含 新帧(accepted+queued) 与 重放帧(accepted+dup reason) 各至少 1 条: fresh=%d replay=%d other=%d", freshAI, replayDup, unexpected)
	}

	// ③ 错误路径：err 非 nil 且不伪造成功回执。
	errRes, err := h.invokeErr(t, p)
	if err == nil {
		t.Error("错误路径应返回非 nil err（不静默裸发成功回执）")
	}
	if errRes != nil {
		t.Errorf("错误路径不应返回结果: %+v", errRes)
	}

	// ④⑤ pending 认领 + ack 重放：认领行带 claimed_at 租约/inflight；首次 ack
	// 翻转 1 行、重放 0 影响；详细 ack 重放归 duplicate（重复送达=0 的对账依据）。
	msgID := h.seedPendingEvent(t, p)
	hubs, err := p.ClaimOutbound(ctx, model.ChannelDouyin, contractAccountID, 10)
	if err != nil || len(hubs) == 0 {
		t.Fatalf("认领应至少 1 条: err=%v len=%d", err, len(hubs))
	}
	var got *model.MessageHub
	for _, h := range hubs {
		if h.MsgID == msgID {
			got = h
			break
		}
	}
	if got == nil {
		t.Fatalf("认领行应含播种 msg_id=%s: %+v", msgID, hubs)
	}
	if got.ClaimedAt == nil {
		t.Error("认领返回行 ClaimedAt 不应为空（栅栏凭据随行）")
	}
	if got.Status != "inflight" {
		t.Errorf("认领行 status=%q，期望 inflight", got.Status)
	}
	n1, err := p.AckOutbound(ctx, model.ChannelDouyin, contractAccountID, []string{msgID})
	if err != nil || n1 != 1 {
		t.Fatalf("首次 ack 应翻转 1 行: n=%d err=%v", n1, err)
	}
	n2, err := p.AckOutbound(ctx, model.ChannelDouyin, contractAccountID, []string{msgID})
	if err != nil || n2 != 0 {
		t.Errorf("ack 重放应 0 影响（幂等）: n=%d err=%v", n2, err)
	}
	dRes, err := p.AckOutboundDetailed(ctx, model.ChannelDouyin, contractAccountID, []string{msgID}, "", model.BridgeAckStatusDelivered, nil, nil)
	if err != nil {
		t.Fatalf("详细 ack 重放不应失败: %v", err)
	}
	if len(dRes.Items) != 1 || dRes.Items[0].Status != model.BridgeAckStatusDuplicate {
		t.Errorf("详细 ack 重放应归 duplicate: items=%+v", dRes.Items)
	}
}

// TestIngressPipeline_Contract_RealAdapter 真实接缝 adapter（PG 8217）。
func TestIngressPipeline_Contract_RealAdapter(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	ingress := service.NewInboxIngressService()
	p := NewPipeline(ingress)

	const conv = "conv-contract-real"

	runIngressPipelineContract(t, p, contractHooks{
		seedMixedEvents: func(t *testing.T) []*model.MessageEvent {
			t.Helper()
			// 先单独入站一帧建立去重锚点（跨批去重由进程缓存+库内约束双证），
			// 再让混合批含「新帧 + 该锚点帧重放」→ 逐事件 accepted/duplicate 混合。
			pre := &model.MessageEvent{
				EventID:        "contract-pre-" + t.Name(),
				SessionID:      "sess-contract-pre",
				Channel:        model.ChannelDouyin,
				SenderID:       "cust-contract",
				MsgType:        "text",
				Content:        "contract 去重锚点 " + t.Name(),
				ConversationID: conv,
				Timestamp:      time.Now(),
			}
			if _, err := p.IngestBatch(context.Background(), []*model.MessageEvent{pre}); err != nil {
				t.Fatalf("锚点帧入站失败: %v", err)
			}
			fresh := &model.MessageEvent{
				EventID:        "contract-mix-" + t.Name(),
				SessionID:      "sess-contract-mix",
				Channel:        model.ChannelDouyin,
				SenderID:       "cust-contract",
				MsgType:        "text",
				Content:        "contract 混合结果专用内容 " + t.Name(),
				ConversationID: conv,
				Timestamp:      time.Now(),
			}
			return []*model.MessageEvent{fresh, evDupOf(pre)}
		},
		programMixed: func(t *testing.T, _ IngressPipeline) {},
		invokeErr: func(t *testing.T, p IngressPipeline) (any, error) {
			t.Helper()
			// 真实侧错误路径用已取消 ctx 走认领（DB 往返 ctx 敏感）；批入站路径
			// 的 ctx 沿出属 service 既有语义，本契约不拿它当错误注入点。
			cctx, cancel := context.WithCancel(context.Background())
			cancel()
			rows, err := p.ClaimOutbound(cctx, model.ChannelDouyin, contractAccountID, 10)
			if rows == nil {
				return nil, err
			}
			return rows, err
		},
		seedPendingEvent: func(t *testing.T, _ IngressPipeline) string {
			t.Helper()
			return seedOutbound(t, ingress, model.ChannelDouyin, contractAccountID, conv, "contract claim/ack replay")
		},
	})
}

func evDupOf(ev *model.MessageEvent) *model.MessageEvent {
	cp := *ev
	return &cp
}

// TestIngressPipeline_Contract_FakeAdapter fake 同形运行同一组契约用例。
func TestIngressPipeline_Contract_FakeAdapter(t *testing.T) {
	f := newContractFakePipeline()
	runIngressPipelineContract(t, f, contractHooks{
		seedMixedEvents: func(t *testing.T) []*model.MessageEvent {
			t.Helper()
			return []*model.MessageEvent{{EventID: "contract-mix-f1"}, {EventID: "contract-mix-f2"}}
		},
		programMixed: func(t *testing.T, _ IngressPipeline) {
			t.Helper()
			f.ingestResult = &IngestBatchResult{
				PerEvent: []*IngestEventResult{
					{Accepted: true, QueuedForAI: true, SessionID: "s-fake-1", Reason: "accepted"},
					{Accepted: true, QueuedForAI: false, Reason: "duplicate event id replayed"},
				},
			}
		},
		invokeErr: func(t *testing.T, p IngressPipeline) (any, error) {
			t.Helper()
			f.ingestErr = errPipelineNotConfigured
			r, err := p.IngestBatch(context.Background(), nil)
			if r == nil {
				return nil, err
			}
			return r, err
		},
		seedPendingEvent: func(t *testing.T, _ IngressPipeline) string {
			t.Helper()
			id := "contract-fake-msg-1"
			f.claimQue = append(f.claimQue, &model.MessageHub{MsgID: id, Status: "pending"})
			return id
		},
	})
}

// TestIngressPipeline_Contract_JSONSemanticParity 本地契约类型与 service DTO 的
// JSON 语义对照：按解析后的值比较（不靠整串字段顺序相等），保留缺省/空数组/null
// 的现协议区别。
func TestIngressPipeline_Contract_JSONSemanticParity(t *testing.T) {
	assertSemanticEqual := func(name string, svc, local any) {
		t.Helper()
		sb, err := json.Marshal(svc)
		if err != nil {
			t.Fatalf("%s service 侧 marshal 失败: %v", name, err)
		}
		lb, err := json.Marshal(local)
		if err != nil {
			t.Fatalf("%s 本地侧 marshal 失败: %v", name, err)
		}
		var sm, lm any
		if err := json.Unmarshal(sb, &sm); err != nil {
			t.Fatalf("%s service 侧 unmarshal 失败: %v", name, err)
		}
		if err := json.Unmarshal(lb, &lm); err != nil {
			t.Fatalf("%s 本地侧 unmarshal 失败: %v", name, err)
		}
		if !reflect.DeepEqual(sm, lm) {
			t.Errorf("%s JSON 语义不符:\nservice=%s\nlocal =%s", name, sb, lb)
		}
	}

	// 入站结果：nil PerEvent / 空数组 / 有值（含 nil 元素）三种协议区别都对照。
	assertSemanticEqual("ingress nil per_event",
		&service.InboxIngressBatchResult{},
		&IngestBatchResult{})
	assertSemanticEqual("ingress empty per_event",
		&service.InboxIngressBatchResult{PerEvent: []*service.InboxIngressResult{}},
		&IngestBatchResult{PerEvent: []*IngestEventResult{}})
	assertSemanticEqual("ingress populated per_event",
		&service.InboxIngressBatchResult{
			PerEvent:    []*service.InboxIngressResult{{Accepted: true, HumanLocked: true, QueuedForAI: true, SessionID: "s1", Reason: "r"}, nil},
			TriggeredAI: true,
			Reason:      "batch reason",
		},
		&IngestBatchResult{
			PerEvent:    []*IngestEventResult{{Accepted: true, HumanLocked: true, QueuedForAI: true, SessionID: "s1", Reason: "r"}, nil},
			TriggeredAI: true,
			Reason:      "batch reason",
		})

	// 详细 ack 结果：nil Items / 空数组 / 有值（含 error 缺省）三种协议区别都对照。
	assertSemanticEqual("ack nil items",
		&service.AckOutboundResult{},
		&AckOutboundResult{})
	assertSemanticEqual("ack empty items",
		&service.AckOutboundResult{Items: []service.AckOutboundItem{}},
		&AckOutboundResult{Items: []OutboundAckItem{}})
	assertSemanticEqual("ack populated items",
		&service.AckOutboundResult{
			AffectedCount: 2, AckedItemsCount: 1, FailedItemsCount: 0,
			UnknownItemsCount: 0, DuplicateCount: 1, NotFoundCount: 0, NotInScopeCount: 0,
			Items: []service.AckOutboundItem{{MsgID: "m1", Status: "acked"}, {MsgID: "m2", Status: "duplicate", Error: "late"}},
		},
		&AckOutboundResult{
			AffectedCount: 2, AckedItemsCount: 1, FailedItemsCount: 0,
			UnknownItemsCount: 0, DuplicateCount: 1, NotFoundCount: 0, NotInScopeCount: 0,
			Items: []OutboundAckItem{{MsgID: "m1", Status: "acked"}, {MsgID: "m2", Status: "duplicate", Error: "late"}},
		})

	// 栅栏入参同型：map[string]time.Time 语义在类型转换下不变（序列化同构）。
	fence := AckOutboundClaimFence{"m1": time.Now().UTC().Truncate(time.Microsecond)}
	svcFence := service.AckOutboundClaimFence(fence)
	sb, err := json.Marshal(svcFence)
	if err != nil {
		t.Fatalf("service fence marshal 失败: %v", err)
	}
	lb, err := json.Marshal(fence)
	if err != nil {
		t.Fatalf("local fence marshal 失败: %v", err)
	}
	if string(sb) != string(lb) {
		t.Errorf("fence JSON 不符:\nservice=%s\nlocal =%s", sb, lb)
	}
}
