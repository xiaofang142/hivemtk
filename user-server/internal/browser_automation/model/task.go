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
	// CopyText 触达任务文案：JEV TYPE_TEXT 唯一文本来源（JEV 绝不编造文本）。
	CopyText string `gorm:"column:copy_text;type:text" json:"copy_text"`
	// G9 死代码收口（R25）：LlmPlanID 零写入零消费（成本账走 browser_llm_plans 表
	// 按 task_id/session_id 维度查，D3 已落地）；字段删除，DB 列按 D6 决策保留不动。
	// 执行控制
	LoopCount  int `gorm:"column:loop_count;default:1" json:"loop_count"`
	DelayMs    int `gorm:"column:delay_ms;default:1000" json:"delay_ms"`
	TimeoutSec int `gorm:"column:timeout_sec;default:120" json:"timeout_sec"`
	// workflow 依赖（SetDependsOn 时 DFS 检环）
	DependsOnTaskID *uint  `gorm:"column:depends_on_task_id;index" json:"depends_on_task_id,omitempty"`
	DependsOnMode   string `gorm:"column:depends_on_mode;size:32;default:all_done" json:"depends_on_mode"` // all_done / any_success
	// 失败自动重试（session 级）
	RetryOnFail bool `gorm:"column:retry_on_fail;default:false" json:"retry_on_fail"`
	// RequireConfirm D7：写操作（post_comment 提交点）前置人工确认开关。
	// 默认 false=铁律 4 全自动不变；true 时不可逆提交前挂起等 POST /sessions/:id/confirm。
	RequireConfirm bool `gorm:"column:require_confirm;default:false" json:"require_confirm"`
	// ConfirmWaitSec 确认挂起的独立预算（秒），0=按默认 600s。
	// 此前确认等待「兼职」在 TimeoutSec 上——人还没看到待确认，任务就先被执行预算掐死；
	// 反之确认占用的时间又把执行预算吃光。两条预算各管各的（见 service.taskExecBudget）。
	ConfirmWaitSec int `gorm:"column:confirm_wait_sec;default:600" json:"confirm_wait_sec"`
	RetryDelaySec  int `gorm:"column:retry_delay_sec;default:300" json:"retry_delay_sec"`
	MaxRetryTimes  int `gorm:"column:max_retry_times;default:3" json:"max_retry_times"`
	RetryCount     int `gorm:"column:retry_count;default:0" json:"retry_count"`
	// NextRetryAt 重试持久化到期时间（D4b/G5）：原为内存 goroutine 定时器，进程重启即丢；
	// 现在 scheduleRetry 落列 + 每分钟扫描认领（条件更新置 NULL，多副本同库仅一方触发）——重启不丢。
	NextRetryAt *time.Time `gorm:"column:next_retry_at" json:"next_retry_at,omitempty"`
	// —— 触达 P0 Chunk 4：活动级预算（单次触达成本核算）——
	//
	// 「活动」在本仓没有独立实体：触达活动就是任务本体（cron/loop/retry 的所有 session
	// 都累计到同一行 browser_tasks）。因此预算落成任务行上的**持久累计额度**，跨 session、
	// 跨重跑累计——Executor 是进程级单例，放内存就等于「换个进程/重启即清零」，
	// 而预算的意义恰恰是跨进程的累计上限。
	//
	// 全部字段 0/空 = 不限。加字段不等于加限制：存量任务与未配置任务的行为逐字不变。
	TokenBudget int `gorm:"column:token_budget;default:0" json:"token_budget"` // session 级 token 预算覆盖（0=用默认常量 brainTokenBudget）
	// CampaignKey 活动键（同一次活动的多次触达任务共用同一键，用于归组与看板）
	CampaignKey string `gorm:"column:campaign_key;size:128;default:'';index" json:"campaign_key"`
	// CampaignActBudget/CampaignActUsed 活动触达条数预算与已用量（原子条件更新扣减，多副本不超发）
	CampaignActBudget int `gorm:"column:campaign_act_budget;default:0" json:"campaign_act_budget"`
	CampaignActUsed   int `gorm:"column:campaign_act_used;default:0" json:"campaign_act_used"`
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
