package model

import (
	"time"
)

// IntegrationAccount 第三方对接账号
type IntegrationAccount struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform     string     `gorm:"type:varchar(50);index;not null" json:"platform"`
	AccountName  string     `gorm:"type:varchar(100)" json:"account_name"`
	APIKey       string     `gorm:"type:varchar(200)" json:"api_key"`
	APISecret    string     `gorm:"type:varchar(200)" json:"api_secret"`
	RefreshToken string     `gorm:"type:text" json:"refresh_token"`
	AccessToken  string     `gorm:"type:text" json:"access_token"`
	TokenExpires *time.Time `json:"token_expires"`
	WebhookURL   string     `gorm:"type:varchar(500)" json:"webhook_url"`
	Config       string     `gorm:"type:text" json:"config"`
	Status       int        `gorm:"default:1" json:"status"`
	LastSyncAt   *time.Time `json:"last_sync_at"`
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (IntegrationAccount) TableName() string {
	return "integration_accounts"
}

// SyncLog 同步日志
type SyncLog struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform     string     `gorm:"type:varchar(50);index" json:"platform"`
	SyncType     string     `gorm:"type:varchar(50)" json:"sync_type"`
	Status       int        `gorm:"default:0" json:"status"`
	RecordCount  int        `gorm:"default:0" json:"record_count"`
	ErrorMessage string     `gorm:"type:text" json:"error_message"`
	StartTime    time.Time  `json:"start_time"`
	EndTime      *time.Time `json:"end_time"`
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (SyncLog) TableName() string {
	return "sync_logs"
}

// ExternalCustomer 外部客户（CRM 对接）
type ExternalCustomer struct {
	ID            uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform      string     `gorm:"type:varchar(50);index:idx_extcust_platform_external,priority:1" json:"platform"`
	ExternalID    string     `gorm:"type:varchar(100);index:idx_extcust_platform_external,priority:2" json:"external_id"`
	Name          string     `gorm:"type:varchar(100)" json:"name"`
	Phone         string     `gorm:"type:varchar(50);index" json:"phone"`
	Email         string     `gorm:"type:varchar(100)" json:"email"`
	Company       string     `gorm:"type:varchar(200)" json:"company"`
	Position      string     `gorm:"type:varchar(100)" json:"position"`
	Industry      string     `gorm:"type:varchar(100)" json:"industry"`
	Level         string     `gorm:"type:varchar(50)" json:"level"`
	Source        string     `gorm:"type:varchar(100)" json:"source"`
	OwnerID       string     `gorm:"type:varchar(100)" json:"owner_id"`
	OwnerName     string     `gorm:"type:varchar(100)" json:"owner_name"`
	Status        string     `gorm:"type:varchar(50)" json:"status"`
	Tags          string     `gorm:"type:text" json:"tags"`
	LastContactAt *time.Time `json:"last_contact_at"`
	CreatedAt     time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (ExternalCustomer) TableName() string {
	return "external_customers"
}

// ExternalOrder 外部订单（电商对接）
type ExternalOrder struct {
	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform       string     `gorm:"type:varchar(50);index" json:"platform"`
	OrderID        string     `gorm:"type:varchar(100);unique;not null" json:"order_id"`
	OrderNo        string     `gorm:"type:varchar(100);index" json:"order_no"`
	UserID         string     `gorm:"type:varchar(100);index" json:"user_id"`
	UserName       string     `gorm:"type:varchar(100)" json:"user_name"`
	UserPhone      string     `gorm:"type:varchar(50)" json:"user_phone"`
	TotalAmount    int64      `gorm:"type:bigint;default:0" json:"total_amount"`
	PayAmount      int64      `gorm:"type:bigint;default:0" json:"pay_amount"`
	DiscountAmount int64      `gorm:"type:bigint;default:0" json:"discount_amount"`
	Status         string     `gorm:"type:varchar(50)" json:"status"`
	OrderTime      *time.Time `json:"order_time"`
	PayTime        *time.Time `json:"pay_time"`
	ShipTime       *time.Time `json:"ship_time"`
	CompleteTime   *time.Time `json:"complete_time"`
	Items          string     `gorm:"type:text" json:"items"`
	ShippingAddr   string     `gorm:"type:text" json:"shipping_addr"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (ExternalOrder) TableName() string {
	return "external_orders"
}

// ExternalProduct 外部商品（电商对接）
type ExternalProduct struct {
	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform      string    `gorm:"type:varchar(50);index" json:"platform"`
	ProductID     string    `gorm:"type:varchar(100);index" json:"product_id"`
	Name          string    `gorm:"type:varchar(200)" json:"name"`
	CategoryID    string    `gorm:"type:varchar(100)" json:"category_id"`
	CategoryName  string    `gorm:"type:varchar(100)" json:"category_name"`
	Price         int64     `gorm:"type:bigint;default:0" json:"price"`
	OriginalPrice int64     `gorm:"type:bigint;default:0" json:"original_price"`
	Stock         int       `gorm:"default:0" json:"stock"`
	Sales         int       `gorm:"default:0" json:"sales"`
	Images        string    `gorm:"type:text" json:"images"`
	Status        int       `gorm:"default:1" json:"status"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (ExternalProduct) TableName() string {
	return "external_products"
}

// WebhookEvent Webhook 事件
type WebhookEvent struct {
	ID          uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform    string     `gorm:"type:varchar(50);index" json:"platform"`
	EventID     string     `gorm:"type:varchar(100);unique" json:"event_id"`
	EventType   string     `gorm:"type:varchar(50)" json:"event_type"`
	AccountID   string     `gorm:"type:varchar(100);default:''" json:"account_id"`
	RawData     string     `gorm:"type:text" json:"raw_data"`
	Processed   bool       `gorm:"default:false;index:idx_webhook_events_processed_created,priority:1" json:"processed"`
	ProcessedAt *time.Time `json:"processed_at"`
	CreatedAt   time.Time  `gorm:"autoCreateTime;index:idx_webhook_events_processed_created,priority:2" json:"created_at"`
}

// TableName 指定表名
func (WebhookEvent) TableName() string {
	return "webhook_events"
}
