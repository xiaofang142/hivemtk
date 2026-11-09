package channelgw

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type fakePipeline struct {
	mu       sync.Mutex
	ingested []*model.MessageEvent
	history  []*model.MessageEvent
	claimQue []*model.MessageHub
	acked    []string
	detailed []detailedAckCall
}

// detailedAckCall 记录一次 AckOutboundDetailed 调用的关键入参（CH-37b 刀2 WS Frame.Status 消费用）。
type detailedAckCall struct {
	channel, accountID string
	msgIDs             []string
	conversationID     string
	terminalStatus     string
	fenceLen           int
}

func (f *fakePipeline) IngestBatch(_ context.Context, events []*model.MessageEvent) (*IngestBatchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ingested = append(f.ingested, events...)
	res := &IngestBatchResult{PerEvent: make([]*IngestEventResult, len(events))}
	for i := range events {
		res.PerEvent[i] = &IngestEventResult{Accepted: true, QueuedForAI: true}
	}
	return res, nil
}

func (f *fakePipeline) PersistHistory(_ context.Context, ev *model.MessageEvent, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.history = append(f.history, ev)
	return nil
}

func (f *fakePipeline) ClaimOutbound(_ context.Context, _, _ string, limit int) ([]*model.MessageHub, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.claimQue) == 0 {
		return nil, nil
	}
	n := len(f.claimQue)
	if n > limit {
		n = limit
	}
	out := f.claimQue[:n]
	f.claimQue = f.claimQue[n:]
	return out, nil
}

func (f *fakePipeline) AckOutbound(_ context.Context, _, _ string, msgIDs []string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acked = append(f.acked, msgIDs...)
	return len(msgIDs), nil
}

