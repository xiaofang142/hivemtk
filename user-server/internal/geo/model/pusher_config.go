package model

import (
	"time"

	"github.com/google/uuid"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// GeoPusherConfig 蜘蛛平台配置
//
// 每个引擎一份配置（百度 token、Google Service Account JSON、
// IndexNow key 等）。config_json 用 AES-256-GCM 加密后存。
type GeoPusherConfig struct {
	ID          string         `gorm:"type:varchar(36);primaryKey" json:"id"`
	Platform    string         `gorm:"type:varchar(30);uniqueIndex" json:"platform"`
	ConfigJSON  datatypes.JSON `gorm:"type:jsonb;not null" json:"config_json"`
	DailyLimit  int            `json:"daily_limit"`
	UsedToday   int            `gorm:"default:0" json:"used_today"`
	LastResetAt *time.Time     `json:"last_reset_at"`
	Active      bool           `gorm:"default:true" json:"active"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (GeoPusherConfig) TableName() string { return "geo_pusher_configs" }

func (c *GeoPusherConfig) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
