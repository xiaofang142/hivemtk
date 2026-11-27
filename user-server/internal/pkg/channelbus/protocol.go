// Package channelbus 是统一渠道消息总线的**线路协议常量单源**。
//
// 这里只放无依赖的协议标识符与枚举值，供 channelgw（桥接 HTTP/WS）、service
// （webhook / SageX / embed 适配层）以及未来任何渠道兼容层直接引用；任何包
// 都不得再复制这些字面量。字段名/载荷结构仍由各传输定义，但协议身份、帧
// 类型、状态值必须从这里取，否则就又退回"每条渠道一套协议"的老路。
//
// 设计约束：
//   - 本包不得 import 任何 internal 业务包（保持零环依赖，谁都能引用）。
//   - 破坏性变更必须 bump ProtocolVersion，并在 docs/architecture/
//     UNIFIED_CHANNEL_MESSAGE_BUS.md 写迁移路径。
package channelbus

// 协议版本：v1 无 v 字段（缺省按 v1 处理）；v2 增加 v 字段与逐条 ack。
const (
	ProtocolVersionV1      = 1
	ProtocolVersionV2      = 2
	CurrentProtocolVersion = ProtocolVersionV2
)

// SchemaVersion 是线路协议在线上的身份标识。所有上报端点/适配层回执都必须
// 使用同一个值：bridge gateway、SageX upload、webhook ack、embed 归一口。
const SchemaVersion = "hivemtk.channel.v1"

// 上行逐条 ack 状态（ingest 语义）。
const (
	StatusAccepted  = "accepted"
	StatusDuplicate = "duplicate"
	StatusRetry     = "retry"
	StatusRejected  = "rejected"
)

// 下行投递终态（outbox ack 语义）。
const (
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
	StatusUnknown   = "unknown"
)

// 帧类型：HTTP 与 WebSocket 传输共用同一套语义。
const (
	FrameRegister       = "register"
	FrameRegisterReject = "register_rejected"
	FrameInbound        = "inbound_message"
	FrameHistory        = "history"
	FramePong           = "pong"
	FrameAck            = "ack"
	FramePing           = "ping"
	FrameOutboundReply  = "outbound_reply"
	FrameConfigPush     = "config_push"
)

// 传输类型标识。
const (
	TransportHTTP      = "http"
	TransportWebSocket = "websocket"
)
