package model

import (
	"time"

	"gorm.io/gorm"
)

// AgentStatus 客服状态
type AgentStatus struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID              uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID         uint       `gorm:"uniqueIndex;not null" json:"agent_id"`
	AgentName       string     `gorm:"type:varchar(100)" json:"agent_name"`
	Status          string     `gorm:"type:varchar(20);default:'offline'" json:"status"`
	MaxSessions     int        `gorm:"default:5" json:"max_sessions"`
	ActiveSessions  int        `gorm:"default:0" json:"active_sessions"`
	TodaySessions   int        `gorm:"default:0" json:"today_sessions"`
	TodayMessages   int        `gorm:"default:0" json:"today_messages"`
	AvgResponseTime int        `gorm:"default:0" json:"avg_response_time"`
	OnlineAt        *time.Time `json:"online_at"`
	OfflineAt       *time.Time `json:"offline_at"`
	// AutoOnlineAt / AutoOnlineSource：J-13（2026-11-04）——「回复时自动上线」
	// 只记录可用性证据，不改 Status 语义。多坐席团队看板/派单按真 Status 统计，
	// 不会被「离线坐席点了一次回复」污染在线率；单老板租户照常能回复（Reply
	// 触发的接管放行离线坐席）。Source 取值：reply（收件箱/会话人工回复）。
	AutoOnlineAt     *time.Time     `json:"auto_online_at,omitempty"`
	AutoOnlineSource string         `gorm:"type:varchar(32)" json:"auto_online_source,omitempty"`
	LastActiveAt     *time.Time     `json:"last_active_at"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 指定表名
func (AgentStatus) TableName() string {
	return "agent_statuses"
}
