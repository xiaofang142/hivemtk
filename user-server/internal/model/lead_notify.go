package model

import "time"

const (
	LeadNotifyChannelInApp         = "in_app"
	LeadNotifyChannelTelegram      = "telegram"
	LeadNotifyChannelFeishu        = "feishu"
	LeadNotifyStatusPending        = "pending"
	LeadNotifyStatusSending        = "sending"
	LeadNotifyStatusRetry          = "retry"
	LeadNotifyStatusDelivered      = "delivered"
	LeadNotifyStatusExhausted      = "exhausted"
	LeadNotifyStatusReceiptUnknown = "receipt_unknown"
	LeadNotifyStatusCancelled      = "cancelled"
	LeadNotifyStatusNoRoute        = "no_route"
	LeadNotifyStatusNoRecipient    = "no_recipient"
	LeadNotifyDefaultMaxAttempts   = 6
)

// LeadNotifyOutbox 是通知投递意图的耐久事实源。同一租户内按 idempotency_key 唯一；
// claim_token + lease_until 为并发认领与回写栅栏，stale claim 转 receipt_unknown，避免自动重发。
type LeadNotifyOutbox struct {
	TenantID       uint       `gorm:"not null;default:1;index;uniqueIndex:uq_lead_notify_outbox_tenant_key,priority:1" json:"tenant_id"`
	ID             uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	IdempotencyKey string     `gorm:"type:varchar(96);not null;uniqueIndex:uq_lead_notify_outbox_tenant_key,priority:2" json:"idempotency_key"`
	ClueID         string     `gorm:"type:varchar(64);not null;default:'';index" json:"clue_id"`
	EventID        string     `gorm:"type:varchar(64);not null;default:'';index" json:"event_id"`
	CustomerID     string     `gorm:"type:varchar(64);not null;default:''" json:"customer_id"`
	Channel        string     `gorm:"type:varchar(16);not null;default:'in_app'" json:"channel"`
	Recipient      string     `gorm:"type:varchar(255);not null;default:''" json:"recipient"`
	RouteID        string     `gorm:"type:varchar(64);not null;default:''" json:"route_id"`
	RouteScope     string     `gorm:"type:varchar(16);not null;default:''" json:"route_scope"`
	AccountID      uint       `gorm:"not null;default:0" json:"account_id"`
	Title          string     `gorm:"type:varchar(255);not null;default:''" json:"title"`
	Content        string     `gorm:"type:text;not null;default:''" json:"content"`
	Link           string     `gorm:"type:varchar(512);not null;default:''" json:"link"`
	MaskPolicy     string     `gorm:"type:varchar(255);not null;default:''" json:"mask_policy"`
	Status         string     `gorm:"type:varchar(24);not null;default:'pending';index" json:"status"`
	Attempts       int        `gorm:"not null;default:0" json:"attempts"`
	MaxAttempts    int        `gorm:"not null;default:6" json:"max_attempts"`
	LastError      string     `gorm:"type:text;not null;default:''" json:"last_error"`
	ClaimToken     string     `gorm:"type:varchar(64);not null;default:''" json:"claim_token"`
	LeaseUntil     *time.Time `json:"lease_until,omitempty"`
	NextAttemptAt  time.Time  `gorm:"index;not null" json:"next_attempt_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (LeadNotifyOutbox) TableName() string { return "lead_notify_outbox" }

// LeadNotifyDelivery 是独立于通知路由的送达/审计历史。路由删除不会丢历史；
// delivery_key 保证同一 outbox attempt 或同一审计事件重复写入时保持幂等。
type LeadNotifyDelivery struct {
	TenantID    uint      `gorm:"not null;default:1;index;uniqueIndex:uq_lead_notify_delivery_key,priority:1" json:"tenant_id"`
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	DeliveryKey string    `gorm:"type:varchar(128);not null;uniqueIndex:uq_lead_notify_delivery_key,priority:2" json:"delivery_key"`
	OutboxID    uint64    `gorm:"not null;default:0;index" json:"outbox_id"`
	ClueID      string    `gorm:"type:varchar(64);not null;default:'';index" json:"clue_id"`
	EventID     string    `gorm:"type:varchar(64);not null;default:'';index" json:"event_id"`
	CustomerID  string    `gorm:"type:varchar(64);not null;default:''" json:"customer_id"`
	Recipient   string    `gorm:"type:varchar(255);not null;default:''" json:"recipient"`
	Channel     string    `gorm:"type:varchar(16);not null;default:'';index" json:"channel"`
	RouteID     string    `gorm:"type:varchar(64);not null;default:''" json:"route_id"`
	RouteScope  string    `gorm:"type:varchar(16);not null;default:''" json:"route_scope"`
	Success     bool      `gorm:"not null;default:false" json:"success"`
	Status      string    `gorm:"type:varchar(24);not null;default:'';index" json:"status"`
	Error       string    `gorm:"type:text;not null;default:''" json:"error"`
	MaskPolicy  string    `gorm:"type:varchar(255);not null;default:''" json:"mask_policy"`
	Attempt     int       `gorm:"not null;default:0" json:"attempt"`
	CreatedAt   time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (LeadNotifyDelivery) TableName() string { return "lead_notify_deliveries" }

type LeadNotifyDeliveryQuery struct {
	ClueID  string
	EventID string
	RouteID string
	Channel string
	Limit   int
}

type LeadNotifyDeliverySummary struct {
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
}
