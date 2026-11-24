package model

import "time"

// Channel 渠道主数据（预设数据表驱动，一次配置全系统读取）。
//
// 对应顶层迁移 migrations/088_channels.sql；由 internal/pkg/db/migrate.go 的
// allModels() 登记 AutoMigrate 建表。code 是全局唯一语义（跨租户可读同一份预设），
// 故用 global_uniq_ 前缀避免被 tenancyidx.Normalize 复合化为 (tenant_id, code)。
type Channel struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID      uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Name    string `gorm:"type:varchar(100);not null" json:"name"`
	Code    string `gorm:"type:varchar(64);not null;uniqueIndex:global_uniq_channel_code" json:"code"`
	Type    string `gorm:"type:varchar(32);not null;default:'dm';index" json:"type"`
	Enabled bool   `gorm:"default:true" json:"enabled"`
	Remark  string `gorm:"type:varchar(500)" json:"remark"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 渠道主数据表。
func (Channel) TableName() string { return "channels" }
