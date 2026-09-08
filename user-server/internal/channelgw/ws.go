package channelgw

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	wsRegisterTimeout     = 15 * time.Second
	wsReadIdleTimeout     = 90 * time.Second
	wsWriteTimeout        = 10 * time.Second
	wsPushIntervalDefault = 2 * time.Second
	wsPushBatchDefault    = 20
	wsMaxFrameSize        = 1 << 20
	wsPipelineTimeout     = 30 * time.Second
)

func runtimeWSRegisterTimeout(ctx context.Context) time.Duration {
	return service.GlobalConfigParam().GetDuration(ctx, "channelgw", "ws_register_timeout", wsRegisterTimeout)
}
func runtimeWSReadIdleTimeout(ctx context.Context) time.Duration {
	return service.GlobalConfigParam().GetDuration(ctx, "channelgw", "ws_read_idle_timeout", wsReadIdleTimeout)
}
func runtimeWSWriteTimeout(ctx context.Context) time.Duration {
	return service.GlobalConfigParam().GetDuration(ctx, "channelgw", "ws_write_timeout", wsWriteTimeout)
}
func runtimeWSPushIntervalDefault(ctx context.Context) time.Duration {
	return service.GlobalConfigParam().GetDuration(ctx, "channelgw", "ws_push_interval_default", wsPushIntervalDefault)
}
func runtimeWSPipelineTimeout(ctx context.Context) time.Duration {
	return service.GlobalConfigParam().GetDuration(ctx, "channelgw", "ws_pipeline_timeout", wsPipelineTimeout)
}

// WSTransport WebSocket 传输处理器。
type WSTransport struct {
	pipeline     IngressPipeline
	registry     *Registry
	upgrader     websocket.Upgrader
	pushInterval time.Duration
	pushBatch    int
	OnRegister   func(ctx context.Context, channel, accountID string)
}

// NewWSTransport 构造 WS 传输。registry 为 nil 时使用 Default。
func NewWSTransport(pipeline IngressPipeline, registry *Registry) *WSTransport {
	if registry == nil {
		registry = Default
	}
	return &WSTransport{
		pipeline: pipeline,
		registry: registry,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// 校验 Origin 与 Host 一致（同源）或命中显式白名单；
			// 渠道客户端通常不带 Origin（非浏览器），不受影响。
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true
				}
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
					scheme = p
				}
				if origin == scheme+"://"+r.Host {
					return true
				}
				for _, a := range strings.Split(os.Getenv("CHANNEL_WS_ALLOWED_ORIGINS"), ",") {
					if strings.TrimSpace(a) != "" && strings.TrimSpace(a) == origin {
						return true
					}
				}
				logger.Warnf("[ChannelGW WS] 拒绝跨源 WebSocket 握手 origin=%s host=%s", origin, r.Host)
				return false
			},
		},
		pushInterval: runtimeWSPushIntervalDefault(context.Background()),
		pushBatch:    wsPushBatchDefault,
	}
}

// HandleWS gin 处理器：GET /api/ws/channel。
// 握手（register）通过后进入双泵循环，连接断开才返回。
func (t *WSTransport) HandleWS(c *gin.Context) {
	conn, err := t.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Warnf("[ChannelGW WS] 协议升级失败: %v", err)
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(runtimeWSRegisterTimeout(context.Background())))
	var reg Frame
	if err := conn.ReadJSON(&reg); err != nil {
		logger.Warnf("[ChannelGW WS] 读取 register 帧失败: %v", err)
		_ = conn.Close()
		return
	}
	channel, accountID, reason := t.validateRegister(&reg)
	if reason != "" {
		logger.Warnf("[ChannelGW WS] 注册拒绝: channel=%s account=%s reason=%s", channel, accountID, reason)
		_ = conn.WriteJSON(&Frame{
			V: CurrentProtocolVersion, Type: FrameRegisterReject,
			Channel: channel, AccountID: accountID, Reason: reason,
		})
		_ = conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(runtimeWSReadIdleTimeout(context.Background())))

	cn := &wsConn{
		t:         t,
		conn:      conn,
		channel:   channel,
		accountID: accountID,
		done:      make(chan struct{}),
	}
	logger.Infof("[ChannelGW WS] 渠道连接已注册: channel=%s account=%s", channel, accountID)

	if t.OnRegister != nil {

		utils.SafeGo(context.Background(), "channelgw.ws.on_register", func(ctx context.Context) {
			cbCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			t.OnRegister(cbCtx, channel, accountID)
		})
	}

	go cn.readPump()
	go cn.pushPump()
	<-cn.done
	logger.Infof("[ChannelGW WS] 渠道连接已断开: channel=%s account=%s", channel, accountID)
}

