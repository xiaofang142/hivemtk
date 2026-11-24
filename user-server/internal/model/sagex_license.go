package model

import "time"

type SageXActivationCode struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	Code string `gorm:"type:varchar(128);not null;uniqueIndex" json:"code"`

	Status string `gorm:"type:varchar(20);not null;default:'issued'" json:"status"`

	DeviceFingerprint string `gorm:"type:varchar(256)" json:"device_fingerprint,omitempty"`

	DeviceModel string `gorm:"type:varchar(128)" json:"device_model,omitempty"`

	IP string `gorm:"type:varchar(64)" json:"ip,omitempty"`

	Phone string `gorm:"type:varchar(32)" json:"phone,omitempty"`

	ActivatedAt *time.Time `json:"activated_at,omitempty"`

	InvalidatedAt      *time.Time `json:"invalidated_at,omitempty"`
	InvalidationReason string     `gorm:"type:varchar(256)" json:"invalidation_reason,omitempty"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (SageXActivationCode) TableName() string { return "sagex_activation_codes" }

type SageXHeartbeat struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	Code string `gorm:"type:varchar(128);not null;index" json:"code"`

	DeviceFingerprint string `gorm:"type:varchar(256);not null;index" json:"device_fingerprint"`

	DeviceModel string `gorm:"type:varchar(128)" json:"device_model,omitempty"`
	IP          string `gorm:"type:varchar(64)" json:"ip,omitempty"`
	Phone       string `gorm:"type:varchar(32)" json:"phone,omitempty"`
	AppVersion  string `gorm:"type:varchar(64)" json:"app_version,omitempty"`

	ReportedAt time.Time `gorm:"not null" json:"reported_at"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (SageXHeartbeat) TableName() string { return "sagex_heartbeats" }
