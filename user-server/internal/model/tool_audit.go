// tool_audit.go 工具调用审计持久化模型（T-P1-08 / W-6）
//
// 模型放这里而不是留在 tooluse 包内的原因只有一条：本仓的建表入口是
// `internal/pkg/db` 的 AutoMigrate()，它按 model 包的清单登记（见 migrate.go
// allModels()）。模型留在 tooluse 里 = 有生产写入路径却没有建表登记，
// 正是 migrate_test.go TestAllModels_CoversModelsWithWritePaths 要拦的那类缺陷
// （2026-09-16 审计 DB-07 的 GeoCrawlerVisit 同源）。
// tooluse 侧保留同名别名，写入口径不变。
package model

import "time"

// ToolCallAudit 单次工具调用的审计行（表 tool_call_audits）
//
// duration_ms 与 success 是 CS-58 要求的耗时口径落库；计费侧无需另建表：
// MemoryCostTracker 统计的四个量（次数/成功/失败/总耗时）都是本表按 tool_name
// 聚合的子集，见 repository.ToolAuditRepository.CostAggregates。
type ToolCallAudit struct {
	ID            uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TraceID       string    `gorm:"index;size:64" json:"trace_id"`
	ToolName      string    `gorm:"index;size:128" json:"tool_name"`
	CallerID      string    `gorm:"size:64" json:"caller_id"`
	AgentID       string    `gorm:"size:64" json:"agent_id"`
	CustomerID    string    `gorm:"size:64" json:"customer_id"`
	SessionID     string    `gorm:"index;size:64" json:"session_id"`
	Success       bool      `gorm:"index" json:"success"`
	Error         string    `gorm:"type:text" json:"error"`
	DurationMs    int64     `json:"duration_ms"`
	RetryCount    int       `json:"retry_count"`
	AuditTrace    string    `gorm:"size:128" json:"audit_trace"`
	ArgsSummary   string    `gorm:"type:text" json:"args_summary"`
	ResultSummary string    `gorm:"type:text" json:"result_summary"`
	ExecutedAt    time.Time `gorm:"index" json:"executed_at"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (ToolCallAudit) TableName() string { return "tool_call_audits" }
