package model

import "time"

// SOPExecEvent SOP 执行事件流（审计 + 幂等性 + 调试回放）
type SOPExecEvent struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ExecutionID  uint      `gorm:"index;not null" json:"execution_id"`
	SOPID        uint      `gorm:"not null" json:"sop_id"`
	NodeID       string    `gorm:"type:varchar(50);not null" json:"node_id"`
	NodeType     string    `gorm:"type:varchar(30);not null" json:"node_type"`
	EventType    string    `gorm:"type:varchar(30);not null" json:"event_type"`
	Attempt      int       `gorm:"default:0" json:"attempt"`
	Status       string    `gorm:"type:varchar(20)" json:"status"`
	Input        JSONMap   `gorm:"type:jsonb" json:"input"`
	Output       JSONMap   `gorm:"type:jsonb" json:"output"`
	SideEffects  JSONArray `gorm:"type:jsonb" json:"side_effects"`
	ErrorMessage string    `gorm:"type:text" json:"error_message"`
	LatencyMs    int       `json:"latency_ms"`
	TokensUsed   int       `json:"tokens_used"`
	TraceID      string    `gorm:"type:varchar(64);index" json:"trace_id"`
	CreatedAt    time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 指定表名
func (SOPExecEvent) TableName() string { return "sop_exec_events" }

type SOPTimer struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	ExecutionID uint       `gorm:"index;not null" json:"execution_id"`
	NodeID      string     `gorm:"type:varchar(50);not null" json:"node_id"`
	WaitEvent   string     `gorm:"type:varchar(30);not null" json:"wait_event"`
	WaitUntil   time.Time  `gorm:"not null" json:"wait_until"`
	Status      string     `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	Payload     JSONMap    `gorm:"type:jsonb" json:"payload"`
	FiredAt     *time.Time `json:"fired_at"`
	CreatedAt   time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime" json:"updated_at"`

	ExpiresAt *time.Time `gorm:"index:idx_sop_timers_expires_at" json:"expires_at,omitempty"`

	MaxWaitAt *time.Time `json:"max_wait_at,omitempty"`

	ClaimCount int `gorm:"default:0" json:"claim_count"`
}

// TableName 指定表名
func (SOPTimer) TableName() string { return "sop_timers" }

// SOPOutbox SOP Outbox 事件（解耦执行与外部副作用）。
// EC-03 note: this table/model is present but not yet part of a unified command/event contract
// across 消息/业务/触达; event versioning/schemaVersion is not modeled here.
type SOPOutbox struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	ExecutionID uint       `gorm:"index;not null" json:"execution_id"`
	EventType   string     `gorm:"type:varchar(50);not null" json:"event_type"`
	Payload     JSONMap    `gorm:"type:jsonb;not null" json:"payload"`
	Processed   bool       `gorm:"default:false;index" json:"processed"`
	ProcessedAt *time.Time `json:"processed_at"`
	RetryCount  int        `gorm:"default:0" json:"retry_count"`
	CreatedAt   time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 指定表名
func (SOPOutbox) TableName() string { return "sop_outbox" }
