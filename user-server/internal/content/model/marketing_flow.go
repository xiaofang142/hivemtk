package model

import (
	"time"
)

// FlowStatus 流程状态
type FlowStatus string

const (
	FlowStatusDraft    FlowStatus = "draft"
	FlowStatusActive   FlowStatus = "active"
	FlowStatusPaused   FlowStatus = "paused"
	FlowStatusInactive FlowStatus = "inactive"
)

// TriggerType 触发器类型
type TriggerType string

const (
	TriggerTypeUserFollow       TriggerType = "user_follow"
	TriggerTypeMessageReceive   TriggerType = "message_receive"
	TriggerTypeLeadCreate       TriggerType = "lead_create"
	TriggerTypeLeadStatusChange TriggerType = "lead_status_change"
	TriggerTypeTimer            TriggerType = "timer"
	TriggerTypeTagChange        TriggerType = "tag_change"
	TriggerTypeOrderCreate      TriggerType = "order_create"
	TriggerTypeOrderPay         TriggerType = "order_pay"
)

// ActionType 动作类型
type ActionType string

const (
	ActionTypeSendMessage ActionType = "send_message"
	ActionTypeAddTag      ActionType = "add_tag"
	ActionTypeRemoveTag   ActionType = "remove_tag"
	ActionTypeAssignAgent ActionType = "assign_agent"
	ActionTypeCreateTask  ActionType = "create_task"
	ActionTypeWebhook     ActionType = "webhook"
	ActionTypeSendEmail   ActionType = "send_email"
	ActionTypeSendSms     ActionType = "send_sms"
	ActionTypeUpdateLead  ActionType = "update_lead"
	ActionTypeCreateOrder ActionType = "create_order"
)

// MarketingFlow 营销流程
type MarketingFlow struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID            uint        `gorm:"primaryKey;autoIncrement" json:"id"`
	Name          string      `gorm:"type:varchar(100);not null" json:"name"`
	Description   string      `gorm:"type:varchar(500)" json:"description"`
	Status        FlowStatus  `gorm:"type:varchar(20);default:'draft'" json:"status"`
	TriggerType   TriggerType `gorm:"type:varchar(20)" json:"trigger_type"`
	TriggerConfig string      `gorm:"type:text" json:"trigger_config"`
	FlowData      string      `json:"flow_data"`
	Version       int         `gorm:"default:1" json:"version"`
	CreatedBy     uint        `json:"created_by"`
	CreatedAt     time.Time   `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time   `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (MarketingFlow) TableName() string {
	return "marketing_flows"
}

// FlowExecution 流程执行记录
type FlowExecution struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID            uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	FlowID        uint       `gorm:"index;not null" json:"flow_id"`
	TriggerID     string     `gorm:"type:varchar(50);index" json:"trigger_id"`
	UserID        string     `gorm:"type:varchar(50);index" json:"user_id"`
	Status        string     `gorm:"type:varchar(20);default:'running'" json:"status"`
	CurrentNode   string     `gorm:"type:varchar(50)" json:"current_node"`
	ExecutionData string     `gorm:"type:text" json:"execution_data"`
	StartedAt     time.Time  `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at"`
	ErrorMessage  string     `gorm:"type:text" json:"error_message"`
	FlowVersion   int        `gorm:"not null;default:0" json:"flow_version"`
	GraphSnapshot string     `gorm:"type:text" json:"-"`
	InputSnapshot string     `gorm:"type:text" json:"-"`
	TriggerKey    *string    `gorm:"size:64;uniqueIndex:ux_flow_execution_trigger" json:"-"`
	DueAt         *time.Time `gorm:"index" json:"due_at,omitempty"`
	LeaseToken    string     `gorm:"size:64" json:"-"`
	LeaseUntil    *time.Time `json:"-"`
}

// TableName 指定表名
func (FlowExecution) TableName() string {
	return "flow_executions"
}

// FlowNode 流程节点
type FlowNode struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Config    map[string]any `json:"config"`
	NextNodes []string       `json:"next_nodes"`
}

// FlowDefinition 流程定义
type FlowDefinition struct {
	Nodes []FlowNode `json:"nodes"`
}
