package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BrowserSession Chrome tab 会话（一次执行 = 一个 session）
// Chrome "寄生"式：复用主 Profile、后台 tab、不抢焦点
type BrowserSession struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TaskID      uint           `gorm:"column:task_id;index;not null" json:"task_id"`
	UserID      uint           `gorm:"column:user_id;index;not null" json:"user_id"`
	ChromeTabID int            `gorm:"column:chrome_tab_id;index" json:"chrome_tab_id"` // Chrome tab.id（扩展上报）
	Url         string         `gorm:"column:url;size:2048" json:"url"`
	Title       string         `gorm:"column:title;size:512" json:"title"`
	Status      string         `gorm:"column:status;size:32;not null;default:created;index" json:"status"` // created / active / completed / failed / stopped
	Snapshot    string         `gorm:"column:snapshot;type:text" json:"snapshot,omitempty"`
	LlmPlan     datatypes.JSON `gorm:"column:llm_plan;type:jsonb" json:"llm_plan,omitempty"`
	StartedAt   *time.Time     `gorm:"column:started_at" json:"started_at,omitempty"`
	CompletedAt *time.Time     `gorm:"column:completed_at" json:"completed_at,omitempty"`
	DurationMs  int64          `gorm:"column:duration_ms" json:"duration_ms"`
	ErrorMsg    string         `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`
	// 监控指标
	TotalSteps    int    `gorm:"column:total_steps;default:0" json:"total_steps"`
	SuccessSteps  int    `gorm:"column:success_steps;default:0" json:"success_steps"`
	FailedSteps   int    `gorm:"column:failed_steps;default:0" json:"failed_steps"`
	HandLatencyMs int64  `gorm:"column:hand_latency_ms" json:"hand_latency_ms"`
	ConsoleErrors string `gorm:"column:console_errors;type:text" json:"console_errors,omitempty"`
	// 反馈产物（截图存 LocalDriver 的 URL，不落 base64）
	ExtractedData      datatypes.JSON `gorm:"column:extracted_data;type:jsonb" json:"extracted_data,omitempty"`
	FinalScreenshotURL string         `gorm:"column:final_screenshot_url;size:1024" json:"final_screenshot_url,omitempty"`
	LlmSummary         string         `gorm:"column:llm_summary;type:text" json:"llm_summary,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserSession) TableName() string { return "browser_sessions" }
