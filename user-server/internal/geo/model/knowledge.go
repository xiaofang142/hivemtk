package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GeoKnowledgeDocument GEO 品牌知识库文档模型
type GeoKnowledgeDocument struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          string `gorm:"type:varchar(36);primaryKey" json:"id"`
	Title       string `gorm:"type:varchar(500)" json:"title"`
	Content     string `gorm:"type:text" json:"content"`
	DocType     string `gorm:"type:varchar(50)" json:"doc_type"`
	SourceLevel string `gorm:"column:source_level;size:2;index" json:"source_level"`
	SourceURL   string `gorm:"column:source_url;size:512" json:"source_url"`
	Metadata    string `gorm:"type:text" json:"metadata"`
	// IsPublic 控制该文档是否可被公开承接页的 FAQ 检索。
	// 默认 false：内部资料（报价、SOP、客户名单）不能因为落在 GEO 知识库
	// 就被公网访客问出来。
	IsPublic bool `gorm:"column:is_public;default:false;index" json:"is_public"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *GeoKnowledgeDocument) TableName() string {
	return "geo_knowledge_documents"
}

func (m *GeoKnowledgeDocument) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
