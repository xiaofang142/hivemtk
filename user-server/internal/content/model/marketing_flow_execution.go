package model

import "time"

// FlowNodeAttempt 保存每个节点的不可重复执行边界，动作结果未知时只允许核验。
type FlowNodeAttempt struct {
	ID                  uint       `gorm:"primaryKey" json:"id"`
	TenantID            uint       `gorm:"not null;uniqueIndex:ux_flow_node_attempt,priority:1" json:"tenant_id"`
	ExecutionID         uint       `gorm:"not null;uniqueIndex:ux_flow_node_attempt,priority:2" json:"execution_id"`
	NodeID              string     `gorm:"size:100;not null;uniqueIndex:ux_flow_node_attempt,priority:3" json:"node_id"`
	State               string     `gorm:"size:32;not null" json:"state"`
	Output              string     `gorm:"type:text" json:"-"`
	DueAt               *time.Time `json:"due_at,omitempty"`
	DispatchCommittedAt *time.Time `json:"dispatch_committed_at,omitempty"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

func (FlowNodeAttempt) TableName() string { return "flow_node_attempts" }
