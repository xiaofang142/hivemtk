package model

import (
	"time"

	"gorm.io/gorm"
)

// UserMFA 用户 MFA 配置
// 私域独立部署：无 merchant_id 字段
//
// 设计要点：
//   - 一对一关联 SystemUser（user_id 唯一索引）
//   - mfa_secret 存储 base32 编码的 TOTP 密钥（本私有化单用户部署直接明文存储，不做额外加密包装）
//   - mfa_enabled: true=已启用 / false=已禁用
//   - backup_codes: 一次性恢复码 JSON 数组（bcrypt 哈希后存储）
//   - last_used_at: 最近一次 TOTP 验证时间（用于检测重放）
type UserMFA struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       uint       `gorm:"uniqueIndex;not null" json:"user_id"`
	MFASecret    string     `gorm:"type:varchar(255);not null" json:"-"`
	MFAEnabled   bool       `gorm:"default:false" json:"mfa_enabled"`
	MFAType      string     `gorm:"type:varchar(20);default:'totp'" json:"mfa_type"`
	BackupCodes  string     `gorm:"type:text" json:"-"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	LastUsedCode string     `gorm:"type:varchar(20)" json:"-"`
	EnabledAt    *time.Time `json:"enabled_at"`
	DisabledAt   *time.Time `json:"disabled_at"`
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (UserMFA) TableName() string {
	return "user_mfa"
}

// MFAType 常量
const (
	MFATypeTOTP = "totp"
)

// BeforeCreate GORM 钩子
func (m *UserMFA) BeforeCreate(tx *gorm.DB) error {
	if m.MFAType == "" {
		m.MFAType = MFATypeTOTP
	}
	return nil
}
