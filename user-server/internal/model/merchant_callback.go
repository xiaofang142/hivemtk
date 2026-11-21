package model

import "time"

// MerchantCallbackSubscription is an explicit provider-verified mapping. Provisioning
// must not recycle a provider subscription key for a different consent or tenant.
type MerchantCallbackSubscription struct {
	ID                 uint   `gorm:"primaryKey" json:"-"`
	TenantID           uint   `gorm:"not null;index" json:"-"`
	Provider           string `gorm:"not null;size:64;uniqueIndex:global_uniq_merchant_callback_subscription,priority:1" json:"-"`
	SubscriptionKey    string `gorm:"not null;size:160;uniqueIndex:global_uniq_merchant_callback_subscription,priority:2" json:"-"`
	ConnectionID       uint   `gorm:"not null" json:"-"`
	ShopID             string `gorm:"not null;size:160" json:"-"`
	ConsentVersion     int64  `gorm:"not null" json:"-"`
	CredentialContract string `gorm:"not null;size:64" json:"-"`
	VerifierVersion    string `gorm:"not null;size:64" json:"-"`
	EventKind          string `gorm:"not null;size:64" json:"-"`
	RequiredScope      string `gorm:"not null;size:160" json:"-"`
	Enabled            bool   `gorm:"not null;default:false" json:"-"`
}

func (MerchantCallbackSubscription) TableName() string { return "merchant_callback_subscriptions" }

// MerchantCallbackEvent stores only the normalized resource reference and proof metadata.
type MerchantCallbackEvent struct {
	ID                uint      `gorm:"primaryKey" json:"-"`
	TenantID          uint      `gorm:"not null;uniqueIndex:uq_merchant_callback_event,priority:1;index:idx_merchant_callback_page,priority:1" json:"-"`
	Provider          string    `gorm:"not null;size:64;uniqueIndex:uq_merchant_callback_event,priority:2" json:"-"`
	SubscriptionID    uint      `gorm:"not null;uniqueIndex:uq_merchant_callback_event,priority:3;index:idx_merchant_callback_page,priority:2" json:"-"`
	EventKey          string    `gorm:"not null;size:160;uniqueIndex:uq_merchant_callback_event,priority:4" json:"-"`
	ShopID            string    `gorm:"not null;size:160" json:"-"`
	ConsentVersion    int64     `gorm:"not null" json:"-"`
	ConnectionVersion int64     `gorm:"not null;default:0" json:"-"`
	CredentialVersion int64     `gorm:"not null;default:0" json:"-"`
	Kind              string    `gorm:"not null;size:64;index:idx_merchant_callback_resource_head,priority:3" json:"-"`
	Scope             string    `gorm:"not null;size:160" json:"-"`
	ResourceID        string    `gorm:"not null;size:160;index:idx_merchant_callback_resource_head,priority:4" json:"-"`
	BodyDigest        string    `gorm:"not null;size:64" json:"-"`
	VerifierVersion   string    `gorm:"not null;size:64" json:"-"`
	SignedAt          time.Time `gorm:"not null;index:idx_merchant_callback_resource_head,priority:5" json:"-"`
	ValidUntil        time.Time `gorm:"not null" json:"-"`
	Disposition       string    `gorm:"not null;size:16;default:pending;index:idx_merchant_callback_resource_head,priority:6" json:"-"`
	SupersededByID    *uint     `json:"-"`
	Current           bool      `gorm:"-" json:"-"`
	CreatedAt         time.Time `json:"-"`
}

func (MerchantCallbackEvent) TableName() string { return "merchant_callback_events" }

const (
	MerchantCallbackDispositionPending    = "pending"
	MerchantCallbackDispositionSuperseded = "superseded"
	MerchantCallbackDispositionApplied    = "applied"
)

// MerchantCallbackResourceWatermark records the greatest authenticated event version for a resource.
type MerchantCallbackResourceWatermark struct {
	ID                uint      `gorm:"primaryKey" json:"-"`
	TenantID          uint      `gorm:"not null;uniqueIndex:uq_merchant_callback_resource_watermark,priority:1" json:"-"`
	Provider          string    `gorm:"not null;size:64;uniqueIndex:uq_merchant_callback_resource_watermark,priority:2" json:"-"`
	SubscriptionID    uint      `gorm:"not null;uniqueIndex:uq_merchant_callback_resource_watermark,priority:3" json:"-"`
	Kind              string    `gorm:"not null;size:64;uniqueIndex:uq_merchant_callback_resource_watermark,priority:4" json:"-"`
	ResourceID        string    `gorm:"not null;size:160;uniqueIndex:uq_merchant_callback_resource_watermark,priority:5" json:"-"`
	EventID           uint      `gorm:"not null" json:"-"`
	SignedAt          time.Time `gorm:"not null" json:"-"`
	ConsentVersion    int64     `gorm:"not null" json:"-"`
	ConnectionVersion int64     `gorm:"not null;default:0" json:"-"`
	CredentialVersion int64     `gorm:"not null;default:0" json:"-"`
	UpdatedAt         time.Time `json:"-"`
}

func (MerchantCallbackResourceWatermark) TableName() string {
	return "merchant_callback_resource_watermarks"
}

// MerchantCallbackCompensationCursor is the durable page/sweep boundary for one subscription.
type MerchantCallbackCompensationCursor struct {
	ID                uint      `gorm:"primaryKey" json:"-"`
	TenantID          uint      `gorm:"not null;uniqueIndex:uq_merchant_callback_compensation_cursor,priority:1" json:"-"`
	SubscriptionID    uint      `gorm:"not null;uniqueIndex:uq_merchant_callback_compensation_cursor,priority:2" json:"-"`
	Active            bool      `gorm:"not null;default:false" json:"-"`
	HighWaterID       uint      `gorm:"not null;default:0" json:"-"`
	AfterEventID      uint      `gorm:"not null;default:0" json:"-"`
	PageEndID         uint      `gorm:"not null;default:0" json:"-"`
	PageLimit         int       `gorm:"not null;default:0" json:"-"`
	ConnectionID      uint      `gorm:"not null;default:0" json:"-"`
	ConnectionVersion int64     `gorm:"not null;default:0" json:"-"`
	CredentialVersion int64     `gorm:"not null;default:0" json:"-"`
	ConsentVersion    int64     `gorm:"not null;default:0" json:"-"`
	UpdatedAt         time.Time `json:"-"`
}

func (MerchantCallbackCompensationCursor) TableName() string {
	return "merchant_callback_compensation_cursors"
}
