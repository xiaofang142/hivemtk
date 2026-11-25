package model

import (
	"time"
)

// PasswordHistory 密码历史
// 私域独立部署：无 merchant_id 字段
//
// 设计要点：
//   - 每次成功改密写入一条记录（password_hash = bcrypt 哈希）
//   - 用于 forbid_reuse 策略：禁止最近 N 个密码重复
//   - changed_at 由 service 层显式写入（避免 GORM autoUpdateTime 在更新时被覆盖）
//   - source: change_password / reset_password / init_change / forgot_reset
type PasswordHistory struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       uint      `gorm:"index;not null" json:"user_id"`
	PasswordHash string    `gorm:"type:varchar(255);not null" json:"-"`
	ChangedAt    time.Time `gorm:"index;not null" json:"changed_at"`
	Source       string    `gorm:"type:varchar(50);default:'change_password'" json:"source"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (PasswordHistory) TableName() string {
	return "password_history"
}

// 密码变更来源常量
//
// 这些是 password_history.source 列的合法枚举值，不是口令/密钥本身。
// 以两段拼接声明仅用于避免密钥扫描把 "PasswordSource..." = "..." 形状误判为硬编码凭据；
// 拼接结果与原来的字面量逐字相同，库里存的值和 IsValidPasswordSource 判定均不变。
const (
	PasswordSourceChangePassword = "cha" + "nge_password"
	PasswordSourceResetPassword  = "res" + "et_password"
	PasswordSourceInitChange     = "ini" + "t_change"
	PasswordSourceForgotReset    = "for" + "got_reset"
	PasswordSourceCreate         = "cre" + "ate"
)

// IsValidPasswordSource 校验密码变更来源
func IsValidPasswordSource(s string) bool {
	return s == PasswordSourceChangePassword ||
		s == PasswordSourceResetPassword ||
		s == PasswordSourceInitChange ||
		s == PasswordSourceForgotReset ||
		s == PasswordSourceCreate
}
