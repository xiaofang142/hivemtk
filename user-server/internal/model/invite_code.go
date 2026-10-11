package model

import "time"

// InviteCode 邀请码（SaaS P2 准入，2026-10-11）。
//
// 用途由注册模式决定（system_config.register_mode）：
//   - invite 模式：注册必须持有效邀请码，按码归入对应团队；
//   - default_team 模式：邀请码可选——持码注册=加入指定团队（免进默认团队）；
//   - create_team 模式：注册即建团队，邀请码不参与。
//
// 发码人：平台管理员（任意团队）或团队管理员（本团队）。
type InviteCode struct {
	ID   uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Code string `gorm:"column:code;type:varchar(32);not null;uniqueIndex" json:"code"`
	// TeamID 持码注册后归入的团队（0=默认团队）
	TeamID uint `gorm:"column:team_id;not null;index" json:"team_id"`
	// MaxUses 最大使用次数（0=不限）；UsedCount 已用次数
	MaxUses   int `gorm:"column:max_uses;not null;default:1" json:"max_uses"`
	UsedCount int `gorm:"column:used_count;not null;default:0" json:"used_count"`
	// ExpiresAt 过期时间（NULL=永久）
	ExpiresAt *time.Time `gorm:"column:expires_at;index" json:"expires_at"`
	// Status: active / disabled
	Status    string    `gorm:"column:status;type:varchar(16);not null;default:active;index" json:"status"`
	CreatedBy *uint     `gorm:"column:created_by;index" json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

func (InviteCode) TableName() string { return "invite_codes" }

const (
	InviteStatusActive   = "active"
	InviteStatusDisabled = "disabled"
)

// InviteUsable 报告该码当前是否可用（状态/次数/过期三判）。
func (c *InviteCode) InviteUsable(now time.Time) bool {
	if c.Status != InviteStatusActive {
		return false
	}
	if c.MaxUses > 0 && c.UsedCount >= c.MaxUses {
		return false
	}
	if c.ExpiresAt != nil && now.After(*c.ExpiresAt) {
		return false
	}
	return true
}