func (t *WSTransport) validateRegister(f *Frame) (string, string, string) {
	if f == nil || f.Type != FrameRegister {
		return "", "", "first frame must be register"
	}
	channel := f.Channel
	accountID := f.AccountID
	if f.Message != nil {
		channel = firstNonEmpty(channel, f.Message.Channel)
		accountID = firstNonEmpty(accountID, f.Message.AccountID)
	}
	if channel == "" || accountID == "" {
		return channel, accountID, "channel and account_id required"
	}
	if !t.registry.Supports(channel, TransportWebSocket) {
		return channel, accountID, "channel not registered for websocket transport"
	}
	return channel, accountID, ""
}

type wsConn struct {
	t         *WSTransport
	conn      *websocket.Conn
	channel   string
	accountID string

	sendMu    sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
}

func (c *wsConn) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *wsConn) send(f *Frame) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(runtimeWSWriteTimeout(context.Background())))
	if err := c.conn.WriteJSON(f); err != nil {
		logger.Warnf("[ChannelGW WS] 写帧失败 channel=%s account=%s type=%s: %v",
			c.channel, c.accountID, f.Type, err)
		c.close()
		return false
	}
	return true
}

func (c *wsConn) readPump() {
	defer c.close()
	c.conn.SetReadLimit(wsMaxFrameSize)
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(runtimeWSReadIdleTimeout(context.Background())))
		var f Frame
		if err := c.conn.ReadJSON(&f); err != nil {
			return
		}
		c.handleFrame(&f)
	}
}

func (c *wsConn) handleFrame(f *Frame) {
	switch f.Type {
	case FramePing:
		c.send(&Frame{V: CurrentProtocolVersion, Type: FramePong, Channel: c.channel, AccountID: c.accountID})
	case FrameAck:
		if len(f.MsgIDs) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), runtimeWSPipelineTimeout(context.Background()))
		defer cancel()
		acked, err := c.t.pipeline.AckOutbound(ctx, c.channel, c.accountID, f.MsgIDs)
		if err != nil {
			logger.Warnf("[ChannelGW WS] ack 回写失败 channel=%s account=%s: %v", c.channel, c.accountID, err)
			return
		}
		logger.Infof("[ChannelGW WS] 出站已确认 delivered: channel=%s account=%s acked=%d", c.channel, c.accountID, acked)
	case FrameInbound:
		c.handleInbound(f)
	case FrameHistory:
		c.handleHistory(f)
	default:
	}
}

func collectFrameMessages(f *Frame, channel, accountID string) []*IngestMessage {
	msgs := make([]*IngestMessage, 0, len(f.Messages)+1)
	if f.Message != nil {
		msgs = append(msgs, f.Message)
	}
	msgs = append(msgs, f.Messages...)
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if m.Channel == "" {
			m.Channel = channel
		}
		if m.AccountID == "" {
			m.AccountID = accountID
		}
	}
	return msgs
}

