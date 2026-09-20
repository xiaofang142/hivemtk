package channelgw

import (
	"strings"
	"time"

	"hivemtk-user/internal/model"
)

// 协议版本：
//   - v1: 初始版本（无 v 字段，默认按 v1 处理）
//   - v2: 加 protocol_version 字段、frame 增加 v 字段；保留 v1 向后兼容
//   - 后续破坏性变更必须 bump v+1，并写清迁移路径
const (
	ProtocolVersionV1      = 1
	ProtocolVersionV2      = 2
	CurrentProtocolVersion = ProtocolVersionV2
)

// 帧类型常量（渠道客户端 <-> 服务器 双向，HTTP 与 WS 传输共用同一套语义）。
const (
	FrameRegister       = "register"
	FrameRegisterReject = "register_rejected"
	FrameRegistered     = "registered"
	FrameInbound        = "inbound_message"
	FrameHistory        = "history"
	FramePong           = "pong"
	FrameAck            = "ack"
	FramePing           = "ping"
	FrameOutboundReply  = "outbound_reply"
	FrameConfigPush     = "config_push"
)

// 传输类型标识（渠道注册表声明每渠道支持的传输）。
type Transport string

const (
	TransportHTTP      Transport = "http"
	TransportWebSocket Transport = "websocket"
)

// HistoryItem 会话级 history 中的单轮消息（一个会话 = 一条消息，内含多轮历史）。
type HistoryItem struct {
	EventID    string `json:"event_id,omitempty"`
	SenderType string `json:"sender_type,omitempty"`
	SenderID   string `json:"sender_id,omitempty"`
	SenderName string `json:"sender_name,omitempty"`
	ReceiverID string `json:"receiver_id,omitempty"`
	MsgType    string `json:"msg_type"`
	Content    string `json:"content"`
	MediaURL   string `json:"media_url,omitempty"`
	Timestamp  int64  `json:"timestamp"`
	Direction  string `json:"direction,omitempty"`
	IsGroup    bool   `json:"is_group,omitempty"`
	GroupID    string `json:"group_id,omitempty"`
	GroupName  string `json:"group_name,omitempty"`
}

// IngestMessage 渠道 -> 服务器 上行统一消息（唯一线路协议）。
//
// 合并了历史三套结构（bridge.UnifiedMessage / bridge.HTTPIngestMessage /
// channelbot core.InboundMessage 的服务端侧对应物），字段名与 JSON tag
// 与既有前端（user-web/bridge types.js）严格兼容。
//
// 语义说明：
//   - inbound_message 与 history 两种上行帧复用本结构（通过 Frame.Type 区分语义）
//   - Direction / SenderType 仅在 history 场景使用（实时入站由服务端固定为 inbound）
//   - SenderType 仅供前端参考；服务端在入库环节按「内容是否命中本会话平台下发
//     (outbound)」权威重判自/他（见 InboxIngressService.isPlatformOutboundEcho）
//   - History 非空时为「会话级」帧：一个会话 = 一条消息，History 内含全部多轮
//   - ContentHash 前端按与服务端 ContentHashMsgID 同源算法生成（mh: 前缀 FNV-1a），
//     用作回环去重钩子2 的兜底依据
type IngestMessage struct {
	EventID        string         `json:"event_id,omitempty"`
	Channel        string         `json:"channel,omitempty"`
	AccountID      string         `json:"account_id,omitempty"`
	AgentID        uint           `json:"agent_id,omitempty"`
	AccountName    string         `json:"account_name,omitempty"`
	ConversationID string         `json:"conversation_id"`
	SenderID       string         `json:"sender_id"`
	SenderName     string         `json:"sender_name,omitempty"`
	ReceiverID     string         `json:"receiver_id,omitempty"`
	SenderType     string         `json:"sender_type,omitempty"`
	MsgType        string         `json:"msg_type"`
	Content        string         `json:"content"`
	MediaURL       string         `json:"media_url,omitempty"`
	Timestamp      int64          `json:"timestamp"`
	Direction      string         `json:"direction,omitempty"`
	IsGroup        bool           `json:"is_group,omitempty"`
	GroupID        string         `json:"group_id,omitempty"`
	GroupName      string         `json:"group_name,omitempty"`
	History        []*HistoryItem `json:"history,omitempty"`
	Extra          map[string]any `json:"extra,omitempty"`
	ContentHash    string         `json:"content_hash,omitempty"`
}

