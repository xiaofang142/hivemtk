package model

import "time"

// BrowserOutreachDedupe 跨任务用户级触达去重行：同一平台同一目标同一动作同一文案只触达一次。
// 与 write_claims 的任务内 (task_id,text_hash) 键互补（读侧跨任务防重打扰）。
type BrowserOutreachDedupe struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform  string    `gorm:"column:platform;type:varchar(64);not null;uniqueIndex:uk_outreach_dedupe,priority:1" json:"platform"`
	TargetURL string    `gorm:"column:target_url;type:text;not null;uniqueIndex:uk_outreach_dedupe,priority:2" json:"target_url"`
	Action    string    `gorm:"column:action;type:varchar(64);not null;uniqueIndex:uk_outreach_dedupe,priority:3" json:"action"`
	CopyHash  string    `gorm:"column:copy_hash;type:varchar(64);not null;uniqueIndex:uk_outreach_dedupe,priority:4" json:"copy_hash"`
	TaskID    uint      `gorm:"column:task_id;not null" json:"task_id"`
	SessionID uint      `gorm:"column:session_id;not null" json:"session_id"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 表名。
func (BrowserOutreachDedupe) TableName() string { return "browser_outreach_dedupe" }
