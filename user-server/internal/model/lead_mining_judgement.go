package model

import "time"

// LeadMiningJudgement 线索发掘判定记录（LM-PR1）。每次判定可审计、可回放。
type LeadMiningJudgement struct {
	TenantID       uint       `gorm:"not null;default:1;index" json:"tenant_id"`
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	JudgementID    string     `gorm:"type:varchar(32);not null;index" json:"judgement_id"`
	MessageID      string     `gorm:"type:varchar(128);not null;default:'';index" json:"message_id"`
	ConversationID string     `gorm:"type:varchar(128);not null;default:'';index" json:"conversation_id"`
	Platform       string     `gorm:"type:varchar(32);not null;default:''" json:"platform"`
	AccountID      string     `gorm:"type:varchar(128);not null;default:''" json:"account_id"`
	IsGroup        bool       `gorm:"not null;default:false" json:"is_group"`
	GroupID        string     `gorm:"type:varchar(128);not null;default:''" json:"group_id"`
	SenderID       string     `gorm:"type:varchar(128);not null;default:''" json:"sender_id"`
	ContentHash    string     `gorm:"type:varchar(64);not null;default:''" json:"content_hash"`
	ContextJSON    string     `gorm:"type:jsonb;not null;default:'{}'" json:"context_json"`
	ResultJSON     string     `gorm:"type:jsonb;not null;default:'{}'" json:"result_json"`
	Matched        bool       `gorm:"not null;default:false" json:"matched"`
	LeadRole       string     `gorm:"type:varchar(16);not null;default:'unknown'" json:"lead_role"`
	IntentScore    int        `gorm:"not null;default:0" json:"intent_score"`
	Reason         string     `gorm:"type:text;not null;default:''" json:"reason"`
	FeedbackStatus string     `gorm:"type:varchar(16);not null;default:'none';index" json:"feedback_status"` // none/false_positive/valid
	FeedbackReason string     `gorm:"type:text;not null;default:''" json:"feedback_reason"`
	FeedbackBy     string     `gorm:"type:varchar(64);not null;default:''" json:"feedback_by"`
	FeedbackAt     *time.Time `json:"feedback_at,omitempty"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (LeadMiningJudgement) TableName() string { return "lead_mining_judgements" }

// LeadMiningNotifyRoute 线索发掘通知路由（LM-PR4）。
type LeadMiningNotifyRoute struct {
	TenantID      uint       `gorm:"not null;default:1;index" json:"tenant_id"`
	ID            int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	RouteID       string     `gorm:"type:varchar(32);not null;index" json:"route_id"`
	Channel       string     `gorm:"type:varchar(32);not null;default:'';index" json:"channel"`
	AccountID     string     `gorm:"type:varchar(128);not null;default:'';index" json:"account_id"`
	NotifyChannel string     `gorm:"type:varchar(16);not null;default:'in_app'" json:"notify_channel"` // in_app/telegram/feishu
	Target        string     `gorm:"type:varchar(255);not null;default:''" json:"target"`
	Enabled       bool       `gorm:"not null;default:true" json:"enabled"`
	CreatedBy     *uint      `json:"created_by,omitempty"`
	CreatedAt     time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (LeadMiningNotifyRoute) TableName() string { return "lead_mining_notify_routes" }
