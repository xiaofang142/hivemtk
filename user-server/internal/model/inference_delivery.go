package model

import "time"

// InferenceDelivery 保存不可变外发意向及租约；不复用推理阶段成功游标。
type InferenceDelivery struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	TenantID     uint   `gorm:"not null;uniqueIndex:uq_inference_delivery_identity,priority:1;index:idx_inference_delivery_state,priority:1"`
	Key          string `gorm:"size:64;not null;uniqueIndex:uq_inference_delivery_identity,priority:2"`
	Fingerprint  string `gorm:"size:64;not null"`
	IntentJSON   string `gorm:"type:text;not null"`
	Kind         string `gorm:"size:16;not null"`
	State        string `gorm:"size:16;not null;index:idx_inference_delivery_state,priority:2"`
	LeaseToken   string `gorm:"size:36"`
	LeaseUntil   *time.Time
	SendAttempts int    `gorm:"not null;default:0"`
	ReceiptID    string `gorm:"size:512"`
	ErrorCode    string `gorm:"size:64"`
	AcceptedAt   *time.Time
	ConfirmedAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
