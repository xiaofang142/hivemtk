package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GeoKeyword GEO 关键词模型
type GeoKeyword struct {
	ID           string  `gorm:"type:varchar(36);primaryKey" json:"id"`
	Keyword      string  `gorm:"type:text;not null" json:"keyword"`
	Category     string  `gorm:"type:varchar(100)" json:"category"`
	Source       string  `gorm:"type:varchar(50)" json:"source"`
	SearchVolume int     `gorm:"default:0" json:"search_volume"`
	Difficulty   float64 `gorm:"default:0" json:"difficulty"`
	Intent       string  `gorm:"type:varchar(50)" json:"intent"`
	Cluster      string  `gorm:"type:varchar(100);index" json:"cluster"`
	FunnelStage  string  `gorm:"column:funnel_stage;size:20;index" json:"funnel_stage"`
	Status       string  `gorm:"type:varchar(20);default:'active'" json:"status"`

	// ⬇️ 新增：4 层漏斗字段
	Layer string `gorm:"size:20;default:'seed';index" json:"layer"`
	// 'seed' | 'related' | 'suggest' | 'longtail'
	ParentID string `gorm:"size:36;index" json:"parent_id"`
	// ParentKeyword 记录派生自哪个种子词（站位用；落库时解析为 ParentID）
	ParentKeyword string `gorm:"size:255" json:"parent_keyword"`
	QueryIntent   string `gorm:"size:30;index" json:"query_intent"`
	// 'how_to' | 'comparison' | 'recommendation' | 'problem' | 'pricing' | 'case_study'
	SuggestEngines string     `gorm:"type:text" json:"suggest_engines"` // JSON 数组
	SuggestCount   int        `gorm:"default:0" json:"suggest_count"`
	LastMinedAt    *time.Time `json:"last_mined_at"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *GeoKeyword) TableName() string {
	return "geo_keywords"
}

func (m *GeoKeyword) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// GeoKeywordGroup GEO 关键词分组模型
type GeoKeywordGroup struct {
	ID           string `gorm:"type:varchar(36);primaryKey" json:"id"`
	Name         string `gorm:"type:varchar(200);not null" json:"name"`
	Description  string `gorm:"type:text" json:"description"`
	KeywordCount int    `gorm:"default:0" json:"keyword_count"`
	// KeywordList 关键词分组内的关键词 JSON 数组文本（避免 GORM text[] 特殊处理）
	KeywordList string `gorm:"type:text" json:"keyword_list"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *GeoKeywordGroup) TableName() string {
	return "geo_keyword_groups"
}

func (m *GeoKeywordGroup) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
