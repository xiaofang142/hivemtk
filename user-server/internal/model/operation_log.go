package model

import "time"

// OperationLog 操作日志模型
type OperationLog struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID     uint      `gorm:"index;not null" json:"user_id"`
	Username   string    `gorm:"type:varchar(50)" json:"username"`
	Action     string    `gorm:"type:varchar(50);not null" json:"action"`
	Module     string    `gorm:"type:varchar(50);not null" json:"module"`
	Resource   string    `gorm:"type:varchar(50)" json:"resource"`
	ResourceID string    `gorm:"type:varchar(50)" json:"resource_id"`
	Detail     string    `gorm:"type:text" json:"detail"`
	OldValue   string    `gorm:"type:text" json:"old_value"`
	NewValue   string    `gorm:"type:text" json:"new_value"`
	IP         string    `gorm:"type:text" json:"ip"`
	UserAgent  string    `gorm:"type:text" json:"user_agent"`
	CreatedAt  time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 指定表名
func (OperationLog) TableName() string {
	return "operation_logs"
}
