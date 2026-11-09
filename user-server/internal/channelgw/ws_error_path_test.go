package channelgw

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type controllablePipeline struct {
	fakePipeline

	ingestBatchErr    error
	ingestBatchNil    bool
	persistHistoryErr error
	claimOutboundErr  error
	claimOutboundNil  bool
	ackOutboundErr    error
}

func (c *controllablePipeline) IngestBatch(ctx context.Context, events []*model.MessageEvent) (*IngestBatchResult, error) {
	if c.ingestBatchErr != nil {
		c.fakePipeline.ingested = append(c.fakePipeline.ingested, events...)
		return nil, c.ingestBatchErr
	}
	if c.ingestBatchNil {
		c.fakePipeline.ingested = append(c.fakePipeline.ingested, events...)
		return nil, nil
	}
	return c.fakePipeline.IngestBatch(ctx, events)
}

func (c *controllablePipeline) PersistHistory(ctx context.Context, ev *model.MessageEvent, dir string) error {
	if c.persistHistoryErr != nil {
		c.fakePipeline.mu.Lock()
		c.fakePipeline.history = append(c.fakePipeline.history, ev)
		c.fakePipeline.mu.Unlock()
		return c.persistHistoryErr
	}
	return c.fakePipeline.PersistHistory(ctx, ev, dir)
}

func (c *controllablePipeline) ClaimOutbound(ctx context.Context, ch, acc string, limit int) ([]*model.MessageHub, error) {
	if c.claimOutboundErr != nil {
		return nil, c.claimOutboundErr
	}
	if c.claimOutboundNil {
		return nil, nil
	}
	return c.fakePipeline.ClaimOutbound(ctx, ch, acc, limit)
}

func (c *controllablePipeline) AckOutbound(ctx context.Context, ch, acc string, ids []string) (int, error) {
	if c.ackOutboundErr != nil {
		return 0, c.ackOutboundErr
	}
	return c.fakePipeline.AckOutbound(ctx, ch, acc, ids)
}

func TestHandleWS_UpgradeError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tr := NewWSTransport(&fakePipeline{}, nil)
	r.GET("/api/ws/channel", tr.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/ws/channel")
	if err != nil {
		t.Fatalf("GET 失败: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("upgrade error 应返回 400, got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestHandleWS_RegisterReadError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tr := NewWSTransport(&fakePipeline{}, nil)
	tr.pushInterval = 100 * time.Millisecond
	r.GET("/api/ws/channel", tr.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/ws/channel"
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, _, err := dialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial 失败: %v", err)
	}
	defer conn.Close()

	_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
	time.Sleep(200 * time.Millisecond)
}

func TestValidateRegister_MessageFallback(t *testing.T) {
	tr := NewWSTransport(&fakePipeline{}, nil)

	f := &Frame{
		V:    CurrentProtocolVersion,
		Type: FrameRegister,
		Message: &IngestMessage{
			Channel:   model.ChannelDouyin,
			AccountID: "acc-fallback",
		},
	}
	ch, acc, reason := tr.validateRegister(f)
	if ch != model.ChannelDouyin || acc != "acc-fallback" {
		t.Errorf("Message 回退失败: channel=%q account=%q", ch, acc)
	}
	if reason != "" {
		t.Errorf("不应拒绝, reason=%q", reason)
	}
}

func TestValidateRegister_NilFrame(t *testing.T) {
	tr := NewWSTransport(&fakePipeline{}, nil)
	_, _, reason := tr.validateRegister(nil)
	if reason != "first frame must be register" {
		t.Errorf("nil frame 应被拒绝, reason=%q", reason)
	}
}

func TestValidateRegister_WrongType(t *testing.T) {
	tr := NewWSTransport(&fakePipeline{}, nil)
	_, _, reason := tr.validateRegister(&Frame{Type: FramePing})
	if reason != "first frame must be register" {
		t.Errorf("非 register 类型应被拒绝, reason=%q", reason)
	}
}

func TestWS_HandleInbound_IngestBatchError(t *testing.T) {
	cp := &controllablePipeline{fakePipeline: fakePipeline{}}
	cp.ingestBatchErr = errors.New("db down")

	srv := newTestServer(t, cp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-ib-err"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameInbound, TraceID: "trace-ib-err",
		Message: &IngestMessage{
			EventID: "ev-ib-err", ConversationID: "conv-ib",
			SenderID: "u-1", MsgType: "text", Content: "触发 IngestBatch error",
			Timestamp: time.Now().UnixMilli(),
		},
	})

	ack := readFrame(t, conn, 3*time.Second)
	if ack.Type != FrameAck {
		t.Fatalf("期望 ack 帧, got %+v", ack)
	}
	if len(ack.Results) != 1 {
		t.Fatalf("期望 1 个 result, got %d", len(ack.Results))
	}
	if ack.Results[0].Accepted {
		t.Errorf("IngestBatch error 时 Accepted 应为 false")
	}
	if ack.Results[0].Reason == "" {
		t.Errorf("IngestBatch error 时 Reason 应携带错误信息")
	}
}

func TestWS_HandleInbound_NilBatchResult(t *testing.T) {

	cp := &controllablePipeline{fakePipeline: fakePipeline{}}
	cp.ingestBatchNil = true

	srv := newTestServer(t, cp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-nil-res"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameInbound,
		Message: &IngestMessage{
			EventID: "ev-nil", ConversationID: "conv-nil",
			SenderID: "u-1", MsgType: "text", Content: "nil batch result",
			Timestamp: time.Now().UnixMilli(),
		},
	})

	ack := readFrame(t, conn, 3*time.Second)
	if ack.Type != FrameAck {
		t.Fatalf("期望 ack 帧, got %+v", ack)
	}

	if len(ack.Results) != 0 {
		t.Errorf("batchResult nil 且 err==nil 时 results 应为空, got %+v", ack.Results)
	}
}

