package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GeoArticle GEO 生成文章模型
type GeoArticle struct {
	ID          string  `gorm:"type:varchar(36);primaryKey" json:"id"`
	Title       string  `gorm:"type:varchar(500)" json:"title"`
	Content     string  `gorm:"type:text" json:"content"`
	Keyword     string  `gorm:"type:varchar(500)" json:"keyword"`
	Model       string  `gorm:"type:varchar(100)" json:"model"`
	Prompt      string  `gorm:"type:text" json:"prompt"`
	WordCount   int     `gorm:"default:0" json:"word_count"`
	Status      string  `gorm:"type:varchar(20);default:'draft'" json:"status"`
	Score       float64 `gorm:"default:0" json:"score"`
	ScoreDetail string  `gorm:"type:text" json:"score_detail"`
	JSONLD      string  `gorm:"column:json_ld;type:text" json:"json_ld"`
	BrandName   string  `gorm:"type:varchar(200)" json:"brand_name"`

	// ⬇️ 新增：静态站部署字段
	SiteURL      string     `gorm:"type:varchar(500)" json:"site_url"`
	SitePath     string     `gorm:"type:varchar(200)" json:"site_path"`
	SchemaJSON   string     `gorm:"type:text" json:"schema_json"`
	MarkdownPath string     `gorm:"type:varchar(200)" json:"markdown_path"`
	DeployedAt   *time.Time `json:"deployed_at"`
	LlmsIncluded bool       `gorm:"default:false" json:"llms_included"`

	// ⬇️ 新增：漏斗分组字段（llms.txt 按层分组、frontmatter 输出）
	FunnelStage string `gorm:"column:funnel_stage;size:20;index" json:"funnel_stage"`
	QueryIntent string `gorm:"size:30;index" json:"query_intent"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *GeoArticle) TableName() string {
	return "geo_articles"
}

func (m *GeoArticle) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// GeoOptimization GEO 内容优化记录模型
type GeoOptimization struct {
	ID               string  `gorm:"type:varchar(36);primaryKey" json:"id"`
	ArticleID        string  `gorm:"type:varchar(36);index" json:"article_id"`
	OriginalContent  string  `gorm:"type:text" json:"original_content"`
	OptimizedContent string  `gorm:"type:text" json:"optimized_content"`
	ScoreBefore      float64 `gorm:"default:0" json:"score_before"`
	ScoreAfter       float64 `gorm:"default:0" json:"score_after"`
	Suggestions      string  `gorm:"type:text" json:"suggestions"`
	Model            string  `gorm:"type:varchar(100)" json:"model"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *GeoOptimization) TableName() string {
	return "geo_optimizations"
}

func (m *GeoOptimization) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
