package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BrowserLLMPlan LLM 生成的执行计划（Brain 层产物）
// Brain 层吃 accessibility snapshot → 产出 plan → Translator 层翻译成 steps → Hand 执行
// v3.40.0（G3/D3）：session_id + kind 列——plan/judge/summary 三类 LLM 消耗全落库，
// session 级成本账可审计（BRAIN_PRODUCT_SPEC P1-1 收口）。
type BrowserLLMPlan struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	TaskID    uint           `gorm:"column:task_id;index;not null" json:"task_id"`
	SessionID uint           `gorm:"column:session_id;index;not null;default:0" json:"session_id"`
	Kind      string         `gorm:"column:kind;size:16;not null;default:plan" json:"kind"` // plan / judge / summary
	Goal      string         `gorm:"column:goal;type:text;not null" json:"goal"`
	Snapshot  string         `gorm:"column:snapshot;type:text" json:"snapshot,omitempty"`
	Steps     datatypes.JSON `gorm:"column:steps;type:jsonb" json:"steps"`
	Reasoning string         `gorm:"column:reasoning;type:text" json:"reasoning,omitempty"` // 调试用，落库前脱敏截断
	Model     string         `gorm:"column:model;size:64" json:"model"`
	TokenIn   int            `gorm:"column:token_in" json:"token_in"`
	TokenOut  int            `gorm:"column:token_out" json:"token_out"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserLLMPlan) TableName() string { return "browser_llm_plans" }
