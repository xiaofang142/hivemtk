package model

import "time"

// BrowserProfileHealth 单主 Profile 的平台级健康监护行。
// 主 Profile 复用是本项目风控特色（不做多账号矩阵）：每平台一行，
// blocked=true 即熔断——同平台新任务拒绝/排队，人工恢复后清标记。
// 熔断状态必须落库（Executor 进程级单例禁内存计数）。
type BrowserProfileHealth struct {
	ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform  string     `gorm:"column:platform;type:varchar(64);uniqueIndex;not null" json:"platform"`
	Blocked   bool       `gorm:"column:blocked;not null;default:false" json:"blocked"`
	Reason    string     `gorm:"column:reason;type:text;not null;default:''" json:"reason"`
	BlockedAt *time.Time `gorm:"column:blocked_at" json:"blocked_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

// TableName 表名。
func (BrowserProfileHealth) TableName() string { return "browser_profile_health" }