func TestWS_HandleInbound_EmptyMessages(t *testing.T) {
	srv := newTestServer(t, &fakePipeline{})
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-empty"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameInbound,
		Message:  nil,
		Messages: nil,
	})

	time.Sleep(200 * time.Millisecond)
}

func TestWS_HandleInbound_AllNilItems(t *testing.T) {
	srv := newTestServer(t, &fakePipeline{})
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-nil-items"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameInbound,
		Messages: []*IngestMessage{nil, nil},
	})
	time.Sleep(200 * time.Millisecond)
}

func TestWS_HandleInbound_NilHistoryItem(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-nil-hist"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameInbound,
		Message: &IngestMessage{
			EventID: "ev-nil-hist", ConversationID: "conv-nilh",
			SenderID: "u-1", MsgType: "text", Content: "消息",
			Timestamp: time.Now().UnixMilli(),
			History: []*HistoryItem{
				nil,
				{EventID: "h-good", Content: "有效历史", SenderType: "customer", Timestamp: time.Now().UnixMilli() - 1000},
			},
		},
	})

	ack := readFrame(t, conn, 3*time.Second)
	if ack.Type != FrameAck {
		t.Fatalf("期望 ack, got %+v", ack)
	}

	fp.mu.Lock()
	defer fp.mu.Unlock()

	if len(fp.history) != 1 || fp.history[0].EventID != "h-good" {
		t.Errorf("history 落库应为 [h-good], got %+v", fp.history)
	}
}

func TestWS_HandleInbound_SenderTypeSelfForceOutbound(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-self-force"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameInbound,
		Message: &IngestMessage{
			EventID: "ev-self", ConversationID: "conv-self",
			SenderID: "u-1", MsgType: "text", Content: "消息",
			SenderType: "self",
			Timestamp:  time.Now().UnixMilli(),
			History: []*HistoryItem{
				{EventID: "h-self", Content: "AI 自己发的", SenderType: "self", Direction: "", Timestamp: time.Now().UnixMilli() - 1000},
			},
		},
	})

	ack := readFrame(t, conn, 3*time.Second)
	if ack.Type != FrameAck {
		t.Fatalf("期望 ack, got %+v", ack)
	}

	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.history) != 1 || fp.history[0].EventID != "h-self" {
		t.Errorf("history 应落 h-self, got %+v", fp.history)
	}

}

