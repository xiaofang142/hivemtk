package model

import "time"

// WorkflowVersion 可视化工作流版本
//
// 表: workflow_versions（由 WorkflowVisualOrchestratorMigration 创建）
// 一条记录 = 一个工作流的某个版本，definition (JSONB) 存可视化编排图。
// status: draft / published / archived
type WorkflowVersion struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkflowID  string    `gorm:"type:varchar(64);not null;index:idx_workflow_versions_wf_id" json:"workflow_id"`
	Version     int       `gorm:"not null" json:"version"`
	Name        string    `gorm:"type:varchar(200);not null" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	Definition  JSONMap   `gorm:"type:jsonb;not null" json:"definition"`
	Status      string    `gorm:"type:varchar(16);not null;default:'draft';index:idx_workflow_versions_status" json:"status"`
	Changelog   string    `gorm:"type:text" json:"changelog"`
	CreatedBy   string    `gorm:"type:varchar(64)" json:"created_by"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (WorkflowVersion) TableName() string { return "workflow_versions" }

// WorkflowExecution 工作流执行实例
//
// 表: workflow_executions
// status: running / completed / failed / terminated
// context (JSONB): 运行时上下文（变量、循环计数等）
type WorkflowExecution struct {
	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkflowID     string     `gorm:"type:varchar(64);not null;index:idx_workflow_executions_wf_id" json:"workflow_id"`
	Version        int        `gorm:"not null" json:"version"`
	TriggerPayload JSONMap    `gorm:"type:jsonb" json:"trigger_payload"`
	Status         string     `gorm:"type:varchar(16);not null;index:idx_workflow_executions_status" json:"status"`
	CurrentNodeID  string     `gorm:"type:varchar(64)" json:"current_node_id"`
	Context        JSONMap    `gorm:"type:jsonb" json:"context"`
	StartedAt      time.Time  `gorm:"autoCreateTime;index:idx_workflow_executions_started" json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	Error          string     `gorm:"type:text" json:"error"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (WorkflowExecution) TableName() string { return "workflow_executions" }

// WorkflowNodeExecution 节点执行明细
//
// 表: workflow_node_executions
// 用于实时高亮与耗时分析。
// node_type: trigger / action / condition / subflow
// status: pending / running / completed / failed / skipped
type WorkflowNodeExecution struct {
	ID          uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	ExecutionID uint       `gorm:"not null;index:idx_wf_node_exec_exec_id" json:"execution_id"`
	NodeID      string     `gorm:"type:varchar(64);not null;index:idx_wf_node_exec_node_id" json:"node_id"`
	NodeType    string     `gorm:"type:varchar(32);not null" json:"node_type"`
	NodeName    string     `gorm:"type:varchar(200)" json:"node_name"`
	InputData   JSONMap    `gorm:"type:jsonb" json:"input_data"`
	OutputData  JSONMap    `gorm:"type:jsonb" json:"output_data"`
	Status      string     `gorm:"type:varchar(16);not null" json:"status"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	DurationMs  int        `json:"duration_ms"`
	Error       string     `gorm:"type:text" json:"error"`
	CreatedAt   time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (WorkflowNodeExecution) TableName() string { return "workflow_node_executions" }

// 工作流状态常量
const (
	WorkflowStatusDraft     = "draft"
	WorkflowStatusPublished = "published"
	WorkflowStatusArchived  = "archived"

	WorkflowExecRunning    = "running"
	WorkflowExecCompleted  = "completed"
	WorkflowExecFailed     = "failed"
	WorkflowExecWaiting    = "waiting"
	WorkflowExecTerminated = "terminated"

	WorkflowNodeRunning   = "running"
	WorkflowNodeCompleted = "completed"
	WorkflowNodeFailed    = "failed"
	WorkflowNodeSkipped   = "skipped"
)

// 节点类型常量
const (
	WorkflowNodeTypeTrigger   = "trigger"
	WorkflowNodeTypeAction    = "action"
	WorkflowNodeTypeCondition = "condition"
	WorkflowNodeTypeSubflow   = "subflow"
)
