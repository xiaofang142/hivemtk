// cs01_douyin_samples_test.go CS-01 固定主链路样本（douyin 蜂桥网页桥）——channelgw 管道边界层。
//
// 服务层样本在 internal/service/cs01_douyin_samples_test.go；本文件钉的是**传输/管道边界**
// 的同一样本契约（真实 PG 8217 受控变量）：
//
//	① 入站批次 per_event 回读契约：accepted / human_locked / queued_for_ai / reason 与
//	   triggered_ai 按实际协议回读（IngestBatch 的委托体真实执行，fake 只换 AITrigger）
//	② 样本①外发腿：获准路径一次有效外发 = outbox 恰 1 条 pending → 认领恰 1 次
//	   （pending→inflight、租约 claimed_at 落行）→ ack 翻 delivered → 重发/重 ack 幂等
//	③ 重放负例（管道判定语义）：同 event_id 原帧重放，per_event.reason 必须落在
//	   IsDuplicateReason 的"服务端明确重复结论"词表里——扩展侧据此不再重报
//
// 样本字段冻结口径与服务层同源（cs01- 前缀、douyin 规范渠道名、wire 帧带
// account_id/session_id/type/timestamp/conversation 全量真实字段）。
package channelgw

import (
	"context"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// —— 冻结样本字段（与服务层 cs01_* 同源；wire 帧层字段名以 IngestMessage json tag 为准）——
const (
	cg01Channel   = "douyin"
	cg01AccountA1 = "cs01-acc-a1"
	cg01ConvA     = "cs01-conv-a1-001"
	cg01CustA     = "cs01-openid-a-001"
	cg01EventA    = "cs01-evt-a-0001"

	// cg01Reply 样本①获准路径的有效外发内容（服务层断言知识标记；此处只对账状态增量）。
	cg01Reply = "CS01-FAQ-5199 标准版 1999 元/年。"
)

// cg01CountingTrigger 可计数 AI 触发 fake：钉住"管道入站 → 恰一次 AI 触发"这一格。
// （生产接线是 bridgeIngressSvc.SetAITrigger(webhookSvc)；fake 只占触发器的位，
// IngestBatch 的其余部分全部真实执行。）
type cg01CountingTrigger struct {
	mu    sync.Mutex
	calls int
	last  map[string]string
}

func (f *cg01CountingTrigger) TriggerInboundAI(_ context.Context, channel, accountID, conversationID, customerID, content, eventID string, _ ...service.TriggerInboundOption) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = map[string]string{
		"channel": channel, "account": accountID, "conv": conversationID,
		"cust": customerID, "content": content, "event": eventID,
	}
}

func (f *cg01CountingTrigger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *cg01CountingTrigger) snapshot() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]string, len(f.last))
	for k, v := range f.last {
		out[k] = v
	}
	return out
}

// cg01SampleFrame 样本①的 wire 帧（douyin 传输真实字段冻结；account_id 在帧顶层，
// 经 ToEvent 落入 Extra["account_id"]——与 persistMessage/triggerAIForEvent 的读法同源）。
func cg01SampleFrame(eventID, content string) *IngestMessage {
	return &IngestMessage{
		EventID:        eventID,
		Channel:        cg01Channel,
		AccountID:      cg01AccountA1,
		ConversationID: cg01ConvA,
		SenderID:       cg01CustA,
		ReceiverID:     cg01AccountA1,
		SenderType:     "customer",
		MsgType:        "text",
		Content:        content,
		Timestamp:      time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC).UnixMilli(),
	}
}

// cg01Pipeline 真实管道 + 真库 + 可计数触发器。注册全局 ingress 句柄
// （DeliverBridgeOutbound 的出库入口按全局句柄取服务，同 bridge_outbound_test 惯例）。
func cg01Pipeline(t *testing.T) (IngressPipeline, *cg01CountingTrigger, *service.InboxIngressService) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	ingress := service.NewInboxIngressService()
	tr := &cg01CountingTrigger{}
	ingress.SetAITrigger(tr)
	// 桥接渠道白名单按注册表同源装配（生产由 app 装配层执行；样本只认 douyin）。
	service.SetBridgeChannels([]string{cg01Channel})
	t.Cleanup(func() { service.SetBridgeChannels(nil) })
	service.SetGlobalInboxIngressService(ingress)
	t.Cleanup(func() { service.SetGlobalInboxIngressService(nil) })
	return NewPipeline(ingress), tr, ingress
}