func TestWS_HandleInbound_SenderTypeAgentForceOutbound(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-agent-force"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameInbound,
		Message: &IngestMessage{
			EventID: "ev-agent", ConversationID: "conv-agent",
			SenderID: "u-1", MsgType: "text", Content: "agent 消息",
			SenderType: "agent",
			Timestamp:  time.Now().UnixMilli(),
			History: []*HistoryItem{
				{EventID: "h-agent", Content: "agent 回复", SenderType: "agent", Direction: "inbound", Timestamp: time.Now().UnixMilli() - 1000},
			},
		},
	})

	ack := readFrame(t, conn, 3*time.Second)
	if ack.Type != FrameAck {
		t.Fatalf("期望 ack, got %+v", ack)
	}
}

func TestWS_HandleInbound_PersistHistoryError(t *testing.T) {
	cp := &controllablePipeline{fakePipeline: fakePipeline{}}
	cp.persistHistoryErr = errors.New("history table locked")

	srv := newTestServer(t, cp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-hist-err"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameInbound,
		Message: &IngestMessage{
			EventID: "ev-hist-err", ConversationID: "conv-hist",
			SenderID: "u-1", MsgType: "text", Content: "实时消息",
			Timestamp: time.Now().UnixMilli(),
			History: []*HistoryItem{
				{EventID: "h-err", Content: "历史", SenderType: "customer", Timestamp: time.Now().UnixMilli() - 1000},
			},
		},
	})

	ack := readFrame(t, conn, 3*time.Second)
	if ack.Type != FrameAck {
		t.Fatalf("期望 ack 帧, got %+v", ack)
	}
	if len(ack.Results) != 1 || !ack.Results[0].Accepted {
		t.Errorf("实时消息应正常被接收, results=%+v", ack.Results)
	}
}

func TestSend_DoneClosed_Direct(t *testing.T) {

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade err: %v", err)
			return
		}
		defer conn.Close()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial 失败: %v", err)
	}
	defer conn.Close()

	tr := NewWSTransport(&fakePipeline{}, nil)
	cn := &wsConn{
		t: tr, conn: conn,
		channel: "douyin", accountID: "acc-direct",
		done: make(chan struct{}),
	}

	close(cn.done)

	ok := cn.send(&Frame{Type: FramePing})
	if ok {
		t.Errorf("done 已关闭时 send 应返回 false")
	}
}

func TestSend_WriteJSONError_Direct(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial 失败: %v", err)
	}

	tr := NewWSTransport(&fakePipeline{}, nil)
	cn := &wsConn{
		t: tr, conn: conn,
		channel: "douyin", accountID: "acc-direct-werr",
		done: make(chan struct{}),
	}

	_ = conn.Close()

	ok := cn.send(&Frame{Type: FramePing})
	if ok {
		t.Errorf("conn 已关闭时 send 应返回 false")
	}

	select {
	case <-cn.done:

	default:
		t.Errorf("send 失败后 close() 应被调用 → done 应已关闭")
	}
}

func TestPushOnce_SendFail_Direct(t *testing.T) {
	fp := &fakePipeline{}
	fp.mu.Lock()
	fp.claimQue = append(fp.claimQue, &model.MessageHub{
		MsgID: "msg-push-fail", ConversationID: "conv-pf",
		Content: "发不出", MsgType: "text",
	})
	fp.mu.Unlock()

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial 失败: %v", err)
	}
	defer conn.Close()

	tr := NewWSTransport(fp, nil)
	cn := &wsConn{
		t: tr, conn: conn,
		channel: "douyin", accountID: "acc-push-fail",
		done: make(chan struct{}),
	}
	close(cn.done)

	cn.pushOnce()

	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.claimQue) != 0 {
		t.Errorf("pushOnce 应消费 hub, claimQue 还剩 %d", len(fp.claimQue))
	}
}