// IngestRequest HTTP 上报请求体（与扩展端 http-ingest.js 严格对齐）。
type IngestRequest struct {
	V              int              `json:"v,omitempty"`
	Channel        string           `json:"channel"`
	AccountID      string           `json:"account_id"`
	ConversationID string           `json:"conversation_id,omitempty"`
	Messages       []*IngestMessage `json:"messages"`
	ExpectReply    bool             `json:"expect_reply,omitempty"`
	TimeoutMs      int              `json:"timeout_ms,omitempty"`
	AccountName    string           `json:"account_name,omitempty"`
	AgentID        uint             `json:"agent_id,omitempty"`
	InternalOnly   bool             `json:"internal_only,omitempty"`
}

// IngestResult 单条消息处理结果（HTTP 响应与 WS ack 帧共用）。
//
// 注意：Duplicate / AIHandled 不要加 omitempty —— 结果对象里的布尔标志位必须
// 「始终显式出现」，否则客户端无法安全读取 result.duplicate（被省略后变成
// undefined/null，只能退化为 `=== true` 判断，易踩坑）。accepted 已显式无
// omitempty，此处与之保持一致。
type IngestResult struct {
	EventID   string `json:"event_id"`
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	AIHandled bool   `json:"ai_handled"`
	Reason    string `json:"reason,omitempty"`
}