// TestIngressCS01_PipelineDouyinSampleA_BatchPerEventContract 契约①：
// 管道入站样本①帧 → per_event 回读 + triggered_ai + AI 触发恰一次（身份元组可核对）。
//
// 协议口径：批次入站的 per_event 与单条路径不同——批次路径把「每帧 accepted+queued」
// 逐帧回读、AI 触发按会话组合并在批尾一次执行（HandleIngressBatch 的合并语义），
// 因此 reason 的实际协议值是 "batched; will be merged and triggered at batch end"，
// triggered_ai 才是触发事实的权威回读位。
func TestIngressCS01_PipelineDouyinSampleA_BatchPerEventContract(t *testing.T) {
	p, tr, _ := cg01Pipeline(t)
	ctx := context.Background()

	res, err := p.IngestBatch(ctx, []*model.MessageEvent{cg01SampleFrame(cg01EventA, "你们的标准版套餐多少钱？").ToEvent("ws")})
	if err != nil {
		t.Fatalf("样本①入站失败: %v", err)
	}
	if !res.TriggeredAI {
		t.Error("triggered_ai 应为 true（管道按协议回读触发事实）")
	}
	if len(res.PerEvent) != 1 || res.PerEvent[0] == nil {
		t.Fatalf("per_event[0] 必须非空回读: %+v", res.PerEvent)
	}
	ev := res.PerEvent[0]
	if !ev.Accepted {
		t.Error("accepted = false（期望一条有效入站）")
	}
	if ev.HumanLocked {
		t.Error("样本①新访客不应 human_locked")
	}
	if !ev.QueuedForAI {
		t.Errorf("queued_for_ai = false，reason=%q", ev.Reason)
	}
	if ev.Reason != "batched; will be merged and triggered at batch end" {
		t.Errorf("reason = %q，期望批次路径的实际协议值（合并到批尾触发）", ev.Reason)
	}
	if tr.count() != 1 {
		t.Fatalf("AI 触发应恰 1 次，实际 %d", tr.count())
	}
	if tr.snapshot()["channel"] != cg01Channel || tr.snapshot()["account"] != cg01AccountA1 ||
		tr.snapshot()["conv"] != cg01ConvA || tr.snapshot()["event"] != cg01EventA {
		t.Errorf("触发身份元组不符: %+v", tr.snapshot())
	}
}

// TestPersistBridgeCS01_SampleA_OutboundClaimAckReconciliation 契约②（样本①外发腿）：
// 获准路径一次有效外发 = outbox 恰 1 条 pending → 管道认领恰 1 次（inflight + 租约）→
// ack 翻 delivered → 重复 ack 归 duplicate（审计可对账、重复送达=0）。
func TestPersistBridgeCS01_SampleA_OutboundClaimAckReconciliation(t *testing.T) {
	p, _, ingress := cg01Pipeline(t)
	ctx := context.Background()

	// 获准路径外发：bridge 出库唯一入口（人工/AI 回复同库同口径）。
	if err := service.DeliverBridgeOutbound(ctx, cg01Channel, cg01AccountA1, cg01ConvA, "text", cg01Reply, "cs01-evt-a-0009"); err != nil {
		t.Fatalf("获准外发失败: %v", err)
	}
	pending, err := ingress.ListPendingOutbound(ctx, cg01Channel, cg01AccountA1)
	if err != nil || len(pending) != 1 {
		t.Fatalf("outbox 应恰 1 条 pending，实际 %d (err=%v)", len(pending), err)
	}
	msgID := pending[0].MsgID

	// 认领：pending→inflight，租约列落行（桥端 ack 栅栏的凭据）。
	claimed, err := p.ClaimOutbound(ctx, cg01Channel, cg01AccountA1, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("认领应恰 1 条，实际 %d (err=%v)", len(claimed), err)
	}
	row := claimed[0]
	if row.MsgID != msgID || row.Status != "inflight" {
		t.Errorf("认领行 %s status=%s，期望 %s/inflight", row.MsgID, row.Status, msgID)
	}
	if row.ClaimedAt == nil {
		t.Error("认领返回行必须带 claimed_at（push_attempts/租约栅栏的凭据）")
	}
	if row.SenderID != cg01AccountA1 {
		t.Errorf("bridge 单一 sender：SenderID=%q 期望 ≡ accountID", row.SenderID)
	}
	// 入站/出站消息 ID 不要求相等：出站 msg_id 是内容哈希口径，与原平台 event_id 各记各的。
	if row.MsgID == cg01EventA {
		t.Error("出站 msg_id 不应复用入站 event_id（两套 ID 各记原平台 ID 与内部 msg_id）")
	}

	// ack：inflight→delivered；重复 ack 幂等归 duplicate。
	acked, err := p.AckOutbound(ctx, cg01Channel, cg01AccountA1, []string{msgID})
	if err != nil || acked != 1 {
		t.Fatalf("首次 ack 应翻转 1 行，实际 affected=%d err=%v", acked, err)
	}
	res, err := p.AckOutboundDetailed(ctx, cg01Channel, cg01AccountA1, []string{msgID}, cg01ConvA, model.BridgeAckStatusDelivered, nil, nil)
	if err != nil {
		t.Fatalf("详细 ack 失败: %v", err)
	}
	if res.Items[0].Status != model.BridgeAckStatusDuplicate {
		t.Errorf("重复 ack 应归 duplicate，实际 %s（重复送达=0 的对账依据）", res.Items[0].Status)
	}

	// 发送次数对账：delivered 后 outbox 再认领为空 ⇒ 恰好外发 1 次。
	again, err := p.ClaimOutbound(ctx, cg01Channel, cg01AccountA1, 10)
	if err != nil || len(again) != 0 {
		t.Errorf("delivered 后不得再被认领（重复送达=0），实际 %d 条 err=%v", len(again), err)
	}
}

