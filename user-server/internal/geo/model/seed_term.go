package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 种子词条类目：技术/概念属性词 / 业务线核心词 / 品牌组合词
const (
	SeedTermCategoryTechAttr  = "tech_attr"
	SeedTermCategoryCoreTerm  = "core_term"
	SeedTermCategoryBrandCombo = "brand_combo"
)

// GeoSeedTerm GEO 关键词种子词条（数据库驱动，替代硬编码 TechStackAttrs）。
//
// 设计：独立部署的首轮运行时若表为空，由 SeedTermService 用内置缺省包
// （TechStackAttrs + BusinessLines + 品牌组合）自动播种；之后 DB 即唯一
// 真实来源，可经 /geo/seed-terms 接口查看与维护。
type GeoSeedTerm struct {
	ID           string `gorm:"type:varchar(36);primaryKey" json:"id"`
	Term         string `gorm:"type:varchar(200);uniqueIndex;not null" json:"term"`
	Category     string `gorm:"type:varchar(32);index" json:"category"`
	BusinessLine string `gorm:"type:varchar(64);index;default:''" json:"business_line"`
	Source       string `gorm:"type:varchar(200);default:''" json:"source"`
	Active       bool   `gorm:"default:true;index" json:"active"`
	Sort         int    `gorm:"default:0;index" json:"sort"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *GeoSeedTerm) TableName() string {
	return "geo_seed_terms"
}

func (m *GeoSeedTerm) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	return nil
}
