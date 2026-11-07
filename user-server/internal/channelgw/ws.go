package channelgw

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
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
	// key 必须是 channelgw.ws_push_interval —— 与 config_param_seeds.go 里的登记一致。
	// 这里原本写成 "ws_push_interval_default"，种子里并没有这个 key，
	// GetDuration 每次都 miss 然后回落到兜底值，等于这个参数改了永远不生效（硬闸也管不到：
	// 它只检查「种子 key 有没有读取点」，而这里是读取点用了另一个 key）。
	return service.GlobalConfigParam().GetDuration(ctx, "channelgw", "ws_push_interval", wsPushIntervalDefault)
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

	// TenantBinder 在 register 帧判清渠道账号的租户归属（on 模式必需）。
	//
	// 由装配层注入（router 把 bridge 的解析函数交进来）：本包不能直接 import
	// bridge —— bridge 已经 import channelgw，反向依赖会成环。
	//
	// 契约：返回携带租户的 ctx；判不出归属时返回 error，调用方据此拒绝这条连接。
	// off 模式下注入方原样返回入参 ctx，行为与旧版本等价。
	TenantBinder func(r *http.Request, channel, accountID string) (context.Context, error)
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

	_ = conn.SetReadDeadline(time.Now().Add(runtimeWSRegisterTimeout(c.Request.Context())))
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

	// 租户归属必须在双泵启动前判清：on 模式下 channel/account_id 是唯一的归属线索，
	// 带着无绑定的 ctx 进泵，每一帧的读写都会被隔离 callback 判错，而泵里所有错误
	// 只落一行 Warn —— 渠道端看见连接正常、心跳照常，消息与下发却一条都没落地。
	base := context.Background()
	if t.TenantBinder != nil {
		bound, terr := t.TenantBinder(c.Request, channel, accountID)
		if terr != nil {
			logger.Warnf("[ChannelGW WS] 注册拒绝（租户归属判不清）: channel=%s account=%s: %v",
				channel, accountID, terr)
			_ = conn.WriteJSON(&Frame{
				V: CurrentProtocolVersion, Type: FrameRegisterReject,
				Channel: channel, AccountID: accountID,
				Reason: terr.Error(),
			})
			_ = conn.Close()
			return
		}
		if bound != nil {
			base = bound
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(runtimeWSReadIdleTimeout(c.Request.Context())))

	cn := &wsConn{
		t:         t,
		conn:      conn,
		channel:   channel,
		accountID: accountID,
		base:      base,
		done:      make(chan struct{}),
	}
	logger.Infof("[ChannelGW WS] 渠道连接已注册: channel=%s account=%s", channel, accountID)

	if t.OnRegister != nil {

		utils.SafeGo(context.Background(), "channelgw.ws.on_register", func(ctx context.Context) {
			// BackgroundWithTenant：这条异步回调不随连接断开而取消，但必须带着
			// 本连接的租户归属，否则它写的账号在线位落不进任何租户。
			cbCtx, cancel := context.WithTimeout(tenant.BackgroundWithTenant(base), 3*time.Second)
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
	// base 携带本连接的租户归属（register 帧判清后由 TenantBinder 绑定）。
	// 泵里每一次超时与派生 ctx 都从它起步，而不是 context.Background()。
	base context.Context

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

// baseCtx 取本连接的基准 ctx。
//
// 零值 wsConn（测试里直接构造）没有 base，泵是在 goroutine 里跑的，
// nil ctx 传进 context.WithTimeout 会 panic 并带走整个进程——兜一层 Background。
func (c *wsConn) baseCtx() context.Context {
	if c.base == nil {
		return context.Background()
	}
	return c.base
}

func (c *wsConn) send(f *Frame) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(runtimeWSWriteTimeout(c.baseCtx())))
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
		_ = c.conn.SetReadDeadline(time.Now().Add(runtimeWSReadIdleTimeout(c.baseCtx())))
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
		c.handleAckFrame(f)
	case FrameInbound:
		c.handleInbound(f)
	case FrameHistory:
		c.handleHistory(f)
	default:
	}
}

// wsAckFrameStatus 归一 ack 帧 Status 字段（CH-37b 刀2 起 WS 消费；刀4 R2/R6 三值）。
// 返回 model.BridgeAckStatusDelivered（含空值：legacy 仅-delivered 帧不带 Status 或 Status 空
// =delivered，行为不变）、model.BridgeAckStatusFailed、model.BridgeAckStatusUnknown
// （「已提交但结果不明」真终态：翻转入 unknown，永不自动重发，对账走显式迁移）；
// 其余值返回空串——调用方按协议错误处理（拒翻转 + Warn + FrameAckUnknownStatusCount 打点）。
func wsAckFrameStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", model.BridgeAckStatusDelivered:
		return model.BridgeAckStatusDelivered
	case model.BridgeAckStatusFailed:
		return model.BridgeAckStatusFailed
	case model.BridgeAckStatusUnknown:
		return model.BridgeAckStatusUnknown
	default:
		return ""
	}
}