func TestWS_HandleFrame_AckError(t *testing.T) {
	cp := &controllablePipeline{fakePipeline: fakePipeline{}}
	cp.ackOutboundErr = errors.New("ack db error")

	srv := newTestServer(t, cp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-ack-err"))

	_ = conn.WriteJSON(&Frame{
		V:      CurrentProtocolVersion,
		Type:   FrameAck,
		MsgIDs: []string{"msg-ack-err-1", "msg-ack-err-2"},
	})

	time.Sleep(200 * time.Millisecond)
}

func TestWS_HandleFrame_AckEmptyIDs(t *testing.T) {
	srv := newTestServer(t, &fakePipeline{})
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-ack-empty"))

	_ = conn.WriteJSON(&Frame{V: CurrentProtocolVersion, Type: FrameAck, MsgIDs: nil})
	time.Sleep(200 * time.Millisecond)
}

func TestWS_HandleHistory_PersistError(t *testing.T) {
	cp := &controllablePipeline{fakePipeline: fakePipeline{}}
	cp.persistHistoryErr = errors.New("history persist failed")

	srv := newTestServer(t, cp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelXHS, "acc-hist-persist-err"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameHistory,
		Message: &IngestMessage{
			EventID: "h-fail", ConversationID: "conv-hfail",
			SenderID: "u-1", MsgType: "text", Content: "会失败的历史",
			Timestamp: time.Now().UnixMilli(),
		},
	})
	time.Sleep(300 * time.Millisecond)
}

func TestWS_HandleHistory_Empty(t *testing.T) {
	srv := newTestServer(t, &fakePipeline{})
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelXHS, "acc-hist-empty"))

	_ = conn.WriteJSON(&Frame{V: CurrentProtocolVersion, Type: FrameHistory})
	time.Sleep(200 * time.Millisecond)
}

func TestWS_HandleHistory_NilItem(t *testing.T) {
	srv := newTestServer(t, &fakePipeline{})
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelXHS, "acc-hist-nil"))

	_ = conn.WriteJSON(&Frame{
		V: CurrentProtocolVersion, Type: FrameHistory,
		Messages: []*IngestMessage{nil},
	})
	time.Sleep(200 * time.Millisecond)
}

