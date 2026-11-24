package model

import "time"

// FollowupRecord 完整跟进记录（PR-4/R-03）。FollowUpReminder 仅提醒语义。
type FollowupRecord struct {
	TenantID       uint       `gorm:"not null;default:1;index" json:"tenant_id"`
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	RecordID       string     `gorm:"type:varchar(32);not null;index" json:"record_id"`
	CustomerID     string     `gorm:"type:varchar(36);not null;default:'';index" json:"customer_id"`
	CompanyID      *int64     `gorm:"index" json:"company_id,omitempty"`
	ContactID      *int64     `gorm:"index" json:"contact_id,omitempty"`
	OpportunityID  *int64     `gorm:"index" json:"opportunity_id,omitempty"`
	ReminderID     string     `gorm:"type:varchar(32);not null;default:'';index" json:"reminder_id"`
	OwnerID        uint       `gorm:"not null;default:0;index" json:"owner_id"`
	FollowupType   string     `gorm:"type:varchar(32);not null;default:'note'" json:"followup_type"`
	Channel        string     `gorm:"type:varchar(32);not null;default:''" json:"channel"`
	Content        string     `gorm:"type:text;not null;default:''" json:"content"`
	Result         string     `gorm:"type:text;not null;default:''" json:"result"`
	NextAction     string     `gorm:"type:text;not null;default:''" json:"next_action"`
	NextFollowupAt *time.Time `gorm:"index" json:"next_followup_at,omitempty"`
	Source         string     `gorm:"type:varchar(64);not null;default:''" json:"source"`
	SourceRef      string     `gorm:"type:varchar(255);not null;default:''" json:"source_ref"`
	EvidenceJSON   string     `gorm:"type:jsonb;not null;default:'[]'" json:"evidence_json"`
	CreatedBy      *uint      `json:"created_by,omitempty"`
	UpdatedBy      *uint      `json:"updated_by,omitempty"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (FollowupRecord) TableName() string { return "followup_records" }
