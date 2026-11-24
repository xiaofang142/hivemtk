package model

import "time"

// BusinessAccountAuthorization 业务账号授权（PR-5/D-03）。
type BusinessAccountAuthorization struct {
	TenantID  uint       `gorm:"not null;default:1;index" json:"tenant_id"`
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint       `gorm:"not null;index" json:"user_id"`
	Channel   string     `gorm:"type:varchar(32);not null;default:'';index" json:"channel"`
	AccountID string     `gorm:"type:varchar(128);not null;default:'';index" json:"account_id"`
	Scope     string     `gorm:"type:varchar(16);not null;default:'read'" json:"scope"` // read/write/manage
	GrantedBy *uint      `json:"granted_by,omitempty"`
	CreatedAt time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (BusinessAccountAuthorization) TableName() string { return "business_account_authorizations" }
