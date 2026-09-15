package model

import (
	"database/sql/driver"
	"encoding/json"
	"gorm.io/gorm"
	"time"
)

// ReplyRule 回复规则
type ReplyRule struct {
	ID            string   `json:"id"`
	Priority      int      `json:"priority"`
	Keywords      []string `json:"keywords"`
	ReplyTemplate string   `json:"reply_template"`
	IsActive      bool     `json:"is_active"`
}

// 实现GORM的Serializer接口用于JSON序列化
func (r ReplyRule) Value() (driver.Value, error) {
	return json.Marshal(r)
}

func (r *ReplyRule) Scan(value any) error {
	if value == nil {
		return nil
	}
	return json.Unmarshal(value.([]byte), r)
}

type PlatformAccountConfig struct {
	ID                 string      `json:"id" gorm:"primaryKey;size:64"`
	AccountID          string      `json:"account_id" gorm:"size:255;not null"`
	Platform           string      `json:"platform" gorm:"size:50;not null"`
	RagProductID       *string     `json:"rag_product_id" gorm:"size:64"`
	IsAutoReplyEnabled bool        `json:"is_auto_reply_enabled" gorm:"default:false"`
	IsRagEnabled       bool        `json:"is_rag_enabled" gorm:"default:false"`
	ReplyRules         []ReplyRule `json:"reply_rules" gorm:"serializer:json"`
	MaxDailyQueries    int         `json:"max_daily_queries" gorm:"default:1000"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`

	RagProduct *RagProduct    `json:"rag_product,omitempty" gorm:"foreignKey:RagProductID"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 指定表名
func (PlatformAccountConfig) TableName() string {
	return "platform_account_configs"
}
