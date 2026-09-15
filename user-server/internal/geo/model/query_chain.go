package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GeoQueryChain 用户查询思维链（v3 GEO 决策链化 Phase1）
//
// 以 chain_id 聚合同一用户决策会话的多次查询，记录每跳的意图、
// 品牌位置与被引信源，支撑"针对提问深度与多轮交互信源做反推迭代"。
type GeoQueryChain struct {
	ID            string `gorm:"type:varchar(36);primaryKey" json:"id"`
	ChainID       string `gorm:"type:varchar(64);index;not null" json:"chain_id"`
	Seq           int    `gorm:"default:0" json:"seq"`
	Query         string `gorm:"type:text" json:"query"`
	Intent        string `gorm:"type:varchar(20)" json:"intent"`
	BrandName     string `gorm:"type:varchar(200);index" json:"brand_name"`
	BrandPosition string `gorm:"type:varchar(20)" json:"brand_position"`
	CitedURLs     string `gorm:"type:text" json:"cited_urls"`
	Source        string `gorm:"type:varchar(20)" json:"source"`
	// 与 customers.unified_id 对齐为 varchar(128)：手机号型 OneID 长 70 字符
	// （"phone:" + sha256 hex 64 位），varchar(64) 会写入溢出。
	OneID     string         `gorm:"type:varchar(128);index" json:"one_id,omitempty"`
	CreatedAt time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GeoQueryChain) TableName() string { return "geo_query_chains" }

// BeforeCreate 主键为字符串，需显式生成 UUID（否则第二条起主键冲突静默失败）
func (c *GeoQueryChain) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	return nil
}

// GeoContentTask 信源缺口补位任务（content_gap_fill 执行器产出）
type GeoContentTask struct {
	ID        string         `gorm:"type:varchar(36);primaryKey" json:"id"`
	Keyword   string         `gorm:"type:varchar(200);index" json:"keyword"`
	Intent    string         `gorm:"type:varchar(20)" json:"intent"`
	GapType   string         `gorm:"type:varchar(30)" json:"gap_type"`
	Detail    string         `gorm:"type:text" json:"detail"`
	Status    string         `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GeoContentTask) TableName() string { return "geo_content_tasks" }

// BeforeCreate 同上：字符串主键生成
func (t *GeoContentTask) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	return nil
}
