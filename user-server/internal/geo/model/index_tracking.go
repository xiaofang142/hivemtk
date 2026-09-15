package model

import (
	"time"

	"github.com/google/uuid"

	"gorm.io/gorm"
)

// GeoIndexTracking 收录 + AI 引用追踪
//
// IndexTrackerService.VerifyArticleFull 写入。
// FunnelDashboard / IndexTracking 页面读取。
// engine 字段同时覆盖搜索引擎（baidu/google/bing）和 AI 引擎（doubao/wenxin/kimi/deepseek）。
type GeoIndexTracking struct {
	ID        string `gorm:"type:varchar(36);primaryKey" json:"id"`
	ArticleID string `gorm:"type:varchar(36);index" json:"article_id"`
	Engine    string `gorm:"type:varchar(30);index" json:"engine"`
	// 'baidu' | 'google' | 'bing' | 'toutiao' | 'shenma' | '360' | 'doubao' | 'wenxin' | 'kimi' | 'deepseek'
	Keyword      string     `gorm:"type:text" json:"keyword"`
	Indexed      bool       `gorm:"default:false" json:"indexed"`
	RankPosition int        `json:"rank_position"`
	AICited      bool       `gorm:"default:false" json:"ai_cited"`
	AICiteCount  int        `gorm:"default:0" json:"ai_cite_count"`
	LastChecked  *time.Time `json:"last_checked"`

	CreatedAt time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (GeoIndexTracking) TableName() string { return "geo_index_trackings" }

func (t *GeoIndexTracking) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}
