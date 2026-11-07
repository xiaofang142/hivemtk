package channelgw

// CH-37b 刀2：WS Frame.Status 消费测试（fake pipeline，无需 DB）。
//
// 覆盖契约④/⑤：
//   - Frame.Status="failed" → 走 pipeline 详细 ack 的 failed 路径（逐项分类，行级终态）
//   - Frame.Status 空 / "delivered" → legacy AckOutbound 路径，行为零变化
//   - Frame.Status 未知值 → 协议错误：拒翻转 + Warn + FrameAckUnknownStatusCount 打点
//   - outbound_reply 帧透出认领租约 claimed_at/push_attempts（既有列，栅栏锚点随认领下发）

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// waitDetailedAck 等待 readPump 异步处理 ack 帧后 fake 记录到 detailed 调用。
func waitDetailedAck(t *testing.T, fp *fakePipeline, n int) []detailedAckCall {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		fp.mu.Lock()
		calls := append([]detailedAckCall(nil), fp.detailed...)
		acked := append([]string(nil), fp.acked...)
		fp.mu.Unlock()
		if len(calls) >= n {
			return calls
		}
		if time.Now().After(deadline) {
			t.Fatalf("详细 ack 未到达: detailed=%d acked=%v", len(calls), acked)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitLegacyAcked(t *testing.T, fp *fakePipeline, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		fp.mu.Lock()
		calls := append([]detailedAckCall(nil), fp.detailed...)
		acked := append([]string(nil), fp.acked...)
		fp.mu.Unlock()
		if len(acked) >= n {
			return acked
		}
		if time.Now().After(deadline) {
			t.Fatalf("legacy ack 未到达: acked=%d detailed=%d", len(acked), len(calls))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestWS_AckFrame_StatusFailed_RoutesDetailedAck 契约④：failed 帧走详细 ack failed 路径。
func TestWS_AckFrame_StatusFailed_RoutesDetailedAck(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-status-1"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: model.ChannelDouyin, AccountID: "acc-status-1",
		MsgIDs: []string{"m-f1", "m-f2"}, Status: model.BridgeAckStatusFailed,
	})

	calls := waitDetailedAck(t, fp, 1)
	c := calls[0]
	if c.terminalStatus != model.BridgeAckStatusFailed {
		t.Errorf("terminalStatus = %q, want failed", c.terminalStatus)
	}
	if len(c.msgIDs) != 2 || c.msgIDs[0] != "m-f1" || c.msgIDs[1] != "m-f2" {
		t.Errorf("msgIDs = %v, want [m-f1 m-f2]", c.msgIDs)
	}
	if c.channel != model.ChannelDouyin || c.accountID != "acc-status-1" {
		t.Errorf("channel/account = %s/%s, want douyin/acc-status-1", c.channel, c.accountID)
	}
	if c.fenceLen != 0 {
		t.Errorf("WS 帧不带栅栏, fenceLen = %d, want 0", c.fenceLen)
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.acked) != 0 {
		t.Errorf("failed 帧不应走 legacy AckOutbound: acked=%v", fp.acked)
	}
}

// TestWS_AckFrame_StatusEmptyKeepsLegacyPath 契约④/⑤：不带 Status = delivered，legacy 路径零变化。
func TestWS_AckFrame_StatusEmptyKeepsLegacyPath(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-status-2"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: model.ChannelDouyin, AccountID: "acc-status-2",
		MsgIDs: []string{"m-e1"}, // 无 Status 字段（legacy 客户端格式）
	})

	acked := waitLegacyAcked(t, fp, 1)
	if len(acked) != 1 || acked[0] != "m-e1" {
		t.Errorf("acked = %v, want [m-e1]", acked)
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.detailed) != 0 {
		t.Errorf("legacy 帧不应进详细 ack 路径: detailed=%+v", fp.detailed)
	}
}

