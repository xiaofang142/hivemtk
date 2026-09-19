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

	// Kind 区分两条互补的延迟语义：
	//   - quiet_hours（含改动前的历史空值行）：命中免打扰时段，窗口开放时首发
	//   - send_retry：实时投递失败后的持久化重试，到期重投同一份内容
	Kind string `gorm:"type:varchar(20);default:'quiet_hours'" json:"kind"`
	// Attempts 已重投次数（首次入队为 0），达到上限后判 failed 不再重投。
	Attempts int `gorm:"not null;default:0" json:"attempts"`
	// LastError 最近一次投递失败的原因，供排障与"为什么客户没收到"复盘。
	LastError string `gorm:"type:text" json:"last_error"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (DelayedOutboundReply) TableName() string { return "reach_delayed_outbound" }

// 延迟出站条目类别与终态
const (
	DelayedKindQuietHours = "quiet_hours"
	DelayedKindSendRetry  = "send_retry"

	DelayedStatusPending = "pending"
	DelayedStatusSending = "sending"
	DelayedStatusSent    = "sent"
	DelayedStatusExpired = "expired"
	// DelayedStatusSuperseded 重投前发现会话已被回复（人工或后续 AI），这条旧内容不再补投
	DelayedStatusSuperseded = "superseded"
	// DelayedStatusFailed 重投次数用尽仍失败，保留行与 last_error 作为终态证据
	DelayedStatusFailed = "failed"
)