func (c *wsConn) handleAckFrame(f *Frame) {
	if len(f.MsgIDs) == 0 {
		return
	}
	status := wsAckFrameStatus(f.Status)
	if status == "" {
		// 协议错误：未知 Status 一律不翻转任何行（宁可不 ack 重发，不可错标终态）。
		FrameAckUnknownStatusCount.Add(1)
		logger.Warnf("[ChannelGW WS] ack 帧 Status 非法（协议错误，已拒翻转）: channel=%s account=%s status=%q msg_ids=%d",
			c.channel, c.accountID, f.Status, len(f.MsgIDs))
		return
	}
	ctx, cancel := context.WithTimeout(c.baseCtx(), runtimeWSPipelineTimeout(c.baseCtx()))
	defer cancel()
	// CH-37b 刀4（R2/R6）：failed 与 unknown 共用详细 ack 路径，但语义分叉——
	// failed=平台明确拒绝（可安全重试语义由既有 failed 链路处理）；unknown=「已提交但
	// 结果不明」**真终态**（刀2 时是拒翻转+打点，本刀起真翻转入 unknown 态），
	// 行级落 status='unknown'+push_error='ack_unknown'，此后不进任何认领/回收/重推路径，
	// 出路只有显式对账（service.ReconcileUnknownOutbound）。
	if status == model.BridgeAckStatusFailed || status == model.BridgeAckStatusUnknown {
		res, err := c.t.pipeline.AckOutboundDetailed(ctx, c.channel, c.accountID, f.MsgIDs, "", status, nil, nil)
		if err != nil {
			logger.Warnf("[ChannelGW WS] ack %s 回写失败 channel=%s account=%s: %v", status, c.channel, c.accountID, err)
			return
		}
		if res != nil {
			logger.Infof("[ChannelGW WS] 出站已确认 %s: channel=%s account=%s failed=%d unknown=%d duplicate=%d not_found=%d",
				status, c.channel, c.accountID, res.FailedItemsCount, res.UnknownItemsCount, res.DuplicateCount, res.NotFoundCount)
		}
		return
	}
	// legacy 仅-delivered 路径：原样保留（Status 空/显式 delivered 共用）。
	acked, err := c.t.pipeline.AckOutbound(ctx, c.channel, c.accountID, f.MsgIDs)
	if err != nil {
		logger.Warnf("[ChannelGW WS] ack 回写失败 channel=%s account=%s: %v", c.channel, c.accountID, err)
		return
	}
	logger.Infof("[ChannelGW WS] 出站已确认 delivered: channel=%s account=%s acked=%d", c.channel, c.accountID, acked)
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
	ctx, cancel := context.WithTimeout(c.baseCtx(), runtimeWSPipelineTimeout(c.baseCtx()))
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
	ctx, cancel := context.WithTimeout(c.baseCtx(), runtimeWSPipelineTimeout(c.baseCtx()))
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
	ctx, cancel := context.WithTimeout(c.baseCtx(), runtimeWSPipelineTimeout(c.baseCtx()))
	hubs, err := c.t.pipeline.ClaimOutbound(ctx, c.channel, c.accountID, c.t.pushBatch)
	cancel()
	if err != nil {
		logger.Warnf("[ChannelGW WS] 认领下发队列失败 channel=%s account=%s: %v", c.channel, c.accountID, err)
		return
	}
	if len(hubs) == 0 {
		return
	}
	tracing.RecordDownlinkFetchBatch(c.baseCtx(), c.channel, c.accountID, hubs)
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
				// CH-37b 刀2：认领租约透出（既有列），供客户端回 ack 时原样带回做栅栏断言。
				ClaimedAt:    hub.ClaimedAt,
				PushAttempts: hub.PushAttempts,
			},
		}
		if !c.send(f) {
			return
		}
	}
}
