package model

import (
	"time"

	"gorm.io/gorm"
)

// GeoSearchQuery Google Search Console 性能数据（CSV 导入）。
//
// GSC 导出的 query/page 组合很长，直接用 (query,page) 建唯一索引会触发
// Postgres btree 行大小上限；RowKey 存 (date|query|page) 的 sha256，
// 唯一索引落在 (tenant_id, row_key) 上。
type GeoSearchQuery struct {
	TenantID uint `gorm:"not null;default:1;index;uniqueIndex:idx_geo_search_queries_row,priority:1" json:"tenant_id"`

	ID     uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	RowKey string `gorm:"type:varchar(64);not null;uniqueIndex:idx_geo_search_queries_row,priority:2" json:"row_key"`

	StatDate    time.Time `gorm:"type:date;not null;index" json:"stat_date"`
	Query       string    `gorm:"type:text" json:"query"`
	Page        string    `gorm:"type:text" json:"page"`
	Clicks      int       `json:"clicks"`
	Impressions int       `json:"impressions"`
	CTR         float64   `json:"ctr"`
	Position    float64   `json:"position"`
	Source      string    `gorm:"type:varchar(20);default:'gsc_csv'" json:"source"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (GeoSearchQuery) TableName() string { return "geo_search_queries" }