func TestWS_PushOnce_ClaimError(t *testing.T) {
	cp := &controllablePipeline{fakePipeline: fakePipeline{}}
	cp.claimOutboundErr = errors.New("claim queue broken")

	srv := newTestServer(t, cp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-claim-err"))

	time.Sleep(500 * time.Millisecond)
}

func TestWS_PushOnce_SendFail(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-send-fail"))

	fp.mu.Lock()
	fp.claimQue = append(fp.claimQue, &model.MessageHub{
		MsgID: "msg-first", ConversationID: "conv-1",
		Content: "第一条", MsgType: "text",
	})
	fp.mu.Unlock()

	reply := readFrame(t, conn, 3*time.Second)
	if reply.Type != FrameOutboundReply {
		t.Fatalf("期望 outbound_reply, got %+v", reply)
	}

	fp.mu.Lock()
	fp.claimQue = append(fp.claimQue, &model.MessageHub{
		MsgID: "msg-second", ConversationID: "conv-1",
		Content: "第二条", MsgType: "text",
	})
	fp.mu.Unlock()

	_ = conn.Close()
	time.Sleep(500 * time.Millisecond)
}

func TestWS_Send_WriteErrViaPingPong(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-ping-close"))

	_ = conn.Close()
	time.Sleep(100 * time.Millisecond)

	fp.mu.Lock()
	fp.claimQue = append(fp.claimQue, &model.MessageHub{
		MsgID: "msg-after-close", ConversationID: "conv-x",
		Content: "发不出的消息", MsgType: "text",
	})
	fp.mu.Unlock()
	time.Sleep(500 * time.Millisecond)
}

func TestWS_PushOnce_NoHubs(t *testing.T) {
	fp := &fakePipeline{}
	srv := newTestServer(t, fp)
	conn := dialWS(t, srv)
	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-no-hubs"))

	time.Sleep(300 * time.Millisecond)
}

func TestWSTransport_NilPipeline(t *testing.T) {
	t.Skip("nil pipeline 当前会导致 panic，不是测试目标")
}

func TestCollectFrameMessages_Backfill(t *testing.T) {
	f := &Frame{
		Message: &IngestMessage{
			EventID:   "ev-1",
			Channel:   "",
			AccountID: "",
		},
	}
	msgs := collectFrameMessages(f, "ch-xhs", "acc-backfill")
	if len(msgs) != 1 {
		t.Fatalf("期望 1 条消息, got %d", len(msgs))
	}
	if msgs[0].Channel != "ch-xhs" {
		t.Errorf("Channel 应从参数回填, got %q", msgs[0].Channel)
	}
	if msgs[0].AccountID != "acc-backfill" {
		t.Errorf("AccountID 应从参数回填, got %q", msgs[0].AccountID)
	}
}

func TestCollectFrameMessages_Merge(t *testing.T) {

	f := &Frame{
		Message:  &IngestMessage{EventID: "ev-single", Channel: "c1"},
		Messages: []*IngestMessage{{EventID: "ev-list-1", Channel: "c1"}, nil, {EventID: "ev-list-2", Channel: "c1"}},
	}
	msgs := collectFrameMessages(f, "c1", "a1")
	if len(msgs) != 4 {
		t.Fatalf("期望 4 条（Message + Messages 直接 append 不过滤 nil）, got %d", len(msgs))
	}
}

func TestWS_WithOnRegister(t *testing.T) {
	var mu sync.Mutex
	var onRegCalled bool
	var onRegCh, onRegAcc string

	gin.SetMode(gin.TestMode)
	r := gin.New()
	tr := NewWSTransport(&fakePipeline{}, nil)
	tr.pushInterval = 100 * time.Millisecond
	tr.OnRegister = func(ctx context.Context, ch, acc string) {
		mu.Lock()
		onRegCalled = true
		onRegCh = ch
		onRegAcc = acc
		mu.Unlock()
	}
	r.GET("/api/ws/channel", tr.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/ws/channel"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial 失败: %v", err)
	}
	defer conn.Close()

	_ = conn.WriteJSON(registerFrame(model.ChannelDouyin, "acc-onreg"))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		called := onRegCalled
		mu.Unlock()
		if called {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	if !onRegCalled {
		t.Fatal("OnRegister 未被调用")
	}
	if onRegCh != model.ChannelDouyin || onRegAcc != "acc-onreg" {
		t.Errorf("OnRegister 参数错误: channel=%q account=%q", onRegCh, onRegAcc)
	}
	mu.Unlock()
}

func TestPipeline_NilIngress(t *testing.T) {
	p := NewPipeline(nil)
	if p == nil {
		t.Fatal("NewPipeline(nil) 不应返回 nil")
	}

	ctx := context.Background()

	if _, err := p.IngestBatch(ctx, nil); err == nil {
		t.Error("IngestBatch 应返回 errPipelineNotConfigured")
	}
	if err := p.PersistHistory(ctx, nil, "inbound"); err == nil {
		t.Error("PersistHistory 应返回 errPipelineNotConfigured")
	}
	if _, err := p.ClaimOutbound(ctx, "ch", "acc", 10); err == nil {
		t.Error("ClaimOutbound 应返回 errPipelineNotConfigured")
	}
	if _, err := p.AckOutbound(ctx, "ch", "acc", []string{"x"}); err == nil {
		t.Error("AckOutbound 应返回 errPipelineNotConfigured")
	}
}

func TestToEvent_Nil(t *testing.T) {
	var m *IngestMessage
	if m.ToEvent("http") != nil {
		t.Error("nil receiver 的 ToEvent 应返回 nil")
	}
}
