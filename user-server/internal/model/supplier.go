package model

import "time"

// Supplier 供应商主数据（US1 统一主数据补缺实体，T019 新建）。
type Supplier struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID               int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Name             string `gorm:"type:varchar(200);not null;index" json:"name"`
	CustomerID       string `gorm:"type:varchar(64);not null;index" json:"customer_id"`
	ChannelAccountID *uint  `gorm:"index" json:"channel_account_id,omitempty"`
	ContactInfo      string `gorm:"type:jsonb;default:'{}'" json:"contact_info"`
	Role             string `gorm:"type:varchar(64);default:''" json:"role,omitempty"`
	Remark           string `gorm:"type:text;default:''" json:"remark,omitempty"`

	Source    string `gorm:"type:varchar(64);not null;default:''" json:"source"`
	SourceRef string `gorm:"type:varchar(255);not null;default:''" json:"source_ref"`
	CreatedBy *uint  `json:"created_by,omitempty"`
	UpdatedBy *uint  `json:"updated_by,omitempty"`

	CreatedAt time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (Supplier) TableName() string { return "suppliers" }
