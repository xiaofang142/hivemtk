package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/security"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service/translation"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

// VisitorWSHandler 访客 WebSocket 处理器
//
// 端点：GET /api/ws/visitor
// 参数：
//   - session_id  必填
//   - visitor_id  必填
//   - channel_id  可选（缺失时使用 default）
//
// 私域部署模式：无需鉴权，自己网站直连。
// AppKey/Channel 解析由 AppKeyResolve 中间件完成（HTTP API）；WS 端为公开端点。
//
// 每个访客连接分配独立的 trace_id（可由前端通过 X-Trace-Id 透传，或自动生成），
// 并绑定 module=websocket，使该连接生命周期内的所有日志共享同一追踪标识。
//
// 下行消息类型（type 字段）：
//   - welcome            接入欢迎
//   - message            AI/坐席消息
//   - offline_messages   离线消息批量（连接时推送一次）
//   - agent_joined       坐席接管
//   - session_closed     会话关闭
//   - ai_typing          AI 正在输入
//   - error              错误通知
//   - pong               心跳响应
//
// 上行消息类型：
//   - ping        心跳
//   - delivered   标记消息已投递（防止重连重复拉取）
//   - close       关闭
type VisitorWSHandler struct {
	hub          *Hub
	db           *gorm.DB
	langResolver *translation.LangConfigResolver
}

// NewVisitorWSHandler 创建访客 WebSocket 处理器
func NewVisitorWSHandler(db *gorm.DB) *VisitorWSHandler {
	return &VisitorWSHandler{hub: GetHub(), db: db}
}

// SetLangResolver 注入多语言解析器（v1.2 出海方案）。
// 未注入时仍可正常工作，ctx 中语言走默认 zh 兜底。
func (h *VisitorWSHandler) SetLangResolver(r *translation.LangConfigResolver) {
	h.langResolver = r
}

var upgraderVisitor = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}

		if isAllowedOrigin(origin) {
			return true
		}

		privatePatterns := []string{
			"http://localhost",
			"http://127.0.0.1",
			"http://192.168.",
			"http://10.",
			"http://172.",
			"https://localhost",
			"https://127.0.0.1",
		}
		for _, pattern := range privatePatterns {
			if strings.HasPrefix(origin, pattern) {
				return true
			}
		}
		return false
	},
}

func getAllowedWSOrigins() []string {
	return allowedWSOrigins
}

var allowedWSOrigins = []string{}

// SetAllowedWSOrigins 动态设置允许的 WebSocket Origin 列表（供启动时配置调用）
func SetAllowedWSOrigins(origins []string) {
	allowedWSOrigins = origins
}

func (h *VisitorWSHandler) HandleVisitorWebSocket(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	visitorID := strings.TrimSpace(c.Query("visitor_id"))
	channelID := strings.TrimSpace(c.Query("channel_id"))
	if channelID == "" {
		channelID = "default"
	}

	ctx := logger.WithTraceID(c.Request.Context(), c.GetHeader("X-Trace-Id"))
	ctx = logger.WithModule(ctx, "websocket")

	visitorToken := strings.TrimSpace(c.Query("visitor_token"))
	if visitorToken == "" {
		visitorToken = strings.TrimSpace(c.Query("token"))
	}
	// fail-closed：session_id/visitor_id 已给出但缺 token 一律拒绝，
	// 防止凭 session_id 冒连他人会话接收坐席/AI 回复
	if visitorToken == "" && sessionID != "" && visitorID != "" {
		logger.Ctx(ctx).Warn().Str("session_id", sessionID).Str("visitor_id", visitorID).Msg("WebSocket 连接无 visitor_token，已拒绝")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "visitor_token required"})
		return
	}
	if visitorToken != "" {
		secretKey, serr := getVisitorTokenSecret()
		if serr != nil {
			logger.Ctx(ctx).Error().Err(serr).Msg("visitor token 密钥未配置，拒绝连接")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server visitor token secret not configured"})
			return
		}
		if err := security.ValidateVisitorToken(secretKey, visitorToken, channelID, visitorID, sessionID); err != nil {
			logger.Ctx(ctx).Warn().Str("session_id", sessionID).Str("visitor_id", visitorID).Err(err).Msg("visitor_token 验证失败，拒绝 WebSocket 连接")
			c.JSON(http.StatusForbidden, gin.H{
				"error": "visitor_token 无效或已过期",
			})
			return
		}
	}

	sinceSeq := uint64(0)

	if c.Query("epoch") == CurrentEpoch() {
		if s := strings.TrimSpace(c.Query("since_seq")); s != "" {
			if n, err := strconv.ParseUint(s, 10, 64); err == nil {
				sinceSeq = n
			}
		}
	} else {
		logger.Ctx(ctx).Info().Str("client_epoch", c.Query("epoch")).Str("server_epoch", CurrentEpoch()).
			Msg("[WS] epoch mismatch on query, full resync")
	}

	if sessionID == "" || visitorID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "缺少必要参数：session_id / visitor_id",
		})
		return
	}

	ctx = injectLangToCtx(ctx, h.langResolver, channelID, 0)

	conn, err := upgraderVisitor.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("session_id", sessionID).Str("visitor_id", visitorID).Msg("ws upgrade failed")
		return
	}

	client := NewVisitorClient(h.hub, sessionID, visitorID, channelID)
	client.hub = h.hub
	registerVisitorClient(client)

	logger.Ctx(ctx).Info().
		Str("session_id", sessionID).
		Str("visitor_id", visitorID).
		Str("channel_id", channelID).
		Msg("visitor connected")

	go h.writePump(client, conn)
	go h.readPump(client, conn, ctx)

	go h.onConnect(client, sinceSeq, ctx)
}