func (f *fakePipeline) AckOutboundDetailed(_ context.Context, channel, accountID string, msgIDs []string, conversationID, terminalStatus string, _ map[string]OutboundAckInput, fence AckOutboundClaimFence) (*AckOutboundResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detailed = append(f.detailed, detailedAckCall{
		channel: channel, accountID: accountID,
		msgIDs:         append([]string(nil), msgIDs...),
		conversationID: conversationID, terminalStatus: terminalStatus,
		fenceLen: len(fence),
	})
	items := make([]OutboundAckItem, 0, len(msgIDs))
	res := &AckOutboundResult{Items: items}
	for _, id := range msgIDs {
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

func newTestServer(t *testing.T, fp IngressPipeline) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tr := NewWSTransport(fp, nil)
	tr.pushInterval = 100 * time.Millisecond
	r.GET("/api/ws/channel", tr.HandleWS)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/ws/channel"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("WS 拨号失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readFrame(t *testing.T, conn *websocket.Conn, timeout time.Duration) *Frame {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	var f Frame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("读帧失败: %v", err)
	}
	return &f
}

func registerFrame(channel, accountID string) *Frame {
	return &Frame{V: CurrentProtocolVersion, Type: FrameRegister, Channel: channel, AccountID: accountID}
}

// TestWS_RegisterReject 握手拒绝路径：首帧非 register / 未注册渠道 / 缺 account_id。
func TestWS_RegisterReject(t *testing.T) {
	srv := newTestServer(t, &fakePipeline{})

	c1 := dialWS(t, srv)
	_ = c1.WriteJSON(&Frame{Type: FramePing})
	_ = c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f1 Frame
	if err := c1.ReadJSON(&f1); err == nil && f1.Type != FrameRegisterReject {
		t.Errorf("首帧非 register 应断开或拒绝, got %+v", f1)
	}

	c2 := dialWS(t, srv)
	_ = c2.WriteJSON(registerFrame("unknown_channel", "acc-1"))
	f2 := readFrame(t, c2, 2*time.Second)
	if f2.Type != FrameRegisterReject || f2.Reason == "" {
		t.Errorf("未注册渠道应收到拒绝帧, got %+v", f2)
	}

	c3 := dialWS(t, srv)
	_ = c3.WriteJSON(registerFrame(model.ChannelDouyin, ""))
	f3 := readFrame(t, c3, 2*time.Second)
	if f3.Type != FrameRegisterReject {
		t.Errorf("缺 account_id 应收到拒绝帧, got %+v", f3)
	}
}

// TestWS_E2E register → inbound(ack) → ping/pong → 出站推帧 → ack(delivered)。
func TestWS_E2E(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)

	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-1"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameInbound,
		Channel: model.ChannelDouyin, AccountID: "acc-1", TraceID: "trace-1",
		Message: &IngestMessage{
			EventID: "ev-1", ConversationID: "conv-1",
			SenderID: "u-9", MsgType: "text", Content: "你好",
			Timestamp: time.Now().UnixMilli(),
			History: []*HistoryItem{
				{EventID: "h-old", Content: "早先一轮", SenderType: "customer", Timestamp: time.Now().UnixMilli() - 60000},
				{EventID: "ev-1", Content: "与实时同 ID 应跳过", SenderType: "customer"},
			},
		},
	})

	ack := readFrame(t, conn, 3*time.Second)
	if ack.Type != FrameAck {
		t.Fatalf("期望 ack 帧, got %+v", ack)
	}
	if ack.TraceID != "trace-1" {
		t.Errorf("ack 应透传 trace_id, got %q", ack.TraceID)
	}
	if len(ack.Results) != 1 || ack.Results[0].EventID != "ev-1" || !ack.Results[0].Accepted || !ack.Results[0].AIHandled {
		t.Errorf("ack results 异常: %+v", ack.Results)
	}

	fp.mu.Lock()
	if len(fp.ingested) != 1 || fp.ingested[0].EventID != "ev-1" {
		t.Errorf("IngestBatch 事件异常: %+v", fp.ingested)
	} else if fp.ingested[0].Extra["transport"] != "websocket" {
		t.Errorf("transport 标记 = %v, want websocket", fp.ingested[0].Extra["transport"])
	}
	if len(fp.history) != 1 || fp.history[0].EventID != "h-old" {
		t.Errorf("历史回填应仅落 h-old 一条: %+v", fp.history)
	}
	fp.mu.Unlock()

	_ = conn.WriteJSON(&Frame{V: CurrentProtocolVersion, Type: FramePing})
	pong := readFrame(t, conn, 3*time.Second)
	if pong.Type != FramePong {
		t.Errorf("期望 pong, got %+v", pong)
	}

	fp.mu.Lock()
	fp.claimQue = append(fp.claimQue, &model.MessageHub{
		MsgID: "msg-1", ConversationID: "conv-1",
		Content: "AI 回复", MsgType: "text",
	})
	fp.mu.Unlock()

	reply := readFrame(t, conn, 3*time.Second)
	if reply.Type != FrameOutboundReply {
		t.Fatalf("期望 outbound_reply, got %+v", reply)
	}
	if reply.MsgID != "msg-1" || reply.Reply == nil || reply.Reply.Content != "AI 回复" {
		t.Errorf("出站推帧内容异常: %+v", reply)
	}
	if reply.Reply.Channel != model.ChannelDouyin || reply.Reply.AccountID != "acc-1" {
		t.Errorf("出站推帧渠道/账号异常: %+v", reply.Reply)
	}

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: model.ChannelDouyin, AccountID: "acc-1",
		MsgIDs: []string{"msg-1"}, Status: "delivered",
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		fp.mu.Lock()
		n := len(fp.acked)
		fp.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ack 未回写 delivered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	fp.mu.Lock()
	if fp.acked[0] != "msg-1" {
		t.Errorf("acked = %v, want [msg-1]", fp.acked)
	}
	fp.mu.Unlock()
}

// TestWS_HistoryFrame history 帧仅落库（走 PersistHistory，不进 IngestBatch）。
func TestWS_HistoryFrame(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelXHS, "acc-2"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameHistory,
		Message: &IngestMessage{
			EventID: "hist-1", ConversationID: "conv-2",
			SenderID: "u-1", MsgType: "text", Content: "回填消息",
			Direction: "outbound", Timestamp: time.Now().UnixMilli(),
		},
	})

	deadline := time.Now().Add(3 * time.Second)
	for {
		fp.mu.Lock()
		n := len(fp.history)
		fp.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("history 帧未落库")
		}
		time.Sleep(20 * time.Millisecond)
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if fp.history[0].EventID != "hist-1" || fp.history[0].Channel != model.ChannelXHS {
		t.Errorf("history 落库事件异常: %+v", fp.history[0])
	}
	if len(fp.ingested) != 0 {
		t.Errorf("history 帧不应触发 IngestBatch: %+v", fp.ingested)
	}
}
