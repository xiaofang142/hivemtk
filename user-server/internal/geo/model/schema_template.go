package model

import (
	"time"

	"github.com/google/uuid"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// GeoSchemaTemplate Schema 模板库
//
// 按页面类型 + Schema @type 组织。ContentService.GenerateSchema /
// ContentCreation 前端页面下拉选择都从这里读。
type GeoSchemaTemplate struct {
	ID       string `gorm:"type:varchar(36);primaryKey" json:"id"`
	PageType string `gorm:"type:varchar(50);index" json:"page_type"`
	// 'article' | 'faq' | 'guide' | 'product'
	SchemaType string `gorm:"type:varchar(50);index" json:"schema_type"`
	// 'FAQPage' | 'Product' | 'Article' | 'Comparison' | 'Organization'
	TemplateJSON datatypes.JSON `gorm:"type:jsonb;not null" json:"template_json"`
	Active       bool           `gorm:"default:true" json:"active"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (GeoSchemaTemplate) TableName() string { return "geo_schema_templates" }

func (t *GeoSchemaTemplate) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}
