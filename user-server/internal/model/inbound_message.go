package model

// InboundMessage 三路消息入口（bridge HTTP / channelgw WS / webhook）归一化后的
// 统一收件箱信封。T015 只做归一化与投递收口骨架；线索触发与自动回复触发由
// 后续 US2/US3/US4 任务接入，本阶段不改变任何既有入站行为。
//
// 刻意不映射数据库表：真实落库仍由既有 inbox/message_hub 负责，避免重复收件箱。
// InboundMemberRole 群成员的平台稳定身份与显式角色（CH-08）。
// 只承载证据，不在此判定 self：判定统一由 ChannelIdentityResolver 完成。
type InboundMemberRole struct {
	PlatformUserID string
	Role           string
}

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
	// SenderName 上报消息里的发送者名称（T03）。仅作为账号标记的弱提示兜底，
	// 稳定身份/显式角色优先；缺名称不猜，名称匹配不代表稳定身份。
	SenderName string
	// SelfPlatformID 本账号在平台上的稳定 userID（CH-08）。只有它或显式 self 角色
	// 能判定「自己」，名称/识别码/数据库账号 ID 一律不作为 self 证据。
	SelfPlatformID string
	// MemberRoles 群成员稳定 userID→角色证据；同 ID 多角色冲突时解析器拒绝（不猜）。
	MemberRoles    []InboundMemberRole
	MsgType        string
	Content        string
	IsGroup        bool
	GroupID        string
	Timestamp      int64
	Raw            []byte
	Extra          map[string]any
}
