package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GeoConfig GEO 全局配置模型（单例：固定 ID=1）
type GeoConfig struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID               string `gorm:"type:varchar(36);primaryKey" json:"id"`
	BrandName        string `gorm:"type:varchar(200)" json:"brand_name"`
	BrandDescription string `gorm:"type:text" json:"brand_description"`
	Advantages       string `gorm:"type:text" json:"advantages"`
	Competitors      string `gorm:"type:text" json:"competitors"`
	Domain           string `gorm:"type:varchar(500)" json:"domain"`
	Language         string `gorm:"type:varchar(20);default:'zh'" json:"language"`
	DefaultModel     string `gorm:"type:varchar(100)" json:"default_model"`
	VerifyModels     string `gorm:"type:text" json:"verify_models"`
	// NegativeKeywords 负面监控种子词（逗号分隔；空则用后端默认种子）
	NegativeKeywords string `gorm:"type:text" json:"negative_keywords"`
	// CronSpecs 定时任务调度配置 JSON map：{"sov_refresh":"0 0 2 * * *",...}
	CronSpecs string `gorm:"type:text" json:"cron_specs"`

	// 产品叙事（官网六问）：AI 需要的不是“漂亮页面”，而是能直接回答
	// 这是什么产品/核心功能/适合谁/代际差异/竞品差异/最新版本。
	ProductCategory string `gorm:"type:varchar(200)" json:"product_category"`
	TargetAudience  string `gorm:"type:varchar(500)" json:"target_audience"`
	KeyFeatures     string `gorm:"type:text" json:"key_features"`
	GenerationNote  string `gorm:"type:text" json:"generation_note"`
	VersionLabel    string `gorm:"type:varchar(100)" json:"version_label"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *GeoConfig) TableName() string {
	return "geo_config"
}

func (m *GeoConfig) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// GeoPlatformAccount GEO 平台账号模型
type GeoPlatformAccount struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          string `gorm:"type:varchar(36);primaryKey" json:"id"`
	Platform    string `gorm:"type:varchar(50);index" json:"platform"`
	AccountID   string `gorm:"type:varchar(200)" json:"account_id"`
	AccountName string `gorm:"type:varchar(200)" json:"account_name"`
	Status      string `gorm:"type:varchar(20);default:'active'" json:"status"`
	Config      string `gorm:"type:text" json:"config"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *GeoPlatformAccount) TableName() string {
	return "geo_platform_accounts"
}

func (m *GeoPlatformAccount) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// GeoPublishRecord GEO 发布记录模型
type GeoPublishRecord struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID           string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	ArticleID    string    `gorm:"type:varchar(36);index" json:"article_id"`
	Platform     string    `gorm:"type:varchar(50)" json:"platform"`
	AccountID    string    `gorm:"type:varchar(200)" json:"account_id"`
	Status       string    `gorm:"type:varchar(20);default:'pending'" json:"status"`
	PublishedURL string    `gorm:"type:varchar(500)" json:"published_url"`
	PublishedAt  time.Time `json:"published_at"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *GeoPublishRecord) TableName() string {
	return "geo_publish_records"
}

func (m *GeoPublishRecord) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