// TestWS_AckFrame_StatusDeliveredKeepsLegacyPath 契约④/⑤：显式 delivered 与空值同走 legacy 路径。
func TestWS_AckFrame_StatusDeliveredKeepsLegacyPath(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-status-3"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: model.ChannelDouyin, AccountID: "acc-status-3",
		MsgIDs: []string{"m-d1", "m-d2"}, Status: model.BridgeAckStatusDelivered,
	})

	acked := waitLegacyAcked(t, fp, 2)
	if len(acked) != 2 {
		t.Errorf("acked = %v, want 2 条", acked)
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.detailed) != 0 {
		t.Errorf("delivered 帧应保持 legacy 路径（行为零变化）: detailed=%+v", fp.detailed)
	}
}

// TestWS_AckFrame_UnknownStatusRejectedNoFlip 契约④：unknown Status = 协议错误——
// 拒翻转（既不 legacy 也不详细 ack）+ FrameAckUnknownStatusCount 打点。
func TestWS_AckFrame_UnknownStatusRejectedNoFlip(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-status-4"))

	before := FrameAckUnknownStatusCount.Load()
	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: model.ChannelDouyin, AccountID: "acc-status-4",
		MsgIDs: []string{"m-u1"}, Status: "sent", // 非法：既非 delivered/failed 也非空
	})

	// 给 readPump 足够时间处理；unknown 帧必须不产生任何翻转。
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		fp.mu.Lock()
		done := len(fp.acked) == 0 && len(fp.detailed) == 0
		fp.mu.Unlock()
		if !done {
			t.Fatal("unknown Status 不应触发任何 ack 路径")
		}
		time.Sleep(20 * time.Millisecond)
	}
	after := FrameAckUnknownStatusCount.Load()
	if after != before+1 {
		t.Errorf("FrameAckUnknownStatusCount 增量 = %d, want 1 (before=%d after=%d)", after-before, before, after)
	}
}

// TestWS_OutboundReply_CarriesClaimLease 契约②的 WS 线路半边：
// outbound_reply 帧载荷透出认领租约 claimed_at/push_attempts（既有列）；nil 租约字段省略（旧客户端兼容）。
func TestWS_OutboundReply_CarriesClaimLease(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-lease"))

	lease := time.Now().Truncate(time.Millisecond)
	fp.mu.Lock()
	fp.claimQue = append(fp.claimQue, &model.MessageHub{
		MsgID: "m-lease-1", ConversationID: "conv-lease",
		Content: "带租约回复", MsgType: "text",
		ClaimedAt:    &lease,
		PushAttempts: 3,
	})
	fp.mu.Unlock()

	// 原始字节断言（ReadMessage 绕过结构体解码，验证线路 JSON 键位）。
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("读 outbound_reply 原始帧失败: %v", err)
	}
	if !strings.Contains(string(raw), `"claimed_at"`) {
		t.Errorf("outbound_reply 线路帧应含 claimed_at 键: %s", raw)
	}
	if !strings.Contains(string(raw), `"push_attempts":3`) {
		t.Errorf("outbound_reply 线路帧应含 push_attempts=3: %s", raw)
	}

	// 解码断言：值与认领行一致。
	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("解码 outbound_reply 失败: %v", err)
	}
	if f.Reply == nil || f.Reply.ClaimedAt == nil {
		t.Fatalf("Reply.ClaimedAt 不应为空: %+v", f.Reply)
	}
	if !f.Reply.ClaimedAt.Equal(lease) {
		t.Errorf("Reply.ClaimedAt = %v, want %v", f.Reply.ClaimedAt, lease)
	}
	if f.Reply.PushAttempts != 3 {
		t.Errorf("Reply.PushAttempts = %d, want 3", f.Reply.PushAttempts)
	}
}

// TestWS_OutboundReply_NilLeaseOmitted 兼容性：无租约（fake 未填）时字段整体省略，
// legacy 客户端（不认识新键）不受影响。
func TestWS_OutboundReply_NilLeaseOmitted(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-nolease"))

	fp.mu.Lock()
	fp.claimQue = append(fp.claimQue, &model.MessageHub{
		MsgID: "m-nolease", ConversationID: "conv-nolease",
		Content: "无租约回复", MsgType: "text",
	})
	fp.mu.Unlock()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("读 outbound_reply 失败: %v", err)
	}
	if strings.Contains(string(raw), "claimed_at") || strings.Contains(string(raw), "push_attempts") {
		t.Errorf("无租约时租约键应省略（omitempty）: %s", raw)
	}
}
