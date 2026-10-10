package model

import (
	"time"
)

// AlertRuleSeverity 告警分级
type AlertRuleSeverity string

const (
	AlertSeverityCritical AlertRuleSeverity = "critical"
	AlertSeverityWarning  AlertRuleSeverity = "warning"
	AlertSeverityInfo     AlertRuleSeverity = "info"
)

// AlertRuleStatus 告警规则状态
type AlertRuleStatus string

// AlertChannel 告警通知渠道
type AlertChannel string

const (
	AlertChannelEmail    AlertChannel = "email"
	AlertChannelDingTalk AlertChannel = "dingtalk"
	AlertChannelWebhook  AlertChannel = "webhook"
)

// IsValidAlertChannel 校验通知渠道是否合法
func IsValidAlertChannel(c string) bool {
	switch AlertChannel(c) {
	case AlertChannelEmail, AlertChannelDingTalk, AlertChannelWebhook:
		return true
	}
	return false
}

// AlertRule 告警规则
//
// 设计要点（plan v3.1 §T8）：
//   - source: 指标来源，如 bridge.message_failed_count / system.error_rate / llm.p95_latency
//   - operator: 比较运算符 gt/ge/lt/le/eq/ne
//   - threshold: 数值阈值
//   - window_seconds: 触发时间窗口（秒），避免瞬时抖动
//   - cooldown_seconds: 静默期，防止告警风暴
//   - channels: 通知渠道 JSON 数组 ["email","dingtalk"]
//   - targets: 通知目标 JSON（邮箱列表 / 钉钉 webhook / webhook url）
//   - enabled: 启用开关
type AlertRule struct {
	ID              uint              `json:"id" gorm:"primaryKey"`
	Name            string            `json:"name" gorm:"size:100;not null;uniqueIndex"`
	Description     string            `json:"description" gorm:"size:500"`
	Source          string            `json:"source" gorm:"size:100;not null;index"`
	Operator        string            `json:"operator" gorm:"size:10;not null;default:'gt'"`
	Threshold       float64           `json:"threshold" gorm:"not null;default:0"`
	WindowSeconds   int               `json:"window_seconds" gorm:"not null;default:60"`
	CooldownSeconds int               `json:"cooldown_seconds" gorm:"not null;default:300"`
	Severity        AlertRuleSeverity `json:"severity" gorm:"size:20;default:'warning'"`
	Channels        string            `json:"channels" gorm:"type:jsonb;default:'[]'"`
	Targets         string            `json:"targets" gorm:"type:jsonb;default:'{}'"`
	Enabled         bool              `json:"enabled" gorm:"default:true;not null"`
	LastTriggeredAt *time.Time        `json:"last_triggered_at"`
	CreatedBy       uint              `json:"created_by" gorm:"default:0"`
	CreatedAt       time.Time         `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time         `json:"updated_at" gorm:"autoUpdateTime"`

	Managers []AlertRuleManager `json:"managers,omitempty" gorm:"-"`
}

// TableName 返回表名
func (AlertRule) TableName() string {
	return "alert_rules"
}

// AlertRuleManager 规则负责人（用于前端展示，未落库单独表）
type AlertRuleManager struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
}

// AlertHistory 告警历史
//
// 记录每次规则触发的快照，用于审计、恢复通知、查重。
type AlertHistory struct {
	ID           uint              `json:"id" gorm:"primaryKey"`
	RuleID       uint              `json:"rule_id" gorm:"not null;index"`
	RuleName     string            `json:"rule_name" gorm:"size:100"`
	Source       string            `json:"source" gorm:"size:100;index"`
	Value        float64           `json:"value"`
	Threshold    float64           `json:"threshold"`
	Severity     AlertRuleSeverity `json:"severity" gorm:"size:20;index"`
	Message      string            `json:"message" gorm:"type:text"`
	Status       string            `json:"status" gorm:"size:20;default:'firing';index"`
	Channels     string            `json:"channels" gorm:"type:jsonb;default:'[]'"`
	NotifyResult string            `json:"notify_result" gorm:"type:text"`
	TriggeredAt  time.Time         `json:"triggered_at" gorm:"not null;index"`
	ResolvedAt   *time.Time        `json:"resolved_at"`
	CreatedAt    time.Time         `json:"created_at" gorm:"autoCreateTime"`

	// 软删除（与全局 SoftDelete 迁移对齐：用 deleted_at）
	DeletedAt *time.Time `json:"deleted_at" gorm:"index"`
}

// TableName 返回表名
func (AlertHistory) TableName() string {
	return "alert_histories"
}

// AlertHistoryStatus 告警历史状态
const (
	AlertHistoryFiring   = "firing"
	AlertHistoryResolved = "resolved"
)
