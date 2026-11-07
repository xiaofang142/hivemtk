package channelgw

// CH-37b 刀4（R2/R6）：WS Frame.Status=unknown 真翻转入 unknown 终态。
//
// 刀2 时 unknown 是「拒翻转 + Warn + FrameAckUnknownStatusCount 打点」的协议错误；
// 刀4 起 unknown 是合法终态（「已提交但结果不明」）：
//   - 契约①：Frame.Status="unknown" → 走 pipeline 详细 ack，terminalStatus=unknown（fake pipeline 路由）
//   - 契约②：真 PG e2e —— WS 帧 → 真实 pipeline → 行落 status='unknown' + push_error='ack_unknown'
//   - 契约③：legacy 仅-delivered 帧（Status 空）零变化（仍走 legacy AckOutbound）
//   - 契约④：真非法值（"sent" 等非三值）仍按协议错误拒翻转 + FrameAckUnknownStatusCount 打点

import (
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// TestWS_AckFrame_StatusUnknown_RoutesDetailedAck 契约①：unknown 帧走详细 ack，
// terminalStatus 原样透传 unknown，不冒充 legacy delivered。
func TestWS_AckFrame_StatusUnknown_RoutesDetailedAck(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-unk-route"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: model.ChannelDouyin, AccountID: "acc-unk-route",
		MsgIDs: []string{"m-u1", "m-u2"}, Status: model.BridgeAckStatusUnknown,
	})

	calls := waitDetailedAck(t, fp, 1)
	c := calls[0]
	if c.terminalStatus != model.BridgeAckStatusUnknown {
		t.Errorf("terminalStatus = %q, want unknown", c.terminalStatus)
	}
	if len(c.msgIDs) != 2 || c.msgIDs[0] != "m-u1" || c.msgIDs[1] != "m-u2" {
		t.Errorf("msgIDs = %v, want [m-u1 m-u2]", c.msgIDs)
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.acked) != 0 {
		t.Errorf("unknown 帧不应走 legacy AckOutbound: acked=%v", fp.acked)
	}
}

// TestWS_AckFrame_Unknown_RealFlip_E2E 契约②：WS ack 帧（status=unknown）经真实 pipeline
// 落库——行翻转为 status='unknown' 且 push_error='ack_unknown'（真 PG 8217）。
// 渠道用 model.ChannelDouyin（Default 注册表 WS 双传输；douyin_web 仅 HTTP 注册）。
func TestWS_AckFrame_Unknown_RealFlip_E2E(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	ingress := service.NewInboxIngressService()
	srv := newTestServer(t, NewPipeline(ingress))
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-unk-e2e"))

	msgID := seedOutbound(t, ingress, model.ChannelDouyin, "acc-unk-e2e", "conv-unk-e2e", "unknown e2e content")

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: model.ChannelDouyin, AccountID: "acc-unk-e2e",
		MsgIDs: []string{msgID}, Status: model.BridgeAckStatusUnknown,
	})

	// readPump 异步处理：轮询等翻转，3s 兜底。
	deadline := time.Now().Add(3 * time.Second)
	for {
		h := hubRow(t, msgID)
		if h.Status == model.BridgeAckStatusUnknown {
			if h.PushError != "ack_unknown" {
				t.Errorf("push_error = %q, want ack_unknown（unknown 来源痕迹）", h.PushError)
			}
			if h.SentAt.IsZero() {
				t.Errorf("sent_at 不应为零（与桥端 ack failed 路径同口径照写）")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("unknown 翻转未到达（3s）: status=%q", h.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestWS_AckFrame_LegacyEmptyStatus_Unchanged 契约③：Status 空的 legacy 帧仍走
// legacy AckOutbound（delivered），刀4 不改变 legacy 行为。
func TestWS_AckFrame_LegacyEmptyStatus_Unchanged(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-unk-legacy"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: model.ChannelDouyin, AccountID: "acc-unk-legacy",
		MsgIDs: []string{"m-legacy-1"},
	})

	acked := waitLegacyAcked(t, fp, 1)
	if len(acked) != 1 || acked[0] != "m-legacy-1" {
		t.Errorf("legacy ack = %v, want [m-legacy-1]", acked)
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.detailed) != 0 {
		t.Errorf("legacy 帧不应走详细 ack: detailed=%v", fp.detailed)
	}
}

// TestWS_AckFrame_BogusStatus_StillRejected 契约④：非三值（"sent"）仍是协议错误——
// 拒翻转 + FrameAckUnknownStatusCount 打点；"unknown" 本身不再计入该打点。
func TestWS_AckFrame_BogusStatus_StillRejected(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-unk-bogus"))

	before := FrameAckUnknownStatusCount.Load()
	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: model.ChannelDouyin, AccountID: "acc-unk-bogus",
		MsgIDs: []string{"m-bogus-1"}, Status: "sent",
	})

	deadline := time.Now().Add(3 * time.Second)
	for {
		if FrameAckUnknownStatusCount.Load() > before {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("非法 Status 未被打点（FrameAckUnknownStatusCount 未增加）")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// 快照即解锁（不得持锁跨 waitDetailedAck：fake 的 detailed 记录也要拿同一把锁，持锁等待=死锁）。
	fp.mu.Lock()
	sawLeak := len(fp.detailed) != 0 || len(fp.acked) != 0
	fp.mu.Unlock()
	if sawLeak {
		t.Errorf("非法 Status 不得翻转任何行: detailed=%v acked=%v", fp.detailed, fp.acked)
	}

	// 对照：unknown 是合法值，不计入「非法 Status」打点。
	before = FrameAckUnknownStatusCount.Load()
	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: model.ChannelDouyin, AccountID: "acc-unk-bogus",
		MsgIDs: []string{"m-bogus-2"}, Status: model.BridgeAckStatusUnknown,
	})
	_ = waitDetailedAck(t, fp, 1)
	if got := FrameAckUnknownStatusCount.Load(); got != before {
		t.Errorf("unknown 是合法终态，不应计入 FrameAckUnknownStatusCount: before=%d after=%d", before, got)
	}
}
