package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BrowserTask 浏览器自动化任务主体（寄生式 Chrome Native Messaging）
type BrowserTask struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"column:name;size:256;not null;index" json:"name"`
	Description string `gorm:"column:description;type:text" json:"description"`
	TaskType    string `gorm:"column:task_type;size:32;not null;default:one_shot;index" json:"task_type"` // one_shot / loop / cron / workflow
	Status      string `gorm:"column:status;size:32;not null;default:draft;index" json:"status"`          // draft / ready / running / paused / done / failed / archived
	Url         string `gorm:"column:url;size:2048;not null" json:"url"`
	// 步骤编排（显式原语模式）：[{"action":"click","target":"#submit"}, ...]
	Steps     datatypes.JSON `gorm:"column:steps;type:jsonb" json:"steps"`
	BrainMode bool           `gorm:"column:brain_mode;default:false" json:"brain_mode"`
	BrainGoal string         `gorm:"column:brain_goal;type:text" json:"brain_goal"`
	LlmPlanID *uint          `gorm:"column:llm_plan_id;index" json:"llm_plan_id,omitempty"`
	// 执行控制
	LoopCount  int `gorm:"column:loop_count;default:1" json:"loop_count"`
	DelayMs    int `gorm:"column:delay_ms;default:1000" json:"delay_ms"`
	TimeoutSec int `gorm:"column:timeout_sec;default:120" json:"timeout_sec"`
	// workflow 依赖（SetDependsOn 时 DFS 检环）
	DependsOnTaskID *uint  `gorm:"column:depends_on_task_id;index" json:"depends_on_task_id,omitempty"`
	DependsOnMode   string `gorm:"column:depends_on_mode;size:32;default:all_done" json:"depends_on_mode"` // all_done / any_success
	// 失败自动重试（session 级）
	RetryOnFail   bool `gorm:"column:retry_on_fail;default:false" json:"retry_on_fail"`
	RetryDelaySec int  `gorm:"column:retry_delay_sec;default:300" json:"retry_delay_sec"`
	MaxRetryTimes int  `gorm:"column:max_retry_times;default:3" json:"max_retry_times"`
	RetryCount    int  `gorm:"column:retry_count;default:0" json:"retry_count"`
	// 归属
	UserID    uint   `gorm:"column:user_id;index;not null" json:"user_id"`
	AccountID uint   `gorm:"column:account_id;index" json:"account_id"`
	Platform  string `gorm:"column:platform;size:32;not null;default:xiaohongshu;index" json:"platform"` // 平台标识：xiaohongshu / douyin / xianyu（L3 适配器 identifier）
	// 执行状态快照
	LastRunAt  *time.Time `gorm:"column:last_run_at" json:"last_run_at,omitempty"`
	LastResult string     `gorm:"column:last_result;type:text" json:"last_result,omitempty"`
	ErrorMsg   string     `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserTask) TableName() string { return "browser_tasks" }
