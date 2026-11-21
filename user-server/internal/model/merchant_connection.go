package model

import "time"

// MerchantConnection stores one authorization slot per tenant/provider/expected shop.
// ShopID denotes a verified binding only after a credential has been committed.
type MerchantConnection struct {
	ID       uint   `gorm:"primaryKey" json:"-"`
	TenantID uint   `gorm:"not null;uniqueIndex:uq_merchant_connection_target,priority:1" json:"-"`
	Provider string `gorm:"not null;size:64;uniqueIndex:uq_merchant_connection_target,priority:2" json:"-"`
	ShopID   string `gorm:"not null;size:160;uniqueIndex:uq_merchant_connection_target,priority:3" json:"-"`

	Version            int64      `gorm:"not null" json:"-"`
	AttemptActor       uint       `gorm:"not null" json:"-"`
	AttemptContract    string     `gorm:"not null;size:64" json:"-"`
	AttemptExpiresAt   *time.Time `json:"-"`
	StateDigest        string     `gorm:"not null;size:64" json:"-"`
	VerifierCiphertext string     `gorm:"not null;type:text" json:"-"`
	AttemptFailed      bool       `gorm:"not null" json:"-"`

	CredentialVersion    int64      `gorm:"not null" json:"-"`
	ConsentVersion       int64      `gorm:"not null;default:0" json:"-"`
	CredentialContract   string     `gorm:"not null;size:64" json:"-"`
	CredentialCiphertext string     `gorm:"not null;type:text" json:"-"`
	RefreshUntil         *time.Time `json:"-"`
	RefreshFailed        bool       `gorm:"not null;default:false" json:"-"`
	Revoked              bool       `gorm:"not null;default:false" json:"-"`
	Scopes               []string   `gorm:"serializer:json;type:text" json:"-"`
	ExpiresAt            *time.Time `json:"-"`
	CreatedAt            time.Time  `json:"-"`
	UpdatedAt            time.Time  `json:"-"`
}

func (MerchantConnection) TableName() string { return "merchant_connections" }
