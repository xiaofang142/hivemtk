package model

import "time"

const (
	BridgeLeadPending   = "pending"
	BridgeLeadClaimed   = "claimed"
	BridgeLeadRunning   = "running"
	BridgeLeadCompleted = "completed"
	BridgeLeadUnknown   = "unknown"
	BridgeLeadSkipped   = "skipped"
)

// BridgeLeadTask 仅引用可信消息，不复制客户正文或桥接凭证。
type BridgeLeadTask struct {
	ID             uint       `gorm:"primaryKey"`
	TenantID       uint       `gorm:"not null;uniqueIndex:uni_bridge_lead_hub,priority:1;index:idx_bridge_lead_due,priority:1"`
	HubID          uint       `gorm:"not null;uniqueIndex:uni_bridge_lead_hub,priority:2"`
	Channel        string     `gorm:"type:varchar(30);not null"`
	AccountID      string     `gorm:"type:varchar(100);not null"`
	ConversationID string     `gorm:"type:varchar(100);not null"`
	Status         string     `gorm:"type:varchar(20);not null;default:'pending';index:idx_bridge_lead_due,priority:2"`
	LeaseToken     string     `gorm:"type:varchar(64);not null;default:''"`
	LeaseUntil     *time.Time `gorm:"index:idx_bridge_lead_due,priority:3"`
	StartedAt      *time.Time
	CompletedAt    *time.Time
	LastError      string `gorm:"type:varchar(200);not null;default:''"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (BridgeLeadTask) TableName() string { return "bridge_lead_tasks" }
