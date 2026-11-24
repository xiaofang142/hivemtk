package model

// InboundMessage 三路消息入口（bridge HTTP / channelgw WS / webhook）归一化后的
// 统一收件箱信封。T015 只做归一化与投递收口骨架；线索触发与自动回复触发由
// 后续 US2/US3/US4 任务接入，本阶段不改变任何既有入站行为。
//
// 刻意不映射数据库表：真实落库仍由既有 inbox/message_hub 负责，避免重复收件箱。
type InboundMessage struct {
	EventID        string
	Source         string // bridge_http | channelgw_ws | webhook
	Channel        string
	AccountRef     string // 原始账号引用：可能是账号表 id，也可能是识别码
	IdentifyCode   string // 已识别出的识别码（若有）
	TenantID       uint
	ConversationID string
	SenderID       string
	SenderType     string
	MsgType        string
	Content        string
	IsGroup        bool
	GroupID        string
	Timestamp      int64
	Raw            []byte
	Extra          map[string]any
}
