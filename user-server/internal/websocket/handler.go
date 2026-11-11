package websocket

import (
	"context"
	"encoding/json"
	"hivemtk-user/internal/pkg/tenant"
	"net/http"
	"os"
	"strconv"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service/translation"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// isAllowedOrigin 判定 WS 握手 Origin 是否放行。
//
// r 传入请求是为了做同源判定：同源（Origin 的 host == 请求 Host）一律放行，
// 见 config.IsSameOriginWS 的说明（同源部署下白名单漏配会让坐席
// 实时通知 WS 恒 403 并触发前端重连循环）。
func isAllowedOrigin(r *http.Request) bool {
	origin := ""
	if r != nil {
		origin = r.Header.Get("Origin")
	}
	if origin == "" {
		return true
	}
	if config.IsSameOriginWS(origin, r) {
		return true
	}
	allowed := config.GetAllowedWSOrigins()
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	return false
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,

	CheckOrigin: func(r *http.Request) bool {
		return isAllowedOrigin(r)
	},
}

// wsAgentAllowedRoles 允许订阅坐席 WS 的角色集合（admin + 坐席角色；
// manager 死词已随 A-23 清除——system_users.role 白名单无 manager，无签发链恒假）
var wsAgentAllowedRoles = map[string]bool{
	"admin":            true,
	"staff":            true,
	"customer_service": true,
}

func roleStrOf(v any) string {
	s, _ := v.(string)
	return s
}

const heartbeatTouchInterval = 30 * time.Second

var heartbeatToucher func(ctx context.Context, agentID uint) error

// SetHeartbeatToucher 注入心跳上报实现（启动时调用一次）
func SetHeartbeatToucher(fn func(ctx context.Context, agentID uint) error) {
	heartbeatToucher = fn
}

func touchHeartbeat(ctx context.Context, agentID uint) {
	if heartbeatToucher == nil {
		return
	}
	if err := heartbeatToucher(ctx, agentID); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("agent_id", agentID).Msg("[S-3] ws heartbeat touch failed")
	}
}

// WSHandler WebSocket 处理器
type WSHandler struct {
	hub          *Hub
	langResolver *translation.LangConfigResolver
}

// VisitorChannelView 访客侧渠道视图（websocket 自有，避免包间耦合）
type VisitorChannelView struct {
	ChannelID string
	TenantID  uint
}

// VisitorChannelResolver 访客侧渠道解析窄接口（装配层由 service.ChatChannelService
// 适配注入；nil = 未装配，访客连接不绑租户）
type VisitorChannelResolver interface {
	ResolveByChannelID(ctx context.Context, channelID string) (*VisitorChannelView, error)
}

// visitorChannelResolver 包级注入点（visitor handler 使用；装配期 Set 一次）
var visitorChannelResolver VisitorChannelResolver

// SetVisitorChannelResolver 装配层注入
func SetVisitorChannelResolver(r VisitorChannelResolver) { visitorChannelResolver = r }

// NewWSHandler 创建 WebSocket 处理器
func NewWSHandler() *WSHandler {
	return &WSHandler{
		hub: GetHub(),
	}
}

// SetLangResolver 注入多语言解析器（v1.2 出海方案）。
// 未注入时仍可正常工作，ctx 中语言走默认 zh 兜底。
func (h *WSHandler) SetLangResolver(r *translation.LangConfigResolver) {
	h.langResolver = r
}

// HandleWebSocket 处理 WebSocket 连接
func (h *WSHandler) HandleWebSocket(c *gin.Context) {
	agentIDStr := c.Query("agent_id")
	agentName := c.Query("agent_name")

	if agentIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "缺少必要参数：agent_id",
		})
		return
	}

	agentID, err := strconv.ParseUint(agentIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "无效的 agent_id",
		})
		return
	}

	if os.Getenv("WS_AGENT_ALLOW_ALL_USERS") != "true" {
		// 坐席订阅按设计面向客服工作台角色：admin/manager 及坐席（staff/customer_service）
		// 管理端页面 /api/agents/me 会为坐席返回 agent_id，messageHub/customerSession 据此自动订阅；
		// 若仅放行 admin，坐席登录后将陷入 403 重连循环
		if role, _ := c.Get("role"); !wsAgentAllowedRoles[roleStrOf(role)] {
			c.JSON(http.StatusForbidden, gin.H{"error": "仅管理员或坐席可订阅坐席通知"})
			return
		}
		// 坐席身份自证（P2-3）：agent_id ≡ 系统用户 ID（GetOrCreateAgentStatusByUserID
		// 以 user_id 建行），query 自报他人 agent_id = 冒名收听其通知队列 + 代打心跳。
		// 与令牌 user_id 不一致一律拒绝；admin 例外（管理台可代看任意坐席的队列）。
		uid, _ := c.Get("user_id")
		tokenUID, _ := uid.(uint)
		role, _ := c.Get("role")
		if uint(agentID) != tokenUID && roleStrOf(role) != "admin" {
			logger.Warnf("[SECURITY] ws agent_id 与令牌身份不符，拒绝订阅 claimed_agent_id=%d", agentID)
			c.JSON(http.StatusForbidden, gin.H{"error": "agent_id 与登录身份不符"})
			return
		}
	}

	ctx := logger.WithTraceID(c.Request.Context(), c.GetHeader("X-Trace-Id"))
	ctx = logger.WithModule(ctx, "websocket")

	ctx = injectLangToCtx(ctx, h.langResolver, "", 0)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("agent_id", agentIDStr).Msg("ws upgrade failed")
		return
	}

	client := NewWSClient(h.hub, agentIDStr, agentName)
	client.hub = h.hub
	// 租户分桶：auth 组已把租户绑进请求 ctx
	if tid, ok := tenant.FromContext(ctx); ok {
		client.tenantID = tid
	}

	h.hub.Register(client)

	// SafeGo：readPump 解析用户可控 WS 消息、writePump 常驻写泵——任一 panic
	// 不能击穿进程（连接级 defer 清理在 panic 展开时照常执行，SafeGo 只兜底层）
	utils.SafeGo(ctx, "ws.agent.write_pump", func(_ context.Context) {
		h.writePump(client, conn, ctx)
	})
	utils.SafeGo(ctx, "ws.agent.read_pump", func(_ context.Context) {
		h.readPump(client, conn, uint(agentID), ctx)
	})

	logger.Ctx(ctx).Info().
		Str("agent_name", agentName).
		Str("agent_id", agentIDStr).
		Msg("agent connected")
}

