package model

import "time"

type CustomerSegment struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"type:varchar(100);not null" json:"name"`
	Description string    `gorm:"type:varchar(500)" json:"description"`
	RulesJSON   string    `gorm:"type:text" json:"rules_json"`
	Trigger     string    `gorm:"type:varchar(50)" json:"trigger"`
	Size        int64     `gorm:"default:0" json:"size"`
	CreatedAt   time.Time `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (CustomerSegment) TableName() string { return "customer_segments" }

type Macro struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string    `gorm:"type:varchar(100);not null;uniqueIndex" json:"name"`
	Actions   string    `gorm:"type:text;not null" json:"actions"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (Macro) TableName() string { return "macros" }

type SessionAISummary struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID string    `gorm:"type:varchar(120);uniqueIndex;not null" json:"session_id"`
	Summary   string    `gorm:"type:text" json:"summary"`
	Sentiment string    `gorm:"type:varchar(20)" json:"sentiment"`
	Model     string    `gorm:"type:varchar(60)" json:"model"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (SessionAISummary) TableName() string { return "session_ai_summaries" }

type WebhookSubscription struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	URL       string    `gorm:"type:varchar(500);not null" json:"url"`
	Events    string    `gorm:"type:varchar(500);not null" json:"events"`
	Secret    string    `gorm:"type:varchar(120);not null" json:"secret"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (WebhookSubscription) TableName() string { return "webhook_subscriptions" }

type WebhookDelivery struct {
	TenantID       uint       `gorm:"not null;default:1;index:idx_whd_tenant_status,priority:1" json:"tenant_id"`
	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	SubscriptionID uint       `gorm:"index;not null" json:"subscription_id"`
	Event          string     `gorm:"type:varchar(80);not null;index" json:"event"`
	Payload        string     `gorm:"type:text;not null" json:"payload"`
	Status         string     `gorm:"type:varchar(20);not null;default:'pending';index:idx_whd_tenant_status,priority:2" json:"status"`
	Attempts       int        `gorm:"not null;default:0" json:"attempts"`
	LastError      string     `gorm:"type:text" json:"last_error,omitempty"`
	ResponseCode   int        `gorm:"not null;default:0" json:"response_code"`
	NextRetryAt    *time.Time `gorm:"index" json:"next_retry_at,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (WebhookDelivery) TableName() string { return "webhook_deliveries" }

type SavedView struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint      `gorm:"index;not null" json:"user_id"`
	Name      string    `gorm:"type:varchar(100);not null" json:"name"`
	Route     string    `gorm:"type:varchar(100);not null" json:"route"`
	Filter    string    `gorm:"type:text" json:"filter"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (SavedView) TableName() string { return "saved_views" }

type ReportSubscription struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Email     string     `gorm:"type:varchar(200);not null;uniqueIndex" json:"email"`
	Schedule  string     `gorm:"type:varchar(20);default:'daily'" json:"schedule"`
	Enabled   bool       `gorm:"default:true" json:"enabled"`
	LastSent  *time.Time `json:"last_sent,omitempty"`
	CreatedAt time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (ReportSubscription) TableName() string { return "report_subscriptions" }

type AutomationRule struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name         string    `gorm:"type:varchar(100);not null" json:"name"`
	Event        string    `gorm:"type:varchar(40);not null;index" json:"event"`
	Conditions   string    `gorm:"type:text" json:"conditions"`
	Actions      string    `gorm:"type:text;not null" json:"actions"`
	DelayMinutes int       `gorm:"default:0" json:"delay_minutes"`
	Priority     int       `gorm:"default:0" json:"priority"`
	Enabled      bool      `gorm:"default:true;index" json:"enabled"`
	RunCount     int64     `gorm:"default:0" json:"run_count"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (AutomationRule) TableName() string { return "automation_rules" }

type RulePendingExecution struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	RuleID    uint      `gorm:"index;not null" json:"rule_id"`
	SessionID string    `gorm:"type:varchar(120);not null" json:"session_id"`
	ExecuteAt time.Time `gorm:"index;not null" json:"execute_at"`
	Status    string    `gorm:"type:varchar(20);default:'pending'" json:"status"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (RulePendingExecution) TableName() string { return "rule_pending_executions" }
