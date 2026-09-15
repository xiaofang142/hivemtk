package model

import (
	"gorm.io/gorm"
	"time"
)

// MemoryLayer 记忆层级
type MemoryLayer string

const (
	MemoryLayerShortTerm MemoryLayer = "L1_short_term"
	MemoryLayerLongTerm  MemoryLayer = "L2_long_term"
	MemoryLayerSOPState  MemoryLayer = "L3_sop_state"
	MemoryLayerBusiness  MemoryLayer = "L4_business"
)

// MemoryItem 统一记忆条目（L1/L2/L4 通用）
type MemoryItem struct {
	ID         uint        `gorm:"primaryKey;autoIncrement" json:"id"`
	Layer      MemoryLayer `gorm:"type:varchar(32);not null;index" json:"layer"`
	SessionID  string      `gorm:"type:varchar(64);index" json:"session_id"`
	CustomerID string      `gorm:"type:varchar(64);index" json:"customer_id"`
	ItemType   string      `gorm:"type:varchar(32);index" json:"item_type"`
	Content    string      `gorm:"type:text;not null" json:"content"`
	Role       string      `gorm:"type:varchar(16)" json:"role"`
	Importance int         `gorm:"default:5" json:"importance"`
	Metadata   JSONMap     `gorm:"type:text" json:"metadata"`
	ExpiresAt  *time.Time  `gorm:"index" json:"expires_at"`

	ValidFrom *time.Time     `gorm:"index" json:"valid_from,omitempty"`
	InvalidAt *time.Time     `gorm:"index" json:"invalid_at,omitempty"`
	CreatedAt time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MemoryItem) TableName() string { return "memory_items" }

// SOPStateMemory L3 SOP 状态记忆（独立于 sop_executions，按 session 维度）
type SOPStateMemory struct {
	ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID   string         `gorm:"type:varchar(64);not null;index" json:"session_id"`
	CustomerID  string         `gorm:"type:varchar(64);not null;index" json:"customer_id"`
	SOPID       uint           `gorm:"not null;index" json:"sop_id"`
	ExecutionID uint           `gorm:"index" json:"execution_id"`
	CurrentNode string         `gorm:"type:varchar(64)" json:"current_node"`
	StepIndex   int            `gorm:"default:0" json:"step_index"`
	Status      string         `gorm:"type:varchar(20);default:'running';index" json:"status"`
	StateData   JSONMap        `gorm:"type:text" json:"state_data"`
	LastStepAt  time.Time      `gorm:"index" json:"last_step_at"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (SOPStateMemory) TableName() string { return "sop_state_memories" }

// BusinessMemory L4 业务记忆（订单/咨询/投诉/意向快照）
type BusinessMemory struct {
	ID         uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	CustomerID string         `gorm:"type:varchar(64);not null;index" json:"customer_id"`
	MemoryType string         `gorm:"type:varchar(32);not null;index" json:"memory_type"`
	Content    string         `gorm:"type:text;not null" json:"content"`
	RelatedID  string         `gorm:"type:varchar(64);index" json:"related_id"`
	Importance int            `gorm:"default:5;index" json:"importance"`
	Metadata   JSONMap        `gorm:"type:text" json:"metadata"`
	CreatedAt  time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (BusinessMemory) TableName() string { return "business_memories" }
