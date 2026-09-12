package model

import (
	"time"

	"gorm.io/gorm"
)

// BrowserCronTrigger 定时触发器（调度由 internal/pkg/cron TaskManager 接管）
// CronExpr 存储 5 段表达式（前端展示习惯），注册到 TaskManager 前补秒段转 6 段
type BrowserCronTrigger struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	TaskID    uint       `gorm:"column:task_id;uniqueIndex;not null" json:"task_id"`
	CronExpr  string     `gorm:"column:cron_expr;size:128;not null" json:"cron_expr"` // "*/5 * * * *"
	Enabled   bool       `gorm:"column:enabled;default:true;index" json:"enabled"`
	NextRunAt *time.Time `gorm:"column:next_run_at" json:"next_run_at,omitempty"`
	LastRunAt *time.Time `gorm:"column:last_run_at" json:"last_run_at,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserCronTrigger) TableName() string { return "browser_cron_triggers" }
