package model

import (
	"time"

	"github.com/google/uuid"

	"gorm.io/gorm"
)

// GeoPushRecord 蜘蛛推送记录
//
// 每次 PushService 向百度/Google/IndexNow 等推送 URL 时落一条记录，
// 用于配额追踪、失败重试、收录验证关联。
type GeoPushRecord struct {
	ID        string `gorm:"type:varchar(36);primaryKey" json:"id"`
	ArticleID string `gorm:"type:varchar(36);index" json:"article_id"`
	Platform  string `gorm:"type:varchar(30);index" json:"platform"`
	// 'baidu' | 'google' | 'indexnow' | 'toutiao' | 'shenma' | 'sitemap'
	URL          string     `gorm:"type:text;index" json:"url"`
	BatchID      string     `gorm:"type:varchar(36)" json:"batch_id"`
	SuccessCount int        `gorm:"default:0" json:"success_count"`
	FailCount    int        `gorm:"default:0" json:"fail_count"`
	RemainQuota  int        `json:"remain_quota"`
	ErrorMsg     string     `gorm:"type:text" json:"error_msg"`
	PushedAt     time.Time  `gorm:"autoCreateTime;index" json:"pushed_at"`
	Verified     bool       `gorm:"default:false" json:"verified"`
	VerifiedAt   *time.Time `json:"verified_at"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (GeoPushRecord) TableName() string { return "geo_push_records" }

func (r *GeoPushRecord) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}
