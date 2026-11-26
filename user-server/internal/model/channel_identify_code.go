package model

import "time"

// ChannelIdentifyCode 存量**渠道级**识别码（历史兼容凭证，替代旧 Bridge 通道凭证）。
//
// 语义澄清：识别码归「每个渠道账号一枚」，真正的权威列是
// ChannelAccount.IdentifyCode（全局唯一）。本表由 migrations/090 建立，
// 每行以 channel_id 唯一，仅承载历史渠道级码：凭证闸门（ValidCodes）与
// 解析器回退继续接受这些老码，保证历史扩展/上报端不因语义调整而 401。
// 管理面不再为本表生成新行；账号级新码只写 channel_accounts。
// version 是历史渠道码的重置代数，每次重置 +1。
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
