package websocket

import (
	"context"
	"encoding/json"
	"time"
)

// ClientType 客户端类型
type ClientType string

const (
	ClientTypeAgent   ClientType = "agent"
	ClientTypeVisitor ClientType = "visitor"
)

// MsgType WebSocket 消息类型常量
//
// 统一管理各业务模块推送的消息类型字符串，
// 避免散落在调用方导致命名不一致。
const (
	MsgTypeSOP = "sop_message"
)

const (
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	writeWait      = 10 * time.Second
	maxMessageSize = 8192
)

// Envelope 信封：所有 WebSocket 下行消息统一外层结构
//
// 设计动机（方向B 鲁棒性加固）：
//   - seq：全局唯一递增序号，用于客户端断点续传 / 排序 / 丢包检测
//   - ts：服务端发送时间戳（Unix 毫秒），便于客户端做时序展示
//   - payload：业务原始负载（type + data），保持向后兼容
//
// 客户端协议：
//   - 重连时发 `{"type":"resume","since_seq":N}`，服务端返回 seq>N 的所有消息
//   - 收到消息后发 `{"type":"ack","seq":N}`，服务端清理待 ACK 队列
type Envelope struct {
	Seq     uint64          `json:"seq,omitempty"`
	TS      int64           `json:"ts,omitempty"`
	Epoch   string          `json:"epoch,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// AgentSessionExecutor 坐席 WebSocket 操作执行器接口
//
// 设计动机：WebSocket 上行消息（已读 / 接管 / 转接 / 关闭）会触达 CustomerSessionService
// 的领域方法。为避免 websocket 包反向 import service（导致 import cycle），
// 在 web 层（websocket 包）定义本接口，由 service 层提供实现（ws_agent_executor.go）。
//
// 调用方：WSHandler / VisitorWSHandler
// 实现方：service.WSAgentExecutor
//
// sessionID 语义：WebSocket 协议使用业务字符串 session_id（sess_xxx），实现方负责
// 兼容数字主键与业务字符串两种形态。
type AgentSessionExecutor interface {
	MarkSessionRead(ctx context.Context, agentID uint, sessionID string) error
	TakeoverSession(ctx context.Context, agentID uint, sessionID string, reason string) error
	TransferSession(ctx context.Context, fromAgentID uint, sessionID string, toAgentID uint) error
	CloseSession(ctx context.Context, agentID uint, sessionID string) error
}
