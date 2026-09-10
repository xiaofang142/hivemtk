// reach_compliance_log.go 合规审计日志模型与批量落库仓储（五层 L5）
package model

import (
	"time"
)

// ReachComplianceLog 合规提醒审计日志（表 reach_compliance_log）
type ReachComplianceLog struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Channel     string    `gorm:"type:varchar(30);index" json:"channel"`
	RecipientID string    `gorm:"type:varchar(128)" json:"recipient_id"`
	CreatedAt   time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 指定表名
func (ReachComplianceLog) TableName() string { return "reach_compliance_log" }
