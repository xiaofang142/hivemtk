package model

import (
	"time"

	"gorm.io/gorm"
)

// GeoCaptureLink GEO 承接链接：把 AI 引用/文章流量接到一个可自助问答、
// 可留资的公开落地页，并记录点击/线索/首响。
//
// Code 是公开入口的自证键，必须全局唯一（索引名带 global_uniq_ 前缀，
// 避免被多租户唯一索引规范化改成 (tenant_id, code)——那会让同码在
// 两个租户并存，公开入口无法判定归属，只能拒绝服务）。
type GeoCaptureLink struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID   uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Code string `gorm:"size:32;not null;uniqueIndex:global_uniq_geo_capture_code" json:"code"`

	// ShortLinkID 指向 short_links.id；未配置公网基座时为 0（只有站内落地页路径）。
	ShortLinkID uint `gorm:"default:0;index" json:"short_link_id"`

	// ArticleID 对应 geo_articles.id（varchar(36) UUID）。此前是 uint，
	// 前端填的数字永远对不上文章主键；这里按真实类型存字符串。
	ArticleID string `gorm:"type:varchar(36);index" json:"article_id"`
	ChainID   string `gorm:"size:255;index" json:"chain_id"`
	// SourceID 写进 clues.source_id，用于把线索归因回本条承接链接。
	SourceID  string `gorm:"size:255;index" json:"source_id"`
	Keyword   string `gorm:"size:255" json:"keyword"`
	Title     string `gorm:"size:255" json:"title"`
	TargetURL string `gorm:"size:2048" json:"target_url"`

	Clicks int64 `gorm:"default:0" json:"clicks"`
	Leads  int64 `gorm:"default:0" json:"leads"`

	// 公开端点的按日配额：日期 + 计数，落在同一行上原子递增。
	AskDate   *time.Time `gorm:"type:date" json:"-"`
	AskCount  int        `gorm:"default:0" json:"ask_count"`
	LeadDate  *time.Time `gorm:"type:date" json:"-"`
	LeadCount int        `gorm:"default:0" json:"lead_count"`

	CreatedBy string `gorm:"size:100" json:"created_by"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (GeoCaptureLink) TableName() string { return "geo_capture_links" }
