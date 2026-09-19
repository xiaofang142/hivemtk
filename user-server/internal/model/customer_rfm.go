// Package model 数据层模型 - 客户 RFM 分层
//
// 五层架构归属: L3 数据模型层
// 设计依据: 缺口修复 - RFM 联动分层
// 私域独立部署: 无 merchant_id 字段
package model

import (
	"time"
)

// CustomerRFM 客户 RFM 维度记录
//
//	R (Recency):   最近一次消费距今天数（越小越好）
//	F (Frequency): 累计消费次数（越大越好）
//	M (Monetary):  累计消费金额（越大越好，单位：分）
//
// 每项独立计算 1-5 分（5 最佳），加权后聚合为综合 RFM 分（0-100）。
// 综合分层：champion(8+)/loyal(6-7)/potential(5)/at_risk(3-4)/churn(<=2)
type CustomerRFM struct {
	ID         uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	CustomerID string `gorm:"type:varchar(64);not null;uniqueIndex" json:"customer_id"`
	UnifiedID  string `gorm:"type:varchar(128);default:'';index" json:"unified_id"`

	RecencyDays   int   `gorm:"type:int;not null;default:9999" json:"recency_days"`
	Frequency     int   `gorm:"type:int;not null;default:0" json:"frequency"`
	MonetaryTotal int64 `gorm:"type:bigint;not null;default:0" json:"monetary_total"`
	AvgOrderValue int64 `gorm:"type:bigint;not null;default:0" json:"avg_order_value"`

	RScore int `gorm:"type:int;not null;default:1" json:"r_score"`
	FScore int `gorm:"type:int;not null;default:1" json:"f_score"`
	MScore int `gorm:"type:int;not null;default:1" json:"m_score"`

	CompositeScore int    `gorm:"type:int;not null;default:0" json:"composite_score"`
	Segment        string `gorm:"type:varchar(16);not null;default:'churn'" json:"segment"`

	ChurnRiskLevel string     `gorm:"type:varchar(8);not null;default:'high'" json:"churn_risk_level"`
	ChurnScore     int        `gorm:"type:int;not null;default:100" json:"churn_score"`
	LastActiveAt   *time.Time `gorm:"index" json:"last_active_at"`

	ComputedAt time.Time `gorm:"not null;default:NOW()" json:"computed_at"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (CustomerRFM) TableName() string { return "customer_rfm" }

// RFM 分层常量
const (
	RFMSegmentChampion  = "champion"
	RFMSegmentLoyal     = "loyal"
	RFMSegmentPotential = "potential"
	RFMSegmentAtRisk    = "at_risk"
	RFMSegmentChurn     = "churn"
)

// RFMSegmentDescriptions 分层描述
var RFMSegmentDescriptions = map[string]string{
	RFMSegmentChampion:  "高价值忠诚客户 - 优先维护和提升客单价",
	RFMSegmentLoyal:     "忠诚客户 - 保持活跃并推送新品",
	RFMSegmentPotential: "潜力客户 - 加强触达和复购引导",
	RFMSegmentAtRisk:    "风险客户 - 主动关怀并查明原因",
	RFMSegmentChurn:     "流失客户 - 进入挽回队列，启动召回",
}

// RecoveryQueue 流失挽回队列
// 触达任务，由 1 个或多个渠道触达 + 多个 step 组成
type RecoveryQueue struct {
	ID         uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	CustomerID string `gorm:"type:varchar(64);not null;index" json:"customer_id"`
	UnifiedID  string `gorm:"type:varchar(128);default:'';index" json:"unified_id"`
	Account    string `gorm:"type:varchar(255);default:''" json:"account"`

	Reason   string `gorm:"type:varchar(32);not null;default:'churn'" json:"reason"`
	Strategy string `gorm:"type:varchar(32);not null;default:'sms_coupon'" json:"strategy"`
	Priority int    `gorm:"type:int;not null;default:5" json:"priority"`
	Stage    string `gorm:"type:varchar(16);not null;default:'queued';index" json:"stage"`

	Attempts      int        `gorm:"type:int;not null;default:0" json:"attempts"`
	MaxAttempts   int        `gorm:"type:int;not null;default:3" json:"max_attempts"`
	LastAttemptAt *time.Time `json:"last_attempt_at"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	LastChannel   string     `gorm:"type:varchar(32);default:''" json:"last_channel"`
	LastResult    string     `gorm:"type:varchar(255);default:''" json:"last_result"`

	RecoveredAt   *time.Time `json:"recovered_at"`
	RecoveryValue int64      `gorm:"type:bigint;not null;default:0" json:"recovery_value"`

	MetaJSON  string    `gorm:"type:text;default:'{}'" json:"meta_json"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (RecoveryQueue) TableName() string { return "recovery_queue" }

// 阶段常量
const (
	RecoveryStageQueued    = "queued"
	RecoveryStageRunning   = "running"
	RecoveryStageSucceed   = "succeed"
	RecoveryStageFailed    = "failed"
	RecoveryStageCancelled = "cancelled"
)

// RecoveryDefaultMaxAttempts 挽回队列默认最大尝试次数。
//
// 与列上的 `default:3` 同源（两条建表路径都已实测：AutoMigrate 与
// internal/migration/migrations/h_p1_migration.go 的 DDL 都把 max_attempts 落成 3，
// 且零值插入后 GORM 会把 3 回写进结构体）。常量存在的意义是给
// "显式写入" 与 "依赖列默认" 两条路同一个口径，避免默认值改了一处忘一处。
const RecoveryDefaultMaxAttempts = 3
