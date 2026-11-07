package model

import "time"

// FeatureFlag 功能开关（K2，对标 Unleash/GrowthBook 管理端最小完备集）
//
// SaaS 下开关也是租户级：key 的唯一性语义是 (tenant_id, key)，各租户可独立
// 灰度/关闭同名开关；审计与曝光日志同样按租户隔离。
//
// 评估语义（service 层实现）：
//  1. !Enabled            → off（kill switch 紧急全关）
//  2. rollout 0-100       → FNV-1a(key:contextKey)%100 < RolloutPercentage 灰度放量（确定性粘性分桶）
//  3. not found           → off, reason=not_found（前端降级默认值）
type FeatureFlag struct {
	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    uint   `gorm:"not null;default:1;uniqueIndex:uni_feature_flags_tenant_key,priority:1" json:"tenant_id,omitempty"`
	Key         string `gorm:"type:varchar(100);not null;uniqueIndex:uni_feature_flags_tenant_key,priority:2" json:"key"`
	Name        string `gorm:"type:varchar(200)" json:"name"`
	Description string `gorm:"type:varchar(500)" json:"description"`
	Enabled     bool   `gorm:"default:false;index" json:"enabled"`
	// RolloutPercentage 不再打 `default` 标签：GORM 在 Create 时会把带列默认值的
	// 零值字段替换成默认值（Select("*") 也绕不过），导致显式 rollout_percentage=0
	// （合法的"谁都不放量"）被静默改写成 100。默认 100 由 service.Create 负责；
	// 仓储层必须原样写入调用方给的值。
	RolloutPercentage int        `json:"rollout_percentage"`
	Payload           string     `gorm:"type:text" json:"payload"`
	Tags              string     `gorm:"type:varchar(200)" json:"tags"`
	LastEvaluatedAt   *time.Time `json:"last_evaluated_at,omitempty"`
	CreatedBy         uint       `json:"created_by"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (FeatureFlag) TableName() string { return "feature_flags" }

// FeatureFlagAuditLog Flag 变更审计（谁/何时/改了什么）
type FeatureFlagAuditLog struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  uint      `gorm:"not null;default:1;index" json:"tenant_id,omitempty"`
	FlagID    uint      `gorm:"index" json:"flag_id"`
	FlagKey   string    `gorm:"type:varchar(100);index" json:"flag_key"`
	Action    string    `gorm:"type:varchar(50);not null" json:"action"`
	ActorID   uint      `json:"actor_id"`
	Detail    string    `gorm:"type:varchar(500)" json:"detail"`
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (FeatureFlagAuditLog) TableName() string { return "feature_flag_audit_logs" }

// FeatureFlagEvalLog 评估/曝光日志（Unleash impression data：供 AB 分析与审计）
type FeatureFlagEvalLog struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  uint      `gorm:"not null;default:1;index" json:"tenant_id,omitempty"`
	FlagKey   string    `gorm:"type:varchar(100);index" json:"flag_key"`
	ContextID string    `gorm:"type:varchar(100);index" json:"context_id"`
	Enabled   bool      `json:"enabled"`
	Value     string    `gorm:"type:text" json:"value"`
	Reason    string    `gorm:"type:varchar(100)" json:"reason"`
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (FeatureFlagEvalLog) TableName() string { return "feature_flag_eval_logs" }
