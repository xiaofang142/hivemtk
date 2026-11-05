package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// 审计类型。站点评分走 rubric v2/v3（技术可达性 + 产品叙事），
// 内容评分走 25 因子加权表（可引用性），两者共用 run/item 两张表。
const (
	GeoAuditKindSite    = "site"
	GeoAuditKindContent = "content"
)

// 审计范围。
const (
	GeoAuditScopePage = "page"
	GeoAuditScopeSite = "site"
)

// 审计来源。source 只用于展示与统计，不能靠它推断数据真假；
// 数据真假由 rubric_version / 抓取方式决定。
const (
	GeoAuditSourceManual    = "manual"
	GeoAuditSourceSchedule  = "schedule"
	GeoAuditSourceAutopilot = "autopilot"
	GeoAuditSourceAPI       = "api"
)

// GeoAuditRun 一次 GEO 体检的头部记录。
//
// categories/fixes 同时存一份快照 JSON：详情页与报告不依赖 item 行的顺序，
// item 行则承担 compare 的逐项对齐（分类分可以从 item 聚合复算）。
type GeoAuditRun struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID string `gorm:"type:varchar(36);primaryKey" json:"id"`

	Kind string `gorm:"type:varchar(20);not null;index:idx_geo_audit_runs_lookup,priority:1" json:"kind"`
	// Scope 页面级还是全站级；全站审计的 Score 是各页加权平均，PageCount 为实际审计页数。
	Scope string `gorm:"type:varchar(20);not null;default:'page'" json:"scope"`

	URL   string `gorm:"type:varchar(1024);not null;index:idx_geo_audit_runs_lookup,priority:2" json:"url"`
	Brand string `gorm:"type:varchar(200)" json:"brand"`

	// RubricVersion 是可比性的硬条件：不同版本的总分分母不同，禁止直接相减。
	RubricVersion string `gorm:"type:varchar(32);not null;index:idx_geo_audit_runs_lookup,priority:3" json:"rubric_version"`

	Score    int    `json:"score"`
	MaxScore int    `json:"max_score"`
	Grade    string `gorm:"type:varchar(20)" json:"grade"`

	Categories datatypes.JSON `gorm:"type:jsonb" json:"categories"`
	Fixes      datatypes.JSON `gorm:"type:jsonb" json:"fixes"`
	// PageResults 全站审计的逐页结果；单页审计为 null。
	// 聚合分落在 Score/Categories/Items，逐页明细只用于报告与定位问题。
	PageResults datatypes.JSON `gorm:"type:jsonb" json:"page_results,omitempty"`
	Summary     string         `gorm:"type:text" json:"summary"`

	Source    string `gorm:"type:varchar(20);index" json:"source"`
	PageCount int    `gorm:"default:1" json:"page_count"`
	CreatedBy string `gorm:"type:varchar(100)" json:"created_by"`

	CreatedAt time.Time      `gorm:"index:idx_geo_audit_runs_lookup,priority:4" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (GeoAuditRun) TableName() string { return "geo_audit_runs" }

func (r *GeoAuditRun) BeforeCreate(_ *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

// GeoAuditItem 审计的逐项结果。
//
// Category 与 Check 是 compare 的对齐键；同一 run 内 (category, check) 唯一。
// SortOrder 保留报告渲染顺序（分类顺序与检查项顺序都由评分函数决定）。
type GeoAuditItem struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID    uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	RunID string `gorm:"type:varchar(36);not null;index" json:"run_id"`

	Category      string `gorm:"type:varchar(64);not null" json:"category"`
	CategoryLabel string `gorm:"type:varchar(100)" json:"category_label"`
	Check         string `gorm:"type:varchar(120);not null" json:"check"`

	Score int  `json:"score"`
	Max   int  `json:"max"`
	Pass  bool `json:"pass"`

	Detail string `gorm:"type:text" json:"detail"`
	Fix    string `gorm:"type:text" json:"fix"`
	Source string `gorm:"type:varchar(300)" json:"source"`

	SortOrder int       `gorm:"default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

func (GeoAuditItem) TableName() string { return "geo_audit_items" }
