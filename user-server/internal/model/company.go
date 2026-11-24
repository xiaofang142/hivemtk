package model

import "time"

// Company 公司主数据（CRM P0）。
// 053 创建基础表，079 演进补齐 tenant/owner/source/audit 字段。
// D-02: customer.company_id 作为兼容缓存，主关系以 customer_company_relations 为准。
type Company struct {
	TenantID    uint   `gorm:"not null;default:1;index" json:"tenant_id"`
	ID          int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string `gorm:"type:varchar(200);not null;index" json:"name"`
	Domain      string `gorm:"type:varchar(200)" json:"domain"`
	Industry    string `gorm:"type:varchar(50)" json:"industry"`
	Size        string `gorm:"type:varchar(20)" json:"size"` // startup/smb/enterprise
	Metadata    string `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	OwnerUserID *uint  `gorm:"index" json:"owner_user_id,omitempty"`
	Source      string `gorm:"type:varchar(64);not null;default:''" json:"source"`
	SourceRef   string `gorm:"type:varchar(255);not null;default:''" json:"source_ref"`
	CreatedBy   *uint  `json:"created_by,omitempty"`
	UpdatedBy   *uint  `json:"updated_by,omitempty"`
	CreatedAt   time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (Company) TableName() string { return "companies" }

// CustomerCompanyRelation 客户-公司关系（CRM P0）。
// 支持一客户多公司/多角色；is_primary 与 customers.company_id 双写过渡（D-02）。
type CustomerCompanyRelation struct {
	TenantID     uint      `gorm:"not null;default:1;index" json:"tenant_id"`
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CustomerID   string    `gorm:"type:varchar(36);not null;index" json:"customer_id"`
	CompanyID    int64     `gorm:"not null;index" json:"company_id"`
	RelationType string    `gorm:"type:varchar(32);not null;default:'primary'" json:"relation_type"`
	IsPrimary    bool      `gorm:"not null;default:false" json:"is_primary"`
	Source       string    `gorm:"type:varchar(64);not null;default:''" json:"source"`
	SourceRef    string    `gorm:"type:varchar(255);not null;default:''" json:"source_ref"`
	CreatedBy    *uint     `json:"created_by,omitempty"`
	UpdatedBy    *uint     `json:"updated_by,omitempty"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (CustomerCompanyRelation) TableName() string { return "customer_company_relations" }
