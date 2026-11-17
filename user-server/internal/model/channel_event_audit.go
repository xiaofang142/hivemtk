package model

import "time"

// ChannelEventAudit 平台事件的落库证据（specs/024 D2/R3）。
//
// 为什么不是只发事件总线：总线是 best-effort，队列满即丢（event/bus.go），而「谁在什么时候
// 入群/退群/点了哪个按钮」是资产归属与纠纷复盘要用的证据，丢了就查无实据。
// 为什么不是 operation_logs：那张表的 tenant_id 是 uint 且语义是「后台人操作」，
// 平台事件是系统事件、租户键是字符串，混用会让查询口径变糊。
//
// 幂等：唯一键 (tenant_id, channel, channel_msg_id, event_type) —— 平台重投同一事件不产生第二行。
type ChannelEventAudit struct {
	TenantID       string `gorm:"column:tenant_id;type:varchar(64);not null;default:'';uniqueIndex:idx_channel_event_audit,priority:1" json:"tenant_id"`
	Channel        string `gorm:"column:channel;type:varchar(32);not null;uniqueIndex:idx_channel_event_audit,priority:2" json:"channel"`
	ChannelMsgID   string `gorm:"column:channel_msg_id;type:varchar(128);not null;default:'';uniqueIndex:idx_channel_event_audit,priority:3" json:"channel_msg_id"`
	EventType      string `gorm:"column:event_type;type:varchar(64);not null;uniqueIndex:idx_channel_event_audit,priority:4" json:"event_type"`
	ID             uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID      string `gorm:"column:account_id;type:varchar(64);not null;default:'';index" json:"account_id"`
	SenderID       string `gorm:"column:sender_id;type:varchar(128);not null;default:''" json:"sender_id"`
	SenderName     string `gorm:"column:sender_name;type:varchar(128);not null;default:''" json:"sender_name"`
	ConversationID string `gorm:"column:conversation_id;type:varchar(128);not null;default:'';index" json:"conversation_id"`
	RouteRuleID    uint   `gorm:"column:route_rule_id;not null;default:0" json:"route_rule_id"`
	// Payload 保留平台原始事件的关键字段（动作、成员、按钮值等），排查时不依赖外部日志。
	Payload   JSONMap   `gorm:"column:payload;type:jsonb" json:"payload,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime;index" json:"created_at"`
}

func (ChannelEventAudit) TableName() string { return "channel_event_audits" }
