package service

import "time"

// TriggerInboundOption 携带单次入站的权威元数据；消息内容不能修改这些策略字段。
type TriggerInboundOption func(*TriggerInboundMeta)

// TriggerInboundMeta 是消息总线入站元数据，保留原始身份/时间/总线会话键。
type TriggerInboundMeta struct {
	SenderName string
	IsGroup    bool
	GroupID    string
	GroupName  string

	// SessionWebhook 钉钉机器人临时回复通道（官方协议：回复即 POST 此 URL）。
	SessionWebhook          string
	SessionWebhookExpiredAt int64
	// ChannelMsgID 渠道平台原始消息 ID（如 QQ 事件 id）。
	ChannelMsgID string

	BusLLMSessionID string
	ConversationID  string
	MessageID       string
	Direction       string
	PlatformMsgID   string
	CorrelationID   string
	OriginalSentAt  time.Time
}

func WithSenderName(name string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.SenderName = name }
}

func WithGroup(groupID, groupName string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) {
		m.IsGroup = true
		m.GroupID = groupID
		m.GroupName = groupName
	}
}

func WithSessionWebhook(url string, expiredAt int64) TriggerInboundOption {
	return func(m *TriggerInboundMeta) {
		m.SessionWebhook = url
		m.SessionWebhookExpiredAt = expiredAt
	}
}

// WithChannelMsgID 传递渠道平台原始消息 ID（QQ 被动回复依赖）。
func WithChannelMsgID(msgID string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.ChannelMsgID = msgID }
}

func WithBusLLMSessionID(id string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.BusLLMSessionID = id }
}

func WithConversationID(id string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.ConversationID = id }
}

func WithMessageID(id string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.MessageID = id }
}

func WithDirection(direction string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.Direction = direction }
}

func WithPlatformMsgID(id string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.PlatformMsgID = id }
}

func WithCorrelationID(id string) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.CorrelationID = id }
}

func WithOriginalSentAt(at time.Time) TriggerInboundOption {
	return func(m *TriggerInboundMeta) { m.OriginalSentAt = at }
}

// EffectiveSentAt 只有缺少权威原始时间时才允许兜底，禁止把入站时刻覆盖为处理时刻。
func (m *TriggerInboundMeta) EffectiveSentAt(fallback time.Time) time.Time {
	if m != nil && !m.OriginalSentAt.IsZero() {
		return m.OriginalSentAt
	}
	return fallback
}
