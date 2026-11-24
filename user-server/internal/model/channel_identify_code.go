package model

import "time"

// ChannelIdentifyCode 渠道唯一识别码（替代旧 Bridge 通道凭证）。
//
// 对应顶层迁移 migrations/090_channel_identify_codes.sql。code 全局唯一，
// 是三条消息入口反查租户/账号的权威键；version 每次重置 +1。
//
// PrevCode / PrevValidUntil 只服务于「重置在途消息窗口」（T061）：重置瞬间仍在
// 飞行的旧码请求在短暂窗口内继续归属，窗口过后立即失效——这不是长期兼容期。
type ChannelIdentifyCode struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ChannelID uint   `gorm:"not null;uniqueIndex:global_uniq_channel_identify_channel" json:"channel_id"`
	Code      string `gorm:"type:varchar(128);not null;uniqueIndex:global_uniq_channel_identify_code" json:"code"`
	Version   int    `gorm:"default:1" json:"version"`

	// 重置在途窗口：旧码在 PrevValidUntil 之前仍被凭证闸门接受。
	PrevCode       string     `gorm:"type:varchar(128)" json:"prev_code,omitempty"`
	PrevValidUntil *time.Time `json:"prev_valid_until,omitempty"`

	CreatedAt time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	ResetAt   *time.Time `json:"reset_at,omitempty"`
}

// TableName 识别码表。
func (ChannelIdentifyCode) TableName() string { return "channel_identify_codes" }
