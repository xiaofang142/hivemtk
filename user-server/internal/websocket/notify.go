package websocket

import (
	"strconv"
)

// 消息类型常量（统一管理）
const (
	TypeNewSession    = "new_session"
	TypeNewMessage    = "new_message"
	TypeSessionUpdate = "session_update"

	TypeWelcome     = "welcome"
	TypeMessage     = "message"
	TypeAgentJoined = "agent_joined"
	TypeAITyping    = "ai_typing"
	TypeError       = "error"
	TypePong        = "pong"
)

// SendToAgent 发送消息给指定客服（坐席 ID 为 uint）
//
// 内部走 Envelope：分配全局 seq 编号，便于客户端排序 / 丢包检测。
// 不自动 track ACK（agent 端仅用 seq 做排序；ack 跟踪仅 visitor 端）。
func SendToAgent(messageType string, payload any, agentID uint) error {
	agentIDStr := strconv.FormatUint(uint64(agentID), 10)
	env := MustEnvelope(NextSeq(), messageType, payload)
	return GetHub().sendToAgentWithEnvelope(agentIDStr, env)
}

// SendToVisitor 发送消息给指定会话的访客
//
// 内部走 Envelope：分配全局 seq + 自动 Track ACK，
// 客户端发 `{"type":"ack","seq":N}` 后由 GlobalPendingAck().Ack 清理。
// 通过 sessionID 路由到对应的访客 WebSocket 连接。
//
// 鲁棒性加固（方向B）：
//   - 走 MustEnvelope 分配 seq，与 visitor_handler.onConnect / hub.Broadcast 对齐
//   - 自动 GlobalPendingAck().Track，确保重连后可通过 PendingSince 拉取未确认
//   - 客户端断开时由 visitor_handler.readPump 的 defer Drop 清理
func SendToVisitor(messageType string, payload any, sessionID string) error {
	return GetHub().SendToVisitor(sessionID, messageType, payload)
}

// BroadcastToAll 广播给所有客户端（坐席 + 访客）
func BroadcastToAll(messageType string, payload any) error {
	return GetHub().BroadcastToAll(messageType, payload)
}

// BroadcastToAgents 广播给所有坐席（排除访客）
func BroadcastToAgents(messageType string, payload any) error {
	return GetHub().BroadcastToAgents(messageType, payload)
}

// SendToVisitor 发送消息给指定会话的访客
//
// 走 Envelope：分配 seq + Track ACK；
// 客户端通过 `{"type":"ack","seq":N}` 确认收到。
func (h *Hub) SendToVisitor(sessionID, messageType string, payload any) error {
	if sessionID == "" {
		return nil
	}

	visitor := getVisitorClient(sessionID)
	if visitor == nil {
		return nil
	}

	env := MustEnvelope(NextSeq(), messageType, payload)
	bytes, err := env.MarshalBytes()
	if err != nil {
		return err
	}
	GlobalPendingAck().Track(sessionID, env.Seq)

	select {
	case visitor.send <- bytes:
	default:
	}
	return nil
}

// BroadcastToAll 广播给所有客户端
//
// 走 Envelope 统一分配 seq：保证全站消息序号单调递增。
// agent 端不自动 Track ACK（agent 端仅用 seq 做排序，不维护待 ACK 队列）。
// visitor 端不自动 Track ACK（仅 SendToVisitor 单播走 ACK 跟踪，广播消息的 ACK
// 由 visitor_handler.onConnect 的 offline_messages 路径统一处理）。
func (h *Hub) BroadcastToAll(messageType string, payload any) error {
	env := MustEnvelope(NextSeq(), messageType, payload)
	bytes, err := env.MarshalBytes()
	if err != nil {
		return err
	}

	h.mu.RLock()
	for _, client := range h.clients {
		select {
		case client.send <- bytes:
		default:
		}
	}
	h.mu.RUnlock()

	visitorClientsMu.RLock()
	for _, client := range visitorClients {
		select {
		case client.send <- bytes:
		default:
		}
	}
	visitorClientsMu.RUnlock()

	return nil
}

// BroadcastToAgents 广播给所有坐席（不含访客）
//
// 走 Envelope 统一分配 seq：保证全站消息序号单调递增。
func (h *Hub) BroadcastToAgents(messageType string, payload any) error {
	env := MustEnvelope(NextSeq(), messageType, payload)
	bytes, err := env.MarshalBytes()
	if err != nil {
		return err
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		if client.IsVisitor() {
			continue
		}
		select {
		case client.send <- bytes:
		default:
		}
	}
	return nil
}
