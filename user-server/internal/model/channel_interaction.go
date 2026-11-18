package model

import "time"

// ChannelInteraction 我们发出的可交互卡片 / 平台侧点击的一次映射（specs/024 D8）。
//
// 一张卡片可能被点很多次，但每次点击在平台上有一个唯一 id（Telegram callback_query id、
// 企微/飞书回调的事件 id）。这张表回答三个问题：
//  1. 这次点击对应哪条卡片消息（PlatformMsgID）；
//  2. 点的是哪个动作（Action，平台按钮 payload）；
//  3. 答复过没有（Status），避免重复答复与「点了没反应」。
//
// 幂等键 (tenant_id, channel, interaction_id)：平台重投同一回调不建第二行。
type ChannelInteraction struct {
	TenantID uint   `gorm:"column:tenant_id;not null;default:1;uniqueIndex:idx_interaction_identity,priority:1" json:"tenant_id"`
	Channel  string `gorm:"column:channel;type:varchar(32);not null;uniqueIndex:idx_interaction_identity,priority:2" json:"channel"`
	// AccountID 入幂等键（specs/024 三审 T209 契约）：交互不能跨账号去重。
	AccountID      string  `gorm:"column:account_id;type:varchar(64);not null;default:'';uniqueIndex:idx_interaction_identity,priority:3" json:"account_id"`
	InteractionID  string  `gorm:"column:interaction_id;type:varchar(128);not null;uniqueIndex:idx_interaction_identity,priority:4" json:"interaction_id"`
	ID             uint    `gorm:"primaryKey;autoIncrement" json:"id"`
	ConversationID string  `gorm:"column:conversation_id;type:varchar(128);not null;default:'';index" json:"conversation_id"`
	PlatformMsgID  string  `gorm:"column:platform_msg_id;type:varchar(128);not null;default:'';index" json:"platform_msg_id"`
	SenderID       string  `gorm:"column:sender_id;type:varchar(128);not null;default:''" json:"sender_id"`
	Action         string  `gorm:"column:action;type:varchar(128);not null;default:''" json:"action"`
	Payload        JSONMap `gorm:"column:payload;type:jsonb" json:"payload,omitempty"`
	// Status: pending（已登记待答复）/ answered（已答复）/ expired（过期未答复）。
	Status     string     `gorm:"column:status;type:varchar(16);not null;default:'pending';index" json:"status"`
	AnsweredAt *time.Time `gorm:"column:answered_at" json:"answered_at,omitempty"`
	ExpiresAt  *time.Time `gorm:"column:expires_at;index" json:"expires_at,omitempty"`
	CreatedAt  time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

const (
	ChannelInteractionPending  = "pending"
	ChannelInteractionAnswered = "answered"
	ChannelInteractionExpired  = "expired"
)

func (ChannelInteraction) TableName() string { return "channel_interactions" }
