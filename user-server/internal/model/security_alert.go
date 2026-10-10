package model

import (
	"time"
)

// SecurityAlertStatus 告警状态
type SecurityAlertStatus string

const (
	SecurityAlertStatusOpen     SecurityAlertStatus = "open"
	SecurityAlertStatusResolved SecurityAlertStatus = "resolved"
	SecurityAlertStatusIgnored  SecurityAlertStatus = "ignored"
)

// IsValidSecurityAlertStatus 校验告警状态
func IsValidSecurityAlertStatus(s SecurityAlertStatus) bool {
	return s == SecurityAlertStatusOpen || s == SecurityAlertStatusResolved || s == SecurityAlertStatusIgnored
}

// SecurityAlert 安全告警
// 私域独立部署：无 merchant_id 字段
//
// 设计要点：
//   - 由 login_risk 服务在检测到 high/critical 风险时写入
//   - alert_type: abnormal_login / brute_force / device_change / location_change / frequent_failure
//   - risk_level 关联 LoginEvent.RiskLevel
//   - notified: 是否已推送通知（站内信 / 邮件 / 短信）
//   - resolved_at / resolved_by: 处理轨迹
type SecurityAlert struct {
	ID           uint                `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       uint                `gorm:"index" json:"user_id"`
	Username     string              `gorm:"type:varchar(50);index" json:"username"`
	AlertType    string              `gorm:"type:varchar(50);index;not null" json:"alert_type"`
	RiskLevel    RiskLevel           `gorm:"type:varchar(20);index;not null" json:"risk_level"`
	Title        string              `gorm:"type:varchar(255);not null" json:"title"`
	Description  string              `gorm:"type:text" json:"description"`
	IP           string              `gorm:"type:varchar(50);index" json:"ip"`
	Location     string              `gorm:"type:varchar(255)" json:"location"`
	LoginEventID uint                `gorm:"index" json:"login_event_id"`
	Notified     bool                `gorm:"default:false" json:"notified"`
	Status       SecurityAlertStatus `gorm:"type:varchar(20);default:'open';index" json:"status"`
	ResolvedAt   *time.Time          `json:"resolved_at"`
	ResolvedBy   uint                `json:"resolved_by"`
	ResolveNote  string              `gorm:"type:text" json:"resolve_note"`
	CreatedAt    time.Time           `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt    time.Time           `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (SecurityAlert) TableName() string {
	return "security_alerts"
}

// 告警类型常量
const (
	AlertTypeAbnormalLogin   = "abnormal_login"
	AlertTypeDeviceChange    = "device_change"
	AlertTypeFrequentFailure = "frequent_failure"
)
