package model

import "time"

// 计费三件套（SaaS 化 P2 首批，2026-10-11）：
//   BillingPlan 计费方案（可增删改查的"卖什么"）
//   TeamSubscription 团队订阅（"谁买了什么"，一团队一有效订阅）
//   BillingRecord 账单记录（"应收/实收流水"）
//
// 设计口径：
//   - 金额一律用分（price_cents/amount_cents int64），避免浮点计数事故；
//   - 配额（max_users/max_messages_monthly）挂在方案上，随订阅生效；
//     未订阅的团队不做配额限制（存量兼容），有订阅即按方案限额；
//   - teams.plan 字符串字段保留为"展示档位"，真值源是订阅记录。

// BillingPlan 计费方案。
type BillingPlan struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Code 方案编码（唯一，如 free/pro/enterprise）
	Code string `gorm:"column:code;type:varchar(32);not null;uniqueIndex" json:"code"`
	Name string `gorm:"type:varchar(64);not null" json:"name"`
	// PriceCents 月付价格（分）；YearlyPriceCents 年付价格（分）
	PriceCents       int64 `gorm:"column:price_cents;not null;default:0" json:"price_cents"`
	YearlyPriceCents int64 `gorm:"column:yearly_price_cents;not null;default:0" json:"yearly_price_cents"`
	// MaxUsers 团队成员数上限（0=不限）；MaxMessagesMonthly 月消息量上限（0=不限）
	MaxUsers           int   `gorm:"column:max_users;not null;default:0" json:"max_users"`
	MaxMessagesMonthly int64 `gorm:"column:max_messages_monthly;not null;default:0" json:"max_messages_monthly"`
	// Features 功能特性 JSON（供前端渲染与 P2 功能开关消费）
	Features string `gorm:"type:text" json:"features"`
	// Status: active（可售）/ retired（下架，已有订阅不受影响）
	Status    string    `gorm:"column:status;type:varchar(16);not null;default:active;index" json:"status"`
	Sort      int       `json:"sort"`
	Remark    string    `gorm:"type:varchar(255)" json:"remark"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (BillingPlan) TableName() string { return "billing_plans" }

const (
	BillingPlanStatusActive  = "active"
	BillingPlanStatusRetired = "retired"
)

// TeamSubscription 团队订阅（一团队一行的"当前有效订阅"语义：续期=顺延 EndsAt）。
type TeamSubscription struct {
	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	TeamID   uint   `gorm:"column:team_id;not null;uniqueIndex" json:"team_id"`
	PlanID   uint   `gorm:"column:plan_id;not null;index" json:"plan_id"`
	PlanCode string `gorm:"column:plan_code;type:varchar(32)" json:"plan_code"`
	// Status: active / expired（到期由查询侧按时间判定，这里是人工停用位）
	Status    string    `gorm:"column:status;type:varchar(16);not null;default:active" json:"status"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	AutoRenew bool      `gorm:"not null;default:false" json:"auto_renew"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (TeamSubscription) TableName() string { return "team_subscriptions" }

const (
	TeamSubStatusActive  = "active"
	TeamSubStatusExpired = "expired"
)

// BillingRecord 账单流水（人工记账口径：创建=应收，标记支付=实收）。
type BillingRecord struct {
	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	TeamID   uint   `gorm:"column:team_id;not null;index" json:"team_id"`
	PlanCode string `gorm:"column:plan_code;type:varchar(32);not null" json:"plan_code"`
	// AmountCents 应收金额（分）；Months 覆盖月数
	AmountCents int64 `gorm:"column:amount_cents;not null" json:"amount_cents"`
	Months      int   `gorm:"column:months;not null;default:1" json:"months"`
	// Status: pending（应收）/ paid（已收）/ void（作废）
	Status      string     `gorm:"column:status;type:varchar(16);not null;default:pending;index" json:"status"`
	PaidAt      *time.Time `json:"paid_at"`
	PeriodStart *time.Time `json:"period_start"`
	PeriodEnd   *time.Time `json:"period_end"`
	Remark      string     `gorm:"type:varchar(255)" json:"remark"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (BillingRecord) TableName() string { return "billing_records" }

const (
	BillingStatusPending = "pending"
	BillingStatusPaid    = "paid"
	BillingStatusVoid    = "void"
)
