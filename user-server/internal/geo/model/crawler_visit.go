package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	CrawlerSourceUnknown       = "unknown"
	CrawlerSourceActiveUAProbe = "active_ua_probe"
	CrawlerSourceUAObservation = "ua_observation"
)

// GeoCrawlerVisit 保存主动 UA 探测或未经身份验证的 UA 访问观察。
// UA 不能证明请求来自外部 AI 引擎；旧记录加列后保持 unknown，不猜测回填来源。
type GeoCrawlerVisit struct {
	TenantID uint `gorm:"not null;default:1;index;index:idx_geo_crawler_probe_window,priority:1" json:"tenant_id"`

	ID        uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Keyword   string         `gorm:"type:varchar(200);index" json:"keyword"`
	UserAgent string         `gorm:"type:varchar(255)" json:"user_agent"`
	Path      string         `gorm:"type:varchar(500)" json:"path"`
	Engine    string         `gorm:"type:varchar(50);index" json:"engine"`
	IP        string         `gorm:"type:varchar(45)" json:"ip,omitempty"`
	Source    string         `gorm:"type:text;not null;default:unknown;index:idx_geo_crawler_probe_window,priority:2" json:"source"`
	CreatedAt time.Time      `gorm:"autoCreateTime;index;index:idx_geo_crawler_probe_window,priority:3" json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GeoCrawlerVisit) TableName() string { return "geo_crawler_visits" }
