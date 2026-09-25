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
	// Ok 是「这一帧自带的结论」，三态而非两态（批20b / A2）：
	//   true  = 这帧声明它观测到的结果为正（event：Host 回执无错；judge：裁决为是）
	//   false = 这帧声明结果为负
	//   nil   = 这帧不携带结论（command 帧写下时 Host 还没回执，批21 之后甚至可能根本没上线）
	// 旧实现是 bool 且 command 帧恒传字面量 true，于是审计流里每一条「下发」都自带一个 ✓，
	// 而那个 ✓ 在这行上没有任何含义——比没结论更糟，它把「试过」画成「成了」。
	// 指针 + 无默认值是有意的：带 default:false 的标签会让 gorm 把 nil 当成「用默认」写回 false，
	// 三态立刻退化成两态；列的可空性由 v3.44.0 迁移负责（旧列是 NOT NULL）。
	Ok        *bool     `gorm:"column:ok" json:"ok"`
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (BrowserCommandLog) TableName() string { return "browser_command_log" }