// TestIngressCS01_ReplayNegative_DuplicateReasonWordlist 契约③（重放负例，管道判定语义）：
// 同 event_id 原帧重放 → 不增有效入站（queued_for_ai=false）、不重复触 AI，
// 且 reason 必须落在 IsDuplicateReason 的"服务端明确重复结论"词表——
// 扩展侧 _markConfirmedFromResponse 只认 accepted||duplicate，一次误判=永久不重报，
// 所以判定只能认结论短语（dup_outcome_b17_test.go 的同一条红线，落到样本上）。
func TestIngressCS01_ReplayNegative_DuplicateReasonWordlist(t *testing.T) {
	p, tr, ingress := cg01Pipeline(t)
	ctx := context.Background()

	// 本例用专用 conv/sender/content：内容窗口去重键与「AI 处理中」标记都在
	// 进程级缓存里（与真库无关），与样本①合同用例共享任何一个键都会让"首帧"
	// 提前撞窗口/撞排他标记——样本独立性由此保证。
	const (
		replayConv    = "cs01-conv-a1-009"
		replayCust    = "cs01-openid-a-replay"
		replayContent = "CS01重放负例专用：查订单发货"
	)
	frame := cg01SampleFrame(cg01EventA, replayContent)
	frame.SenderID = replayCust
	frame.ConversationID = replayConv

	first, err := p.IngestBatch(ctx, []*model.MessageEvent{frame.ToEvent("ws")})
	if err != nil {
		t.Fatalf("首帧失败: %v", err)
	}
	if !first.PerEvent[0].QueuedForAI || tr.count() != 1 {
		t.Fatalf("首帧应触发 AI 1 次: %+v called=%d", first.PerEvent[0], tr.count())
	}

	// 原帧重放（同 event_id 字节级同帧）。
	replay, err := p.IngestBatch(ctx, []*model.MessageEvent{frame.ToEvent("ws")})
	if err != nil {
		t.Fatalf("重放帧失败: %v", err)
	}
	r := replay.PerEvent[0]
	if !r.Accepted {
		t.Error("重放帧应 accepted=true（幂等回执）")
	}
	if r.QueuedForAI || replay.TriggeredAI {
		t.Errorf("重放不得再触 AI: queued=%v triggered=%v", r.QueuedForAI, replay.TriggeredAI)
	}
	if !IsDuplicateReason(r.Reason) {
		t.Errorf("重放 reason %q 不在服务端明确重复结论词表——扩展侧将永久重报或永久闭嘴", r.Reason)
	}
	if tr.count() != 1 {
		t.Errorf("AI 触发应仍为 1 次，实际 %d（不重复触 AI）", tr.count())
	}
	var n int64
	if err := db.GetDB().Model(&model.MessageHub{}).Where("msg_id = ?", cg01EventA).Count(&n).Error; err != nil {
		t.Fatalf("数行失败: %v", err)
	}
	if n != 1 {
		t.Errorf("同 event_id 库内行应仍 1 条（不增有效入站），实际 %d", n)
	}
	_ = ingress
}
