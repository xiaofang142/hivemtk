package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CustomerTagAssignment 客户标签归属记录（客户级标签存储：每客户每标签一行，含置信度与添加时间）
type CustomerTagAssignment struct {
	ID         string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	CustomerID string    `gorm:"type:varchar(64);index:idx_customer_tag_assignment,unique;not null" json:"customer_id"`
	Tag        string    `gorm:"type:varchar(100);index:idx_customer_tag_assignment,unique;not null" json:"tag"`
	Category   string    `gorm:"type:varchar(32);index" json:"category"`
	Source     string    `gorm:"type:varchar(32)" json:"source"`
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 返回表名
func (CustomerTagAssignment) TableName() string {
	return "customer_tag_assignments"
}

// BeforeCreate 创建前钩子 - 自动生成 ID
func (a *CustomerTagAssignment) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}
