package model

import (
	"gorm.io/gorm"
	"time"
)

// SessionStatus 会话状态
type SessionStatus string

const (
	SessionStatusPending       SessionStatus = "pending"
	SessionStatusAIHandling    SessionStatus = "ai_handling"
	SessionStatusHumanHandling SessionStatus = "human_handling"
	SessionStatusWaiting       SessionStatus = "waiting"
	SessionStatusResolved      SessionStatus = "resolved"
	SessionStatusClosed        SessionStatus = "closed"
)

// HandlerType 处理者类型
type HandlerType string

const (
	HandlerTypeAI    HandlerType = "ai"
	HandlerTypeHuman HandlerType = "human"
)

// CustomerSession 客服会话
type CustomerSession struct {
	ID          uint          `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID   string        `gorm:"type:varchar(120);uniqueIndex;not null" json:"session_id"`
	Platform    Platform      `gorm:"type:varchar(20)" json:"platform"`
	AccountID   string        `gorm:"type:varchar(50)" json:"account_id"`
	UserID      string        `gorm:"type:varchar(50);index" json:"user_id"`
	OneID       string        `gorm:"type:varchar(100);index:idx_sessions_one_id_status" json:"one_id"`
	UserName    string        `gorm:"type:varchar(100)" json:"user_name"`
	UserAvatar  string        `gorm:"type:varchar(500)" json:"user_avatar"`
	UserPhone   string        `gorm:"type:varchar(20)" json:"user_phone"`
	UserEmail   string        `gorm:"type:varchar(100)" json:"user_email"`
	Status      SessionStatus `gorm:"type:varchar(20);default:'pending'" json:"status"`
	HandlerType HandlerType   `gorm:"type:varchar(20)" json:"handler_type"`

	// D20: 转人工 outcome 追踪（episode：handoff_at → first_human_reply_at）
	HandoffAt         *time.Time     `gorm:"index" json:"handoff_at,omitempty"`
	HandoffReason     string         `gorm:"type:varchar(200)" json:"handoff_reason,omitempty"`
	FirstHumanReplyAt *time.Time     `json:"first_human_reply_at,omitempty"`
	AgentID           uint           `gorm:"index" json:"agent_id"`
	AgentName         string         `gorm:"type:varchar(100)" json:"agent_name"`
	Priority          int            `gorm:"default:0" json:"priority"`
	SnoozedUntil      *time.Time     `gorm:"index" json:"snoozed_until"`
	LastMessage       string         `gorm:"type:text" json:"last_message"`
	LastMessageAt     *time.Time     `json:"last_message_at"`
	LastMessageBy     string         `gorm:"type:varchar(20)" json:"last_message_by"`
	MessageCount      int            `gorm:"default:0" json:"message_count"`
	AIReplyCount      int            `gorm:"default:0" json:"ai_reply_count"`
	HumanReplyCount   int            `gorm:"default:0" json:"human_reply_count"`
	AvgResponseTime   int            `json:"avg_response_time"`
	Rating            int            `json:"rating"`
	RatingComment     string         `gorm:"type:text" json:"rating_comment"`
	Tags              string         `gorm:"type:text" json:"tags"`
	DNCBlocked        bool           `gorm:"default:false;index" json:"dnc_blocked"`
	Version           int            `gorm:"default:1" json:"version"`
	CreatedAt         time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	ResolvedAt        *time.Time     `json:"resolved_at"`
	ClosedAt          *time.Time     `json:"closed_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 指定表名
func (CustomerSession) TableName() string {
	return "customer_sessions"
}
