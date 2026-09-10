package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BrowserStep 任务内的执行步骤（每次执行产生一条 step 记录，用于回放 + 调试）
type BrowserStep struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	SessionID  uint           `gorm:"column:session_id;index;not null" json:"session_id"`
	TaskID     uint           `gorm:"column:task_id;index;not null" json:"task_id"`
	StepIndex  int            `gorm:"column:step_index;not null" json:"step_index"`
	Action     string         `gorm:"column:action;size:32;not null;index" json:"action"` // open_tab / click / type / snapshot / markdown / screenshot / wait / wait_for_selector / scroll / extract / close_tab
	Target     string         `gorm:"column:target;size:1024" json:"target"`              // selector 或 @e3 refs
	Value      string         `gorm:"column:value;type:text" json:"value"`                // type 动作的输入值
	Params     datatypes.JSON `gorm:"column:params;type:jsonb" json:"params"`             // wait/scroll/extract/screenshot 等扩展参数
	Status     string         `gorm:"column:status;size:32;not null;default:pending;index" json:"status"` // pending / running / success / failed / skipped
	Result     datatypes.JSON `gorm:"column:result;type:jsonb" json:"result,omitempty"`   // action 返回值（snapshot/extract 结果）
	DurationMs int64          `gorm:"column:duration_ms" json:"duration_ms"`
	ErrorMsg   string         `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserStep) TableName() string { return "browser_steps" }
