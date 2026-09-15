package model

import (
	"gorm.io/gorm"
	"time"
)

// AgentStatus 客服状态
type AgentStatus struct {
	ID              uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID         uint           `gorm:"uniqueIndex;not null" json:"agent_id"`
	AgentName       string         `gorm:"type:varchar(100)" json:"agent_name"`
	Status          string         `gorm:"type:varchar(20);default:'offline'" json:"status"`
	MaxSessions     int            `gorm:"default:5" json:"max_sessions"`
	ActiveSessions  int            `gorm:"default:0" json:"active_sessions"`
	TodaySessions   int            `gorm:"default:0" json:"today_sessions"`
	TodayMessages   int            `gorm:"default:0" json:"today_messages"`
	AvgResponseTime int            `gorm:"default:0" json:"avg_response_time"`
	OnlineAt        *time.Time     `json:"online_at"`
	OfflineAt       *time.Time     `json:"offline_at"`
	LastActiveAt    *time.Time     `json:"last_active_at"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 指定表名
func (AgentStatus) TableName() string {
	return "agent_statuses"
}