func (h *VisitorWSHandler) onConnect(client *Client, sinceSeq uint64, ctx context.Context) {
	if !client.onConnectInflight.CompareAndSwap(false, true) {
		return
	}
	defer client.onConnectInflight.Store(false)

	if h.db == nil {
		return
	}

	welcomeEnv := MustEnvelope(NextSeq(), TypeWelcome, map[string]any{
		"session_id": client.sessionID,
		"visitor_id": client.visitorID,
		"channel_id": client.channelID,
		"since_seq":  sinceSeq,
		"time":       time.Now().Unix(),
	})
	if bytes, err := welcomeEnv.MarshalBytes(); err == nil {
		sendToClient(client, bytes)
	}

	type offlineRow struct {
		ID           uint      `gorm:"primaryKey"`
		SessionID    string    `gorm:"column:session_id"`
		Content      string    `gorm:"column:content"`
		SenderType   string    `gorm:"column:sender_type"`
		SenderName   string    `gorm:"column:sender_name"`
		AISource     string    `gorm:"column:ai_source"`
		AIConfidence float64   `gorm:"column:ai_confidence"`
		CreatedAt    time.Time `gorm:"column:created_at"`
	}
	var rows []offlineRow
	if err := h.db.Table("session_messages").
		Select("id, session_id, content, sender_type, sender_name, ai_source, ai_confidence, created_at").
		Where("session_id = ? AND sender_type IN ? AND delivered_at IS NULL",
			client.sessionID, []string{"ai", "agent"}).
		Order("created_at ASC").
		Scan(&rows).Error; err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("session_id", client.sessionID).Msg("fetch offline messages failed")
		return
	}

	if len(rows) == 0 {
		if sinceSeq > 0 {
			pending := GlobalPendingAck().PendingSince(client.sessionID, sinceSeq)
			if len(pending) == 0 {
				return
			}
			missedEnv := MustEnvelope(NextSeq(), "missed_ack", map[string]any{
				"session_id": client.sessionID,
				"pending":    pending,
			})
			if bytes, err := missedEnv.MarshalBytes(); err == nil {
				sendToClient(client, bytes)
			}
		}
		return
	}

	ids := make([]uint, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		env := MustEnvelope(NextSeq(), TypeMessage, map[string]any{
			"id":          r.ID,
			"content":     r.Content,
			"sender_type": r.SenderType,
			"sender_name": r.SenderName,
			"ai_source":   r.AISource,
			"confidence":  r.AIConfidence,
			"created_at":  r.CreatedAt,
			"offline":     true,
		})
		if bytes, err := env.MarshalBytes(); err == nil {
			sendToClient(client, bytes)
			GlobalPendingAck().Track(client.sessionID, env.Seq)
		}
	}

	now := time.Now()
	if err := h.db.Table("session_messages").
		Where("session_id = ? AND id IN ?", client.sessionID, ids).
		Update("delivered_at", &now).Error; err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("session_id", client.sessionID).Msg("mark delivered failed")
	}
}

func sendToClient(client *Client, payload []byte) {
	defer func() {
		_ = recover()
	}()
	select {
	case client.send <- payload:
	case <-time.After(500 * time.Millisecond):
	}
}

