package model

import (
	"time"

	"gorm.io/datatypes"
)

// BrowserCommandLog append-only 命令-事件日志（设计稿 P8：durable execution 单机版）。
// 每条 = Executor 下发的一个命令帧及其回包/错误，断点续跑=重放（M4 接 DBOS，本表先落事实）。
// 不提供 Update/Delete：审计与重放都依赖不可变性。
type BrowserCommandLog struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	SessionID  uint           `gorm:"column:session_id;index;not null" json:"session_id"`
	TaskID     uint           `gorm:"column:task_id;index;not null" json:"task_id"`
	StepID     uint           `gorm:"column:step_id;index" json:"step_id"`
	Seq        int            `gorm:"column:seq;not null" json:"seq"`                     // session 内单调递增
	Direction  string         `gorm:"column:direction;size:16;not null" json:"direction"` // command / event（回包）
	Action     string         `gorm:"column:action;size:32;not null" json:"action"`
	Payload    datatypes.JSON `gorm:"column:payload;type:jsonb" json:"payload"` // 命令帧或回包/错误全文
	DurationMs int64          `gorm:"column:duration_ms" json:"duration_ms"`
	Ok         bool           `gorm:"column:ok;not null;default:false" json:"ok"`
	CreatedAt  time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
}

func (BrowserCommandLog) TableName() string { return "browser_command_log" }
