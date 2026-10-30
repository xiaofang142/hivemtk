package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// WhatsappMessageTemplate WhatsApp 消息模板
type WhatsappMessageTemplate struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID       string `gorm:"type:varchar(64);primaryKey" json:"id"`
	Name     string `gorm:"type:varchar(255);not null" json:"name"`
	Content  string `gorm:"type:text;not null" json:"content"`
	Category string `gorm:"type:varchar(100);index" json:"category"`
	// IsActive 不能带 gorm 的 default 标签：bool + default:true 时 GORM 把显式 false
	// 当零值换成 true，创建时关不掉模板。默认值由控制器（未传 is_active 时补 true）负责。
	IsActive    bool      `gorm:"not null" json:"is_active"`
	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (*WhatsappMessageTemplate) TableName() string {
	return "whatsapp_message_templates"
}

// BeforeCreate 钩子：若未提供 ID 则自动生成
func (m *WhatsappMessageTemplate) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = "tmpl_" + uuid.New().String()
	}
	return nil
}
