package model

import (
	"time"
)

// ConfigParam 动态阈值参数表（替代分散的 const 硬编码）。
//
// 设计原则：
//  1. 单一表通存所有类型（int/float/bool/string/duration），value 存字符串按需解析
//  2. group + key 联合唯一，key 采用点号命名（bridge.polling_max_timeout）
//  3. default_value 保证每次启动/重置都有合法值
//  4. min/max/value_type 约束 + UI 渲染提示
//  5. read_only=true 标记由系统自动推导/锁死（暂不开放给用户改）
type ConfigParam struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Group        string    `gorm:"column:param_group;type:varchar(50);not null;index;uniqueIndex:idx_group_key,priority:1" json:"group"`
	Key          string    `gorm:"type:varchar(100);not null;uniqueIndex:idx_group_key,priority:2" json:"key"`
	Name         string    `gorm:"type:varchar(200);not null" json:"name"`
	Description  string    `gorm:"type:varchar(500)" json:"description"`
	ValueType    string    `gorm:"type:varchar(20);not null;default:int" json:"value_type"`
	Value        string    `gorm:"column:param_value;type:text;not null" json:"value"`
	DefaultValue string    `gorm:"column:default_value;type:text;not null" json:"default_value"`
	Min          *string   `gorm:"type:varchar(50)" json:"min,omitempty"`
	Max          *string   `gorm:"type:varchar(50)" json:"max,omitempty"`
	Step         *string   `gorm:"type:varchar(20)" json:"step,omitempty"`
	ReadOnly     bool      `gorm:"default:false" json:"read_only"`
	Restart      bool      `gorm:"default:false" json:"restart"`
	Category     string    `gorm:"type:varchar(100)" json:"category"`
	UpdatedBy    uint      `json:"updated_by"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ConfigParam) TableName() string { return "config_params" }

// ConfigParamAuditLog 变更审计
type ConfigParamAuditLog struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ParamKey  string    `gorm:"type:varchar(100);index;not null" json:"param_key"`
	OldValue  string    `gorm:"type:text" json:"old_value"`
	NewValue  string    `gorm:"type:text" json:"new_value"`
	Action    string    `gorm:"type:varchar(20);not null" json:"action"`
	ActorID   uint      `json:"actor_id"`
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (ConfigParamAuditLog) TableName() string { return "config_param_audit_logs" }