type IngestResponse struct {
	OK         bool            `json:"ok"`
	Ingested   []*IngestResult `json:"ingested"`
	SessionID  string          `json:"session_id,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	ServerTime int64           `json:"server_time"`
}

// OutboundReply 服务器 -> 渠道 下行统一回复（WS outbound_reply 帧载荷）。
//
// Truncated：服务端截断 content 到 4KB 后置 true，客户端可据此提示「消息被截断」。
type OutboundReply struct {
	Channel        string `json:"channel"`
	AccountID      string `json:"account_id"`
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
	MsgType        string `json:"msg_type"`
	MediaURL       string `json:"media_url,omitempty"`
	ReplyToEventID string `json:"reply_to_event_id,omitempty"`
	Truncated      bool   `json:"truncated,omitempty"`
}

type OutboxMessage struct {
	MsgID          string         `json:"msg_id"`
	ConversationID string         `json:"conversation_id"`
	MsgType        string         `json:"msg_type"`
	Content        string         `json:"content"`
	MediaURL       string         `json:"media_url"`
	SenderID       string         `json:"sender_id"`
	ReceiverID     string         `json:"receiver_id"`
	IsAIReply      bool           `json:"is_ai_reply"`
	CreatedAt      time.Time      `json:"created_at"`
	Extra          map[string]any `json:"extra,omitempty"`
}

// Frame 通用帧（WS 传输线路格式；HTTP 传输使用 IngestRequest/Response）。
//
// 字段说明：
//   - V: 协议版本；缺省按 v1 处理，老客户端无需升级即可兼容
//   - Message: 单条上行消息（inbound_message / history / register 帧）
//   - Messages: 批量上行消息（inbound_message 帧可选，与 Message 合并处理）
//   - Reply: 下行回复（outbound_reply 帧载荷），MsgID 为下发队列关联键（ack 回写用）
//   - MsgIDs/Status: ack 帧（客户端确认下发 delivered）
//   - Results: ack 帧（服务端回执上行处理结果）
type Frame struct {
	V         int              `json:"v,omitempty"`
	Type      string           `json:"type"`
	Channel   string           `json:"channel,omitempty"`
	AccountID string           `json:"account_id,omitempty"`
	Seq       int64            `json:"seq,omitempty"`
	EventID   string           `json:"event_id,omitempty"`
	TraceID   string           `json:"trace_id,omitempty"`
	MsgID     string           `json:"msg_id,omitempty"`
	Message   *IngestMessage   `json:"message,omitempty"`
	Messages  []*IngestMessage `json:"messages,omitempty"`
	Reply     *OutboundReply   `json:"reply,omitempty"`
	MsgIDs    []string         `json:"msg_ids,omitempty"`
	Status    string           `json:"status,omitempty"`
	Results   []*IngestResult  `json:"results,omitempty"`
	Reason    string           `json:"reason,omitempty"`
}

// MessageEventID 透传帧内单条上行消息的 EventID（便于日志/去重）。
func (f *Frame) MessageEventID() string {
	if f == nil || f.Message == nil {
		return ""
	}
	return f.Message.EventID
}

// ProtocolVersion 透传 Frame.V（缺省按 v1 处理，向后兼容）。
func (f *Frame) ProtocolVersion() int {
	if f == nil || f.V == 0 {
		return ProtocolVersionV1
	}
	return f.V
}

// ToEvent 将上行消息转换为消息中台的 model.MessageEvent（不含 History 拷贝）。
//
// transport 标记来源传输（"http" / "websocket"），写入 Extra["transport"] 供可观测。
// History 字段不在此拷贝：HTTP/WS 传输均由传输层先逐条 PersistHistory 落库，
// 实时消息事件再入批处理管道，避免双重落库（与历史 handler 行为严格一致）。
func (m *IngestMessage) ToEvent(transport string) *model.MessageEvent {
	if m == nil {
		return nil
	}
	ts := time.UnixMilli(m.Timestamp)
	if m.Timestamp == 0 {
		ts = time.Now()
	}
	ev := &model.MessageEvent{
		EventID:        m.EventID,
		SessionID:      m.Channel + ":" + m.AccountID + ":" + m.ConversationID,
		Channel:        m.Channel,
		SenderID:       m.SenderID,
		SenderName:     m.SenderName,
		SenderType:     m.SenderType,
		ReceiverID:     m.ReceiverID,
		MsgType:        m.MsgType,
		Content:        m.Content,
		MediaURL:       m.MediaURL,
		ConversationID: m.ConversationID,
		IsGroup:        m.IsGroup,
		GroupID:        m.GroupID,
		Timestamp:      ts,
		Extra: map[string]any{
			"account_id":  m.AccountID,
			"bridge":      true,
			"sender_type": m.SenderType,
		},
	}
	if transport != "" {
		ev.Extra["transport"] = transport
	}
	if m.ContentHash != "" {
		ev.Extra["content_hash"] = m.ContentHash
	}
	if m.IsGroup {
		ev.Extra["is_group"] = true
	}
	if m.GroupID != "" {
		ev.Extra["group_id"] = m.GroupID
	}
	if m.GroupName != "" {
		ev.Extra["group_name"] = m.GroupName
	}
	return ev
}

// ToEventFull 与 ToEvent 相同，但额外把 History 拷贝到 MessageEvent.History
// 并冗余到 Extra["history"]（供统一收件箱展示/可观测；兼容旧统一收信中心读取路径）。
func (m *IngestMessage) ToEventFull(transport string) *model.MessageEvent {
	ev := m.ToEvent(transport)
	if ev == nil || len(m.History) == 0 {
		return ev
	}
	hist := make([]model.MessageEventHistoryItem, 0, len(m.History))
	for _, it := range m.History {
		if it == nil {
			continue
		}
		hist = append(hist, model.MessageEventHistoryItem{
			EventID:    it.EventID,
			SenderType: it.SenderType,
			SenderID:   it.SenderID,
			SenderName: it.SenderName,
			ReceiverID: it.ReceiverID,
			MsgType:    it.MsgType,
			Content:    it.Content,
			MediaURL:   it.MediaURL,
			Timestamp:  it.Timestamp,
			Direction:  it.Direction,
			IsGroup:    it.IsGroup,
			GroupID:    it.GroupID,
			GroupName:  it.GroupName,
		})
	}
	ev.History = hist
	ev.Extra["history"] = hist
	return ev
}

// HistoryToEvent 把会话级 history 中的单轮（HistoryItem）映射为 model.MessageEvent。
// 会话元数据（channel/account/conversation/群）取自帧顶层消息 parent，轮次字段取自 item。
func HistoryToEvent(parent *IngestMessage, it *HistoryItem) *model.MessageEvent {
	if parent == nil || it == nil {
		return nil
	}
	ts := time.UnixMilli(it.Timestamp)
	if it.Timestamp == 0 {
		ts = time.Now()
	}
	ev := &model.MessageEvent{
		EventID:        it.EventID,
		SessionID:      parent.Channel + ":" + parent.AccountID + ":" + parent.ConversationID,
		Channel:        parent.Channel,
		SenderID:       it.SenderID,
		SenderName:     it.SenderName,
		ReceiverID:     firstNonEmpty(it.ReceiverID, parent.ReceiverID),
		MsgType:        it.MsgType,
		Content:        it.Content,
		MediaURL:       it.MediaURL,
		ConversationID: parent.ConversationID,
		IsGroup:        it.IsGroup || parent.IsGroup,
		GroupID:        firstNonEmpty(it.GroupID, parent.GroupID),
		Timestamp:      ts,
		Extra: map[string]any{
			"account_id":  parent.AccountID,
			"bridge":      true,
			"sender_type": firstNonEmpty(it.SenderType, parent.SenderType),
		},
	}
	if ev.ReceiverID == "" && it.Direction == "outbound" {
		ev.ReceiverID = parent.ConversationID
	}
	if ev.IsGroup {
		ev.Extra["is_group"] = true
	}
	if ev.GroupID != "" {
		ev.Extra["group_id"] = ev.GroupID
	}
	if groupName := firstNonEmpty(it.GroupName, parent.GroupName); groupName != "" {
		ev.Extra["group_name"] = groupName
	}
	return ev
}

// duplicateOutcomePrefixes 服务端「这条已处理过」的**结论短语**，按前缀认。
// 与产出方逐字对齐：`inbox_ingress.go` 钩子2（msg_id 命中 / 方向冲突 / content_hash 回声）、
// 中间件拦截包装（其内层 self-echo/duplicate 两类结论），以及夹具 mock 的同形文案。
var duplicateOutcomePrefixes = []string{
	"msg_id already exists",
	"msg_id exists with different direction",
	"content_hash already exists",
	"intercepted by middleware",
	"self-echo",
	"self echo",
	"duplicate",
}

// IsDuplicateReason 判断入站处理结果原因是否属于「已被服务端幂等/拦截确认为重复」。
// 命中则传输层把该 event_id 标记 Duplicate，客户端据此停止重发（允许重复上报，
// 服务端用 ack 确认去重）。
//
// 只认结论前缀、不认任意子串：这条判定的下游是「客户端从此不再上报该 event_id」
// （`user-web/bridge/src/core/uplink.js` 的 `accepted || duplicate`），判错的代价是
// 一条消息永久消失。而 reason 里混得进**失败原文**——`inbox_ingress.go` 的批量分支会把
// 落库错误包成 `batch handle error: 持久化消息失败: <DB 原文>`，PG 唯一键冲突原文天然带
// "duplicate key value violates unique constraint"。用子串嗅探时「没存进去」就成了
// 「已经存过」，一次 DB 抖动替客户端做了永久决定。宁可漏判（客户端重报，服务端下一轮
// 用真结论短语作答）也不可误判。
func IsDuplicateReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false
	}
	for _, prefix := range duplicateOutcomePrefixes {
		if strings.HasPrefix(reason, prefix) {
			return true
		}
	}
	return false
}

func firstNonEmpty(v, def string) string {
	if v != "" {
		return v
	}
	return def
}
