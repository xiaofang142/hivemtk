package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BrowserSession Chrome tab 会话（一次执行 = 一个 session）
// Chrome "寄生"式：复用主 Profile、后台 tab、不抢焦点
// G9 死字段收口（R25）：title/llm_plan/hand_latency_ms/console_errors 四列——
// 零生产/零消费（title 从未写入、llm_plan 被 browser_llm_plans 表取代、hand 延迟恒 0、
// console 不采集）——模型字段已删；DB 列按 D6 决策保留待统一清理，不动线上 DDL。
type BrowserSession struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	TaskID      uint       `gorm:"column:task_id;index;not null" json:"task_id"`
	UserID      uint       `gorm:"column:user_id;index;not null" json:"user_id"`
	ChromeTabID int        `gorm:"column:chrome_tab_id;index" json:"chrome_tab_id"` // Chrome tab.id（扩展上报）
	Url         string     `gorm:"column:url;size:2048" json:"url"`
	Status      string     `gorm:"column:status;size:32;not null;default:created;index" json:"status"` // created / active / completed / failed / stopped
	Snapshot    string     `gorm:"column:snapshot;type:text" json:"snapshot,omitempty"`
	StartedAt   *time.Time `gorm:"column:started_at" json:"started_at,omitempty"`
	CompletedAt *time.Time `gorm:"column:completed_at" json:"completed_at,omitempty"`
	DurationMs  int64      `gorm:"column:duration_ms" json:"duration_ms"`
	ErrorMsg    string     `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`
	// 监控指标
	TotalSteps   int `gorm:"column:total_steps;default:0" json:"total_steps"`
	SuccessSteps int `gorm:"column:success_steps;default:0" json:"success_steps"`
	FailedSteps  int `gorm:"column:failed_steps;default:0" json:"failed_steps"`
	// 反馈产物（截图存 LocalDriver 的 URL，不落 base64）
	ExtractedData      datatypes.JSON `gorm:"column:extracted_data;type:jsonb" json:"extracted_data,omitempty"`
	FinalScreenshotURL string         `gorm:"column:final_screenshot_url;size:1024" json:"final_screenshot_url,omitempty"`
	LlmSummary         string         `gorm:"column:llm_summary;type:text" json:"llm_summary,omitempty"`
	// ConfirmPending D7 运行时位（不落库）：该 session 当前是否停在 require_confirm 闸门
	// 等人工放行，由 controller 读侧从 Executor 实时状态填充。
	ConfirmPending bool `gorm:"-" json:"confirm_pending"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserSession) TableName() string { return "browser_sessions" }