func (c *wsConn) handleInbound(f *Frame) {
	msgs := collectFrameMessages(f, c.channel, c.accountID)
	if len(msgs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeWSPipelineTimeout(context.Background()))
	defer cancel()

	events := make([]*model.MessageEvent, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		for _, it := range m.History {
			if it == nil {
				continue
			}
			if m.EventID != "" && it.EventID == m.EventID {
				continue
			}
			dir := it.Direction
			if dir == "" {
				dir = "inbound"
			}
			if (m.SenderType == "self" || m.SenderType == "agent") && dir != "outbound" {
				dir = "outbound"
			}
			if err := c.t.pipeline.PersistHistory(ctx, HistoryToEvent(m, it), dir); err != nil {
				logger.Warnf("[ChannelGW WS] history 落库失败 event_id=%s conv=%s: %v",
					it.EventID, m.ConversationID, err)
			}
		}
		events = append(events, m.ToEvent(string(TransportWebSocket)))
	}
	if len(events) == 0 {
		return
	}

	batchResult, err := c.t.pipeline.IngestBatch(ctx, events)
	results := make([]*IngestResult, 0, len(events))
	if err != nil {
		logger.Warnf("[ChannelGW WS] IngestBatch 失败 channel=%s account=%s: %v", c.channel, c.accountID, err)
		for _, ev := range events {
			results = append(results, &IngestResult{EventID: ev.EventID, Accepted: false, Reason: err.Error()})
		}
	} else if batchResult != nil {
		for i, ev := range events {
			r := &IngestResult{EventID: ev.EventID}
			if i < len(batchResult.PerEvent) && batchResult.PerEvent[i] != nil {
				pr := batchResult.PerEvent[i]
				r.Accepted = pr.Accepted
				r.AIHandled = pr.QueuedForAI
				r.Reason = pr.Reason
				r.Duplicate = IsDuplicateReason(pr.Reason)
			}
			results = append(results, r)
		}
	}
	c.send(&Frame{
		V: CurrentProtocolVersion, Type: FrameAck,
		Channel: c.channel, AccountID: c.accountID,
		TraceID: f.TraceID, Results: results,
	})
}

func (c *wsConn) handleHistory(f *Frame) {
	msgs := collectFrameMessages(f, c.channel, c.accountID)
	if len(msgs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeWSPipelineTimeout(context.Background()))
	defer cancel()
	for _, m := range msgs {
		if m == nil {
			continue
		}
		dir := m.Direction
		if dir == "" {
			dir = "inbound"
		}
		if err := c.t.pipeline.PersistHistory(ctx, m.ToEvent(string(TransportWebSocket)), dir); err != nil {
			logger.Warnf("[ChannelGW WS] history 帧落库失败 event_id=%s conv=%s: %v",
				m.EventID, m.ConversationID, err)
		}
	}
}

func (c *wsConn) pushPump() {
	ticker := time.NewTicker(c.t.pushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.pushOnce()
		}
	}
}

func (c *wsConn) pushOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeWSPipelineTimeout(context.Background()))
	hubs, err := c.t.pipeline.ClaimOutbound(ctx, c.channel, c.accountID, c.t.pushBatch)
	cancel()
	if err != nil {
		logger.Warnf("[ChannelGW WS] 认领下发队列失败 channel=%s account=%s: %v", c.channel, c.accountID, err)
		return
	}
	if len(hubs) == 0 {
		return
	}
	tracing.RecordDownlinkFetchBatch(context.Background(), c.channel, c.accountID, hubs)
	for _, hub := range hubs {
		f := &Frame{
			V: CurrentProtocolVersion, Type: FrameOutboundReply,
			Channel: c.channel, AccountID: c.accountID,
			MsgID: hub.MsgID,
			Reply: &OutboundReply{
				Channel:        c.channel,
				AccountID:      c.accountID,
				ConversationID: hub.ConversationID,
				Content:        hub.Content,
				MsgType:        hub.MsgType,
				MediaURL:       hub.MediaURL,
			},
		}
		if !c.send(f) {
			return
		}
	}
}
