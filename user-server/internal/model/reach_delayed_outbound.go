// reach_delayed_outbound.go AI 回复延迟出站记录模型（表 reach_delayed_outbound）
package model

import (
	"time"
)

// DelayedOutboundReply AI 回复延迟出站记录（quiet hours 延迟队列）
type DelayedOutboundReply struct {
	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform       string     `gorm:"type:varchar(30);index" json:"platform"`
	AccountID      string     `gorm:"type:varchar(64)" json:"account_id"`
	ConversationID string     `gorm:"type:varchar(128);index" json:"conversation_id"`
	SenderID       string     `gorm:"type:varchar(128)" json:"sender_id"`
	Content        string     `gorm:"type:text" json:"content"`
	Cards          JSONMap    `gorm:"type:jsonb" json:"cards"`
	SendAt         time.Time  `gorm:"index" json:"send_at"`
	Status         string     `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	SentAt         *time.Time `json:"sent_at"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (DelayedOutboundReply) TableName() string { return "reach_delayed_outbound" }