func (h *VisitorWSHandler) writePump(client *Client, conn *websocket.Conn) {
	defer func() {
		conn.Close()
	}()

	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case message, ok := <-client.send:
			if !ok {
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *VisitorWSHandler) readPump(client *Client, conn *websocket.Conn, ctx context.Context) {
	defer func() {
		GlobalPendingAck().Drop(client.sessionID)
		unregisterVisitorClient(client)
		conn.Close()
		logger.Ctx(ctx).Info().Str("session_id", client.sessionID).Msg("visitor disconnected")
	}()

	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	msgLimiter := newMessageRateLimiter(10, 20)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Ctx(ctx).Error().Err(err).Str("session_id", client.sessionID).Msg("ws read error")
			}
			break
		}

		if !msgLimiter.Allow() {
			logger.Ctx(ctx).Warn().Str("session_id", client.sessionID).Msg("ws message rate limit exceeded")
			sendVisitorError(client, "消息过于频繁，请稍后再试")
			continue
		}

		var msg map[string]any
		if err := json.Unmarshal(message, &msg); err != nil {
			sendVisitorError(client, "消息格式错误")
			continue
		}

		msgType, _ := msg["type"].(string)
		switch msgType {
		case "ping":
			pongEnv := MustEnvelope(NextSeq(), TypePong, map[string]any{"time": time.Now().Unix()})
			if bytes, err := pongEnv.MarshalBytes(); err == nil {
				sendToClient(client, bytes)
			}
		case "ack":
			ackedCount := handleAckMessage(client, msg)
			logger.Ctx(ctx).Debug().Str("session_id", client.sessionID).Int("acked", ackedCount).Msg("ack received")
		case "resume":

			sinceSeq := uint64(0)
			if msgEpoch, _ := msg["epoch"].(string); msgEpoch == CurrentEpoch() {
				sinceSeq = parseSinceSeq(msg)
			} else {
				logger.Ctx(ctx).Info().Str("session_id", client.sessionID).
					Msg("[WS] epoch mismatch on resume, full resync")
			}
			logger.Ctx(ctx).Info().Str("session_id", client.sessionID).Uint64("since_seq", sinceSeq).Msg("resume requested")
			go h.onConnect(client, sinceSeq, ctx)
		case "delivered":
			logger.Ctx(ctx).Debug().Str("session_id", client.sessionID).Interface("ack", msg).Msg("received delivered ack (legacy)")
		case "close":
			logger.Ctx(ctx).Info().Str("session_id", client.sessionID).Msg("visitor closed connection")
			return
		default:
			logger.Ctx(ctx).Warn().Str("session_id", client.sessionID).Str("msg_type", msgType).Msg("unhandled message type")
		}
	}
}

func handleAckMessage(client *Client, msg map[string]any) int {
	raw, ok := msg["seq"]
	if !ok {
		return 0
	}
	var seqs []uint64
	switch v := raw.(type) {
	case float64:
		seqs = []uint64{uint64(v)}
	case []any:
		for _, item := range v {
			if f, ok := item.(float64); ok {
				seqs = append(seqs, uint64(f))
			}
		}
	case []uint64:
		seqs = v
	}
	return GlobalPendingAck().Ack(client.sessionID, seqs...)
}

func parseSinceSeq(msg map[string]any) uint64 {
	if v, ok := msg["since_seq"].(float64); ok {
		return uint64(v)
	}
	return 0
}

func sendVisitorError(client *Client, msg string) {
	errBytes, _ := json.Marshal(map[string]any{
		"type": TypeError,
		"payload": map[string]any{
			"message": msg,
		},
	})
	sendToClient(client, errBytes)
}

type messageRateLimiter struct {
	rate   float64
	cap    float64
	tokens float64
	last   time.Time
	mu     sync.Mutex
}

func newMessageRateLimiter(rate, cap float64) *messageRateLimiter {
	return &messageRateLimiter{
		rate:   rate,
		cap:    cap,
		tokens: cap,
		last:   time.Now(),
	}
}

func (rl *messageRateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.last).Seconds()
	rl.last = now
	rl.tokens += elapsed * rl.rate
	if rl.tokens > rl.cap {
		rl.tokens = rl.cap
	}

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

func getVisitorTokenSecret() (string, error) {
	if cfg := config.GetAppConfig(); cfg.Security.VisitorTokenSecret != "" {
		return cfg.Security.VisitorTokenSecret, nil
	}

	return "", errors.New("VISITOR_TOKEN_SECRET 未配置")
}
