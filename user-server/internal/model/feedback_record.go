package model

import (
	"gorm.io/gorm"
	"time"
)

// FeedbackRecordORM 反馈记录持久化行
type FeedbackRecordORM struct {
	ID             uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID      string         `gorm:"type:varchar(64);index" json:"session_id"`
	CustomerID     string         `gorm:"type:varchar(64);index" json:"customer_id"`
	IntentType     string         `gorm:"type:varchar(50);index" json:"intent_type"`
	Confidence     float64        `gorm:"type:decimal(5,4);default:0" json:"confidence"`
	SOPName        string         `gorm:"type:varchar(100)" json:"sop_name"`
	AIReply        string         `gorm:"type:text" json:"ai_reply"`
	HumanReply     string         `gorm:"type:text" json:"human_reply"`
	CustomerAccept bool           `gorm:"default:false" json:"customer_accept"`
	Transferred    bool           `gorm:"default:false" json:"transferred"`
	TransferReason string         `gorm:"type:varchar(200)" json:"transfer_reason"`
	Tokens         int            `gorm:"default:0" json:"tokens"`
	LatencyMs      int            `gorm:"default:0" json:"latency_ms"`
	CreatedAt      time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 表名
func (FeedbackRecordORM) TableName() string { return "feedback_records" }