func (h *WSHandler) writePump(client *Client, conn *websocket.Conn, ctx context.Context) {
	defer func() {
		_ = conn.Close()
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
			w, err := conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}

			msg := map[string]any{
				"type":    "message",
				"payload": json.RawMessage(message),
				"time":    time.Now().Format("2006-01-02 15:04:05"),
			}

			msgBytes, _ := json.Marshal(msg)
			if _, err := w.Write(msgBytes); err != nil {
				_ = w.Close()
				return
			}

			if err := w.Close(); err != nil {
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

func (h *WSHandler) readPump(client *Client, conn *websocket.Conn, agentID uint, ctx context.Context) {
	defer func() {
		h.hub.Unregister(client)
		_ = conn.Close()
		logger.Ctx(ctx).Info().Uint("agent_id", agentID).Msg("agent disconnected")
	}()

	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	lastBeat := time.Now().Add(-heartbeatTouchInterval)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Ctx(ctx).Error().Err(err).Uint("agent_id", agentID).Msg("ws read error")
			}
			break
		}

		if time.Since(lastBeat) >= heartbeatTouchInterval {
			lastBeat = time.Now()
			touchHeartbeat(ctx, agentID)
		}

		var msg map[string]any
		if err := json.Unmarshal(message, &msg); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Uint("agent_id", agentID).Msg("message parse failed")
			continue
		}

		msgType, ok := msg["type"].(string)
		if !ok {
			continue
		}

		switch msgType {
		case "ping":
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
		case "mark_read":
			h.handleMarkRead(client, msg)
		case "session_action":
			h.handleSessionAction(client, msg)
		}
	}
}

func (h *WSHandler) handleMarkRead(client *Client, msg map[string]any) {
	sessionID, ok := msg["session_id"].(string)
	if !ok {
		return
	}

	logger.GetLogger().Info().
		Str("agent_name", client.agentName).
		Str("session_id", sessionID).
		Msg("mark session read")
}

func (h *WSHandler) handleSessionAction(client *Client, msg map[string]any) {
	action, ok := msg["action"].(string)
	if !ok {
		return
	}

	sessionID, _ := msg["session_id"].(string)

	logger.GetLogger().Info().
		Str("agent_name", client.agentName).
		Str("session_id", sessionID).
		Str("action", action).
		Msg("session action")

	switch action {
	case "take_over":
		h.handleTakeOver(client, sessionID)
	case "transfer":
		targetAgentID, _ := msg["target_agent_id"].(float64)
		h.handleTransfer(client, sessionID, uint(targetAgentID))
	case "close":
		h.handleClose(client, sessionID)
	}
}

func (h *WSHandler) handleTakeOver(client *Client, sessionID string) {
	logger.GetLogger().Info().
		Str("agent_name", client.agentName).
		Str("session_id", sessionID).
		Msg("take over session")
}

func (h *WSHandler) handleTransfer(client *Client, sessionID string, targetAgentID uint) {
	logger.GetLogger().Info().
		Str("agent_name", client.agentName).
		Str("session_id", sessionID).
		Uint("target_agent_id", targetAgentID).
		Msg("transfer session")
}

func (h *WSHandler) handleClose(client *Client, sessionID string) {
	logger.GetLogger().Info().
		Str("agent_name", client.agentName).
		Str("session_id", sessionID).
		Msg("close session")
}

// BroadcastMessage 向本租户全部坐席广播（ctx 必须携带租户，见 Hub.BroadcastToMerchant）
func (h *WSHandler) BroadcastMessage(ctx context.Context, messageType string, data any) error {
	return GetHub().BroadcastToMerchant(ctx, "", messageType, data)
}

// SendToAgent 发送消息给指定客服（ctx 必带租户，见 Hub.SendToAgentCtx）
func (h *WSHandler) SendToAgent(ctx context.Context, agentID uint, messageType string, data any) error {
	return GetHub().SendToAgentCtx(ctx, strconv.FormatUint(uint64(agentID), 10), messageType, data)
}
