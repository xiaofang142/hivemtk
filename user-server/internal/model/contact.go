package model

import "time"

// Contact 客户联系人（US1 统一主数据补缺实体，T019 新建）。
// 来源字段不在此重复建模：channel_account_id / source / source_ref 即来源口径。
type Contact struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID               int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Name             string `gorm:"type:varchar(100);not null;index" json:"name"`
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

func (Contact) TableName() string { return "contacts" }
